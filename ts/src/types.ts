export type Scope = "user" | "project";

export type AgentSelector = "*" | "auto" | string[];

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
  | { kind: "directory"; path: string }
  | { kind: "files"; files: SkillFile[] }
  | { kind: "github"; options: GitHubBundleOptions }
  | {
      kind: "metadata";
      bundle: SkillBundle;
      metadata: BundledSkillMetadata;
    };

export interface BundledSkillMetadata {
  sourceId?: string;
  cliVersion?: string;
  cliRevision?: string;
  provenance?: Record<string, string>;
}

export interface GitHubBundleOptions {
  owner: string;
  repo: string;
  path: string;
  ref: string;
}

export interface InstallOptions extends BaseOptions {
  appId: string;
  skillBundle: SkillBundle;
  scope: Scope;
  agents?: AgentSelector;
  force?: boolean;
}

export interface UninstallOptions extends BaseOptions {
  appId: string;
  skillName: string;
  scope: Scope;
  agents?: AgentSelector;
}

export interface StatusOptions extends UninstallOptions {}

export interface InstallSelectionOptions extends BaseOptions {
  scope: Scope;
  agents?: AgentSelector;
  yes?: boolean;
  stdinTTY?: boolean;
  currentAgent?: string;
}

export interface InstallWorkflowOptions
  extends InstallOptions, InstallSelectionOptions {
  dryRun?: boolean;
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
  force?: boolean;
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
  force: boolean;
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

export type BundleError = {
  reason: "bundle-resolve-failed";
};

export type InvalidSkillNameError = {
  skillName: string;
  reason: "invalid-skill-name";
};

export type InvalidAppIdError = { reason: "invalid-app-id" };

export type TargetError =
  | UnknownHostError
  | UnsupportedScopeError
  | SkillError
  | BundleError
  | InvalidSkillNameError
  | InvalidAppIdError;

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

export interface InstalledMetadata extends BundledSkillMetadata {
  schemaVersion: 1;
  appId: string;
  skillName: string;
  source: "bundled" | "github";
  hash: string;
  version?: string;
}

export type InstalledTarget = TargetResult & {
  metadata: InstalledMetadata;
};

export interface StatusReport {
  installed: InstalledTarget[];
  missing: TargetResult[];
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
