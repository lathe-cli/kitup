mod hosts_generated;

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, HashMap, HashSet};
use std::fs;
use std::io::{self, BufRead, Write};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Scope {
    User,
    Project,
}

pub struct InstallUxText {
    pub skill_use: &'static str,
    pub skill_short: &'static str,
    pub install_use: &'static str,
    pub install_short: &'static str,
    pub scope_flag: &'static str,
    pub agent_flag: &'static str,
    pub dry_run_flag: &'static str,
    pub yes_flag: &'static str,
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

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AgentSelector {
    Auto,
    All,
    Explicit(Vec<String>),
}

#[derive(Clone, Debug, Default)]
pub struct InstallFlagValues {
    pub scope: Option<String>,
    pub scope_set: bool,
    pub agents: Vec<String>,
    pub yes: bool,
    pub dry_run: bool,
}

#[derive(Clone, Debug)]
pub struct ParsedInstallFlags {
    pub scope: Scope,
    pub scope_set: bool,
    pub agents: AgentSelector,
    pub yes: bool,
    pub dry_run: bool,
    pub errors: Vec<Value>,
}

#[derive(Clone, Debug, Default)]
pub struct BaseOptions {
    pub home: Option<PathBuf>,
    pub cwd: Option<PathBuf>,
    pub hosts_file: Option<PathBuf>,
}

#[derive(Clone, Debug)]
pub struct InstallOptions {
    pub base: BaseOptions,
    pub app_id: String,
    pub skill_bundle: SkillBundle,
    pub scope: Scope,
    pub agents: AgentSelector,
}

#[derive(Clone, Debug)]
pub struct UninstallOptions {
    pub base: BaseOptions,
    pub app_id: String,
    pub skill_name: String,
    pub scope: Scope,
    pub agents: AgentSelector,
}

#[derive(Clone, Debug, Default)]
pub struct InstallSelectionOptions {
    pub base: BaseOptions,
    pub scope: Option<Scope>,
    pub agents: Option<AgentSelector>,
    pub yes: bool,
    pub stdin_tty: bool,
    pub current_agent: Option<String>,
}

#[derive(Clone, Debug)]
pub struct InstallWorkflowOptions {
    pub install: InstallOptions,
    pub yes: bool,
    pub dry_run: bool,
    pub stdin_tty: bool,
    pub current_agent: Option<String>,
    pub default_scope: Option<Scope>,
    pub scope_set: bool,
    pub prompt_scope: bool,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Host {
    pub id: String,
    pub display_name: String,
    #[serde(default)]
    pub aliases: Vec<String>,
    pub project_skills_dirs: Vec<String>,
    pub user_skills_dirs: Vec<String>,
    pub detect: Vec<String>,
    pub status: String,
    #[serde(default)]
    pub notes: Vec<String>,
}

#[derive(Deserialize)]
struct HostSpec {
    hosts: Vec<Host>,
}

#[derive(Clone, Debug)]
pub struct SkillInfo {
    pub valid: bool,
    pub skill_name: Option<String>,
    pub description: Option<String>,
    pub error_code: Option<String>,
}

#[derive(Clone, Debug)]
pub struct SkillFile {
    pub path: String,
    pub contents: Vec<u8>,
    pub mode: Option<u32>,
}

#[derive(Clone, Debug)]
pub enum SkillBundle {
    Directory(PathBuf),
    Files(Vec<SkillFile>),
}

pub fn directory_bundle(path: impl Into<PathBuf>) -> SkillBundle {
    SkillBundle::Directory(path.into())
}

pub fn files_bundle(files: Vec<SkillFile>) -> SkillBundle {
    SkillBundle::Files(files)
}

#[derive(Clone, Debug)]
pub struct TargetGroup {
    pub host_ids: Vec<String>,
    pub skill_name: String,
    pub target_dir: PathBuf,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstallSelection {
    pub action: String,
    pub selected_host_ids: Vec<String>,
    pub candidate_host_ids: Vec<String>,
    pub detected_host_ids: Vec<String>,
    pub needs_confirmation: bool,
    pub errors: Vec<Value>,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstallWorkflowReport {
    pub selection: InstallSelection,
    pub scope: String,
    pub plan: Value,
    pub report: Value,
    pub canceled: bool,
    pub dry_run: bool,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstallWorkflowExit {
    pub ok: bool,
    pub code: String,
    pub message: String,
}

#[derive(Deserialize)]
struct Metadata {
    #[serde(rename = "appId")]
    app_id: String,
    hash: String,
}

#[derive(Clone, Debug)]
struct BundleFile {
    path: String,
    contents: Vec<u8>,
    mode: u32,
}

#[derive(Clone, Debug)]
struct NormalizedSkillBundle {
    files: Vec<BundleFile>,
    by_path: BTreeMap<String, BundleFile>,
}

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
    if report.canceled {
        return install_workflow_exit(false, "canceled", INSTALL_UX.canceled);
    }
    if !report.selection.errors.is_empty() {
        return install_workflow_exit(false, "selection-error", INSTALL_UX.selection_error);
    }
    if report_count(&report.report, "conflicts") > 0 {
        return install_workflow_exit(false, "conflict", INSTALL_UX.conflict);
    }
    if report_count(&report.report, "errors") > 0 {
        return install_workflow_exit(false, "error", INSTALL_UX.failed);
    }
    install_workflow_exit(true, "ok", "")
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

fn install_workflow_exit(ok: bool, code: &str, message: &str) -> InstallWorkflowExit {
    InstallWorkflowExit {
        ok,
        code: code.to_string(),
        message: message.to_string(),
    }
}

pub fn load_host_spec(hosts_file: Option<&Path>) -> io::Result<Vec<Host>> {
    let spec: HostSpec = match hosts_file {
        Some(path) => serde_json::from_slice(&fs::read(path)?)?,
        None => serde_json::from_str(hosts_generated::DEFAULT_HOSTS_SPEC_JSON)?,
    };
    Ok(spec.hosts)
}

pub fn resolve_hosts(agents: &AgentSelector, hosts: &[Host]) -> (Vec<Host>, Vec<Value>) {
    match agents {
        AgentSelector::All => (hosts.to_vec(), vec![]),
        AgentSelector::Auto => (vec![], vec![]),
        AgentSelector::Explicit(ids) => {
            let mut by_name = HashMap::new();
            for host in hosts {
                by_name.insert(host.id.as_str(), host);
                for alias in &host.aliases {
                    by_name.insert(alias.as_str(), host);
                }
            }
            let mut seen = HashSet::new();
            let mut resolved = Vec::new();
            let mut errors = Vec::new();
            for id in ids {
                if let Some(host) = by_name.get(id.as_str()) {
                    if seen.insert(host.id.clone()) {
                        resolved.push((*host).clone());
                    }
                } else {
                    errors.push(json!({ "agent": id, "reason": "unknown-host" }));
                }
            }
            (resolved, errors)
        }
    }
}

pub fn detect_hosts(options: &BaseOptions, scope: Option<Scope>) -> io::Result<Vec<Host>> {
    let hosts = load_host_spec(options.hosts_file.as_deref())?;
    let (home, cwd) = defaults(options)?;
    let mut detected = Vec::new();
    for host in hosts {
        if let Some(path) = host.detect.first() {
            if !is_generic_detect_path(path) && expand_host_path(path, &home, &cwd).exists() {
                detected.push(host);
            }
        }
    }
    if let Some(scope) = scope {
        detected.sort_by(|a, b| {
            let left = canonical_scope_path(a, scope, &home, &cwd).unwrap_or_default();
            let right = canonical_scope_path(b, scope, &home, &cwd).unwrap_or_default();
            left.cmp(&right).then_with(|| a.id.cmp(&b.id))
        });
    }
    Ok(detected)
}

pub fn resolve_install_selection(
    options: &InstallSelectionOptions,
) -> io::Result<InstallSelection> {
    let hosts = load_host_spec(options.base.hosts_file.as_deref())?;
    let explicit_agents = matches!(
        options.agents,
        Some(AgentSelector::All) | Some(AgentSelector::Explicit(_))
    );
    if let Some(current_agent) = &options.current_agent {
        if !explicit_agents {
            let (selected, errors) = resolve_hosts(
                &AgentSelector::Explicit(vec![current_agent.clone()]),
                &hosts,
            );
            let selected = add_universal_host(selected, &hosts);
            return Ok(install_selection(
                host_ids(&selected),
                vec![],
                !options.yes && options.stdin_tty,
                errors,
            ));
        }
    }
    if explicit_agents {
        match options.agents.as_ref().unwrap() {
            AgentSelector::All => {
                return Ok(install_selection(
                    host_ids(&hosts),
                    vec![],
                    !options.yes && options.stdin_tty,
                    vec![],
                ))
            }
            AgentSelector::Explicit(_) => {
                let (selected, errors) = resolve_hosts(options.agents.as_ref().unwrap(), &hosts);
                if !errors.is_empty() {
                    return Ok(error_selection(errors, vec![]));
                }
                return Ok(install_selection(
                    host_ids(&selected),
                    vec![],
                    !options.yes && options.stdin_tty,
                    vec![],
                ));
            }
            AgentSelector::Auto => {}
        }
    }
    let scope = options.scope.unwrap_or(Scope::User);
    let detected = detect_hosts(&options.base, Some(scope))?;
    let detected_host_ids = host_ids(&detected);
    if !options.stdin_tty && !options.yes {
        return Ok(error_selection(
            vec![json!({ "reason": "agent-selection-required" })],
            detected_host_ids,
        ));
    }
    if options.yes {
        if detected.is_empty() {
            return Ok(error_selection(
                vec![json!({ "reason": "no-detected-hosts" })],
                detected_host_ids,
            ));
        }
        return Ok(install_selection(
            detected_host_ids.clone(),
            detected_host_ids,
            false,
            vec![],
        ));
    }
    if detected.is_empty() {
        return Ok(select_agents_selection(host_ids(&hosts), vec![], vec![]));
    }
    if detected.len() == 1 {
        return Ok(install_selection(
            detected_host_ids.clone(),
            detected_host_ids,
            true,
            vec![],
        ));
    }
    Ok(select_agents_selection(
        detected_host_ids.clone(),
        detected_host_ids,
        vec![],
    ))
}

pub fn resolve_install_targets(
    options: &BaseOptions,
    agents: &AgentSelector,
    scope: Scope,
    skill_name: &str,
) -> io::Result<(Vec<TargetGroup>, Vec<Value>, Vec<String>)> {
    let hosts = load_host_spec(options.hosts_file.as_deref())?;
    let (home, cwd) = defaults(options)?;
    let (selected, mut errors) = match agents {
        AgentSelector::Auto => (detect_hosts(options, Some(scope))?, vec![]),
        _ => resolve_hosts(agents, &hosts),
    };
    let mut by_target: BTreeMap<PathBuf, TargetGroup> = BTreeMap::new();
    for host in selected {
        if let Some(root) = choose_scope_path(&host, scope, &home, &cwd) {
            let target_dir = root.join(skill_name);
            by_target
                .entry(target_dir.clone())
                .or_insert_with(|| TargetGroup {
                    host_ids: Vec::new(),
                    skill_name: skill_name.to_string(),
                    target_dir,
                })
                .host_ids
                .push(host.id);
        } else {
            errors.push(json!({
                "hostId": host.id,
                "skillName": skill_name,
                "scope": scope_text(scope),
                "reason": "unsupported-scope"
            }));
        }
    }
    let targets: Vec<_> = by_target.into_values().collect();
    let detected_host_ids = targets
        .iter()
        .flat_map(|target| target.host_ids.clone())
        .collect();
    Ok((targets, errors, detected_host_ids))
}

fn add_universal_host(mut selected: Vec<Host>, hosts: &[Host]) -> Vec<Host> {
    if selected.iter().any(|host| host.id == "universal") {
        return selected;
    }
    if let Some(host) = hosts.iter().find(|host| host.id == "universal") {
        selected.push(host.clone());
    }
    selected
}

fn host_ids(hosts: &[Host]) -> Vec<String> {
    hosts.iter().map(|host| host.id.clone()).collect()
}

fn install_selection(
    selected_host_ids: Vec<String>,
    detected_host_ids: Vec<String>,
    mut needs_confirmation: bool,
    errors: Vec<Value>,
) -> InstallSelection {
    let action = if errors.is_empty() {
        "install"
    } else {
        needs_confirmation = false;
        "error"
    };
    InstallSelection {
        action: action.to_string(),
        selected_host_ids,
        candidate_host_ids: vec![],
        detected_host_ids,
        needs_confirmation,
        errors,
    }
}

fn select_agents_selection(
    candidate_host_ids: Vec<String>,
    detected_host_ids: Vec<String>,
    selected_host_ids: Vec<String>,
) -> InstallSelection {
    InstallSelection {
        action: "select-agents".to_string(),
        selected_host_ids,
        candidate_host_ids,
        detected_host_ids,
        needs_confirmation: true,
        errors: vec![],
    }
}

fn error_selection(errors: Vec<Value>, detected_host_ids: Vec<String>) -> InstallSelection {
    InstallSelection {
        action: "error".to_string(),
        selected_host_ids: vec![],
        candidate_host_ids: vec![],
        detected_host_ids,
        needs_confirmation: false,
        errors,
    }
}

pub fn validate_skill_bundle(bundle: &SkillBundle) -> SkillInfo {
    let bundle = match read_skill_bundle(bundle) {
        Ok(bundle) => bundle,
        Err(_) => {
            return SkillInfo {
                valid: false,
                skill_name: None,
                description: None,
                error_code: Some("invalid-skill-bundle".to_string()),
            }
        }
    };
    validate_normalized_skill(&bundle)
}

fn validate_normalized_skill(bundle: &NormalizedSkillBundle) -> SkillInfo {
    let Some(file) = bundle.by_path.get("SKILL.md") else {
        return SkillInfo {
            valid: false,
            skill_name: None,
            description: None,
            error_code: Some("missing-skill-md".to_string()),
        };
    };
    let content = String::from_utf8_lossy(&file.contents);
    let Some(rest) = content.strip_prefix("---\n") else {
        return invalid_frontmatter();
    };
    let Some(end) = rest.find("\n---\n") else {
        return invalid_frontmatter();
    };
    let fields = parse_frontmatter(&rest[..end]);
    let name = fields.get("name").cloned().unwrap_or_default();
    let description = fields.get("description").cloned().unwrap_or_default();
    if !valid_skill_name(&name) || description.is_empty() || description.len() > 1024 {
        return invalid_frontmatter();
    }
    SkillInfo {
        valid: true,
        skill_name: Some(name),
        description: Some(description),
        error_code: None,
    }
}

pub fn compute_bundle_content_hash(bundle: &SkillBundle) -> io::Result<String> {
    Ok(content_hash(&read_skill_bundle(bundle)?))
}

fn content_hash(bundle: &NormalizedSkillBundle) -> String {
    let mut hash = Sha256::new();
    for file in &bundle.files {
        hash.update(file.path.as_bytes());
        hash.update([0]);
        hash.update(&file.contents);
        hash.update([0]);
    }
    format!("sha256:{:x}", hash.finalize())
}

pub fn install_bundled_skill(options: &InstallOptions) -> io::Result<Value> {
    install_or_plan(options, true)
}

pub fn plan_bundled_skill(options: &InstallOptions) -> io::Result<Value> {
    install_or_plan(options, false)
}

pub fn run_bundled_skill_install(
    options: &InstallWorkflowOptions,
) -> io::Result<InstallWorkflowReport> {
    let stdin = io::stdin();
    let mut input = stdin.lock();
    let stdout = io::stdout();
    let mut output = stdout.lock();
    run_bundled_skill_install_with_io(options, &mut input, &mut output)
}

pub fn run_bundled_skill_install_with_io<R: BufRead, W: Write>(
    options: &InstallWorkflowOptions,
    input: &mut R,
    output: &mut W,
) -> io::Result<InstallWorkflowReport> {
    let (scope, scope_error) = resolve_workflow_scope(input, output, options)?;
    if let Some(selection) = scope_error {
        render_selection_errors(output, &selection)?;
        let empty = install_report(vec![]);
        return Ok(InstallWorkflowReport {
            selection,
            scope: String::new(),
            plan: empty.clone(),
            report: empty,
            canceled: false,
            dry_run: options.dry_run,
        });
    }
    let scope = scope.unwrap();
    let workflow_scope = scope_text(scope).to_string();
    let mut selection = resolve_install_selection(&InstallSelectionOptions {
        base: options.install.base.clone(),
        scope: Some(scope),
        agents: Some(options.install.agents.clone()),
        yes: options.yes,
        stdin_tty: options.stdin_tty,
        current_agent: options.current_agent.clone(),
    })?;
    if selection.action == "error" {
        render_selection_errors(output, &selection)?;
        let empty = install_report(vec![]);
        return Ok(InstallWorkflowReport {
            selection,
            scope: workflow_scope,
            plan: empty.clone(),
            report: empty,
            canceled: false,
            dry_run: options.dry_run,
        });
    }
    if selection.action == "select-agents" {
        let hosts = load_host_spec(options.install.base.hosts_file.as_deref())?;
        let selected = prompt_agent_selection(input, output, &selection, &hosts)?;
        selection = install_selection(
            selected,
            selection.detected_host_ids.clone(),
            !options.yes && options.stdin_tty,
            vec![],
        );
        if selection.selected_host_ids.is_empty() {
            let empty = install_report(vec![]);
            return Ok(InstallWorkflowReport {
                selection,
                scope: workflow_scope,
                plan: empty.clone(),
                report: empty,
                canceled: true,
                dry_run: options.dry_run,
            });
        }
    }
    let mut install = options.install.clone();
    install.agents = AgentSelector::Explicit(selection.selected_host_ids.clone());
    install.scope = scope;
    let plan = plan_bundled_skill(&install)?;
    if !has_visible_install_plan(&plan) {
        return Ok(InstallWorkflowReport {
            selection,
            scope: workflow_scope,
            plan: plan.clone(),
            report: plan,
            canceled: false,
            dry_run: options.dry_run,
        });
    }
    render_install_summary(output, &plan)?;
    if options.dry_run {
        return Ok(InstallWorkflowReport {
            selection,
            scope: workflow_scope,
            plan: plan.clone(),
            report: plan,
            canceled: false,
            dry_run: true,
        });
    }
    if !has_install_writes(&plan) {
        return Ok(InstallWorkflowReport {
            selection,
            scope: workflow_scope,
            plan: plan.clone(),
            report: plan,
            canceled: false,
            dry_run: false,
        });
    }
    if selection.needs_confirmation && !prompt_confirmation(input, output)? {
        return Ok(InstallWorkflowReport {
            selection,
            scope: workflow_scope,
            plan,
            report: install_report(vec![]),
            canceled: true,
            dry_run: false,
        });
    }
    let report = install_bundled_skill(&install)?;
    Ok(InstallWorkflowReport {
        selection,
        scope: workflow_scope,
        plan,
        report,
        canceled: false,
        dry_run: false,
    })
}

pub fn update_bundled_skill(options: &InstallOptions) -> io::Result<Value> {
    install_bundled_skill(options)
}

pub fn uninstall_bundled_skill(options: &UninstallOptions) -> io::Result<Value> {
    let (targets, errors, _) = resolve_install_targets(
        &options.base,
        &options.agents,
        options.scope,
        &options.skill_name,
    )?;
    let mut report = uninstall_report(errors);
    for target in targets {
        let result = target_result(&target);
        match read_metadata(&target.target_dir) {
            MetadataState::Missing => {
                push_report(&mut report, "skipped", with_reason(result, "missing"))
            }
            MetadataState::Unmanaged => {
                push_report(&mut report, "conflicts", with_reason(result, "unmanaged"))
            }
            MetadataState::Managed(meta) if meta.app_id != options.app_id => push_report(
                &mut report,
                "conflicts",
                with_reason(result, "owner-mismatch"),
            ),
            MetadataState::Managed(_) => {
                fs::remove_dir_all(&target.target_dir)?;
                push_report(&mut report, "removed", result);
            }
        }
    }
    Ok(report)
}

fn install_or_plan(options: &InstallOptions, write: bool) -> io::Result<Value> {
    let bundle = match read_skill_bundle(&options.skill_bundle) {
        Ok(bundle) => bundle,
        Err(_) => {
            return Ok(install_report(vec![json!({
                "reason": "invalid-skill-bundle"
            })]));
        }
    };
    let skill = validate_normalized_skill(&bundle);
    if !skill.valid {
        return Ok(install_report(vec![json!({
            "reason": skill.error_code
        })]));
    }
    let skill_name = skill.skill_name.unwrap();
    let hash = content_hash(&bundle);
    let (targets, errors, _) =
        resolve_install_targets(&options.base, &options.agents, options.scope, &skill_name)?;
    let mut report = install_report(errors);
    for target in targets {
        let result = target_result(&target);
        match read_metadata(&target.target_dir) {
            MetadataState::Missing => {
                if write {
                    copy_managed_skill(
                        &bundle,
                        &target.target_dir,
                        &options.app_id,
                        &skill_name,
                        &hash,
                    )?;
                }
                push_report(&mut report, "installed", result);
            }
            MetadataState::Unmanaged => {
                push_report(&mut report, "conflicts", with_reason(result, "unmanaged"))
            }
            MetadataState::Managed(meta) if meta.app_id != options.app_id => push_report(
                &mut report,
                "conflicts",
                with_reason(result, "owner-mismatch"),
            ),
            MetadataState::Managed(meta) if meta.hash == hash => {
                push_report(&mut report, "skipped", with_reason(result, "unchanged"))
            }
            MetadataState::Managed(_) => {
                if write {
                    replace_managed_skill(
                        &bundle,
                        &target.target_dir,
                        &options.app_id,
                        &skill_name,
                        &hash,
                    )?;
                }
                push_report(&mut report, "updated", result);
            }
        }
    }
    Ok(report)
}

enum MetadataState {
    Missing,
    Unmanaged,
    Managed(Metadata),
}

fn copy_managed_skill(
    bundle: &NormalizedSkillBundle,
    target_dir: &Path,
    app_id: &str,
    skill_name: &str,
    hash: &str,
) -> io::Result<()> {
    let _ = fs::remove_dir_all(target_dir);
    copy_skill_bundle(bundle, target_dir)?;
    write_metadata(target_dir, app_id, skill_name, hash)
}

fn replace_managed_skill(
    bundle: &NormalizedSkillBundle,
    target_dir: &Path,
    app_id: &str,
    skill_name: &str,
    hash: &str,
) -> io::Result<()> {
    let suffix = format!(
        ".kitup-{}",
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    );
    let tmp = PathBuf::from(format!("{}{}", target_dir.display(), suffix));
    let backup = PathBuf::from(format!("{}{}-backup", target_dir.display(), suffix));
    let _ = fs::remove_dir_all(&tmp);
    copy_skill_bundle(bundle, &tmp)?;
    write_metadata(&tmp, app_id, skill_name, hash)?;
    fs::rename(target_dir, &backup)?;
    if let Err(error) = fs::rename(&tmp, target_dir) {
        let _ = fs::remove_dir_all(&tmp);
        if !target_dir.exists() && backup.exists() {
            let _ = fs::rename(&backup, target_dir);
        }
        return Err(error);
    }
    fs::remove_dir_all(backup)
}

fn copy_skill_bundle(bundle: &NormalizedSkillBundle, dest: &Path) -> io::Result<()> {
    fs::create_dir_all(dest)?;
    for file in &bundle.files {
        let to = dest.join(PathBuf::from(
            file.path.replace('/', std::path::MAIN_SEPARATOR_STR),
        ));
        if let Some(parent) = to.parent() {
            fs::create_dir_all(parent)?;
        }
        fs::write(&to, &file.contents)?;
        set_mode(&to, file.mode)?;
    }
    Ok(())
}

fn write_metadata(target_dir: &Path, app_id: &str, skill_name: &str, hash: &str) -> io::Result<()> {
    let data = serde_json::to_vec_pretty(&json!({
        "schemaVersion": 1,
        "appId": app_id,
        "skillName": skill_name,
        "source": "bundled",
        "hash": hash
    }))?;
    let mut data = data;
    data.push(b'\n');
    fs::write(target_dir.join(".kitup.json"), data)
}

fn read_metadata(target_dir: &Path) -> MetadataState {
    if !target_dir.exists() {
        return MetadataState::Missing;
    }
    let Ok(data) = fs::read(target_dir.join(".kitup.json")) else {
        return MetadataState::Unmanaged;
    };
    match serde_json::from_slice(&data) {
        Ok(meta) => MetadataState::Managed(meta),
        Err(_) => MetadataState::Unmanaged,
    }
}

fn target_result(target: &TargetGroup) -> Value {
    if target.host_ids.len() == 1 {
        json!({
            "hostId": target.host_ids[0],
            "skillName": target.skill_name,
            "targetDir": target.target_dir
        })
    } else {
        json!({
            "hostIds": target.host_ids,
            "skillName": target.skill_name,
            "targetDir": target.target_dir
        })
    }
}

fn with_reason(mut value: Value, reason: &str) -> Value {
    value["reason"] = json!(reason);
    value
}

fn install_report(errors: Vec<Value>) -> Value {
    json!({
        "installed": [],
        "updated": [],
        "skipped": [],
        "conflicts": [],
        "errors": errors
    })
}

fn has_visible_install_plan(report: &Value) -> bool {
    report_count(report, "installed")
        + report_count(report, "updated")
        + report_count(report, "conflicts")
        + report_count(report, "errors")
        > 0
}

fn has_install_writes(report: &Value) -> bool {
    report_count(report, "installed") + report_count(report, "updated") > 0
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

fn render_install_summary<W: Write>(output: &mut W, report: &Value) -> io::Result<()> {
    for item in report["installed"]
        .as_array()
        .into_iter()
        .flatten()
        .chain(report["updated"].as_array().into_iter().flatten())
    {
        for host in summary_hosts(item) {
            writeln!(
                output,
                "  - {} -> {} ({})",
                item["skillName"].as_str().unwrap_or(""),
                item["targetDir"].as_str().unwrap_or(""),
                host
            )?;
        }
    }
    Ok(())
}

fn summary_hosts(item: &Value) -> Vec<String> {
    if let Some(host) = item["hostId"].as_str() {
        return vec![host.to_string()];
    }
    item["hostIds"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .map(String::from)
        .collect()
}

fn report_count(report: &Value, key: &str) -> usize {
    report[key].as_array().map(Vec::len).unwrap_or(0)
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

fn hosts_by_id(hosts: &[Host], ids: &[String]) -> Vec<Host> {
    let by_id: HashMap<_, _> = hosts.iter().map(|host| (host.id.as_str(), host)).collect();
    ids.iter()
        .filter_map(|id| by_id.get(id.as_str()).map(|host| (*host).clone()))
        .collect()
}

fn uninstall_report(errors: Vec<Value>) -> Value {
    json!({
        "removed": [],
        "skipped": [],
        "conflicts": [],
        "errors": errors
    })
}

fn push_report(report: &mut Value, key: &str, value: Value) {
    report[key].as_array_mut().unwrap().push(value);
}

fn canonical_scope_path(host: &Host, scope: Scope, home: &Path, cwd: &Path) -> Option<PathBuf> {
    let paths = scope_paths(host, scope);
    paths.first().map(|path| expand_host_path(path, home, cwd))
}

fn choose_scope_path(host: &Host, scope: Scope, home: &Path, cwd: &Path) -> Option<PathBuf> {
    let paths = scope_paths(host, scope);
    for path in paths {
        let expanded = expand_host_path(path, home, cwd);
        if expanded.exists() {
            return Some(expanded);
        }
    }
    paths.first().map(|path| expand_host_path(path, home, cwd))
}

fn scope_paths(host: &Host, scope: Scope) -> &[String] {
    match scope {
        Scope::User => &host.user_skills_dirs,
        Scope::Project => &host.project_skills_dirs,
    }
}

fn expand_host_path(path: &str, home: &Path, cwd: &Path) -> PathBuf {
    if let Some(rest) = path.strip_prefix("~/") {
        home.join(rest)
    } else {
        cwd.join(path)
    }
}

fn defaults(options: &BaseOptions) -> io::Result<(PathBuf, PathBuf)> {
    let home = match &options.home {
        Some(home) => home.clone(),
        None => PathBuf::from(std::env::var("HOME").unwrap_or_default()),
    };
    let cwd = match &options.cwd {
        Some(cwd) => cwd.clone(),
        None => std::env::current_dir()?,
    };
    Ok((home, cwd))
}

fn parse_frontmatter(content: &str) -> HashMap<String, String> {
    let mut fields = HashMap::new();
    for line in content.lines() {
        if let Some((key, value)) = line.split_once(':') {
            fields.insert(key.to_string(), value.trim().to_string());
        }
    }
    fields
}

fn invalid_frontmatter() -> SkillInfo {
    SkillInfo {
        valid: false,
        skill_name: None,
        description: None,
        error_code: Some("invalid-frontmatter".to_string()),
    }
}

fn valid_skill_name(name: &str) -> bool {
    let mut last_dash = false;
    if name.is_empty() || name.starts_with('-') || name.ends_with('-') {
        return false;
    }
    for byte in name.bytes() {
        let ok = byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-';
        if !ok || (byte == b'-' && last_dash) {
            return false;
        }
        last_dash = byte == b'-';
    }
    true
}

fn read_skill_bundle(bundle: &SkillBundle) -> io::Result<NormalizedSkillBundle> {
    match bundle {
        SkillBundle::Directory(root) => normalize_skill_files(read_directory_bundle_files(root)?),
        SkillBundle::Files(files) => normalize_skill_files(files.clone()),
    }
}

fn read_directory_bundle_files(root: &Path) -> io::Result<Vec<SkillFile>> {
    let mut files = Vec::new();
    collect_directory_bundle_files(root, root, &mut files)?;
    Ok(files)
}

fn collect_directory_bundle_files(
    root: &Path,
    dir: &Path,
    files: &mut Vec<SkillFile>,
) -> io::Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if skip_name(&name) {
            continue;
        }
        let path = entry.path();
        let metadata = entry.metadata()?;
        if metadata.is_dir() {
            collect_directory_bundle_files(root, &path, files)?;
        } else if metadata.is_file() {
            let rel = path.strip_prefix(root).unwrap();
            files.push(SkillFile {
                path: rel.to_string_lossy().replace('\\', "/"),
                contents: fs::read(&path)?,
                mode: mode_bits(&metadata),
            });
        }
    }
    Ok(())
}

fn normalize_skill_files(files: Vec<SkillFile>) -> io::Result<NormalizedSkillBundle> {
    let mut by_path = BTreeMap::new();
    for file in files {
        let Some(path) = normalize_bundle_path(&file.path)? else {
            continue;
        };
        if by_path.contains_key(&path) {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("duplicate skill file: {path}"),
            ));
        }
        let mode = file.mode.unwrap_or(0o644);
        by_path.insert(
            path.clone(),
            BundleFile {
                path,
                contents: file.contents,
                mode,
            },
        );
    }
    let files = by_path.values().cloned().collect();
    Ok(NormalizedSkillBundle { files, by_path })
}

fn normalize_bundle_path(value: &str) -> io::Result<Option<String>> {
    if value.is_empty()
        || value.contains('\\')
        || value.starts_with('/')
        || value.as_bytes().get(1) == Some(&b':')
    {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            format!("invalid skill file path: {value}"),
        ));
    }
    let parts: Vec<_> = value.split('/').collect();
    for part in &parts {
        if part.is_empty() || *part == "." || *part == ".." {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("invalid skill file path: {value}"),
            ));
        }
        if skip_name(part) {
            return Ok(None);
        }
    }
    Ok(Some(parts.join("/")))
}

#[cfg(unix)]
fn mode_bits(metadata: &fs::Metadata) -> Option<u32> {
    use std::os::unix::fs::PermissionsExt;
    Some(metadata.permissions().mode() & 0o777)
}

#[cfg(not(unix))]
fn mode_bits(_metadata: &fs::Metadata) -> Option<u32> {
    None
}

#[cfg(unix)]
fn set_mode(path: &Path, mode: u32) -> io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    fs::set_permissions(path, fs::Permissions::from_mode(mode))
}

#[cfg(not(unix))]
fn set_mode(_path: &Path, _mode: u32) -> io::Result<()> {
    Ok(())
}

fn skip_name(name: &str) -> bool {
    name == ".git"
        || name == ".kitup.json"
        || name == ".DS_Store"
        || name.ends_with(".swp")
        || name.ends_with('~')
}

fn is_generic_detect_path(path: &str) -> bool {
    path == "~/.agents" || path == "~/.agents/skills" || path == "~/.config/agents"
}

fn scope_text(scope: Scope) -> &'static str {
    match scope {
        Scope::User => "user",
        Scope::Project => "project",
    }
}
