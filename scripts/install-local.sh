#!/usr/bin/env bash
set -euo pipefail

echo "Building and installing Praetor..."

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

go build -o "$tmp" ./cmd/praetor
sudo install -m 755 "$tmp" /usr/local/bin/praetor

echo "Praetor installed: $(command -v praetor)"

