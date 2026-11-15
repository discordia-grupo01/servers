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

if [ -x "$VENV/Scripts/python.exe" ]; then PY_VENV="$VENV/Scripts/python.exe"; elif [ -x "$VENV/bin/python" ]; then PY_VENV="$VENV/bin/python"; else PY_VENV="$PY"; fi

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

rm -rf "$REPORT_DIR"
BIN=.contract-servers-api
go build -o "$BIN" ./cmd/api
"./$BIN" >"$LOG" 2>&1 &
APP_PID=$!
cleanup() {
  kill "$APP_PID" 2>/dev/null || true
  # En Windows (Git Bash) kill no alcanza al proceso nativo: se mata por PID de Windows.
  if command -v taskkill >/dev/null 2>&1; then
    WINPID="$(ps -p "$APP_PID" -W 2>/dev/null | awk 'NR>1 {print $4}' || true)"
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

# Datos de prueba por la API (ids reales para Schemathesis); ver seed.py. Las
# operaciones que borran o que consumen estado se corren aparte, cada una con un
# juego de datos nuevo, para que no destruyan lo que usan las demas:
#   1) todo menos DELETE y menos aceptar/rechazar transferencias
#   2) DELETE que deshacen algo        3) DELETE de baneos
#   4) DELETE de canal, rol, invitacion y emoji   5) DELETE del servidor
#   6) abandonar el servidor (como el propio miembro)
#   7) aceptar y 8) rechazar la transferencia (como el destinatario)
# Despues, scenario.py recorre el camino feliz de los flujos que el fuzzing no
# alcanza y coverage.py mide cuantas operaciones tuvieron un 2xx real.
seed() {
  "$PY" scripts/contract/seed.py "$BASE" "$USER_ID" | tr -d '\r'
}
DESTRUCTIVE='^(/v1/channels/\{channelId\}|/v1/roles/\{roleId\}|/v1/invites/\{code\}|/v1/servers/\{serverId\}/emojis/\{emojiId\})$'
NOT_DELETE='^(GET|POST|PUT|PATCH)$'
TRANSFER_DECISION='/ownership-transfers/\{transferId\}/(accept|reject)$'

run_st() {
  local tok="$TOKEN"
  if [ -n "${AS_TOKEN:-}" ]; then tok="$AS_TOKEN"; fi
  "$ST" --config-file scripts/contract/schemathesis.toml run openapi.yaml \
    --url "$BASE" \
    -H "Authorization: Bearer $tok" \
    --max-examples "$MAX_EXAMPLES" \
    --seed 1 \
    --workers 1 \
    --report junit,har --report-dir "$REPORT_DIR" "$@"
}

status=0
step() { echo "== $1"; eval "$(seed)"; }

step "1/8: operaciones que no son DELETE ni aceptar/rechazar"
run_st --exclude-method DELETE --exclude-path-regex "$TRANSFER_DECISION" || status=1
step "2/8: DELETE que deshacen algo"
run_st --include-method DELETE --exclude-path-regex "$DESTRUCTIVE|/bans/|^/v1/servers/\{serverId\}(/members/\{userId\})?$" || status=1
step "3/8: DELETE de baneos"
SEED_MEMBER="$SEED_BAN" run_st --include-path-regex '/bans/' || status=1
step "4/8: DELETE de canal, rol, invitacion y emoji"
run_st --include-path-regex "$DESTRUCTIVE" --exclude-method-regex "$NOT_DELETE" || status=1
step "5/8: DELETE del servidor"
SEED_SERVER="$SEED_SERVER_DEL" SEED_SERVER_NAME="$SEED_SERVER_DEL_NAME" \
  run_st --include-path-regex '^/v1/servers/\{serverId\}$' --exclude-method-regex "$NOT_DELETE" || status=1
step "6/8: abandonar el servidor"
SEED_MEMBER="$SEED_LEAVER" AS_TOKEN="$SEED_LEAVER_TOKEN" \
  run_st --include-path-regex '^/v1/servers/\{serverId\}/members/\{userId\}$' || status=1
step "7/8: aceptar la transferencia"
AS_TOKEN="$SEED_MEMBER_TOKEN" run_st --include-path-regex '/ownership-transfers/\{transferId\}/accept$' || status=1
step "8/8: rechazar la transferencia"
AS_TOKEN="$SEED_MEMBER_TOKEN" run_st --include-path-regex '/ownership-transfers/\{transferId\}/reject$' || status=1

echo "== escenario de camino feliz"
"$PY_VENV" scripts/contract/scenario.py openapi.yaml "$BASE" "$REPORT_DIR/har-scenario.json" || status=1

echo "== cobertura de contrato"
"$PY_VENV" scripts/contract/coverage.py openapi.yaml "$REPORT_DIR"/har-*.json || status=1
exit "$status"
