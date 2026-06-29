# kitup

Shared installer SDK for bundled Agent Skills.

CLI authors ship a skill with their tool. `kitup` detects local coding-agent hosts, validates `SKILL.md`, copies the bundled skill into the right host directories, and writes ownership metadata so updates stay safe.

```text
mycli skill install
  -> kitup SDK
  -> local agent hosts
  -> installed SKILL.md + .kitup.json
```

## What it does

- detect installed agent hosts
- resolve user and project skill directories
- validate bundled skills
- copy, update, and uninstall kitup-owned installs
- refuse unsafe overwrite conflicts
- return structured install reports

## What it is not

- not a skill marketplace
- not a remote registry
- not a replacement for user-facing skill discovery tools

## Repository layout

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

## Check

```bash
make check
```

## Git Hooks

```bash
make hooks
```

## Generate

```bash
make generate
```

## Release

Push a `vX.Y.Z` tag to publish npm, crates.io, the `go/vX.Y.Z` module tag, and the GitHub Release.

See [docs/RELEASE.md](docs/RELEASE.md).

Required setup:

- npm trusted publishing for `.github/workflows/release.yml`
- `CARGO_REGISTRY_TOKEN` GitHub secret

## Acknowledgments

Host adapter coverage builds on prior work from [GitHub CLI `gh skill`](https://cli.github.com/manual/gh_skill_install) and [`npx skills` / skills.sh](https://github.com/vercel-labs/skills).
