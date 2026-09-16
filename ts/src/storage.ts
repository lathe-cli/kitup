import {
  chmod,
  mkdir,
  mkdtemp,
  lstat,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import type { InstalledMetadata, ConflictReason } from "./types.js";
import {
  isValidSkillName,
  type NormalizedSkillBundle,
  type ResolvedBundleMetadata,
} from "./bundle.js";

type InstallMetadata = InstalledMetadata;
const metadataStrings = [
  "sourceId",
  "version",
  "cliVersion",
  "cliRevision",
] as const;

export async function readInstalledMetadata(
  targetDir: string,
): Promise<InstalledMetadata | undefined> {
  const metadata = await readMetadata(targetDir);
  if (!metadata.exists) return undefined;
  if (!metadata.value) throw new Error("unmanaged install metadata");
  return metadata.value;
}

export async function writeManagedSkill(
  bundle: NormalizedSkillBundle,
  targetDir: string,
  metadata: InstalledMetadata,
  replace: boolean,
) {
  const tmp = await makeStagingDir(targetDir);
  const backup = `${tmp}-backup`;
  try {
    await copySkillBundle(bundle, tmp);
    await writeMetadata(tmp, metadata);
    if (replace) await rename(targetDir, backup);
    await rename(tmp, targetDir);
    if (replace) await rm(backup, { recursive: true, force: true });
  } catch (error) {
    await rm(tmp, { recursive: true, force: true });
    if (replace && (await exists(backup)) && !(await exists(targetDir)))
      await rename(backup, targetDir);
    throw error;
  }
}

export async function removeManagedSkill(
  targetDir: string,
  appId: string,
  skillName: string,
): Promise<ConflictReason | undefined> {
  const quarantine = await mkdtemp(
    join(dirname(targetDir), `.${basename(targetDir)}.kitup-uninstall-`),
  );
  await rm(quarantine, { recursive: true });
  await rename(targetDir, quarantine);
  const metadata = await readMetadata(quarantine);
  const reason = ownershipConflict(metadata.value, appId, skillName);
  if (reason) {
    if (await exists(targetDir)) {
      throw new Error(`cannot restore changed install: ${targetDir}`);
    }
    await rename(quarantine, targetDir);
    return reason;
  }
  await rm(quarantine, { recursive: true });
  return undefined;
}

async function makeStagingDir(targetDir: string) {
  const parent = dirname(targetDir);
  await mkdir(parent, { recursive: true });
  const tmp = await mkdtemp(join(parent, `.${basename(targetDir)}.kitup-`));
  try {
    await chmod(tmp, 0o755);
    return tmp;
  } catch (error) {
    await rm(tmp, { recursive: true, force: true });
    throw error;
  }
}

async function copySkillBundle(bundle: NormalizedSkillBundle, dest: string) {
  await mkdir(dest, { recursive: true });
  for (const file of bundle) {
    const target = join(dest, file.path);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, file.bytes, { mode: file.mode });
    await chmod(target, file.mode);
  }
}

export async function repairSkillBundleModes(
  bundle: NormalizedSkillBundle,
  dest: string,
  write: boolean,
) {
  let repaired = false;
  for (const file of bundle) {
    const target = join(dest, file.path);
    let info;
    try {
      info = await lstat(target);
    } catch (error: any) {
      if (error.code === "ENOENT") continue;
      throw error;
    }
    if (info.isFile() && (info.mode & 0o777) !== file.mode) {
      repaired = true;
      if (write) await chmod(target, file.mode);
    }
  }
  return repaired;
}

export async function writeMetadata(
  targetDir: string,
  metadata: InstalledMetadata,
) {
  await writeFile(
    join(targetDir, ".kitup.json"),
    `${JSON.stringify(metadata, null, 2)}\n`,
  );
}

export function ownershipConflict(
  metadata: InstalledMetadata | undefined,
  appId: string,
  skillName: string,
): ConflictReason | undefined {
  if (!metadata || metadata.skillName !== skillName) return "unmanaged";
  if (metadata.appId !== appId) return "owner-mismatch";
}

export async function readMetadata(
  targetDir: string,
): Promise<{ exists: boolean; value?: InstallMetadata }> {
  if (!(await exists(targetDir))) return { exists: false };
  try {
    const raw = JSON.parse(
      await readFile(join(targetDir, ".kitup.json"), "utf8"),
    );
    const value = parseOwnedMetadata(raw);
    return value ? { exists: true, value } : { exists: true };
  } catch {
    return { exists: true };
  }
}

function parseOwnedMetadata(raw: unknown): InstallMetadata | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const value = raw as Record<string, unknown>;
  if (value.schemaVersion !== 1) return undefined;
  if (typeof value.appId !== "string" || value.appId.length === 0)
    return undefined;
  if (typeof value.skillName !== "string" || !isValidSkillName(value.skillName))
    return undefined;
  if (value.source !== "bundled" && value.source !== "github") return undefined;
  if (typeof value.hash !== "string" || value.hash.length === 0)
    return undefined;
  const metadata: InstallMetadata = {
    schemaVersion: 1,
    appId: value.appId,
    skillName: value.skillName,
    source: value.source,
    hash: value.hash,
  };
  for (const key of metadataStrings) {
    if (key in value && typeof value[key] !== "string") return undefined;
    if (value[key]) metadata[key] = value[key] as string;
  }
  if ("provenance" in value) {
    if (
      !value.provenance ||
      typeof value.provenance !== "object" ||
      Array.isArray(value.provenance) ||
      !Object.values(value.provenance).every((item) => typeof item === "string")
    ) {
      return undefined;
    }
    if (Object.keys(value.provenance).length > 0)
      metadata.provenance = {
        ...(value.provenance as Record<string, string>),
      };
  }
  return metadata;
}

export function installedMetadata(
  appId: string,
  skillName: string,
  hash: string,
  metadata: ResolvedBundleMetadata,
): InstalledMetadata {
  const value: InstalledMetadata = {
    schemaVersion: 1,
    appId,
    skillName,
    source: metadata.source,
    hash,
  };
  for (const key of metadataStrings)
    if (metadata[key]) value[key] = metadata[key];
  if (metadata.provenance) value.provenance = { ...metadata.provenance };
  return value;
}

export function installedMetadataEqual(
  left: InstalledMetadata,
  right: InstalledMetadata,
) {
  const keys = [
    "schemaVersion",
    "appId",
    "skillName",
    "source",
    "hash",
    ...metadataStrings,
  ] as const;
  return (
    keys.every((key) => left[key] === right[key]) &&
    recordEqual(left.provenance, right.provenance)
  );
}

function recordEqual(
  left: Record<string, string> | undefined,
  right: Record<string, string> | undefined,
) {
  const leftEntries = Object.entries(left ?? {}).sort();
  const rightEntries = Object.entries(right ?? {}).sort();
  return JSON.stringify(leftEntries) === JSON.stringify(rightEntries);
}

export async function exists(path: string) {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

export async function isDirectory(path: string) {
  try {
    return (await stat(path)).isDirectory();
  } catch {
    return false;
  }
}
