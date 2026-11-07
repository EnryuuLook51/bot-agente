package main

// Estado interno del bot (offset de Telegram, avisos ya mandados,
// sugerencias pendientes de aceptar). Archivo chico en DATOS_DIR. Todos los
// accesos pasan por el mutex: lo tocan el loop de Telegram, el ticker y el
// worker de IA.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type estadoBot struct {
	mu             sync.Mutex            // protege todo lo de abajo
	dir            string                // dónde se persiste
	Offset         int                   `json:"offset"`
	AvisosEnviados map[string]bool       `json:"avisos"`      // "b1@2026-11-05", "rec06:30@2026-11-05"
	Sugerencias    map[string]sugerencia `json:"sugerencias"` // clave de callback → pendiente
}

// sugerencia es una propuesta de /sugerir esperando decisión.
type sugerencia struct {
	Texto     string `json:"texto"`
	Bloque    string `json:"bloque"`
	Razon     string `json:"razon"`
	MensajeID int    `json:"mensajeId"`
}

// cargarEstado lee <dir>/estado.json (vacío si es la primera vez).
func cargarEstado(dir string) *estadoBot {
	datos := &estadoBot{dir: dir}
	if contenido, err := os.ReadFile(filepath.Join(dir, "estado.json")); err == nil {
		_ = json.Unmarshal(contenido, datos) // corrupto: arranca limpio
	}
	if datos.AvisosEnviados == nil {
		datos.AvisosEnviados = map[string]bool{}
	}
	if datos.Sugerencias == nil {
		datos.Sugerencias = map[string]sugerencia{}
	}
	return datos
}

// cambiar ejecuta f con el lock tomado y persiste el resultado.
func (e *estadoBot) cambiar(f func(*estadoBot)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e)
	final, err := json.Marshal(e)
	if err != nil {
		return
	}
	// En fallo se sigue: lo peor es repetir un aviso o perder una sugerencia.
	_ = escribirAtomico(filepath.Join(e.dir, "estado.json"), final)
}

// leer ejecuta f con el lock tomado, sin persistir.
func (e *estadoBot) leer(f func(*estadoBot)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e)
}

// podarAvisos borra las marcas de días anteriores (el mapa no crece sin fin).
func (e *estadoBot) podarAvisos(hoy string) {
	for llave := range e.AvisosEnviados {
		if !strings.HasSuffix(llave, "@"+hoy) {
			delete(e.AvisosEnviados, llave)
		}
	}
}

func llaveAviso(id, fecha string) string {
	return id + "@" + fecha
}
