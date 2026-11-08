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
Agenda actual en JSON (bloques = horario fijo; tareas = pendientes y hechas):
%s

Tu respuesta es SIEMPRE un único objeto JSON válido, sin texto alrededor:
{"acciones":[...],"mensaje":"..."}
Acciones posibles:
 {"tipo":"agregar_tarea","texto":"...","fecha":"YYYY-MM-DD o vacío","bloque":"id o vacío"}
 {"tipo":"completar_tarea","id":"..."}
 {"tipo":"editar_tarea","id":"...","texto":"..."}
 {"tipo":"asignar_tarea","id":"...","fecha":"YYYY-MM-DD o vacío","bloque":"id o vacío"}
 {"tipo":"eliminar_tarea","id":"..."}
 {"tipo":"agregar_bloque","tipobloque":"clase|estudio|personal","nombre":"...","fuente":"HORARIO|PERSONAL","dia":"lun|mar|mié|jue|vie|sáb|dom|diario","inicio":"HH:MM","fin":"HH:MM"}
 {"tipo":"editar_bloque","id":"...", ...solo los campos a cambiar}
 {"tipo":"eliminar_bloque","id":"...","confirmado":true}
Reglas:
- Usa solo ids que existan en la agenda; los ids nuevos los pone el sistema.
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
	compacta, err := json.Marshal(agenda)
	if err != nil {
		compacta = []byte("{}")
	}
	return fmt.Sprintf(consignaTurno, fecha, string(compacta))
}

// ejecutarTurno corre la Función 2: agenda → modelo → acciones aplicadas sobre
// la agenda FRESCA (por si cambió mientras el modelo pensaba) → guardado.
// Devuelve la respuesta para el usuario y el resumen de cambios para el registro.
func ejecutarTurno(cliente *clienteIA, datos *almacen, mensaje string, ahora time.Time) (string, []string, error) {
	agenda, err := datos.cargar()
	if err != nil {
		return "", nil, err
	}
	respuesta, err := cliente.completar(contextoTurno(agenda, ahora), mensaje)
	if err != nil {
		return "", nil, err
	}
	acciones, mensajeModelo, err := decodificarAcciones(respuesta)
	if err != nil {
		return "", nil, err
	}
	mensajeModelo = strings.TrimSpace(mensajeModelo)
	if len(acciones) == 0 {
		// Consulta o pregunta de aclaración: nada que guardar.
		if mensajeModelo != "" {
			return mensajeModelo, nil, nil
		}
		return "", nil, BridgeError("el modelo respondió vacío; reintenta")
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
		return "", resumen, err
	}
	partes := resumen
	if mensajeModelo != "" && err == nil {
		partes = append([]string{mensajeModelo}, resumen...)
	}
	return strings.Join(partes, "\n"), resumen, nil
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
