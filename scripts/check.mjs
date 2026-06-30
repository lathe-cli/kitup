#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const root = new URL("../", import.meta.url);
const rootPath = fileURLToPath(root);

function readJson(path) {
  return JSON.parse(readFileSync(new URL(path, root), "utf8"));
}

function fail(message) {
  throw new Error(message);
}

function assert(condition, message) {
  if (!condition) fail(message);
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

const hostsSpec = readJson("spec/hosts.json");
const cases = readJson("testdata/cases/bundled-skill-install.json");
readJson("spec/hosts.schema.json");
readJson("testdata/cases.schema.json");

const { ids, aliases } = validateHosts(hostsSpec);
validateCases(cases, hostsSpec.hosts);
validateFixtures();

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

for (const [name, command, args, cwd, env] of [
  ["generated-hosts", "node", ["scripts/sync-hosts.mjs", "--check"], rootPath],
  [
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
  ["typescript", "pnpm", ["--dir", "ts", "test"], rootPath],
  ["go", "go", ["test", "./..."], new URL("../go/", import.meta.url)],
  [
    "go-cobra",
    "go",
    ["test", "./..."],
    new URL("../go-cobra/", import.meta.url),
  ],
  ["rust", "cargo", ["test"], new URL("../rust/", import.meta.url)],
  [
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
    "example-ts",
    "pnpm",
    ["--dir", "examples/ts", "install-skill"],
    rootPath,
    detectedEnv("kitup-example-ts-"),
  ],
  [
    "example-go",
    "go",
    ["run", "."],
    new URL("../examples/go/", import.meta.url),
    detectedEnv("kitup-example-go-"),
  ],
  [
    "example-rust",
    "cargo",
    ["run", "--quiet"],
    new URL("../examples/rust/", import.meta.url),
    detectedEnv("kitup-example-rust-"),
  ],
]) {
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
