mod bundle;
mod github;
mod hosts;
mod hosts_generated;
mod install;
mod metadata;
mod types;
mod workflow;

pub use bundle::{
    compute_bundle_content_hash, directory_bundle, files_bundle, github_bundle,
    validate_skill_bundle, with_bundle_metadata,
};
pub use hosts::{
    detect_hosts, load_host_spec, resolve_hosts, resolve_install_selection, resolve_install_targets,
};
pub use install::{
    install_bundled_skill, plan_bundled_skill, status_bundled_skill, uninstall_bundled_skill,
    update_bundled_skill,
};
pub use metadata::read_installed_metadata;
pub use types::{
    AgentSelector, BaseOptions, BundledSkillMetadata, GitHubBundleOptions, Host, InstallFlagValues,
    InstallOptions, InstallReport, InstallSelection, InstallSelectionOptions, InstallWorkflowExit,
    InstallWorkflowOptions, InstallWorkflowReport, InstalledMetadata, InstalledTarget,
    ParsedInstallFlags, ReportError, Scope, SkillBundle, SkillFile, SkillInfo, StatusOptions,
    StatusReport, TargetGroup, TargetResult, TargetStatus, UninstallOptions, UninstallReport,
};
pub use workflow::{
    agent_selector_from_flags, classify_install_workflow_exit, install_flag_error,
    install_workflow_error, parse_install_flags, parse_scope_flag, run_bundled_skill_install,
    run_bundled_skill_install_with_io, InstallUxText, INSTALL_UX,
};

#[cfg(feature = "include-dir")]
pub use bundle::include_dir_bundle;
