#!/usr/bin/env bash
# Usage: scripts/release-build.sh v0.1.0
# Builds release archives and checksums into dist/.
set -euo pipefail
tag="${1:?usage: $0 TAG}"
rm -rf dist && mkdir -p dist
ldflags="-s -w -buildid= -X github.com/zahidhasann88/meshlink/internal/app.Version=${tag}"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	os="${target%/*}" arch="${target#*/}" ext=""
	[ "$os" = windows ] && ext=".exe"
	name="meshlink_${tag#v}_${os}_${arch}"
	mkdir -p "dist/$name"
	for cmd in meshlink meshlink-registry; do
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "dist/$name/$cmd$ext" "./cmd/$cmd"
	done
	cp README.md LICENSE "dist/$name/"
	if [ "$os" = windows ]; then
		(cd dist && zip -qr "$name.zip" "$name")
	else
		tar -C dist -czf "dist/$name.tar.gz" "$name"
	fi
	rm -rf "dist/$name"
done
(cd dist && sha256sum -- *.tar.gz *.zip > checksums.txt)
ls -l dist
