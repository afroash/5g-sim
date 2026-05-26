#!/usr/bin/env bash
# Start all core NFs + UE supervisor for local standalone dev (one command).
#
# Usage (from repo root):
#   ./scripts/run-local-stack.sh
#   ./scripts/stop-local-stack.sh
#
# Then in the same or another terminal:
#   go run ./cmd/observatory
#
# Logs: .local-stack/logs/*.log

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

STACK_DIR="${STACK_DIR:-$REPO_ROOT/.local-stack}"
LOG_DIR="$STACK_DIR/logs"
PID_FILE="$STACK_DIR/pids"
mkdir -p "$LOG_DIR"

export OBSERVATORY_URL="${OBSERVATORY_URL:-http://127.0.0.1:9090}"

if [[ -f "$PID_FILE" ]] && [[ -s "$PID_FILE" ]]; then
  echo "PID file exists ($PID_FILE). Run ./scripts/stop-local-stack.sh first." >&2
  exit 1
fi

: >"$PID_FILE"

wait_http() {
  local url=$1
  local label=${2:-$url}
  local max=${3:-45}
  echo -n "  waiting for $label"
  for _ in $(seq 1 "$max"); do
    if curl -sf "$url" >/dev/null 2>&1; then
      echo " ok"
      return 0
    fi
    echo -n "."
    sleep 1
  done
  echo " TIMEOUT" >&2
  return 1
}

start_nf() {
  local name=$1
  shift
  echo ">>> $name"
  go run "$@" >>"$LOG_DIR/$name.log" 2>&1 &
  echo "$!" >>"$PID_FILE"
}

echo "============================================"
echo " 5g-sim local stack"
echo " OBSERVATORY_URL=$OBSERVATORY_URL"
echo " logs: $LOG_DIR"
echo "============================================"
echo ""

start_nf nrf     ./cmd/nrf
wait_http "http://127.0.0.1:8000/health" "NRF"

start_nf udm     ./cmd/udm
wait_http "http://127.0.0.1:8004/health" "UDM"

start_nf smf     ./cmd/smf
wait_http "http://127.0.0.1:8001/health" "SMF"

start_nf amf     ./cmd/amf
wait_http "http://127.0.0.1:8090/health" "AMF"

start_nf upf     ./cmd/upf
wait_http "http://127.0.0.1:8002/health" "UPF"

start_nf gnb     ./cmd/gnb
wait_http "http://127.0.0.1:8003/health" "gNB"

start_nf ue-supervisor ./cmd/ue
wait_http "http://127.0.0.1:9080/health" "UE supervisor"

echo ""
echo "Core stack is up ($(wc -l <"$PID_FILE") processes)."
echo ""
echo "  Observatory GUI:  go run ./cmd/observatory"
echo "                    open http://127.0.0.1:9090"
echo ""
echo "  Tail logs:        tail -f .local-stack/logs/*.log"
echo "  Stop everything:  ./scripts/stop-local-stack.sh"
echo ""
