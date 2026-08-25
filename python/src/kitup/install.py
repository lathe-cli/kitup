from __future__ import annotations

import stat
import shutil
import tempfile
from pathlib import Path

from ._github import fetch_github_directory_with_metadata
from ._metadata import (
    is_valid_skill_name,
    read_install_metadata,
    read_installed_metadata,
    write_install_metadata,
)
from .bundle import (
    DirectoryBundle,
    FilesBundle,
    GitHubBundle,
    copy_normalized_bundle,
    compute_normalized_bundle_content_hash,
    normalize_directory_bundle,
    normalize_files_bundle,
    validate_normalized_skill_bundle,
)
from .hosts import detect_hosts, load_host_spec, resolve_hosts
from .types import (
    BaseOptions,
    BundleFile,
    Host,
    InstallOptions,
    InstallReport,
    Scope,
    TargetError,
    TargetGroup,
    TargetResult,
    TargetStatus,
    UninstallOptions,
    UninstallReport,
)


def expand_host_path(path: str, *, home: Path, cwd: Path) -> Path:
    if path.startswith("~/"):
        return home / path[2:]
    return cwd / path


def choose_scope_path(
    host: Host, *, scope: Scope, home: Path, cwd: Path
) -> Path | None:
    paths = host.user_skills_dirs if scope == "user" else host.project_skills_dirs
    for path in paths:
        expanded = expand_host_path(path, home=home, cwd=cwd)
        if expanded.exists():
            return expanded
    if not paths:
        return None
    return expand_host_path(paths[0], home=home, cwd=cwd)


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


def empty_install_report(errors: list[TargetError] | None = None) -> InstallReport:
    return InstallReport(errors=errors or [])


def empty_uninstall_report(errors: list[TargetError] | None = None) -> UninstallReport:
    return UninstallReport(errors=errors or [])


def target_result(target: TargetGroup) -> TargetResult:
    if len(target.host_ids) == 1:
        return TargetResult(
            host_id=target.host_ids[0],
            skill_name=target.skill_name,
            target_dir=target.target_dir,
        )
    return TargetResult(
        host_ids=list(target.host_ids),
        skill_name=target.skill_name,
        target_dir=target.target_dir,
    )


def target_status(target: TargetGroup, reason: str) -> TargetStatus:
    result = target_result(target)
    return TargetStatus(
        host_id=result.host_id,
        host_ids=result.host_ids,
        skill_name=result.skill_name,
        target_dir=result.target_dir,
        reason=reason,
    )


def plan_bundled_skill(options: InstallOptions) -> InstallReport:
    return install_or_plan(options, write=False)


def install_bundled_skill(options: InstallOptions) -> InstallReport:
    return install_or_plan(options, write=True)


def update_bundled_skill(options: InstallOptions) -> InstallReport:
    return install_bundled_skill(options)


def write_managed_bundle(
    target_dir: Path,
    *,
    files: list[BundleFile],
    app_id: str,
    skill_name: str,
    digest: str,
    metadata: dict[str, object],
    replace: bool,
) -> None:
    target_dir.parent.mkdir(parents=True, exist_ok=True)
    staged_dir = Path(
        tempfile.mkdtemp(
            prefix=f".{target_dir.name}.kitup-",
            dir=target_dir.parent,
        )
    )
    backup_dir: Path | None = None
    try:
        staged_dir.chmod(0o755)
        copy_normalized_bundle(files, staged_dir)
        write_bundle_metadata(
            staged_dir,
            app_id=app_id,
            skill_name=skill_name,
            digest=digest,
            metadata=metadata,
        )
        if replace and target_dir.exists():
            backup_dir = Path(
                tempfile.mkdtemp(
                    prefix=f".{target_dir.name}.kitup-old-",
                    dir=target_dir.parent,
                )
            )
            backup_dir.rmdir()
            target_dir.replace(backup_dir)
            staged_dir.replace(target_dir)
            shutil.rmtree(backup_dir)
            return
        staged_dir.replace(target_dir)
    except Exception:
        if backup_dir is not None and backup_dir.exists() and not target_dir.exists():
            backup_dir.replace(target_dir)
        shutil.rmtree(staged_dir, ignore_errors=True)
        raise


def install_or_plan(options: InstallOptions, *, write: bool) -> InstallReport:
    if not options.app_id:
        return empty_install_report([TargetError(reason="invalid-app-id")])

    try:
        normalized, bundle_metadata = _resolve_bundle_and_metadata(
            options.skill_bundle, cwd=options.base.cwd
        )
    except Exception:
        reason = (
            "bundle-resolve-failed"
            if isinstance(options.skill_bundle, GitHubBundle)
            else "invalid-skill-bundle"
        )
        return empty_install_report([TargetError(reason=reason)])

    info = validate_normalized_skill_bundle(normalized)
    if not info.valid or not info.skill_name:
        return empty_install_report(
            [TargetError(reason=info.error_code or "invalid-skill-bundle")]
        )

    digest = compute_normalized_bundle_content_hash(normalized)
    targets, errors = _resolve_install_targets_with_errors(
        options.base,
        options.agents,
        options.scope,
        info.skill_name,
    )
    report = empty_install_report(errors)
    for target in targets:
        result = target_result(target)
        target_dir = Path(target.target_dir)
        metadata = read_install_metadata(target_dir)

        if not target_dir.exists():
            if write:
                write_managed_bundle(
                    target_dir,
                    app_id=options.app_id,
                    skill_name=info.skill_name,
                    digest=digest,
                    metadata=bundle_metadata,
                    files=normalized.files,
                    replace=False,
                )
            report.installed.append(result)
            continue

        if metadata is None or metadata.get("skillName") != info.skill_name:
            if options.force:
                if write:
                    write_managed_bundle(
                        target_dir,
                        app_id=options.app_id,
                        skill_name=info.skill_name,
                        digest=digest,
                        metadata=bundle_metadata,
                        files=normalized.files,
                        replace=True,
                    )
                report.updated.append(result)
            else:
                report.conflicts.append(target_status(target, "unmanaged"))
            continue
        if metadata.get("appId") != options.app_id:
            if options.force:
                if write:
                    write_managed_bundle(
                        target_dir,
                        app_id=options.app_id,
                        skill_name=info.skill_name,
                        digest=digest,
                        metadata=bundle_metadata,
                        files=normalized.files,
                        replace=True,
                    )
                report.updated.append(result)
            else:
                report.conflicts.append(target_status(target, "owner-mismatch"))
            continue
        if metadata.get("hash") == digest:
            repaired = repair_bundle_modes(normalized.files, target_dir, write=write)
            if repaired or not _installed_metadata_matches_bundle(
                metadata, bundle_metadata
            ):
                if write:
                    write_bundle_metadata(
                        target_dir,
                        app_id=options.app_id,
                        skill_name=info.skill_name,
                        digest=digest,
                        metadata=bundle_metadata,
                    )
                report.updated.append(result)
            else:
                report.skipped.append(target_status(target, "unchanged"))
            continue

        if write:
            write_managed_bundle(
                target_dir,
                app_id=options.app_id,
                skill_name=info.skill_name,
                digest=digest,
                metadata=bundle_metadata,
                files=normalized.files,
                replace=True,
            )
        report.updated.append(result)

    return report


def repair_bundle_modes(
    files: list[BundleFile], target_dir: Path, *, write: bool
) -> bool:
    repaired = False
    for file in files:
        destination = target_dir / file.path
        try:
            mode = destination.lstat().st_mode
        except FileNotFoundError:
            continue
        if stat.S_ISREG(mode) and mode & 0o777 != file.mode:
            repaired = True
            if write:
                destination.chmod(file.mode)
    return repaired


def write_bundle_metadata(
    target_dir: Path,
    *,
    app_id: str,
    skill_name: str,
    digest: str,
    metadata: dict[str, object],
) -> None:
    write_install_metadata(
        target_dir,
        app_id=app_id,
        skill_name=skill_name,
        digest=digest,
        source=str(metadata["source"]),
        source_id=_metadata_text(metadata, "source_id"),
        version=_metadata_text(metadata, "version"),
        cli_version=_metadata_text(metadata, "cli_version"),
        revision=_metadata_text(metadata, "revision"),
        provenance=_metadata_provenance(metadata),
    )


def uninstall_bundled_skill(options: UninstallOptions) -> UninstallReport:
    if not options.app_id:
        return empty_uninstall_report([TargetError(reason="invalid-app-id")])

    targets, errors = _resolve_install_targets_with_errors(
        options.base,
        options.agents,
        options.scope,
        options.skill_name,
    )
    report = empty_uninstall_report(errors)
    for target in targets:
        result = target_result(target)
        target_dir = Path(target.target_dir)
        metadata = read_install_metadata(target_dir)

        if not target_dir.exists():
            report.skipped.append(target_status(target, "missing"))
            continue
        if metadata is None or metadata.get("skillName") != options.skill_name:
            report.conflicts.append(target_status(target, "unmanaged"))
            continue
        if metadata.get("appId") != options.app_id:
            report.conflicts.append(target_status(target, "owner-mismatch"))
            continue

        reason = _remove_managed_target(
            target_dir, app_id=options.app_id, skill_name=options.skill_name
        )
        if reason is not None:
            report.conflicts.append(target_status(target, reason))
            continue
        report.removed.append(result)

    return report


def _remove_managed_target(
    target_dir: Path, *, app_id: str, skill_name: str
) -> str | None:
    quarantine = Path(
        tempfile.mkdtemp(
            prefix=f".{target_dir.name}.kitup-uninstall-",
            dir=target_dir.parent,
        )
    )
    quarantine.rmdir()
    target_dir.replace(quarantine)

    def restore() -> None:
        if target_dir.exists():
            raise RuntimeError(
                f"uninstall target changed; preserved quarantined target at {quarantine}"
            )
        quarantine.replace(target_dir)

    try:
        metadata = read_installed_metadata(quarantine)
    except Exception:
        restore()
        return "unmanaged"
    if metadata is None or metadata.skill_name != skill_name:
        restore()
        return "unmanaged"
    if metadata.app_id != app_id:
        restore()
        return "owner-mismatch"
    shutil.rmtree(quarantine)
    return None


def _resolve_bundle_and_metadata(
    skill_bundle: object, *, cwd: str | None
) -> tuple[object, dict[str, object]]:
    if isinstance(skill_bundle, DirectoryBundle):
        return normalize_directory_bundle(
            skill_bundle.path, cwd=cwd
        ), _resolved_bundled_metadata(skill_bundle.metadata)
    if isinstance(skill_bundle, FilesBundle):
        return normalize_files_bundle(skill_bundle.files), _resolved_bundled_metadata(
            skill_bundle.metadata
        )
    if isinstance(skill_bundle, GitHubBundle):
        files, metadata = fetch_github_directory_with_metadata(skill_bundle.options)
        return normalize_files_bundle(files), metadata
    raise TypeError(f"unsupported bundle: {type(skill_bundle)!r}")


def _resolve_install_targets_with_errors(
    options: BaseOptions,
    agents: str | list[str] | None,
    scope: Scope,
    skill_name: str,
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
        root = choose_scope_path(host, scope=scope, home=home, cwd=cwd)
        if root is None:
            errors.append(
                TargetError(
                    reason="unsupported-scope",
                    host_id=host.id,
                    skill_name=skill_name,
                    scope=scope,
                )
            )
            continue
        target_dir = str(root / skill_name)
        group = by_target.get(target_dir)
        if group is None:
            group = TargetGroup(skill_name=skill_name, target_dir=target_dir)
            by_target[target_dir] = group
        group.host_ids.append(host.id)

    return [by_target[path] for path in sorted(by_target)], errors


def _metadata_text(metadata: dict[str, object], key: str) -> str | None:
    value = metadata.get(key)
    return value if isinstance(value, str) and value else None


def _metadata_provenance(metadata: dict[str, object]) -> dict[str, object] | None:
    value = metadata.get("provenance")
    return value if isinstance(value, dict) and value else None


def _resolved_bundled_metadata(metadata: object) -> dict[str, object]:
    source_id = getattr(metadata, "source_id", None)
    cli_version = getattr(metadata, "cli_version", None)
    revision = getattr(metadata, "revision", None)
    provenance = getattr(metadata, "provenance", None)
    for value in (source_id, cli_version, revision):
        if value is not None and not isinstance(value, str):
            raise TypeError("invalid bundled metadata")
    if not isinstance(provenance, dict) or not all(
        isinstance(key, str) and isinstance(value, str)
        for key, value in provenance.items()
    ):
        raise TypeError("invalid bundled metadata")
    return {
        "source": "bundled",
        "source_id": source_id or None,
        "cli_version": cli_version or None,
        "revision": revision or None,
        "provenance": provenance,
    }


def _installed_metadata_matches_bundle(
    installed: dict[str, object], bundled: dict[str, object]
) -> bool:
    return all(
        installed.get(installed_key) == bundled.get(bundle_key)
        for installed_key, bundle_key in (
            ("source", "source"),
            ("sourceId", "source_id"),
            ("version", "version"),
            ("cliVersion", "cli_version"),
            ("revision", "revision"),
        )
    ) and installed.get("provenance", {}) == bundled.get("provenance", {})
