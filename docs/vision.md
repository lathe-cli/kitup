# kitup Vision

## The Problem

Every modern developer CLI is becoming agent-facing.

A CLI is no longer just a binary with flags. It often needs to teach coding agents how to use it: which commands are safe, which workflows matter, where examples live, what should not be touched, and how the tool fits into a project.

Agent Skills are the right shape for that knowledge. They are file-based, inspectable, easy to bundle, and compatible with progressive disclosure.

The broken part is installation.

Each coding agent has its own skill directory. Each host has its own project-level and user-level conventions. Some hosts use `.agents/skills`, some use `.claude/skills`, some use `~/.codex/skills`, and new hosts keep appearing.

If 1,000 CLI projects each write their own skill installer, and 100 agent hosts exist, the ecosystem creates 100,000 redundant adapter decisions. Most of them will be incomplete, stale, unsafe, or subtly incompatible.

`kitup` exists to remove that multiplication.

## What kitup Is

`kitup` is the shared installer SDK for bundled and public GitHub Agent Skills.

It lets a CLI author say:

```text
Here is my skill directory.
Install or update it for the user's local agent hosts.
Do not overwrite anything I do not own.
Tell me exactly what happened.
```

The SDK handles the boring parts:

- detecting installed agent hosts
- resolving user and project skill directories
- validating `SKILL.md`
- copying skill bundle files
- writing ownership metadata
- updating changed skills
- skipping unchanged skills
- refusing unsafe overwrite conflicts
- returning a structured install report

That is all v0.1 needs to do.

## What kitup Is Not

`kitup` is not a marketplace or remote registry.

Marketplace tools serve users who want to discover and install arbitrary skills. `kitup` serves CLI authors who already ship a skill with their product.

The difference matters:

```text
Marketplace flow:
  user wants skill X from source Y

kitup flow:
  CLI author owns skill X
  user runs mycli skill install
  kitup installs that configured skill bundle into local agent hosts
```

Keeping that boundary small makes the SDK reliable enough for other projects to embed.

## Why the Name Fits

To "kit up" is to equip a tool or person with the gear needed for the job.

That is exactly the product.

A CLI already exists. It already has a purpose. `kitup` equips it for the LLM-agent environment by placing its skill where local agents can use it.

The name is short, typed as a command, and does not leak implementation detail.

## The Product Shape

The ideal integration is almost invisible.

```bash
mycli skill install
```

Under the hood:

```text
mycli -> kitup SDK -> local agent hosts -> installed SKILL.md
```

A CLI author should not need to know where Codex, Claude Code, Cursor, OpenCode, Gemini CLI, or future hosts keep skills. They should only choose:

- app id
- skill bundle
- user or project scope
- automatic or explicit agent selection

The SDK should do the rest and report:

```text
installed: codex, claude-code
updated: opencode
skipped: cursor unchanged
conflict: gemini-cli has an unmanaged skill with the same name
```

## Design Philosophy

### Files over services

Skills are files. Installing them should not require a daemon, service, MCP server, or remote account.

### Data over branching code

Most host support is path data. Put it in a shared host adapter database. Keep the installer code generic.

### Native over clever

TypeScript, Go, Rust, and Python users should get native SDKs. Avoid forcing a Go CLI to shell out to Node, or a Rust CLI to ship a JS runtime.

### Copy over symlink

Copies survive package manager cleanup, binary relocation, npm cache pruning, Cargo target cleanup, Homebrew upgrades, and app bundle moves. Symlink mode can exist later for development, but copy is the stable default.

### Conflict over clobber

If a user already has a skill with the same name and no kitup ownership metadata, the correct default is to stop.

### Report over print

Embedding CLIs need structured results. They can decide how much output to show.

## The Agent Bench Slot

`kitup` sits in the Bridge / Craft layer of The Agent Bench.

It is not the memory layer. It is not the workflow runner. It is the fitting that lets every CLI carry its agent-facing instructions into the hosts the user already has.

In the broader story:

- `lathe` shapes APIs into agent-safe CLIs.
- `gmc` runs parallel agents.
- `barrow` preserves source-backed memory.
- `recall` finds prior sessions.
- `kitup` equips CLI tools with installable agent skills.

It is infrastructure, but infrastructure that removes real product friction.

## Long-Term Direction

The first release should prove safe producer-side skill installation.

After that, `kitup` can grow in three directions:

### 1. Adapter authority

Become the most accurate open host path matrix for agent skills.

The matrix should track:

- host id
- display name
- user skill path
- project skill path
- detection rules
- support status
- known post-install notes

### 2. Producer workflows

Help CLI authors build better bundled skills:

- validate frontmatter
- check missing references
- warn on oversized instructions
- compute install hash
- preview target writes
- generate minimal install command examples

### 3. Optional package-manager layer

Public GitHub directory bundles are a producer-owned bundle input, not a package-manager layer. Only after the producer-side SDK is stable, consider broader package-manager behavior:

- private source authentication
- install from tarball
- registry index
- version pinning
- lockfile restore

This is intentionally later. The core SDK must not be delayed by marketplace ambition.

## Open Source Posture

`kitup` should be boring to contribute to.

Most contributions should be one of:

- add or correct a host adapter entry
- add a fixture case
- fix a language implementation to match the shared behavior
- improve validation

The project should avoid custom ideology where a table and a test are enough.

## Success Story

A new CLI author should be able to read one page, copy one code snippet, and ship:

```bash
mycli skill install
```

Their users should not care which agent they use.

Their agents should get the right skill.

The CLI author should never maintain a list of 100 host-specific install paths.

That is the whole product.
