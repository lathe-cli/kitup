# API

`kitup` exposes the same installer concepts in TypeScript, Go, and Rust.

The core flow is:

1. resolve a local directory tree, embedded files, or public GitHub bundle
2. resolve safe target agent selection for CLI workflows
3. validate `SKILL.md`
4. copy, update, skip, or report conflicts
5. write `.kitup.json` ownership metadata
6. return a structured report

## TypeScript

Package: `@kitup/sdk`

```ts
import {
  detectHosts,
  directoryBundle,
  filesBundle,
  githubBundle,
  installBundledSkill,
  installFlagError,
  installWorkflowError,
  installUxText,
  planBundledSkill,
  parseInstallFlags,
  classifyInstallWorkflowExit,
  resolveInstallSelection,
  runBundledSkillInstall,
  uninstallBundledSkill,
  updateBundledSkill,
  validateSkillBundle,
} from "@kitup/sdk";
```

Primitive install call:

```ts
const report = await installBundledSkill({
  appId: "mycli",
  skillBundle: directoryBundle("./skills/mycli"),
  scope: "user",
});
```

Public GitHub bundle call:

```ts
const report = await installBundledSkill({
  appId: "mycli",
  skillBundle: githubBundle({
    owner: "acme",
    repo: "mycli-skills",
    path: "skills/mycli",
    ref: "v1.2.3",
  }),
  scope: "user",
});
```

Implemented functions:

- `loadHostSpec(hostsFile?)`
- `resolveHosts({ agents, hostsFile?, hosts? })`
- `detectHosts({ home?, cwd?, hostsFile?, scope? })`
- `resolveInstallSelection({ home?, cwd?, hostsFile?, scope, agents?, yes?, stdinTTY?, currentAgent? })`
- `resolveInstallTargets({ home?, cwd?, hostsFile?, agents?, scope, skillName })`
- `validateSkillBundle(bundle, cwd?)`
- `computeBundleContentHash(bundle, cwd?)`
- `directoryBundle(path)`
- `filesBundle(files)`
- `githubBundle(options)`
- `parseInstallFlags(flags)`
- `agentSelectorFromFlags(values)`
- `parseScopeFlag(value)`
- `classifyInstallWorkflowExit(workflow)`
- `installWorkflowError(workflow)`
- `installFlagError(errors)`
- `runBundledSkillInstall(options)`
- `planBundledSkill(options)`
- `installBundledSkill(options)`
- `updateBundledSkill(options)`
- `uninstallBundledSkill(options)`
- `installUxText`

## Go

Module: `github.com/samzong/kitup/go`

```go
import kitup "github.com/samzong/kitup/go"
```

Primitive install call:

```go
report, err := kitup.InstallBundledSkill(kitup.InstallOptions{
	AppID:       "mycli",
	SkillBundle: kitup.DirectoryBundle("./skills/mycli"),
	Scope:       kitup.UserScope,
})
```

Public GitHub bundle call:

```go
report, err := kitup.InstallBundledSkill(kitup.InstallOptions{
	AppID: "mycli",
	SkillBundle: kitup.GitHubBundle(kitup.GitHubBundleOptions{
		Owner: "acme",
		Repo:  "mycli-skills",
		Path:  "skills/mycli",
		Ref:   "v1.2.3",
	}),
	Scope: kitup.UserScope,
})
```

Implemented functions:

- `LoadHostSpec(hostsFile string)`
- `ResolveHosts(agents, hosts)`
- `DetectHosts(opts, scope)`
- `ResolveInstallSelection(opts)`
- `ResolveInstallTargets(opts, agents, scope, skillName)`
- `ValidateSkillBundle(bundle)`
- `ComputeBundleContentHash(bundle)`
- `DirectoryBundle(path)`
- `FSBundle(fsys, root)`
- `FilesBundle(files)`
- `GitHubBundle(opts)`
- `ParseInstallFlags(flags)`
- `AgentSelectorFromFlags(values)`
- `ParseScopeFlag(value)`
- `ClassifyInstallWorkflowExit(report)`
- `InstallWorkflowError(report)`
- `InstallFlagError(errors)`
- `RunBundledSkillInstall(opts)`
- `PlanBundledSkill(opts)`
- `InstallBundledSkill(opts)`
- `UpdateBundledSkill(opts)`
- `UninstallBundledSkill(opts)`
- `InstallUX`

Optional Cobra adapter module: `github.com/samzong/kitup/go-cobra`

- `NewSkillCommand(opts)`
- `NewInstallCommand(opts)`

## Rust

Crate: `kitup`

Primitive install call:

```rust
let report = kitup::install_bundled_skill(&kitup::InstallOptions {
    base: kitup::BaseOptions::default(),
    app_id: "mycli".to_string(),
    skill_bundle: kitup::directory_bundle("./skills/mycli"),
    scope: kitup::Scope::User,
    agents: kitup::AgentSelector::Auto,
    force: false,
})?;
```

Public GitHub bundle call:

```rust
let report = kitup::install_bundled_skill(&kitup::InstallOptions {
    base: kitup::BaseOptions::default(),
    app_id: "mycli".to_string(),
    skill_bundle: kitup::github_bundle(kitup::GitHubBundleOptions {
        owner: "acme".to_string(),
        repo: "mycli-skills".to_string(),
        path: "skills/mycli".to_string(),
        ref_name: "v1.2.3".to_string(),
    }),
    scope: kitup::Scope::User,
    agents: kitup::AgentSelector::Auto,
    force: false,
})?;
```

Implemented functions:

- `load_host_spec(hosts_file)`
- `resolve_hosts(agents, hosts)`
- `detect_hosts(options, scope)`
- `resolve_install_selection(options)`
- `resolve_install_targets(options, agents, scope, skill_name)`
- `validate_skill_bundle(bundle)`
- `compute_bundle_content_hash(bundle)`
- `directory_bundle(path)`
- `files_bundle(files)`
- `github_bundle(options)`
- `parse_install_flags(flags)`
- `agent_selector_from_flags(values, errors)`
- `parse_scope_flag(value, errors)`
- `classify_install_workflow_exit(report)`
- `install_workflow_error(report)`
- `install_flag_error(errors)`
- `run_bundled_skill_install(options)`
- `run_bundled_skill_install_with_io(options, input, output)`
- `plan_bundled_skill(options)`
- `install_bundled_skill(options)`
- `update_bundled_skill(options)`
- `uninstall_bundled_skill(options)`
- `INSTALL_UX`

## Options

Install options use the same concepts across languages:

- `appId` / `AppID` / `app_id`: owner id written to `.kitup.json`
- `skillBundle` / `SkillBundle` / `skill_bundle`: local directory, embedded files, or public GitHub bundle
- `scope`: `user` or `project`
- `agents`: `"auto"`, `"*"`, or explicit host ids
- `force`: overwrite unmanaged or different-owner target directories instead of reporting conflicts
- `home`, `cwd`, `hostsFile`: optional test and embedding overrides

Bundle file paths must use root-relative POSIX paths. SDKs reject empty paths, absolute paths, `..`, duplicate files, and backslash paths. SDKs exclude `.kitup.json`, `.git`, `.DS_Store`, swap files, and editor backups before validation, hashing, and copy.

The first non-local bundle constructor is GitHub only:

- TypeScript: `githubBundle({ owner, repo, path, ref })`
- Go: `GitHubBundle(GitHubBundleOptions{Owner, Repo, Path, Ref})`
- Rust: `github_bundle(GitHubBundleOptions { owner, repo, path, ref_name })`

GitHub bundle resolution downloads only files under the configured directory path, requires `SKILL.md` at that bundle root, records the requested ref and resolved commit, and writes GitHub provenance into `.kitup.json`. It does not search GitHub, install dependencies, execute scripts, handle private auth, or install whole repositories by default.

The embedding CLI owns command names and framework attachment. `kitup` owns standard install flag semantics, selector mapping, user-facing workflow text, summary rendering, confirmation, dry-run planning, workflow exit classification, and execution. For user-facing commands, call `runBundledSkillInstall` / `RunBundledSkillInstall` / `run_bundled_skill_install` with values from the shared flag parsing helpers.

Workflow-only options:

- `yes`: skip prompts and accept policy-selected targets
- `dryRun`: render and return the plan without writing
- `stdinTTY`: whether interactive prompts are allowed
- `currentAgent`: host id detected by an embedding agent runtime
- `defaultScope`: scope used by Enter at the scope prompt and by `yes` when scope was not explicit
- `scopeSet`: whether the user explicitly provided `--scope`
- `promptScope`: whether missing explicit scope should trigger workflow scope selection
- `input` / `In` and `output` / `Out`: prompt and rendering streams

Standard install flags:

- `--scope`: `user` or `project`
- `--agent`: repeatable target agent id; comma-separated values are accepted; `*` means all hosts and must be the only agent value
- `--dry-run`: render the plan without writing
- `--yes` / `-y`: skip prompts and accept policy-selected targets
- `--force`: overwrite unmanaged or different-owner target directories

Flag parsing returns a normalized scope, `scopeSet`, normalized agent selector, `yes`, `dryRun`, `force`, and structured flag errors. An empty agent list maps to `auto`; `*` maps to all hosts; explicit ids are deduplicated in input order.

For CLI workflows, pass `promptScope: true`. If `scopeSet` is false, TTY mode prompts for scope before agent selection and planning. Enter uses `defaultScope` or `user`. Non-TTY without explicit scope and without `yes` returns `scope-selection-required`. `yes` with no explicit scope uses `defaultScope` or `user`.

Workflow exit helpers classify reports into:

- `ok`
- `canceled`
- `selection-error`
- `conflict`
- `error`

Recommended CLI behavior:

```bash
mycli skill install
mycli skill install --scope user --agent codex
mycli skill install --scope project --agent codex --agent claude-code
mycli skill install --scope user --agent codex --force
```

The lower-level selection resolver remains available for custom shells. It returns one of:

- `install`: proceed to plan and confirmation with `selectedHostIds`
- `select-agents`: ask the user to choose from `candidateHostIds`
- `error`: do not write

Non-TTY without explicit agents and without `yes` returns `agent-selection-required`. `yes` with zero detected hosts returns `no-detected-hosts`; it does not mean all hosts.

In TTY mode, zero detected hosts prompts from all supported hosts. One detected host is auto-selected but still goes through summary confirmation. Multiple detected hosts prompt from the detected host candidates without defaulting to all; an empty selection cancels instead of writing.

Selector semantics:

- `scope: "user"` installs into the first `userSkillsDirs` path for each host.
- `scope: "project"` installs into the first `projectSkillsDirs` path for each host.
- `agents: "auto"` uses host detection.
- `agents: "*"` selects every host adapter.
- explicit agents select canonical host ids or aliases.
- hosts without a path for the selected scope return an `unsupported-scope` error.

## Reports

Install reports include:

- `installed`
- `updated`
- `skipped`
- `conflicts`
- `errors`

Uninstall reports include:

- `removed`
- `skipped`
- `conflicts`
- `errors`

TypeScript returns typed report objects. Go exposes `InstallReport`, `UninstallReport`, `TargetResult`, `TargetStatus`, and `ReportError`. Rust exposes `InstallReport`, `UninstallReport`, `TargetResult`, `TargetStatus`, and `ReportError`.

The serialized JSON report shape is the same across TypeScript, Go, and Rust. `installed`, `updated`, and `removed` contain target results. `skipped` and `conflicts` contain target results plus `reason`.

Conflict is the safe default. A target directory without matching `.kitup.json` ownership metadata is reported as a conflict, not overwritten unless `force` / `--force` is explicit.
