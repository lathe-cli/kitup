import type {
  Scope,
  AgentSelector,
  Host,
  InstallFlagValues,
  InstallFlagError,
  ParsedInstallFlags,
  InstallSelectionOptions,
  InstallSelection,
  InstallWorkflowOptions,
  InstallWorkflowReport,
  InstallWorkflowExit,
  InstallOptions,
  InstallReport,
  TargetResult,
} from "./types.js";
import { loadHostSpec, resolveHosts, detectHosts } from "./hosts.js";
import {
  planBundledSkill,
  installBundledSkill,
  emptyInstallReport,
} from "./installer.js";

export const installUxText = {
  skillUse: "skill",
  skillShort: "Manage bundled Agent Skill",
  installUse: "install",
  installShort: "Install bundled Agent Skill",
  scopeFlag: "Install scope: user or project",
  agentFlag: "Target agent id. Repeat for multiple agents. Use '*' for all.",
  dryRunFlag: "Show install plan without writing",
  yesFlag: "Skip prompts and accept policy-selected targets",
  forceFlag: "Overwrite unsafe target conflicts",
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
    force: Boolean(flags.force),
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
  return values.flatMap((value) => value.split(/[,\s]+/)).filter(Boolean);
}

export function classifyInstallWorkflowExit(
  workflow: InstallWorkflowReport,
): InstallWorkflowExit {
  const failures = [
    [workflow.canceled, "canceled", installUxText.canceled],
    [
      workflow.selection.errors.length,
      "selection-error",
      installUxText.selectionError,
    ],
    [workflow.report.conflicts.length, "conflict", installUxText.conflict],
    [workflow.report.errors.length, "error", installUxText.failed],
  ] as const;
  const failure = failures.find(([present]) => present);
  return failure
    ? { ok: false, code: failure[1], message: failure[2] }
    : { ok: true, code: "ok", message: "" };
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

export async function resolveInstallSelection(
  options: InstallSelectionOptions,
): Promise<InstallSelection> {
  const hosts = (await loadHostSpec(options.hostsFile)).hosts;
  const stdinTTY = options.stdinTTY ?? Boolean(process.stdin.isTTY);
  const explicitAgents =
    options.agents !== undefined && options.agents !== "auto";

  const requested = explicitAgents
    ? options.agents!
    : options.currentAgent
      ? [options.currentAgent]
      : undefined;
  if (requested) {
    const resolved = await resolveHosts({ agents: requested, hosts });
    if (explicitAgents && resolved.errors.length)
      return errorSelection(resolved.errors);
    const selected = explicitAgents
      ? resolved.hosts
      : addUniversalHost(resolved.hosts, hosts);
    return installSelection(
      selected.map((host) => host.id),
      [],
      !options.yes && stdinTTY,
      resolved.errors,
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

  if (detected.length === 1)
    return installSelection(detectedHostIds, detectedHostIds, true);
  return {
    ...installSelection([], detectedHostIds, true),
    action: "select-agents",
    candidateHostIds: detected.length
      ? detectedHostIds
      : hosts.map((host) => host.id),
  };
}

export async function runBundledSkillInstall(
  options: InstallWorkflowOptions,
): Promise<InstallWorkflowReport> {
  const stdinTTY = options.stdinTTY ?? Boolean(process.stdin.isTTY);
  const output = options.output ?? process.stdout;
  const reader = readLines(options.input ?? process.stdin);
  const { scope, selection: scopeError } = await resolveWorkflowScope(
    reader,
    output,
    options,
    stdinTTY,
  );
  const workflow: InstallWorkflowReport = {
    selection:
      scopeError ??
      (await resolveInstallSelection({
        ...options,
        scope: scope as Scope,
        stdinTTY,
      })),
    scope,
    plan: emptyInstallReport(),
    report: emptyInstallReport(),
    canceled: false,
    dryRun: Boolean(options.dryRun),
  };
  if (workflow.selection.action === "error") {
    renderSelectionErrors(output, workflow.selection);
    return workflow;
  }
  if (workflow.selection.action === "select-agents") {
    const hosts = (await loadHostSpec(options.hostsFile)).hosts;
    const selected = await promptAgentSelection(
      reader,
      output,
      workflow.selection,
      hosts,
    );
    workflow.selection = installSelection(
      selected,
      workflow.selection.detectedHostIds,
      !options.yes && stdinTTY,
    );
    if (selected.length === 0) {
      workflow.canceled = true;
      return workflow;
    }
  }
  const installOptions: InstallOptions = {
    ...options,
    scope: scope as Scope,
    agents: workflow.selection.selectedHostIds,
  };
  const plan = await planBundledSkill(installOptions);
  workflow.plan = workflow.report = plan;
  if (
    plan.installed.length +
      plan.updated.length +
      plan.conflicts.length +
      plan.errors.length ===
    0
  )
    return workflow;
  if (options.dryRun) {
    renderInstallSummary(output, plan);
    return workflow;
  }
  if (plan.conflicts.length + plan.errors.length > 0) {
    workflow.report = { ...plan, installed: [], updated: [] };
    return workflow;
  }
  renderInstallSummary(output, plan);
  if (
    workflow.selection.needsConfirmation &&
    !(await promptConfirmation(reader, output))
  ) {
    workflow.report = emptyInstallReport();
    workflow.canceled = true;
    return workflow;
  }
  workflow.report = await installBundledSkill(installOptions);
  return workflow;
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

function errorSelection(
  errors: InstallSelection["errors"],
  detectedHostIds: string[] = [],
): InstallSelection {
  return {
    ...installSelection([], detectedHostIds, false, errors),
    action: "error",
  };
}

async function* readLines(
  input: AsyncIterable<string | Uint8Array>,
): AsyncGenerator<string, void> {
  let buffer = "";
  for await (const chunk of input) {
    buffer +=
      typeof chunk === "string" ? chunk : Buffer.from(chunk).toString("utf8");
    let newline;
    while ((newline = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, newline).replace(/\r$/, "");
      buffer = buffer.slice(newline + 1);
      yield line;
    }
  }
  if (buffer) yield buffer.replace(/\r$/, "");
}

async function resolveWorkflowScope(
  reader: AsyncGenerator<string, void>,
  output: { write(chunk: string): unknown },
  options: InstallWorkflowOptions,
  stdinTTY: boolean,
): Promise<{ scope: Scope | ""; selection?: InstallSelection }> {
  const defaultScope = options.defaultScope ?? "user";
  const scope = options.scope ?? defaultScope;
  if ((options.scopeSet ?? options.scope !== undefined) || !options.promptScope)
    return { scope };
  if (options.yes) return { scope: defaultScope };
  if (!stdinTTY)
    return {
      scope: "",
      selection: errorSelection([{ reason: "scope-selection-required" }]),
    };
  return { scope: await promptScopeSelection(reader, output, defaultScope) };
}

async function promptScopeSelection(
  reader: AsyncGenerator<string, void>,
  output: { write(chunk: string): unknown },
  defaultScope: Scope,
) {
  while (true) {
    writeLine(output, installUxText.selectScope);
    writeLine(output, "  1. user");
    writeLine(output, "  2. project");
    output.write(`${installUxText.scopePrompt} [${defaultScope}]: `);
    const selected = parseScopeSelection(
      (await reader.next()).value ?? "",
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
  const choices: Record<string, Scope> = {
    "": defaultScope,
    "1": "user",
    u: "user",
    user: "user",
    "2": "project",
    p: "project",
    project: "project",
  };
  return Object.hasOwn(choices, line.trim().toLowerCase())
    ? choices[line.trim().toLowerCase()]
    : undefined;
}

async function promptAgentSelection(
  reader: AsyncGenerator<string, void>,
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
    const line = (await reader.next()).value;
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

  const selected = new Set<string>();
  for (const part of trimmed.split(/[,\s]+/)) {
    const id = byName.get(part);
    if (!id) return undefined;
    selected.add(id);
  }
  return [...selected];
}

async function promptConfirmation(
  reader: AsyncGenerator<string, void>,
  output: { write(chunk: string): unknown },
) {
  output.write(installUxText.proceed);
  const line = (await reader.next()).value ?? "";
  return ["y", "yes"].includes(line.trim().toLowerCase());
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
