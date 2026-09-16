from __future__ import annotations

import json
import re
from pathlib import Path

from .types import InstalledMetadata

_SKILL_NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")


def is_valid_skill_name(skill_name: str) -> bool:
    return bool(_SKILL_NAME_RE.fullmatch(skill_name))


def write_install_metadata(target_dir: Path, payload: dict[str, object]) -> None:
    (target_dir / ".kitup.json").write_text(
        json.dumps(payload, indent=2) + "\n", encoding="utf-8"
    )


def ownership_conflict(
    metadata: dict[str, object] | None, app_id: str, skill_name: str
) -> str | None:
    if metadata is None or metadata.get("skillName") != skill_name:
        return "unmanaged"
    if metadata.get("appId") != app_id:
        return "owner-mismatch"
    return None


def read_install_metadata(target_dir: Path) -> dict[str, object] | None:
    metadata_file = target_dir / ".kitup.json"
    if not metadata_file.exists():
        return None
    try:
        payload = json.loads(metadata_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    if not isinstance(payload, dict):
        return None
    if not is_owned_metadata(payload):
        return None
    return payload


def is_owned_metadata(payload: dict[str, object]) -> bool:
    schema_version = payload.get("schemaVersion")
    if type(schema_version) is not int or schema_version != 1:
        return False
    app_id = payload.get("appId")
    skill_name = payload.get("skillName")
    source = payload.get("source")
    digest = payload.get("hash")
    required_valid = (
        isinstance(app_id, str)
        and bool(app_id)
        and isinstance(skill_name, str)
        and is_valid_skill_name(skill_name)
        and source in ("bundled", "github")
        and isinstance(digest, str)
        and bool(digest)
    )
    if not required_valid:
        return False
    for key in ("sourceId", "version", "cliVersion", "cliRevision"):
        if key in payload and not isinstance(payload[key], str):
            return False
    provenance = payload.get("provenance")
    return provenance is None or (
        isinstance(provenance, dict)
        and all(
            isinstance(key, str) and isinstance(value, str)
            for key, value in provenance.items()
        )
    )


def _installed_metadata(payload: dict[str, object]) -> InstalledMetadata:
    return InstalledMetadata(
        schema_version=1,
        app_id=str(payload["appId"]),
        skill_name=str(payload["skillName"]),
        source=str(payload["source"]),
        hash=str(payload["hash"]),
        source_id=_nonempty_metadata_text(payload, "sourceId"),
        version=_nonempty_metadata_text(payload, "version"),
        cli_version=_nonempty_metadata_text(payload, "cliVersion"),
        cli_revision=_nonempty_metadata_text(payload, "cliRevision"),
        provenance=_metadata_provenance(payload) or None,
    )


def _installed_metadata_dict(
    *, app_id: str, skill_name: str, digest: str, metadata: dict[str, object]
) -> dict[str, object]:
    value: dict[str, object] = {
        "schemaVersion": 1,
        "appId": app_id,
        "skillName": skill_name,
        "source": metadata["source"],
        "hash": digest,
    }
    for source_key, target_key in (
        ("source_id", "sourceId"),
        ("version", "version"),
        ("cli_version", "cliVersion"),
        ("cli_revision", "cliRevision"),
    ):
        field_value = _metadata_text(metadata, source_key)
        if field_value is not None:
            value[target_key] = field_value
    provenance = _metadata_provenance(metadata)
    if provenance:
        value["provenance"] = provenance
    return value


def _metadata_text(metadata: dict[str, object], key: str) -> str | None:
    value = metadata.get(key)
    return value if isinstance(value, str) else None


def _nonempty_metadata_text(metadata: dict[str, object], key: str) -> str | None:
    value = _metadata_text(metadata, key)
    return value or None


def _metadata_provenance(metadata: dict[str, object]) -> dict[str, object] | None:
    value = metadata.get("provenance")
    return value if isinstance(value, dict) else None
