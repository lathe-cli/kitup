use crate::bundle::valid_skill_name;
use crate::hosts_generated;
use crate::metadata::{read_metadata, MetadataState};
use crate::types::*;
use serde::Deserialize;
use serde_json::{json, Value};
use std::collections::{BTreeMap, HashMap, HashSet};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

#[derive(Deserialize)]
struct HostSpec {
    hosts: Vec<Host>,
}

pub fn load_host_spec(hosts_file: Option<&Path>) -> io::Result<Vec<Host>> {
    let spec: HostSpec = match hosts_file {
        Some(path) => serde_json::from_slice(&fs::read(path)?)?,
        None => serde_json::from_str(hosts_generated::DEFAULT_HOSTS_SPEC_JSON)?,
    };
    validate_host_spec(&spec.hosts)?;
    Ok(spec.hosts)
}

fn validate_host_spec(hosts: &[Host]) -> io::Result<()> {
    for host in hosts {
        let install_dirs: HashSet<_> = host
            .project_skills_dirs
            .iter()
            .chain(&host.user_skills_dirs)
            .collect();
        for (kind, paths) in [
            ("project", &host.project_skills_dirs),
            ("user", &host.user_skills_dirs),
            ("detect", &host.detect),
        ] {
            for path in paths {
                let valid = match kind {
                    "project" => is_project_host_path(path),
                    "user" => is_home_host_path(path),
                    _ => is_home_host_path(path) || is_project_host_path(path),
                };
                if !valid {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        format!("invalid {kind} path {path:?} for host {}", host.id),
                    ));
                }
                if kind == "detect" && !is_generic_detect_path(path) && install_dirs.contains(path)
                {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        format!(
                            "detect path is an install target for host {}: {path:?}",
                            host.id
                        ),
                    ));
                }
            }
        }
    }
    Ok(())
}

fn is_project_host_path(path: &str) -> bool {
    !path.is_empty() && !path.starts_with('/') && !path.starts_with('~') && is_safe_host_path(path)
}

fn is_home_host_path(path: &str) -> bool {
    path.starts_with("~/") && is_safe_host_path(&path[2..])
}

fn is_safe_host_path(path: &str) -> bool {
    !path
        .chars()
        .any(|character| matches!(character, '\0' | '\\' | ':'))
        && !path
            .split('/')
            .any(|segment| segment == "." || segment == ".." || segment.is_empty())
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
        if host.detect.iter().any(|path| {
            !is_generic_detect_path(path) && expand_host_path(path, &home, &cwd).exists()
        }) {
            detected.push(host);
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
    let confirm = !options.yes && options.stdin_tty;
    if let Some(agents @ (AgentSelector::All | AgentSelector::Explicit(_))) = &options.agents {
        let (selected, errors) = resolve_hosts(agents, &hosts);
        return Ok(if errors.is_empty() {
            install_selection(host_ids(&selected), vec![], confirm, errors)
        } else {
            error_selection(errors, vec![])
        });
    }
    if let Some(current_agent) = &options.current_agent {
        let (selected, errors) = resolve_hosts(
            &AgentSelector::Explicit(vec![current_agent.clone()]),
            &hosts,
        );
        return Ok(install_selection(
            host_ids(&add_universal_host(selected, &hosts)),
            vec![],
            confirm,
            errors,
        ));
    }
    let detected = detect_hosts(&options.base, Some(options.scope.unwrap_or(Scope::User)))?;
    let ids = host_ids(&detected);
    match (options.yes, options.stdin_tty, detected.len()) {
        (false, false, _) => Ok(error_selection(
            vec![json!({ "reason": "agent-selection-required" })],
            ids,
        )),
        (true, _, 0) => Ok(error_selection(
            vec![json!({ "reason": "no-detected-hosts" })],
            ids,
        )),
        (true, _, _) | (false, true, 1) => Ok(install_selection(ids.clone(), ids, confirm, vec![])),
        _ => Ok(InstallSelection {
            action: "select-agents".into(),
            candidate_host_ids: if detected.is_empty() {
                host_ids(&hosts)
            } else {
                ids.clone()
            },
            detected_host_ids: ids,
            needs_confirmation: true,
            ..InstallSelection::default()
        }),
    }
}

pub fn resolve_install_targets(
    options: &BaseOptions,
    agents: &AgentSelector,
    scope: Scope,
    skill_name: &str,
) -> io::Result<(Vec<TargetGroup>, Vec<Value>, Vec<String>)> {
    resolve_install_targets_for_lifecycle(options, agents, scope, skill_name, None)
}

pub(crate) fn resolve_install_targets_for_lifecycle(
    options: &BaseOptions,
    agents: &AgentSelector,
    scope: Scope,
    skill_name: &str,
    uninstall_app_id: Option<&str>,
) -> io::Result<(Vec<TargetGroup>, Vec<Value>, Vec<String>)> {
    if !valid_skill_name(skill_name) {
        return Ok((
            vec![],
            vec![json!({
                "skillName": skill_name,
                "reason": "invalid-skill-name"
            })],
            vec![],
        ));
    }
    let hosts = load_host_spec(options.hosts_file.as_deref())?;
    let (home, cwd) = defaults(options)?;
    let (selected, mut errors) = match agents {
        AgentSelector::Auto => (detect_hosts(options, Some(scope))?, vec![]),
        _ => resolve_hosts(agents, &hosts),
    };
    let mut by_target: BTreeMap<PathBuf, TargetGroup> = BTreeMap::new();
    for host in selected {
        let roots = match uninstall_app_id {
            Some(app_id) => uninstall_scope_paths(&host, scope, &home, &cwd, skill_name, app_id),
            None => choose_scope_path(&host, scope, &home, &cwd, skill_name)
                .into_iter()
                .collect(),
        };
        if roots.is_empty() {
            errors.push(json!({
                "hostId": host.id,
                "skillName": skill_name,
                "scope": scope_text(scope),
                "reason": "unsupported-scope"
            }));
        } else {
            for root in roots {
                let target_dir = root.join(skill_name);
                let group = by_target
                    .entry(target_dir.clone())
                    .or_insert_with(|| TargetGroup {
                        host_ids: Vec::new(),
                        skill_name: skill_name.to_string(),
                        target_dir,
                    });
                if !group.host_ids.contains(&host.id) {
                    group.host_ids.push(host.id.clone());
                }
            }
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

pub(crate) fn host_ids(hosts: &[Host]) -> Vec<String> {
    hosts.iter().map(|host| host.id.clone()).collect()
}

pub(crate) fn install_selection(
    selected_host_ids: Vec<String>,
    detected_host_ids: Vec<String>,
    needs_confirmation: bool,
    errors: Vec<Value>,
) -> InstallSelection {
    InstallSelection {
        action: if errors.is_empty() {
            "install"
        } else {
            "error"
        }
        .into(),
        selected_host_ids,
        detected_host_ids,
        needs_confirmation: needs_confirmation && errors.is_empty(),
        errors,
        ..InstallSelection::default()
    }
}

pub(crate) fn error_selection(
    errors: Vec<Value>,
    detected_host_ids: Vec<String>,
) -> InstallSelection {
    install_selection(vec![], detected_host_ids, false, errors)
}

pub(crate) fn hosts_by_id(hosts: &[Host], ids: &[String]) -> Vec<Host> {
    let by_id: HashMap<_, _> = hosts.iter().map(|host| (host.id.as_str(), host)).collect();
    ids.iter()
        .filter_map(|id| by_id.get(id.as_str()).map(|host| (*host).clone()))
        .collect()
}

fn canonical_scope_path(host: &Host, scope: Scope, home: &Path, cwd: &Path) -> Option<PathBuf> {
    let paths = scope_paths(host, scope);
    paths.first().map(|path| expand_host_path(path, home, cwd))
}

fn choose_scope_path(
    host: &Host,
    scope: Scope,
    home: &Path,
    cwd: &Path,
    skill_name: &str,
) -> Option<PathBuf> {
    let paths = scope_paths(host, scope);
    let existing: Vec<_> = paths
        .iter()
        .map(|path| expand_host_path(path, home, cwd))
        .filter(|path| path.is_dir())
        .collect();
    for root in &existing {
        if let MetadataState::Managed(metadata) = read_metadata(&root.join(skill_name)) {
            if metadata.skill_name == skill_name {
                return Some(root.clone());
            }
        }
    }
    if let Some(root) = existing.into_iter().next() {
        return Some(root);
    }
    paths.first().map(|path| expand_host_path(path, home, cwd))
}

fn uninstall_scope_paths(
    host: &Host,
    scope: Scope,
    home: &Path,
    cwd: &Path,
    skill_name: &str,
    app_id: &str,
) -> Vec<PathBuf> {
    let owned: Vec<_> = scope_paths(host, scope)
        .iter()
        .map(|path| expand_host_path(path, home, cwd))
        .filter(|root| {
            matches!(
                read_metadata(&root.join(skill_name)).for_owner(app_id, skill_name),
                MetadataState::Managed(_)
            )
        })
        .collect();
    if !owned.is_empty() {
        return owned;
    }
    choose_scope_path(host, scope, home, cwd, skill_name)
        .into_iter()
        .collect()
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
        None => PathBuf::from(
            std::env::var("HOME")
                .or_else(|_| std::env::var("USERPROFILE"))
                .unwrap_or_default(),
        ),
    };
    let cwd = match &options.cwd {
        Some(cwd) => cwd.clone(),
        None => std::env::current_dir()?,
    };
    Ok((home, cwd))
}

fn is_generic_detect_path(path: &str) -> bool {
    matches!(
        path,
        "~/.agents"
            | "~/.agents/skills"
            | "~/.config/agents"
            | ".agents"
            | ".agents/skills"
            | "package.json"
    )
}

pub(crate) fn scope_text(scope: Scope) -> &'static str {
    match scope {
        Scope::User => "user",
        Scope::Project => "project",
    }
}
