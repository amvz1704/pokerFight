#!/usr/bin/env bash
# Prueba de humo: levanta una mesa local y conecta 2 bots de ejemplo para
# jugar un torneo corto de punta a punta.
#
# No depende de `make build`: ese target todavia intenta compilar
# ./cmd/casino y ./cmd/bot, que no tienen dueño terminado (ver README,
# seccion "Alcance y estado"). Este script compila solo lo que hace falta
# para la prueba: la mesa y los dos bots de sparring.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN=bin
mkdir -p "$BIN"

go build -o "$BIN/mesa" ./cmd/mesa
go build -o "$BIN/bot-aleatorio" ./bots/aleatorio
go build -o "$BIN/bot-conservador" ./bots/conservador

ADDR="${ADDR:-:9000}"
RONDAS="${RONDAS:-10}"

"$BIN/mesa" -addr "$ADDR" -jugadores 2 -min-jugadores 2 -rondas "$RONDAS" &
MESA_PID=$!
trap 'kill "$MESA_PID" "$BOT1_PID" "$BOT2_PID" 2>/dev/null || true' EXIT

sleep 1

"$BIN/bot-aleatorio" -addr "localhost${ADDR}" -token bot-aleatorio-1 &
BOT1_PID=$!
"$BIN/bot-conservador" -addr "localhost${ADDR}" -token bot-conservador-1 &
BOT2_PID=$!

wait "$MESA_PID"
