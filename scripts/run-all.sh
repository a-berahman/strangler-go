#!/usr/bin/env bash
# Starts legacy, newsvc and router together. Ctrl+C stops all three.
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p bin
go build -o bin/ ./cmd/...

trap 'kill 0' EXIT
./bin/legacy -addr :8081 &
./bin/newsvc -addr :8082 &
sleep 0.5
./bin/router -config router.json -debug &
wait
