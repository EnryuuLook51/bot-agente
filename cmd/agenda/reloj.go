package main

// Reloj local del servidor: America/Lima, en español, sin depender del locale.
// Es la inyección de "fecha y hora actual" que pide la Función 2.

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	diasSemana = [...]string{"lunes", "martes", "miércoles", "jueves", "viernes", "sábado", "domingo"}
	nombresMes = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio",
		"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
)

var cargaZona sync.Once
var zonaLima *time.Location

// lima carga America/Lima del sistema; si no hay tzdata, usa el desfase fijo
// UTC-5 (en Perú no hay horario de verano).
func lima() *time.Location {
	cargaZona.Do(func() {
		z, err := time.LoadLocation("America/Lima")
		if err != nil {
			z = time.FixedZone("America/Lima", -5*3600)
		}
		zonaLima = z
	})
	return zonaLima
}

// diaAbreviado devuelve lun..dom del momento dado.
func diaAbreviado(ahora time.Time) string {
	// Go: domingo=0; aquí lunes=0.
	return diasAbreviados[(int(ahora.Weekday())+6)%7]
}

func fechaAbreviada(ahora time.Time) string {
	return ahora.Format("2006-01-02")
}

// fechaLegible convierte "2026-11-06" en "viernes 6 de noviembre".
func fechaLegible(fecha string) string {
	t, err := time.ParseInLocation("2006-01-02", fecha, lima())
	if err != nil {
		return fecha
	}
	return diasSemana[indiceSemana(t)] + " " + strconv.Itoa(t.Day()) + " de " +
		nombresMes[int(t.Month())-1]
}

// horaEnMinutos convierte "14:05" en minutos desde medianoche (-1 si no vale).
func horaEnMinutos(hora string) int {
	if !formatoHora.MatchString(hora) {
		return -1
	}
	seg := strings.SplitN(hora, ":", 2)
	h, err1 := strconv.Atoi(seg[0])
	m, err2 := strconv.Atoi(seg[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 {
		return -1
	}
	return h*60 + m
}

func indiceSemana(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}
