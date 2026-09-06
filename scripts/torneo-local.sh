#!/usr/bin/env bash
# Prueba de humo: compila todo y corre el torneo definido en torneo.json.
#
# Sin argumentos corre en modo abierto: el token de cada participante es
# directamente su identificador. Con -casino, registra las cuentas en un Casino
# real, hace login para conseguir tokens, corre el torneo validando contra esas
# cuentas y al final imprime el ranking historico.
#
# Uso:
#   scripts/torneo-local.sh
#   scripts/torneo-local.sh -casino casino.json
#   CONFIG=mi-torneo.json scripts/torneo-local.sh
set -euo pipefail
cd "$(dirname "$0")/.."

CONFIG="${CONFIG:-torneo.json}"
CASINO=""

while [ $# -gt 0 ]; do
    case "$1" in
        -casino) CASINO="$2"; shift 2 ;;
        -config) CONFIG="$2"; shift 2 ;;
        *) echo "opcion desconocida: $1" >&2; exit 2 ;;
    esac
done

echo "Compilando..."
go build -o bin/ ./cmd/... ./bots/...

if [ ! -f "$CONFIG" ]; then
    echo "No existe $CONFIG, escribiendo uno de ejemplo..."
    ./bin/arena init -config "$CONFIG"
fi

ARGS=(correr -config "$CONFIG")

if [ -n "$CASINO" ]; then
    echo "Registrando cuentas en $CASINO (las que ya existan dan error y se ignora)..."
    # Los ids del archivo de torneo son los usuarios del Casino. jq no siempre
    # esta instalado, asi que se sacan con el propio validador de la arena.
    ids=$(./bin/arena validar -config "$CONFIG" \
          | sed -n 's/.*sillas: \[\(.*\)\].*/\1/p' | tr ' ' '\n' | sort -u)

    for id in $ids; do
        ./bin/casino -db "$CASINO" registrar -usuario "$id" >/dev/null 2>&1 || true
    done

    echo "AVISO: con -casino cada participante necesita su token en el archivo de"
    echo "       torneo. Conseguilos con: ./bin/casino -db $CASINO login -usuario <id>"
    ARGS+=(-casino-db "$CASINO")
fi

./bin/arena "${ARGS[@]}"

if [ -n "$CASINO" ]; then
    echo
    echo "Ranking historico:"
    ./bin/casino -db "$CASINO" ranking
fi
