package main

// Errores "públicos": el texto es lo que puede llegar a Telegram, sin
// detalles internos (tokens, rutas absolutas, mensajes crudos de APIs).

import "errors"

// BridgeError construye un error seguro para mostrar al usuario.
func BridgeError(mensaje string) error { return errors.New(mensaje) }
