import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, mkdir, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import {
  computeBundleContentHash,
  directoryBundle,
  installBundledSkill,
  moduleDirBundle,
  validateSkillBundle,
} from "../dist/index.js";

const repo = fileURLToPath(new URL("../../", import.meta.url));
const basicSkill = new URL("../../testdata/skills/basic/", import.meta.url);
const skillMd = "---\nname: basic\ndescription: demo\n---\n";

{
  const bundle = await moduleDirBundle(
    import.meta.url,
    "../../testdata/skills/basic",
  );
  const result = await validateSkillBundle(bundle);
  assert.equal(result.valid, true);
  assert.equal(result.skillName, "basic");
  assert.equal(
    await computeBundleContentHash(bundle),
    await computeBundleContentHash(
      directoryBundle(join(repo, "testdata/skills/basic")),
    ),
  );
}

{
  const root = await mkdtemp(join(tmpdir(), "kitup-module-dir-skip-"));
  try {
    await writeFile(join(root, "SKILL.md"), skillMd);
    await writeFile(join(root, ".kitup.json"), '{"ignored":true}');
    await writeFile(join(root, "notes.txt~"), "backup");
    await writeFile(join(root, ".DS_Store"), "junk");

    const digest = await computeBundleContentHash(
      await moduleDirBundle(pathToFileURL(join(root, "caller.js")), "."),
    );
    const expected =
      "sha256:" +
      createHash("sha256")
        .update("SKILL.md")
        .update("\0")
        .update(skillMd)
        .update("\0")
        .digest("hex");
    assert.equal(digest, expected);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

{
  const root = await mkdtemp(join(tmpdir(), "kitup-module-dir-install-"));
  const home = join(root, "home");
  const workspace = join(root, "workspace");
  await mkdir(home, { recursive: true });
  await mkdir(workspace, { recursive: true });
  try {
    const report = await installBundledSkill({
      home,
      cwd: workspace,
      hostsFile: join(repo, "spec/hosts.json"),
      appId: "example-cli",
      skillBundle: await moduleDirBundle(basicSkill, "."),
      scope: "user",
      agents: ["codex"],
    });

    const target = join(home, ".agents/skills/basic");
    assert.equal(report.installed.length, 1);
    for (const relative of [
      "SKILL.md",
      "references/guide.md",
      "assets/template.json",
      "scripts/helper.sh",
    ]) {
      assert.ok((await stat(join(target, relative))).isFile());
    }
    assert.equal(
      (await stat(join(target, "scripts/helper.sh"))).mode & 0o777,
      0o755,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

{
  const root = await mkdtemp(join(tmpdir(), "kitup-module-dir-mode-"));
  const home = join(root, "home");
  const workspace = join(root, "workspace");
  const skill = join(root, "skill");
  await mkdir(home, { recursive: true });
  await mkdir(workspace, { recursive: true });
  await mkdir(join(skill, "scripts"), { recursive: true });
  await writeFile(join(skill, "SKILL.md"), skillMd);
  await writeFile(join(skill, "scripts/notes.txt"), "not executable\n");
  await chmod(join(skill, "scripts/notes.txt"), 0o644);
  try {
    await installBundledSkill({
      home,
      cwd: workspace,
      hostsFile: join(repo, "spec/hosts.json"),
      appId: "example-cli",
      skillBundle: await moduleDirBundle(
        pathToFileURL(join(skill, "caller.js")),
        ".",
      ),
      scope: "user",
      agents: ["codex"],
    });
    assert.equal(
      (await stat(join(home, ".agents/skills/basic/scripts/notes.txt"))).mode &
        0o777,
      0o644,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
}

console.log("ok: TypeScript moduleDirBundle cases");
