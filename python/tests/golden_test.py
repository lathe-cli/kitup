from __future__ import annotations

from dataclasses import asdict
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io
import json
import os
from pathlib import Path
import shutil
import tempfile
import threading

from kitup import (
    BaseOptions,
    InstallOptions,
    InstallSelectionOptions,
    InstallWorkflowOptions,
    ParsedInstallFlags,
    UninstallOptions,
    classify_install_workflow_exit,
    compute_bundle_content_hash,
    detect_hosts,
    directory_bundle,
    files_bundle,
    github_bundle,
    install_bundled_skill,
    load_host_spec,
    parse_install_flags,
    plan_bundled_skill,
    resolve_hosts,
    resolve_install_selection,
    run_bundled_skill_install_with_io,
    uninstall_bundled_skill,
    update_bundled_skill,
    validate_skill_bundle,
)
from kitup.types import GitHubBundleOptions, SkillFile


def test_golden_cases():
    cases = json.loads(
        repo_path("testdata/cases/bundled-skill-install.json").read_text()
    )["cases"]
    for case in cases:
        root = Path(tempfile.mkdtemp(prefix=f"kitup-{case['id']}-"))
        home = root / "home"
        workspace = root / "workspace"
        home.mkdir(parents=True)
        workspace.mkdir(parents=True)
        server = None
        env_backup = None
        try:
            server, env_backup = setup_given(case, home, workspace)
            run_case(case, home, workspace)
        finally:
            if server is not None:
                server.shutdown()
                server.server_close()
            if env_backup is not None:
                restore_github_env(env_backup)
            shutil.rmtree(root, ignore_errors=True)


def run_case(case, home: Path, workspace: Path) -> None:
    operation = case["operation"]
    if operation == "resolve-hosts":
        spec = load_host_spec(repo_path(case["given"]["hostsFile"]))
        hosts, errors = resolve_hosts(case["options"]["agents"], spec.hosts)
        expected = case["expected"]
        if "count" in expected:
            assert len(hosts) == expected["count"]
        if "hostIds" in expected:
            assert [host.id for host in hosts] == expected["hostIds"]
        if "resolvedHostIds" in expected:
            assert [host.id for host in hosts] == expected["resolvedHostIds"]
        if "errors" in expected:
            assert errors == expected["errors"]
        return

    if operation == "validate":
        result = validate_skill_bundle(skill_bundle_from_case(case))
        assert result.valid == case["expected"]["valid"]
        assert result.error_code == case["expected"].get("errorCode")
        return

    if operation == "parse-install-flags":
        parsed = parse_install_flags(case["options"])
        assert normalize_parsed_flags(parsed) == case["expected"]["parsed"]
        return

    if operation == "resolve-install-selection":
        selection = resolve_install_selection(
            selection_options_from_case(case, home, workspace)
        )
        assert_selection(
            normalize_value(selection),
            expand_value(case["expected"]["selection"], home, workspace),
        )
        return

    if operation == "run-install-workflow":
        input_stream = io.StringIO(case["options"].get("input", ""))
        output_stream = io.StringIO()
        workflow = run_bundled_skill_install_with_io(
            workflow_options_from_case(case, home, workspace),
            input_stream,
            output_stream,
        )
        assert_workflow(
            normalize_value(workflow),
            expand_value(case["expected"].get("workflow"), home, workspace),
        )
        if "exit" in case["expected"]:
            assert (
                normalize_value(classify_install_workflow_exit(workflow))
                == case["expected"]["exit"]
            )
        assert_output(output_stream.getvalue(), case["expected"].get("output"))
        assert_output_contains(
            output_stream.getvalue(), case["expected"].get("outputContains")
        )
        if "report" in case["expected"]:
            assert normalize_value(workflow.report) == camel_to_snake_dict(
                expand_value(case["expected"]["report"], home, workspace)
            )
        assert_expected_files(case, home, workspace)
        assert_expected_metadata(case, home, workspace)
        return

    if "detectedHosts" in case["expected"]:
        hosts = detect_hosts(
            BaseOptions(
                home=str(home),
                cwd=str(workspace),
                hosts_file=repo_path("spec/hosts.json"),
            ),
            case["options"]["scope"],
        )
        assert [host.id for host in hosts] == case["expected"]["detectedHosts"]

    report = run_report_case(case, home, workspace)
    if "report" in case["expected"]:
        assert normalize_value(report) == camel_to_snake_dict(
            expand_value(case["expected"]["report"], home, workspace)
        )
    assert_expected_write_counts(case, report, home, workspace)
    assert_expected_files(case, home, workspace)
    assert_expected_metadata(case, home, workspace)


def run_report_case(case, home: Path, workspace: Path):
    operation = case["operation"]
    if operation == "uninstall":
        return uninstall_bundled_skill(
            uninstall_options_from_case(case, home, workspace)
        )
    install_options = install_options_from_case(case, home, workspace)
    if operation == "update":
        return update_bundled_skill(install_options)
    if operation == "plan":
        return plan_bundled_skill(install_options)
    if operation == "install":
        return install_bundled_skill(install_options)
    raise AssertionError(f"unsupported operation: {operation}")


def setup_given(case, home: Path, workspace: Path):
    for value in case["given"].get("dirs", []):
        expand_path(value, home, workspace).mkdir(parents=True, exist_ok=True)
    for path, value in case["given"].get("files", {}).items():
        write_fixture_file(expand_path(path, home, workspace), value)
    if "copySkillBundleTo" in case["given"]:
        target = expand_path(case["given"]["copySkillBundleTo"], home, workspace)
        shutil.rmtree(target, ignore_errors=True)
        shutil.copytree(case_skill_bundle_dir(case), target)
    if "metadata" in case["given"]:
        write_metadata_fixture(case, home, workspace, case["given"]["metadata"])
    github = case["given"].get("github")
    if github is None:
        return None, None
    return start_github_fixture(github)


def assert_expected_files(case, home: Path, workspace: Path) -> None:
    for value in case["expected"].get("filesPresent", []):
        path = expand_path(value, home, workspace)
        assert path.exists(), f"expected file to exist: {path}"
    for value in case["expected"].get("filesAbsent", []):
        path = expand_path(value, home, workspace)
        assert not path.exists(), f"expected file to be absent: {path}"


def assert_expected_metadata(case, home: Path, workspace: Path) -> None:
    metadata = case["expected"].get("metadata")
    if metadata is None:
        return
    path = expand_path(metadata["path"], home, workspace)
    actual = json.loads(path.read_text())
    for key, value in metadata["fields"].items():
        assert actual[key] == value
    assert actual["hash"] == expected_bundle_hash(case, metadata["hash"])


def assert_selection(actual, expected) -> None:
    actual_value = dict(actual)
    expected_value = dict(expected)
    if "selectedCount" in expected_value:
        assert len(actual_value["selected_host_ids"]) == expected_value["selectedCount"]
        actual_value.pop("selected_host_ids")
        expected_value.pop("selectedCount")
    if "candidateCount" in expected_value:
        assert (
            len(actual_value["candidate_host_ids"]) == expected_value["candidateCount"]
        )
        actual_value.pop("candidate_host_ids")
        expected_value.pop("candidateCount")
    assert actual_value == camel_to_snake_dict(expected_value)


def assert_workflow(actual, expected) -> None:
    if expected is None:
        return
    for key, value in camel_to_snake_dict(expected).items():
        assert actual[key] == value


def assert_output_contains(actual: str, expected) -> None:
    if expected is None:
        return
    for value in expected:
        assert value in actual, f"expected output to contain {value!r}, got:\n{actual}"


def assert_output(actual: str, expected) -> None:
    if expected is None:
        return
    assert actual == expected


def assert_expected_write_counts(case, report, home: Path, workspace: Path) -> None:
    expected = case["expected"].get("writeCountByTargetDir")
    if expected is None:
        return
    actual: dict[str, int] = {}
    normalized = normalize_value(report)
    for key in ["installed", "updated"]:
        for item in normalized[key]:
            target_dir = item["target_dir"]
            actual[target_dir] = actual.get(target_dir, 0) + 1
    assert actual == expand_value(expected, home, workspace)


def write_metadata_fixture(case, home: Path, workspace: Path, metadata) -> None:
    fields = dict(metadata["fields"])
    fields["hash"] = expected_bundle_hash(case, metadata["hash"])
    write_fixture_file(expand_path(metadata["path"], home, workspace), fields)


def expected_bundle_hash(case, marker: str) -> str:
    if marker == "from-skill-bundle-dir":
        return compute_bundle_content_hash(
            directory_bundle(str(case_skill_bundle_dir(case)))
        )
    if marker == "from-skill-files":
        return compute_bundle_content_hash(
            files_bundle(skill_files(case["options"]["skillFiles"]))
        )
    if marker == "from-github-bundle":
        return compute_bundle_content_hash(files_bundle(github_skill_files(case)))
    return marker


def write_fixture_file(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(value, str):
        path.write_text(value, encoding="utf-8")
    else:
        path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def start_github_fixture(github):
    owner = github["owner"]
    repo = github["repo"]
    ref_name = github["ref"]
    commit = github["commit"]
    tree_sha = github["treeSha"]
    files = github["files"]

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            path = self.path.split("?", 1)[0]
            commit_path = f"/repos/{owner}/{repo}/commits/{ref_name}"
            tree_path = f"/repos/{owner}/{repo}/git/trees/{tree_sha}"
            if path == commit_path:
                self._write_json({"sha": commit, "commit": {"tree": {"sha": tree_sha}}})
                return
            if path == tree_path:
                self._write_json(
                    {
                        "tree": [
                            {
                                "path": file_path,
                                "type": "blob",
                                "mode": "100755"
                                if file_path.endswith(".sh")
                                else "100644",
                            }
                            for file_path in files
                        ]
                    }
                )
                return
            raw_prefix = f"/{owner}/{repo}/{commit}/"
            if path.startswith(raw_prefix):
                relative = path[len(raw_prefix) :]
                if relative in files:
                    self._write_bytes(files[relative].encode("utf-8"))
                    return
            self.send_response(404)
            self.send_header("content-type", "text/plain")
            self.end_headers()
            self.wfile.write(b"not found")

        def log_message(self, format, *args):
            return

        def _write_json(self, value):
            payload = json.dumps(value).encode("utf-8")
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def _write_bytes(self, value: bytes):
            self.send_response(200)
            self.send_header("content-type", "application/octet-stream")
            self.send_header("content-length", str(len(value)))
            self.end_headers()
            self.wfile.write(value)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    base_url = f"http://127.0.0.1:{server.server_port}"
    env_backup = {
        "KITUP_GITHUB_API_BASE_URL": os.environ.get("KITUP_GITHUB_API_BASE_URL"),
        "KITUP_GITHUB_RAW_BASE_URL": os.environ.get("KITUP_GITHUB_RAW_BASE_URL"),
    }
    os.environ["KITUP_GITHUB_API_BASE_URL"] = base_url
    os.environ["KITUP_GITHUB_RAW_BASE_URL"] = base_url
    return server, env_backup


def restore_github_env(env_backup) -> None:
    for key, value in env_backup.items():
        if value is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = value


def install_options_from_case(case, home: Path, workspace: Path) -> InstallOptions:
    return InstallOptions(
        base=BaseOptions(
            home=str(home),
            cwd=str(workspace),
            hosts_file=repo_path("spec/hosts.json"),
        ),
        app_id=case["options"]["appId"],
        skill_bundle=skill_bundle_from_case(case),
        scope=case["options"].get("scope", "user"),
        agents=case["options"].get("agents", "auto"),
        force=case["options"].get("force", False),
    )


def uninstall_options_from_case(case, home: Path, workspace: Path) -> UninstallOptions:
    return UninstallOptions(
        base=BaseOptions(
            home=str(home),
            cwd=str(workspace),
            hosts_file=repo_path("spec/hosts.json"),
        ),
        app_id=case["options"]["appId"],
        skill_name=case["options"]["skillName"],
        scope=case["options"]["scope"],
        agents=case["options"].get("agents", "auto"),
    )


def selection_options_from_case(
    case, home: Path, workspace: Path
) -> InstallSelectionOptions:
    return InstallSelectionOptions(
        base=BaseOptions(
            home=str(home),
            cwd=str(workspace),
            hosts_file=repo_path("spec/hosts.json"),
        ),
        scope=case["options"].get("scope", "user"),
        agents=case["options"].get("agents", "auto"),
        yes=case["options"].get("yes", False),
        stdin_tty=case["options"].get("stdinTTY", False),
        current_agent=case["options"].get("currentAgent"),
    )


def workflow_options_from_case(
    case, home: Path, workspace: Path
) -> InstallWorkflowOptions:
    return InstallWorkflowOptions(
        install=install_options_from_case(case, home, workspace),
        yes=case["options"].get("yes", False),
        dry_run=case["options"].get("dryRun", False),
        stdin_tty=case["options"].get("stdinTTY", False),
        current_agent=case["options"].get("currentAgent"),
        default_scope=case["options"].get("defaultScope", "user"),
        scope_set=case["options"].get("scopeSet", "scope" in case["options"]),
        prompt_scope=case["options"].get("promptScope", False),
    )


def skill_bundle_from_case(case) -> object:
    if "skillFiles" in case["options"]:
        return files_bundle(skill_files(case["options"]["skillFiles"]))
    if "skillBundleDir" in case["options"]:
        return directory_bundle(str(repo_path(case["options"]["skillBundleDir"])))
    if "githubBundle" in case["options"]:
        bundle = case["options"]["githubBundle"]
        return github_bundle(
            GitHubBundleOptions(
                owner=bundle["owner"],
                repo=bundle["repo"],
                path=bundle["path"],
                ref=bundle["ref"],
            )
        )
    raise AssertionError(f"missing skill bundle for case {case['id']}")


def skill_files(values) -> list[SkillFile]:
    return [
        SkillFile(path=value["path"], contents=value["contents"]) for value in values
    ]


def github_skill_files(case) -> list[SkillFile]:
    root = f"{case['options']['githubBundle']['path'].strip('/')}/"
    return [
        SkillFile(path=path[len(root) :], contents=contents)
        for path, contents in case["given"]["github"]["files"].items()
        if path.startswith(root)
    ]


def case_skill_bundle_dir(case) -> Path:
    if "skillBundleDir" in case["options"]:
        return repo_path(case["options"]["skillBundleDir"])
    return repo_path(f"testdata/skills/{case['options']['skillName']}")


def repo_path(path: str) -> Path:
    repo = Path(__file__).resolve().parents[2]
    target = Path(path)
    return target if target.is_absolute() else repo / target


def expand_value(value, home: Path, workspace: Path):
    if isinstance(value, str):
        if "$HOME" in value or "$WORKSPACE" in value:
            return str(expand_path(value, home, workspace))
        return value
    if isinstance(value, list):
        return [expand_value(item, home, workspace) for item in value]
    if isinstance(value, dict):
        return {
            str(expand_path(key, home, workspace)): expand_value(item, home, workspace)
            for key, item in value.items()
        }
    return value


def expand_path(value: str, home: Path, workspace: Path) -> Path:
    return Path(value.replace("$HOME", str(home)).replace("$WORKSPACE", str(workspace)))


def normalize_value(value):
    if hasattr(value, "__dataclass_fields__"):
        return normalize_value(asdict(value))
    if isinstance(value, Path):
        return str(value)
    if isinstance(value, list):
        return [normalize_value(item) for item in value]
    if isinstance(value, dict):
        return {
            key: normalize_value(item)
            for key, item in value.items()
            if item is not None
        }
    return value


def normalize_parsed_flags(parsed: ParsedInstallFlags) -> dict[str, object]:
    return {
        "scope": parsed.scope,
        "scopeSet": parsed.scope_set,
        "agentKind": "explicit" if isinstance(parsed.agents, list) else parsed.agents,
        "agentIds": parsed.agents if isinstance(parsed.agents, list) else [],
        "yes": parsed.yes,
        "dryRun": parsed.dry_run,
        "force": parsed.force,
        "errors": parsed.errors,
    }


def camel_to_snake_dict(value):
    if isinstance(value, list):
        return [camel_to_snake_dict(item) for item in value]
    if not isinstance(value, dict):
        return value
    return {
        camel_to_snake(key): camel_to_snake_dict(item) for key, item in value.items()
    }


def camel_to_snake(value: str) -> str:
    result: list[str] = []
    for char in value:
        if char.isupper():
            result.append("_")
            result.append(char.lower())
        else:
            result.append(char)
    return "".join(result).lstrip("_")
