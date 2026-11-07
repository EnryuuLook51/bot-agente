package main

// Notificaciones sin IA, revisadas en cada tick:
//   - aviso N min antes de cada bloque de hoy (Config.AvisoMinutos);
//   - recordatorios de pendientes a las horas de Config.Recordatorios
//     (06:30 y 21:00 por defecto), con un botón ✅ por tarea.
// Cada aviso sale una sola vez por día (marcas en estadoBot.AvisosEnviados).

import (
	"strconv"
	"time"
)

// maxBotones limita los botones del recordatorio para no saturar el mensaje.
const maxBotones = 12

// avisoBloque es un aviso de bloque listo para enviar.
type avisoBloque struct {
	llave string
	texto string
}

// avisosPendientes devuelve los avisos de bloque que tocan ahora (sin marcar).
func avisosPendientes(agenda *Agenda, enviados map[string]bool, ahora time.Time) []avisoBloque {
	ventana := agenda.Config.AvisoMinutos
	if ventana < 0 {
		return nil // avisos apagados
	}
	if ventana == 0 {
		ventana = 15
	}
	fecha := fechaAbreviada(ahora)
	minutoActual := ahora.Hour()*60 + ahora.Minute()
	var avisos []avisoBloque
	for _, b := range bloquesDeDia(agenda, diaAbreviado(ahora)) {
		inicio := horaEnMinutos(b.Inicio)
		llave := llaveAviso(b.ID, fecha)
		if inicio < 0 || enviados[llave] {
			continue
		}
		if inicio-ventana <= minutoActual && minutoActual < inicio {
			avisos = append(avisos, avisoBloque{llave: llave,
				texto: "⏰ En " + strconv.Itoa(inicio-minutoActual) + " min: " + b.Nombre + " (" + b.Inicio + "–" + b.Fin + ")"})
		}
	}
	return avisos
}

// recordatorioQueToca devuelve la hora de recordatorio que corresponde a este
// minuto (y su llave), o "" si no toca ninguno o ya se mandó.
func recordatorioQueToca(agenda *Agenda, enviados map[string]bool, ahora time.Time) (string, string) {
	minutoActual := ahora.Hour()*60 + ahora.Minute()
	for _, hora := range agenda.Config.Recordatorios {
		h := horaEnMinutos(hora)
		// Ventana de 5 min por si el bot reinició justo a esa hora.
		if h < 0 || minutoActual < h || minutoActual >= h+5 {
			continue
		}
		llave := llaveAviso("rec"+hora, fechaAbreviada(ahora))
		if !enviados[llave] {
			return hora, llave
		}
	}
	return "", ""
}

// tareasParaRecordar une las pendientes de hoy y las atrasadas (fecha pasada).
func tareasParaRecordar(agenda *Agenda, ahora time.Time) (hoy, atrasadas []Tarea) {
	fecha := fechaAbreviada(ahora)
	hoy = tareasDelDia(agenda, diaAbreviado(ahora), fecha)
	for _, t := range agenda.Tareas {
		if t.Estado == "pendiente" && t.Fecha != "" && t.Fecha < fecha {
			atrasadas = append(atrasadas, t)
		}
	}
	return hoy, atrasadas
}

// renderRecordatorio arma el texto y los botones (un ✅ por tarea).
// Mañana: itinerario del día. Noche: qué quedó pendiente para marcarlo.
func renderRecordatorio(agenda *Agenda, ahora time.Time) (string, [][2]string) {
	hoy, atrasadas := tareasParaRecordar(agenda, ahora)
	var texto string
	if ahora.Hour() < 12 {
		texto = "☀️ Buenos días.\n" + renderDia(agenda, ahora)
	} else {
		texto = "🌙 Cierre del día."
		if len(hoy) == 0 {
			texto += "\nNo quedan tareas de hoy."
		} else {
			texto += "\nPendientes de hoy:"
			for _, t := range hoy {
				texto += "\n☐ " + t.Texto
			}
		}
	}
	if len(atrasadas) > 0 {
		texto += "\n\n⚠️ Atrasadas:"
		for _, t := range atrasadas {
			texto += "\n☐ " + t.Texto + " (" + fechaLegible(t.Fecha) + ")"
		}
	}
	var botones [][2]string
	for _, t := range append(hoy, atrasadas...) {
		if len(botones) == maxBotones {
			break
		}
		botones = append(botones, [2]string{"✅ " + recortar(t.Texto, 40), "hecho:" + t.ID})
	}
	if len(botones) > 0 {
		texto += "\n\nToca lo que ya hiciste:"
	}
	return texto, botones
}

// recortar acorta un texto a n runas con "…".
func recortar(texto string, n int) string {
	runas := []rune(texto)
	if len(runas) <= n {
		return texto
	}
	return string(runas[:n-1]) + "…"
}
