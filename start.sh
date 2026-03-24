#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
PROCESSOR_BIN="$ROOT/processor/poracle-processor"
ALERTER_DIR="$ROOT/alerter"
CONFIG_FILE="$ROOT/config/config.toml"
HEALTH_URL=""
PROCESSOR_PID=""
ALERTER_PID=""

graceful_kill() {
	local pid=$1 name=$2
	[ -z "$pid" ] && return
	kill "$pid" 2>/dev/null || return
	local i=0
	while [ $i -lt 8 ] && kill -0 "$pid" 2>/dev/null; do
		sleep 1
		i=$((i + 1))
	done
	if kill -0 "$pid" 2>/dev/null; then
		echo "[start] $name did not stop after 8s, sending SIGKILL"
		kill -9 "$pid" 2>/dev/null
	fi
	wait "$pid" 2>/dev/null
}

cleanup() {
	echo ""
	echo "[start] Shutting down..."
	graceful_kill "$ALERTER_PID" "Alerter"
	graceful_kill "$PROCESSOR_PID" "Processor"
	echo "[start] Stopped"
	exit 0
}

trap cleanup SIGINT SIGTERM

fail() {
	echo "[start] ERROR: $1" >&2
	exit 1
}

# ---- Pre-flight checks ----

# Config
if [ ! -f "$CONFIG_FILE" ]; then
	fail "Config file not found at $CONFIG_FILE
  Copy config/config.example.toml to config/config.toml and edit it."
fi

# Processor binary
if [ ! -x "$PROCESSOR_BIN" ]; then
	echo "[start] Processor binary not found, building..."
	(cd "$ROOT/processor" && go build -o poracle-processor ./cmd/processor) || fail "Failed to build processor"
	echo "[start] Processor built"
fi

# Node modules
if [ ! -d "$ALERTER_DIR/node_modules" ]; then
	echo "[start] Node modules not found, installing..."
	(cd "$ALERTER_DIR" && npm ci) || fail "npm ci failed"
	echo "[start] Node modules installed"
fi

# ---- Extract processor listen address from config ----

# Parse host and port from [processor] section
PROC_HOST=$(awk '
	/^\[processor\]/ { in_section=1; next }
	/^\[/ { in_section=0 }
	in_section && /^host/ {
		gsub(/.*=[ \t]*"/, ""); gsub(/".*/, ""); print; exit
	}
' "$CONFIG_FILE")
PROC_PORT=$(awk '
	/^\[processor\]/ { in_section=1; next }
	/^\[/ { in_section=0 }
	in_section && /^port/ {
		gsub(/.*=[ \t]*/, ""); gsub(/[^0-9].*/, ""); print; exit
	}
' "$CONFIG_FILE")
PROC_HOST="${PROC_HOST:-0.0.0.0}"
PROC_PORT="${PROC_PORT:-3030}"

# For health check, replace 0.0.0.0 with localhost
HEALTH_HOST="$PROC_HOST"
[ "$HEALTH_HOST" = "0.0.0.0" ] && HEALTH_HOST="127.0.0.1"
HEALTH_URL="http://${HEALTH_HOST}:${PROC_PORT}/health"

# ---- Start processor ----

echo "[start] Starting processor..."
"$PROCESSOR_BIN" -basedir "$ROOT" &
PROCESSOR_PID=$!

# Wait for processor health endpoint
echo "[start] Waiting for processor to be ready..."
RETRIES=0
MAX_RETRIES=30
while [ $RETRIES -lt $MAX_RETRIES ]; do
	if curl -sf "$HEALTH_URL" >/dev/null 2>&1; then
		echo "[start] Processor ready"
		break
	fi
	# Check processor hasn't crashed
	if ! kill -0 "$PROCESSOR_PID" 2>/dev/null; then
		fail "Processor exited unexpectedly"
	fi
	RETRIES=$((RETRIES + 1))
	sleep 1
done

if [ $RETRIES -eq $MAX_RETRIES ]; then
	fail "Processor did not become ready within ${MAX_RETRIES}s"
fi

# ---- Start alerter ----

echo "[start] Starting alerter..."
cd "$ALERTER_DIR"
node src/app.js &
ALERTER_PID=$!
cd "$ROOT"

echo "[start] Both components running (processor=$PROCESSOR_PID, alerter=$ALERTER_PID)"

# ---- Monitor ----

while true; do
	if ! kill -0 "$PROCESSOR_PID" 2>/dev/null; then
		echo "[start] Processor exited unexpectedly" >&2
		[ -n "$ALERTER_PID" ] && kill "$ALERTER_PID" 2>/dev/null
		exit 1
	fi
	if ! kill -0 "$ALERTER_PID" 2>/dev/null; then
		echo "[start] Alerter exited unexpectedly" >&2
		[ -n "$PROCESSOR_PID" ] && kill "$PROCESSOR_PID" 2>/dev/null
		exit 1
	fi
	sleep 2
done
