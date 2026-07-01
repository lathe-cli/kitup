# Contributing

`kitup` is a producer-side SDK for CLI authors who ship bundled Agent Skills.

Keep changes inside the v0.1 boundary:

- validate local skill directories
- validate embedded skill directory trees
- resolve public GitHub skill bundle directory trees
- detect agent hosts
- resolve safe CLI install selection
- run the safe CLI install workflow
- resolve user and project skill directories
- copy, update, and uninstall kitup-owned installs
- preserve `.kitup.json` ownership safety
- return structured reports
- keep TypeScript, Go, and Rust behavior aligned through golden cases

Do not add marketplace, registry, private remote install, custom provider, script execution, MCP server, GUI, or agent runtime behavior unless the product boundary changes first.

## Setup

```bash
make hooks
make check
```

## Repository Layout

```text
spec/hosts.json          host adapter database
spec/hosts.schema.json   host spec schema
testdata/                skill fixtures and golden cases
docs/                    product and contract docs
examples/                minimal SDK consumer CLIs
Makefile                 root developer commands
scripts/check.mjs        spec, fixture, and SDK parity validation
scripts/sync-hosts.mjs   generated host constants
```

TypeScript, Go, and Rust SDKs live in `ts/`, `go/`, and `rust/`.

## Common Commands

```bash
make generate        # refresh generated host constants
make generate-check  # verify generated host constants are current
make check           # full parity and example gate
make fmt             # format TypeScript, Go, and Rust files
make clean           # remove local build outputs
```

## Verification

Run the full parity gate before opening a pull request:

```bash
make check
```

This validates the shared spec, fixtures, generated host constants, TypeScript, Go, Rust, and examples.

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

Every bundle input, selection, or installer behavior needs a golden case in `testdata/cases`.

Before opening a pull request:

```bash
make check
```

The check must pass locally before claiming parity.

## Release

Do not publish packages from a pull request.

Use `make release-patch`, `make release-minor`, or `make release-major` from a clean, up-to-date `main` branch to create the release branch and version commit. Open and merge the release PR manually, then tag `main` manually. The release workflow publishes:

- `@kitup/sdk`
- `kitup` on PyPI
- `kitup` on crates.io
- `github.com/lathe-cli/kitup/go` through the `go/vX.Y.Z` tag
- `github.com/lathe-cli/kitup/go-cobra` through the `go-cobra/vX.Y.Z` tag
- GitHub Release notes

See [docs/RELEASE.md](docs/RELEASE.md) for the release flow, first npm release recovery, and public install smoke check.
