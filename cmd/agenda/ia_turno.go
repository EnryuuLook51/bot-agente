package main

// Función 2: texto libre → el modelo recibe SOLO la agenda (horario + tareas
// en JSON), la fecha/hora actual (inyectada por código) y el mensaje; responde
// acciones JSON que el bot valida, aplica y guarda. Si algo es ambiguo, pide
// aclaración en vez de actuar. La consigna va como mensaje de sistema y el
// texto del usuario aparte, para que no pueda reescribir las reglas.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const consignaTurno = `Eres el gestor de la agenda del dueño de este bot por Telegram.
Fecha y hora actuales: %s.

CALENDARIO (fechas exactas; úsalo SIEMPRE para convertir días a fechas y
nunca calcules el día de la semana por tu cuenta):
%s

BLOQUES del horario semanal (id día inicio–fin nombre); se repiten cada semana:
%s

TAREAS (JSON):
%s

Tu respuesta es SIEMPRE un único objeto JSON válido, sin texto alrededor:
{"acciones":[...],"mensaje":"..."}
Acciones posibles:
 {"tipo":"agregar_tarea","texto":"...","fecha":"YYYY-MM-DD o vacío","hora":"HH:MM o vacío","bloque":"id o vacío"}
 {"tipo":"completar_tarea","id":"..."}
 {"tipo":"editar_tarea","id":"...","texto":"..."}
 {"tipo":"asignar_tarea","id":"...", y solo los campos a cambiar: "fecha":"YYYY-MM-DD","hora":"HH:MM","bloque":"id" ("" los borra)}
 {"tipo":"eliminar_tarea","id":"..."}
 {"tipo":"agregar_bloque","tipobloque":"clase|estudio|personal","nombre":"...","fuente":"HORARIO|PERSONAL","dia":"lun|mar|mié|jue|vie|sáb|dom|diario","inicio":"HH:MM","fin":"HH:MM"}
 {"tipo":"editar_bloque","id":"...", ...solo los campos a cambiar}
 {"tipo":"eliminar_bloque","id":"...","confirmado":true}
Reglas:
- Usa solo ids que existan en la agenda; los ids nuevos los pone el sistema.
  Nunca muestres ids (i1, b3...) al usuario: nombra la tarea o el bloque.
- Para agregar una tarea necesitas CUÁNDO: una fecha o un bloque. Si el
  usuario no lo dice, NO la agregues: pregunta en "mensaje" para cuándo es.
  Solo agrégala sin fecha si el usuario dice explícitamente "sin fecha".
- Si asignas un bloque semanal a una tarea, pon también la fecha concreta
  del próximo día de ese bloque.
- Los mensajes anteriores de esta conversación te dan contexto: úsalos para
  entender referencias como "esa", "lo" o una respuesta a tu pregunta.
- REGLA DE ORO: "mensaje" solo puede afirmar lo que hacen tus acciones. Si
  dices que programaste una fecha u hora, DEBE haber una acción con ese
  campo. Nunca digas que guardaste algo que no está en "acciones".
- Si el usuario te deja decidir ("tu criterio", "cuando veas"), decide tú:
  elige un hueco libre mirando los bloques de ese día (las clases y los
  traslados 🚍 están ocupados; evita 00:00–06:00) y aplícalo directamente
  con una acción; en "mensaje" di qué elegiste y por qué, en una frase.
- Si te preguntan si un horario está libre, revisa los bloques de ese día
  y responde concretamente qué hay antes y después.
- Nunca pidas formatos (YYYY-MM-DD, HH:MM): el usuario habla normal y tú
  conviertes.
- Convierte fechas relativas ("mañana", "el viernes") a YYYY-MM-DD usando la
  fecha actual.
- No agregues una tarea idéntica a una pendiente existente.
- Si el pedido es ambiguo (qué tarea, qué día, qué bloque), NO pongas
  acciones: usa "mensaje" para hacer UNA pregunta de aclaración.
- Si es solo una consulta ("qué tengo mañana"), no pongas acciones; responde
  en "mensaje" desde lo que ves en la agenda.
- En "mensaje" confirma en una frase corta lo que hiciste.
- "eliminar_bloque" solo con confirmado:true si el usuario lo confirmó
  explícitamente; si no, pregunta en "mensaje".`

// contextoTurno arma la consigna con la fecha inyectada + agenda compacta.
func contextoTurno(agenda *Agenda, ahora time.Time) string {
	fecha := ahora.Format("2006-01-02 15:04") + " (" + diasSemana[indiceSemana(ahora)] +
		", America/Lima)"
	tareas, err := json.Marshal(agenda.Tareas)
	if err != nil {
		tareas = []byte("[]")
	}
	return fmt.Sprintf(consignaTurno, fecha, calendario(ahora, 14), bloquesCompactos(agenda), string(tareas))
}

// calendario lista los próximos n días: "hoy sáb 2026-11-07", "dom 2026-11-08"...
func calendario(ahora time.Time, n int) string {
	var lineas []string
	for i := 0; i < n; i++ {
		d := ahora.AddDate(0, 0, i)
		linea := diasSemana[indiceSemana(d)] + " " + fechaAbreviada(d)
		switch i {
		case 0:
			linea += " (hoy)"
		case 1:
			linea += " (mañana)"
		}
		lineas = append(lineas, linea)
	}
	return strings.Join(lineas, "\n")
}

// bloquesCompactos resume el horario en una línea por bloque, en orden.
func bloquesCompactos(agenda *Agenda) string {
	var lineas []string
	for _, dia := range append(diasAbreviados[:], "diario") {
		for _, b := range bloquesDeDia(agenda, dia) { // ordenados por hora
			if b.Dia == dia {
				lineas = append(lineas, b.ID+" "+b.Dia+" "+b.Inicio+"–"+b.Fin+" "+b.Nombre)
			}
		}
	}
	if len(lineas) == 0 {
		return "(sin bloques)"
	}
	return strings.Join(lineas, "\n")
}

// ejecutarTurno corre la Función 2: agenda → modelo → acciones aplicadas sobre
// la agenda FRESCA (por si cambió mientras el modelo pensaba) → guardado.
// historial son los últimos intercambios (usuario / respuesta JSON del modelo).
// Devuelve la respuesta para el usuario, el resumen de cambios para el
// registro y la respuesta cruda del modelo (para el historial).
func ejecutarTurno(cliente *clienteIA, datos *almacen, historial []mensajeChat, mensaje string, ahora time.Time) (string, []string, string, error) {
	agenda, err := datos.cargar()
	if err != nil {
		return "", nil, "", err
	}
	respuesta, err := cliente.completar(contextoTurno(agenda, ahora), historial, mensaje)
	if err != nil {
		return "", nil, "", err
	}
	acciones, mensajeModelo, err := decodificarAcciones(respuesta)
	if err != nil {
		return "", nil, "", err
	}
	mensajeModelo = strings.TrimSpace(mensajeModelo)
	if len(acciones) == 0 {
		// Consulta o pregunta de aclaración: nada que guardar.
		if mensajeModelo != "" {
			return mensajeModelo, nil, respuesta, nil
		}
		return "", nil, "", BridgeError("el modelo respondió vacío; reintenta")
	}
	var resumen []string
	err = datos.modificar(func(fresca *Agenda) error {
		var aplicadas int
		resumen, aplicadas = aplicarAcciones(fresca, acciones)
		if aplicadas == 0 {
			return errSinCambios
		}
		return nil
	})
	if err != nil && err != errSinCambios {
		return "", resumen, "", err
	}
	partes := resumen
	if mensajeModelo != "" && err == nil {
		partes = append([]string{mensajeModelo}, resumen...)
	}
	return strings.Join(partes, "\n"), resumen, respuesta, nil
}

// errSinCambios indica que ninguna acción fue válida: no se escribe a disco.
var errSinCambios = BridgeError("ninguna acción fue válida")

// decodificarAcciones parsea {acciones, mensaje} tolerando markdown alrededor.
func decodificarAcciones(texto string) ([]map[string]any, string, error) {
	var envoltura struct {
		Acciones []map[string]any `json:"acciones"`
		Mensaje  string           `json:"mensaje"`
	}
	if err := json.Unmarshal([]byte(extraerJSON(texto)), &envoltura); err != nil {
		return nil, "", BridgeError("el modelo no devolvió un JSON válido; reintenta")
	}
	if len(envoltura.Acciones) > 20 {
		envoltura.Acciones = envoltura.Acciones[:20]
	}
	return envoltura.Acciones, envoltura.Mensaje, nil
}
