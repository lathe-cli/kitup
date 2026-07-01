from dataclasses import dataclass, field
from typing import Literal


class KitupError(Exception):
    pass


Scope = Literal["user", "project"]


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


@dataclass(frozen=True)
class SkillInfo:
    valid: bool
    skill_name: str | None = None
    description: str | None = None
    error_code: Literal[
        "missing-skill-md",
        "invalid-frontmatter",
        "invalid-skill-bundle",
    ] | None = None


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


INSTALL_UX = {
    "skill_use": "skill",
    "install_use": "install",
    "scope_prompt": "Scope (user/project)",
    "agents_prompt": "Agents (numbers, ids, comma-separated, empty cancels)",
}
