# kitup

Shared installer SDK for bundled Agent Skills.

CLI authors ship a skill with their tool. `kitup` models that bundled skill as a directory tree, resolves safe agent targets, validates `SKILL.md`, copies the full tree into the right host directories, and writes ownership metadata so updates stay safe.

```text
mycli skill install
  -> kitup SDK
  -> local agent hosts
  -> installed skill tree + .kitup.json
```

## What it does

- detect installed agent hosts
- resolve user and project skill directories
- resolve safe CLI install selection before writing
- validate bundled skills
- install from a local directory or embedded bundle tree
- copy, update, and uninstall kitup-owned installs
- refuse unsafe overwrite conflicts
- return structured install reports

## What it is not

- not a skill marketplace
- not a remote registry
- not a replacement for user-facing skill discovery tools

## Usage

Bundle a skill in your CLI project:

```text
mycli/
  skills/mycli/SKILL.md
```

Your CLI owns the command name and framework shell. `kitup` owns the standard install flags, agent selector mapping, host detection, safe selection policy, summary text, confirmation, workflow exit classification, target paths, bundle validation, copy/update semantics, metadata, and conflicts.

### TypeScript

Install:

```bash
npm install @kitup/sdk
```

Use the workflow API for user-facing install commands:

```ts
import {
  directoryBundle,
  installFlagError,
  installWorkflowError,
  parseInstallFlags,
  runBundledSkillInstall,
} from "@kitup/sdk";

const flags = parseInstallFlags({
  scope: "user",
  agents: ["codex"],
  yes: false,
  dryRun: false,
});
const flagError = installFlagError(flags.errors);
if (flagError) throw flagError;

const result = await runBundledSkillInstall({
  appId: "mycli",
  skillBundle: directoryBundle("./skills/mycli"),
  scope: flags.scope,
  scopeSet: flags.scopeSet,
  promptScope: true,
  agents: flags.agents,
  yes: flags.yes,
  dryRun: flags.dryRun,
});
const workflowError = installWorkflowError(result);
if (workflowError) throw workflowError;
```

For embedded bundles, pass the whole skill tree:

```ts
import { filesBundle, runBundledSkillInstall } from "@kitup/sdk";

await runBundledSkillInstall({
  appId: "mycli",
  skillBundle: filesBundle([
    { path: "SKILL.md", contents: skillMd },
    { path: "references/guide.md", contents: guide },
  ]),
  scope: "user",
  agents: ["codex"],
});
```

### Go

Install:

```bash
go get github.com/samzong/kitup/go
```

Use the workflow API for user-facing install commands:

```go
import (
	"os"

	kitup "github.com/samzong/kitup/go"
)

result, err := kitup.RunBundledSkillInstall(kitup.InstallWorkflowOptions{
	InstallOptions: kitup.InstallOptions{
		AppID:       "mycli",
		SkillBundle: kitup.DirectoryBundle("./skills/mycli"),
		Scope:       kitup.UserScope,
		Agents:      kitup.AutoAgents(),
	},
	Yes: false,
	Out: os.Stdout,
})
```

For `go:embed`, pass the embedded directory tree:

```go
result, err := kitup.RunBundledSkillInstall(kitup.InstallWorkflowOptions{
	InstallOptions: kitup.InstallOptions{
		AppID:       "mycli",
		SkillBundle: kitup.FSBundle(embeddedSkills, "skills/mycli"),
		Scope:       kitup.UserScope,
		Agents:      kitup.ExplicitAgents("codex"),
	},
	Yes: false,
})
```

For Cobra CLIs, use the optional adapter. The adapter does not own installer behavior; it only wires Cobra flags to the core workflow.

```go
import (
	kitup "github.com/samzong/kitup/go"
	kitupcobra "github.com/samzong/kitup/go-cobra"
)

root.AddCommand(kitupcobra.NewSkillCommand(kitupcobra.Options{
	AppID:  "mycli",
	Bundle: kitup.FSBundle(embeddedSkills, "skills/mycli"),
}))
```

### Rust

Install:

```bash
cargo add kitup
```

Use:

Set `stdin_tty` from your CLI's terminal detection.

```rust
let result = kitup::run_bundled_skill_install(&kitup::InstallWorkflowOptions {
    install: kitup::InstallOptions {
        base: kitup::BaseOptions::default(),
        app_id: "mycli".to_string(),
        skill_bundle: kitup::directory_bundle("./skills/mycli"),
        scope: kitup::Scope::User,
        agents: kitup::AgentSelector::Auto,
    },
    yes: false,
    dry_run: false,
    stdin_tty: stdin_tty,
    current_agent: None,
})?;
```

The workflow result contains the selected agents, dry-run plan, final install report, and cancellation state. The final install report contains `installed`, `updated`, `skipped`, `conflicts`, and `errors`.

For lower-level integrations, `InstallBundledSkill` / `installBundledSkill` / `install_bundled_skill` remain primitive copy APIs. Do not wire a user-facing CLI directly to `agents: "auto"` unless you intentionally want primitive auto-detect behavior. Use the shared flag parsing, selector mapping, UX strings, and workflow exit helpers for CLI commands.

For user-facing install commands, missing `--scope` is interactive state, not `user`. Pass `scopeSet` and enable `promptScope` so TTY workflows ask for scope before agent selection. `--yes` uses the configured default scope.

## Docs

- [API](docs/API.md)
- [Contributing](CONTRIBUTING.md)
- [Host adapter contract](docs/host-adapter-contract.md)
- [Release](docs/RELEASE.md)

## Acknowledgments

Host adapter coverage builds on prior work from [GitHub CLI `gh skill`](https://cli.github.com/manual/gh_skill_install) and [`npx skills` / skills.sh](https://github.com/vercel-labs/skills).
