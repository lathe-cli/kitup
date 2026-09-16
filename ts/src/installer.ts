import type {
  InstallOptions,
  InstallReport,
  UninstallOptions,
  UninstallReport,
  StatusOptions,
  StatusReport,
  TargetGroup,
  TargetResult,
  TargetError,
} from "./types.js";
import {
  resolveSkillBundle,
  validateNormalizedSkill,
  contentHash,
  isGitHubBundle,
  type NormalizedSkillBundle,
  type ResolvedBundleMetadata,
} from "./bundle.js";
import {
  resolveInstallTargets,
  resolveInstallTargetsForLifecycle,
} from "./hosts.js";
import {
  readMetadata,
  writeManagedSkill,
  ownershipConflict,
  removeManagedSkill,
  repairSkillBundleModes,
  installedMetadata,
  installedMetadataEqual,
  writeMetadata,
} from "./storage.js";

export async function installBundledSkill(
  options: InstallOptions,
): Promise<InstallReport> {
  return installOrPlan(options, true);
}

export async function planBundledSkill(
  options: InstallOptions,
): Promise<InstallReport> {
  return installOrPlan(options, false);
}

async function installOrPlan(
  options: InstallOptions,
  write: boolean,
): Promise<InstallReport> {
  if (!options.appId) return emptyInstallReport([{ reason: "invalid-app-id" }]);

  const cwd = options.cwd ?? process.cwd();
  let bundle: NormalizedSkillBundle;
  let bundleMetadata: ResolvedBundleMetadata;
  try {
    ({ bundle, metadata: bundleMetadata } = await resolveSkillBundle(
      options.skillBundle,
      cwd,
    ));
  } catch {
    return emptyInstallReport([
      {
        reason: isGitHubBundle(options.skillBundle)
          ? "bundle-resolve-failed"
          : "invalid-skill-bundle",
      },
    ]);
  }
  const skill = validateNormalizedSkill(bundle);
  if (!skill.valid || !skill.skillName) {
    return emptyInstallReport([{ reason: skill.errorCode }]);
  }

  const hash = contentHash(bundle);
  const { targets, errors } = await resolveInstallTargets({
    ...options,
    skillName: skill.skillName,
  });
  const report = emptyInstallReport(errors);

  const expectedMetadata = installedMetadata(
    options.appId,
    skill.skillName,
    hash,
    bundleMetadata,
  );
  for (const target of targets) {
    const result = targetResult(target);
    const { exists, value } = await readMetadata(target.targetDir);
    const conflict = exists
      ? ownershipConflict(value, options.appId, skill.skillName)
      : undefined;
    if (conflict && !options.force) {
      report.conflicts.push({ ...result, reason: conflict });
      continue;
    }
    if (exists && !conflict && value!.hash === hash) {
      const repaired = await repairSkillBundleModes(
        bundle,
        target.targetDir,
        write,
      );
      const metadataChanged =
        bundleMetadata.explicit &&
        !installedMetadataEqual(value!, expectedMetadata);
      if (!repaired && !metadataChanged) {
        report.skipped.push({ ...result, reason: "unchanged" });
        continue;
      }
      if (write) await writeMetadata(target.targetDir, expectedMetadata);
    } else if (write) {
      await writeManagedSkill(
        bundle,
        target.targetDir,
        expectedMetadata,
        exists,
      );
    }
    report[exists ? "updated" : "installed"].push(result);
  }

  return report;
}

export async function updateBundledSkill(
  options: InstallOptions,
): Promise<InstallReport> {
  return installBundledSkill(options);
}

export async function uninstallBundledSkill(
  options: UninstallOptions,
): Promise<UninstallReport> {
  const report: UninstallReport = {
    removed: [],
    skipped: [],
    conflicts: [],
    errors: [],
  };
  for await (const { target, metadata } of ownedTargets(options, report)) {
    if (!metadata) {
      report.skipped.push({ ...target, reason: "missing" });
      continue;
    }
    const reason = await removeManagedSkill(
      target.targetDir,
      options.appId,
      options.skillName,
    );
    if (reason) report.conflicts.push({ ...target, reason });
    else report.removed.push(target);
  }
  return report;
}

export async function statusBundledSkill(
  options: StatusOptions,
): Promise<StatusReport> {
  const report: StatusReport = {
    installed: [],
    missing: [],
    conflicts: [],
    errors: [],
  };
  for await (const { target, metadata } of ownedTargets(options, report)) {
    if (metadata) report.installed.push({ ...target, metadata });
    else report.missing.push(target);
  }
  return report;
}

async function* ownedTargets(
  options: UninstallOptions,
  report: Pick<StatusReport, "conflicts" | "errors">,
) {
  if (!options.appId) {
    report.errors.push({ reason: "invalid-app-id" });
    return;
  }
  const { targets, errors } = await resolveInstallTargetsForLifecycle(
    options,
    options.appId,
  );
  report.errors.push(...errors);
  for (const group of targets) {
    const target = targetResult(group);
    const { exists, value } = await readMetadata(target.targetDir);
    const reason = exists
      ? ownershipConflict(value, options.appId, options.skillName)
      : undefined;
    if (reason) report.conflicts.push({ ...target, reason });
    else yield { target, metadata: value };
  }
}

function targetResult(target: TargetGroup): TargetResult {
  const base = { skillName: target.skillName, targetDir: target.targetDir };
  return target.hostIds.length === 1
    ? { hostId: target.hostIds[0], ...base }
    : { hostIds: target.hostIds, ...base };
}

export function emptyInstallReport(errors: TargetError[] = []): InstallReport {
  return { installed: [], updated: [], skipped: [], conflicts: [], errors };
}
