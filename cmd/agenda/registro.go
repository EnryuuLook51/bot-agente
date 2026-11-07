package main

// Registro de entradas y salidas del bot (registro.jsonl, una línea JSON por
// evento): mensajes recibidos, respuestas enviadas y cambios aplicados.
// Es solo para auditoría y conteo de horas: NUNCA se manda a la IA.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const limiteRegistro = 5 << 20 // al pasar 5 MB se rota a registro.1.jsonl

type registro struct {
	ruta string
	mu   sync.Mutex
}

type eventoRegistro struct {
	Fecha string `json:"ts"`
	Tipo  string `json:"tipo"` // entrada | salida | cambio | error
	Texto string `json:"texto"`
}

func nuevoRegistro(dir string) *registro {
	return &registro{ruta: filepath.Join(dir, "registro.jsonl")}
}

// anotar agrega un evento; los fallos se ignoran (el registro no debe tumbar el bot).
func (r *registro) anotar(tipo, texto string) {
	if r == nil {
		return
	}
	linea, err := json.Marshal(eventoRegistro{
		Fecha: time.Now().In(lima()).Format(time.RFC3339),
		Tipo:  tipo,
		Texto: texto,
	})
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, err := os.Stat(r.ruta); err == nil && info.Size() > limiteRegistro {
		_ = os.Rename(r.ruta, r.ruta[:len(r.ruta)-len(".jsonl")]+".1.jsonl")
	}
	archivo, err := os.OpenFile(r.ruta, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer archivo.Close()
	_, _ = archivo.Write(append(linea, '\n'))
}
