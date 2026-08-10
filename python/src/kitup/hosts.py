import json
from pathlib import Path

from ._hosts_generated import DEFAULT_HOSTS_SPEC_JSON
from .types import BaseOptions, Host, HostSpec, KitupError, Scope

_GENERIC_DETECT_PATHS = {"~/.agents", "~/.agents/skills", "~/.config/agents"}


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
        for path in host.project_skills_dirs:
            if not _is_project_host_path(path):
                raise KitupError(f"invalid project path {path!r} for host {host.id}")
        for path in host.user_skills_dirs:
            if not _is_home_host_path(path):
                raise KitupError(f"invalid user path {path!r} for host {host.id}")
        for path in host.detect:
            if not _is_home_host_path(path) and not _is_project_host_path(path):
                raise KitupError(f"invalid detect path {path!r} for host {host.id}")


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
        segment in ("..", "") for segment in path.split("/")
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
        if _primary_detect_path_exists(host, home=home, cwd=cwd):
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


def _primary_detect_path_exists(host: Host, *, home: Path, cwd: Path) -> bool:
    if not host.detect:
        return False
    path = host.detect[0]
    if path in _GENERIC_DETECT_PATHS:
        return False
    return _expand_host_path(path, home=home, cwd=cwd).exists()


def _expand_host_path(path: str, *, home: Path, cwd: Path) -> Path:
    if path.startswith("~/"):
        return home / path[2:]
    return cwd / path
