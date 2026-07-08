#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const root = new URL("../", import.meta.url);
const rootPath = fileURLToPath(root);

function readJson(path) {
  return JSON.parse(readText(path));
}

function readText(path) {
  return readFileSync(new URL(path, root), "utf8");
}

function fail(message) {
  throw new Error(message);
}

function assert(condition, message) {
  if (!condition) fail(message);
}

const knownGroups = new Set(["source", "typescript", "go", "rust", "python"]);
const selectedGroups = new Set(process.argv.slice(2));
for (const group of selectedGroups) {
  assert(knownGroups.has(group), `unknown check group: ${group}`);
}

function shouldRun(group) {
  return selectedGroups.size === 0 || selectedGroups.has(group);
}

function validateHosts(spec) {
  assert(spec.schemaVersion === 1, "hosts schemaVersion must be 1");
  assert(Array.isArray(spec.hosts), "hosts must be an array");

  const idPattern = /^[a-z0-9]+(-[a-z0-9]+)*$/;
  const projectPattern = /^(?!\/)(?!~)(?!.*(^|\/)\.\.(\/|$))[^\0]+$/;
  const homePattern = /^~\/[^\0]+$/;
  const statuses = new Set([
    "verified",
    "documented",
    "community",
    "experimental",
  ]);
  const ids = new Set();
  const aliases = new Set();

  for (const host of spec.hosts) {
    assert(idPattern.test(host.id), `bad host id: ${host.id}`);
    assert(!ids.has(host.id), `duplicate host id: ${host.id}`);
    ids.add(host.id);
    assert(host.displayName, `missing displayName: ${host.id}`);

    assert(
      Array.isArray(host.projectSkillsDirs),
      `projectSkillsDirs must be an array: ${host.id}`,
    );
    assert(
      Array.isArray(host.userSkillsDirs),
      `userSkillsDirs must be an array: ${host.id}`,
    );
    assert(
      host.projectSkillsDirs.length + host.userSkillsDirs.length > 0,
      `host needs at least one install path: ${host.id}`,
    );

    for (const path of host.projectSkillsDirs) {
      assert(
        projectPattern.test(path),
        `bad project path for ${host.id}: ${path}`,
      );
    }
    for (const path of host.userSkillsDirs) {
      assert(homePattern.test(path), `bad user path for ${host.id}: ${path}`);
    }

    assert(
      Array.isArray(host.detect) && host.detect.length > 0,
      `missing detect paths: ${host.id}`,
    );
    for (const path of host.detect) {
      assert(
        homePattern.test(path) || projectPattern.test(path),
        `bad detect path for ${host.id}: ${path}`,
      );
    }

    assert(
      statuses.has(host.status),
      `bad status for ${host.id}: ${host.status}`,
    );

    for (const alias of host.aliases || []) {
      assert(idPattern.test(alias), `bad alias for ${host.id}: ${alias}`);
      assert(!ids.has(alias), `alias conflicts with host id: ${alias}`);
      assert(!aliases.has(alias), `duplicate alias: ${alias}`);
      aliases.add(alias);
    }
  }

  return { ids, aliases };
}

function validateCases(cases, hosts) {
  assert(cases.schemaVersion === 1, "cases schemaVersion must be 1");
  assert(Array.isArray(cases.cases), "cases must be an array");

  const caseIds = new Set();
  for (const testCase of cases.cases) {
    assert(!caseIds.has(testCase.id), `duplicate case id: ${testCase.id}`);
    caseIds.add(testCase.id);
  }

  const allHostsCase = cases.cases.find(
    (testCase) => testCase.id === "all-supported-hosts-load",
  );
  assert(allHostsCase, "missing all-supported-hosts-load case");
  assert(
    allHostsCase.expected.count === hosts.length,
    "all-supported-hosts-load count drifted",
  );
  assert(
    JSON.stringify(allHostsCase.expected.hostIds) ===
      JSON.stringify(hosts.map((host) => host.id)),
    "all-supported-hosts-load hostIds drifted",
  );

  for (const id of [
    "alias-resolution",
    "kimi-alias-resolution",
    "unknown-host-id",
    "parse-install-flags-defaults",
    "parse-install-flags-explicit",
    "parse-install-flags-star",
    "parse-install-flags-errors",
    "shared-target-deduplication-many-hosts",
    "user-scope-install",
    "codex-user-scope-prefers-first-user-dir",
    "project-scope-install",
    "project-scope-plan",
    "project-only-host-project-scope-install",
    "project-only-host-user-scope-error",
    "auto-host-detection",
    "auto-host-detection-empty",
    "unchanged-noop",
    "unchanged-repairs-script-mode",
    "workflow-unchanged-silent",
    "workflow-conflict-exit",
    "changed-update",
    "unmanaged-conflict",
    "different-owner-conflict",
    "uninstall-owned-skill",
    "uninstall-owner-mismatch",
    "missing-skill-md",
    "invalid-frontmatter",
    "nested-resources-copied",
    "embedded-skill-source",
    "workflow-explicit-agent",
    "workflow-agent-star",
    "workflow-scope-prompt-before-agent",
    "workflow-scope-non-tty-error",
    "workflow-scope-yes-default",
    "workflow-shared-target-renders-host-rows",
    "workflow-zero-detected-tty-prompts",
    "workflow-one-detected-auto-selects",
    "workflow-many-detected-tty-prompts",
    "workflow-many-detected-enter-cancels",
    "workflow-many-detected-select-one-confirms",
    "workflow-one-detected-confirms-installs",
    "workflow-yes-batch-installs-detected",
    "workflow-many-detected-yes-installs",
    "workflow-non-tty-no-agent-error",
    "workflow-zero-detected-yes-error",
    "github-bundle-install",
    "github-bundle-mode-only-update-refreshes-metadata",
    "github-bundle-dry-run",
    "github-bundle-resolve-failure",
    "github-bundle-unchanged",
  ]) {
    assert(caseIds.has(id), `missing golden case: ${id}`);
  }
}

function validateFixtures() {
  const skill = readFileSync(
    new URL("testdata/skills/basic/SKILL.md", root),
    "utf8",
  );
  assert(
    /^---\n[\s\S]*?\n---\n/.test(skill),
    "basic SKILL.md missing frontmatter",
  );
  assert(/^name: basic$/m.test(skill), "basic skill name mismatch");
  assert(
    /^description: .{1,1024}$/m.test(skill),
    "basic skill description missing",
  );
  readJson("testdata/skills/basic/assets/template.json");
}

function matchOne(path, pattern, label) {
  const match = readText(path).match(pattern);
  assert(match, `missing ${label}`);
  return match[1];
}

function validateVersions() {
  const version = readJson("ts/package.json").version;
  assert(
    matchOne("rust/Cargo.toml", /^version = "([^"]+)"$/m, "rust version") ===
      version,
    "rust version drifted",
  );
  assert(
    matchOne(
      "python/pyproject.toml",
      /^version = "([^"]+)"$/m,
      "python version",
    ) === version,
    "python version drifted",
  );
  assert(
    matchOne(
      "go-cobra/go.mod",
      /^\s*github\.com\/lathe-cli\/kitup\/go v([^\s]+)$/m,
      "go-cobra core version",
    ) === version,
    "go-cobra core version drifted",
  );
  assert(
    matchOne(
      "python/pyproject.toml",
      /^name = "([^"]+)"$/m,
      "python package name",
    ) === "kitup-sdk",
    "python package name drifted",
  );
  assert(
    matchOne(
      "python/pyproject.toml",
      /^requires-python = "([^"]+)"$/m,
      "python requires-python",
    ) === ">=3.10",
    "python requires-python drifted",
  );
}

function validateReleaseWorkflow() {
  const workflow = readText(".github/workflows/release.yml");
  assert(
    workflow.includes("github.com\\/lathe-cli\\/kitup\\/go"),
    "release workflow must check the canonical Go module path",
  );
  assert(
    !workflow.includes("github.com\\/samzong\\/kitup\\/go"),
    "release workflow still checks the old Go module path",
  );
  assert(
    /^\s*environment: pypi$/m.test(workflow),
    "release workflow must use the pypi environment",
  );
  assert(
    workflow.includes("https://pypi.org/pypi/kitup-sdk/"),
    "release workflow must check kitup-sdk on PyPI",
  );
  assert(
    !workflow.includes("https://pypi.org/pypi/kitup/"),
    "release workflow still checks the old PyPI package name",
  );
  const smoke = readText("scripts/smoke-release.sh");
  assert(
    smoke.includes('"kitup-sdk==$version"'),
    "release smoke must install kitup-sdk from PyPI",
  );
}

const hostsSpec = readJson("spec/hosts.json");
const cases = readJson("testdata/cases/bundled-skill-install.json");
readJson("spec/hosts.schema.json");
readJson("testdata/cases.schema.json");

const { ids, aliases } = validateHosts(hostsSpec);
validateCases(cases, hostsSpec.hosts);
validateFixtures();
validateVersions();
validateReleaseWorkflow();

assert(ids.has("kimi-cli"), "kimi-cli must be canonical");
assert(!ids.has("kimi-code-cli"), "kimi-code-cli must not be canonical");
assert(aliases.has("kimi-code-cli"), "kimi-code-cli alias missing");

console.log(`ok: ${hostsSpec.hosts.length} hosts; ${cases.cases.length} cases`);

function detectedEnv(prefix) {
  const home = mkdtempSync(`${tmpdir()}/${prefix}`);
  mkdirSync(`${home}/.codex`, { recursive: true });
  return {
    CARGO_HOME: process.env.CARGO_HOME ?? `${process.env.HOME}/.cargo`,
    HOME: home,
    RUSTUP_HOME: process.env.RUSTUP_HOME ?? `${process.env.HOME}/.rustup`,
  };
}

for (const [group, name, command, args, cwd, env] of [
  [
    "source",
    "generated-hosts",
    "node",
    ["scripts/sync-hosts.mjs", "--check"],
    rootPath,
  ],
  [
    "typescript",
    "typescript-format",
    "pnpm",
    [
      "--dir",
      "ts",
      "exec",
      "prettier",
      "--check",
      "src",
      "test",
      "../examples/ts/cli.ts",
      "../scripts/check.mjs",
      "../scripts/prepare-release.mjs",
    ],
    rootPath,
  ],
  ["typescript", "typescript", "pnpm", ["--dir", "ts", "test"], rootPath],
  ["go", "go", "go", ["test", "./..."], new URL("../go/", import.meta.url)],
  [
    "go",
    "go-cobra",
    "go",
    ["test", "./..."],
    new URL("../go-cobra/", import.meta.url),
  ],
  ["rust", "rust", "cargo", ["test"], new URL("../rust/", import.meta.url)],
  [
    "rust",
    "rust-clippy",
    "cargo",
    [
      "clippy",
      "--manifest-path",
      "rust/Cargo.toml",
      "--all-targets",
      "--",
      "-D",
      "warnings",
    ],
    rootPath,
  ],
  [
    "python",
    "python-format",
    "uv",
    [
      "run",
      "ruff",
      "format",
      "--check",
      "src",
      "tests",
      "--exclude",
      "src/kitup/_hosts_generated.py",
    ],
    new URL("../python/", import.meta.url),
  ],
  [
    "python",
    "python-lint",
    "uv",
    ["run", "ruff", "check", "src", "tests"],
    new URL("../python/", import.meta.url),
  ],
  [
    "python",
    "python",
    "uv",
    ["run", "pytest", "tests", "-q"],
    new URL("../python/", import.meta.url),
  ],
  [
    "typescript",
    "example-ts",
    "pnpm",
    ["--dir", "examples/ts", "install-skill"],
    rootPath,
    detectedEnv("kitup-example-ts-"),
  ],
  [
    "go",
    "example-go",
    "go",
    ["run", "."],
    new URL("../examples/go/", import.meta.url),
    detectedEnv("kitup-example-go-"),
  ],
  [
    "rust",
    "example-rust",
    "cargo",
    ["run", "--quiet"],
    new URL("../examples/rust/", import.meta.url),
    detectedEnv("kitup-example-rust-"),
  ],
  [
    "python",
    "example-python",
    "uv",
    ["run", "python", "main.py"],
    new URL("../examples/python/", import.meta.url),
    detectedEnv("kitup-example-python-"),
  ],
]) {
  if (!shouldRun(group)) continue;
  console.log(`\n==> ${name}`);
  const result = spawnSync(command, args, {
    cwd,
    env: { ...process.env, ...env },
    stdio: "inherit",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

console.log("\nok: all checks passed");
