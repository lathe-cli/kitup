import json
from pathlib import Path

from ._hosts_generated import DEFAULT_HOSTS_SPEC_JSON
from ._metadata import is_valid_skill_name, read_install_metadata
from .types import (
    BaseOptions,
    Host,
    HostSpec,
    KitupError,
    Scope,
    TargetError,
    TargetGroup,
)

_GENERIC_DETECT_PATHS = {
    "~/.agents",
    "~/.agents/skills",
    "~/.config/agents",
    ".agents",
    ".agents/skills",
    "package.json",
}


def load_host_spec(hosts_file: str | None = None) -> HostSpec:
    raw = json.loads(
        Path(hosts_file).read_text() if hosts_file else DEFAULT_HOSTS_SPEC_JSON
    )
    hosts = [
        Host(
            id=item["id"],
            display_name=item["displayName"],
            aliases=item.get("aliases", []),
            project_skills_dirs=item["projectSkillsDirs"],
            user_skills_dirs=item["userSkillsDirs"],
            detect=item["detect"],
            status=item["status"],
            notes=item.get("notes", []),
        )
        for item in raw["hosts"]
    ]
    _validate_host_spec(hosts)
    return HostSpec(
        schema_version=raw["schemaVersion"],
        hosts=hosts,
    )


def _validate_host_spec(hosts: list[Host]) -> None:
    for host in hosts:
        install_dirs = set(host.project_skills_dirs + host.user_skills_dirs)
        for path in host.project_skills_dirs:
            if not _is_project_host_path(path):
                raise KitupError(f"invalid project path {path!r} for host {host.id}")
        for path in host.user_skills_dirs:
            if not _is_home_host_path(path):
                raise KitupError(f"invalid user path {path!r} for host {host.id}")
        for path in host.detect:
            if not _is_home_host_path(path) and not _is_project_host_path(path):
                raise KitupError(f"invalid detect path {path!r} for host {host.id}")
            if path not in _GENERIC_DETECT_PATHS and path in install_dirs:
                raise KitupError(
                    f"detect path is an install target for host {host.id}: {path!r}"
                )


def _is_project_host_path(path: str) -> bool:
    return (
        bool(path)
        and not path.startswith("/")
        and not path.startswith("~")
        and _is_safe_host_path(path)
    )


def _is_home_host_path(path: str) -> bool:
    return path.startswith("~/") and _is_safe_host_path(path[2:])


def _is_safe_host_path(path: str) -> bool:
    return not any(character in path for character in "\0\\:") and not any(
        segment in (".", "..", "") for segment in path.split("/")
    )


def resolve_hosts(
    agents: str | list[str] | None, hosts: list[Host]
) -> tuple[list[Host], list[dict[str, str]]]:
    if agents == "*":
        return list(hosts), []
    if agents in (None, "auto"):
        return [], []

    ids = [agents] if isinstance(agents, str) else list(agents)
    by_name: dict[str, Host] = {}
    for host in hosts:
        by_name[host.id] = host
        for alias in host.aliases:
            by_name[alias] = host

    seen: set[str] = set()
    resolved: list[Host] = []
    errors: list[dict[str, str]] = []
    for host_id in ids:
        host = by_name.get(host_id)
        if host is None:
            errors.append({"agent": host_id, "reason": "unknown-host"})
            continue
        if host.id in seen:
            continue
        seen.add(host.id)
        resolved.append(host)
    return resolved, errors


def detect_hosts(options: BaseOptions, scope: Scope | None = None) -> list[Host]:
    spec = load_host_spec(options.hosts_file)
    home = Path(options.home).expanduser() if options.home else Path.home()
    cwd = Path(options.cwd) if options.cwd else Path.cwd()
    detected: list[Host] = []
    for host in spec.hosts:
        if _detect_path_exists(host, home=home, cwd=cwd):
            detected.append(host)

    if scope is not None:
        detected.sort(
            key=lambda host: (
                str(_canonical_scope_path(host, scope=scope, home=home, cwd=cwd) or ""),
                host.id,
            )
        )
    return detected


def _canonical_scope_path(
    host: Host, *, scope: Scope, home: Path, cwd: Path
) -> Path | None:
    paths = host.user_skills_dirs if scope == "user" else host.project_skills_dirs
    if not paths:
        return None
    return _expand_host_path(paths[0], home=home, cwd=cwd)


def _detect_path_exists(host: Host, *, home: Path, cwd: Path) -> bool:
    return any(
        _expand_host_path(path, home=home, cwd=cwd).exists()
        for path in host.detect
        if path not in _GENERIC_DETECT_PATHS
    )


def _expand_host_path(path: str, *, home: Path, cwd: Path) -> Path:
    if path.startswith("~/"):
        return home / path[2:]
    return cwd / path


def choose_scope_path(
    host: Host,
    *,
    scope: Scope,
    home: Path,
    cwd: Path,
    skill_name: str,
) -> Path | None:
    paths = host.user_skills_dirs if scope == "user" else host.project_skills_dirs
    existing = [
        expanded
        for path in paths
        if (expanded := _expand_host_path(path, home=home, cwd=cwd)).is_dir()
    ]
    for root in existing:
        metadata = read_install_metadata(root / skill_name)
        if metadata and metadata.get("skillName") == skill_name:
            return root
    if existing:
        return existing[0]
    if not paths:
        return None
    return _expand_host_path(paths[0], home=home, cwd=cwd)


def _uninstall_scope_paths(
    host: Host,
    *,
    scope: Scope,
    home: Path,
    cwd: Path,
    skill_name: str,
    app_id: str,
) -> list[Path]:
    paths = host.user_skills_dirs if scope == "user" else host.project_skills_dirs
    owned = []
    for path in paths:
        root = _expand_host_path(path, home=home, cwd=cwd)
        metadata = read_install_metadata(root / skill_name)
        if (
            metadata
            and metadata.get("skillName") == skill_name
            and metadata.get("appId") == app_id
        ):
            owned.append(root)
    if owned:
        return owned
    fallback = choose_scope_path(
        host,
        scope=scope,
        home=home,
        cwd=cwd,
        skill_name=skill_name,
    )
    return [fallback] if fallback is not None else []


def resolve_install_targets(
    options: BaseOptions,
    agents: str | list[str] | None,
    scope: Scope,
    skill_name: str,
) -> list[TargetGroup]:
    targets, _ = _resolve_install_targets_with_errors(
        options, agents, scope, skill_name
    )
    return targets


def _resolve_install_targets_with_errors(
    options: BaseOptions,
    agents: str | list[str] | None,
    scope: Scope,
    skill_name: str,
    *,
    uninstall_app_id: str | None = None,
) -> tuple[list[TargetGroup], list[TargetError]]:
    if not is_valid_skill_name(skill_name):
        return [], [TargetError(reason="invalid-skill-name", skill_name=skill_name)]

    spec = load_host_spec(options.hosts_file)
    home = Path(options.home).expanduser() if options.home else Path.home()
    cwd = Path(options.cwd) if options.cwd else Path.cwd()
    if agents in (None, "auto"):
        selected = detect_hosts(options, scope)
        errors: list[TargetError] = []
    else:
        selected, resolution_errors = resolve_hosts(agents, spec.hosts)
        errors = [
            TargetError(reason=error["reason"], agent=error["agent"])
            for error in resolution_errors
        ]

    by_target: dict[str, TargetGroup] = {}
    for host in selected:
        if uninstall_app_id is not None:
            roots = _uninstall_scope_paths(
                host,
                scope=scope,
                home=home,
                cwd=cwd,
                skill_name=skill_name,
                app_id=uninstall_app_id,
            )
        else:
            root = choose_scope_path(
                host,
                scope=scope,
                home=home,
                cwd=cwd,
                skill_name=skill_name,
            )
            roots = [root] if root is not None else []
        if not roots:
            errors.append(
                TargetError(
                    reason="unsupported-scope",
                    host_id=host.id,
                    skill_name=skill_name,
                    scope=scope,
                )
            )
            continue
        for root in roots:
            target_dir = str(root / skill_name)
            group = by_target.get(target_dir)
            if group is None:
                group = TargetGroup(skill_name=skill_name, target_dir=target_dir)
                by_target[target_dir] = group
            if host.id not in group.host_ids:
                group.host_ids.append(host.id)

    return [by_target[path] for path in sorted(by_target)], errors
