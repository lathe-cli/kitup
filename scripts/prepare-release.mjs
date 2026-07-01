#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const rootUrl = new URL("../", import.meta.url);
const root = fileURLToPath(rootUrl);
const args = process.argv.slice(2);
const positional = args.filter((arg) => !arg.startsWith("-"));
const flags = args.filter((arg) => arg.startsWith("-"));
const dryRun = flags.includes("--dry-run");

if (
  positional.length !== 1 ||
  !["patch", "minor", "major"].includes(positional[0])
) {
  fail(
    "Usage: node scripts/prepare-release.mjs <patch|minor|major> [--dry-run]",
  );
}
if (flags.some((arg) => arg !== "--dry-run")) {
  fail(`Unknown flag: ${flags.find((arg) => arg !== "--dry-run")}`);
}

const bump = positional[0];
const packagePath = "ts/package.json";
const cargoPath = "rust/Cargo.toml";
const goCobraModPath = "go-cobra/go.mod";
const rustLockPath = "rust/Cargo.lock";
const exampleRustLockPath = "examples/rust/Cargo.lock";
const pythonPackagePath = "python/pyproject.toml";
const pkg = JSON.parse(read(packagePath));
const currentVersion = pkg.version;
const nextVersion = bumpVersion(currentVersion, bump);
const tag = `v${nextVersion}`;
const branch = `release/${tag}`;
const changedFiles = [
  packagePath,
  cargoPath,
  rustLockPath,
  exampleRustLockPath,
  goCobraModPath,
  pythonPackagePath,
];

if (!dryRun) {
  assertCleanMain(branch);
  run("git", ["switch", "-c", branch]);
}

pkg.version = nextVersion;
write(packagePath, `${JSON.stringify(pkg, null, 2)}\n`);
replaceOne(cargoPath, /^version = "([^"]+)"$/m, `version = "${nextVersion}"`);
replaceOne(
  pythonPackagePath,
  /^version = "([^"]+)"$/m,
  `version = "${nextVersion}"`,
);
replaceOne(
  goCobraModPath,
  /(github\.com\/lathe-cli\/kitup\/go\s+)v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?/,
  `$1${tag}`,
);

if (dryRun) {
  console.log(`Would create branch: ${branch}`);
  console.log(`Would update version: ${currentVersion} -> ${nextVersion}`);
  console.log(`Would update files: ${changedFiles.join(", ")}`);
  console.log(`Would run: cargo generate-lockfile, make check, git commit -s`);
  process.exit(0);
}

run("cargo", ["generate-lockfile", "--manifest-path", cargoPath]);
run("cargo", [
  "generate-lockfile",
  "--manifest-path",
  "examples/rust/Cargo.toml",
]);
run("make", ["check"]);
run("git", ["add", ...changedFiles]);
run("git", ["commit", "-s", "-m", `chore: prepare ${tag} release`]);

console.log("");
console.log(`Prepared ${tag} on ${branch}.`);
console.log("Open and merge the release PR manually, then tag main manually.");

function read(path) {
  return readFileSync(new URL(path, rootUrl), "utf8");
}

function write(path, content) {
  if (!dryRun) writeFileSync(new URL(path, rootUrl), content);
}

function replaceOne(path, pattern, replacement) {
  const before = read(path);
  let count = 0;
  const after = before.replace(pattern, (...match) => {
    count += 1;
    return replacement.replace(
      /\$(\d+)/g,
      (_, index) => match[Number(index)] ?? "",
    );
  });
  if (count !== 1) fail(`Expected one match in ${path}, found ${count}`);
  write(path, after);
}

function bumpVersion(version, kind) {
  if (!/^\d+\.\d+\.\d+$/.test(version)) fail(`Unsupported version: ${version}`);
  const parts = version.split(".").map((part) => Number.parseInt(part, 10));
  if (kind === "major") return `${parts[0] + 1}.0.0`;
  if (kind === "minor") return `${parts[0]}.${parts[1] + 1}.0`;
  return `${parts[0]}.${parts[1]}.${parts[2] + 1}`;
}

function assertCleanMain(nextBranch) {
  const branchName = output("git", ["branch", "--show-current"]);
  if (branchName !== "main")
    fail(`Release prep must start from main, current branch is ${branchName}`);
  if (output("git", ["status", "--porcelain"]) !== "")
    fail("Release prep requires a clean worktree");
  if (
    commandSucceeds("git", [
      "rev-parse",
      "--verify",
      `refs/heads/${nextBranch}`,
    ])
  ) {
    fail(`Branch already exists: ${nextBranch}`);
  }
}

function run(command, commandArgs) {
  console.log(`$ ${[command, ...commandArgs].join(" ")}`);
  execFileSync(command, commandArgs, { cwd: root, stdio: "inherit" });
}

function output(command, commandArgs) {
  return execFileSync(command, commandArgs, {
    cwd: root,
    encoding: "utf8",
  }).trim();
}

function commandSucceeds(command, commandArgs) {
  try {
    execFileSync(command, commandArgs, { cwd: root, stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
