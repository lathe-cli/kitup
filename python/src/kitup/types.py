from dataclasses import dataclass, field
from typing import Literal


class KitupError(Exception):
    pass


Scope = Literal["user", "project"]


@dataclass(frozen=True)
class BaseOptions:
    home: str | None = None
    cwd: str | None = None
    hosts_file: str | None = None


@dataclass(frozen=True)
class Host:
    id: str
    display_name: str
    project_skills_dirs: list[str]
    user_skills_dirs: list[str]
    detect: list[str]
    status: str
    aliases: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)


@dataclass(frozen=True)
class HostSpec:
    hosts: list[Host]
    schema_version: int = 1


@dataclass
class TargetGroup:
    host_ids: list[str] = field(default_factory=list)
    skill_name: str = ""
    target_dir: str = ""


@dataclass(frozen=True)
class SkillInfo:
    valid: bool
    skill_name: str | None = None
    description: str | None = None
    error_code: (
        Literal[
            "missing-skill-md",
            "invalid-frontmatter",
            "invalid-skill-bundle",
        ]
        | None
    ) = None


@dataclass(frozen=True)
class SkillFile:
    path: str
    contents: str | bytes
    mode: int = 0o644


@dataclass(frozen=True)
class GitHubBundleOptions:
    owner: str
    repo: str
    path: str
    ref: str


@dataclass(frozen=True)
class BundleFile:
    path: str
    bytes: bytes
    mode: int = 0o644


@dataclass(frozen=True)
class NormalizedSkillBundle:
    files: list[BundleFile]
    by_path: dict[str, BundleFile]
    label: str | None = None


@dataclass(frozen=True)
class InstallOptions:
    base: BaseOptions
    app_id: str
    skill_bundle: object
    scope: Scope
    agents: str | list[str] = "auto"
    force: bool = False


@dataclass(frozen=True)
class UninstallOptions:
    base: BaseOptions
    app_id: str
    skill_name: str
    scope: Scope
    agents: str | list[str] = "auto"


@dataclass(frozen=True)
class TargetResult:
    skill_name: str
    target_dir: str
    host_id: str | None = None
    host_ids: list[str] | None = None


@dataclass(frozen=True)
class TargetStatus(TargetResult):
    reason: str = ""


@dataclass(frozen=True)
class TargetError:
    reason: str
    agent: str | None = None
    host_id: str | None = None
    skill_name: str | None = None
    scope: Scope | None = None


@dataclass(frozen=True)
class InstallReport:
    installed: list[TargetResult] = field(default_factory=list)
    updated: list[TargetResult] = field(default_factory=list)
    skipped: list[TargetStatus] = field(default_factory=list)
    conflicts: list[TargetStatus] = field(default_factory=list)
    errors: list[TargetError] = field(default_factory=list)


@dataclass(frozen=True)
class UninstallReport:
    removed: list[TargetResult] = field(default_factory=list)
    skipped: list[TargetStatus] = field(default_factory=list)
    conflicts: list[TargetStatus] = field(default_factory=list)
    errors: list[TargetError] = field(default_factory=list)


@dataclass
class InstallSelection:
    action: Literal["install", "select-agents", "error"]
    selected_host_ids: list[str]
    candidate_host_ids: list[str]
    detected_host_ids: list[str]
    needs_confirmation: bool
    errors: list[dict[str, str]]


@dataclass(frozen=True)
class InstallSelectionOptions:
    base: BaseOptions
    scope: Scope
    agents: str | list[str] = "auto"
    yes: bool = False
    stdin_tty: bool = False
    current_agent: str | None = None


@dataclass(frozen=True)
class InstallWorkflowOptions:
    install: InstallOptions
    yes: bool = False
    dry_run: bool = False
    stdin_tty: bool | None = None
    current_agent: str | None = None
    default_scope: Scope = "user"
    scope_set: bool = False
    prompt_scope: bool = False
    input: object | None = None
    output: object | None = None


@dataclass(frozen=True)
class InstallWorkflowExit:
    ok: bool
    code: str
    message: str


@dataclass
class InstallWorkflowReport:
    selection: InstallSelection
    scope: Scope | Literal[""]
    plan: InstallReport
    report: InstallReport
    canceled: bool
    dry_run: bool


@dataclass(frozen=True)
class ParsedInstallFlags:
    scope: Scope
    scope_set: bool
    agents: str | list[str]
    yes: bool
    dry_run: bool
    force: bool
    errors: list[dict[str, str]]


INSTALL_UX = {
    "skill_use": "skill",
    "install_use": "install",
    "select_scope": "Select install scope:",
    "scope_prompt": "Scope (user/project)",
    "invalid_scope_selection": "Invalid scope selection.",
    "select_agents": "Select agents:",
    "agents_prompt": "Agents (numbers, ids, comma-separated, empty cancels)",
    "invalid_agent_selection": "Invalid agent selection.",
    "proceed": "Proceed? [y/N] ",
    "error_prefix": "kitup:",
    "canceled": "Installation canceled.",
    "selection_error": "Agent selection failed.",
    "conflict": "Installation has conflicts.",
    "failed": "Installation failed.",
    "invalid_flags": "Invalid install flags.",
}
