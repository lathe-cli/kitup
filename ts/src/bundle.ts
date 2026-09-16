import { createHash } from "node:crypto";
import { readdir, readFile, stat } from "node:fs/promises";
import { join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import type {
  SkillBundle,
  SkillFile,
  SkillInfo,
  GitHubBundleOptions,
  BundledSkillMetadata,
  InstalledMetadata,
} from "./types.js";

const skillNamePattern = /^[a-z0-9]+(-[a-z0-9]+)*$/;

interface BundleFile {
  path: string;
  bytes: Uint8Array;
  mode: number;
}

export type NormalizedSkillBundle = BundleFile[];

export interface ResolvedBundleMetadata extends Omit<
  InstalledMetadata,
  "schemaVersion" | "appId" | "skillName" | "hash"
> {
  explicit?: boolean;
}

export function directoryBundle(path: string): SkillBundle {
  return { kind: "directory", path };
}

export function filesBundle(files: SkillFile[]): SkillBundle {
  return { kind: "files", files };
}

export async function moduleDirBundle(
  importMetaUrl: string | URL,
  relativePath: string,
): Promise<SkillBundle> {
  const root = fileURLToPath(new URL(relativePath, importMetaUrl));
  return filesBundle(await readDirectoryBundleFiles(root));
}

export function githubBundle(options: GitHubBundleOptions): SkillBundle {
  return { kind: "github", options };
}

export function withBundleMetadata(
  bundle: SkillBundle,
  metadata: BundledSkillMetadata,
): SkillBundle {
  return { kind: "metadata", bundle, metadata };
}

export function isValidSkillName(skillName: string) {
  return skillNamePattern.test(skillName);
}

export async function validateSkillBundle(
  bundle: SkillBundle,
  cwd = process.cwd(),
): Promise<SkillInfo> {
  try {
    return validateNormalizedSkill(await readSkillBundle(bundle, cwd));
  } catch {
    return { valid: false, errorCode: "invalid-skill-bundle" };
  }
}

export function validateNormalizedSkill(
  bundle: NormalizedSkillBundle,
): SkillInfo {
  const skillFile = bundle.find((file) => file.path === "SKILL.md");
  if (!skillFile) return { valid: false, errorCode: "missing-skill-md" };
  const content = Buffer.from(skillFile.bytes).toString("utf8");
  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!match) return { valid: false, errorCode: "invalid-frontmatter" };
  const frontmatter = parseFrontmatter(match[1]);
  const name = frontmatter.get("name") ?? "";
  const description = frontmatter.get("description") ?? "";
  if (!isValidSkillName(name)) {
    return { valid: false, errorCode: "invalid-frontmatter" };
  }
  if (description.length < 1 || description.length > 1024) {
    return { valid: false, errorCode: "invalid-frontmatter" };
  }
  return { valid: true, skillName: name, description };
}

export async function computeBundleContentHash(
  bundle: SkillBundle,
  cwd = process.cwd(),
): Promise<string> {
  return contentHash(await readSkillBundle(bundle, cwd));
}

export function contentHash(bundle: NormalizedSkillBundle) {
  const hash = createHash("sha256");
  for (const file of bundle) {
    hash.update(file.path).update("\0").update(file.bytes).update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

async function readSkillBundle(
  bundle: SkillBundle,
  cwd = process.cwd(),
): Promise<NormalizedSkillBundle> {
  return (await resolveSkillBundle(bundle, cwd)).bundle;
}

export async function resolveSkillBundle(
  bundle: SkillBundle,
  cwd = process.cwd(),
): Promise<{
  bundle: NormalizedSkillBundle;
  metadata: ResolvedBundleMetadata;
}> {
  if (bundle.kind === "metadata") {
    const resolved = await resolveSkillBundle(bundle.bundle, cwd);
    return {
      bundle: resolved.bundle,
      metadata: mergeBundleMetadata(resolved.metadata, bundle.metadata),
    };
  }
  if (bundle.kind === "github") return resolveGitHubBundle(bundle.options);
  const files =
    bundle.kind === "files"
      ? bundle.files
      : await readDirectoryBundleFiles(resolve(cwd, bundle.path));
  return {
    bundle: normalizeSkillFiles(files),
    metadata: { source: "bundled" },
  };
}

async function resolveGitHubBundle(options: GitHubBundleOptions): Promise<{
  bundle: NormalizedSkillBundle;
  metadata: ResolvedBundleMetadata;
}> {
  const root = options.path.replace(/^\/+|\/+$/g, "");
  if (!options.owner || !options.repo || !root || !options.ref) {
    throw new Error("invalid github bundle");
  }
  const apiBase = envBaseUrl(
    "KITUP_GITHUB_API_BASE_URL",
    "https://api.github.com",
  );
  const rawBase = envBaseUrl(
    "KITUP_GITHUB_RAW_BASE_URL",
    "https://raw.githubusercontent.com",
  );
  const commit: any = await (
    await githubResponse(
      `${apiBase}/repos/${encodeURIComponent(options.owner)}/${encodeURIComponent(options.repo)}/commits/${encodeURIComponent(options.ref)}`,
      "request",
    )
  ).json();
  const resolvedCommit = String(commit.sha ?? "");
  const treeSha = String(commit.commit?.tree?.sha ?? "");
  if (!resolvedCommit || !treeSha) throw new Error("invalid github commit");

  const tree: any = await (
    await githubResponse(
      `${apiBase}/repos/${encodeURIComponent(options.owner)}/${encodeURIComponent(options.repo)}/git/trees/${encodeURIComponent(treeSha)}?recursive=1`,
      "request",
    )
  ).json();
  const files: SkillFile[] = [];
  const prefix = `${root}/`;
  for (const item of tree.tree ?? []) {
    const path = String(item.path ?? "");
    if (item.type !== "blob" || !path.startsWith(prefix)) continue;
    const relativePath = path.slice(prefix.length);
    const url = `${rawBase}/${encodeURIComponent(options.owner)}/${encodeURIComponent(options.repo)}/${encodeURIComponent(resolvedCommit)}/${path.split("/").map(encodeURIComponent).join("/")}`;
    files.push({
      path: relativePath,
      contents: new Uint8Array(
        await (await githubResponse(url, "download")).arrayBuffer(),
      ),
      mode: item.mode === "100755" ? 0o755 : 0o644,
    });
  }
  if (files.length === 0) throw new Error("github bundle path not found");
  const bundle = normalizeSkillFiles(files);
  return {
    bundle,
    metadata: {
      source: "github",
      sourceId: `github:${options.owner}/${options.repo}/${root}`,
      version: options.ref,
      provenance: {
        owner: options.owner,
        repo: options.repo,
        path: root,
        ref: options.ref,
        resolvedCommit,
      },
    },
  };
}

function mergeBundleMetadata(
  resolved: ResolvedBundleMetadata,
  supplied: BundledSkillMetadata,
): ResolvedBundleMetadata {
  return {
    ...resolved,
    sourceId: supplied.sourceId || resolved.sourceId,
    cliVersion: supplied.cliVersion,
    cliRevision: supplied.cliRevision,
    provenance:
      resolved.provenance || supplied.provenance
        ? { ...resolved.provenance, ...supplied.provenance }
        : undefined,
    explicit: true,
  };
}

export function isGitHubBundle(bundle: SkillBundle): boolean {
  return (
    bundle.kind === "github" ||
    (bundle.kind === "metadata" && isGitHubBundle(bundle.bundle))
  );
}

function envBaseUrl(name: string, fallback: string) {
  return (process.env[name] ?? fallback).replace(/\/+$/, "");
}

async function githubResponse(url: string, operation: string) {
  const response = await fetch(url, {
    headers: { "User-Agent": "kitup" },
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok) throw new Error(`github ${operation} failed: ${url}`);
  return response;
}

async function readDirectoryBundleFiles(
  dir: string,
  base = dir,
): Promise<SkillFile[]> {
  const files: SkillFile[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    if (skipName(entry.name)) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await readDirectoryBundleFiles(full, base)));
    } else if (entry.isFile()) {
      files.push({
        path: relative(base, full).split(sep).join("/"),
        contents: await readFile(full),
        mode: (await stat(full)).mode & 0o777,
      });
    }
  }
  return files;
}

function normalizeSkillFiles(files: SkillFile[]): NormalizedSkillBundle {
  const byPath = new Map<string, BundleFile>();
  for (const file of files) {
    const normalizedPath = normalizeBundlePath(file.path);
    if (!normalizedPath) continue;
    if (byPath.has(normalizedPath)) {
      throw new Error(`duplicate skill file: ${normalizedPath}`);
    }
    byPath.set(normalizedPath, {
      path: normalizedPath,
      bytes:
        typeof file.contents === "string"
          ? Buffer.from(file.contents)
          : file.contents,
      mode:
        file.mode === undefined
          ? normalizedPath.startsWith("scripts/")
            ? 0o755
            : 0o644
          : file.mode,
    });
  }
  return [...byPath.values()].sort((a, b) => a.path.localeCompare(b.path));
}

function normalizeBundlePath(path: string) {
  const parts = path.split("/");
  if (
    path.includes("\\") ||
    /^[A-Za-z]:/.test(path) ||
    parts.some((part) => !part || part === "." || part === "..")
  ) {
    throw new Error(`invalid skill file path: ${path}`);
  }
  if (parts.some(skipName)) return undefined;
  return path;
}

function parseFrontmatter(content: string) {
  const values = new Map<string, string>();
  for (const line of content.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (match) values.set(match[1], match[2].trim());
  }
  return values;
}

function skipName(name: string) {
  return (
    name === ".git" ||
    name === ".kitup.json" ||
    name === ".DS_Store" ||
    name.endsWith(".swp") ||
    name.endsWith("~")
  );
}
