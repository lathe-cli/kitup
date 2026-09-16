import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Readable } from "node:stream";
import { fileURLToPath } from "node:url";
import * as kitup from "../dist/index.js";
import { runGoldenCases } from "../../scripts/golden.mjs";

const repo = fileURLToPath(new URL("../../", import.meta.url));
const defaultHostsFile = join(repo, "spec/hosts.json");

await runGoldenCases(
  async ({ operation, options, given, detect }: any) => {
    options = { ...options };
    if (options.skillBundleDir)
      options.skillBundle = kitup.directoryBundle(options.skillBundleDir);
    if (options.skillFiles)
      options.skillBundle = kitup.filesBundle(options.skillFiles);
    if (options.githubBundle)
      options.skillBundle = kitup.githubBundle(options.githubBundle);
    if (options.bundleMetadata)
      options.skillBundle = kitup.withBundleMetadata(
        options.skillBundle,
        options.bundleMetadata,
      );
    switch (operation) {
      case "resolve-hosts": {
        const { hosts } = await kitup.loadHostSpec(given.hostsFile);
        const result = await kitup.resolveHosts({
          agents: options.agents,
          hosts,
        });
        const hostIds = result.hosts.map((host) => host.id);
        return {
          count: hostIds.length,
          hostIds,
          resolvedHostIds: hostIds,
          errors: result.errors,
        };
      }
      case "validate":
        return kitup.validateSkillBundle(options.skillBundle, options.cwd);
      case "parse-install-flags": {
        const { agents, ...flags } = kitup.parseInstallFlags(options);
        return {
          parsed: {
            ...flags,
            agentKind: Array.isArray(agents) ? "explicit" : agents,
            agentIds: Array.isArray(agents) ? agents : [],
          },
        };
      }
      case "resolve-install-selection":
        return { selection: await kitup.resolveInstallSelection(options) };
      case "resolve-install-targets":
        return {
          targets: (await kitup.resolveInstallTargets(options)).targets,
        };
      case "run-install-workflow": {
        let output = "";
        const workflow = await kitup.runBundledSkillInstall({
          ...options,
          input: Readable.from([options.input ?? ""]),
          output: {
            write(chunk: string) {
              output += chunk;
            },
          },
        });
        return {
          workflow,
          report: workflow.report,
          exit: kitup.classifyInstallWorkflowExit(workflow),
          output,
        };
      }
      default: {
        const operations: Record<string, (options: any) => Promise<unknown>> = {
          install: kitup.installBundledSkill,
          update: kitup.updateBundledSkill,
          plan: kitup.planBundledSkill,
          status: kitup.statusBundledSkill,
          uninstall: kitup.uninstallBundledSkill,
        };
        const detectedHosts = detect
          ? (await kitup.detectHosts(options)).map((host) => host.id)
          : undefined;
        return { report: await operations[operation](options), detectedHosts };
      }
    }
  },
  (a, b) => a.localeCompare(b),
);

await assertConcurrentInitialInstallIsolation();

async function assertConcurrentInitialInstallIsolation() {
  const root = await mkdtemp(join(tmpdir(), "kitup-concurrent-install-"));
  const home = join(root, "home");
  const workspace = join(root, "workspace");
  await mkdir(home, { recursive: true });
  await mkdir(workspace, { recursive: true });

  const bundles = ["A", "B"].map((payload) =>
    kitup.filesBundle([
      {
        path: "SKILL.md",
        contents:
          "---\nname: concurrent\ndescription: Concurrent install fixture.\n---\n",
      },
      { path: "payload.txt", contents: payload },
    ]),
  );
  const originalNow = Date.now;
  Date.now = () => 1_700_000_000_000;
  try {
    const results = await Promise.allSettled(
      bundles.map((skillBundle, index) =>
        kitup.installBundledSkill({
          appId: `app-${index}`,
          skillBundle,
          scope: "user",
          agents: ["codex"],
          home,
          cwd: workspace,
          hostsFile: defaultHostsFile,
        }),
      ),
    );
    const installed = results.flatMap((result, index) =>
      result.status === "fulfilled" && result.value.installed.length === 1
        ? [index]
        : [],
    );
    assert.equal(installed.length, 1);
    const winner = installed[0];
    const target = join(home, ".agents/skills/concurrent");
    const metadata = JSON.parse(
      await readFile(join(target, ".kitup.json"), "utf8"),
    );
    assert.equal(metadata.appId, `app-${winner}`);
    assert.equal(
      await readFile(join(target, "payload.txt"), "utf8"),
      ["A", "B"][winner],
    );
    assert.deepEqual(await readdir(join(home, ".agents/skills")), [
      "concurrent",
    ]);
  } finally {
    Date.now = originalNow;
    await rm(root, { recursive: true, force: true });
  }
}
