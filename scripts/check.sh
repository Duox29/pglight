#!/usr/bin/env bash
# Quality gate for pglight (see AGENTS.md §1). Fails on first red check.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== gofmt =="
OUT="$(gofmt -l main.go internal/)"
if [ -n "$OUT" ]; then echo "gofmt dirty:"; echo "$OUT"; exit 1; fi

echo "== go vet =="
go vet ./...

echo "== go test =="
go test ./...

echo "== go build =="
go build -o /tmp/pglight-check .

echo "== tsc =="
(cd web && npx tsc --noEmit)

echo "== eslint =="
(cd web && npx eslint src)

echo "ALL CHECKS PASSED"
