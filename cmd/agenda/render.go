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

// renderDia arma el itinerario de un día concreto.
func renderDia(agenda *Agenda, ahora time.Time) string {
	dia := diaAbreviado(ahora)
	fecha := fechaAbreviada(ahora)
	bloques := bloquesDeDia(agenda, dia)
	tareas := tareasDelDia(agenda, dia, fecha)
	var lineas []string
	titulo := "📅 " + diasSemana[indiceSemana(ahora)] + " " + strconv.Itoa(ahora.Day()) +
		" de " + nombresMes[int(ahora.Month())-1]
	lineas = append(lineas, titulo)
	if len(bloques) == 0 && len(tareas) == 0 {
		lineas = append(lineas, "Sin bloques ni tareas para hoy.")
		return strings.Join(lineas, "\n")
	}
	for _, b := range bloques {
		linea := "• " + b.Inicio + "–" + b.Fin + " " + b.Nombre
		if b.Fuente != "" {
			linea += " (" + b.Fuente + ")"
		}
		lineas = append(lineas, linea)
		for _, t := range tareas {
			if t.Bloque == b.ID {
				lineas = append(lineas, "   ☐ "+t.Texto)
			}
		}
	}
	var sueltas []string
	for _, t := range tareas {
		if t.Bloque == "" || agenda.encontrarBloque(t.Bloque) == nil {
			sueltas = append(sueltas, "☐ "+t.Texto)
		}
	}
	if len(sueltas) > 0 {
		lineas = append(lineas, "Pendientes sin bloque:")
		lineas = append(lineas, sueltas...)
	}
	return strings.Join(lineas, "\n")
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
		if len(bloques) == 0 {
			lineas = append(lineas, "• "+dia+marca+": —")
			continue
		}
		lineas = append(lineas, "• "+dia+marca+":")
		for _, b := range bloques {
			lineas = append(lineas, fmt.Sprintf("   %s–%s %s", b.Inicio, b.Fin, b.Nombre))
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
