package main

// Cliente de Groq (API compatible con OpenAI, /chat/completions). Las
// funciones IA del bot no necesitan herramientas: una consigna de sistema,
// el mensaje y respuesta en modo JSON. Sin estado entre turnos.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

const urlGroq = "https://api.groq.com/openai/v1/chat/completions"

type clienteIA struct {
	url    string
	clave  string
	modelo string
	cli    *http.Client
	espera func(time.Duration) // inyectable en tests
}

func nuevoClienteIA(clave, modelo string) *clienteIA {
	return &clienteIA{
		url:    urlGroq,
		clave:  clave,
		modelo: modelo,
		cli:    &http.Client{Timeout: 90 * time.Second},
		espera: time.Sleep,
	}
}

type mensajeChat struct {
	Rol       string `json:"role"`
	Contenido string `json:"content"`
}

// completar manda sistema + usuario y devuelve el texto (JSON) del modelo.
// Ante un 429 (límite por minuto de Groq) espera y reintenta hasta 3 veces.
func (c *clienteIA) completar(sistema, usuario string) (string, error) {
	cuerpo, err := json.Marshal(map[string]any{
		"model":            c.modelo,
		"messages":         []mensajeChat{{"system", sistema}, {"user", usuario}},
		"response_format":  map[string]string{"type": "json_object"},
		"reasoning_effort": "low",
		"temperature":      0.2,
	})
	if err != nil {
		return "", BridgeError("no se pudo preparar la petición al modelo")
	}
	for intento := 0; ; intento++ {
		texto, reintentarEn, err := c.peticion(cuerpo)
		if reintentarEn > 0 && intento < 3 {
			c.espera(reintentarEn)
			continue
		}
		return texto, err
	}
}

// peticion hace una llamada; si es un 429 devuelve cuánto esperar.
func (c *clienteIA) peticion(cuerpo []byte) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(cuerpo))
	if err != nil {
		return "", 0, BridgeError("no se pudo armar la petición al modelo")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.clave)
	resp, err := c.cli.Do(req)
	if err != nil {
		return "", 0, BridgeError("el modelo no está disponible; reintenta en unos minutos")
	}
	defer resp.Body.Close()
	contenido, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		espera := 20 * time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 60 {
			espera = time.Duration(s) * time.Second
		}
		return "", espera, BridgeError("llegué al límite de uso del modelo; espera un minuto y reintenta")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", 0, BridgeError("el modelo rechazó la clave; revisa GROQ_API_KEY en el servidor")
	case resp.StatusCode >= 400:
		return "", 0, BridgeError("el modelo devolvió un error (" + strconv.Itoa(resp.StatusCode) + "); reintenta")
	}
	var respuesta struct {
		Choices []struct {
			Message mensajeChat `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(contenido, &respuesta); err != nil || len(respuesta.Choices) == 0 {
		return "", 0, BridgeError("respuesta inesperada del modelo")
	}
	return respuesta.Choices[0].Message.Contenido, 0, nil
}
