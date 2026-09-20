#!/usr/bin/env bash
# Full pipeline: build frontend (Vite dist) + restart backend from source.
# Usage: ./scripts/rebuild.sh            # port 8080
#        PORT=9001 ./scripts/rebuild.sh  # custom port
# Windows: run from Git Bash. Also tolerates WSL bash, where Windows binaries
# (go.exe, netstat.exe, taskkill.exe) need explicit resolution.
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${PORT:-8080}"

# Resolve a Windows tool from Git Bash or WSL bash (no PATHEXT lookup there).
resolve() {
  if command -v "$1" >/dev/null 2>&1; then echo "$1"; return 0; fi
  if command -v "$1.exe" >/dev/null 2>&1; then echo "$1.exe"; return 0; fi
  for d in "/mnt/c/Program Files/Go/bin" "/c/Program Files/Go/bin" "C:/Program Files/Go/bin" \
           "/mnt/c/Program Files/nodejs" "/c/Program Files/nodejs" "C:/Program Files/nodejs"; do
    if [ -x "$d/$1.exe" ]; then echo "$d/$1.exe"; return 0; fi
  done
  echo "$1" # fall through to the normal "command not found" error
}
GO="$(resolve go)"
NPM="$(resolve npm)"
SYS32=""
if [ -x /mnt/c/Windows/System32/netstat.exe ]; then
  SYS32=/mnt/c/Windows/System32
elif [ -x /c/Windows/System32/netstat.exe ]; then
  SYS32=/c/Windows/System32
fi
if [ -z "$SYS32" ]; then
  SYS32="$(dirname "$(resolve taskkill)" 2>/dev/null || true)"
fi

echo "== frontend ($NPM) =="
if [ ! -d web/node_modules ]; then
  echo "node_modules missing, installing..."
  (cd web && "$NPM" install)
fi
(cd web && "$NPM" run build)

echo "== backend: go run . (from source, no binary) =="
echo "== restart :$PORT =="
PID=""
if command -v ss >/dev/null 2>&1; then
  PID="$(ss -ltnp 2>/dev/null | grep -E ":${PORT}[[:space:]]" | grep -oP 'pid=\K[0-9]+' | head -n 1 || true)"
else
  NETSTAT="${SYS32:+$SYS32/}netstat"
  TASKKILL="${SYS32:+$SYS32/}taskkill"
  PID="$("$NETSTAT" -ano 2>/dev/null | grep -E "TCP.*:${PORT}[[:space:]]" | grep LISTENING | awk '{print $NF}' | head -n 1 || true)"
fi

if [ -n "${PID:-}" ]; then
  echo "Stopping pid $PID..."
  kill "$PID" 2>/dev/null || "${TASKKILL:-taskkill}" //F //PID "$PID" 2>/dev/null || true
  for _ in $(seq 1 10); do
    kill -0 "$PID" 2>/dev/null || break
    sleep 1
  done
  kill -9 "$PID" 2>/dev/null || "${TASKKILL:-taskkill}" //F //PID "$PID" 2>/dev/null || true
else
  echo "Nothing listening on :$PORT."
fi

PORT="$PORT" nohup "$GO" run . >/tmp/pglight.log 2>&1 &
echo "pglight (go run .) on :$PORT (pid $!). Log: /tmp/pglight.log"
