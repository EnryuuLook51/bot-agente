# Bot de agenda (Go) en Docker para el servidor ASUS.
# La CPU del host (AMD E2-1800, 2011) no corre el baseline de OpenCode
# (compilado con Bun: pide SSE4.2/AVX2), así que OpenCode va bajo
# qemu-x86_64-static y el binario del bot es nativo (Go).
# Convenciones del MANUAL: sin puertos publicados (el bot solo hace polling
# saliente), restart: unless-stopped.

FROM golang:1.27-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /fuente
COPY go.mod go.sum ./
COPY cmd/agenda ./cmd/agenda
RUN go mod download && go build -trimpath -ldflags "-s -w" -o /salida/agenda ./cmd/agenda

FROM debian:trixie-slim
ENV TZ=America/Lima \
    OPENCODE_DISABLE_AUTOUPDATE=1
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl git qemu-user-static tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /root/.opencode/bin /root/.local/share/opencode \
    && curl -fsSL -o /tmp/oc.tar.gz \
        "https://github.com/anomalyco/opencode/releases/latest/download/opencode-linux-x64-baseline.tar.gz" \
    && tar xzf /tmp/oc.tar.gz -C /root/.opencode/bin \
    && rm /tmp/oc.tar.gz \
    && /usr/bin/qemu-x86_64-static /root/.opencode/bin/opencode --version \
    && mkdir -p /root/.cache/opencode \
    && curl -fsSL -o /root/.cache/opencode/models.json https://models.dev/api.json
# El catálogo de modelos va precargado: bajo qemu, OpenCode no alcanza a
# descargarlo al arrancar y queda sin modelos ("Unsupported API ... native").
COPY --from=build /salida/agenda /usr/local/bin/agenda
CMD ["agenda"]
