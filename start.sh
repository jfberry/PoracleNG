#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
PROCESSOR_BIN="$ROOT/processor/poracle-processor"
CONFIG_FILE="$ROOT/config/config.toml"

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

# ---- Run processor ----

echo "[start] Starting processor..."
exec "$PROCESSOR_BIN" -basedir "$ROOT"
