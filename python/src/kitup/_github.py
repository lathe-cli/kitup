from __future__ import annotations

import json
import os
import urllib.parse
import urllib.request

from ._paths import trim_github_path
from .types import GitHubBundleOptions, KitupError, SkillFile


def fetch_github_directory(options: GitHubBundleOptions) -> list[SkillFile]:
    files, _ = fetch_github_directory_with_metadata(options)
    return files


def fetch_github_directory_with_metadata(
    options: GitHubBundleOptions,
) -> tuple[list[SkillFile], dict[str, object]]:
    root = trim_github_path(options.path)
    if not options.owner or not options.repo or not root or not options.ref:
        raise KitupError("invalid github bundle")

    api_base = _env_base_url("KITUP_GITHUB_API_BASE_URL", "https://api.github.com")
    raw_base = _env_base_url(
        "KITUP_GITHUB_RAW_BASE_URL",
        "https://raw.githubusercontent.com",
    )

    commit = github_json(
        f"{api_base}/repos/{_encode_path_part(options.owner)}/"
        f"{_encode_path_part(options.repo)}/commits/{_encode_path_part(options.ref)}"
    )
    resolved_commit = str(commit.get("sha") or "")
    tree_sha = str(((commit.get("commit") or {}).get("tree") or {}).get("sha") or "")
    if not resolved_commit or not tree_sha:
        raise KitupError("invalid github commit")

    tree = github_json(
        f"{api_base}/repos/{_encode_path_part(options.owner)}/"
        f"{_encode_path_part(options.repo)}/git/trees/{_encode_path_part(tree_sha)}"
        "?recursive=1"
    )

    prefix = f"{root}/"
    files: list[SkillFile] = []
    for item in tree.get("tree") or []:
        if not isinstance(item, dict):
            continue
        path = str(item.get("path") or "")
        if item.get("type") != "blob" or not path.startswith(prefix):
            continue
        url = (
            f"{raw_base}/{_encode_path_part(options.owner)}/"
            f"{_encode_path_part(options.repo)}/"
            f"{_encode_path_part(resolved_commit)}/{_encode_path(path)}"
        )
        files.append(
            SkillFile(
                path=path[len(prefix) :],
                contents=github_bytes(url),
                mode=0o755 if item.get("mode") == "100755" else 0o644,
            )
        )

    if not files:
        raise KitupError("github bundle path not found")

    return files, {
        "source": "github",
        "source_id": f"github:{options.owner}/{options.repo}/{root}",
        "version": options.ref,
        "provenance": {
            "owner": options.owner,
            "repo": options.repo,
            "path": root,
            "ref": options.ref,
            "resolvedCommit": resolved_commit,
        },
    }


def github_json(url: str) -> dict[str, object]:
    with urllib.request.urlopen(_request(url), timeout=30) as response:
        return json.loads(response.read().decode("utf-8"))


def github_bytes(url: str) -> bytes:
    with urllib.request.urlopen(_request(url), timeout=30) as response:
        return response.read()


def _request(url: str) -> urllib.request.Request:
    return urllib.request.Request(url, headers={"User-Agent": "kitup"})


def _env_base_url(name: str, fallback: str) -> str:
    value = os.environ.get(name, "").rstrip("/")
    return value or fallback


def _encode_path(path: str) -> str:
    return "/".join(_encode_path_part(part) for part in path.split("/"))


def _encode_path_part(part: str) -> str:
    return urllib.parse.quote(part, safe="")
