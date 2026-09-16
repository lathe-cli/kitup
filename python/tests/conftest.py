from dataclasses import replace
import json

import pytest

from kitup import BaseOptions, InstallOptions, directory_bundle


@pytest.fixture
def base(tmp_path):
    for name in ("home", "workspace"):
        (tmp_path / name).mkdir()
    return BaseOptions(home=str(tmp_path / "home"), cwd=str(tmp_path / "workspace"))


@pytest.fixture
def install_options(base, tmp_path):
    skill = tmp_path / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text("---\nname: basic\ndescription: demo\n---\n")
    return InstallOptions(
        base, "example-cli", directory_bundle(str(skill)), "user", ["codex"]
    )


@pytest.fixture
def host_options(base, tmp_path):
    hosts = {
        "codex": (
            "Codex",
            ".agents/skills",
            ["~/.agents/skills", "~/.codex/skills"],
            ["~/.codex", "~/.agents/skills", "~/.agents"],
        ),
        "claude-code": (
            "Claude Code",
            ".claude/skills",
            ["~/.claude/skills"],
            ["~/.claude"],
        ),
        "eve": ("Eve", "agent/skills", [], ["agent", "package.json"]),
        "generic": ("Generic", ".agents/skills", ["~/.agents/skills"], ["~/.agents"]),
    }

    def write(ids):
        records = []
        for host_id in ids:
            display, project, user, detect = hosts[host_id]
            records.append(
                dict(
                    id=host_id,
                    displayName=display,
                    projectSkillsDirs=[project],
                    userSkillsDirs=user,
                    detect=detect,
                    status="community" if host_id in ("eve", "generic") else "verified",
                )
            )
        path = tmp_path / "hosts.json"
        path.write_text(json.dumps({"schemaVersion": 1, "hosts": records}))
        return replace(base, hosts_file=str(path))

    return write
