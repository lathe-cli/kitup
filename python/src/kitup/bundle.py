from __future__ import annotations

import hashlib
import os
import re
from dataclasses import dataclass
from pathlib import Path

from ._github import fetch_github_directory
from ._paths import normalize_bundle_path, resolve_path, skip_name
from .types import (
    BundleFile,
    GitHubBundleOptions,
    KitupError,
    NormalizedSkillBundle,
    SkillFile,
    SkillInfo,
)


@dataclass(frozen=True)
class DirectoryBundle:
    path: str


@dataclass(frozen=True)
class FilesBundle:
    files: list[SkillFile]


@dataclass(frozen=True)
class GitHubBundle:
    options: GitHubBundleOptions


SkillBundle = DirectoryBundle | FilesBundle | GitHubBundle


def directory_bundle(path: str) -> DirectoryBundle:
    return DirectoryBundle(path=path)


def files_bundle(files: list[SkillFile]) -> FilesBundle:
    return FilesBundle(files=files)


def github_bundle(options: GitHubBundleOptions) -> GitHubBundle:
    return GitHubBundle(options=options)


def validate_skill_bundle(bundle: SkillBundle, cwd: str | None = None) -> SkillInfo:
    try:
        normalized = normalize_skill_bundle(bundle, cwd=cwd)
    except Exception:
        return SkillInfo(valid=False, error_code="invalid-skill-bundle")

    return validate_normalized_skill_bundle(normalized)


def validate_normalized_skill_bundle(normalized: NormalizedSkillBundle) -> SkillInfo:
    skill_md = normalized.by_path.get("SKILL.md")
    if skill_md is None:
        return SkillInfo(valid=False, error_code="missing-skill-md")

    try:
        content = skill_md.bytes.decode("utf-8")
    except UnicodeDecodeError:
        return SkillInfo(valid=False, error_code="invalid-frontmatter")

    match = re.match(r"^---\r?\n([\s\S]*?)\r?\n---\r?\n", content)
    if match is None:
        return SkillInfo(valid=False, error_code="invalid-frontmatter")

    fields = _parse_frontmatter(match.group(1))
    skill_name = fields.get("name", "")
    description = fields.get("description", "")
    if not _valid_skill_name(skill_name) or not description or len(description) > 1024:
        return SkillInfo(valid=False, error_code="invalid-frontmatter")

    return SkillInfo(valid=True, skill_name=skill_name, description=description)


def compute_bundle_content_hash(bundle: SkillBundle, cwd: str | None = None) -> str:
    normalized = normalize_skill_bundle(bundle, cwd=cwd)
    return compute_normalized_bundle_content_hash(normalized)


def compute_normalized_bundle_content_hash(normalized: NormalizedSkillBundle) -> str:
    digest = hashlib.sha256()
    for file in normalized.files:
        digest.update(file.path.encode("utf-8"))
        digest.update(b"\0")
        digest.update(file.bytes)
        digest.update(b"\0")
    return f"sha256:{digest.hexdigest()}"


def normalize_skill_bundle(
    bundle: SkillBundle, cwd: str | None = None
) -> NormalizedSkillBundle:
    if isinstance(bundle, DirectoryBundle):
        return normalize_directory_bundle(bundle.path, cwd=cwd)
    if isinstance(bundle, FilesBundle):
        return normalize_files_bundle(bundle.files)
    if isinstance(bundle, GitHubBundle):
        return normalize_files_bundle(fetch_github_directory(bundle.options))
    raise KitupError(f"unsupported bundle: {type(bundle)!r}")


def normalize_directory_bundle(
    path: str, cwd: str | None = None
) -> NormalizedSkillBundle:
    root = resolve_path(path, cwd=cwd)
    if not root.is_dir():
        raise KitupError(f"invalid bundle directory: {root}")
    files: list[SkillFile] = []

    for current_root, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(name for name in dirnames if not skip_name(name))
        for filename in sorted(filenames):
            if skip_name(filename):
                continue
            source = Path(current_root) / filename
            relative = source.relative_to(root).as_posix()
            files.append(
                SkillFile(
                    path=relative,
                    contents=source.read_bytes(),
                    mode=source.stat().st_mode & 0o777,
                )
            )

    return normalize_files_bundle(files)


def normalize_files_bundle(files: list[SkillFile]) -> NormalizedSkillBundle:
    by_path: dict[str, BundleFile] = {}
    for file in files:
        normalized_path = normalize_bundle_path(file.path)
        if normalized_path is None:
            continue
        if normalized_path in by_path:
            raise KitupError(f"duplicate skill file: {normalized_path}")
        by_path[normalized_path] = BundleFile(
            path=normalized_path,
            bytes=file.contents.encode("utf-8")
            if isinstance(file.contents, str)
            else file.contents,
            mode=file.mode or 0o644,
        )

    normalized_files = [by_path[path] for path in sorted(by_path)]
    return NormalizedSkillBundle(files=normalized_files, by_path=by_path)


def copy_normalized_bundle(files: list[BundleFile], target_dir: str | Path) -> None:
    destination_root = Path(target_dir)
    destination_root.mkdir(parents=True, exist_ok=True)
    for file in files:
        destination = destination_root / file.path
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(file.bytes)
        destination.chmod(file.mode)


def _parse_frontmatter(content: str) -> dict[str, str]:
    fields: dict[str, str] = {}
    for line in content.splitlines():
        if ":" not in line:
            continue
        key, value = line.split(":", 1)
        fields[key] = value.strip()
    return fields


def _valid_skill_name(name: str) -> bool:
    return re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", name) is not None
