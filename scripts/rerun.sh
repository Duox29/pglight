#!/usr/bin/env bash
# Kill the DbClient Go process currently listening on $PORT (default 8080)
# and rerun a fresh build in the background.
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${PORT:-8080}"
PID="$(ss -ltnp 2>/dev/null | grep -E ":${PORT}[[:space:]]" | grep -oP 'pid=\K[0-9]+' | head -n 1 || true)"

if [ -n "${PID:-}" ]; then
  echo "Killing pid $PID on :$PORT..."
  kill "$PID" 2>/dev/null || true
  for _ in $(seq 1 10); do
    kill -0 "$PID" 2>/dev/null || break
    sleep 1
  done
  kill -9 "$PID" 2>/dev/null || true
else
  echo "Nothing listening on :$PORT."
fi

go build -o /tmp/dbclient .
PORT="$PORT" nohup /tmp/dbclient >/tmp/dbclient.log 2>&1 &
echo "DbClient rerun on :$PORT (pid $!). Log: /tmp/dbclient.log"
