package main

// Tests de avisos, recordatorios, renders y troceo de mensajes.

import (
	"strings"
	"testing"
	"time"
)

func TestAviso15MinutosAntes(t *testing.T) {
	ahora := time.Date(2026, 11, 13, 20, 47, 0, 0, lima()) // viernes 20:47; b2 empieza 21:00
	agenda := agendaDePrueba()
	enviados := map[string]bool{}
	avisos := avisosPendientes(agenda, enviados, ahora)
	if len(avisos) != 1 || !strings.Contains(avisos[0].texto, "Estudio nocturno") || !strings.Contains(avisos[0].texto, "En 13 min") {
		t.Fatalf("aviso inesperado: %v", avisos)
	}
	enviados[avisos[0].llave] = true
	if avisos := avisosPendientes(agenda, enviados, ahora); len(avisos) != 0 {
		t.Fatalf("aviso repetido: %v", avisos)
	}
	if avisos := avisosPendientes(agenda, map[string]bool{}, ahora.Add(16*time.Minute)); len(avisos) != 0 {
		t.Fatalf("aviso fuera de ventana: %v", avisos)
	}
	agenda.Config.AvisoMinutos = -1
	if avisos := avisosPendientes(agenda, map[string]bool{}, ahora); len(avisos) != 0 {
		t.Fatalf("avisos apagados pero salió: %v", avisos)
	}
}

func TestRecordatorioDosVecesAlDia(t *testing.T) {
	agenda := agendaDePrueba()
	enviados := map[string]bool{}
	manana := time.Date(2026, 11, 13, 6, 31, 0, 0, lima())
	hora, llave := recordatorioQueToca(agenda, enviados, manana)
	if hora != "06:30" || llave == "" {
		t.Fatalf("no tocó el recordatorio de la mañana: %q", hora)
	}
	enviados[llave] = true
	if hora, _ := recordatorioQueToca(agenda, enviados, manana); hora != "" {
		t.Fatal("recordatorio repetido")
	}
	if hora, _ := recordatorioQueToca(agenda, enviados, manana.Add(2*time.Hour)); hora != "" {
		t.Fatal("recordatorio fuera de hora")
	}
	noche := time.Date(2026, 11, 13, 21, 0, 0, 0, lima())
	if hora, _ := recordatorioQueToca(agenda, enviados, noche); hora != "21:00" {
		t.Fatalf("no tocó el de la noche: %q", hora)
	}
}

func TestRenderRecordatorioConBotones(t *testing.T) {
	agenda := agendaDePrueba()
	agenda.Tareas = append(agenda.Tareas, Tarea{ID: "i9", Texto: "Entregar informe", Estado: "pendiente", Fecha: "2026-11-10", Origen: "usuario"})
	noche := time.Date(2026, 11, 13, 21, 0, 0, 0, lima()) // viernes
	texto, botones := renderRecordatorio(agenda, noche)
	if !strings.Contains(texto, "Leer capítulo 3") || !strings.Contains(texto, "Atrasadas") || !strings.Contains(texto, "Entregar informe") {
		t.Fatalf("recordatorio incompleto: %s", texto)
	}
	if len(botones) != 2 || botones[0][1] != "hecho:i1" || botones[1][1] != "hecho:i9" {
		t.Fatalf("botones inesperados: %v", botones)
	}
	manana := time.Date(2026, 11, 13, 6, 30, 0, 0, lima())
	if texto, _ := renderRecordatorio(agenda, manana); !strings.Contains(texto, "Buenos días") || !strings.Contains(texto, "viernes") {
		t.Fatalf("recordatorio de mañana sin itinerario: %s", texto)
	}
}

func TestRenderHoyViernes(t *testing.T) {
	ahora := time.Date(2026, 11, 13, 19, 0, 0, 0, lima())
	salida := renderDia(agendaDePrueba(), ahora)
	for _, esperado := range []string{"viernes", "Estudio nocturno", "Leer capítulo 3"} {
		if !strings.Contains(salida, esperado) {
			t.Fatalf("falta %q en: %s", esperado, salida)
		}
	}
}

func TestRenderSemanaMarcaHoy(t *testing.T) {
	ahora := time.Date(2026, 11, 13, 19, 0, 0, 0, lima())
	salida := renderSemana(agendaDePrueba(), ahora)
	if !strings.Contains(salida, "vie ←") {
		t.Fatalf("semana sin marca del día: %s", salida)
	}
}

func TestPartirMensajesLargos(t *testing.T) {
	linea := strings.Repeat("á", 50) + "\n"
	trozos := partir(strings.Repeat(linea, 200), 1000)
	if len(trozos) < 10 {
		t.Fatalf("no partió: %d trozos", len(trozos))
	}
	for _, tr := range trozos {
		if len([]rune(tr)) > 1000 {
			t.Fatalf("trozo demasiado largo: %d", len([]rune(tr)))
		}
	}
	if got := partir("corto", 1000); len(got) != 1 || got[0] != "corto" {
		t.Fatalf("texto corto alterado: %v", got)
	}
}

func TestHoyMuestraTareasSinFecha(t *testing.T) {
	agenda := agendaDePrueba()
	agenda.Tareas = append(agenda.Tareas, Tarea{ID: "i8", Texto: "Leer libro de patrones", Estado: "pendiente", Origen: "ia"})
	salida := renderDia(agenda, time.Date(2026, 11, 14, 19, 0, 0, 0, lima())) // sábado sin bloques propios
	if !strings.Contains(salida, "Sin fecha") || !strings.Contains(salida, "Leer libro de patrones") {
		t.Fatalf("la tarea sin fecha no aparece en /hoy: %s", salida)
	}
}

func TestConfirmacionIncluyeFecha(t *testing.T) {
	agenda := agendaDePrueba()
	resumen, _ := aplicarAcciones(agenda, []map[string]any{
		{"tipo": "agregar_tarea", "texto": "Con fecha", "fecha": "2026-11-09"},
		{"tipo": "agregar_tarea", "texto": "Suelta"},
	})
	if !strings.Contains(resumen[0], "lunes 9 de noviembre") || !strings.Contains(resumen[1], "sin fecha") {
		t.Fatalf("confirmación sin fecha legible: %v", resumen)
	}
}

func TestTareaConHora(t *testing.T) {
	agenda := agendaDePrueba()
	_, n := aplicarAcciones(agenda, []map[string]any{
		{"tipo": "asignar_tarea", "id": "i1", "hora": "9:30"}, // conserva la fecha 2026-11-13
	})
	if n != 1 || agenda.encontrarTarea("i1").Fecha != "2026-11-13" || agenda.encontrarTarea("i1").Hora != "09:30" {
		t.Fatalf("asignar solo la hora falló: %+v", agenda.encontrarTarea("i1"))
	}
	if _, n := aplicarAcciones(agenda, []map[string]any{{"tipo": "agregar_tarea", "texto": "x", "hora": "10:00"}}); n != 0 {
		t.Fatal("una hora sin fecha debió rechazarse")
	}
	viernes := time.Date(2026, 11, 13, 9, 15, 0, 0, lima())
	avisos := avisosPendientes(agenda, map[string]bool{}, viernes)
	if len(avisos) != 1 || !strings.Contains(avisos[0].texto, "Leer capítulo 3") {
		t.Fatalf("sin aviso de tarea con hora: %v", avisos)
	}
	salida := renderDia(agenda, viernes)
	iTarea, iBloque := strings.Index(salida, "09:30 ☐ Leer capítulo 3"), strings.Index(salida, "21:00–22:30")
	if iTarea < 0 || iBloque < 0 || iTarea > iBloque {
		t.Fatalf("la tarea con hora no quedó en orden cronológico:\n%s", salida)
	}
	if !strings.Contains(renderSemana(agenda, viernes), "☐ 09:30 Leer capítulo 3") {
		t.Fatalf("/semana no muestra la tarea:\n%s", renderSemana(agenda, viernes))
	}
}
