from pathlib import Path

from kitup import detect_hosts, load_host_spec, resolve_hosts


def test_load_host_spec_uses_baked_default_when_no_override():
    spec = load_host_spec()
    assert spec.schema_version == 1
    assert len(spec.hosts) == 78
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


def test_detect_hosts_skips_generic_detect_paths_and_sorts_by_scope_path(
    base,
    host_options,
):
    home = Path(base.home)
    (home / ".claude").mkdir()
    (home / ".codex").mkdir()

    base = host_options(["generic", "claude-code", "codex"])

    hosts = detect_hosts(
        base,
        scope="user",
    )

    assert [host.id for host in hosts] == ["codex", "claude-code"]


def test_detect_hosts_scans_all_non_generic_detect_paths(
    base,
):
    home = Path(base.home)
    (home / ".kimi").mkdir()

    hosts = detect_hosts(
        base,
        scope="user",
    )

    assert "kimi-cli" in [host.id for host in hosts]


def test_detect_hosts_never_counts_generic_detect_paths(
    base,
):
    home = Path(base.home)
    workspace = Path(base.cwd)
    (home / ".agents").mkdir()
    (home / ".agents" / "skills").mkdir()
    (home / ".config").mkdir()
    (home / ".config" / "agents").mkdir()
    (workspace / "package.json").write_text("{}")

    hosts = detect_hosts(
        base,
        scope="user",
    )

    assert hosts == []
