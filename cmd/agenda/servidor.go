package main

// Servidor OpenCode supervisado: `opencode serve` corre como hijo del bot
// (bajo qemu-x86_64-static, si se configura) y se relanza con retroceso si
// muere. En la CPU del servidor el arranque emulado tarda ~4 min; con un solo
// proceso de larga vida, las llamadas del bot son rápidas.

import (
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type servidorSupervisado struct {
	bin  string // ruta del binario opencode
	qemu string // ruta de qemu-x86_64-static; vacía = ejecución directa
	base string // URL donde escucha, p. ej. http://127.0.0.1:4096

	mu        sync.Mutex
	proceso   *exec.Cmd
	vivo      bool // el proceso hijo no ha terminado
	listo     bool // vivo y respondiendo /api/info
	debeParar bool
}

func nuevoServidor(bin, qemu, base string) *servidorSupervisado {
	return &servidorSupervisado{bin: bin, qemu: qemu, base: base}
}

func (s *servidorSupervisado) puerto() int {
	parseado, err := url.Parse(s.base)
	if err != nil || parseado.Port() == "" {
		return 4096
	}
	n, err := strconv.Atoi(parseado.Port())
	if err != nil {
		return 4096
	}
	return n
}

// arrancar lanza el proceso y bloquea hasta que /api/info responda.
func (s *servidorSupervisado) arrancar() error {
	if err := s.lanzar(); err != nil {
		return err
	}
	if err := s.esperarListo(); err != nil {
		return err
	}
	s.vigilar()
	return nil
}

// lanzar crea el proceso hijo con grupo propio (para apagarlo en bloque).
func (s *servidorSupervisado) lanzar() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vivo {
		return nil // ya vive
	}
	programa := s.qemu // vacío → ejecución directa del binario
	argumentos := []string{}
	if s.qemu != "" {
		argumentos = append(argumentos, s.bin)
	} else {
		programa = s.bin
	}
	argumentos = append(argumentos, "serve",
		"--port", fmt.Sprintf("%d", s.puerto()),
		"--hostname", "127.0.0.1",
		"--log-level", "ERROR")
	cmd := exec.Command(programa, argumentos...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return BridgeError("no se pudo iniciar el servidor de OpenCode: " + err.Error())
	}
	go func() {
		_ = cmd.Wait() // el reinicio lo maneja vigilar por sondeo
		s.mu.Lock()
		if s.proceso == cmd {
			s.vivo, s.listo = false, false
		}
		s.mu.Unlock()
	}()
	s.proceso = cmd
	s.vivo, s.listo = true, false
	return nil
}

// esperarListo sondea /api/info hasta 8 minutos (el arranque emulado es lento).
func (s *servidorSupervisado) esperarListo() error {
	plazo := time.Now().Add(8 * time.Minute)
	for time.Now().Before(plazo) {
		if s.sondaOk() {
			s.marcarListo()
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return BridgeError("el servidor de OpenCode no quedó disponible a tiempo")
}

func (s *servidorSupervisado) sondaOk() bool {
	peticion, err := http.NewRequest("GET", s.base+"/api/info", nil)
	if err != nil {
		return false
	}
	respuesta, err := (&http.Client{Timeout: 10 * time.Second}).Do(peticion)
	if err != nil {
		return false
	}
	respuesta.Body.Close()
	return respuesta.StatusCode < 300
}

// vigilar en fondo: si el proceso muere, lo relanza con retroceso creciente.
func (s *servidorSupervisado) vigilar() {
	go func() {
		atraso := 5 * time.Second
		for {
			time.Sleep(time.Second)
			if s.parado() {
				return
			}
			s.mu.Lock()
			vivo := s.vivo
			s.mu.Unlock()
			if vivo {
				atraso = 5 * time.Second
				continue
			}
			time.Sleep(atraso)
			if s.parado() {
				return
			}
			if atraso < 2*time.Minute {
				atraso *= 2
			}
			_ = s.lanzar()
			plazo := time.Now().Add(10 * time.Minute)
			for time.Now().Before(plazo) && !s.parado() {
				if s.sondaOk() {
					s.marcarListo()
					break
				}
				time.Sleep(3 * time.Second)
			}
		}
	}()
}

func (s *servidorSupervisado) marcarListo() {
	s.mu.Lock()
	s.listo = s.vivo
	s.mu.Unlock()
}

func (s *servidorSupervisado) parado() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.debeParar
}

// disponible informa si hay servidor vivo y asentado.
func (s *servidorSupervisado) disponible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listo
}

// detener mata el grupo de procesos del servidor.
func (s *servidorSupervisado) detener() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debeParar = true
	if s.proceso != nil && s.proceso.Process != nil {
		_ = syscall.Kill(-s.proceso.Process.Pid, syscall.SIGTERM)
	}
}
