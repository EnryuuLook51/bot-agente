# bot-agenda

Bot de Telegram que gestiona tu horario semanal y tus tareas, con IA solo
donde suma valor.

## Qué hace

**Sin IA (instantáneo):**
- `/hoy` — itinerario de hoy: bloques ordenados + tareas del día.
- `/semana` — horario de lunes a domingo.
- Recordatorios a las **06:30** (itinerario del día) y **21:00** (cierre del
  día) con las tareas de hoy y las atrasadas; un botón ✅ por tarea para
  marcarla hecha.
- Aviso 15 min antes de cada bloque.

**Con IA (contexto mínimo; Groq, `openai/gpt-oss-120b`, respuesta en ~2 s):**
- **Texto libre** (Función 2) — agrega, completa, mueve o edita tareas y
  bloques. El modelo recibe solo el JSON de horario + tareas, la fecha/hora
  actual (inyectada por código) y tu mensaje. Si algo es ambiguo, pregunta.
  Cada acción se valida por separado; las inválidas se descartan y se avisan.
- **`/sugerir`** (Función 1) — lee los `.md` listados en `config.fuentes` y
  propone tareas para tus bloques; aceptas o rechazas con botones.

El agente conversacional que lee la bóveda completa es otro proyecto
(`asistente-notas`).

## Datos

Viven **fuera de la bóveda**, en `/opt/apps/data/bot-agenda/` (servidor). La
bóveda se monta en solo lectura: el bot nunca escribe en ella, así que no
provoca divergencias en el sync de Git.

| Archivo | Contenido |
|---|---|
| `horario.json` | `bloques` fijos (clases de la uni, gimnasio, estudio…) y `config` |
| `tareas.json` | `tareas`: texto, estado, fecha, bloque, origen (usuario/ia/sugerencia) |
| `registro.jsonl` | log de entradas, salidas y cambios con fecha/hora (nunca se manda a la IA) |
| `estado.json` | offset de Telegram, avisos ya enviados, sugerencias pendientes |
| `respaldos/` | versión anterior de `horario.json` y `tareas.json` antes de cada cambio |

`config` en `horario.json`:

```json
{
  "fuentes": ["03-Universidad/2026-II/Pendientes académicos.md"],
  "avisoMinutos": 15,
  "recordatorios": ["06:30", "21:00"]
}
```

`avisoMinutos` negativo apaga los avisos de bloque. El horario inicial
(`deploy/horario.inicial.json`) sale de *Horario semanal - 2026-II.md* y se
instala solo si aún no existe `horario.json`.

## Estructura

```
cmd/agenda/
  main.go        composición: Telegram, ticker, worker IA, botones
  datos.go       esquema y validación
  almacen.go     horario.json + tareas.json: lectura, escritura atómica, respaldo
  estado.go      estado interno (con mutex)
  registro.go    registro.jsonl
  aplicar.go     acciones de la IA sobre la agenda (validadas una por una)
  avisos.go      avisos de bloque y recordatorios con botones ✅
  render.go      /hoy y /semana
  reloj.go       hora de Lima en español
  ia.go          cliente de Groq (/chat/completions, modo JSON, reintento ante 429)
  ia_turno.go    Función 2: texto libre
  ia_sugerir.go  Función 1: /sugerir
  telegram.go    wrapper de Telegram (sin exponer el token)
deploy/          Dockerfile, docker-compose.yml, deploy.sh, horario.inicial.json
```

## Secretos

`/opt/apps/services/bot-agenda/agent.env` (chmod 600, nunca en Git):

```
TELEGRAM_BOT_TOKEN=...   # bot propio (BotFather), distinto al de asistente-notas
TELEGRAM_OWNER_ID=...
GROQ_API_KEY=...         # la misma clave del agente de la bóveda
```

## Desarrollo

```sh
go test -race ./...
go run ./cmd/agenda -token X -owner 1 -vault RUTA -datos RUTA check
```

## Despliegue

```sh
./deploy/deploy.sh asus     # o asus-tail
```

Copia el código a `/opt/apps/services/bot-agenda/`, instala el horario
inicial si falta, valida con `agenda check` y levanta el contenedor. No
publica puertos: el bot solo hace conexiones salientes (Telegram y Groq).
