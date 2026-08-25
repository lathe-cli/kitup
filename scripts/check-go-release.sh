#!/bin/sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
version="$(node -p 'require(process.argv[1]).version' "$root/ts/package.json")"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

mkdir -p "$tmp/modules"
tar -C "$root" -cf "$tmp/go.tar" go
tar -C "$root" -cf "$tmp/go-cobra.tar" go-cobra
tar -C "$tmp/modules" -xf "$tmp/go.tar"
tar -C "$tmp/modules" -xf "$tmp/go-cobra.tar"

(
	cd "$tmp/modules/go"
	GOWORK=off go test ./...
)
(
	cd "$tmp/modules/go-cobra"
	go mod edit "-replace=github.com/lathe-cli/kitup/go=$tmp/modules/go"
	GOWORK=off go test ./...
	go mod edit -dropreplace=github.com/lathe-cli/kitup/go
)

consumer="$tmp/consumer"
mkdir -p "$consumer"
cd "$consumer"
go mod init kitup-release-consumer >/dev/null
go mod edit \
	"-require=github.com/lathe-cli/kitup/go@v$version" \
	"-require=github.com/lathe-cli/kitup/go-cobra@v$version" \
	"-replace=github.com/lathe-cli/kitup/go=$tmp/modules/go" \
	"-replace=github.com/lathe-cli/kitup/go-cobra=$tmp/modules/go-cobra"

cat > main.go <<'GO'
package main

import (
	"fmt"

	kitup "github.com/lathe-cli/kitup/go"
	kitupcobra "github.com/lathe-cli/kitup/go-cobra"
)

func main() {
	cmd := kitupcobra.NewSkillCommand(kitupcobra.Options{})
	if cmd.Use != kitup.InstallUX.SkillUse {
		panic(fmt.Sprintf("expected %s, got %s", kitup.InstallUX.SkillUse, cmd.Use))
	}
}
GO

GOWORK=off go mod tidy
GOWORK=off go list -m all >/dev/null
GOWORK=off go test ./...
GOWORK=off go build .

test "$(GOWORK=off go list -m -f '{{.Version}}' github.com/lathe-cli/kitup/go)" = "v$version"
test "$(GOWORK=off go list -m -f '{{.Version}}' github.com/lathe-cli/kitup/go-cobra)" = "v$version"

echo "ok: packaged Go modules are self-contained at v$version"
