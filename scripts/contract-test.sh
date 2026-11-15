#!/usr/bin/env bash
# Valida que servers cumpla su openapi.yaml con Schemathesis.
# Levanta el servicio contra un Postgres y un RabbitMQ LOCALES y efimeros,
# nunca contra servicios remotos. Uso: make contract-test
# Las dependencias las levanta `make contract-deps-up` (o, en el CI, los
# servicios del job); las migraciones ya tienen que estar aplicadas.
set -euo pipefail

cd "$(dirname "$0")/.."

# Puertos de la tabla del plan de contratos (servers).
DB_HOST="${CONTRACT_DB_HOST:-localhost}"
DB_PORT="${CONTRACT_DB_PORT:-55505}"
MQ_HOST="${CONTRACT_MQ_HOST:-localhost}"
MQ_PORT="${CONTRACT_MQ_PORT:-55515}"
APP_PORT="${CONTRACT_APP_PORT:-18085}"
MAX_EXAMPLES="${CONTRACT_MAX_EXAMPLES:-10}"
REPORT_DIR="${CONTRACT_REPORT_DIR:-schemathesis-report}"

# Valores de juguete: no son secretos reales.
USER_ID="00000000-0000-4000-8000-0000000000c1"

# Guard: se rechaza todo host que no sea local.
for host in "$DB_HOST" "$MQ_HOST"; do
  case "$host" in
    localhost | 127.0.0.1 | ::1) ;;
    *)
      echo "contract-test: host no local '$host'; este script solo corre contra servicios locales." >&2
      exit 1
      ;;
  esac
done

export MSYS_NO_PATHCONV=1
# Schemathesis imprime caracteres que la consola de Windows (cp1252) no puede escribir.
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8
PY="${PYTHON:-python}"

VENV=".contract-venv"
if [ -x "$VENV/bin/schemathesis" ]; then
  ST="$VENV/bin/schemathesis"
elif [ -x "$VENV/Scripts/schemathesis.exe" ]; then
  ST="$VENV/Scripts/schemathesis.exe"
elif command -v schemathesis >/dev/null 2>&1; then
  ST="schemathesis"
else
  "$PY" -m venv "$VENV"
  if [ -x "$VENV/bin/pip" ]; then PIP="$VENV/bin/pip"; else PIP="$VENV/Scripts/pip.exe"; fi
  "$PIP" install -q "schemathesis==4.33.4"
  if [ -x "$VENV/bin/schemathesis" ]; then ST="$VENV/bin/schemathesis"; else ST="$VENV/Scripts/schemathesis.exe"; fi
fi

# servers no verifica la firma del JWT (la valida Kong): toma el `sub` del payload.
TOKEN="$("$PY" scripts/contract/make_token.py "$USER_ID")"

LOG="$(mktemp)"
ICONS="$(mktemp -d)"
export ENVIRONMENT=development HOST=127.0.0.1 PORT="$APP_PORT"
export DATABASE_HOST="$DB_HOST" DATABASE_PORT="$DB_PORT" DATABASE_USER=discordia DATABASE_PASSWORD=discordia
export DATABASE_NAME=discordia_servers_contract DATABASE_SSLMODE=disable DATABASE_SCHEMA=
export RABBITMQ_URL="amqp://guest:guest@$MQ_HOST:$MQ_PORT/"
export ADMIN_EVENTS_HMAC_SECRET="contract-test-hmac" BILLING_EVENTS_HMAC_SECRET="contract-test-hmac"
# Iconos en disco (carpeta temporal): sin storage de objetos real.
export ICON_DIR="$ICONS"
unset USER_ID_HEADER SUPABASE_STORAGE_URL SUPABASE_STORAGE_BUCKET SUPABASE_SERVICE_KEY DATABASE_MAX_CONNS

BIN=.contract-servers-api
go build -o "$BIN" ./cmd/api
"./$BIN" >"$LOG" 2>&1 &
APP_PID=$!
cleanup() {
  kill "$APP_PID" 2>/dev/null || true
  # En Windows (Git Bash) kill no alcanza al proceso nativo: se mata por PID de Windows.
  if command -v taskkill >/dev/null 2>&1; then
    WINPID="$(ps -p "$APP_PID" -W 2>/dev/null | awk 'NR>1 {print $4}')"
    if [ -n "${WINPID:-}" ]; then taskkill //F //PID "$WINPID" >/dev/null 2>&1 || true; fi
  fi
  rm -rf "$BIN" "$BIN.exe" "$LOG" "$ICONS"
}
trap cleanup EXIT

BASE="http://127.0.0.1:$APP_PORT"
ready=0
for _ in $(seq 1 60); do
  if curl -fsS "$BASE/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$APP_PID" 2>/dev/null; then
    echo "contract-test: el servicio no arrancó:" >&2
    cat "$LOG" >&2
    exit 1
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "contract-test: /readyz no respondió en 60 s:" >&2
  cat "$LOG" >&2
  exit 1
fi

"$ST" --config-file scripts/contract/schemathesis.toml run openapi.yaml \
  --url "$BASE" \
  -H "Authorization: Bearer $TOKEN" \
  --max-examples "$MAX_EXAMPLES" \
  --seed 1 \
  --workers 1 \
  --report junit --report-dir "$REPORT_DIR"
