package main

// Cliente del servidor OpenCode (opencode serve HTTP, en el mismo contenedor).
// Las Funciones 1 y 2 no necesitan herramientas del agente: con chat simple
// basta (el contexto es pequeño y determinista). El turno se manda a una
// sesión sin permisos y se sondea la respuesta, porque el endpoint
// experimental/wait devolvió HTML en la versión actual de la API.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type clienteIA struct {
	base   string // http://127.0.0.1:4096
	vault  string
	modelo string
	cli    *http.Client
}

// nuevoIDMensaje genera un id de mensaje con el prefijo "msg_" que exige
// OpenCode (mismo formato que usa asistente-notas).
func nuevoIDMensaje() string {
	return fmt.Sprintf("msg_agenda_%020d", time.Now().UnixNano())
}

func nuevoClienteIA(base, vault, modelo string) *clienteIA {
	return &clienteIA{
		base:   base,
		vault:  vault,
		modelo: modelo,
		cli:    &http.Client{},
	}
}

func (c *clienteIA) llamada(metodo, ruta string, cuerpo any, timeout time.Duration) (map[string]json.RawMessage, error) {
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var lector io.Reader
	if cuerpo != nil {
		valor, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, BridgeError("no se pudo preparar la petición al modelo")
		}
		lector = bytes.NewReader(valor)
	}
	peticion, err := http.NewRequestWithContext(ctx, metodo, c.base+ruta, lector)
	if err != nil {
		return nil, BridgeError("no se pudo armar la petición al modelo")
	}
	if cuerpo != nil {
		peticion.Header.Set("Content-Type", "application/json")
	}
	respuesta, err := c.cli.Do(peticion)
	if err != nil {
		return nil, BridgeError("el modelo no está disponible; reintenta en unos minutos")
	}
	defer respuesta.Body.Close()
	contenido, err := io.ReadAll(io.LimitReader(respuesta.Body, 1<<24))
	if err != nil {
		return nil, BridgeError("respuesta incompleta del modelo")
	}
	if respuesta.StatusCode >= 400 {
		return nil, BridgeError("el modelo rechazó la operación; revisa la autenticación del servidor")
	}
	if len(bytes.TrimSpace(contenido)) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	resultado := map[string]json.RawMessage{}
	if err := json.Unmarshal(contenido, &resultado); err != nil {
		return nil, BridgeError("respuesta inesperada del modelo")
	}
	if _, marcada := resultado["_tag"]; marcada {
		return nil, BridgeError("el modelo devolvió un error")
	}
	if _, fallo := resultado["error"]; fallo {
		return nil, BridgeError("el modelo devolvió un error")
	}
	return resultado, nil
}

func (c *clienteIA) crearSesion() (string, error) {
	cuerpo := map[string]any{
		"title":    "Agenda",
		"agent":    "build",
		"location": map[string]string{"directory": c.vault},
		// Sin permisos: en estas funciones la IA nunca toca archivos.
		"permissions": []map[string]string{{"action": "*", "resource": "*", "effect": "deny"}},
		"model":       map[string]string{"providerID": "opencode", "id": c.modelo},
	}
	respuesta, err := c.llamada("POST", "/api/session", cuerpo, 120*time.Second)
	if err != nil {
		return "", err
	}
	bruto, ok := respuesta["data"]
	if !ok {
		return "", BridgeError("respuesta inesperada del modelo")
	}
	var sesion struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(bruto, &sesion); err != nil || sesion.ID == "" {
		return "", BridgeError("respuesta inesperada del modelo")
	}
	return sesion.ID, nil
}

// turno envía el prompt y sondea hasta ver la respuesta del asistente.
func (c *clienteIA) turno(session, identificador, texto string) (string, error) {
	cuerpo := map[string]any{
		"id":     identificador,
		"prompt": map[string]string{"text": texto},
	}
	if _, err := c.llamada("POST", "/api/session/"+url.PathEscape(session)+"/prompt", cuerpo, 120*time.Second); err != nil {
		return "", err
	}
	plazo := time.Now().Add(5 * time.Minute)
	ruta := "/api/session/" + url.PathEscape(session) + "/message?limit=20&order=desc"
	for time.Now().Before(plazo) {
		time.Sleep(4 * time.Second)
		respuesta, err := c.llamada("GET", ruta, nil, 60*time.Second)
		if err != nil {
			return "", err
		}
		bruto, ok := respuesta["data"]
		if !ok {
			continue
		}
		if textoRespuesta, listo := extraerTurno(bruto, identificador); listo {
			return textoRespuesta, nil
		}
	}
	return "", BridgeError("el modelo no respondió a tiempo; espera y reintenta")
}

// extraerTurno busca la respuesta al turno del usuario. La página viene del
// mensaje más reciente al más antiguo, así que la respuesta del asistente está
// ANTES del turno en la lista: se juntan esos mensajes y se invierten.
// Devuelve listo=false si el turno aún no aparece o no hay texto todavía.
func extraerTurno(bruto json.RawMessage, identificador string) (string, bool) {
	var mensajes []apiMensaje
	if err := json.Unmarshal(bruto, &mensajes); err != nil {
		return "", false
	}
	var bloques []string
	encontrado := false
	for _, m := range mensajes {
		if m.ID == identificador {
			encontrado = true
			break
		}
		if m.Tipo != "assistant" {
			continue
		}
		for i := len(m.Contenido) - 1; i >= 0; i-- {
			if c := m.Contenido[i]; c.Tipo == "text" && strings.TrimSpace(c.Texto) != "" {
				bloques = append(bloques, c.Texto)
			}
		}
	}
	if !encontrado || len(bloques) == 0 {
		return "", false
	}
	for i, j := 0, len(bloques)-1; i < j; i, j = i+1, j-1 {
		bloques[i], bloques[j] = bloques[j], bloques[i]
	}
	return strings.Join(bloques, "\n"), true
}

// apiMensaje replica el formato de mensajes de la API de OpenCode.
type apiMensaje struct {
	ID        string         `json:"id"`
	Tipo      string         `json:"type"`
	Contenido []apiContenido `json:"content"`
}

type apiContenido struct {
	Tipo  string `json:"type"`
	Texto string `json:"text"`
}
