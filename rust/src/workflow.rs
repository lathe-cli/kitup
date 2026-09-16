use crate::hosts::{
    error_selection, host_ids, hosts_by_id, install_selection, load_host_spec,
    resolve_install_selection, scope_text,
};
use crate::install::{install_bundled_skill, plan_bundled_skill};
use crate::types::*;
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::io::{self, BufRead, Write};

pub struct InstallUxText {
    pub skill_use: &'static str,
    pub skill_short: &'static str,
    pub install_use: &'static str,
    pub install_short: &'static str,
    pub scope_flag: &'static str,
    pub agent_flag: &'static str,
    pub dry_run_flag: &'static str,
    pub yes_flag: &'static str,
    pub force_flag: &'static str,
    pub select_scope: &'static str,
    pub scope_prompt: &'static str,
    pub invalid_scope_selection: &'static str,
    pub select_agents: &'static str,
    pub agents_prompt: &'static str,
    pub invalid_agent_selection: &'static str,
    pub proceed: &'static str,
    pub install_summary: &'static str,
    pub error_prefix: &'static str,
    pub canceled: &'static str,
    pub selection_error: &'static str,
    pub conflict: &'static str,
    pub failed: &'static str,
    pub invalid_flags: &'static str,
}

pub const INSTALL_UX: InstallUxText = InstallUxText {
    skill_use: "skill",
    skill_short: "Manage bundled Agent Skill",
    install_use: "install",
    install_short: "Install bundled Agent Skill",
    scope_flag: "Install scope: user or project",
    agent_flag: "Target agent id. Repeat for multiple agents. Use '*' for all.",
    dry_run_flag: "Show install plan without writing",
    yes_flag: "Skip prompts and accept policy-selected targets",
    force_flag: "Overwrite unsafe target conflicts",
    select_scope: "Select install scope:",
    scope_prompt: "Scope (user/project)",
    invalid_scope_selection: "Invalid scope selection.",
    select_agents: "Select agents:",
    agents_prompt: "Agents (numbers, ids, comma-separated, empty cancels)",
    invalid_agent_selection: "Invalid agent selection.",
    proceed: "Proceed? [y/N] ",
    install_summary: "Install summary:",
    error_prefix: "kitup:",
    canceled: "Installation canceled.",
    selection_error: "Agent selection failed.",
    conflict: "Installation has conflicts.",
    failed: "Installation failed.",
    invalid_flags: "Invalid install flags.",
};

pub fn parse_install_flags(flags: InstallFlagValues) -> ParsedInstallFlags {
    let mut errors = Vec::new();
    let scope = parse_scope_flag(flags.scope.as_deref(), &mut errors);
    let agents = agent_selector_from_flags(&flags.agents, &mut errors);
    ParsedInstallFlags {
        scope,
        scope_set: flags.scope_set || flags.scope.is_some(),
        agents,
        yes: flags.yes,
        dry_run: flags.dry_run,
        force: flags.force,
        errors,
    }
}

pub fn agent_selector_from_flags(values: &[String], errors: &mut Vec<Value>) -> AgentSelector {
    let agents = split_flag_values(values);
    if agents.is_empty() {
        return AgentSelector::Auto;
    }
    if agents.iter().any(|agent| agent == "*") {
        if agents.len() > 1 {
            errors.push(json!({
                "flag": "agent",
                "reason": "agent-star-must-be-alone",
                "value": agents.join(",")
            }));
        }
        return AgentSelector::All;
    }
    let mut seen = HashSet::new();
    AgentSelector::Explicit(
        agents
            .into_iter()
            .filter(|agent| seen.insert(agent.clone()))
            .collect(),
    )
}

pub fn parse_scope_flag(value: Option<&str>, errors: &mut Vec<Value>) -> Scope {
    match value.unwrap_or("user") {
        "" | "user" => Scope::User,
        "project" => Scope::Project,
        value => {
            errors.push(json!({ "flag": "scope", "reason": "invalid-scope", "value": value }));
            Scope::User
        }
    }
}

fn split_flag_values(values: &[String]) -> Vec<String> {
    values
        .iter()
        .flat_map(|value| value.split(|ch: char| ch == ',' || ch.is_whitespace()))
        .filter(|value| !value.is_empty())
        .map(String::from)
        .collect()
}

pub fn classify_install_workflow_exit(report: &InstallWorkflowReport) -> InstallWorkflowExit {
    let (code, message) = [
        (report.canceled, "canceled", INSTALL_UX.canceled),
        (
            !report.selection.errors.is_empty(),
            "selection-error",
            INSTALL_UX.selection_error,
        ),
        (
            !report.report.conflicts.is_empty(),
            "conflict",
            INSTALL_UX.conflict,
        ),
        (!report.report.errors.is_empty(), "error", INSTALL_UX.failed),
    ]
    .into_iter()
    .find_map(|(failed, code, message)| failed.then_some((code, message)))
    .unwrap_or(("ok", ""));
    InstallWorkflowExit {
        ok: code == "ok",
        code: code.into(),
        message: message.into(),
    }
}

pub fn install_workflow_error(report: &InstallWorkflowReport) -> io::Result<()> {
    let exit = classify_install_workflow_exit(report);
    if exit.ok || exit.code == "canceled" {
        Ok(())
    } else {
        Err(io::Error::other(exit.message))
    }
}

pub fn install_flag_error(errors: &[Value]) -> io::Result<()> {
    if errors.is_empty() {
        Ok(())
    } else {
        Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            INSTALL_UX.invalid_flags,
        ))
    }
}

pub fn run_bundled_skill_install(
    options: &InstallWorkflowOptions,
) -> io::Result<InstallWorkflowReport> {
    run_bundled_skill_install_with_io(options, &mut io::stdin().lock(), &mut io::stdout().lock())
}

pub fn run_bundled_skill_install_with_io<R: BufRead, W: Write>(
    options: &InstallWorkflowOptions,
    input: &mut R,
    output: &mut W,
) -> io::Result<InstallWorkflowReport> {
    let (scope, scope_error) = resolve_workflow_scope(input, output, options)?;
    let selection = match scope_error {
        Some(error) => error,
        None => resolve_install_selection(&InstallSelectionOptions {
            base: options.install.base.clone(),
            scope,
            agents: Some(options.install.agents.clone()),
            yes: options.yes,
            stdin_tty: options.stdin_tty,
            current_agent: options.current_agent.clone(),
        })?,
    };
    let mut workflow = InstallWorkflowReport {
        selection,
        scope: scope.map(scope_text).unwrap_or_default().into(),
        plan: InstallReport::default(),
        report: InstallReport::default(),
        canceled: false,
        dry_run: options.dry_run,
    };
    if workflow.selection.action == "error" {
        render_selection_errors(output, &workflow.selection)?;
        return Ok(workflow);
    }
    if workflow.selection.action == "select-agents" {
        let hosts = load_host_spec(options.install.base.hosts_file.as_deref())?;
        let selected = prompt_agent_selection(input, output, &workflow.selection, &hosts)?;
        workflow.selection = install_selection(
            selected,
            workflow.selection.detected_host_ids,
            !options.yes && options.stdin_tty,
            vec![],
        );
        if workflow.selection.selected_host_ids.is_empty() {
            workflow.canceled = true;
            return Ok(workflow);
        }
    }
    let mut install = options.install.clone();
    install.agents = AgentSelector::Explicit(workflow.selection.selected_host_ids.clone());
    install.scope = scope.unwrap();
    workflow.plan = plan_bundled_skill(&install)?;
    workflow.report = workflow.plan.clone();
    let plan = &workflow.plan;
    if plan.installed.is_empty()
        && plan.updated.is_empty()
        && plan.conflicts.is_empty()
        && plan.errors.is_empty()
    {
        return Ok(workflow);
    }
    if options.dry_run {
        render_install_summary(output, plan)?;
    } else if !plan.conflicts.is_empty() || !plan.errors.is_empty() {
        workflow.report.installed.clear();
        workflow.report.updated.clear();
    } else {
        render_install_summary(output, plan)?;
        workflow.canceled =
            workflow.selection.needs_confirmation && !prompt_confirmation(input, output)?;
        workflow.report = if workflow.canceled {
            InstallReport::default()
        } else {
            install_bundled_skill(&install)?
        };
    }
    Ok(workflow)
}

fn resolve_workflow_scope<R: BufRead, W: Write>(
    input: &mut R,
    output: &mut W,
    options: &InstallWorkflowOptions,
) -> io::Result<(Option<Scope>, Option<InstallSelection>)> {
    let default_scope = options.default_scope.unwrap_or(Scope::User);
    if options.scope_set || !options.prompt_scope {
        return Ok((Some(options.install.scope), None));
    }
    if options.yes {
        return Ok((Some(default_scope), None));
    }
    if !options.stdin_tty {
        return Ok((
            None,
            Some(error_selection(
                vec![json!({ "reason": "scope-selection-required" })],
                vec![],
            )),
        ));
    }
    Ok((
        Some(prompt_scope_selection(input, output, default_scope)?),
        None,
    ))
}

fn prompt_scope_selection<R: BufRead, W: Write>(
    input: &mut R,
    output: &mut W,
    default_scope: Scope,
) -> io::Result<Scope> {
    loop {
        writeln!(output, "{}", INSTALL_UX.select_scope)?;
        writeln!(output, "  1. user")?;
        writeln!(output, "  2. project")?;
        write!(
            output,
            "{} [{}]: ",
            INSTALL_UX.scope_prompt,
            scope_text(default_scope)
        )?;
        output.flush()?;
        let line = read_prompt_line(input)?;
        if let Some(scope) = parse_scope_selection(&line, default_scope) {
            return Ok(scope);
        }
        writeln!(output, "{}", INSTALL_UX.invalid_scope_selection)?;
    }
}

fn parse_scope_selection(line: &str, default_scope: Scope) -> Option<Scope> {
    match line.trim().to_ascii_lowercase().as_str() {
        "" => Some(default_scope),
        "1" | "u" | "user" => Some(Scope::User),
        "2" | "p" | "project" => Some(Scope::Project),
        _ => None,
    }
}

fn prompt_agent_selection<R: BufRead, W: Write>(
    input: &mut R,
    output: &mut W,
    selection: &InstallSelection,
    hosts: &[Host],
) -> io::Result<Vec<String>> {
    let candidates = hosts_by_id(hosts, &selection.candidate_host_ids);
    loop {
        writeln!(output, "{}", INSTALL_UX.select_agents)?;
        for (index, host) in candidates.iter().enumerate() {
            writeln!(
                output,
                "  {}. {} ({})",
                index + 1,
                host.display_name,
                host.id
            )?;
        }
        let suffix = if selection.selected_host_ids.is_empty() {
            String::new()
        } else {
            format!(" [{}]", selection.selected_host_ids.join(","))
        };
        write!(output, "{}{}: ", INSTALL_UX.agents_prompt, suffix)?;
        output.flush()?;
        let line = read_prompt_line(input)?;
        if let Some(selected) = parse_agent_selection(&line, selection, &candidates) {
            return Ok(selected);
        }
        writeln!(output, "{}", INSTALL_UX.invalid_agent_selection)?;
    }
}

fn parse_agent_selection(
    line: &str,
    selection: &InstallSelection,
    candidates: &[Host],
) -> Option<Vec<String>> {
    let line = line.trim();
    if line.is_empty() {
        return Some(selection.selected_host_ids.clone());
    }
    if line == "*" {
        return Some(host_ids(candidates));
    }
    let mut by_name = HashMap::new();
    for (index, host) in candidates.iter().enumerate() {
        by_name.insert((index + 1).to_string(), host.id.clone());
        by_name.insert(host.id.clone(), host.id.clone());
        for alias in &host.aliases {
            by_name.insert(alias.clone(), host.id.clone());
        }
    }
    let mut seen = HashSet::new();
    let mut selected = Vec::new();
    for part in line.split(|value: char| value == ',' || value.is_whitespace()) {
        if part.is_empty() {
            continue;
        }
        let id = by_name.get(part)?;
        if seen.insert(id.clone()) {
            selected.push(id.clone());
        }
    }
    Some(selected)
}

fn prompt_confirmation<R: BufRead, W: Write>(input: &mut R, output: &mut W) -> io::Result<bool> {
    write!(output, "{}", INSTALL_UX.proceed)?;
    output.flush()?;
    let line = read_prompt_line(input)?;
    let line = line.trim().to_ascii_lowercase();
    Ok(line == "y" || line == "yes")
}

fn read_prompt_line<R: BufRead>(input: &mut R) -> io::Result<String> {
    let mut line = String::new();
    input.read_line(&mut line)?;
    Ok(line.trim_end_matches(['\r', '\n']).to_string())
}

fn render_install_summary<W: Write>(output: &mut W, report: &InstallReport) -> io::Result<()> {
    for item in report.installed.iter().chain(&report.updated) {
        for host in summary_hosts(item) {
            writeln!(
                output,
                "  - {} -> {} ({})",
                item.skill_name,
                item.target_dir.display(),
                host,
            )?;
        }
    }
    Ok(())
}

fn summary_hosts(item: &TargetResult) -> Vec<String> {
    if let Some(host) = &item.host_id {
        return vec![host.clone()];
    }
    item.host_ids.clone()
}

fn render_selection_errors<W: Write>(
    output: &mut W,
    selection: &InstallSelection,
) -> io::Result<()> {
    for error in &selection.errors {
        writeln!(
            output,
            "{} {}",
            INSTALL_UX.error_prefix,
            error["reason"].as_str().unwrap_or("error")
        )?;
    }
    Ok(())
}
