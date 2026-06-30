import { createHash } from "node:crypto";
import {
  mkdir,
  readdir,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { dirname, join, relative, resolve, sep } from "node:path";
import { defaultHostsSpecJson } from "./hosts.generated.js";

export type Scope = "user" | "project";
export type AgentSelector = "*" | "auto" | string[];

export const installUxText = {
  skillUse: "skill",
  skillShort: "Manage bundled Agent Skill",
  installUse: "install",
  installShort: "Install bundled Agent Skill",
  scopeFlag: "Install scope: user or project",
  agentFlag: "Target agent id. Repeat for multiple agents. Use '*' for all.",
  dryRunFlag: "Show install plan without writing",
  yesFlag: "Skip prompts and accept policy-selected targets",
  selectScope: "Select install scope:",
  scopePrompt: "Scope (user/project)",
  invalidScopeSelection: "Invalid scope selection.",
  selectAgents: "Select agents:",
  agentsPrompt: "Agents (numbers, ids, comma-separated, empty cancels)",
  invalidAgentSelection: "Invalid agent selection.",
  proceed: "Proceed? [y/N] ",
  installSummary: "Install summary:",
  errorPrefix: "kitup:",
  canceled: "Installation canceled.",
  selectionError: "Agent selection failed.",
  conflict: "Installation has conflicts.",
  failed: "Installation failed.",
  invalidFlags: "Invalid install flags.",
} as const;

export interface Host {
  id: string;
  displayName: string;
  aliases?: string[];
  projectSkillsDirs: string[];
  userSkillsDirs: string[];
  detect: string[];
  status: "verified" | "documented" | "community" | "experimental";
  notes?: string[];
}

export interface HostSpec {
  schemaVersion: 1;
  hosts: Host[];
}

export interface BaseOptions {
  home?: string;
  cwd?: string;
  hostsFile?: string;
}

export interface SkillFile {
  path: string;
  contents: string | Uint8Array;
  mode?: number;
}

export type SkillBundle =
  { kind: "directory"; path: string } | { kind: "files"; files: SkillFile[] };

export interface InstallOptions extends BaseOptions {
  appId: string;
  skillBundle: SkillBundle;
  scope: Scope;
  agents?: AgentSelector;
}

export interface UninstallOptions extends BaseOptions {
  appId: string;
  skillName: string;
  scope: Scope;
  agents?: AgentSelector;
}

export interface InstallSelectionOptions extends BaseOptions {
  scope: Scope;
  agents?: AgentSelector;
  yes?: boolean;
  stdinTTY?: boolean;
  currentAgent?: string;
}

export interface InstallWorkflowOptions extends InstallOptions {
  yes?: boolean;
  dryRun?: boolean;
  stdinTTY?: boolean;
  currentAgent?: string;
  defaultScope?: Scope;
  scopeSet?: boolean;
  promptScope?: boolean;
  input?: AsyncIterable<string | Uint8Array>;
  output?: { write(chunk: string): unknown };
}

export interface InstallFlagValues {
  scope?: string;
  scopeSet?: boolean;
  agents?: string[];
  yes?: boolean;
  dryRun?: boolean;
}

export interface InstallFlagError {
  flag: string;
  reason: string;
  value?: string;
}

export interface ParsedInstallFlags {
  scope: Scope;
  scopeSet: boolean;
  agents: AgentSelector;
  yes: boolean;
  dryRun: boolean;
  errors: InstallFlagError[];
}

export interface TargetGroup {
  hostIds: string[];
  skillName: string;
  targetDir: string;
}

export type TargetResult =
  | {
      hostId: string;
      skillName: string;
      targetDir: string;
    }
  | {
      hostIds: string[];
      skillName: string;
      targetDir: string;
    };

export type SkipReason = "unchanged" | "missing";
export type ConflictReason = "unmanaged" | "owner-mismatch";
export type TargetConflict = TargetResult & { reason: ConflictReason };
export type TargetSkip = TargetResult & { reason: SkipReason };
export type UnknownHostError = { agent: string; reason: "unknown-host" };
export type UnsupportedScopeError = {
  hostId: string;
  skillName: string;
  scope: Scope;
  reason: "unsupported-scope";
};
export type SkillError = { reason: SkillInfo["errorCode"] };
export type TargetError = UnknownHostError | UnsupportedScopeError | SkillError;

export interface InstallReport {
  installed: TargetResult[];
  updated: TargetResult[];
  skipped: TargetSkip[];
  conflicts: TargetConflict[];
  errors: TargetError[];
}

export interface UninstallReport {
  removed: TargetResult[];
  skipped: TargetSkip[];
  conflicts: TargetConflict[];
  errors: TargetError[];
}

export type InstallSelectionAction = "install" | "select-agents" | "error";

export interface InstallSelection {
  action: InstallSelectionAction;
  selectedHostIds: string[];
  candidateHostIds: string[];
  detectedHostIds: string[];
  needsConfirmation: boolean;
  errors: Array<{ reason: string; agent?: string }>;
}

export interface InstallWorkflowReport {
  selection: InstallSelection;
  scope: Scope | "";
  plan: InstallReport;
  report: InstallReport;
  canceled: boolean;
  dryRun: boolean;
}

export type InstallWorkflowExitCode =
  "ok" | "canceled" | "selection-error" | "conflict" | "error";

export interface InstallWorkflowExit {
  ok: boolean;
  code: InstallWorkflowExitCode;
  message: string;
}

export interface SkillInfo {
  valid: boolean;
  skillName?: string;
  description?: string;
  errorCode?:
    "missing-skill-md" | "invalid-frontmatter" | "invalid-skill-bundle";
}

interface InstallMetadata {
  schemaVersion: 1;
  appId: string;
  skillName: string;
  source: "bundled";
  hash: string;
}

const source = "bundled";
const defaultAgents: AgentSelector = "auto";

interface BundleFile {
  path: string;
  bytes: Uint8Array;
  mode: number;
}

interface NormalizedSkillBundle {
  label?: string;
  files: BundleFile[];
  byPath: Map<string, BundleFile>;
}

export function directoryBundle(path: string): SkillBundle {
  return { kind: "directory", path };
}

export function filesBundle(files: SkillFile[]): SkillBundle {
  return { kind: "files", files };
}

export function parseInstallFlags(
  flags: InstallFlagValues,
): ParsedInstallFlags {
  const errors: InstallFlagError[] = [];
  const scope = parseScopeFlag(flags.scope, errors);
  const agents = agentSelectorFromFlags(flags.agents ?? [], errors);
  return {
    scope,
    scopeSet: flags.scopeSet ?? flags.scope !== undefined,
    agents,
    yes: Boolean(flags.yes),
    dryRun: Boolean(flags.dryRun),
    errors,
  };
}

export function agentSelectorFromFlags(
  values: string[],
  errors: InstallFlagError[] = [],
): AgentSelector {
  const agents = splitFlagValues(values);
  if (agents.length === 0) return "auto";
  if (agents.includes("*")) {
    if (agents.length > 1) {
      errors.push({
        flag: "agent",
        reason: "agent-star-must-be-alone",
        value: agents.join(","),
      });
    }
    return "*";
  }
  return [...new Set(agents)];
}

export function parseScopeFlag(
  value: string | undefined,
  errors: InstallFlagError[] = [],
): Scope {
  if (!value || value === "user") return "user";
  if (value === "project") return "project";
  errors.push({ flag: "scope", reason: "invalid-scope", value });
  return "user";
}

function splitFlagValues(values: string[]) {
  return values
    .flatMap((value) => value.split(/[,\s]+/))
    .map((value) => value.trim())
    .filter(Boolean);
}

export function classifyInstallWorkflowExit(
  workflow: InstallWorkflowReport,
): InstallWorkflowExit {
  if (workflow.canceled) {
    return { ok: false, code: "canceled", message: installUxText.canceled };
  }
  if (workflow.selection.errors.length > 0) {
    return {
      ok: false,
      code: "selection-error",
      message: installUxText.selectionError,
    };
  }
  if (workflow.report.conflicts.length > 0) {
    return { ok: false, code: "conflict", message: installUxText.conflict };
  }
  if (workflow.report.errors.length > 0) {
    return { ok: false, code: "error", message: installUxText.failed };
  }
  return { ok: true, code: "ok", message: "" };
}

export function installWorkflowError(
  workflow: InstallWorkflowReport,
): Error | undefined {
  const exit = classifyInstallWorkflowExit(workflow);
  return exit.ok || exit.code === "canceled"
    ? undefined
    : new Error(exit.message);
}

export function installFlagError(
  errors: InstallFlagError[],
): Error | undefined {
  return errors.length === 0
    ? undefined
    : new Error(installUxText.invalidFlags);
}

export async function loadHostSpec(hostsFile?: string): Promise<HostSpec> {
  return JSON.parse(
    hostsFile ? await readFile(hostsFile, "utf8") : defaultHostsSpecJson,
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

  const resolvedHosts: Host[] = [];
  const errors: UnknownHostError[] = [];
  const seen = new Set<string>();
  for (const agent of options.agents) {
    const host = byName.get(agent);
    if (!host) {
      errors.push({ agent, reason: "unknown-host" });
    } else if (!seen.has(host.id)) {
      seen.add(host.id);
      resolvedHosts.push(host);
    }
  }
  return { hosts: resolvedHosts, errors };
}

export async function detectHosts(
  options: BaseOptions & { scope?: Scope } = {},
): Promise<Host[]> {
  const spec = await loadHostSpec(options.hostsFile);
  const home = options.home ?? process.env.HOME ?? "";
  const cwd = options.cwd ?? process.cwd();
  const detected: Host[] = [];

  for (const host of spec.hosts) {
    const detectPath = host.detect[0];
    if (
      detectPath &&
      !isGenericDetectPath(detectPath) &&
      (await exists(expandHostPath(detectPath, home, cwd)))
    ) {
      detected.push(host);
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

export async function resolveInstallSelection(
  options: InstallSelectionOptions,
): Promise<InstallSelection> {
  const hosts = (await loadHostSpec(options.hostsFile)).hosts;
  const stdinTTY = options.stdinTTY ?? Boolean(process.stdin.isTTY);
  const explicitAgents =
    options.agents !== undefined && options.agents !== "auto";

  if (options.currentAgent && !explicitAgents) {
    const { hosts: selected, errors } = await resolveHosts({
      agents: [options.currentAgent],
      hosts,
    });
    const withUniversal = addUniversalHost(selected, hosts);
    return installSelection(
      withUniversal.map((host) => host.id),
      [],
      !options.yes && stdinTTY,
      errors.map((error) => ({ ...error })),
    );
  }

  if (explicitAgents) {
    if (options.agents === "*") {
      return installSelection(
        hosts.map((host) => host.id),
        [],
        !options.yes && stdinTTY,
      );
    }
    const resolved = await resolveHosts({ agents: options.agents!, hosts });
    if (resolved.errors.length > 0) {
      return errorSelection(
        resolved.errors.map((error) => ({
          reason: error.reason,
          agent: error.agent,
        })),
      );
    }
    return installSelection(
      resolved.hosts.map((host) => host.id),
      [],
      !options.yes && stdinTTY,
    );
  }

  const detected = await detectHosts({ ...options, scope: options.scope });
  const detectedHostIds = detected.map((host) => host.id);

  if (!stdinTTY && !options.yes) {
    return errorSelection(
      [{ reason: "agent-selection-required" }],
      detectedHostIds,
    );
  }

  if (options.yes) {
    if (detected.length === 0) {
      return errorSelection([{ reason: "no-detected-hosts" }], detectedHostIds);
    }
    return installSelection(detectedHostIds, detectedHostIds, false);
  }

  if (detected.length === 0) {
    return selectAgentsSelection(
      hosts.map((host) => host.id),
      detectedHostIds,
      [],
    );
  }
  if (detected.length === 1) {
    return installSelection(detectedHostIds, detectedHostIds, true);
  }
  return selectAgentsSelection(detectedHostIds, detectedHostIds, []);
}

export async function runBundledSkillInstall(
  options: InstallWorkflowOptions,
): Promise<InstallWorkflowReport> {
  const stdinTTY = options.stdinTTY ?? Boolean(process.stdin.isTTY);
  const output = options.output ?? process.stdout;
  const input =
    options.input ??
    (process.stdin as unknown as AsyncIterable<string | Uint8Array>);
  const reader = new LineReader(input);
  const scopeResult = await resolveWorkflowScope(
    reader,
    output,
    options.scope,
    options.scopeSet ?? options.scope !== undefined,
    Boolean(options.promptScope),
    options.defaultScope,
    Boolean(options.yes),
    stdinTTY,
  );
  if (scopeResult.selection) {
    renderSelectionErrors(output, scopeResult.selection);
    return {
      selection: scopeResult.selection,
      scope: scopeResult.scope,
      plan: emptyInstallReport(),
      report: emptyInstallReport(),
      canceled: false,
      dryRun: Boolean(options.dryRun),
    };
  }
  const scope = scopeResult.scope as Scope;
  let selection = await resolveInstallSelection({
    ...options,
    scope,
    stdinTTY,
    yes: options.yes,
    currentAgent: options.currentAgent,
  });

  if (selection.action === "error") {
    renderSelectionErrors(output, selection);
    return {
      selection,
      scope,
      plan: emptyInstallReport(),
      report: emptyInstallReport(),
      canceled: false,
      dryRun: Boolean(options.dryRun),
    };
  }

  if (selection.action === "select-agents") {
    const hosts = (await loadHostSpec(options.hostsFile)).hosts;
    const selectedHostIds = await promptAgentSelection(
      reader,
      output,
      selection,
      hosts,
    );
    selection = installSelection(
      selectedHostIds,
      selection.detectedHostIds,
      !options.yes && stdinTTY,
    );
    if (selectedHostIds.length === 0) {
      return {
        selection,
        scope,
        plan: emptyInstallReport(),
        report: emptyInstallReport(),
        canceled: true,
        dryRun: Boolean(options.dryRun),
      };
    }
  }

  const installOptions: InstallOptions = {
    ...options,
    scope,
    agents: selection.selectedHostIds,
  };
  const plan = await planBundledSkill(installOptions);
  if (!hasVisibleInstallPlan(plan)) {
    return {
      selection,
      scope,
      plan,
      report: plan,
      canceled: false,
      dryRun: Boolean(options.dryRun),
    };
  }
  renderInstallSummary(output, plan);

  if (options.dryRun) {
    return {
      selection,
      scope,
      plan,
      report: plan,
      canceled: false,
      dryRun: true,
    };
  }

  if (!hasInstallWrites(plan)) {
    return {
      selection,
      scope,
      plan,
      report: plan,
      canceled: false,
      dryRun: false,
    };
  }

  if (selection.needsConfirmation) {
    const confirmed = await promptConfirmation(reader, output);
    if (!confirmed) {
      return {
        selection,
        scope,
        plan,
        report: emptyInstallReport(),
        canceled: true,
        dryRun: false,
      };
    }
  }

  const report = await installBundledSkill(installOptions);
  return { selection, scope, plan, report, canceled: false, dryRun: false };
}

export async function resolveInstallTargets(
  options: BaseOptions & {
    agents?: AgentSelector;
    scope: Scope;
    skillName: string;
  },
): Promise<{
  targets: TargetGroup[];
  errors: TargetError[];
  detectedHostIds: string[];
}> {
  const spec = await loadHostSpec(options.hostsFile);
  const home = options.home ?? process.env.HOME ?? "";
  const cwd = options.cwd ?? process.cwd();
  const agents = options.agents ?? defaultAgents;
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
    const root = await chooseScopePath(host, options.scope, home, cwd);
    if (!root) {
      errors.push({
        hostId: host.id,
        skillName: options.skillName,
        scope: options.scope,
        reason: "unsupported-scope",
      });
      continue;
    }
    const targetDir = join(root, options.skillName);
    const group = byTarget.get(targetDir) ?? {
      hostIds: [],
      skillName: options.skillName,
      targetDir,
    };
    group.hostIds.push(host.id);
    byTarget.set(targetDir, group);
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

function addUniversalHost(selected: Host[], hosts: Host[]) {
  const result = [...selected];
  const universal = hosts.find((host) => host.id === "universal");
  if (universal && !result.some((host) => host.id === universal.id)) {
    result.push(universal);
  }
  return result;
}

function installSelection(
  selectedHostIds: string[],
  detectedHostIds: string[] = [],
  needsConfirmation: boolean,
  errors: InstallSelection["errors"] = [],
): InstallSelection {
  return {
    action: errors.length > 0 ? "error" : "install",
    selectedHostIds,
    candidateHostIds: [],
    detectedHostIds,
    needsConfirmation: errors.length > 0 ? false : needsConfirmation,
    errors,
  };
}

function selectAgentsSelection(
  candidateHostIds: string[],
  detectedHostIds: string[],
  selectedHostIds: string[],
): InstallSelection {
  return {
    action: "select-agents",
    selectedHostIds,
    candidateHostIds,
    detectedHostIds,
    needsConfirmation: true,
    errors: [],
  };
}

function errorSelection(
  errors: InstallSelection["errors"],
  detectedHostIds: string[] = [],
): InstallSelection {
  return {
    action: "error",
    selectedHostIds: [],
    candidateHostIds: [],
    detectedHostIds,
    needsConfirmation: false,
    errors,
  };
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

function validateNormalizedSkill(bundle: NormalizedSkillBundle): SkillInfo {
  const skillFile = bundle.byPath.get("SKILL.md");
  if (!skillFile) return { valid: false, errorCode: "missing-skill-md" };
  const content = Buffer.from(skillFile.bytes).toString("utf8");
  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!match) return { valid: false, errorCode: "invalid-frontmatter" };
  const frontmatter = parseFrontmatter(match[1]);
  const name = frontmatter.get("name") ?? "";
  const description = frontmatter.get("description") ?? "";
  if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(name)) {
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

function contentHash(bundle: NormalizedSkillBundle) {
  const hash = createHash("sha256");
  for (const file of bundle.files) {
    const bytes = file.bytes;
    hash.update(file.path);
    hash.update("\0");
    hash.update(bytes);
    hash.update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

async function readSkillBundle(
  bundle: SkillBundle,
  cwd = process.cwd(),
): Promise<NormalizedSkillBundle> {
  if (bundle.kind === "directory") {
    const dir = resolvePath(bundle.path, cwd);
    return normalizeSkillFiles(await readDirectoryBundleFiles(dir), dir);
  }
  return normalizeSkillFiles(bundle.files);
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

function normalizeSkillFiles(
  files: SkillFile[],
  label?: string,
): NormalizedSkillBundle {
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
      mode: file.mode ?? 0o644,
    });
  }
  const normalizedFiles = [...byPath.values()].sort((a, b) =>
    a.path.localeCompare(b.path),
  );
  return { label, files: normalizedFiles, byPath };
}

function normalizeBundlePath(path: string) {
  if (!path || path.includes("\\") || path.startsWith("/")) {
    throw new Error(`invalid skill file path: ${path}`);
  }
  if (/^[A-Za-z]:/.test(path)) {
    throw new Error(`invalid skill file path: ${path}`);
  }
  const parts = path.split("/");
  if (parts.some((part) => !part || part === "." || part === "..")) {
    throw new Error(`invalid skill file path: ${path}`);
  }
  if (parts.some(skipName)) return undefined;
  return parts.join("/");
}

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
  const cwd = options.cwd ?? process.cwd();
  let bundle: NormalizedSkillBundle;
  try {
    bundle = await readSkillBundle(options.skillBundle, cwd);
  } catch {
    return emptyInstallReport([{ reason: "invalid-skill-bundle" }]);
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

  for (const target of targets) {
    const result = targetResult(target);
    const metadata = await readMetadata(target.targetDir);
    if (!metadata.exists) {
      if (write) {
        await copyManagedSkill(
          bundle,
          target.targetDir,
          options.appId,
          skill.skillName,
          hash,
        );
      }
      report.installed.push(result);
    } else if (!metadata.value) {
      report.conflicts.push({ ...result, reason: "unmanaged" });
    } else if (metadata.value.appId !== options.appId) {
      report.conflicts.push({ ...result, reason: "owner-mismatch" });
    } else if (metadata.value.hash === hash) {
      report.skipped.push({ ...result, reason: "unchanged" });
    } else {
      if (write) {
        await replaceManagedSkill(
          bundle,
          target.targetDir,
          options.appId,
          skill.skillName,
          hash,
        );
      }
      report.updated.push(result);
    }
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
  const { targets, errors } = await resolveInstallTargets({
    ...options,
    skillName: options.skillName,
  });
  const report: UninstallReport = {
    removed: [],
    skipped: [],
    conflicts: [],
    errors,
  };

  for (const target of targets) {
    const result = targetResult(target);
    const metadata = await readMetadata(target.targetDir);
    if (!metadata.exists) {
      report.skipped.push({ ...result, reason: "missing" });
    } else if (!metadata.value) {
      report.conflicts.push({ ...result, reason: "unmanaged" });
    } else if (metadata.value.appId !== options.appId) {
      report.conflicts.push({ ...result, reason: "owner-mismatch" });
    } else {
      await rm(target.targetDir, { recursive: true, force: true });
      report.removed.push(result);
    }
  }

  return report;
}

async function copyManagedSkill(
  bundle: NormalizedSkillBundle,
  targetDir: string,
  appId: string,
  skillName: string,
  hash: string,
) {
  await rm(targetDir, { recursive: true, force: true });
  await copySkillBundle(bundle, targetDir);
  await writeMetadata(targetDir, appId, skillName, hash);
}

async function replaceManagedSkill(
  bundle: NormalizedSkillBundle,
  targetDir: string,
  appId: string,
  skillName: string,
  hash: string,
) {
  const suffix = `.kitup-${process.pid}-${Date.now()}`;
  const tmp = `${targetDir}${suffix}`;
  const backup = `${targetDir}${suffix}-backup`;
  await rm(tmp, { recursive: true, force: true });
  await copySkillBundle(bundle, tmp);
  await writeMetadata(tmp, appId, skillName, hash);
  try {
    await rename(targetDir, backup);
    await rename(tmp, targetDir);
    await rm(backup, { recursive: true, force: true });
  } catch (error) {
    await rm(tmp, { recursive: true, force: true });
    if ((await exists(backup)) && !(await exists(targetDir)))
      await rename(backup, targetDir);
    throw error;
  }
}

async function copySkillBundle(bundle: NormalizedSkillBundle, dest: string) {
  await mkdir(dest, { recursive: true });
  for (const file of bundle.files) {
    const target = join(dest, file.path);
    await mkdir(dirname(target), { recursive: true });
    await writeFile(target, file.bytes, { mode: file.mode });
  }
}

async function writeMetadata(
  targetDir: string,
  appId: string,
  skillName: string,
  hash: string,
) {
  await writeFile(
    join(targetDir, ".kitup.json"),
    `${JSON.stringify({ schemaVersion: 1, appId, skillName, source, hash }, null, 2)}\n`,
  );
}

async function readMetadata(
  targetDir: string,
): Promise<{ exists: boolean; value?: InstallMetadata }> {
  if (!(await exists(targetDir))) return { exists: false };
  try {
    return {
      exists: true,
      value: JSON.parse(await readFile(join(targetDir, ".kitup.json"), "utf8")),
    };
  } catch {
    return { exists: true };
  }
}

function targetResult(target: TargetGroup): TargetResult {
  const base = { skillName: target.skillName, targetDir: target.targetDir };
  return target.hostIds.length === 1
    ? { hostId: target.hostIds[0], ...base }
    : { hostIds: target.hostIds, ...base };
}

function emptyInstallReport(errors: TargetError[] = []): InstallReport {
  return { installed: [], updated: [], skipped: [], conflicts: [], errors };
}

function hasVisibleInstallPlan(report: InstallReport) {
  return (
    report.installed.length +
      report.updated.length +
      report.conflicts.length +
      report.errors.length >
    0
  );
}

function hasInstallWrites(report: InstallReport) {
  return report.installed.length + report.updated.length > 0;
}

class LineReader {
  private buffer = "";
  private done = false;
  private readonly iterator: AsyncIterator<string | Uint8Array>;

  constructor(input: AsyncIterable<string | Uint8Array>) {
    this.iterator = input[Symbol.asyncIterator]();
  }

  async readLine() {
    while (true) {
      const newline = this.buffer.indexOf("\n");
      if (newline >= 0) {
        const line = this.buffer.slice(0, newline).replace(/\r$/, "");
        this.buffer = this.buffer.slice(newline + 1);
        return line;
      }
      if (this.done) {
        if (!this.buffer) return undefined;
        const line = this.buffer.replace(/\r$/, "");
        this.buffer = "";
        return line;
      }
      const next = await this.iterator.next();
      if (next.done) {
        this.done = true;
      } else {
        this.buffer +=
          typeof next.value === "string"
            ? next.value
            : Buffer.from(next.value).toString("utf8");
      }
    }
  }
}

async function resolveWorkflowScope(
  reader: LineReader,
  output: { write(chunk: string): unknown },
  requested: Scope | undefined,
  scopeSet: boolean,
  promptScope: boolean,
  configuredDefault: Scope | undefined,
  yes: boolean,
  stdinTTY: boolean,
): Promise<{ scope: Scope | ""; selection?: InstallSelection }> {
  const defaultScope = configuredDefault ?? "user";
  const scope = requested ?? defaultScope;
  if (scopeSet || !promptScope) return { scope };
  if (yes) return { scope: defaultScope };
  if (!stdinTTY) {
    return {
      scope: "",
      selection: errorSelection([{ reason: "scope-selection-required" }]),
    };
  }
  return { scope: await promptScopeSelection(reader, output, defaultScope) };
}

async function promptScopeSelection(
  reader: LineReader,
  output: { write(chunk: string): unknown },
  defaultScope: Scope,
) {
  while (true) {
    writeLine(output, installUxText.selectScope);
    writeLine(output, "  1. user");
    writeLine(output, "  2. project");
    output.write(`${installUxText.scopePrompt} [${defaultScope}]: `);
    const selected = parseScopeSelection(
      (await reader.readLine()) ?? "",
      defaultScope,
    );
    if (selected) return selected;
    writeLine(output, installUxText.invalidScopeSelection);
  }
}

function parseScopeSelection(
  line: string,
  defaultScope: Scope,
): Scope | undefined {
  switch (line.trim().toLowerCase()) {
    case "":
      return defaultScope;
    case "1":
    case "u":
    case "user":
      return "user";
    case "2":
    case "p":
    case "project":
      return "project";
    default:
      return undefined;
  }
}

async function promptAgentSelection(
  reader: LineReader,
  output: { write(chunk: string): unknown },
  selection: InstallSelection,
  hosts: Host[],
) {
  const candidates = selection.candidateHostIds
    .map((id) => hosts.find((host) => host.id === id))
    .filter((host): host is Host => Boolean(host));
  while (true) {
    writeLine(output, installUxText.selectAgents);
    candidates.forEach((host, index) => {
      writeLine(output, `  ${index + 1}. ${host.displayName} (${host.id})`);
    });
    const current = selection.selectedHostIds.join(",");
    const suffix = current ? ` [${current}]` : "";
    output.write(`${installUxText.agentsPrompt}${suffix}: `);
    const line = await reader.readLine();
    const selected = parseAgentSelection(line ?? "", selection, candidates);
    if (selected) return selected;
    writeLine(output, installUxText.invalidAgentSelection);
  }
}

function parseAgentSelection(
  line: string,
  selection: InstallSelection,
  candidates: Host[],
) {
  const trimmed = line.trim();
  if (!trimmed) return selection.selectedHostIds;
  if (trimmed === "*") return candidates.map((host) => host.id);

  const byName = new Map<string, string>();
  candidates.forEach((host, index) => {
    byName.set(String(index + 1), host.id);
    byName.set(host.id, host.id);
    for (const alias of host.aliases ?? []) byName.set(alias, host.id);
  });

  const selected: string[] = [];
  const seen = new Set<string>();
  for (const part of trimmed.split(/[,\s]+/)) {
    const id = byName.get(part);
    if (!id) return undefined;
    if (!seen.has(id)) {
      seen.add(id);
      selected.push(id);
    }
  }
  return selected;
}

async function promptConfirmation(
  reader: LineReader,
  output: { write(chunk: string): unknown },
) {
  output.write(installUxText.proceed);
  const line = (await reader.readLine()) ?? "";
  return (
    line.trim().toLowerCase() === "y" || line.trim().toLowerCase() === "yes"
  );
}

function renderInstallSummary(
  output: { write(chunk: string): unknown },
  report: InstallReport,
) {
  for (const item of [...report.installed, ...report.updated]) {
    for (const host of summaryHosts(item)) {
      writeLine(output, `  - ${item.skillName} -> ${item.targetDir} (${host})`);
    }
  }
}

function summaryHosts(item: TargetResult) {
  return "hostId" in item ? [item.hostId] : item.hostIds;
}

function renderSelectionErrors(
  output: { write(chunk: string): unknown },
  selection: InstallSelection,
) {
  for (const error of selection.errors) {
    writeLine(output, `${installUxText.errorPrefix} ${error.reason}`);
  }
}

function writeLine(output: { write(chunk: string): unknown }, line: string) {
  output.write(`${line}\n`);
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

async function chooseScopePath(
  host: Host,
  scope: Scope,
  home: string,
  cwd: string,
) {
  const paths = scopePaths(host, scope);
  for (const path of paths) {
    const expanded = expandHostPath(path, home, cwd);
    if (await exists(expanded)) return expanded;
  }
  return paths[0] ? expandHostPath(paths[0], home, cwd) : undefined;
}

function scopePaths(host: Host, scope: Scope) {
  return scope === "user" ? host.userSkillsDirs : host.projectSkillsDirs;
}

function expandHostPath(path: string, home: string, cwd: string) {
  return path.startsWith("~/") ? join(home, path.slice(2)) : join(cwd, path);
}

function resolvePath(path: string, cwd: string) {
  return resolve(cwd, path);
}

function parseFrontmatter(content: string) {
  const values = new Map<string, string>();
  for (const line of content.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (match) values.set(match[1], match[2].trim());
  }
  return values;
}

async function listSkillFiles(dir: string, base = dir): Promise<string[]> {
  const files: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    if (skipName(entry.name)) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...(await listSkillFiles(full, base)));
    if (entry.isFile()) files.push(relative(base, full).split(sep).join("/"));
  }
  return files.sort();
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

function isGenericDetectPath(path: string) {
  return (
    path === "~/.agents" ||
    path === "~/.agents/skills" ||
    path === "~/.config/agents"
  );
}

async function exists(path: string) {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}
