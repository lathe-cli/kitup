#!/bin/sh
set -eu

version="${1:?usage: scripts/smoke-release.sh <version>}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

retry() {
	name="$1"
	shift
	attempt=1
	while ! "$@"; do
		if [ "$attempt" -ge 6 ]; then
			echo "$name failed after $attempt attempts" >&2
			return 1
		fi
		attempt=$((attempt + 1))
		sleep 10
	done
}

smoke_npm() {
	dir="$(mktemp -d "$tmp/npm.XXXXXX")"
	cd "$dir"
	npm init -y >/dev/null
	npm install "@kitup/sdk@$version" >/dev/null
	node --input-type=module -e 'import { loadHostSpec } from "@kitup/sdk"; const spec = await loadHostSpec(); if (spec.hosts.length !== 72) throw new Error(`expected 72 hosts, got ${spec.hosts.length}`); console.log(`npm ok: ${spec.hosts.length}`);'
}

smoke_rust() {
	dir="$(mktemp -d "$tmp/rust.XXXXXX")"
	cargo init --quiet --bin --name kitup-release-smoke "$dir"
	cd "$dir"
	printf '\nkitup = "%s"\n' "$version" >> Cargo.toml
	cat > src/main.rs <<'RS'
fn main() {
    let hosts = kitup::load_host_spec(None).expect("load host spec");
    assert_eq!(hosts.len(), 72);
    println!("rust ok: {}", hosts.len());
}
RS
	cargo run --quiet
}

smoke_go() {
	dir="$(mktemp -d "$tmp/go.XXXXXX")"
	cd "$dir"
	go mod init kitup-release-smoke >/dev/null
	go get "github.com/lathe-cli/kitup/go@v$version" >/dev/null
	cat > main.go <<'GO'
package main

import (
	"fmt"

	kitup "github.com/lathe-cli/kitup/go"
)

func main() {
	hosts, err := kitup.LoadHostSpec("")
	if err != nil {
		panic(err)
	}
	if len(hosts) != 72 {
		panic(fmt.Sprintf("expected 72 hosts, got %d", len(hosts)))
	}
	fmt.Printf("go ok: %d\n", len(hosts))
}
GO
	go run .
}

smoke_go_cobra() {
	dir="$(mktemp -d "$tmp/go-cobra.XXXXXX")"
	cd "$dir"
	go mod init kitup-release-smoke-cobra >/dev/null
	go get "github.com/lathe-cli/kitup/go-cobra@v$version" >/dev/null
	test "$(go list -m -f '{{.Version}}' github.com/lathe-cli/kitup/go)" = "v$version"
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
	fmt.Printf("go-cobra ok: %s\n", cmd.Use)
}
GO
	go run .
}

smoke_python() {
	dir="$(mktemp -d "$tmp/python.XXXXXX")"
	cd "$dir"
	uv run --with "kitup==$version" python - <<'PY'
from kitup import load_host_spec

spec = load_host_spec()
assert len(spec.hosts) == 72
print(f"python ok: {len(spec.hosts)}")
PY
}

retry npm smoke_npm
retry rust smoke_rust
retry go smoke_go
retry go-cobra smoke_go_cobra
retry python smoke_python
