#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";

const root = new URL("../", import.meta.url);
const check = process.argv.includes("--check");
const hosts = JSON.stringify(JSON.parse(readFileSync(new URL("spec/hosts.json", root), "utf8")));

const files = new Map([
  [
    "ts/src/hosts.generated.ts",
    `// Code generated from spec/hosts.json. DO NOT EDIT.\n\n// prettier-ignore\nexport const defaultHostsSpecJson =\n  ${JSON.stringify(hosts)};\n`,
  ],
  [
    "go/hosts_gen.go",
    `// Code generated from spec/hosts.json. DO NOT EDIT.\n\npackage kitup\n\nconst defaultHostsSpecJSON = ${JSON.stringify(hosts)}\n`,
  ],
  [
    "rust/src/hosts_generated.rs",
    `// Code generated from spec/hosts.json. DO NOT EDIT.\n\npub(crate) const DEFAULT_HOSTS_SPEC_JSON: &str = ${rustString(hosts)};\n`,
  ],
  [
    "python/src/kitup/_hosts_generated.py",
    [
      "# Code generated from spec/hosts.json. DO NOT EDIT.",
      "",
      `DEFAULT_HOSTS_SPEC_JSON = ${JSON.stringify(hosts)}`,
      "",
    ].join("\n"),
  ],
]);

let drifted = false;
for (const [path, content] of files) {
  const url = new URL(path, root);
  if (check) {
    let current = "";
    try {
      current = readFileSync(url, "utf8");
    } catch {}
    if (current !== content) {
      console.error(`${path} drifted; run make generate`);
      drifted = true;
    }
  } else {
    writeFileSync(url, content);
  }
}

if (drifted) process.exit(1);

function rustString(value) {
  let hashes = "";
  while (value.includes(`"${hashes}`)) hashes += "#";
  return `r${hashes}"${value}"${hashes}`;
}
