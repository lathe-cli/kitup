---
name: kitup
description: Use when integrating kitup into a CLI that ships bundled Agent Skills and needs to install, update, plan, or uninstall those skills across local agent hosts.
---

# Kitup

Use kitup as a producer-side SDK. The embedding CLI owns the bundled skill; kitup owns host resolution, skill validation, copy/update/uninstall behavior, `.kitup.json` metadata, conflict safety, and structured reports.

Call the SDK with:

- `appId`: stable id for the embedding CLI
- `skillBundle`: bundled skill directory tree source containing `SKILL.md`
- `scope`: `user` or `project`
- `agents`: explicit host ids, `auto`, or all supported hosts

Prefer `plan` before install when showing users what will change. Treat conflicts as stop conditions unless the SDK has explicit tested support for the desired override.
