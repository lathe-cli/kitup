#!/usr/bin/env node
import { cpSync, mkdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = new URL("../", import.meta.url);
const check = process.argv.slice(2).includes("--check");
const files = [
  "spec/hosts.json",
  "testdata/cases/bundled-skill-install.json",
  "testdata/skills/basic/SKILL.md",
  "testdata/skills/basic/assets/template.json",
  "testdata/skills/basic/references/guide.md",
  "testdata/skills/basic/scripts/helper.sh",
  "testdata/skills/invalid-frontmatter/SKILL.md",
  "testdata/skills/missing-skill-md/README.md",
];

for (const sourcePath of files) {
  const targetPath = `go/${sourcePath}`;
  const source = new URL(sourcePath, root);
  const target = new URL(targetPath, root);
  if (check) {
    let targetContents;
    try {
      targetContents = readFileSync(target);
    } catch {
      fail(`missing generated Go test fixture: ${targetPath}`);
    }
    if (!readFileSync(source).equals(targetContents)) {
      fail(`stale generated Go test fixture: ${targetPath}`);
    }
    continue;
  }
  mkdirSync(new URL("./", target), { recursive: true });
  cpSync(source, target);
}

console.log(
  check
    ? `ok: ${files.length} Go test fixtures are current`
    : `updated ${files.length} Go test fixtures`,
);

function fail(message) {
  console.error(message);
  process.exit(1);
}
