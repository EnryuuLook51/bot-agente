package main

// Tests de la lógica determinista: esquema, aplicador de acciones, almacén
// en dos archivos y el parser JSON del modelo.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func agendaDePrueba() *Agenda {
	return &Agenda{
		Bloques: []Bloque{
			{ID: "b1", Tipo: "clase", Nombre: "Base de datos 2", Fuente: "HORARIO", Dia: "lun", Inicio: "08:00", Fin: "10:00"},
			{ID: "b2", Tipo: "estudio", Nombre: "Estudio nocturno", Fuente: "PERSONAL", Dia: "diario", Inicio: "21:00", Fin: "22:30"},
		},
		Tareas: []Tarea{
			{ID: "i1", Texto: "Leer capítulo 3", Estado: "pendiente", Fecha: "2026-11-13", Origen: "usuario"},
			{ID: "i2", Texto: "Resolver guía 1", Estado: "pendiente", Bloque: "b1", Origen: "ia"},
			{ID: "i3", Texto: "Tarea pasada", Estado: "completada", Fecha: "2026-11-01", Origen: "usuario"},
		},
		Config: Config{Fuentes: []string{"X.md"}, AvisoMinutos: 15, Recordatorios: []string{"06:30", "21:00"}},
	}
}

func TestValidarAgendaAceptaBuena(t *testing.T) {
	if err := validarAgenda(agendaDePrueba()); err != nil {
		t.Fatalf("agenda válida rechazada: %v", err)
	}
}

func TestValidarAgendaRechazaMalas(t *testing.T) {
	casos := []func(*Agenda){
		func(a *Agenda) { a.Bloques[0].Dia = "lunes" },
		func(a *Agenda) { a.Bloques[0].Inicio = "8:0" },
		func(a *Agenda) { a.Bloques[0].Inicio = "25:00" },
		func(a *Agenda) { a.Bloques[0].Fin = "07:00" }, // termina antes de empezar
		func(a *Agenda) { a.Tareas[0].Estado = "haciendo" },
		func(a *Agenda) { a.Tareas[0].Fecha = "2026-13-40" },
		func(a *Agenda) { a.Tareas[1].Bloque = "zz" },
		func(a *Agenda) { a.Bloques[1].ID = "b1" },
		func(a *Agenda) { a.Tareas[0].ID = "b1" }, // choca con un bloque
		func(a *Agenda) { a.Config.Recordatorios = []string{"6:30pm"} },
	}
	for i, rompe := range casos {
		agenda := agendaDePrueba()
		rompe(agenda)
		if err := validarAgenda(agenda); err == nil {
			t.Fatalf("caso %d: validación pasó con datos rotos", i)
		}
	}
}

func TestAplicarAccionesAgenda(t *testing.T) {
	agenda := agendaDePrueba()
	resumen, aplicadas := aplicarAcciones(agenda, []map[string]any{
		{"tipo": "agregar_tarea", "texto": "Estudiar TAD", "fecha": "2026-11-14"},
		{"tipo": "completar_tarea", "id": "i1"},
		{"tipo": "asignar_tarea", "id": "i1", "fecha": "", "bloque": "b2"},
		{"tipo": "editar_tarea", "id": "i2", "texto": "Resolver guía 2"},
		{"tipo": "eliminar_tarea", "id": "i3"},
		{"tipo": "agregar_bloque", "tipobloque": "personal", "nombre": "Gimnasio", "fuente": "PERSONAL",
			"dia": "mié", "inicio": "9:00", "fin": "10:30"},
		{"tipo": "editar_bloque", "id": "b1", "inicio": "7:30"},
		{"tipo": "eliminar_bloque", "id": "i999"}, // no existe: se reporta y sigue
	})
	if aplicadas != 7 {
		t.Fatalf("esperaba 7 aplicadas, hubo %d: %v", aplicadas, resumen)
	}
	if err := validarAgenda(agenda); err != nil {
		t.Fatalf("resultado inválido: %v", err)
	}
	if texto := agenda.encontrarTarea("i2").Texto; texto != "Resolver guía 2" {
		t.Fatalf("editar_tarea no aplicó: %q", texto)
	}
	if inicio := agenda.encontrarBloque("b1").Inicio; inicio != "07:30" {
		t.Fatalf("editar_bloque no normalizó la hora: %q", inicio)
	}
	if b := agenda.encontrarBloque("b3"); b == nil || b.Inicio != "09:00" {
		t.Fatalf("agregar_bloque mal armado: %+v", b)
	}
	if len(agenda.Tareas) != 3 {
		t.Fatalf("esperaba 3 tareas, hay %d", len(agenda.Tareas))
	}
	nueva := agenda.encontrarTarea("i4")
	if nueva == nil || nueva.Texto != "Estudiar TAD" || nueva.Origen != "ia" {
		t.Fatalf("tarea nueva mal armada: %+v", nueva)
	}
}

func TestAccionInvalidaSeDescartaSinRomperLasDemas(t *testing.T) {
	agenda := agendaDePrueba()
	resumen, aplicadas := aplicarAcciones(agenda, []map[string]any{
		{"tipo": "agregar_tarea", "texto": "Con bloque inventado", "bloque": "b99"},
		{"tipo": "agregar_tarea", "texto": "Con fecha rota", "fecha": "mañana"},
		{"tipo": "editar_bloque", "id": "b1", "fin": "06:00"},
		{"tipo": "agregar_tarea", "texto": "Buena"},
	})
	if aplicadas != 1 || len(agenda.Tareas) != 4 {
		t.Fatalf("solo debía aplicarse la buena: %d %v", aplicadas, resumen)
	}
	if agenda.encontrarBloque("b1").Fin != "10:00" {
		t.Fatal("la edición inválida del bloque no se deshizo")
	}
	if err := validarAgenda(agenda); err != nil {
		t.Fatalf("agenda quedó inválida: %v", err)
	}
}

func TestEliminarBloquePideConfirmacion(t *testing.T) {
	agenda := agendaDePrueba()
	resumen, _ := aplicarAcciones(agenda, []map[string]any{{"tipo": "eliminar_bloque", "id": "b1"}})
	if len(resumen) == 0 || !strings.Contains(resumen[0], "confirmación") {
		t.Fatalf("debió pedir confirmación: %v", resumen)
	}
	_, aplicadas := aplicarAcciones(agenda, []map[string]any{{"tipo": "eliminar_bloque", "id": "b1", "confirmado": true}})
	if aplicadas != 1 || agenda.encontrarBloque("b1") != nil {
		t.Fatal("bloque no eliminado")
	}
	if agenda.encontrarTarea("i2").Bloque != "" {
		t.Fatal("la tarea del bloque eliminado debió quedar suelta")
	}
}

func TestAlmacenDosArchivos(t *testing.T) {
	dir := t.TempDir()
	datos := nuevoAlmacen(dir)
	vacia, err := datos.cargar()
	if err != nil || len(vacia.Bloques) != 0 || len(vacia.Config.Recordatorios) != 2 {
		t.Fatalf("agenda inicial inesperada: %+v %v", vacia, err)
	}
	prueba := agendaDePrueba()
	if err := datos.modificar(func(a *Agenda) error { *a = *prueba; return nil }); err != nil {
		t.Fatal(err)
	}
	horario, _ := os.ReadFile(filepath.Join(dir, "horario.json"))
	tareas, _ := os.ReadFile(filepath.Join(dir, "tareas.json"))
	if !strings.Contains(string(horario), "Base de datos 2") || strings.Contains(string(horario), "Leer capítulo") {
		t.Fatalf("horario.json mal separado: %s", horario)
	}
	if !strings.Contains(string(tareas), "Leer capítulo") || strings.Contains(string(tareas), "bloques") {
		t.Fatalf("tareas.json mal separado: %s", tareas)
	}
	// Un cambio inválido no se escribe.
	err = datos.modificar(func(a *Agenda) error { a.Tareas[0].Estado = "x"; return nil })
	if err == nil {
		t.Fatal("debió rechazar el cambio inválido")
	}
	releida, err := datos.cargar()
	if err != nil || releida.encontrarTarea("i1").Estado != "pendiente" {
		t.Fatalf("el cambio inválido llegó a disco: %v", err)
	}
	// Segundo cambio válido: deja respaldo de la versión anterior.
	if err := datos.modificar(func(a *Agenda) error { _, err := marcarCompletada(a, "i1"); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "respaldos", "tareas.json")); err != nil {
		t.Fatal("no dejó respaldo de tareas.json")
	}
}

func TestRegistroNoFallaYAnota(t *testing.T) {
	dir := t.TempDir()
	reg := nuevoRegistro(dir)
	reg.anotar("entrada", "hola")
	reg.anotar("salida", "chau")
	contenido, _ := os.ReadFile(filepath.Join(dir, "registro.jsonl"))
	if strings.Count(string(contenido), "\n") != 2 || !strings.Contains(string(contenido), `"tipo":"entrada"`) {
		t.Fatalf("registro inesperado: %s", contenido)
	}
	var nulo *registro
	nulo.anotar("x", "y") // no debe entrar en pánico
}

func TestExtraerJSONConMarkdown(t *testing.T) {
	texto := "Claro, aquí tienes:\n```json\n" +
		`{"sugerencias":[{"texto":"estudiar x { con llaves","razon":"y"}],"mensaje":"ok"}` + "\n```"
	obtenido := extraerJSON(texto)
	if !strings.HasPrefix(obtenido, "{") || !strings.HasSuffix(obtenido, "}") {
		t.Fatalf("recorte malo: %q", obtenido)
	}
	if !strings.Contains(obtenido, `"texto":"estudiar x { con llaves"`) {
		t.Fatalf("pierde contenido con llaves internas: %q", obtenido)
	}
}

func TestDecodificarAcciones(t *testing.T) {
	acc, mensaje, err := decodificarAcciones("```json" + `{"acciones":[{"tipo":"completar_tarea","id":"i1"}],"mensaje":"completada"}` + "```")
	if err != nil {
		t.Fatal(err)
	}
	if len(acc) != 1 || acc[0]["id"] != "i1" || mensaje != "completada" {
		t.Fatalf("mal parseado: %+v %q", acc, mensaje)
	}
	if _, _, err := decodificarAcciones("te entiendo, pero no puedo"); err == nil {
		t.Fatal("debía fallar con texto no-JSON")
	}
}

// groqFalso responde como /chat/completions; con fallos429 > 0, los primeros
// intentos devuelven 429.
func groqFalso(t *testing.T, estado int, fallos429 int, contenido string) (*clienteIA, *int) {
	llamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamadas++
		if r.Header.Get("Authorization") != "Bearer clave" {
			t.Errorf("falta la clave en la cabecera")
		}
		var cuerpo struct {
			Model    string        `json:"model"`
			Messages []mensajeChat `json:"messages"`
			Formato  struct {
				Tipo string `json:"type"`
			} `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		if cuerpo.Model != "m" || len(cuerpo.Messages) != 2 || cuerpo.Messages[0].Rol != "system" || cuerpo.Formato.Tipo != "json_object" {
			t.Errorf("petición mal armada: %+v", cuerpo)
		}
		if llamadas <= fallos429 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(estado)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": contenido}}},
		})
	}))
	t.Cleanup(srv.Close)
	c := nuevoClienteIA("clave", "m")
	c.url = srv.URL
	c.espera = func(time.Duration) {}
	return c, &llamadas
}

func TestGroqRespondeJSON(t *testing.T) {
	c, _ := groqFalso(t, 200, 0, `{"acciones":[],"mensaje":"hola"}`)
	texto, err := c.completar("sistema", nil, "usuario")
	if err != nil || texto != `{"acciones":[],"mensaje":"hola"}` {
		t.Fatalf("respuesta inesperada: %q %v", texto, err)
	}
}

func TestGroqReintentaTras429(t *testing.T) {
	c, llamadas := groqFalso(t, 200, 2, `{"ok":true}`)
	if texto, err := c.completar("s", nil, "u"); err != nil || texto != `{"ok":true}` || *llamadas != 3 {
		t.Fatalf("no reintentó bien: %q %v llamadas=%d", texto, err, *llamadas)
	}
	c, llamadas = groqFalso(t, 200, 10, `{}`)
	if _, err := c.completar("s", nil, "u"); err == nil || *llamadas != 4 {
		t.Fatalf("debió rendirse tras 3 reintentos: %v llamadas=%d", err, *llamadas)
	}
}

func TestGroqClaveRechazada(t *testing.T) {
	c, _ := groqFalso(t, 401, 0, "")
	_, err := c.completar("s", nil, "u")
	if err == nil || !strings.Contains(err.Error(), "GROQ_API_KEY") {
		t.Fatalf("error inesperado: %v", err)
	}
}

func TestMensajeCoherenteConLoGuardado(t *testing.T) {
	guardado := []string{"✔ programada: leer libro (domingo 8 de noviembre a las 10:00)"}
	if mensajeCoherente("Lo puse el sábado a las 10:00 porque está libre.", guardado) {
		t.Fatal("debió descartar un mensaje que nombra otro día")
	}
	if !mensajeCoherente("Elegí esa hora porque no tienes clases.", guardado) {
		t.Fatal("un motivo sin días debe mostrarse")
	}
	if !mensajeCoherente("El domingo tienes la mañana libre.", guardado) {
		t.Fatal("nombrar el mismo día guardado es válido")
	}
}
