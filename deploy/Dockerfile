# Bot de agenda (Go) en Docker para el servidor ASUS.
# Binario estático; la IA es la API de Groq (HTTPS saliente), así que la
# imagen solo necesita certificados y zona horaria.
# Convenciones del MANUAL: sin puertos publicados, restart: unless-stopped.

FROM golang:1.27-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /fuente
COPY go.mod go.sum ./
COPY cmd/agenda ./cmd/agenda
RUN go mod download && go build -trimpath -ldflags "-s -w" -o /salida/agenda ./cmd/agenda

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=America/Lima
COPY --from=build /salida/agenda /usr/local/bin/agenda
CMD ["agenda"]
