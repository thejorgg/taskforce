#!/bin/sh
set -e

BIN="${INSTALL_DIR:-$HOME/.local/bin}/taskforce"

echo "building taskforce..."
go build -o taskforce ./cmd/taskforce

echo "installing to $BIN..."
mkdir -p "$(dirname "$BIN")"
cp taskforce "$BIN"
chmod +x "$BIN"

rm -f taskforce

echo "done · run 'taskforce' to start"
