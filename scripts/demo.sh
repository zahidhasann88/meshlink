#!/usr/bin/env bash
set -euo pipefail
export MSYS_NO_PATHCONV=1
rm -rf dist/demo-bin
for cmd in meshlink; do
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/demo-bin/$cmd ./cmd/$cmd
done
docker run --rm -v "$(pwd -W 2>/dev/null || pwd):/vhs" ghcr.io/charmbracelet/vhs:v0.10.0 docs/demo.tape
rm -rf dist/demo-bin
ls -l docs/demo.gif
