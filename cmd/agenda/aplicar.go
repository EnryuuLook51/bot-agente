package main

// Aplicación de las acciones que devuelve el modelo (texto libre) sobre la
// Agenda en memoria. Cada acción se aplica sobre una copia y se valida: si
// deja la agenda inválida se descarta y se reporta, sin afectar a las demás.

import (
	"fmt"
	"strconv"
	"strings"
)

// siguienteID genera el próximo id libre con el prefijo dado ("i" o "b").
func siguienteID(agenda *Agenda, prefijo string) string {
	usados := map[string]bool{}
	for _, b := range agenda.Bloques {
		usados[b.ID] = true
	}
	for _, t := range agenda.Tareas {
		usados[t.ID] = true
	}
	n := 1
	for usados[prefijo+strconv.Itoa(n)] {
		n++
	}
	return prefijo + strconv.Itoa(n)
}

var accionesConocidas = map[string]func(*Agenda, map[string]any) (string, error){
	"agregar_tarea":   agregarTarea,
	"completar_tarea": completarTarea,
	"editar_tarea":    editarTarea,
	"asignar_tarea":   asignarTarea,
	"eliminar_tarea":  eliminarTarea,
	"agregar_bloque":  agregarBloque,
	"editar_bloque":   editarBloque,
	"eliminar_bloque": eliminarBloque,
}

// aplicarAcciones ejecuta la lista y devuelve el resumen legible ("✔ ..." por
// cada cambio, "! ..." por cada acción descartada) y cuántas aplicó.
func aplicarAcciones(agenda *Agenda, acciones []map[string]any) ([]string, int) {
	var resumen []string
	aplicadas := 0
	for _, accion := range acciones {
		tipo, _ := accion["tipo"].(string)
		funcion, existe := accionesConocidas[tipo]
		if !existe {
			resumen = append(resumen, "! acción desconocida: "+tipo)
			continue
		}
		antes := agenda.copiar()
		linea, err := funcion(agenda, accion)
		if err == nil {
			err = validarAgenda(agenda)
		}
		if err != nil {
			*agenda = *antes
			resumen = append(resumen, "! "+err.Error())
			continue
		}
		aplicadas++
		resumen = append(resumen, "✔ "+linea)
	}
	return resumen, aplicadas
}

func cadenaDe(accion map[string]any, campo string) string {
	valor, _ := accion[campo].(string)
	return strings.TrimSpace(valor)
}

func agregarTarea(agenda *Agenda, accion map[string]any) (string, error) {
	texto := cadenaDe(accion, "texto")
	if texto == "" {
		return "", fmt.Errorf("agregar_tarea sin texto")
	}
	agenda.Tareas = append(agenda.Tareas, Tarea{
		ID:     siguienteID(agenda, "i"),
		Texto:  texto,
		Estado: "pendiente",
		Fecha:  cadenaDe(accion, "fecha"),
		Bloque: cadenaDe(accion, "bloque"),
		Origen: "ia",
	})
	return "nueva tarea: " + texto + " (" + cuandoTarea(agenda, &agenda.Tareas[len(agenda.Tareas)-1]) + ")", nil
}

func completarTarea(agenda *Agenda, accion map[string]any) (string, error) {
	tarea := agenda.encontrarTarea(cadenaDe(accion, "id"))
	if tarea == nil || tarea.Estado == "completada" {
		return "", fmt.Errorf("no encontré esa tarea pendiente para completar")
	}
	tarea.Estado = "completada"
	return "completada: " + tarea.Texto, nil
}

func editarTarea(agenda *Agenda, accion map[string]any) (string, error) {
	tarea := agenda.encontrarTarea(cadenaDe(accion, "id"))
	if tarea == nil {
		return "", fmt.Errorf("no encontré esa tarea para editar")
	}
	if texto := cadenaDe(accion, "texto"); texto != "" {
		tarea.Texto = texto
	}
	return "editada: " + tarea.Texto, nil
}

func asignarTarea(agenda *Agenda, accion map[string]any) (string, error) {
	tarea := agenda.encontrarTarea(cadenaDe(accion, "id"))
	if tarea == nil {
		return "", fmt.Errorf("no encontré esa tarea para mover")
	}
	tarea.Fecha = cadenaDe(accion, "fecha") // vacío desasigna el día
	tarea.Bloque = cadenaDe(accion, "bloque")
	lugar, dia := "sin bloque", "sin fecha"
	if b := agenda.encontrarBloque(tarea.Bloque); b != nil {
		lugar = b.Nombre
	}
	if tarea.Fecha != "" {
		dia = fechaLegible(tarea.Fecha)
	}
	return "movida a " + lugar + " (" + dia + "): " + tarea.Texto, nil
}

func eliminarTarea(agenda *Agenda, accion map[string]any) (string, error) {
	id := cadenaDe(accion, "id")
	for i, t := range agenda.Tareas {
		if t.ID == id {
			agenda.Tareas = append(agenda.Tareas[:i:i], agenda.Tareas[i+1:]...)
			return "eliminada: " + t.Texto, nil
		}
	}
	return "", fmt.Errorf("no encontré esa tarea para eliminar")
}

func agregarBloque(agenda *Agenda, accion map[string]any) (string, error) {
	bloque := Bloque{
		ID:     siguienteID(agenda, "b"),
		Tipo:   cadenaDe(accion, "tipobloque"),
		Nombre: cadenaDe(accion, "nombre"),
		Fuente: cadenaDe(accion, "fuente"),
		Dia:    cadenaDe(accion, "dia"),
		Inicio: normalizarHora(cadenaDe(accion, "inicio")),
		Fin:    normalizarHora(cadenaDe(accion, "fin")),
	}
	if bloque.Dia == "" {
		bloque.Dia = "diario"
	}
	if bloque.Fuente == "" {
		bloque.Fuente = "PERSONAL"
	}
	agenda.Bloques = append(agenda.Bloques, bloque)
	return fmt.Sprintf("bloque nuevo: %s %s–%s %s", bloque.Dia, bloque.Inicio, bloque.Fin, bloque.Nombre), nil
}

func editarBloque(agenda *Agenda, accion map[string]any) (string, error) {
	bloque := agenda.encontrarBloque(cadenaDe(accion, "id"))
	if bloque == nil {
		return "", fmt.Errorf("no encontré ese bloque para editar")
	}
	campos := map[string]*string{
		"tipobloque": &bloque.Tipo,
		"nombre":     &bloque.Nombre,
		"fuente":     &bloque.Fuente,
		"dia":        &bloque.Dia,
		"inicio":     &bloque.Inicio,
		"fin":        &bloque.Fin,
	}
	for campo, objetivo := range campos {
		if valor := cadenaDe(accion, campo); valor != "" {
			if campo == "inicio" || campo == "fin" {
				valor = normalizarHora(valor)
			}
			*objetivo = valor
		}
	}
	return fmt.Sprintf("bloque actualizado: %s (%s %s–%s)", bloque.Nombre, bloque.Dia, bloque.Inicio, bloque.Fin), nil
}

func eliminarBloque(agenda *Agenda, accion map[string]any) (string, error) {
	id := cadenaDe(accion, "id")
	bloque := agenda.encontrarBloque(id)
	if bloque == nil {
		return "", fmt.Errorf("no encontré ese bloque para eliminar")
	}
	if !confirmado(accion) {
		return "", fmt.Errorf("borrar %q pide confirmación; dime «sí, elimina el bloque %s»", bloque.Nombre, bloque.Nombre)
	}
	nombre := bloque.Nombre
	quedan := agenda.Bloques[:0:0]
	for _, b := range agenda.Bloques {
		if b.ID != id {
			quedan = append(quedan, b)
		}
	}
	agenda.Bloques = quedan
	for i := range agenda.Tareas {
		if agenda.Tareas[i].Bloque == id {
			agenda.Tareas[i].Bloque = ""
		}
	}
	return "bloque eliminado: " + nombre, nil
}

// cuandoTarea describe la fecha y el bloque de una tarea en lenguaje natural.
func cuandoTarea(agenda *Agenda, t *Tarea) string {
	var partes []string
	if t.Fecha != "" {
		partes = append(partes, fechaLegible(t.Fecha))
	}
	if b := agenda.encontrarBloque(t.Bloque); b != nil {
		partes = append(partes, b.Nombre)
	}
	if len(partes) == 0 {
		return "sin fecha"
	}
	return strings.Join(partes, ", ")
}

// confirmado admite la bandera como bool JSON (true) o string ("true").
func confirmado(accion map[string]any) bool {
	if valor, ok := accion["confirmado"].(bool); ok {
		return valor
	}
	return cadenaDe(accion, "confirmado") == "true"
}

// marcarCompletada es la versión sin IA (botón ✅ de los recordatorios).
func marcarCompletada(agenda *Agenda, id string) (string, error) {
	return completarTarea(agenda, map[string]any{"id": id})
}
