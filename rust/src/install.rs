use crate::bundle::{
    content_hash, is_github_bundle, mode_bits, resolve_skill_bundle, set_mode,
    validate_normalized_skill, NormalizedSkillBundle,
};
use crate::hosts::{resolve_install_targets, resolve_install_targets_for_lifecycle};
use crate::metadata::{installed_metadata, read_metadata, write_metadata, MetadataState};
use crate::types::*;
use serde_json::{json, Value};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};

pub fn install_bundled_skill(options: &InstallOptions) -> io::Result<InstallReport> {
    install_or_plan(options, true)
}

pub fn plan_bundled_skill(options: &InstallOptions) -> io::Result<InstallReport> {
    install_or_plan(options, false)
}

pub fn update_bundled_skill(options: &InstallOptions) -> io::Result<InstallReport> {
    install_bundled_skill(options)
}

pub fn uninstall_bundled_skill(options: &UninstallOptions) -> io::Result<UninstallReport> {
    if options.app_id.is_empty() {
        return Ok(uninstall_report(vec![json!({
            "reason": "invalid-app-id"
        })]));
    }
    let (targets, errors, _) = resolve_install_targets_for_lifecycle(
        &options.base,
        &options.agents,
        options.scope,
        &options.skill_name,
        Some(&options.app_id),
    )?;
    let mut report = uninstall_report(errors);
    for target in targets {
        let result = target_result(&target);
        match read_metadata(&target.target_dir).for_owner(&options.app_id, &options.skill_name) {
            MetadataState::Missing => report.skipped.push(with_reason(result, "missing")),
            MetadataState::Conflict(reason) => report.conflicts.push(with_reason(result, reason)),
            MetadataState::Managed(_) => {
                if let Some(reason) =
                    remove_managed_skill(&target.target_dir, &options.app_id, &options.skill_name)?
                {
                    report.conflicts.push(with_reason(result, &reason));
                    continue;
                }
                report.removed.push(result);
            }
        }
    }
    Ok(report)
}

pub fn status_bundled_skill(options: &StatusOptions) -> io::Result<StatusReport> {
    if options.app_id.is_empty() {
        return Ok(status_report(vec![json!({ "reason": "invalid-app-id" })]));
    }
    let (targets, errors, _) = resolve_install_targets_for_lifecycle(
        &options.base,
        &options.agents,
        options.scope,
        &options.skill_name,
        Some(&options.app_id),
    )?;
    let mut report = status_report(errors);
    for target in targets {
        let result = target_result(&target);
        match read_metadata(&target.target_dir).for_owner(&options.app_id, &options.skill_name) {
            MetadataState::Missing => report.missing.push(result),
            MetadataState::Conflict(reason) => report.conflicts.push(with_reason(result, reason)),
            MetadataState::Managed(metadata) => report.installed.push(InstalledTarget {
                target: result,
                metadata: *metadata,
            }),
        }
    }
    Ok(report)
}

fn install_or_plan(options: &InstallOptions, write: bool) -> io::Result<InstallReport> {
    if options.app_id.is_empty() {
        return Ok(install_report(vec![json!({ "reason": "invalid-app-id" })]));
    }
    let (bundle, bundle_metadata) = match resolve_skill_bundle(&options.skill_bundle) {
        Ok(value) => value,
        Err(_) => {
            let reason = if is_github_bundle(&options.skill_bundle) {
                "bundle-resolve-failed"
            } else {
                "invalid-skill-bundle"
            };
            return Ok(install_report(vec![json!({ "reason": reason })]));
        }
    };
    let skill = validate_normalized_skill(&bundle);
    if !skill.valid {
        return Ok(install_report(vec![json!({ "reason": skill.error_code })]));
    }
    let skill_name = skill.skill_name.unwrap();
    let metadata = installed_metadata(
        &options.app_id,
        &skill_name,
        &content_hash(&bundle),
        &bundle_metadata,
    );
    let (targets, errors, _) =
        resolve_install_targets(&options.base, &options.agents, options.scope, &skill_name)?;
    let mut report = install_report(errors);
    for target in targets {
        let result = target_result(&target);
        let state = read_metadata(&target.target_dir).for_owner(&options.app_id, &skill_name);
        if let MetadataState::Conflict(reason) = &state {
            if !options.force {
                report.conflicts.push(with_reason(result, reason));
                continue;
            }
        }
        match state {
            MetadataState::Managed(previous) if previous.hash == metadata.hash => {
                let repaired = repair_skill_bundle_modes(&bundle, &target.target_dir, write)?;
                if repaired || (bundle_metadata.explicit && *previous != metadata) {
                    if write {
                        write_metadata(&target.target_dir, &metadata)?;
                    }
                    report.updated.push(result);
                } else {
                    report.skipped.push(with_reason(result, "unchanged"));
                }
            }
            state => {
                let replace = !matches!(state, MetadataState::Missing);
                if write {
                    write_managed_skill(&bundle, &target.target_dir, &metadata, replace)?;
                }
                if replace {
                    report.updated.push(result);
                } else {
                    report.installed.push(result);
                }
            }
        }
    }
    Ok(report)
}

fn write_managed_skill(
    bundle: &NormalizedSkillBundle,
    target_dir: &Path,
    metadata: &InstalledMetadata,
    replace: bool,
) -> io::Result<()> {
    let tmp = make_staging_dir(target_dir)?;
    let backup = PathBuf::from(format!("{}-backup", tmp.display()));
    let result = (|| {
        copy_skill_bundle(bundle, &tmp)?;
        write_metadata(&tmp, metadata)?;
        if replace {
            fs::rename(target_dir, &backup)?;
        }
        if let Err(error) = fs::rename(&tmp, target_dir) {
            if replace && !target_dir.exists() && backup.exists() {
                let _ = fs::rename(&backup, target_dir);
            }
            return Err(error);
        }
        if replace {
            fs::remove_dir_all(&backup)?;
        }
        Ok(())
    })();
    if result.is_err() {
        let _ = fs::remove_dir_all(&tmp);
    }
    result
}

static STAGING_COUNTER: AtomicU64 = AtomicU64::new(0);

fn make_staging_dir(target_dir: &Path) -> io::Result<PathBuf> {
    let parent = target_dir.parent().ok_or_else(|| {
        io::Error::new(io::ErrorKind::InvalidInput, "install target has no parent")
    })?;
    fs::create_dir_all(parent)?;
    let name = target_dir
        .file_name()
        .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "install target has no name"))?
        .to_string_lossy();
    loop {
        let candidate = parent.join(format!(
            ".{name}.kitup-{}-{}",
            std::process::id(),
            STAGING_COUNTER.fetch_add(1, Ordering::Relaxed)
        ));
        match fs::create_dir(&candidate) {
            Ok(()) => {
                if let Err(error) = set_mode(&candidate, 0o755) {
                    let _ = fs::remove_dir_all(&candidate);
                    return Err(error);
                }
                return Ok(candidate);
            }
            Err(error) if error.kind() == io::ErrorKind::AlreadyExists => continue,
            Err(error) => return Err(error),
        }
    }
}

fn remove_managed_skill(
    target_dir: &Path,
    app_id: &str,
    skill_name: &str,
) -> io::Result<Option<String>> {
    let quarantine = make_staging_dir(target_dir)?;
    fs::remove_dir(&quarantine)?;
    fs::rename(target_dir, &quarantine)?;
    let reason = match read_metadata(&quarantine).for_owner(app_id, skill_name) {
        MetadataState::Managed(_) => {
            fs::remove_dir_all(quarantine)?;
            return Ok(None);
        }
        MetadataState::Conflict(reason) => reason,
        MetadataState::Missing => "unmanaged",
    };
    if target_dir.exists() {
        return Err(io::Error::new(
            io::ErrorKind::AlreadyExists,
            format!("cannot restore changed install: {}", target_dir.display()),
        ));
    }
    fs::rename(&quarantine, target_dir)?;
    Ok(Some(reason.into()))
}

fn copy_skill_bundle(bundle: &NormalizedSkillBundle, dest: &Path) -> io::Result<()> {
    fs::create_dir_all(dest)?;
    for (path, file) in bundle {
        let to = dest.join(path.replace('/', std::path::MAIN_SEPARATOR_STR));
        if let Some(parent) = to.parent() {
            fs::create_dir_all(parent)?;
        }
        fs::write(&to, &file.contents)?;
        set_mode(&to, file.mode)?;
    }
    Ok(())
}

fn repair_skill_bundle_modes(
    bundle: &NormalizedSkillBundle,
    dest: &Path,
    write: bool,
) -> io::Result<bool> {
    let mut repaired = false;
    for (path, file) in bundle {
        let to = dest.join(path.replace('/', std::path::MAIN_SEPARATOR_STR));
        let metadata = match fs::symlink_metadata(&to) {
            Ok(metadata) => metadata,
            Err(error) if error.kind() == io::ErrorKind::NotFound => continue,
            Err(error) => return Err(error),
        };
        if metadata.is_file() && mode_bits(&metadata).is_some_and(|mode| mode != file.mode) {
            repaired = true;
            if write {
                set_mode(&to, file.mode)?;
            }
        }
    }
    Ok(repaired)
}

fn target_result(target: &TargetGroup) -> TargetResult {
    let single = target.host_ids.len() == 1;
    TargetResult {
        host_id: single.then(|| target.host_ids[0].clone()),
        host_ids: if single {
            vec![]
        } else {
            target.host_ids.clone()
        },
        skill_name: target.skill_name.clone(),
        target_dir: target.target_dir.clone(),
    }
}

fn with_reason(target: TargetResult, reason: &str) -> TargetStatus {
    TargetStatus {
        target,
        reason: reason.to_string(),
    }
}

fn install_report(errors: Vec<Value>) -> InstallReport {
    InstallReport {
        errors: report_errors(errors),
        ..InstallReport::default()
    }
}

fn uninstall_report(errors: Vec<Value>) -> UninstallReport {
    UninstallReport {
        errors: report_errors(errors),
        ..UninstallReport::default()
    }
}

fn status_report(errors: Vec<Value>) -> StatusReport {
    StatusReport {
        errors: report_errors(errors),
        ..StatusReport::default()
    }
}

fn report_errors(errors: Vec<Value>) -> Vec<ReportError> {
    errors
        .into_iter()
        .map(|error| ReportError {
            agent: string_value(&error, "agent"),
            flag: string_value(&error, "flag"),
            host_id: string_value(&error, "hostId"),
            reason: string_value(&error, "reason").unwrap_or_default(),
            scope: string_value(&error, "scope"),
            skill_name: string_value(&error, "skillName"),
            value: string_value(&error, "value"),
        })
        .collect()
}

fn string_value(value: &Value, key: &str) -> Option<String> {
    value.get(key).and_then(Value::as_str).map(String::from)
}
