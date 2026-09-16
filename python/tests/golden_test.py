from dataclasses import fields, is_dataclass
import io
import json
import os
from pathlib import Path
import subprocess
import sys

import kitup as sdk


def test_golden_cases():
    if "KITUP_GOLDEN_INPUT" not in os.environ:
        script = Path(__file__).resolve().parents[2] / "scripts" / "golden.mjs"
        subprocess.run(
            ["node", str(script), "--", sys.executable, "-m", "pytest", __file__, "-q"],
            check=True,
        )
        return
    results = []
    for request in json.loads(Path(os.environ["KITUP_GOLDEN_INPUT"]).read_text()):
        for key in ("KITUP_GITHUB_API_BASE_URL", "KITUP_GITHUB_RAW_BASE_URL"):
            if key in request["env"]:
                os.environ[key] = request["env"][key]
            else:
                os.environ.pop(key, None)
        try:
            result = run_case(request)
        except Exception as error:
            result = {"threw": True, "error": str(error)}
        results.append({"id": request["id"], **normalize(result)})
    Path(os.environ["KITUP_GOLDEN_OUTPUT"]).write_text(json.dumps(results))


def run_case(request):
    operation, values = request["operation"], request["options"]
    base = sdk.BaseOptions(values["home"], values["cwd"], values["hostsFile"])
    if operation == "resolve-hosts":
        hosts, errors = sdk.resolve_hosts(
            values["agents"], sdk.load_host_spec(request["given"]["hostsFile"]).hosts
        )
        ids = [host.id for host in hosts]
        return dict(count=len(ids), hostIds=ids, resolvedHostIds=ids, errors=errors)
    if operation == "validate":
        result = sdk.validate_skill_bundle(bundle(values))
        return dict(valid=result.valid, errorCode=result.error_code)
    if operation == "parse-install-flags":
        parsed = normalize(sdk.parse_install_flags(values))
        agents = parsed.pop("agents")
        parsed["agentKind"] = "explicit" if isinstance(agents, list) else agents
        parsed["agentIds"] = agents if isinstance(agents, list) else []
        return {"parsed": parsed}
    if operation == "resolve-install-selection":
        return {
            "selection": sdk.resolve_install_selection(
                options(sdk.InstallSelectionOptions, values, base=base)
            )
        }
    if operation == "resolve-install-targets":
        return {
            "targets": sdk.resolve_install_targets(
                base, values["agents"], values["scope"], values["skillName"]
            )
        }
    if operation == "run-install-workflow":
        output = io.StringIO()
        workflow = sdk.run_bundled_skill_install_with_io(
            options(
                sdk.InstallWorkflowOptions,
                values,
                install=options(
                    sdk.InstallOptions, values, base=base, skill_bundle=bundle(values)
                ),
            ),
            io.StringIO(values.get("input", "")),
            output,
        )
        return dict(
            workflow=workflow,
            report=workflow.report,
            exit=sdk.classify_install_workflow_exit(workflow),
            output=output.getvalue(),
        )
    result = {}
    if request["detect"]:
        result["detectedHosts"] = [
            host.id for host in sdk.detect_hosts(base, values["scope"])
        ]
    action, cls = {
        "status": (sdk.status_bundled_skill, sdk.StatusOptions),
        "uninstall": (sdk.uninstall_bundled_skill, sdk.UninstallOptions),
        "plan": (sdk.plan_bundled_skill, sdk.InstallOptions),
        "install": (sdk.install_bundled_skill, sdk.InstallOptions),
        "update": (sdk.update_bundled_skill, sdk.InstallOptions),
    }[operation]
    extra = {"skill_bundle": bundle(values)} if cls is sdk.InstallOptions else {}
    result["report"] = action(options(cls, values, base=base, **extra))
    return result


def options(cls, values, **extra):
    values = {
        "scope": "user",
        "stdinTTY": False,
        "scopeSet": "scope" in values,
        **values,
    }
    return cls(
        **{
            **{
                field.name: values[camel(field.name)]
                for field in fields(cls)
                if camel(field.name) in values
            },
            **extra,
        }
    )


def bundle(values):
    if "skillFiles" in values:
        result = sdk.files_bundle(
            [sdk.SkillFile(**file) for file in values["skillFiles"]]
        )
    elif "skillBundleDir" in values:
        result = sdk.directory_bundle(values["skillBundleDir"])
    else:
        result = sdk.github_bundle(sdk.GitHubBundleOptions(**values["githubBundle"]))
    if "bundleMetadata" in values:
        result = sdk.with_bundle_metadata(
            result, options(sdk.BundledSkillMetadata, values["bundleMetadata"])
        )
    return result


def camel(name):
    head, *tail = name.split("_")
    return head + "".join("TTY" if part == "tty" else part.title() for part in tail)


def normalize(value):
    if is_dataclass(value):
        return {
            camel(field.name): normalize(item)
            for field in fields(value)
            if (item := getattr(value, field.name)) is not None
        }
    if isinstance(value, list):
        return [normalize(item) for item in value]
    if isinstance(value, dict):
        return {key: normalize(item) for key, item in value.items()}
    return value
