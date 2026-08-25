#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = new URL("../", import.meta.url);
const scratch = mkdtempSync(join(tmpdir(), "kitup-go-modules-"));
const core = join(scratch, "go");
const cobra = join(scratch, "go-cobra");
const consumer = join(scratch, "consumer");
const env = { ...process.env, GOWORK: "off" };
delete env.KITUP_TEST_REPO_ROOT;

try {
  cpSync(new URL("go/", root), core, { recursive: true });
  cpSync(new URL("go-cobra/", root), cobra, { recursive: true });

  if (existsSync(join(core, "testdata"))) {
    throw new Error(
      "standalone Go module must not contain copied repository testdata",
    );
  }

  run("go", ["test", "-count=1", "./..."], core);

  cpSync(
    new URL("tests/go-golden/golden_test.go", root),
    join(core, "golden_test.go"),
  );
  run("go", ["test", "-count=1", "./..."], core, {
    ...env,
    KITUP_TEST_REPO_ROOT: fileURLToPath(root),
  });
  console.log("ok: repository Go golden parity");

  run("go", ["test", "-mod=readonly", "-count=1", "./..."], cobra);

  run(
    "go",
    ["mod", "edit", "-replace=github.com/lathe-cli/kitup/go=../go"],
    cobra,
  );
  run("go", ["test", "-count=1", "./..."], cobra);

  mkdirSync(consumer);
  writeFileSync(
    join(consumer, "go.mod"),
    `module kitup-module-smoke

go 1.23

require github.com/lathe-cli/kitup/go-cobra v0.0.0

replace github.com/lathe-cli/kitup/go-cobra => ../go-cobra

replace github.com/lathe-cli/kitup/go => ../go
`,
  );
  writeFileSync(
    join(consumer, "main.go"),
    `package main

import (
	"io"

	kitup "github.com/lathe-cli/kitup/go"
	kitupcobra "github.com/lathe-cli/kitup/go-cobra"
)

func main() {
	_ = kitupcobra.NewSkillCommand(kitupcobra.Options{
		AppID: "module-smoke",
		Bundle: kitup.FilesBundle([]kitup.SkillFile{{
			Path:     "SKILL.md",
			Contents: []byte("---\\nname: module-smoke\\ndescription: Standalone module smoke fixture.\\n---\\n"),
		}}),
		Out: io.Discard,
	})
}
`,
  );
  run("go", ["mod", "tidy"], consumer);
  const modules = output(
    "go",
    [
      "list",
      "-m",
      "-f",
      "{{if .Replace}}{{.Path}}=>{{.Replace.Path}}{{end}}",
      "all",
    ],
    consumer,
  );
  for (const expected of [
    "github.com/lathe-cli/kitup/go=>../go",
    "github.com/lathe-cli/kitup/go-cobra=>../go-cobra",
  ]) {
    if (!modules.split("\n").includes(expected)) {
      throw new Error(`go list did not resolve ${expected}`);
    }
  }
  run("go", ["test", "./..."], consumer);
  run("go", ["build", "."], consumer);
} finally {
  rmSync(scratch, { recursive: true, force: true });
}

console.log("ok: standalone Go modules");

function run(command, args, cwd, commandEnv = env) {
  const result = spawnSync(command, args, {
    cwd,
    env: commandEnv,
    stdio: "inherit",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} exited ${result.status}`);
  }
}

function output(command, args, cwd) {
  const result = spawnSync(command, args, { cwd, env, encoding: "utf8" });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    process.stderr.write(result.stderr);
    throw new Error(`${command} ${args.join(" ")} exited ${result.status}`);
  }
  return result.stdout.trim();
}
