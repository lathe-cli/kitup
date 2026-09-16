import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { once } from "node:events";
import {
  chmod,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = fileURLToPath(new URL("../", import.meta.url));
const githubEnv = ["KITUP_GITHUB_API_BASE_URL", "KITUP_GITHUB_RAW_BASE_URL"];
const ignored = (name) =>
  [".git", ".kitup.json", ".DS_Store"].includes(name) ||
  name.endsWith(".swp") ||
  name.endsWith("~");

export async function runGoldenCases(
  execute,
  comparePaths = (a, b) => (a < b ? -1 : a > b ? 1 : 0),
) {
  const { cases } = JSON.parse(
    await readFile(
      join(repo, "testdata/cases/bundled-skill-install.json"),
      "utf8",
    ),
  );
  const root = await mkdtemp(join(tmpdir(), "kitup-golden-"));
  const servers = [];
  const previousEnv = githubEnv.map((key) => process.env[key]);
  try {
    const requests = [];
    const expectations = [];
    for (const testCase of cases) {
      const home = join(root, testCase.id, "home");
      const cwd = join(root, testCase.id, "workspace");
      await mkdir(home, { recursive: true });
      await mkdir(cwd, { recursive: true });
      const { options, given, expected } = expand(testCase, home, cwd);
      options.home = home;
      options.cwd = cwd;
      options.hostsFile = resolve(repo, options.hostsFile ?? "spec/hosts.json");
      if (options.skillBundleDir)
        options.skillBundleDir = resolve(repo, options.skillBundleDir);
      if (given.hostsFile) given.hostsFile = resolve(repo, given.hostsFile);
      const bundleDir =
        options.skillBundleDir ??
        join(repo, "testdata/skills", options.skillName ?? "");
      for (const dir of given.dirs ?? []) await mkdir(dir, { recursive: true });
      for (const [path, value] of Object.entries(given.files ?? {}))
        await writeFixture(path, value);
      if (given.copySkillBundleTo) {
        await rm(given.copySkillBundleTo, { recursive: true, force: true });
        await cp(bundleDir, given.copySkillBundleTo, { recursive: true });
      }
      for (const [path, mode] of Object.entries(given.fileModes ?? {}))
        await chmod(path, parseInt(mode, 8));
      for (const metadata of [given.metadata, expected.metadata]) {
        if (metadata)
          metadata.hash = await bundleHash(
            metadata.hash,
            options,
            given,
            bundleDir,
            comparePaths,
          );
      }
      if (given.metadata)
        await writeFixture(given.metadata.path, {
          ...given.metadata.fields,
          hash: given.metadata.hash,
        });
      const env = given.github
        ? await githubFixture(given.github, servers)
        : {};
      requests.push({
        id: testCase.id,
        operation: testCase.operation,
        options,
        given,
        detect: "detectedHosts" in expected,
        env,
      });
      expectations.push(expected);
    }

    let results;
    if (typeof execute === "function") {
      results = [];
      for (const request of requests) {
        setGithubEnv(request.env);
        try {
          results.push({ ...(await execute(request)), id: request.id });
        } catch (error) {
          results.push({ id: request.id, threw: true, error: String(error) });
        }
      }
    } else {
      const input = join(root, "requests.json");
      const output = join(root, "results.json");
      await writeFixture(input, requests);
      const child = spawn(execute[0], execute.slice(1), {
        env: {
          ...process.env,
          KITUP_GOLDEN_INPUT: input,
          KITUP_GOLDEN_OUTPUT: output,
        },
        stdio: "inherit",
      });
      const [code, signal] = await once(child, "exit");
      assert.equal(code, 0, `native golden adapter failed: ${signal ?? code}`);
      results = JSON.parse(await readFile(output, "utf8"));
    }
    assert.deepEqual(
      results.map((result) => result.id),
      cases.map((testCase) => testCase.id),
      "golden case coverage changed",
    );
    for (const [index, actual] of results.entries()) {
      try {
        await assertCase(actual, expectations[index]);
      } catch (error) {
        error.message = `${actual.id}: ${error.message}`;
        throw error;
      }
    }
    console.log(`ok: ${cases.length} shared golden cases`);
  } finally {
    setGithubEnv(
      Object.fromEntries(
        githubEnv.map((key, index) => [key, previousEnv[index]]),
      ),
    );
    await Promise.all(
      servers.map((server) => new Promise((done) => server.close(done))),
    );
    await rm(root, { recursive: true, force: true });
  }
}

async function assertCase(actual, expected) {
  assert.equal(actual.threw ?? false, expected.throws ?? false, actual.error);
  for (const key of [
    "count",
    "hostIds",
    "resolvedHostIds",
    "errors",
    "valid",
    "parsed",
    "targets",
    "exit",
    "output",
    "report",
    "detectedHosts",
  ]) {
    if (key in expected) assert.deepEqual(actual[key], expected[key], key);
  }
  if ("valid" in expected)
    assert.equal(actual.errorCode ?? null, expected.errorCode ?? null);
  if (expected.selection) {
    const selection = { ...actual.selection };
    const wanted = { ...expected.selection };
    for (const key of ["selected", "candidate"]) {
      if (`${key}Count` in wanted) {
        assert.equal(selection[`${key}HostIds`].length, wanted[`${key}Count`]);
        delete selection[`${key}HostIds`];
        delete wanted[`${key}Count`];
      }
    }
    assert.deepEqual(selection, wanted);
  }
  for (const [key, value] of Object.entries(expected.workflow ?? {}))
    assert.deepEqual(actual.workflow[key], value, `workflow.${key}`);
  for (const text of expected.outputContains ?? [])
    assert.ok(actual.output.includes(text), `missing output: ${text}`);
  for (const [key, present] of [
    ["filesPresent", true],
    ["filesAbsent", false],
  ]) {
    for (const path of expected[key] ?? []) {
      const exists = await stat(path).then(
        () => true,
        (error) => {
          if (error.code === "ENOENT") return false;
          throw error;
        },
      );
      assert.equal(exists, present, path);
    }
  }
  for (const [path, mode] of Object.entries(expected.fileModes ?? {}))
    assert.equal(((await stat(path)).mode & 0o777).toString(8), mode, path);
  if (expected.metadata) {
    const metadata = JSON.parse(await readFile(expected.metadata.path, "utf8"));
    for (const [key, value] of Object.entries({
      ...expected.metadata.fields,
      hash: expected.metadata.hash,
    }))
      assert.deepEqual(metadata[key], value, `metadata.${key}`);
  }
  if (expected.writeCountByTargetDir) {
    const counts = {};
    for (const item of [...actual.report.installed, ...actual.report.updated])
      counts[item.targetDir] = (counts[item.targetDir] ?? 0) + 1;
    assert.deepEqual(counts, expected.writeCountByTargetDir);
  }
}

async function bundleHash(marker, options, given, bundleDir, comparePaths) {
  let files;
  if (marker === "from-skill-bundle-dir")
    files = await directoryFiles(bundleDir);
  else if (marker === "from-skill-files") files = options.skillFiles;
  else if (marker === "from-github-bundle") {
    const prefix = options.githubBundle.path.replace(/^\/+|\/+$/g, "") + "/";
    files = Object.entries(given.github.files)
      .filter(([path]) => path.startsWith(prefix))
      .map(([path, contents]) => ({
        path: path.slice(prefix.length),
        contents,
      }));
  } else return marker;
  const hash = createHash("sha256");
  for (const file of files
    .filter((file) => !file.path.split("/").some(ignored))
    .sort((a, b) => comparePaths(a.path, b.path)))
    hash.update(file.path).update("\0").update(file.contents).update("\0");
  return `sha256:${hash.digest("hex")}`;
}

async function directoryFiles(root, prefix = "") {
  const files = [];
  for (const entry of await readdir(join(root, prefix), {
    withFileTypes: true,
  })) {
    if (ignored(entry.name)) continue;
    const path = prefix + entry.name;
    if (entry.isDirectory())
      files.push(...(await directoryFiles(root, `${path}/`)));
    else files.push({ path, contents: await readFile(join(root, path)) });
  }
  return files;
}

async function githubFixture(github, servers) {
  const routes = new Map([
    [
      `/repos/${github.owner}/${github.repo}/commits/${github.ref}`,
      JSON.stringify({
        sha: github.commit,
        commit: { tree: { sha: github.treeSha } },
      }),
    ],
    [
      `/repos/${github.owner}/${github.repo}/git/trees/${github.treeSha}`,
      JSON.stringify({
        tree: Object.keys(github.files).map((path) => ({
          path,
          type: "blob",
          mode: path.endsWith(".sh") ? "100755" : "100644",
        })),
      }),
    ],
    ...Object.entries(github.files).map(([path, contents]) => [
      `/${github.owner}/${github.repo}/${github.commit}/${path}`,
      contents,
    ]),
  ]);
  const server = createServer((request, response) => {
    const payload = routes.get(
      decodeURIComponent(new URL(request.url, "http://localhost").pathname),
    );
    response.writeHead(payload === undefined ? 404 : 200);
    response.end(payload ?? "not found");
  });
  servers.push(server);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const base = `http://127.0.0.1:${server.address().port}`;
  return Object.fromEntries(githubEnv.map((key) => [key, base]));
}

function setGithubEnv(values) {
  for (const key of githubEnv) {
    if (values[key] === undefined) delete process.env[key];
    else process.env[key] = values[key];
  }
}

function expand(value, home, cwd) {
  if (typeof value === "string")
    return value.replaceAll("$HOME", home).replaceAll("$WORKSPACE", cwd);
  if (Array.isArray(value)) return value.map((item) => expand(item, home, cwd));
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        expand(key, home, cwd),
        expand(item, home, cwd),
      ]),
    );
  return value;
}

async function writeFixture(path, value) {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(
    path,
    typeof value === "string" ? value : JSON.stringify(value, null, 2) + "\n",
  );
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  assert.equal(
    process.argv[2],
    "--",
    "Usage: node scripts/golden.mjs -- <native test command>",
  );
  await runGoldenCases(process.argv.slice(3));
}
