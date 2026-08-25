from __future__ import annotations

import json
import re
from pathlib import Path

from .types import InstalledMetadata, KitupError

_SKILL_NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")


def is_valid_skill_name(skill_name: str) -> bool:
    return bool(_SKILL_NAME_RE.fullmatch(skill_name))


def write_install_metadata(
    target_dir: Path,
    *,
    app_id: str,
    skill_name: str,
    digest: str,
    source: str,
    source_id: str | None = None,
    version: str | None = None,
    cli_version: str | None = None,
    revision: str | None = None,
    provenance: dict[str, object] | None = None,
) -> None:
    payload = {
        "schemaVersion": 1,
        "appId": app_id,
        "skillName": skill_name,
        "source": source,
        "hash": digest,
    }
    if source_id is not None:
        payload["sourceId"] = source_id
    if version is not None:
        payload["version"] = version
    if cli_version is not None:
        payload["cliVersion"] = cli_version
    if revision is not None:
        payload["revision"] = revision
    if provenance is not None:
        payload["provenance"] = provenance
    (target_dir / ".kitup.json").write_text(
        json.dumps(payload, indent=2) + "\n", encoding="utf-8"
    )


def read_install_metadata(target_dir: Path) -> dict[str, object] | None:
    try:
        metadata = read_installed_metadata(target_dir)
    except KitupError:
        return None
    if metadata is None:
        return None
    payload: dict[str, object] = {
        "schemaVersion": metadata.schema_version,
        "appId": metadata.app_id,
        "skillName": metadata.skill_name,
        "source": metadata.source,
        "hash": metadata.hash,
    }
    for key, value in (
        ("sourceId", metadata.source_id),
        ("version", metadata.version),
        ("cliVersion", metadata.cli_version),
        ("revision", metadata.revision),
    ):
        if value is not None:
            payload[key] = value
    if metadata.provenance:
        payload["provenance"] = metadata.provenance
    return payload


def read_installed_metadata(target_dir: Path) -> InstalledMetadata | None:
    metadata_file = target_dir / ".kitup.json"
    if not metadata_file.exists():
        return None
    try:
        payload = json.loads(metadata_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        raise KitupError("invalid installed metadata")
    if not isinstance(payload, dict):
        raise KitupError("invalid installed metadata")
    if not is_owned_metadata(payload):
        raise KitupError("invalid installed metadata")
    return InstalledMetadata(
        schema_version=1,
        app_id=str(payload["appId"]),
        skill_name=str(payload["skillName"]),
        source=payload["source"],
        hash=str(payload["hash"]),
        source_id=_optional_text(payload, "sourceId"),
        version=_optional_text(payload, "version"),
        cli_version=_optional_text(payload, "cliVersion"),
        revision=_optional_text(payload, "revision"),
        provenance=dict(payload.get("provenance", {})),
    )


def is_owned_metadata(payload: dict[str, object]) -> bool:
    if payload.get("schemaVersion") != 1:
        return False
    app_id = payload.get("appId")
    skill_name = payload.get("skillName")
    source = payload.get("source")
    digest = payload.get("hash")
    if not (
        isinstance(app_id, str)
        and bool(app_id)
        and isinstance(skill_name, str)
        and is_valid_skill_name(skill_name)
        and source in ("bundled", "github")
        and isinstance(digest, str)
        and bool(digest)
    ):
        return False
    for key in ("sourceId", "version", "cliVersion", "revision"):
        if key not in payload:
            continue
        value = payload[key]
        if not isinstance(value, str) or not value:
            return False
    if "provenance" not in payload:
        return True
    provenance = payload["provenance"]
    return isinstance(provenance, dict) and all(
        isinstance(key, str) and isinstance(value, str)
        for key, value in provenance.items()
    )


def _optional_text(payload: dict[str, object], key: str) -> str | None:
    value = payload.get(key)
    return value if isinstance(value, str) else None
