package main

// Carga y guardado de horario.json + tareas.json con escritura atómica y
// respaldo previo. Toda modificación pasa por almacen.modificar, que serializa
// leer → cambiar → validar → guardar con un mutex: un botón ✅ y un turno de
// IA nunca se pisan.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type almacen struct {
	dir string
	mu  sync.Mutex
}

func nuevoAlmacen(dir string) *almacen { return &almacen{dir: dir} }

func (s *almacen) rutaHorario() string { return filepath.Join(s.dir, "horario.json") }
func (s *almacen) rutaTareas() string  { return filepath.Join(s.dir, "tareas.json") }

// cargar lee ambos archivos; si no existen aún, devuelve una agenda vacía.
func (s *almacen) cargar() (*Agenda, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cargarSinLock()
}

func (s *almacen) cargarSinLock() (*Agenda, error) {
	var horario archivoHorario
	if err := leerJSON(s.rutaHorario(), &horario); err != nil {
		return nil, BridgeError("horario.json: " + err.Error())
	}
	var tareas archivoTareas
	if err := leerJSON(s.rutaTareas(), &tareas); err != nil {
		return nil, BridgeError("tareas.json: " + err.Error())
	}
	agenda := &Agenda{Bloques: horario.Bloques, Tareas: tareas.Tareas, Config: horario.Config}
	if agenda.Bloques == nil {
		agenda.Bloques = []Bloque{}
	}
	if agenda.Tareas == nil {
		agenda.Tareas = []Tarea{}
	}
	agenda.Config.valoresPorDefecto()
	if err := validarAgenda(agenda); err != nil {
		return nil, BridgeError("los datos no respetan el esquema: " + err.Error())
	}
	return agenda, nil
}

// modificar aplica cambio sobre la agenda fresca y guarda si devuelve nil.
func (s *almacen) modificar(cambio func(*Agenda) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	agenda, err := s.cargarSinLock()
	if err != nil {
		return err
	}
	if err := cambio(agenda); err != nil {
		return err
	}
	if err := validarAgenda(agenda); err != nil {
		return BridgeError("el cambio no respeta el esquema: " + err.Error())
	}
	if err := escribirJSON(s.rutaHorario(), s.dir, archivoHorario{Bloques: agenda.Bloques, Config: agenda.Config}); err != nil {
		return err
	}
	return escribirJSON(s.rutaTareas(), s.dir, archivoTareas{Tareas: agenda.Tareas})
}

// leerJSON tolera archivo ausente o vacío (deja el destino en cero).
func leerJSON(ruta string, destino any) error {
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return BridgeError("no se pudo leer; revisa el volumen de datos")
	}
	if len(contenido) == 0 {
		return nil
	}
	if err := json.Unmarshal(contenido, destino); err != nil {
		return BridgeError("no es JSON válido; no lo toco hasta que se arregle a mano")
	}
	return nil
}

// escribirJSON guarda atómicamente (tmp → rename) y respalda la versión
// anterior en <dir>/respaldos/, por si la IA propone algo inesperado.
func escribirJSON(ruta, dirDatos string, valor any) error {
	final, err := json.MarshalIndent(valor, "", "  ")
	if err != nil {
		return BridgeError("no se pudo serializar " + filepath.Base(ruta))
	}
	if antes, err := os.ReadFile(ruta); err == nil {
		if string(antes) == string(final) {
			return nil // sin cambios
		}
		dirRespaldos := filepath.Join(dirDatos, "respaldos")
		if err := os.MkdirAll(dirRespaldos, 0o700); err != nil {
			return BridgeError("no se pudo preparar el respaldo")
		}
		if err := os.WriteFile(filepath.Join(dirRespaldos, filepath.Base(ruta)), antes, 0o600); err != nil {
			return BridgeError("no se pudo respaldar " + filepath.Base(ruta))
		}
	}
	return escribirAtomico(ruta, final)
}

func escribirAtomico(ruta string, contenido []byte) error {
	if err := os.MkdirAll(filepath.Dir(ruta), 0o700); err != nil {
		return BridgeError("no se pudo preparar la carpeta de datos")
	}
	tmp := ruta + ".tmp"
	if err := os.WriteFile(tmp, contenido, 0o600); err != nil {
		return BridgeError("no se pudo escribir " + filepath.Base(ruta))
	}
	if err := os.Rename(tmp, ruta); err != nil {
		os.Remove(tmp)
		return BridgeError("no se pudo guardar " + filepath.Base(ruta))
	}
	return nil
}
