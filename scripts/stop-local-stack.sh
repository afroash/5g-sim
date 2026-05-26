#!/usr/bin/env bash
# Stop processes started by run-local-stack.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PID_FILE="${STACK_DIR:-$REPO_ROOT/.local-stack}/pids"

if [[ ! -f "$PID_FILE" ]]; then
  echo "No PID file at $PID_FILE — nothing to stop."
  exit 0
fi

while read -r pid; do
  [[ -z "$pid" ]] && continue
  if kill -0 "$pid" 2>/dev/null; then
    echo "Stopping PID $pid"
    kill "$pid" 2>/dev/null || true
  fi
done <"$PID_FILE"

sleep 1
while read -r pid; do
  [[ -z "$pid" ]] && continue
  kill -9 "$pid" 2>/dev/null || true
done <"$PID_FILE"

rm -f "$PID_FILE"
echo "Local stack stopped."
