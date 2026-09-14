#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=${ITERAUTHOR_VERSION:-0.2.0-test}
mkdir -p dist
for target in linux-amd64 linux-arm64 darwin-arm64; do
    platform=${target%-*}
    arch=${target#*-}
    printf 'Building %s\n' "$target"
    CGO_ENABLED=0 GOOS="$platform" GOARCH="$arch" go build -trimpath -ldflags="-s -w -X main.version=$version" -o "dist/iterauthor-$target" ./cmd/iterauthor
    package="dist/iterauthor-$version-$target"
    mkdir -p "$package/docs"
    cp "dist/iterauthor-$target" "$package/iterauthor"
    cp README.md THIRD_PARTY_NOTICES.txt "$package/"
    cp docs/*.md "$package/docs/"
    tar -czf "$package.tar.gz" -C dist "iterauthor-$version-$target"
done
if command -v sha256sum >/dev/null 2>&1; then
    (cd dist && sha256sum iterauthor-linux-amd64 iterauthor-linux-arm64 iterauthor-darwin-arm64 ./iterauthor-"$version"-*.tar.gz > SHA256SUMS)
else
    (cd dist && shasum -a 256 iterauthor-linux-amd64 iterauthor-linux-arm64 iterauthor-darwin-arm64 ./iterauthor-"$version"-*.tar.gz > SHA256SUMS)
fi
