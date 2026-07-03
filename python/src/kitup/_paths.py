from __future__ import annotations

from pathlib import Path

from .types import KitupError


def skip_name(name: str) -> bool:
    return (
        name == ".git"
        or name == ".kitup.json"
        or name == ".DS_Store"
        or name.endswith(".swp")
        or name.endswith("~")
    )


def normalize_bundle_path(value: str) -> str | None:
    if not value or "\\" in value or value.startswith("/") or value[1:2] == ":":
        raise KitupError(f"invalid skill file path: {value}")

    parts = value.split("/")
    for part in parts:
        if not part or part in {".", ".."}:
            raise KitupError(f"invalid skill file path: {value}")
        if skip_name(part):
            return None

    return "/".join(parts)


def resolve_path(path: str, *, cwd: str | None = None) -> Path:
    resolved = Path(path)
    if not resolved.is_absolute() and cwd is not None:
        resolved = Path(cwd) / resolved
    return resolved


def trim_github_path(path: str) -> str:
    return path.strip("/")
