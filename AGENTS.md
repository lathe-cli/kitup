# AGENTS.md

## Project Boundary

`kitup` is a producer-side SDK for CLI authors who ship bundled Agent Skills with their tools.

The v0.1 product is deliberately small:

- validate a local skill directory
- validate an embedded bundled skill directory tree
- detect local agent hosts
- resolve safe CLI install selection before writes
- run the safe CLI install workflow for bundled skills
- resolve user and project skill directories
- copy bundled skill files
- write `.kitup.json` ownership metadata
- update or uninstall only kitup-owned installs
- refuse unsafe overwrite conflicts
- return structured reports
- enforce parity through shared golden cases

Do not turn kitup into a marketplace, registry client, package manager, MCP server, GUI, agent runtime integration layer, or arbitrary skill installer. Remote installs, GitHub source installs, marketplace search, plugin systems, `AGENTS.md` mutation, script execution, and symlink-first workflows are outside v0.1 unless this boundary and golden cases are changed first.

## Product Promise

One bundled skill, one SDK call, many agent hosts.

The embedding CLI owns the skill and command shell. `kitup` owns the boring installer layer: host path lookup, detection, selection prompts, summary confirmation, validation, copy/update/uninstall semantics, metadata, conflicts, and reports.

Keep the developer experience boring. A CLI author should only need to provide:

- `appId`
- `skillBundle`
- `scope`
- `agents`

Everything else must be derived from shared data and deterministic installer behavior.

## Source Of Truth

Use the live repository state, not memory or aspiration.

- `spec/hosts.json` is the host adapter database.
- `spec/*.schema.json` defines accepted shared data shape.
- `testdata/cases/*.json` defines cross-language behavior.
- Generated host constants in `ts/`, `go/`, and `rust/` come from `spec/hosts.json`; never edit them by hand.
- `scripts/sync-hosts.mjs` refreshes and checks generated host constants.
- `scripts/check.mjs` is the current parity gate.
- `docs/host-adapter-contract.md` explains host adapter semantics.
- `README.md` documents the public product boundary; code, schemas, and golden cases are the enforcement layer.

When docs and executable checks disagree, fix the source of truth or surface the conflict before implementing behavior.

## Drift Guards

Before non-trivial work, classify the request as one of:

- v0.1 bundled-skill source, selection, and installer behavior
- host adapter data correction
- fixture or parity improvement
- documentation of existing behavior
- post-v0.1 scope

Only the first four are normal work. For post-v0.1 scope, do not implement product surface until the boundary is explicitly changed in docs and golden cases.

Every new source, selection, or installer behavior needs a golden case. A behavior that cannot be expressed in `testdata/cases` is not ready to become SDK behavior.

Do not add speculative abstractions for future registries, package managers, remote sources, marketplace metadata, auth, or dependency resolution. Build the shallowest implementation that satisfies the existing case matrix.

## Host Adapter Rules

Host support is data-first.

- Put paths and aliases in `spec/hosts.json` whenever possible.
- Do not add host-specific branching unless the generic resolver cannot express the host.
- `projectSkillsDirs` and `userSkillsDirs` are ordered; the first path is the canonical install target.
- Project paths must be relative. User paths must start with `~/`.
- A host may be project-only or user-only.
- Multiple selected hosts may resolve to the same target directory; copy once and report all matching hosts.
- Aliases are input compatibility only; result objects should use canonical host ids.
- `detect` is only for `agents: "auto"`.
- Detection checks path existence only. It must not run binaries, start editors, mutate config, require network access, or infer support from unrelated side effects.
- Explicit host selection must resolve install targets even when detection paths are absent.
- Do not mark a host `verified` unless current docs and local product behavior or local filesystem evidence support it.

## Installer Semantics

Copy by default. Do not execute anything in a skill directory.

Ownership is controlled by `.kitup.json`:

- missing target: install
- same `appId` and same hash: skip
- same `appId` and different hash: update
- different `appId`: conflict
- no `.kitup.json`: conflict

Conflict is the safe default. `force` and `adopt` must stay explicit, tested, and narrow.

Content hashes must be deterministic across TypeScript, Go, and Rust. Hash bundled skill files by sorted relative path and bytes, excluding `.kitup.json` and transient files.

Reports are API contracts. Return structured `installed`, `updated`, `skipped`, `conflicts`, and `errors` data instead of relying on logs.

Bundled skill sources are directory trees. `SKILL.md` must live at the bundle root, but references, scripts, assets, and other regular files are part of the same source and must be validated, hashed, and copied as a tree.

## Multi-Language Parity

TypeScript, Go, and Rust should be native SDKs that consume the same shared spec and fixtures.

Do not replace this with a single binary core, cross-language FFI, shelling out to another runtime, or generated behavior unless the project direction is explicitly changed.

Implementation order can differ by language. Observable behavior must match the golden cases.

## Verification

Run the narrowest relevant check before claiming completion.

At minimum after spec, fixture, SDK, or documentation-adjacent changes:

```bash
node scripts/check.mjs
```

Do not claim a host path, status, SDK behavior, or parity result without fresh evidence from code, fixtures, docs, or local runtime output.

## Change Discipline

Keep diffs small and behavior-led.

- Prefer changing data and golden cases before changing installer code.
- Prefer one adapter correction over broad host table churn.
- Do not update docs to describe behavior that is not implemented or covered by fixtures.
- Do not widen v0.1 scope to make an isolated task feel complete.
- If a requested change weakens conflict safety, metadata ownership, or parity, stop and surface the trade-off first.
