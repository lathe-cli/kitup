from __future__ import annotations

from dataclasses import asdict
import stat
import shutil
import tempfile
from pathlib import Path

from ._metadata import (
    _installed_metadata,
    _installed_metadata_dict,
    ownership_conflict,
    read_install_metadata,
    write_install_metadata,
)
from .bundle import (
    _resolve_bundle_and_metadata,
    _is_github_bundle,
    copy_normalized_bundle,
    compute_normalized_bundle_content_hash,
    validate_normalized_skill_bundle,
)
from .hosts import _resolve_install_targets_with_errors
from .types import (
    BundleFile,
    InstalledMetadata,
    InstalledTarget,
    InstallOptions,
    InstallReport,
    KitupError,
    StatusOptions,
    StatusReport,
    TargetError,
    TargetGroup,
    TargetResult,
    TargetStatus,
    UninstallOptions,
    UninstallReport,
)


def target_result(target: TargetGroup) -> TargetResult:
    hosts = (
        {"host_id": target.host_ids[0]}
        if len(target.host_ids) == 1
        else {"host_ids": list(target.host_ids)}
    )
    return TargetResult(
        skill_name=target.skill_name, target_dir=target.target_dir, **hosts
    )


def target_status(target: TargetGroup, reason: str) -> TargetStatus:
    return TargetStatus(**asdict(target_result(target)), reason=reason)


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
        write_install_metadata(staged_dir, metadata)
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
        return InstallReport(errors=[TargetError(reason="invalid-app-id")])
    try:
        normalized, bundle_metadata = _resolve_bundle_and_metadata(
            options.skill_bundle, cwd=options.base.cwd
        )
    except Exception:
        reason = (
            "bundle-resolve-failed"
            if _is_github_bundle(options.skill_bundle)
            else "invalid-skill-bundle"
        )
        return InstallReport(errors=[TargetError(reason=reason)])
    info = validate_normalized_skill_bundle(normalized)
    if not info.valid or not info.skill_name:
        return InstallReport(
            errors=[TargetError(reason=info.error_code or "invalid-skill-bundle")]
        )
    digest = compute_normalized_bundle_content_hash(normalized)
    desired = _installed_metadata_dict(
        app_id=options.app_id,
        skill_name=info.skill_name,
        digest=digest,
        metadata=bundle_metadata,
    )
    targets, errors = _resolve_install_targets_with_errors(
        options.base, options.agents, options.scope, info.skill_name
    )
    report = InstallReport(errors=errors)
    for target in targets:
        target_dir = Path(target.target_dir)
        metadata = read_install_metadata(target_dir)
        exists = target_dir.exists()
        conflict = ownership_conflict(metadata, options.app_id, info.skill_name)
        if exists and conflict and not options.force:
            report.conflicts.append(target_status(target, conflict))
            continue
        if exists and not conflict and metadata.get("hash") == digest:
            repaired = repair_bundle_modes(normalized.files, target_dir, write=write)
            if not repaired and not (
                bundle_metadata.get("explicit") and metadata != desired
            ):
                report.skipped.append(target_status(target, "unchanged"))
                continue
            if write:
                write_install_metadata(target_dir, desired)
        elif write:
            write_managed_bundle(
                target_dir, files=normalized.files, metadata=desired, replace=exists
            )
        (report.updated if exists else report.installed).append(target_result(target))
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


def uninstall_bundled_skill(options: UninstallOptions) -> UninstallReport:
    if not options.app_id:
        return UninstallReport(errors=[TargetError(reason="invalid-app-id")])

    targets, errors = _resolve_install_targets_with_errors(
        options.base,
        options.agents,
        options.scope,
        options.skill_name,
        uninstall_app_id=options.app_id,
    )
    report = UninstallReport(errors=errors)
    for target in targets:
        result = target_result(target)
        target_dir = Path(target.target_dir)
        metadata = read_install_metadata(target_dir)

        if not target_dir.exists():
            report.skipped.append(target_status(target, "missing"))
            continue
        conflict = ownership_conflict(metadata, options.app_id, options.skill_name)
        if conflict:
            report.conflicts.append(target_status(target, conflict))
            continue

        reason = _remove_managed_skill(
            target_dir,
            app_id=options.app_id,
            skill_name=options.skill_name,
        )
        if reason is not None:
            report.conflicts.append(target_status(target, reason))
            continue
        report.removed.append(result)

    return report


def status_bundled_skill(options: StatusOptions) -> StatusReport:
    if not options.app_id:
        return StatusReport(errors=[TargetError(reason="invalid-app-id")])
    targets, errors = _resolve_install_targets_with_errors(
        options.base,
        options.agents,
        options.scope,
        options.skill_name,
        uninstall_app_id=options.app_id,
    )
    report = StatusReport(errors=errors)
    for target in targets:
        result = target_result(target)
        target_dir = Path(target.target_dir)
        metadata = read_install_metadata(target_dir)
        if not target_dir.exists():
            report.missing.append(result)
        elif conflict := ownership_conflict(
            metadata, options.app_id, options.skill_name
        ):
            report.conflicts.append(target_status(target, conflict))
        else:
            report.installed.append(
                InstalledTarget(
                    **asdict(result),
                    metadata=_installed_metadata(metadata),
                )
            )
    return report


def read_installed_metadata(
    target_dir: str | Path,
) -> InstalledMetadata | None:
    target = Path(target_dir)
    if not target.exists():
        return None
    metadata = read_install_metadata(target)
    if metadata is None:
        raise KitupError("unmanaged install metadata")
    return _installed_metadata(metadata)


def _remove_managed_skill(
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
    metadata = read_install_metadata(quarantine)
    reason = ownership_conflict(metadata, app_id, skill_name)
    if reason is not None:
        if target_dir.exists():
            raise KitupError(f"cannot restore changed install: {target_dir}")
        quarantine.replace(target_dir)
        return reason
    shutil.rmtree(quarantine)
    return None
