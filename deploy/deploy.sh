#!/bin/sh
# Despliegue del bot de agenda (Go + OpenCode bajo QEMU) en el servidor ASUS.
# Datos del bot en $DATOS (fuera de la bóveda); la bóveda se monta solo lectura.
# Uso: ./deploy.sh [usuario@host]    (por defecto asus-tail)
# Convenciones del MANUAL: servicio en /opt/apps/services/bot-agenda/,
# Docker con sudo (franco no está en el grupo docker), sin puertos publicados.
set -eu

DESTINO="${1:-asus-tail}"
SERVICIO="/opt/apps/services/bot-agenda"
VAULT="/opt/apps/data/obsidian-vault"
DATOS="/opt/apps/data/bot-agenda"
AUTH="/home/franco/.local/share/opencode/auth.json"
ORIGEN="$(cd "$(dirname "$0")/.." && pwd)"

# 1. Escenario limpio: solo lo que la imagen necesita.
ESCENARIO="$(mktemp -d)"
mkdir -p "$ESCENARIO/cmd"
cp "$ORIGEN/go.mod" "$ORIGEN/go.sum" "$ORIGEN/deploy/Dockerfile" \
   "$ORIGEN/deploy/docker-compose.yml" "$ESCENARIO/"
cp -r "$ORIGEN/cmd/agenda" "$ESCENARIO/cmd/agenda"
rm -f "$ESCENARIO"/cmd/agenda/*_test.go
cp "$ORIGEN/deploy/horario.inicial.json" "$ESCENARIO/"
trap 'rm -rf "$ESCENARIO"' EXIT

# 2. Trasladar al servidor.
ssh "$DESTINO" "mkdir -p /tmp/.agenda-escenario"
rsync -r --delete "$ESCENARIO/" "$DESTINO:/tmp/.agenda-escenario/"
ssh "$DESTINO" "
sudo -n mkdir -p '$SERVICIO'
sudo -n rsync -a --delete --exclude agent.env /tmp/.agenda-escenario/ '$SERVICIO/'
rm -rf /tmp/.agenda-escenario
cd '$SERVICIO'
if [ ! -f agent.env ]; then
    echo 'FALTA agent.env; crea $SERVICIO/agent.env con TELEGRAM_BOT_TOKEN y TELEGRAM_OWNER_ID (chmod 600).' >&2
    exit 1
fi
if [ ! -f '$VAULT/Inicio.md' ]; then
    echo \"FALTA la bóveda en $VAULT (Inicio.md); revisa el montaje.\" >&2
    exit 1
fi
if [ ! -f '$AUTH' ]; then
    echo \"FALTA '$AUTH'; autentica OpenCode en el servidor primero (opencode auth login).\" >&2
    exit 1
fi
sudo -n mkdir -p '$DATOS'
sudo -n chmod 700 '$DATOS'
if [ ! -f '$DATOS/horario.json' ]; then
    sudo -n cp horario.inicial.json '$DATOS/horario.json'
    echo 'horario.json inicial instalado en $DATOS'
fi
sudo -n chmod 600 agent.env
sudo -n docker compose build
sudo -n docker compose run --rm agenda agenda check
sudo -n docker compose up -d --build
sudo -n docker compose logs --tail 20 agenda
"
echo "Despliegue listo. Verifica con: ssh $DESTINO 'sudo -n docker compose -f $SERVICIO/docker-compose.yml logs -f agenda'"
