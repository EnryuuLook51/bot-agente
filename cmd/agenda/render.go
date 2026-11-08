package main

// Renderizados de /hoy y /semana: texto plano de Telegram, sin IA.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// bloquesDeDia devuelve los bloques que valen para ese día, ordenados por hora.
func bloquesDeDia(agenda *Agenda, dia string) []Bloque {
	var lista []Bloque
	for _, b := range agenda.Bloques {
		if b.Dia == dia || b.Dia == "diario" {
			lista = append(lista, b)
		}
	}
	sort.Slice(lista, func(i, j int) bool {
		return horaEnMinutos(lista[i].Inicio) < horaEnMinutos(lista[j].Inicio)
	})
	return lista
}

// tareasDelDia une las tareas fechadas ese día y las enganchadas a bloques de ese día.
func tareasDelDia(agenda *Agenda, dia string, fecha string) []Tarea {
	var lista []Tarea
	for _, t := range agenda.Tareas {
		if t.Estado != "pendiente" {
			continue
		}
		if t.Fecha == fecha {
			lista = append(lista, t)
			continue
		}
		if t.Fecha == "" && t.Bloque != "" {
			bloque := agenda.encontrarBloque(t.Bloque)
			if bloque != nil && (bloque.Dia == dia || bloque.Dia == "diario") {
				lista = append(lista, t)
			}
		}
	}
	return lista
}

// lineaTarea formatea una tarea pendiente: "☐ 10:00 Leer libro".
func lineaTarea(t Tarea) string {
	if t.Hora != "" {
		return "☐ " + t.Hora + " " + t.Texto
	}
	return "☐ " + t.Texto
}

// ordenarPorHora deja primero las tareas con hora, en orden.
func ordenarPorHora(tareas []Tarea) {
	sort.SliceStable(tareas, func(i, j int) bool {
		if tareas[i].Hora == "" || tareas[j].Hora == "" {
			return tareas[i].Hora != "" && tareas[j].Hora == ""
		}
		return tareas[i].Hora < tareas[j].Hora
	})
}

// renderDia arma el itinerario de un día: bloques y tareas con hora en orden
// cronológico, las tareas del día sin hora y las pendientes sin fecha.
func renderDia(agenda *Agenda, ahora time.Time) string {
	dia := diaAbreviado(ahora)
	fecha := fechaAbreviada(ahora)
	bloques := bloquesDeDia(agenda, dia)
	tareas := tareasDelDia(agenda, dia, fecha)
	ordenarPorHora(tareas)
	lineas := []string{"📅 " + diasSemana[indiceSemana(ahora)] + " " + strconv.Itoa(ahora.Day()) +
		" de " + nombresMes[int(ahora.Month())-1]}
	if len(bloques) == 0 && len(tareas) == 0 {
		lineas = append(lineas, "Sin bloques ni tareas para hoy.")
	}

	// Línea de tiempo: bloques (con sus tareas) y tareas sueltas con hora.
	type evento struct {
		minuto int
		lineas []string
	}
	var eventos []evento
	for _, b := range bloques {
		ev := evento{horaEnMinutos(b.Inicio), []string{"• " + b.Inicio + "–" + b.Fin + " " + b.Nombre}}
		for _, t := range tareas {
			if t.Bloque == b.ID {
				ev.lineas = append(ev.lineas, "   "+lineaTarea(t))
			}
		}
		eventos = append(eventos, ev)
	}
	var sinHora []string
	for _, t := range tareas {
		if agenda.encontrarBloque(t.Bloque) != nil && bloqueEsDelDia(agenda, t.Bloque, dia) {
			continue // ya salió dentro de su bloque
		}
		if t.Hora != "" {
			eventos = append(eventos, evento{horaEnMinutos(t.Hora), []string{"• " + t.Hora + " ☐ " + t.Texto}})
		} else {
			sinHora = append(sinHora, lineaTarea(t))
		}
	}
	sort.SliceStable(eventos, func(i, j int) bool { return eventos[i].minuto < eventos[j].minuto })
	for _, ev := range eventos {
		lineas = append(lineas, ev.lineas...)
	}
	if len(sinHora) > 0 {
		lineas = append(lineas, "Tareas de hoy:")
		lineas = append(lineas, sinHora...)
	}
	var sinFecha []string
	for _, t := range agenda.Tareas {
		if t.Estado == "pendiente" && t.Fecha == "" && t.Bloque == "" {
			sinFecha = append(sinFecha, lineaTarea(t))
		}
	}
	if len(sinFecha) > 0 {
		lineas = append(lineas, "Sin fecha (dime para cuándo):")
		lineas = append(lineas, sinFecha...)
	}
	return strings.Join(lineas, "\n")
}

func bloqueEsDelDia(agenda *Agenda, id, dia string) bool {
	b := agenda.encontrarBloque(id)
	return b != nil && (b.Dia == dia || b.Dia == "diario")
}

// renderSemana arma la vista de lunes a domingo.
func renderSemana(agenda *Agenda, ahora time.Time) string {
	var lineas []string
	hoy := diaAbreviado(ahora)
	base := ahora.AddDate(0, 0, -(indiceSemana(ahora))) // lunes de esta semana
	lineas = append(lineas, "🗓 Semana "+fechaLegible(fechaAbreviada(base)))
	for i := 0; i < 7; i++ {
		dia := diasAbreviados[i]
		marca := ""
		if dia == hoy {
			marca = " ←"
		}
		bloques := bloquesDeDia(agenda, dia)
		fechaDia := base.AddDate(0, 0, i)
		var tareas []Tarea
		for _, t := range agenda.Tareas {
			if t.Estado == "pendiente" && t.Fecha == fechaAbreviada(fechaDia) {
				tareas = append(tareas, t)
			}
		}
		ordenarPorHora(tareas)
		if len(bloques) == 0 && len(tareas) == 0 {
			lineas = append(lineas, "• "+dia+marca+": —")
			continue
		}
		lineas = append(lineas, "• "+dia+marca+":")
		for _, b := range bloques {
			lineas = append(lineas, fmt.Sprintf("   %s–%s %s", b.Inicio, b.Fin, b.Nombre))
		}
		for _, t := range tareas {
			lineas = append(lineas, "   "+lineaTarea(t))
		}
	}
	pendientes := 0
	for _, t := range agenda.Tareas {
		if t.Estado == "pendiente" {
			pendientes++
		}
	}
	lineas = append(lineas, fmt.Sprintf("Tareas pendientes: %d (detalle en /hoy)", pendientes))
	return strings.Join(lineas, "\n")
}
