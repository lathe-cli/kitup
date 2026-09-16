import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";
import { defaultHostsSpecJson } from "./hosts.generated.js";
import type {
  Scope,
  AgentSelector,
  Host,
  HostSpec,
  BaseOptions,
  TargetGroup,
  TargetError,
  UnknownHostError,
} from "./types.js";
import { isValidSkillName } from "./bundle.js";
import { exists, isDirectory, readMetadata } from "./storage.js";

type TargetOptions = BaseOptions & {
  agents?: AgentSelector;
  scope: Scope;
  skillName: string;
};
type TargetResolution = {
  targets: TargetGroup[];
  errors: TargetError[];
  detectedHostIds: string[];
};

export async function loadHostSpec(hostsFile?: string): Promise<HostSpec> {
  const spec = JSON.parse(
    hostsFile ? await readFile(hostsFile, "utf8") : defaultHostsSpecJson,
  ) as HostSpec;
  validateHostSpec(spec);
  return spec;
}

function validateHostSpec(spec: HostSpec) {
  for (const host of spec.hosts ?? []) {
    const installDirs = new Set([
      ...(host.projectSkillsDirs ?? []),
      ...(host.userSkillsDirs ?? []),
    ]);
    for (const [kind, paths, valid] of [
      ["project", host.projectSkillsDirs, isProjectHostPath],
      ["user", host.userSkillsDirs, isHomeHostPath],
      [
        "detect",
        host.detect,
        (path: string) => isHomeHostPath(path) || isProjectHostPath(path),
      ],
    ] as const) {
      for (const path of paths ?? []) {
        if (!valid(path))
          throw new Error(
            `invalid ${kind} path ${JSON.stringify(path)} for host ${host.id}`,
          );
        if (
          kind === "detect" &&
          !isGenericDetectPath(path) &&
          installDirs.has(path)
        )
          throw new Error(
            `detect path is an install target for host ${host.id}: ${JSON.stringify(path)}`,
          );
      }
    }
  }
}

function isProjectHostPath(path: string) {
  return (
    !!path &&
    !path.startsWith("/") &&
    !path.startsWith("~") &&
    isSafeHostPath(path)
  );
}

function isHomeHostPath(path: string) {
  return path.startsWith("~/") && isSafeHostPath(path.slice(2));
}

function isSafeHostPath(path: string) {
  return (
    !path.includes("\0") &&
    !path.includes("\\") &&
    !path.includes(":") &&
    !path
      .split("/")
      .some((segment) => segment === "." || segment === ".." || segment === "")
  );
}

export async function resolveHosts(options: {
  agents: AgentSelector;
  hostsFile?: string;
  hosts?: Host[];
}): Promise<{ hosts: Host[]; errors: UnknownHostError[] }> {
  const hosts = options.hosts ?? (await loadHostSpec(options.hostsFile)).hosts;
  if (options.agents === "*") return { hosts, errors: [] };
  if (options.agents === "auto") return { hosts: [], errors: [] };

  const byName = new Map<string, Host>();
  for (const host of hosts) {
    byName.set(host.id, host);
    for (const alias of host.aliases ?? []) byName.set(alias, host);
  }

  const resolvedHosts = new Map<string, Host>();
  const errors: UnknownHostError[] = [];
  for (const agent of options.agents) {
    const host = byName.get(agent);
    if (!host) {
      errors.push({ agent, reason: "unknown-host" });
    } else if (!resolvedHosts.has(host.id)) {
      resolvedHosts.set(host.id, host);
    }
  }
  return { hosts: [...resolvedHosts.values()], errors };
}

export async function detectHosts(
  options: BaseOptions & { scope?: Scope } = {},
): Promise<Host[]> {
  const spec = await loadHostSpec(options.hostsFile);
  const home = options.home ?? homedir();
  const cwd = options.cwd ?? process.cwd();
  const detected: Host[] = [];

  for (const host of spec.hosts) {
    for (const detectPath of host.detect) {
      if (isGenericDetectPath(detectPath)) continue;
      if (await exists(expandHostPath(detectPath, home, cwd))) {
        detected.push(host);
        break;
      }
    }
  }

  const scope = options.scope;
  if (!scope) return detected;
  return detected.sort((a, b) => {
    const aPath = canonicalScopePath(a, scope, home, cwd) ?? "";
    const bPath = canonicalScopePath(b, scope, home, cwd) ?? "";
    return aPath.localeCompare(bPath) || a.id.localeCompare(b.id);
  });
}

export async function resolveInstallTargets(
  options: TargetOptions,
): Promise<TargetResolution> {
  return resolveInstallTargetsForLifecycle(options);
}

export async function resolveInstallTargetsForLifecycle(
  options: TargetOptions,
  uninstallAppId?: string,
): Promise<TargetResolution> {
  if (!isValidSkillName(options.skillName)) {
    return {
      targets: [],
      errors: [{ skillName: options.skillName, reason: "invalid-skill-name" }],
      detectedHostIds: [],
    };
  }

  const spec = await loadHostSpec(options.hostsFile);
  const home = options.home ?? homedir();
  const cwd = options.cwd ?? process.cwd();
  const agents = options.agents ?? "auto";
  const resolved =
    agents === "auto"
      ? undefined
      : await resolveHosts({ agents, hosts: spec.hosts });
  const selected =
    agents === "auto"
      ? await detectHosts({ ...options, scope: options.scope })
      : resolved!.hosts;
  const errors: TargetError[] = agents === "auto" ? [] : [...resolved!.errors];
  const byTarget = new Map<string, TargetGroup>();

  for (const host of selected) {
    const paths = scopePaths(host, options.scope).map((path) =>
      expandHostPath(path, home, cwd),
    );
    const roots = await chooseScopePaths(
      paths,
      options.skillName,
      uninstallAppId,
    );
    if (roots.length === 0) {
      errors.push({
        hostId: host.id,
        skillName: options.skillName,
        scope: options.scope,
        reason: "unsupported-scope",
      });
      continue;
    }
    for (const root of roots) {
      const targetDir = join(root, options.skillName);
      const group = byTarget.get(targetDir) ?? {
        hostIds: [],
        skillName: options.skillName,
        targetDir,
      };
      if (!group.hostIds.includes(host.id)) group.hostIds.push(host.id);
      byTarget.set(targetDir, group);
    }
  }

  const targets = [...byTarget.values()].sort((a, b) =>
    a.targetDir.localeCompare(b.targetDir),
  );
  return {
    targets,
    errors,
    detectedHostIds: targets.flatMap((target) => target.hostIds),
  };
}

function canonicalScopePath(
  host: Host,
  scope: Scope,
  home: string,
  cwd: string,
) {
  const paths = scopePaths(host, scope);
  return paths[0] ? expandHostPath(paths[0], home, cwd) : undefined;
}

async function chooseScopePaths(
  paths: string[],
  skillName: string,
  appId?: string,
): Promise<string[]> {
  if (appId) {
    const owned: string[] = [];
    for (const root of paths) {
      const { value } = await readMetadata(join(root, skillName));
      if (value?.skillName === skillName && value.appId === appId)
        owned.push(root);
    }
    if (owned.length) return owned;
  }
  const existing: string[] = [];
  for (const root of paths) if (await isDirectory(root)) existing.push(root);
  for (const root of existing) {
    if (
      (await readMetadata(join(root, skillName))).value?.skillName === skillName
    )
      return [root];
  }
  const fallback = existing[0] ?? paths[0];
  return fallback ? [fallback] : [];
}

function scopePaths(host: Host, scope: Scope) {
  return scope === "user" ? host.userSkillsDirs : host.projectSkillsDirs;
}

function expandHostPath(path: string, home: string, cwd: string) {
  return path.startsWith("~/") ? join(home, path.slice(2)) : join(cwd, path);
}

const GENERIC_DETECT_PATHS = new Set([
  "~/.agents",
  "~/.agents/skills",
  "~/.config/agents",
  ".agents",
  ".agents/skills",
  "package.json",
]);

function isGenericDetectPath(path: string) {
  return GENERIC_DETECT_PATHS.has(path);
}
