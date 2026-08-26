---
name: kitup
description: Use when integrating the kitup SDK into a CLI that ships a bundled Agent Skill and needs to install it on local agent hosts. Also use when wiring install flags, optional status or uninstall, or the Go Cobra adapter.
---

# Kitup

Use kitup as a producer-side SDK. The embedding CLI owns the bundled skill and command names. kitup owns host resolution, skill validation, copy/update/uninstall behavior, `.kitup.json` metadata, conflict safety, and structured reports.

Call the SDK with:

- `appId`: stable id for the embedding CLI
- `skillBundle`: bundled skill directory tree source containing `SKILL.md`
- `scope`: `user` or `project`
- `agents`: explicit host ids, `auto`, or all supported hosts

## CLI surface

Recommended user-facing command:

```bash
mycli skill install
mycli skill install --dry-run
```

Standard install flags: `--scope`, repeatable `--agent`, `--dry-run`, `--yes` / `-y`, `--force`.

Do not add `plan`, `update`, or `upgrade` subcommands. `--dry-run` is the plan. Re-running install updates kitup-owned copies of the same `appId`. `planBundledSkill` and `updateBundledSkill` are that same install path, not extra commands.

Optional `status` and `uninstall` commands call `statusBundledSkill` and `uninstallBundledSkill`. Uninstall writes immediately; require `--yes` when stdin is not a TTY, and confirm in TTY mode. There is no uninstall force mode. Only the Go Cobra adapter ships these commands; other languages wire their own shell.

## Wiring

For user-facing install, parse flags with `parseInstallFlags` and call `runBundledSkillInstall` with `promptScope: true`. Set `stdinTTY` from the process terminal. Map exits with `installFlagError` and `installWorkflowError`.

For scripts or tests that already know scope and agents, call `installBundledSkill` directly.

Treat conflicts as stop conditions unless the caller passed explicit `--force`.
