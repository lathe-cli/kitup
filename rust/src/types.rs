use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::BTreeMap;
use std::path::PathBuf;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Scope {
    User,
    Project,
}

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
    pub force: bool,
}

#[derive(Clone, Debug)]
pub struct ParsedInstallFlags {
    pub scope: Scope,
    pub scope_set: bool,
    pub agents: AgentSelector,
    pub yes: bool,
    pub dry_run: bool,
    pub force: bool,
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
    pub force: bool,
}

#[derive(Clone, Debug)]
pub struct UninstallOptions {
    pub base: BaseOptions,
    pub app_id: String,
    pub skill_name: String,
    pub scope: Scope,
    pub agents: AgentSelector,
}

#[derive(Clone, Debug)]
pub struct StatusOptions {
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

#[derive(Clone, Debug, Default)]
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
    GitHub(GitHubBundleOptions),
    Metadata(Box<SkillBundle>, BundledSkillMetadata),
}

#[derive(Clone, Debug, Default)]
pub struct BundledSkillMetadata {
    pub source_id: Option<String>,
    pub cli_version: Option<String>,
    pub cli_revision: Option<String>,
    pub provenance: BTreeMap<String, String>,
}

#[derive(Clone, Debug)]
pub struct GitHubBundleOptions {
    pub owner: String,
    pub repo: String,
    pub path: String,
    pub ref_name: String,
}

#[derive(Clone, Debug)]
pub struct TargetGroup {
    pub host_ids: Vec<String>,
    pub skill_name: String,
    pub target_dir: PathBuf,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TargetResult {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub host_id: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub host_ids: Vec<String>,
    pub skill_name: String,
    pub target_dir: PathBuf,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct TargetStatus {
    #[serde(flatten)]
    pub target: TargetResult,
    pub reason: String,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ReportError {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub agent: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub flag: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub host_id: Option<String>,
    pub reason: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scope: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub skill_name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<String>,
}

#[derive(Clone, Debug, Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct InstallReport {
    pub installed: Vec<TargetResult>,
    pub updated: Vec<TargetResult>,
    pub skipped: Vec<TargetStatus>,
    pub conflicts: Vec<TargetStatus>,
    pub errors: Vec<ReportError>,
}

#[derive(Clone, Debug, Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct UninstallReport {
    pub removed: Vec<TargetResult>,
    pub skipped: Vec<TargetStatus>,
    pub conflicts: Vec<TargetStatus>,
    pub errors: Vec<ReportError>,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstalledMetadata {
    pub schema_version: u32,
    pub app_id: String,
    pub skill_name: String,
    pub source: String,
    pub hash: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cli_version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cli_revision: Option<String>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub provenance: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstalledTarget {
    #[serde(flatten)]
    pub target: TargetResult,
    pub metadata: InstalledMetadata,
}

#[derive(Clone, Debug, Serialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct StatusReport {
    pub installed: Vec<InstalledTarget>,
    pub missing: Vec<TargetResult>,
    pub conflicts: Vec<TargetStatus>,
    pub errors: Vec<ReportError>,
}

#[derive(Clone, Debug, Serialize, Default)]
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
    pub plan: InstallReport,
    pub report: InstallReport,
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
