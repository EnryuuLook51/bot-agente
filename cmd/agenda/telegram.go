package main

// Wrapper de la API de Telegram (tgbotapi): mensajes solo al propietario, sin
// exponer el token en errores, botones inline y registro de cada salida.

import (
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// limiteMensaje deja margen bajo el máximo de Telegram (4096).
const limiteMensaje = 3900

// Relay envuelve la API con mensajería segura para el propietario.
type Relay struct {
	bot      *tgbotapi.BotAPI
	owner    int64
	registro *registro
}

func newTelegram(token string, owner int64, reg *registro) (*Relay, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		// El detalle del error puede contener la URL con el token.
		return nil, BridgeError("no se pudo conectar con Telegram; revisa el token y la conexión")
	}
	bot.Client = &http.Client{Timeout: 40 * time.Second}
	return &Relay{bot: bot, owner: owner, registro: reg}, nil
}

// registrarComandos instala el menú visible solo para el propietario.
func (r *Relay) registrarComandos() error {
	comandos := []tgbotapi.BotCommand{
		{Command: "hoy", Description: "Itinerario de hoy"},
		{Command: "semana", Description: "Horario semanal"},
		{Command: "sugerir", Description: "Sugerir tareas según tus notas"},
		{Command: "ayuda", Description: "Cómo usar el bot"},
	}
	menu := tgbotapi.NewSetMyCommandsWithScope(
		tgbotapi.BotCommandScope{Type: "chat", ChatID: r.owner}, comandos...)
	if _, err := r.bot.Request(menu); err != nil {
		return BridgeError("no se pudo registrar el menú de comandos; continúo igual")
	}
	return nil
}

// send manda texto plano, partido en trozos si supera el límite.
func (r *Relay) send(texto string) error {
	r.registro.anotar("salida", texto)
	for _, trozo := range partir(texto, limiteMensaje) {
		msg := tgbotapi.NewMessage(r.owner, trozo)
		msg.DisableWebPagePreview = true
		if _, err := r.bot.Send(msg); err != nil {
			return BridgeError("no pude enviar el mensaje de Telegram")
		}
	}
	return nil
}

// sendConBotones manda un texto con botones inline; cada fila es una lista de
// pares {etiqueta, callback}. Devuelve el id del mensaje.
func (r *Relay) sendConBotones(texto string, filas [][][2]string) (int, error) {
	r.registro.anotar("salida", texto)
	msg := tgbotapi.NewMessage(r.owner, recortar(texto, limiteMensaje))
	msg.DisableWebPagePreview = true
	if teclado, ok := tecladoDe(filas); ok {
		msg.ReplyMarkup = teclado
	}
	enviado, err := r.bot.Send(msg)
	if err != nil {
		return 0, BridgeError("no pude enviar el mensaje con botones")
	}
	return enviado.MessageID, nil
}

// cambiarBotones reemplaza los botones de un mensaje (vacío = quitarlos). Si
// Telegram no lo permite (mensaje antiguo), no bloquea el flujo.
func (r *Relay) cambiarBotones(mensajeID int, filas [][][2]string) {
	teclado, ok := tecladoDe(filas)
	if !ok {
		teclado = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
	}
	_, _ = r.bot.Request(tgbotapi.NewEditMessageReplyMarkup(r.owner, mensajeID, teclado))
}

func tecladoDe(filas [][][2]string) (tgbotapi.InlineKeyboardMarkup, bool) {
	var teclado [][]tgbotapi.InlineKeyboardButton
	for _, fila := range filas {
		var botones []tgbotapi.InlineKeyboardButton
		for _, par := range fila {
			botones = append(botones, tgbotapi.NewInlineKeyboardButtonData(par[0], par[1]))
		}
		if len(botones) > 0 {
			teclado = append(teclado, botones)
		}
	}
	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: teclado}, len(teclado) > 0
}

// botonesDe extrae las filas actuales de un mensaje (para quitar una sola).
func botonesDe(m *tgbotapi.Message) [][][2]string {
	if m == nil || m.ReplyMarkup == nil {
		return nil
	}
	var filas [][][2]string
	for _, fila := range m.ReplyMarkup.InlineKeyboard {
		var pares [][2]string
		for _, b := range fila {
			if b.CallbackData != nil {
				pares = append(pares, [2]string{b.Text, *b.CallbackData})
			}
		}
		filas = append(filas, pares)
	}
	return filas
}

// typing enciende el indicador "escribiendo..." del propietario.
func (r *Relay) typing() {
	_, _ = r.bot.Request(tgbotapi.NewChatAction(r.owner, tgbotapi.ChatTyping))
}

// updates hace long polling devolviendo updates nuevos desde el offset.
func (r *Relay) updates(offset int) ([]tgbotapi.Update, error) {
	config := tgbotapi.NewUpdate(offset)
	config.Timeout = 25
	config.AllowedUpdates = []string{"message", "callback_query"}
	lote, err := r.bot.GetUpdates(config)
	if err != nil {
		return nil, BridgeError("Telegram no respondió; reintentando")
	}
	return lote, nil
}

// responderCallback saca el "cargando..." del botón presionado.
func (r *Relay) responderCallback(id, texto string) {
	_, _ = r.bot.Request(tgbotapi.NewCallback(id, texto))
}

// partir divide el texto en trozos de hasta n runas, cortando en saltos de
// línea cuando se puede.
func partir(texto string, n int) []string {
	var trozos []string
	for len([]rune(texto)) > n {
		runas := []rune(texto)
		corte := strings.LastIndex(string(runas[:n]), "\n")
		if corte <= 0 {
			corte = len(string(runas[:n]))
		}
		trozos = append(trozos, texto[:corte])
		texto = strings.TrimLeft(texto[corte:], "\n")
	}
	if texto != "" || len(trozos) == 0 {
		trozos = append(trozos, texto)
	}
	return trozos
}
