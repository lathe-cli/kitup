import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../", import.meta.url));
const read = (path) => readFileSync(join(root, path), "utf8");
const workflow = read(".github/workflows/release.yml");
const step = (name) => {
  const block = workflow
    .split(`      - name: ${name}\n`)[1]
    ?.split("      - name:")[0];
  assert.ok(block, name);
  return block
    .match(/        run: \|\n([\s\S]*?)(?=        env:|$)/)[1]
    .replace(/^          /gm, "");
};
const git = (cwd, ...args) =>
  execFileSync("git", args, { cwd, encoding: "utf8" }).trim();

function fixture(t, version = "0.1.5-rc.1", pythonVersion = "0.1.5rc1") {
  const dir = mkdtempSync(
    join(
      existsSync("/Volumes/git/scratch") ? "/Volumes/git/scratch" : tmpdir(),
      "kitup-release-",
    ),
  );
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  for (const path of [
    "scripts",
    "ts",
    "rust",
    "python",
    "go-cobra",
    "examples/go",
  ])
    mkdirSync(join(dir, path), { recursive: true });
  for (const path of [
    "release-version.mjs",
    "check-release-tag.mjs",
    "prepare-release.mjs",
  ])
    cpSync(join(root, "scripts", path), join(dir, "scripts", path));
  writeFileSync(join(dir, "ts/package.json"), JSON.stringify({ version }));
  writeFileSync(
    join(dir, "rust/Cargo.toml"),
    `[package]\nversion = "${version}"\n`,
  );
  writeFileSync(
    join(dir, "python/pyproject.toml"),
    `[project]\nversion = "${pythonVersion}"\n`,
  );
  for (const path of ["go-cobra/go.mod", "examples/go/go.mod"])
    writeFileSync(
      join(dir, path),
      `require (\n  github.com/lathe-cli/kitup/go v${version}\n)\n`,
    );
  git(dir, "init", "-q");
  git(dir, "config", "user.name", "Fixture");
  git(dir, "config", "user.email", "fixture@example.com");
  const tree = git(dir, "mktree");
  const commit = git(dir, "commit-tree", tree, "-m", "fixture");
  git(dir, "update-ref", "HEAD", commit);
  return dir;
}

function gate(dir, tag) {
  return spawnSync("node", ["scripts/check-release-tag.mjs"], {
    cwd: dir,
    encoding: "utf8",
    env: {
      ...process.env,
      RELEASE_TAG: tag,
      GITHUB_OUTPUT: join(dir, "outputs"),
    },
  });
}

test("the first release operation validates the tag before checks and publication", () => {
  const firstRun = workflow.match(/        run: (.+)/)[1];
  assert.equal(firstRun, "node scripts/check-release-tag.mjs");
  assert.ok(workflow.indexOf(firstRun) < workflow.indexOf("run: make check"));
});

test("the release gate rejects invalid tags, mismatched manifests, and tag commits", (t) => {
  const dir = fixture(t);
  for (const tag of [
    "v0.1.5-dev.1",
    "v0.1.5\n",
    "v0.1.5-rc.01",
    "v01.2.0",
    "v0.1.5-rc.0",
    "v0.1.5+build.1",
    "go-cobra/v0.1.5",
  ]) {
    const result = gate(dir, tag);
    assert.notEqual(result.status, 0, tag);
    assert.match(result.stderr, /Invalid release/);
  }
  git(dir, "tag", "v0.1.5-rc.1");
  for (const [path, invalid] of [
    ["python/pyproject.toml", '[project]\nversion = "0.1.5-rc.1"\n'],
    ["ts/package.json", '{"version":"0.1.4"}'],
    ["rust/Cargo.toml", '[package]\nversion = "0.1.4"\n'],
    [
      "go-cobra/go.mod",
      "require (\n  github.com/lathe-cli/kitup/go v0.1.4\n)\n",
    ],
  ]) {
    const original = readFileSync(join(dir, path));
    writeFileSync(join(dir, path), invalid);
    const result = gate(dir, "v0.1.5-rc.1");
    assert.notEqual(result.status, 0, path);
    assert.ok(result.stderr.includes(`${path} version`));
    writeFileSync(join(dir, path), original);
  }
  const valid = gate(dir, "v0.1.5-rc.1");
  assert.equal(valid.status, 0, valid.stderr);
  const tree = git(dir, "mktree");
  const other = execFileSync(
    "git",
    ["commit-tree", tree, "-p", "HEAD", "-m", "other"],
    { cwd: dir, encoding: "utf8" },
  ).trim();
  git(dir, "update-ref", "HEAD", other);
  assert.match(gate(dir, "v0.1.5-rc.1").stderr, /HEAD does not match/);
});

test("all release stages map to PEP 440 and expose publication metadata", (t) => {
  for (const [version, pythonVersion, stage] of [
    ["0.1.5-alpha.1", "0.1.5a1", "alpha"],
    ["0.1.5-beta.2", "0.1.5b2", "beta"],
    ["0.1.5-rc.10", "0.1.5rc10", "rc"],
    ["0.1.5", "0.1.5", ""],
  ]) {
    const dir = fixture(t, version, pythonVersion);
    git(dir, "tag", `v${version}`);
    const result = gate(dir, `v${version}`);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(
      readFileSync(join(dir, "outputs"), "utf8"),
      `version=${version}\npython_version=${pythonVersion}\nstage=${stage}\nis_prerelease=${Boolean(stage)}\n`,
    );
  }
});

test("preparation accepts explicit prereleases and promotion, and refuses backward versions", (t) => {
  const dir = fixture(t);
  const prep = (arg) =>
    spawnSync("node", ["scripts/prepare-release.mjs", arg, "--dry-run"], {
      cwd: dir,
      encoding: "utf8",
    });
  for (const arg of ["0.1.5-rc.2", "0.1.5", "patch", "minor", "major"])
    assert.equal(prep(arg).status, 0, arg);
  assert.match(prep("patch").stdout, /0.1.5-rc.1 -> 0.1.5/);
  for (const arg of [
    "0.1.5-rc.1",
    "0.1.5-beta.9",
    "0.1.4",
    "0.1.5-dev.1",
    "0.1.5-rc.01",
  ])
    assert.notEqual(prep(arg).status, 0, arg);
});

test("preparation writes ecosystem versions from the same requested version", (t) => {
  const dir = fixture(t, "0.1.4", "0.1.4");
  const tools = mkdtempSync(
    join(
      existsSync("/Volumes/git/scratch") ? "/Volumes/git/scratch" : tmpdir(),
      "kitup-prep-tools-",
    ),
  );
  t.after(() => rmSync(tools, { recursive: true, force: true }));
  writeFileSync(
    join(tools, "git"),
    '#!/bin/sh\ncase "$1 $2" in\n"branch --show-current") echo main ;;\n"rev-parse --verify") exit 1 ;;\nesac\n',
    { mode: 0o755 },
  );
  for (const command of ["cargo", "uv", "make"])
    writeFileSync(
      join(tools, command),
      '#!/bin/sh\nprintf "%s\\n" "$*" >> "$PREP_CALLS"\n',
      { mode: 0o755 },
    );
  const result = spawnSync(
    "node",
    ["scripts/prepare-release.mjs", "0.1.5-rc.1"],
    {
      cwd: dir,
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${tools}:${process.env.PATH}`,
        PREP_CALLS: join(tools, "calls"),
      },
    },
  );
  assert.equal(result.status, 0, result.stderr);
  assert.equal(
    JSON.parse(readFileSync(join(dir, "ts/package.json"))).version,
    "0.1.5-rc.1",
  );
  for (const path of [
    "rust/Cargo.toml",
    "go-cobra/go.mod",
    "examples/go/go.mod",
  ])
    assert.ok(
      readFileSync(join(dir, path), "utf8").includes("0.1.5-rc.1"),
      path,
    );
  assert.match(
    readFileSync(join(dir, "python/pyproject.toml"), "utf8"),
    /version = "0.1.5rc1"/,
  );
  assert.equal(
    readFileSync(join(tools, "calls"), "utf8"),
    "generate-lockfile --manifest-path rust/Cargo.toml\ngenerate-lockfile --manifest-path examples/rust/Cargo.toml\nlock --directory python\nlock --directory examples/python\ncheck\n",
  );
});

test("release notes exclude module tags and prereleases never use stable channels", (t) => {
  const dir = fixture(t);
  git(dir, "tag", "v0.1.4");
  git(dir, "tag", "go/v9.9.9");
  git(dir, "tag", "go-cobra/v9.9.9");
  const tree = git(dir, "mktree");
  const head = execFileSync(
    "git",
    ["commit-tree", tree, "-p", "HEAD", "-m", "release"],
    { cwd: dir, encoding: "utf8" },
  ).trim();
  git(dir, "update-ref", "HEAD", head);
  mkdirSync(join(dir, "bin"));
  const capture =
    '#!/bin/sh\nif [ "$1 $2" = "release view" ]; then exit 1; fi\nprintf "%s\\n" "$@" >> "$CAPTURE"\n';
  for (const command of ["gh", "npm"])
    writeFileSync(join(dir, "bin", command), capture, { mode: 0o755 });
  for (const stage of ["alpha", "beta", "rc", ""]) {
    const capturePath = join(dir, "capture");
    writeFileSync(capturePath, "");
    const env = {
      ...process.env,
      PATH: `${join(dir, "bin")}:${process.env.PATH}`,
      CAPTURE: capturePath,
      GITHUB_SHA: git(dir, "rev-parse", "HEAD^"),
      GITHUB_REF_NAME: "main",
      RELEASE_TAG: stage ? `v0.1.5-${stage}.1` : "v0.1.5",
      IS_PRERELEASE: String(Boolean(stage)),
      RELEASE_STAGE: stage,
    };
    for (const name of ["Create GitHub release", "Publish npm package"]) {
      const result = spawnSync("bash", ["-e", "-c", step(name)], {
        cwd: dir,
        encoding: "utf8",
        env,
      });
      assert.equal(result.status, 0, result.stderr);
    }
    const args = readFileSync(capturePath, "utf8");
    assert.match(
      args,
      new RegExp(`create\\n${env.RELEASE_TAG.replaceAll(".", "\\.")}\\n`),
    );
    assert.match(args, /--notes-start-tag\nv0.1.4\n/);
    assert.doesNotMatch(args, /go(?:-cobra)?\//);
    assert.equal(args.includes("--prerelease\n"), Boolean(stage));
    assert.equal(args.includes("--latest=false\n"), Boolean(stage));
    assert.doesNotMatch(args, /--latest(?:=true)?\n/);
    assert.equal(args.includes(`--tag\n${stage}\n`), Boolean(stage));
  }
});

test("recovery reuses Go module tags only at the checked-out release commit", (t) => {
  const dir = fixture(t, "0.1.5", "0.1.5");
  const head = git(dir, "rev-parse", "HEAD");
  const other = git(
    dir,
    "commit-tree",
    git(dir, "mktree"),
    "-p",
    "HEAD",
    "-m",
    "workflow",
  );
  const env = {
    ...process.env,
    RELEASE_TAG: "v0.1.5",
    GITHUB_REF_NAME: "main",
    GITHUB_SHA: other,
  };
  for (const [name, tag] of [
    ["Publish Go core module tag", "go/v0.1.5"],
    ["Publish Go Cobra module tag", "go-cobra/v0.1.5"],
  ]) {
    git(dir, "tag", tag, head);
    const run = () =>
      spawnSync("bash", ["-e", "-c", step(name)], {
        cwd: dir,
        encoding: "utf8",
        env,
      });
    const valid = run();
    assert.equal(valid.status, 0, valid.stderr);
    git(dir, "tag", "-f", tag, other);
    assert.notEqual(run().status, 0);
    assert.equal(git(dir, "rev-parse", tag), other);
  }
});
