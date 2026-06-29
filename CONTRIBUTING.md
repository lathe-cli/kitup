# Contributing

`kitup` is a producer-side SDK for CLI authors who ship bundled Agent Skills.

Keep changes inside the v0.1 boundary:

- validate local skill directories
- detect agent hosts
- resolve user and project skill directories
- copy, update, and uninstall kitup-owned installs
- preserve `.kitup.json` ownership safety
- return structured reports
- keep TypeScript, Go, and Rust behavior aligned through golden cases

Do not add marketplace, registry, remote install, script execution, MCP server, GUI, or agent runtime behavior unless the product boundary changes first.

## Setup

```bash
make hooks
make check
```

## Common Commands

```bash
make generate        # refresh generated host constants
make generate-check  # verify generated host constants are current
make check           # full parity and example gate
make fmt             # format TypeScript, Go, and Rust files
make clean           # remove local build outputs
```

## Host Adapter Changes

Host support is data-first.

- Edit `spec/hosts.json`.
- Run `make generate`.
- Add or update a golden case when observable behavior changes.
- Do not edit generated files by hand:
  - `ts/src/hosts.generated.ts`
  - `go/hosts_gen.go`
  - `rust/src/hosts_generated.rs`

## SDK Behavior Changes

Every installer behavior needs a golden case in `testdata/cases`.

Before opening a pull request:

```bash
make check
```

The check must pass locally before claiming parity.

## Release Changes

Do not publish packages from a pull request.

Release tags are cut from `main` after `make check` passes. The release workflow publishes:

- `@kitup/sdk`
- `kitup` on crates.io
- `github.com/samzong/kitup/go` through the `go/vX.Y.Z` tag
- GitHub Release notes

