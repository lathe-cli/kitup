import json

from kitup import detect_hosts, load_host_spec, resolve_hosts
from kitup.types import BaseOptions


def test_load_host_spec_uses_baked_default_when_no_override():
    spec = load_host_spec()
    assert spec.schema_version == 1
    assert len(spec.hosts) == 72
    assert spec.hosts[0].id == "adal"


def test_resolve_hosts_maps_kimi_alias_to_canonical_id():
    spec = load_host_spec()

    hosts, errors = resolve_hosts(["kimi-code-cli"], spec.hosts)

    assert [host.id for host in hosts] == ["kimi-cli"]
    assert errors == []


def test_resolve_hosts_reports_unknown_ids():
    spec = load_host_spec()

    hosts, errors = resolve_hosts(["missing-agent"], spec.hosts)

    assert hosts == []
    assert errors == [{"agent": "missing-agent", "reason": "unknown-host"}]


def test_detect_hosts_skips_generic_detect_paths_and_sorts_by_scope_path(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".claude").mkdir()
    (home / ".codex").mkdir()

    hosts_file = tmp_path / "hosts.json"
    hosts_file.write_text(
        json.dumps(
            {
                "$schema": "./hosts.schema.json",
                "schemaVersion": 1,
                "hosts": [
                    {
                        "id": "generic",
                        "displayName": "Generic",
                        "projectSkillsDirs": [".agents/skills"],
                        "userSkillsDirs": ["~/.agents/skills"],
                        "detect": ["~/.agents"],
                        "status": "community",
                    },
                    {
                        "id": "claude-code",
                        "displayName": "Claude Code",
                        "projectSkillsDirs": [".claude/skills"],
                        "userSkillsDirs": ["~/.claude/skills"],
                        "detect": ["~/.claude"],
                        "status": "verified",
                    },
                    {
                        "id": "codex",
                        "displayName": "Codex",
                        "projectSkillsDirs": [".agents/skills"],
                        "userSkillsDirs": ["~/.agents/skills", "~/.codex/skills"],
                        "detect": ["~/.codex"],
                        "status": "verified",
                    },
                ],
            }
        )
    )

    hosts = detect_hosts(
        BaseOptions(
            home=str(home),
            cwd=str(workspace),
            hosts_file=str(hosts_file),
        ),
        scope="user",
    )

    assert [host.id for host in hosts] == ["codex", "claude-code"]


def test_detect_hosts_does_not_scan_past_primary_generic_detect_path(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    (home / ".kimi").mkdir()

    hosts = detect_hosts(
        BaseOptions(
            home=str(home),
            cwd=str(workspace),
        ),
        scope="user",
    )

    assert "kimi-cli" not in [host.id for host in hosts]
