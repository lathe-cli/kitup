import io
import json

import kitup
from kitup import (
    BaseOptions,
    GitHubBundleOptions,
    InstallOptions,
    InstallSelectionOptions,
    InstallWorkflowOptions,
    SkillFile,
    agent_selector_from_flags,
    classify_install_workflow_exit,
    directory_bundle,
    github_bundle,
    install_flag_error,
    install_workflow_error,
    parse_install_flags,
    plan_bundled_skill,
    parse_scope_flag,
    resolve_install_selection,
    run_bundled_skill_install,
    run_bundled_skill_install_with_io,
)
from kitup.workflow import split_flag_values


def write_hosts_file(path, hosts) -> None:
    path.write_text(
        json.dumps(
            {
                "$schema": "./hosts.schema.json",
                "schemaVersion": 1,
                "hosts": hosts,
            }
        ),
        encoding="utf-8",
    )


def test_parse_install_flags_defaults_to_user_auto():
    parsed = parse_install_flags({})

    assert parsed.scope == "user"
    assert parsed.scope_set is False
    assert parsed.agents == "auto"
    assert parsed.yes is False
    assert parsed.dry_run is False
    assert parsed.force is False
    assert parsed.errors == []


def test_parse_install_flags_explicit_scope_agents_and_errors():
    parsed = parse_install_flags(
        {
            "scope": "global",
            "agents": ["*", "codex,claude-code"],
            "yes": True,
            "dryRun": True,
            "force": True,
        }
    )

    assert parsed.scope == "user"
    assert parsed.scope_set is True
    assert parsed.agents == "*"
    assert parsed.yes is True
    assert parsed.dry_run is True
    assert parsed.force is True
    assert parsed.errors == [
        {"flag": "scope", "reason": "invalid-scope", "value": "global"},
        {
            "flag": "agent",
            "reason": "agent-star-must-be-alone",
            "value": "*,codex,claude-code",
        },
    ]


def test_flag_helpers_normalize_lists():
    errors: list[dict[str, str]] = []

    assert split_flag_values(["codex,claude-code", " codex "]) == [
        "codex",
        "claude-code",
        "codex",
    ]
    assert agent_selector_from_flags(["codex,claude-code", "codex"], errors) == [
        "codex",
        "claude-code",
    ]
    assert parse_scope_flag("project", errors) == "project"
    assert errors == []


def test_resolve_install_selection_requires_agents_without_tty_or_yes(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    hosts_file = tmp_path / "hosts.json"
    home.mkdir()
    workspace.mkdir()
    (home / ".codex").mkdir()
    write_hosts_file(
        hosts_file,
        [
            {
                "id": "codex",
                "displayName": "Codex",
                "projectSkillsDirs": [".agents/skills"],
                "userSkillsDirs": ["~/.agents/skills"],
                "detect": ["~/.codex"],
                "status": "verified",
            }
        ],
    )

    selection = resolve_install_selection(
        InstallSelectionOptions(
            base=BaseOptions(
                home=str(home), cwd=str(workspace), hosts_file=str(hosts_file)
            ),
            scope="user",
            stdin_tty=False,
            yes=False,
        )
    )

    assert selection.action == "error"
    assert selection.detected_host_ids == ["codex"]
    assert selection.errors == [{"reason": "agent-selection-required"}]


def test_resolve_install_selection_tty_prompts_for_multiple_detected_hosts(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    hosts_file = tmp_path / "hosts.json"
    home.mkdir()
    workspace.mkdir()
    (home / ".codex").mkdir()
    (home / ".claude").mkdir()
    write_hosts_file(
        hosts_file,
        [
            {
                "id": "codex",
                "displayName": "Codex",
                "projectSkillsDirs": [".agents/skills"],
                "userSkillsDirs": ["~/.agents/skills"],
                "detect": ["~/.codex"],
                "status": "verified",
            },
            {
                "id": "claude-code",
                "displayName": "Claude Code",
                "projectSkillsDirs": [".claude/skills"],
                "userSkillsDirs": ["~/.claude/skills"],
                "detect": ["~/.claude"],
                "status": "verified",
            },
        ],
    )

    selection = resolve_install_selection(
        InstallSelectionOptions(
            base=BaseOptions(
                home=str(home), cwd=str(workspace), hosts_file=str(hosts_file)
            ),
            scope="user",
            stdin_tty=True,
            yes=False,
        )
    )

    assert selection.action == "select-agents"
    assert selection.candidate_host_ids == ["codex", "claude-code"]
    assert selection.detected_host_ids == ["codex", "claude-code"]
    assert selection.needs_confirmation is True


def test_resolve_install_selection_explicit_agents_with_unknown_host_is_pure_error(
    tmp_path,
):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    hosts_file = tmp_path / "hosts.json"
    home.mkdir()
    workspace.mkdir()
    write_hosts_file(
        hosts_file,
        [
            {
                "id": "codex",
                "displayName": "Codex",
                "projectSkillsDirs": [".agents/skills"],
                "userSkillsDirs": ["~/.agents/skills"],
                "detect": ["~/.codex"],
                "status": "verified",
            }
        ],
    )

    selection = resolve_install_selection(
        InstallSelectionOptions(
            base=BaseOptions(
                home=str(home), cwd=str(workspace), hosts_file=str(hosts_file)
            ),
            scope="user",
            agents=["codex", "missing-agent"],
            stdin_tty=False,
            yes=False,
        )
    )

    assert selection.action == "error"
    assert selection.selected_host_ids == []
    assert selection.candidate_host_ids == []
    assert selection.detected_host_ids == []
    assert selection.errors == [{"agent": "missing-agent", "reason": "unknown-host"}]


def test_classify_install_workflow_exit_reports_conflict():
    exit_info = classify_install_workflow_exit(
        {
            "canceled": False,
            "selection": {"errors": []},
            "report": {"conflicts": [{}], "errors": []},
        }
    )

    assert exit_info.ok is False
    assert exit_info.code == "conflict"
    assert exit_info.message == "Installation has conflicts."


def test_error_helpers_map_flag_and_workflow_failures():
    assert (
        str(install_flag_error([{"reason": "invalid-scope"}]))
        == "Invalid install flags."
    )
    assert (
        install_workflow_error(
            {
                "canceled": False,
                "selection": {"errors": [{"reason": "scope-selection-required"}]},
                "report": {"conflicts": [], "errors": []},
            }
        ).args[0]
        == "Agent selection failed."
    )
    assert (
        install_workflow_error(
            {
                "canceled": True,
                "selection": {"errors": []},
                "report": {"conflicts": [], "errors": []},
            }
        )
        is None
    )


def test_run_bundled_skill_install_scope_prompt_and_top_level_exports(tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    skill = workspace / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text(
        "---\nname: basic\ndescription: demo\n---\n",
        encoding="utf-8",
    )
    output = io.StringIO()

    report = run_bundled_skill_install_with_io(
        InstallWorkflowOptions(
            install=InstallOptions(
                base=BaseOptions(home=str(home), cwd=str(workspace)),
                app_id="example-cli",
                skill_bundle=directory_bundle(str(skill)),
                scope="user",
                agents=["codex"],
            ),
            stdin_tty=True,
            prompt_scope=True,
            scope_set=False,
        ),
        io.StringIO("project\ny\n"),
        output,
    )

    assert report.scope == "project"
    assert report.canceled is False
    assert "Select install scope:" in output.getvalue()
    assert "Proceed? [y/N] " in output.getvalue()
    assert (workspace / ".agents" / "skills" / "basic" / "SKILL.md").exists()

    assert kitup.parse_install_flags is parse_install_flags
    assert kitup.parse_scope_flag is parse_scope_flag
    assert kitup.agent_selector_from_flags is agent_selector_from_flags
    assert kitup.resolve_install_selection is resolve_install_selection
    assert kitup.classify_install_workflow_exit is classify_install_workflow_exit
    assert kitup.install_flag_error is install_flag_error
    assert kitup.install_workflow_error is install_workflow_error
    assert kitup.run_bundled_skill_install is run_bundled_skill_install
    assert kitup.run_bundled_skill_install_with_io is run_bundled_skill_install_with_io
    assert kitup.GitHubBundleOptions is GitHubBundleOptions
    assert kitup.SkillFile is SkillFile


class _TTYInput(io.StringIO):
    def isatty(self) -> bool:
        return True


class _NonTTYInput(io.StringIO):
    def isatty(self) -> bool:
        return False


def test_run_bundled_skill_install_uses_stdio_defaults_for_interactive_flow(
    monkeypatch, tmp_path
):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    skill = workspace / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text(
        "---\nname: basic\ndescription: demo\n---\n",
        encoding="utf-8",
    )

    stdin = _TTYInput("project\ny\n")
    stdout = io.StringIO()
    monkeypatch.setattr("sys.stdin", stdin)
    monkeypatch.setattr("sys.stdout", stdout)

    report = run_bundled_skill_install(
        InstallWorkflowOptions(
            install=InstallOptions(
                base=BaseOptions(home=str(home), cwd=str(workspace)),
                app_id="example-cli",
                skill_bundle=directory_bundle(str(skill)),
                scope="user",
                agents=["codex"],
            ),
            prompt_scope=True,
            scope_set=False,
        )
    )

    assert report.scope == "project"
    assert report.canceled is False
    assert "Select install scope:" in stdout.getvalue()
    assert "Proceed? [y/N] " in stdout.getvalue()
    assert (workspace / ".agents" / "skills" / "basic" / "SKILL.md").exists()


def test_run_bundled_skill_install_infers_tty_from_custom_input_stream(
    monkeypatch, tmp_path
):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()
    skill = workspace / "skill"
    skill.mkdir()
    (skill / "SKILL.md").write_text(
        "---\nname: basic\ndescription: demo\n---\n",
        encoding="utf-8",
    )

    monkeypatch.setattr("sys.stdin", _NonTTYInput(""))
    stdout = io.StringIO()

    report = run_bundled_skill_install(
        InstallWorkflowOptions(
            install=InstallOptions(
                base=BaseOptions(home=str(home), cwd=str(workspace)),
                app_id="example-cli",
                skill_bundle=directory_bundle(str(skill)),
                scope="user",
                agents=["codex"],
            ),
            prompt_scope=True,
            scope_set=False,
            input=_TTYInput("project\ny\n"),
            output=stdout,
        )
    )

    assert report.scope == "project"
    assert report.canceled is False
    assert "Select install scope:" in stdout.getvalue()
    assert "Proceed? [y/N] " in stdout.getvalue()
    assert (workspace / ".agents" / "skills" / "basic" / "SKILL.md").exists()


def test_plan_bundled_skill_uses_single_github_snapshot(monkeypatch, tmp_path):
    home = tmp_path / "home"
    workspace = tmp_path / "workspace"
    home.mkdir()
    workspace.mkdir()

    fetch_calls: list[str] = []

    def fake_fetch_with_metadata(options):
        fetch_calls.append(f"{options.owner}/{options.repo}@{options.ref}")
        return (
            [
                SkillFile(
                    path="SKILL.md",
                    contents="---\nname: github-basic\ndescription: demo\n---\n",
                ),
                SkillFile(path="references/guide.md", contents="Guide\n"),
            ],
            {
                "source": "github",
                "source_id": "github:acme/skills/skills/github-basic",
                "version": "main",
                "provenance": {
                    "owner": "acme",
                    "repo": "skills",
                    "path": "skills/github-basic",
                    "ref": "main",
                    "resolvedCommit": "abc123",
                },
            },
        )

    def unexpected_refetch(_options):
        raise AssertionError("bundle was re-fetched after snapshot resolution")

    monkeypatch.setattr(
        "kitup.install.fetch_github_directory_with_metadata", fake_fetch_with_metadata
    )
    monkeypatch.setattr("kitup.bundle.fetch_github_directory", unexpected_refetch)

    report = plan_bundled_skill(
        InstallOptions(
            base=BaseOptions(home=str(home), cwd=str(workspace)),
            app_id="example-cli",
            skill_bundle=github_bundle(
                GitHubBundleOptions(
                    owner="acme",
                    repo="skills",
                    path="skills/github-basic",
                    ref="main",
                )
            ),
            scope="user",
            agents=["codex"],
        )
    )

    assert fetch_calls == ["acme/skills@main"]
    assert report.errors == []
    assert [item.skill_name for item in report.installed] == ["github-basic"]
