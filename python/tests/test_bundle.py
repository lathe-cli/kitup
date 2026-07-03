import hashlib

from kitup import (
    compute_bundle_content_hash,
    directory_bundle,
    files_bundle,
    github_bundle,
    validate_skill_bundle,
)
from kitup.types import GitHubBundleOptions, SkillFile


def _skill_md(*, name: str = "basic", description: str = "demo") -> str:
    return f"---\nname: {name}\ndescription: {description}\n---\n"


def test_validate_skill_bundle_rejects_parent_segments():
    bundle = files_bundle([SkillFile(path="../bad.txt", contents="x")])

    result = validate_skill_bundle(bundle)

    assert result.valid is False
    assert result.error_code == "invalid-skill-bundle"


def test_validate_skill_bundle_rejects_missing_directory(tmp_path):
    bundle = directory_bundle(str(tmp_path / "missing"))

    result = validate_skill_bundle(bundle)

    assert result.valid is False
    assert result.error_code == "invalid-skill-bundle"


def test_validate_skill_bundle_requires_frontmatter():
    bundle = files_bundle([SkillFile(path="SKILL.md", contents="name: basic\n")])

    result = validate_skill_bundle(bundle)

    assert result.valid is False
    assert result.error_code == "invalid-frontmatter"


def test_validate_skill_bundle_accepts_valid_skill_file():
    bundle = files_bundle([SkillFile(path="SKILL.md", contents=_skill_md())])

    result = validate_skill_bundle(bundle)

    assert result.valid is True
    assert result.skill_name == "basic"
    assert result.description == "demo"
    assert result.error_code is None


def test_compute_bundle_content_hash_ignores_kitup_metadata(tmp_path):
    root = tmp_path / "skill"
    root.mkdir()
    (root / "SKILL.md").write_text(_skill_md(), encoding="utf-8")
    (root / ".kitup.json").write_text('{"ignored": true}', encoding="utf-8")

    with_metadata = compute_bundle_content_hash(directory_bundle(str(root)))

    (root / ".kitup.json").write_text('{"ignored": false}', encoding="utf-8")
    without_metadata = compute_bundle_content_hash(directory_bundle(str(root)))

    expected = (
        "sha256:"
        + hashlib.sha256(
            b"SKILL.md\x00" + _skill_md().encode("utf-8") + b"\x00"
        ).hexdigest()
    )
    assert with_metadata == without_metadata == expected


def test_compute_bundle_content_hash_ignores_editor_junk_files(tmp_path):
    root = tmp_path / "skill"
    root.mkdir()
    (root / "SKILL.md").write_text(_skill_md(), encoding="utf-8")
    (root / "notes.txt~").write_text("backup", encoding="utf-8")
    (root / "scratch.swp").write_text("swap", encoding="utf-8")
    (root / ".DS_Store").write_text("junk", encoding="utf-8")

    digest = compute_bundle_content_hash(directory_bundle(str(root)))

    expected = (
        "sha256:"
        + hashlib.sha256(
            b"SKILL.md\x00" + _skill_md().encode("utf-8") + b"\x00"
        ).hexdigest()
    )
    assert digest == expected


def test_github_bundle_uses_fetched_relative_paths(monkeypatch):
    class _Response:
        def __init__(self, payload: bytes):
            self._payload = payload

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def read(self) -> bytes:
            return self._payload

    payloads = {
        "https://api.github.com/repos/acme/skills/commits/v1": (
            b'{"sha":"abc123","commit":{"tree":{"sha":"tree123"}}}'
        ),
        "https://api.github.com/repos/acme/skills/git/trees/tree123?recursive=1": (
            b'{"tree":[{"path":"skills/basic/SKILL.md","type":"blob","mode":"100644"},'
            b'{"path":"skills/basic/bin/run.sh","type":"blob","mode":"100755"},'
            b'{"path":"skills/other/SKILL.md","type":"blob","mode":"100644"}]}'
        ),
        "https://raw.githubusercontent.com/acme/skills/abc123/skills/basic/SKILL.md": _skill_md().encode(
            "utf-8"
        ),
        "https://raw.githubusercontent.com/acme/skills/abc123/skills/basic/bin/run.sh": b"#!/bin/sh\n",
    }

    def fake_urlopen(url, timeout=30):
        key = getattr(url, "full_url", url)
        payload = payloads.get(key)
        if payload is None:
            raise AssertionError(
                f"unexpected network call: {key!r} timeout={timeout!r}"
            )
        return _Response(payload)

    monkeypatch.setattr("kitup._github.urllib.request.urlopen", fake_urlopen)

    bundle = github_bundle(
        GitHubBundleOptions(owner="acme", repo="skills", path="skills/basic", ref="v1")
    )

    result = validate_skill_bundle(bundle)

    assert result.valid is True
    digest = compute_bundle_content_hash(bundle)
    expected = hashlib.sha256()
    expected.update(b"SKILL.md\x00")
    expected.update(_skill_md().encode("utf-8"))
    expected.update(b"\x00")
    expected.update(b"bin/run.sh\x00")
    expected.update(b"#!/bin/sh\n")
    expected.update(b"\x00")
    assert digest == f"sha256:{expected.hexdigest()}"


def test_validate_skill_bundle_rejects_duplicate_paths():
    bundle = files_bundle(
        [
            SkillFile(path="SKILL.md", contents=_skill_md()),
            SkillFile(path="SKILL.md", contents=_skill_md(description="other")),
        ]
    )

    result = validate_skill_bundle(bundle)

    assert result.valid is False
    assert result.error_code == "invalid-skill-bundle"


def test_validate_skill_bundle_rejects_github_bundle_without_root_path():
    bundle = github_bundle(
        GitHubBundleOptions(owner="acme", repo="skills", path="/", ref="main")
    )

    result = validate_skill_bundle(bundle)

    assert result.valid is False
    assert result.error_code == "invalid-skill-bundle"
