package main

// Esquema de los datos del bot. Viven fuera de la bóveda (DATOS_DIR), en dos
// archivos que solo escribe este bot:
//   horario.json  bloques fijos (clases de la uni, gimnasio, estudio...) + config
//   tareas.json   tareas móviles (pendientes / completadas)
// En memoria se trabaja con Agenda, la unión de ambos.

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Fuentes       []string `json:"fuentes"`       // .md de la bóveda que alimentan /sugerir
	AvisoMinutos  int      `json:"avisoMinutos"`  // aviso antes de cada bloque; 0 = 15, <0 = apagado
	Recordatorios []string `json:"recordatorios"` // horas de los recordatorios de pendientes
}

type Bloque struct {
	ID     string `json:"id"`
	Tipo   string `json:"tipo"`   // clase | estudio | personal | ...
	Nombre string `json:"nombre"` // "Base de Datos II · teoría", "Gimnasio"
	Fuente string `json:"fuente"` // HORARIO | PERSONAL
	Dia    string `json:"dia"`    // lun..dom, o "diario"
	Inicio string `json:"inicio"` // "14:00" hora America/Lima
	Fin    string `json:"fin"`    // "18:00"
}

type Tarea struct {
	ID     string `json:"id"`
	Texto  string `json:"texto"`
	Estado string `json:"estado"`           // pendiente | completada
	Fecha  string `json:"fecha,omitempty"`  // "2026-11-06" (o vacío)
	Bloque string `json:"bloque,omitempty"` // id del bloque asignado
	Origen string `json:"origen"`           // usuario | ia | sugerencia
}

// Archivos en disco.
type archivoHorario struct {
	Bloques []Bloque `json:"bloques"`
	Config  Config   `json:"config"`
}

type archivoTareas struct {
	Tareas []Tarea `json:"tareas"`
}

// Agenda es la vista en memoria de ambos archivos.
type Agenda struct {
	Bloques []Bloque `json:"bloques"`
	Tareas  []Tarea  `json:"tareas"`
	Config  Config   `json:"-"` // la IA no necesita la config
}

var diasAbreviados = [7]string{"lun", "mar", "mié", "jue", "vie", "sáb", "dom"}

var formatoHora = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
var formatoID = regexp.MustCompile(`^[a-z]{1,4}\d{1,4}$`)

func diaValido(dia string) bool {
	if dia == "diario" {
		return true
	}
	for _, d := range diasAbreviados {
		if d == dia {
			return true
		}
	}
	return false
}

// normalizarHora acepta "9:00" o "09:00" y devuelve "09:00" (o el original
// si no se puede interpretar; la validación lo rechazará).
func normalizarHora(hora string) string {
	h, m, ok := strings.Cut(strings.TrimSpace(hora), ":")
	if !ok {
		return hora
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || len(m) != 2 {
		return hora
	}
	return fmt.Sprintf("%02d:%02d", hh, mm)
}

func fechaValida(fecha string) bool {
	_, err := time.Parse("2006-01-02", fecha)
	return err == nil
}

// valoresPorDefecto rellena la config ausente.
func (c *Config) valoresPorDefecto() {
	if c.AvisoMinutos == 0 {
		c.AvisoMinutos = 15
	}
	if c.Recordatorios == nil {
		c.Recordatorios = []string{"06:30", "21:00"}
	}
	if c.Fuentes == nil {
		c.Fuentes = []string{}
	}
}

// validarAgenda aplica las reglas del esquema y devuelve un error corto,
// apto para Telegram: nada malformado llega a disco.
func validarAgenda(a *Agenda) error {
	if a == nil {
		return errors.New("agenda vacía")
	}
	vistosBloques := map[string]bool{}
	for i := range a.Bloques {
		b := &a.Bloques[i]
		if !formatoID.MatchString(b.ID) {
			return fmt.Errorf("bloque %d: id inválido %q", i, b.ID)
		}
		if vistosBloques[b.ID] {
			return fmt.Errorf("bloque %s repetido", b.ID)
		}
		vistosBloques[b.ID] = true
		if !diaValido(b.Dia) {
			return fmt.Errorf("bloque %s: día inválido %q", b.ID, b.Dia)
		}
		if !formatoHora.MatchString(b.Inicio) || !formatoHora.MatchString(b.Fin) {
			return fmt.Errorf("bloque %s: horario inválido (%q, %q)", b.ID, b.Inicio, b.Fin)
		}
		if b.Inicio >= b.Fin {
			return fmt.Errorf("bloque %s: termina antes de empezar", b.ID)
		}
		if strings.TrimSpace(b.Nombre) == "" {
			return fmt.Errorf("bloque %s sin nombre", b.ID)
		}
	}
	vistasTareas := map[string]bool{}
	for i := range a.Tareas {
		t := &a.Tareas[i]
		if !formatoID.MatchString(t.ID) {
			return fmt.Errorf("tarea %d: id inválido %q", i, t.ID)
		}
		if vistasTareas[t.ID] || vistosBloques[t.ID] {
			return fmt.Errorf("id %s repetido", t.ID)
		}
		vistasTareas[t.ID] = true
		if strings.TrimSpace(t.Texto) == "" {
			return fmt.Errorf("tarea %s sin texto", t.ID)
		}
		if t.Estado != "pendiente" && t.Estado != "completada" {
			return fmt.Errorf("tarea %s: estado inválido %q", t.ID, t.Estado)
		}
		if t.Fecha != "" && !fechaValida(t.Fecha) {
			return fmt.Errorf("tarea %s: fecha inválida %q", t.ID, t.Fecha)
		}
		if t.Bloque != "" && !vistosBloques[t.Bloque] {
			return fmt.Errorf("tarea %s apunta a bloque inexistente %q", t.ID, t.Bloque)
		}
	}
	for _, h := range a.Config.Recordatorios {
		if !formatoHora.MatchString(h) {
			return fmt.Errorf("config: hora de recordatorio inválida %q", h)
		}
	}
	return nil
}

// copiar devuelve una copia profunda (para deshacer una acción inválida).
func (a *Agenda) copiar() *Agenda {
	c := *a
	c.Bloques = append([]Bloque(nil), a.Bloques...)
	c.Tareas = append([]Tarea(nil), a.Tareas...)
	return &c
}

func (a *Agenda) encontrarBloque(id string) *Bloque {
	for i := range a.Bloques {
		if a.Bloques[i].ID == id {
			return &a.Bloques[i]
		}
	}
	return nil
}

func (a *Agenda) encontrarTarea(id string) *Tarea {
	for i := range a.Tareas {
		if a.Tareas[i].ID == id {
			return &a.Tareas[i]
		}
	}
	return nil
}
