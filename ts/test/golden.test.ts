import assert from "node:assert/strict";
import { createServer } from "node:http";
import {
  mkdtemp,
  mkdir,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { Readable } from "node:stream";
import { fileURLToPath } from "node:url";
import {
  classifyInstallWorkflowExit,
  computeBundleContentHash,
  detectHosts,
  directoryBundle,
  filesBundle,
  githubBundle,
  installBundledSkill,
  loadHostSpec,
  planBundledSkill,
  parseInstallFlags,
  resolveInstallSelection,
  resolveHosts,
  runBundledSkillInstall,
  uninstallBundledSkill,
  updateBundledSkill,
  validateSkillBundle,
} from "../dist/index.js";

const repo = fileURLToPath(new URL("../../", import.meta.url));
const casesFile = join(repo, "testdata/cases/bundled-skill-install.json");
const hostsFile = join(repo, "spec/hosts.json");
const cases = JSON.parse(await readFile(casesFile, "utf8")).cases;

let passed = 0;
for (const testCase of cases) {
  const root = await mkdtemp(join(tmpdir(), `kitup-${testCase.id}-`));
  const home = join(root, "home");
  const workspace = join(root, "workspace");
  await mkdir(home, { recursive: true });
  await mkdir(workspace, { recursive: true });

  const cleanups: Array<() => Promise<void> | void> = [];
  try {
    await setupGiven(testCase, home, workspace, cleanups);
    await runCase(testCase, home, workspace);
    passed++;
  } finally {
    for (const cleanup of cleanups.reverse()) await cleanup();
    await rm(root, { recursive: true, force: true });
  }
}

console.log(`ok: ${passed} TypeScript golden cases`);

async function runCase(testCase: any, home: string, workspace: string) {
  const options = expandOptions(testCase.options, home, workspace);

  if (testCase.operation === "resolve-hosts") {
    const spec = await loadHostSpec(resolveRepoPath(testCase.given.hostsFile));
    const result = await resolveHosts({
      agents: options.agents,
      hosts: spec.hosts,
    });
    if (testCase.expected.count !== undefined)
      assert.equal(result.hosts.length, testCase.expected.count);
    if (testCase.expected.hostIds)
      assert.deepEqual(
        result.hosts.map((host) => host.id),
        testCase.expected.hostIds,
      );
    if (testCase.expected.resolvedHostIds) {
      assert.deepEqual(
        result.hosts.map((host) => host.id),
        testCase.expected.resolvedHostIds,
      );
    }
    if (testCase.expected.errors)
      assert.deepEqual(result.errors, testCase.expected.errors);
    return;
  }

  if (testCase.operation === "validate") {
    const result = await validateSkillBundle(options.skillBundle, options.cwd);
    assert.equal(result.valid, testCase.expected.valid);
    assert.equal(result.errorCode, testCase.expected.errorCode);
    return;
  }

  if (testCase.operation === "parse-install-flags") {
    assert.deepEqual(
      normalizeParsedFlags(parseInstallFlags(options)),
      testCase.expected.parsed,
    );
    return;
  }

  if (testCase.operation === "resolve-install-selection") {
    const selection = await resolveInstallSelection({
      ...options,
      hostsFile,
    });
    assertSelection(selection, testCase.expected.selection);
    return;
  }

  if (testCase.operation === "run-install-workflow") {
    const output = {
      text: "",
      write(chunk: string) {
        this.text += chunk;
      },
    };
    const workflow = await runBundledSkillInstall({
      ...options,
      hostsFile,
      input: Readable.from([options.input ?? ""]),
      output,
    });
    assertWorkflow(workflow, testCase.expected.workflow);
    if (testCase.expected.exit)
      assert.deepEqual(
        classifyInstallWorkflowExit(workflow),
        testCase.expected.exit,
      );
    assertOutput(output.text, testCase.expected.output);
    assertOutputContains(output.text, testCase.expected.outputContains);
    if (testCase.expected.report)
      assert.deepEqual(
        workflow.report,
        expandValue(testCase.expected.report, home, workspace),
      );
    await assertExpectedFiles(testCase, home, workspace);
    await assertExpectedMetadata(testCase, home, workspace);
    return;
  }

  if (testCase.expected.detectedHosts) {
    const detected = await detectHosts({
      home,
      cwd: workspace,
      hostsFile,
      scope: options.scope,
    });
    assert.deepEqual(
      detected.map((host) => host.id),
      testCase.expected.detectedHosts,
    );
  }

  const report =
    testCase.operation === "uninstall"
      ? await uninstallBundledSkill({ ...options, hostsFile })
      : testCase.operation === "plan"
        ? await planBundledSkill({ ...options, hostsFile })
        : testCase.operation === "update"
          ? await updateBundledSkill({ ...options, hostsFile })
          : await installBundledSkill({ ...options, hostsFile });

  if (testCase.expected.report)
    assert.deepEqual(
      report,
      expandValue(testCase.expected.report, home, workspace),
    );
  assertExpectedWriteCounts(testCase, report, home, workspace);
  await assertExpectedFiles(testCase, home, workspace);
  await assertExpectedMetadata(testCase, home, workspace);
}

async function setupGiven(
  testCase: any,
  home: string,
  workspace: string,
  cleanups: Array<() => Promise<void> | void>,
) {
  for (const dir of testCase.given.dirs ?? []) {
    await mkdir(expandValue(dir, home, workspace), { recursive: true });
  }

  for (const [path, value] of Object.entries(testCase.given.files ?? {})) {
    await writeFixtureFile(expandValue(path, home, workspace), value);
  }

  if (testCase.given.copySkillBundleTo) {
    await copyFixtureSkill(
      caseSkillBundleDir(testCase),
      expandValue(testCase.given.copySkillBundleTo, home, workspace),
    );
  }

  if (testCase.given.metadata)
    await writeMetadataFixture(
      testCase,
      home,
      workspace,
      testCase.given.metadata,
    );

  if (testCase.given.github)
    await startGitHubFixture(testCase.given.github, cleanups);
}

async function assertExpectedFiles(
  testCase: any,
  home: string,
  workspace: string,
) {
  for (const path of testCase.expected.filesPresent ?? []) {
    await assertFileExists(expandValue(path, home, workspace));
  }
  for (const path of testCase.expected.filesAbsent ?? []) {
    await assertFileMissing(expandValue(path, home, workspace));
  }
}

async function assertExpectedMetadata(
  testCase: any,
  home: string,
  workspace: string,
) {
  const expected = testCase.expected.metadata;
  if (!expected) return;
  const path = expandValue(expected.path, home, workspace);
  const actual = JSON.parse(await readFile(path, "utf8"));
  for (const [key, value] of Object.entries(expected.fields))
    assert.deepEqual(actual[key], value);
  const expectedHash = await expectedBundleHash(testCase, expected.hash);
  assert.equal(actual.hash, expectedHash);
}

function assertExpectedWriteCounts(
  testCase: any,
  report: any,
  home: string,
  workspace: string,
) {
  if (!testCase.expected.writeCountByTargetDir) return;
  const actual: Record<string, number> = {};
  for (const item of [...(report.installed ?? []), ...(report.updated ?? [])]) {
    actual[item.targetDir] = (actual[item.targetDir] ?? 0) + 1;
  }
  assert.deepEqual(
    actual,
    expandValue(testCase.expected.writeCountByTargetDir, home, workspace),
  );
}

async function writeMetadataFixture(
  testCase: any,
  home: string,
  workspace: string,
  metadata: any,
) {
  const path = expandValue(metadata.path, home, workspace);
  const hash = await expectedBundleHash(testCase, metadata.hash);
  await writeFixtureFile(path, { ...metadata.fields, hash });
}

async function expectedBundleHash(testCase: any, marker: string) {
  if (marker === "from-skill-bundle-dir")
    return computeBundleContentHash(
      directoryBundle(caseSkillBundleDir(testCase)),
    );
  if (marker === "from-skill-files")
    return computeBundleContentHash(filesBundle(testCase.options.skillFiles));
  if (marker === "from-github-bundle")
    return computeBundleContentHash(filesBundle(githubSkillFiles(testCase)));
  return marker;
}

async function startGitHubFixture(
  github: any,
  cleanups: Array<() => Promise<void> | void>,
) {
  const previousApi = process.env.KITUP_GITHUB_API_BASE_URL;
  const previousRaw = process.env.KITUP_GITHUB_RAW_BASE_URL;
  const server = createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    const path = decodeURIComponent(url.pathname);
    const commitPath = `/repos/${github.owner}/${github.repo}/commits/${github.ref}`;
    const treePath = `/repos/${github.owner}/${github.repo}/git/trees/${github.treeSha}`;
    if (path === commitPath) {
      writeJson(response, {
        sha: github.commit,
        commit: { tree: { sha: github.treeSha } },
      });
      return;
    }
    if (path === treePath) {
      writeJson(response, {
        tree: Object.keys(github.files).map((file) => ({
          path: file,
          type: "blob",
          mode: file.endsWith(".sh") ? "100755" : "100644",
        })),
      });
      return;
    }
    const rawPrefix = `/${github.owner}/${github.repo}/${github.commit}/`;
    if (path.startsWith(rawPrefix)) {
      const file = path.slice(rawPrefix.length);
      if (github.files[file] !== undefined) {
        response.writeHead(200, { "content-type": "application/octet-stream" });
        response.end(github.files[file]);
        return;
      }
    }
    response.writeHead(404);
    response.end("not found");
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("bad server");
  const base = `http://127.0.0.1:${address.port}`;
  process.env.KITUP_GITHUB_API_BASE_URL = base;
  process.env.KITUP_GITHUB_RAW_BASE_URL = base;
  cleanups.push(async () => {
    restoreEnv("KITUP_GITHUB_API_BASE_URL", previousApi);
    restoreEnv("KITUP_GITHUB_RAW_BASE_URL", previousRaw);
    await new Promise<void>((resolve) => server.close(() => resolve()));
  });
}

function writeJson(response: any, value: any) {
  response.writeHead(200, { "content-type": "application/json" });
  response.end(JSON.stringify(value));
}

function restoreEnv(name: string, value: string | undefined) {
  if (value === undefined) delete process.env[name];
  else process.env[name] = value;
}

async function writeFixtureFile(path: string, value: any) {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(
    path,
    typeof value === "string" ? value : `${JSON.stringify(value, null, 2)}\n`,
  );
}

async function copyFixtureSkill(src: string, dest: string) {
  const { cp } = await import("node:fs/promises");
  await rm(dest, { recursive: true, force: true });
  await cp(src, dest, { recursive: true });
}

async function assertFileExists(path: string) {
  await stat(path).catch((error) => {
    throw new Error(`expected file to exist: ${path}\n${error.message}`);
  });
}

async function assertFileMissing(path: string) {
  try {
    await stat(path);
  } catch (error: any) {
    if (error.code === "ENOENT") return;
    throw error;
  }
  throw new Error(`expected file to be absent: ${path}`);
}

function expandOptions(options: any, home: string, workspace: string) {
  const expanded = expandValue(options, home, workspace);
  if (expanded.skillBundleDir)
    expanded.skillBundle = directoryBundle(
      resolveRepoPath(expanded.skillBundleDir),
    );
  if (expanded.skillFiles)
    expanded.skillBundle = filesBundle(expanded.skillFiles);
  if (expanded.githubBundle)
    expanded.skillBundle = githubBundle(expanded.githubBundle);
  return expanded;
}

function expandValue(value: any, home: string, workspace: string): any {
  if (typeof value === "string")
    return value.replaceAll("$HOME", home).replaceAll("$WORKSPACE", workspace);
  if (Array.isArray(value))
    return value.map((item) => expandValue(item, home, workspace));
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        expandValue(key, home, workspace),
        expandValue(item, home, workspace),
      ]),
    );
  }
  return value;
}

function resolveRepoPath(path: string) {
  return path.startsWith("/") ? path : join(repo, path);
}

function caseSkillBundleDir(testCase: any) {
  return resolveRepoPath(
    testCase.options.skillBundleDir ??
      `testdata/skills/${testCase.options.skillName}`,
  );
}

function githubSkillFiles(testCase: any) {
  const root = testCase.options.githubBundle.path.replace(/^\/+|\/+$/g, "");
  const prefix = `${root}/`;
  return Object.entries(testCase.given.github.files)
    .filter(([path]) => path.startsWith(prefix))
    .map(([path, contents]) => ({
      path: path.slice(prefix.length),
      contents: contents as string,
    }));
}

function assertSelection(actual: any, expected: any) {
  const normalized = { ...actual };
  if (expected.selectedCount !== undefined) {
    assert.equal(normalized.selectedHostIds.length, expected.selectedCount);
    delete normalized.selectedHostIds;
  }
  if (expected.candidateCount !== undefined) {
    assert.equal(normalized.candidateHostIds.length, expected.candidateCount);
    delete normalized.candidateHostIds;
  }
  const normalizedExpected = { ...expected };
  delete normalizedExpected.selectedCount;
  delete normalizedExpected.candidateCount;
  assert.deepEqual(normalized, normalizedExpected);
}

function assertWorkflow(actual: any, expected: any) {
  if (!expected) return;
  for (const [key, value] of Object.entries(expected)) {
    assert.deepEqual(actual[key], value);
  }
}

function assertOutputContains(actual: string, expected: string[] = []) {
  for (const value of expected) {
    assert.ok(actual.includes(value), `expected output to contain ${value}`);
  }
}

function assertOutput(actual: string, expected: string | undefined) {
  if (expected !== undefined) assert.equal(actual, expected);
}

function normalizeParsedFlags(parsed: any) {
  return {
    scope: parsed.scope,
    scopeSet: parsed.scopeSet,
    agentKind: Array.isArray(parsed.agents) ? "explicit" : parsed.agents,
    agentIds: Array.isArray(parsed.agents) ? parsed.agents : [],
    yes: parsed.yes,
    dryRun: parsed.dryRun,
    force: parsed.force,
    errors: parsed.errors,
  };
}
