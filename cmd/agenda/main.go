package main

// Bot de agenda: un solo binario que se conecta a Telegram y corre tres hilos:
// el loop de updates (comandos, botones, texto libre), el ticker (avisos y
// recordatorios) y el worker que ejecuta los turnos de IA (Groq) de a uno.
//
// Datos (DATOS_DIR): horario.json, tareas.json, registro.jsonl, estado.json,
// respaldos/. La bóveda (VAULT) solo se lee, para /sugerir.

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const textoAyuda = `Gestiono tu horario y tus tareas.

Sin IA (instantáneo):
 /hoy — itinerario de hoy con tareas
 /semana — horario de la semana
 /ayuda — esta lista
 Recordatorios a las 06:30 y 21:00 con tus pendientes:
  toca ✅ en lo que ya hiciste
 Aviso 15 min antes de cada bloque

Con IA:
 Texto libre — «agrega tarea resolver guía 1 el viernes»,
  «ya terminé lo de laboratorio», «los sábados gimnasio 9:00–11:00»
  y edito tareas u horario (te pregunto si algo no queda claro)
 /sugerir — leo tus notas importantes y propongo tareas
  (acepta o rechaza con los botones)`

type configuracionBot struct {
	token     string
	owner     int64
	vault     string
	dirDatos  string
	claveIA   string
	modelo    string
	comprobar bool
}

func principal() int {
	conf := configuracionBot{}
	flag.StringVar(&conf.token, "token", os.Getenv("TELEGRAM_BOT_TOKEN"), "token del bot (o env TELEGRAM_BOT_TOKEN)")
	flag.StringVar(&conf.vault, "vault", os.Getenv("VAULT"), "bóveda, solo lectura (o env VAULT)")
	flag.StringVar(&conf.dirDatos, "datos", envOR("DATOS_DIR", "/data"), "directorio de datos (o env DATOS_DIR)")
	flag.Int64Var(&conf.owner, "owner", 0, "id de Telegram del dueño (o env TELEGRAM_OWNER_ID)")
	flag.StringVar(&conf.modelo, "modelo", envOR("MODELO", "openai/gpt-oss-120b"), "modelo de Groq")
	flag.BoolVar(&conf.comprobar, "check", false, "valida configuración y datos, sin arrancar")
	flag.Parse()
	if conf.owner == 0 {
		if valor, err := strconv.ParseInt(os.Getenv("TELEGRAM_OWNER_ID"), 10, 64); err == nil {
			conf.owner = valor
		}
	}
	conf.claveIA = os.Getenv("GROQ_API_KEY") // solo por entorno: nunca en la línea de comandos
	if flag.Arg(0) == "check" {
		conf.comprobar = true // `agenda check` == --check (para compose run)
	}
	datos := nuevoAlmacen(conf.dirDatos)
	if conf.comprobar {
		return comprobar(conf, datos)
	}
	if conf.token == "" || conf.owner == 0 || conf.vault == "" {
		fmt.Fprintln(os.Stderr, "faltan -token/-owner/-vault (o sus variables de entorno)")
		return 2
	}
	if err := os.MkdirAll(conf.dirDatos, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo crear el directorio de datos:", err)
		return 1
	}

	reg := nuevoRegistro(conf.dirDatos)
	relay, err := newTelegram(conf.token, conf.owner, reg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := relay.registrarComandos(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	bot := &botAgenda{
		conf:     conf,
		datos:    datos,
		relay:    relay,
		registro: reg,
		cliente:  nuevoClienteIA(conf.claveIA, conf.modelo),
		estado:   cargarEstado(conf.dirDatos),
		trabajo:  make(chan func(), 8),
	}
	go bot.worker()

	detener := make(chan os.Signal, 1)
	signal.Notify(detener, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-detener
		os.Exit(0)
	}()

	go func() {
		for range time.Tick(30 * time.Second) {
			bot.tick(time.Now().In(lima()))
		}
	}()

	if err := relay.send("Agenda lista. /ayuda para las opciones."); err != nil {
		fmt.Fprintln(os.Stderr, "no pude avisar al dueño:", err)
	}

	for {
		var offset int
		bot.estado.leer(func(e *estadoBot) { offset = e.Offset })
		updates, err := relay.updates(offset)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range updates {
			bot.estado.cambiar(func(e *estadoBot) { e.Offset = u.UpdateID + 1 })
			bot.procesar(u)
		}
	}
}

type botAgenda struct {
	conf      configuracionBot
	datos     *almacen
	relay     *Relay
	registro  *registro
	cliente   *clienteIA
	estado    *estadoBot
	trabajo   chan func()   // turnos IA en serie
	historial []intercambio // solo lo toca el worker
}

// worker ejecuta los turnos IA de a uno (respeta el límite por minuto de Groq).
func (b *botAgenda) worker() {
	for tarea := range b.trabajo {
		tarea()
	}
}

// encolarIA manda un turno de IA al worker sin bloquear el loop de Telegram.
func (b *botAgenda) encolarIA(tarea func()) {
	if b.conf.claveIA == "" {
		_ = b.relay.send("La IA no está configurada (falta GROQ_API_KEY). /hoy y /semana funcionan igual.")
		return
	}
	select {
	case b.trabajo <- tarea:
		b.relay.typing()
	default:
		_ = b.relay.send("Tengo varios pedidos en cola; espera a que termine y reintenta.")
	}
}

// procesar despacha un update de Telegram.
func (b *botAgenda) procesar(u tgbotapi.Update) {
	if u.CallbackQuery != nil {
		b.callback(u.CallbackQuery)
		return
	}
	if u.Message == nil || u.Message.Chat == nil || u.Message.Chat.ID != b.conf.owner {
		return // otros chats: ignorados
	}
	texto := strings.TrimSpace(u.Message.Text)
	if texto == "" {
		return
	}
	b.registro.anotar("entrada", texto)
	comando := strings.ToLower(strings.Fields(texto)[0])
	if i := strings.Index(comando, "@"); i > 0 {
		comando = comando[:i] // /hoy@MiBot
	}
	switch comando {
	case "/start", "/ayuda", "/help":
		_ = b.relay.send(textoAyuda)
	case "/hoy":
		b.responderRender(renderDia)
	case "/semana":
		b.responderRender(renderSemana)
	case "/sugerir":
		b.encolarIA(b.turnoSugerir)
	default:
		if strings.HasPrefix(comando, "/") {
			_ = b.relay.send("No conozco ese comando. /ayuda para ver las opciones.")
			return
		}
		b.encolarIA(func() { b.turnoLibre(texto) })
	}
}

// responderRender manda un render sin IA con los datos frescos de disco.
func (b *botAgenda) responderRender(arma func(*Agenda, time.Time) string) {
	agenda, err := b.datos.cargar()
	if err != nil {
		_ = b.relay.send(err.Error())
		return
	}
	_ = b.relay.send(arma(agenda, time.Now().In(lima())))
}

// turnoLibre corre la Función 2 (texto libre → cambios) y responde.
func (b *botAgenda) turnoLibre(mensaje string) {
	ahora := time.Now().In(lima())
	respuesta, cambios, crudo, err := ejecutarTurno(b.cliente, b.datos, b.historialReciente(ahora), mensaje, ahora)
	for _, c := range cambios {
		if strings.HasPrefix(c, "✔") {
			b.registro.anotar("cambio", c)
		}
	}
	if err != nil {
		b.registro.anotar("error", err.Error())
		_ = b.relay.send(err.Error())
		return
	}
	b.recordar(ahora, mensaje, crudo)
	_ = b.relay.send(respuesta)
}

// Historial corto de la conversación con la IA: los últimos intercambios de
// los últimos 30 minutos, solo en memoria. Lo usa solo el worker (un hilo).
const (
	maxIntercambios   = 4
	vigenciaHistorial = 30 * time.Minute
)

type intercambio struct {
	cuando    time.Time
	usuario   string
	respuesta string // JSON crudo del modelo
}

func (b *botAgenda) recordar(ahora time.Time, usuario, respuesta string) {
	if respuesta == "" {
		return
	}
	b.historial = append(b.historial, intercambio{ahora, usuario, respuesta})
	if len(b.historial) > maxIntercambios {
		b.historial = b.historial[len(b.historial)-maxIntercambios:]
	}
}

func (b *botAgenda) historialReciente(ahora time.Time) []mensajeChat {
	var mensajes []mensajeChat
	for _, i := range b.historial {
		if ahora.Sub(i.cuando) <= vigenciaHistorial {
			mensajes = append(mensajes, mensajeChat{"user", i.usuario}, mensajeChat{"assistant", i.respuesta})
		}
	}
	return mensajes
}

// turnoSugerir corre la Función 1 (/sugerir).
func (b *botAgenda) turnoSugerir() {
	agenda, err := b.datos.cargar()
	if err == nil {
		err = ejecutarSugerir(b.cliente, b.conf.vault, b.relay, agenda, b.estado, time.Now().In(lima()))
	}
	if err != nil {
		b.registro.anotar("error", err.Error())
		_ = b.relay.send(err.Error())
	}
}

// callback responde los botones: hecho:<tarea>, aceptar:<clave>, rechazar:<clave>.
func (b *botAgenda) callback(q *tgbotapi.CallbackQuery) {
	if q.From == nil || q.From.ID != b.conf.owner || q.Message == nil {
		return
	}
	decision, clave, ok := strings.Cut(q.Data, ":")
	if !ok {
		b.relay.responderCallback(q.ID, "")
		return
	}
	b.registro.anotar("entrada", "botón "+q.Data)
	switch decision {
	case "hecho":
		b.marcarHecha(q, clave)
	case "aceptar", "rechazar":
		b.decidirSugerencia(q, decision, clave)
	default:
		b.relay.responderCallback(q.ID, "")
	}
}

// marcarHecha completa una tarea desde el botón ✅ (sin IA) y quita ese botón.
func (b *botAgenda) marcarHecha(q *tgbotapi.CallbackQuery, id string) {
	var linea string
	err := b.datos.modificar(func(a *Agenda) error {
		var err error
		linea, err = marcarCompletada(a, id)
		return err
	})
	if err != nil {
		b.relay.responderCallback(q.ID, "Ya estaba hecha o no existe")
	} else {
		b.registro.anotar("cambio", "✔ "+linea)
		b.relay.responderCallback(q.ID, "✔ "+linea)
	}
	var quedan [][][2]string
	for _, fila := range botonesDe(q.Message) {
		if len(fila) == 1 && fila[0][1] == "hecho:"+id {
			continue
		}
		quedan = append(quedan, fila)
	}
	b.relay.cambiarBotones(q.Message.MessageID, quedan)
}

// decidirSugerencia acepta o rechaza una propuesta de /sugerir.
func (b *botAgenda) decidirSugerencia(q *tgbotapi.CallbackQuery, decision, clave string) {
	var s sugerencia
	var existe bool
	b.estado.cambiar(func(e *estadoBot) {
		s, existe = e.Sugerencias[clave]
		delete(e.Sugerencias, clave)
	})
	b.relay.cambiarBotones(q.Message.MessageID, nil)
	if !existe {
		b.relay.responderCallback(q.ID, "Esa sugerencia ya no está pendiente")
		return
	}
	if decision == "rechazar" {
		b.relay.responderCallback(q.ID, "Rechazada")
		return
	}
	err := b.datos.modificar(func(a *Agenda) error {
		tarea := Tarea{ID: siguienteID(a, "i"), Texto: s.Texto, Estado: "pendiente", Bloque: s.Bloque, Origen: "sugerencia"}
		if a.encontrarBloque(tarea.Bloque) == nil {
			// Sin bloque (o el bloque desapareció): queda para hoy.
			tarea.Bloque = ""
			tarea.Fecha = fechaAbreviada(time.Now().In(lima()))
		}
		a.Tareas = append(a.Tareas, tarea)
		return nil
	})
	if err != nil {
		b.relay.responderCallback(q.ID, "")
		_ = b.relay.send(err.Error())
		return
	}
	b.registro.anotar("cambio", "✔ nueva tarea (sugerencia): "+s.Texto)
	b.relay.responderCallback(q.ID, "Agregada")
	_ = b.relay.send("Agregada: " + s.Texto)
}

// tick corre cada 30 s: avisos de bloques y recordatorios de pendientes.
func (b *botAgenda) tick(ahora time.Time) {
	agenda, err := b.datos.cargar()
	if err != nil {
		return // datos rotos: se reportará al pedir /hoy
	}
	var avisos []avisoBloque
	var horaRecordatorio string
	b.estado.cambiar(func(e *estadoBot) {
		e.podarAvisos(fechaAbreviada(ahora))
		avisos = avisosPendientes(agenda, e.AvisosEnviados, ahora)
		for _, a := range avisos {
			e.AvisosEnviados[a.llave] = true
		}
		var llave string
		horaRecordatorio, llave = recordatorioQueToca(agenda, e.AvisosEnviados, ahora)
		if llave != "" {
			e.AvisosEnviados[llave] = true
		}
	})
	for _, a := range avisos {
		_ = b.relay.send(a.texto)
	}
	if horaRecordatorio != "" {
		texto, botones := renderRecordatorio(agenda, ahora)
		var filas [][][2]string
		for _, par := range botones {
			filas = append(filas, [][2]string{par})
		}
		if _, err := b.relay.sendConBotones(texto, filas); err != nil {
			fmt.Fprintln(os.Stderr, "recordatorio:", err)
		}
	}
}

// comprobar valida la configuración y los datos sin arrancar nada (para
// despliegues: docker compose run --rm agenda check).
func comprobar(conf configuracionBot, datos *almacen) int {
	fallos := 0
	revisar := func(nombre string, ok bool, detalle string) {
		estado := "ok"
		if !ok {
			estado = detalle
			fallos++
		}
		fmt.Printf("  %-8s %s\n", nombre, estado)
	}
	revisar("token", conf.token != "", "vacío")
	revisar("owner", conf.owner > 0, "vacío")
	revisar("groq", conf.claveIA != "", "falta GROQ_API_KEY (la IA quedará apagada)")
	revisar("vault", directorioExiste(conf.vault), "no existe")
	revisar("datos", os.MkdirAll(conf.dirDatos, 0o700) == nil, "no se pudo crear")
	agenda, err := datos.cargar()
	if err != nil {
		revisar("agenda", false, err.Error())
	} else {
		revisar("agenda", true, "")
		fmt.Printf("  %d bloques, %d tareas\n", len(agenda.Bloques), len(agenda.Tareas))
		for _, f := range agenda.Config.Fuentes {
			if _, err := os.Stat(filepath.Join(conf.vault, f)); err != nil {
				fmt.Println("  aviso    fuente de /sugerir no existe:", f)
			}
		}
	}
	if fallos > 0 {
		fmt.Println("revisión: falló")
		return 1
	}
	fmt.Println("revisión: ok")
	return 0
}

// envOR devuelve la variable de entorno o el valor por omisión.
func envOR(clave, porDefecto string) string {
	if valor := os.Getenv(clave); valor != "" {
		return valor
	}
	return porDefecto
}

func directorioExiste(ruta string) bool {
	informacion, err := os.Stat(ruta)
	return err == nil && informacion.IsDir()
}

func main() {
	os.Exit(principal())
}
