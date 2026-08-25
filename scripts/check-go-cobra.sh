#!/bin/sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

mkdir -p "$tmp/modules"
tar -C "$root" -cf "$tmp/go.tar" go
tar -C "$root" -cf "$tmp/go-cobra.tar" go-cobra
tar -C "$tmp/modules" -xf "$tmp/go.tar"
tar -C "$tmp/modules" -xf "$tmp/go-cobra.tar"

cd "$tmp/modules/go-cobra"
go mod edit "-replace=github.com/lathe-cli/kitup/go=$tmp/modules/go"
GOWORK=off go test ./...

echo "ok: Go Cobra adapter passes against the local core module"
