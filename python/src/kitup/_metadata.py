from __future__ import annotations

import json
from pathlib import Path


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
    except OSError, ValueError:
        return None
    if not isinstance(payload, dict):
        return None
    return payload
