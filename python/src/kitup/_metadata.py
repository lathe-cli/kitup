from __future__ import annotations

import json
import re
from pathlib import Path

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
    if provenance is not None:
        payload["provenance"] = provenance
    (target_dir / ".kitup.json").write_text(
        json.dumps(payload, indent=2) + "\n", encoding="utf-8"
    )


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
    return (
        isinstance(app_id, str)
        and bool(app_id)
        and isinstance(skill_name, str)
        and is_valid_skill_name(skill_name)
        and source in ("bundled", "github")
        and isinstance(digest, str)
        and bool(digest)
    )
