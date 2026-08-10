# Host Adapter Contract

`spec/hosts.json` is the shared host adapter database for every kitup SDK implementation.

Each host entry describes where a local Agent Skill can be installed and how the SDK can decide whether that host is likely present on the machine.

## Path Order

`projectSkillsDirs` and `userSkillsDirs` are ordered.

The first path is the canonical install target for that host. Later paths are compatible discovery roots that the host also scans. SDKs should install to the first path unless a caller explicitly requests another supported path.

Project paths must be relative paths. User paths must be home-relative paths beginning with `~/`.
All adapter paths use `/` separators and non-empty segments; `..`, backslashes, colons, and NUL bytes are invalid.

If multiple selected hosts resolve to the same target directory, SDKs must copy once and associate that installed target with every matching host. Shared roots such as `.agents/skills` are common and should not produce duplicate writes.

## Aliases

`aliases` are accepted input names for one host adapter.

Aliases are for ecosystem compatibility only. SDK result objects should return the canonical `id`, not the alias, unless the caller needs an echo of the original selector.

## Detection

`detect` is only a default selector for `agents: "auto"`.

Detection should check path existence. Entries may be home-relative paths such as `~/.codex` or project-relative paths such as `.replit`.

Detection must not run host binaries, start editors, mutate configuration, or require network access.

Explicit host selection should still resolve install targets even when detection paths are absent.

## Status

- `verified`: confirmed against current official documentation and local product behavior or local filesystem state.
- `documented`: sourced from current official documentation, but not locally exercised.
- `community`: contributed path mapping that still needs host-specific confirmation.
- `experimental`: likely path or early product behavior that needs confirmation before broad claims.

Do not mark a host `verified` only because another host scans its compatibility path.

## Adapter Additions

To add a host:

1. Add or update the host in `spec/hosts.json`.
2. Include at least one project or user skill directory.
3. Put native or recommended paths before compatibility paths.
4. Add or update golden cases when behavior changes.
5. Keep installer behavior data-driven; do not add host-specific branching unless the generic path resolver cannot express the host.

## Verification

After changing `spec/hosts.json`, regenerate host constants and run the parity gate:

```bash
make generate
make check
```

Use `make generate-check` in CI or review workflows to verify generated host constants are current.
