from __future__ import annotations

from dataclasses import replace
import io
import sys
from typing import Iterable

from .hosts import detect_hosts, load_host_spec, resolve_hosts
from .install import install_bundled_skill, plan_bundled_skill
from .types import (
    INSTALL_UX,
    InstallReport,
    InstallSelection,
    InstallSelectionOptions,
    InstallWorkflowExit,
    InstallWorkflowOptions,
    InstallWorkflowReport,
    KitupError,
    ParsedInstallFlags,
    Scope,
)


def split_flag_values(values: list[str]) -> list[str]:
    return [
        part.strip()
        for value in values
        for part in value.replace(",", " ").split()
        if part.strip()
    ]


def dedupe(values: list[str]) -> list[str]:
    seen: set[str] = set()
    result: list[str] = []
    for value in values:
        if value not in seen:
            seen.add(value)
            result.append(value)
    return result


def parse_scope_flag(
    value: str | None, errors: list[dict[str, str]] | None = None
) -> Scope:
    issues = errors if errors is not None else []
    if value in (None, "", "user"):
        return "user"
    if value == "project":
        return "project"
    issues.append({"flag": "scope", "reason": "invalid-scope", "value": value})
    return "user"


def agent_selector_from_flags(
    values: list[str], errors: list[dict[str, str]] | None = None
) -> str | list[str]:
    issues = errors if errors is not None else []
    agents = split_flag_values(values)
    if not agents:
        return "auto"
    if "*" in agents:
        if len(agents) > 1:
            issues.append(
                {
                    "flag": "agent",
                    "reason": "agent-star-must-be-alone",
                    "value": ",".join(agents),
                }
            )
        return "*"
    return dedupe(agents)


def parse_install_flags(flags: dict[str, object]) -> ParsedInstallFlags:
    errors: list[dict[str, str]] = []
    agents = flags.get("agents")
    return ParsedInstallFlags(
        scope=parse_scope_flag(_coerce_optional_text(flags.get("scope")), errors),
        scope_set=bool(flags.get("scopeSet", "scope" in flags)),
        agents=agent_selector_from_flags(_coerce_flag_values(agents), errors),
        yes=bool(flags.get("yes")),
        dry_run=bool(flags.get("dryRun")),
        force=bool(flags.get("force")),
        errors=errors,
    )


def resolve_install_selection(options: InstallSelectionOptions) -> InstallSelection:
    spec = load_host_spec(options.base.hosts_file)
    stdin_tty = options.stdin_tty
    explicit_agents = options.agents not in (None, "auto")

    if options.current_agent and not explicit_agents:
        selected, errors = resolve_hosts([options.current_agent], spec.hosts)
        selected = _add_universal_host(selected, spec.hosts)
        return _install_selection(
            [host.id for host in selected],
            [],
            stdin_tty and not options.yes,
            errors,
        )

    if explicit_agents:
        if options.agents == "*":
            return _install_selection(
                [host.id for host in spec.hosts],
                [],
                stdin_tty and not options.yes,
            )
        selected, errors = resolve_hosts(options.agents, spec.hosts)
        if errors:
            return _error_selection(errors, [])
        return _install_selection(
            [host.id for host in selected],
            [],
            stdin_tty and not options.yes,
        )

    detected = detect_hosts(options.base, scope=options.scope)
    detected_host_ids = [host.id for host in detected]

    if not stdin_tty and not options.yes:
        return _error_selection(
            [{"reason": "agent-selection-required"}], detected_host_ids
        )
    if options.yes:
        if not detected_host_ids:
            return _error_selection([{"reason": "no-detected-hosts"}], [])
        return _install_selection(detected_host_ids, detected_host_ids, False)
    if not detected_host_ids:
        return _select_agents_selection(
            [host.id for host in spec.hosts], detected_host_ids, []
        )
    if len(detected_host_ids) == 1:
        return _install_selection(detected_host_ids, detected_host_ids, True)
    return _select_agents_selection(detected_host_ids, detected_host_ids, [])


def classify_install_workflow_exit(
    report: InstallWorkflowReport | dict[str, object],
) -> InstallWorkflowExit:
    if _workflow_value(report, "canceled"):
        return InstallWorkflowExit(
            ok=False, code="canceled", message=INSTALL_UX["canceled"]
        )
    selection = _workflow_value(report, "selection")
    if _workflow_value(selection, "errors"):
        return InstallWorkflowExit(
            ok=False,
            code="selection-error",
            message=INSTALL_UX["selection_error"],
        )
    run_report = _workflow_value(report, "report")
    if _workflow_value(run_report, "conflicts"):
        return InstallWorkflowExit(
            ok=False,
            code="conflict",
            message=INSTALL_UX["conflict"],
        )
    if _workflow_value(run_report, "errors"):
        return InstallWorkflowExit(
            ok=False,
            code="error",
            message=INSTALL_UX["failed"],
        )
    return InstallWorkflowExit(ok=True, code="ok", message="")


def install_flag_error(errors: list[dict[str, str]]) -> Exception | None:
    return None if not errors else KitupError(INSTALL_UX["invalid_flags"])


def install_workflow_error(
    report: InstallWorkflowReport | dict[str, object],
) -> Exception | None:
    exit_info = classify_install_workflow_exit(report)
    return (
        None
        if exit_info.ok or exit_info.code == "canceled"
        else KitupError(exit_info.message)
    )


def run_bundled_skill_install(options: InstallWorkflowOptions) -> InstallWorkflowReport:
    input_source = options.input if options.input is not None else sys.stdin
    output_target = options.output if options.output is not None else sys.stdout
    stdin_tty = (
        options.stdin_tty
        if options.stdin_tty is not None
        else bool(getattr(input_source, "isatty", lambda: False)())
    )
    runtime_options = replace(options, stdin_tty=stdin_tty)
    return run_bundled_skill_install_with_io(
        runtime_options, input_source, output_target
    )


def run_bundled_skill_install_with_io(
    options: InstallWorkflowOptions,
    input: object | None,
    output: object | None,
) -> InstallWorkflowReport:
    reader = _LineReader(input)
    writer = _coerce_output(output)
    scope, scope_error = _resolve_workflow_scope(
        reader=reader,
        output=writer,
        requested=options.install.scope,
        scope_set=options.scope_set,
        prompt_scope=options.prompt_scope,
        configured_default=options.default_scope,
        yes=options.yes,
        stdin_tty=options.stdin_tty,
    )
    if scope_error is not None:
        _render_selection_errors(writer, scope_error)
        return InstallWorkflowReport(
            selection=scope_error,
            scope=scope,
            plan=empty_install_report(),
            report=empty_install_report(),
            canceled=False,
            dry_run=options.dry_run,
        )

    selection = resolve_install_selection(
        InstallSelectionOptions(
            base=options.install.base,
            scope=scope,
            agents=options.install.agents,
            yes=options.yes,
            stdin_tty=options.stdin_tty,
            current_agent=options.current_agent,
        )
    )
    if selection.action == "error":
        _render_selection_errors(writer, selection)
        return InstallWorkflowReport(
            selection=selection,
            scope=scope,
            plan=empty_install_report(),
            report=empty_install_report(),
            canceled=False,
            dry_run=options.dry_run,
        )
    if selection.action == "select-agents":
        hosts = load_host_spec(options.install.base.hosts_file).hosts
        selected_host_ids = _prompt_agent_selection(reader, writer, selection, hosts)
        selection = _install_selection(
            selected_host_ids,
            selection.detected_host_ids,
            options.stdin_tty and not options.yes,
        )
        if not selected_host_ids:
            return InstallWorkflowReport(
                selection=selection,
                scope=scope,
                plan=empty_install_report(),
                report=empty_install_report(),
                canceled=True,
                dry_run=options.dry_run,
            )

    install_options = replace(
        options.install,
        scope=scope,
        agents=selection.selected_host_ids,
    )
    plan = plan_bundled_skill(install_options)
    if not _has_visible_install_plan(plan):
        return InstallWorkflowReport(
            selection=selection,
            scope=scope,
            plan=plan,
            report=plan,
            canceled=False,
            dry_run=options.dry_run,
        )

    if options.dry_run:
        _render_install_summary(writer, plan)
        return InstallWorkflowReport(
            selection=selection,
            scope=scope,
            plan=plan,
            report=plan,
            canceled=False,
            dry_run=True,
        )
    if len(plan.conflicts) + len(plan.errors) > 0:
        return InstallWorkflowReport(
            selection=selection,
            scope=scope,
            plan=plan,
            report=replace(plan, installed=[], updated=[]),
            canceled=False,
            dry_run=False,
        )
    _render_install_summary(writer, plan)
    if not _has_install_writes(plan):
        return InstallWorkflowReport(
            selection=selection,
            scope=scope,
            plan=plan,
            report=plan,
            canceled=False,
            dry_run=False,
        )
    if selection.needs_confirmation and not _prompt_confirmation(reader, writer):
        return InstallWorkflowReport(
            selection=selection,
            scope=scope,
            plan=plan,
            report=empty_install_report(),
            canceled=True,
            dry_run=False,
        )

    report = install_bundled_skill(install_options)
    return InstallWorkflowReport(
        selection=selection,
        scope=scope,
        plan=plan,
        report=report,
        canceled=False,
        dry_run=False,
    )


def empty_install_report(errors: list[object] | None = None) -> InstallReport:
    return InstallReport(errors=list(errors or []))


def _resolve_workflow_scope(
    *,
    reader: "_LineReader",
    output: "_OutputWriter",
    requested: Scope,
    scope_set: bool,
    prompt_scope: bool,
    configured_default: Scope,
    yes: bool,
    stdin_tty: bool,
) -> tuple[Scope | str, InstallSelection | None]:
    default_scope = configured_default or "user"
    scope = requested or default_scope
    if scope_set or not prompt_scope:
        return scope, None
    if yes:
        return default_scope, None
    if not stdin_tty:
        return "", _error_selection([{"reason": "scope-selection-required"}], [])
    return _prompt_scope_selection(reader, output, default_scope), None


def _prompt_scope_selection(
    reader: "_LineReader",
    output: "_OutputWriter",
    default_scope: Scope,
) -> Scope:
    while True:
        _write_line(output, INSTALL_UX["select_scope"])
        _write_line(output, "  1. user")
        _write_line(output, "  2. project")
        output.write(f"{INSTALL_UX['scope_prompt']} [{default_scope}]: ")
        selected = _parse_scope_selection(reader.read_line() or "", default_scope)
        if selected is not None:
            return selected
        _write_line(output, INSTALL_UX["invalid_scope_selection"])


def _parse_scope_selection(line: str, default_scope: Scope) -> Scope | None:
    value = line.strip().lower()
    if value == "":
        return default_scope
    if value in {"1", "u", "user"}:
        return "user"
    if value in {"2", "p", "project"}:
        return "project"
    return None


def _prompt_agent_selection(
    reader: "_LineReader",
    output: "_OutputWriter",
    selection: InstallSelection,
    hosts: list[object],
) -> list[str]:
    candidates = [
        host
        for host_id in selection.candidate_host_ids
        for host in hosts
        if getattr(host, "id", None) == host_id
    ]
    while True:
        _write_line(output, INSTALL_UX["select_agents"])
        for index, host in enumerate(candidates, start=1):
            _write_line(output, f"  {index}. {host.display_name} ({host.id})")
        current = ",".join(selection.selected_host_ids)
        suffix = f" [{current}]" if current else ""
        output.write(f"{INSTALL_UX['agents_prompt']}{suffix}: ")
        selected = _parse_agent_selection(
            reader.read_line() or "", selection, candidates
        )
        if selected is not None:
            return selected
        _write_line(output, INSTALL_UX["invalid_agent_selection"])


def _parse_agent_selection(
    line: str, selection: InstallSelection, candidates: list[object]
) -> list[str] | None:
    trimmed = line.strip()
    if trimmed == "":
        return list(selection.selected_host_ids)
    if trimmed == "*":
        return [host.id for host in candidates]

    by_name: dict[str, str] = {}
    for index, host in enumerate(candidates, start=1):
        by_name[str(index)] = host.id
        by_name[host.id] = host.id
        for alias in host.aliases:
            by_name[alias] = host.id

    selected: list[str] = []
    seen: set[str] = set()
    for part in split_flag_values([trimmed]):
        host_id = by_name.get(part)
        if host_id is None:
            return None
        if host_id not in seen:
            seen.add(host_id)
            selected.append(host_id)
    return selected


def _prompt_confirmation(reader: "_LineReader", output: "_OutputWriter") -> bool:
    output.write(INSTALL_UX["proceed"])
    line = (reader.read_line() or "").strip().lower()
    return line in {"y", "yes"}


def _render_install_summary(output: "_OutputWriter", report: InstallReport) -> None:
    for item in [*report.installed, *report.updated]:
        for host_id in _summary_hosts(item):
            _write_line(
                output, f"  - {item.skill_name} -> {item.target_dir} ({host_id})"
            )


def _summary_hosts(item: object) -> list[str]:
    host_id = getattr(item, "host_id", None)
    if host_id is not None:
        return [host_id]
    host_ids = getattr(item, "host_ids", None)
    return list(host_ids or [])


def _render_selection_errors(
    output: "_OutputWriter", selection: InstallSelection
) -> None:
    for error in selection.errors:
        _write_line(output, f"{INSTALL_UX['error_prefix']} {error['reason']}")


def _write_line(output: "_OutputWriter", line: str) -> None:
    output.write(f"{line}\n")


def _has_visible_install_plan(report: InstallReport) -> bool:
    return (
        len(report.installed)
        + len(report.updated)
        + len(report.conflicts)
        + len(report.errors)
        > 0
    )


def _has_install_writes(report: InstallReport) -> bool:
    return len(report.installed) + len(report.updated) > 0


def _install_selection(
    selected_host_ids: list[str],
    detected_host_ids: list[str],
    needs_confirmation: bool,
    errors: list[dict[str, str]] | None = None,
) -> InstallSelection:
    issues = list(errors or [])
    return InstallSelection(
        action="error" if issues else "install",
        selected_host_ids=selected_host_ids,
        candidate_host_ids=[],
        detected_host_ids=detected_host_ids,
        needs_confirmation=False if issues else needs_confirmation,
        errors=issues,
    )


def _select_agents_selection(
    candidate_host_ids: list[str],
    detected_host_ids: list[str],
    selected_host_ids: list[str],
) -> InstallSelection:
    return InstallSelection(
        action="select-agents",
        selected_host_ids=selected_host_ids,
        candidate_host_ids=candidate_host_ids,
        detected_host_ids=detected_host_ids,
        needs_confirmation=True,
        errors=[],
    )


def _error_selection(
    errors: list[dict[str, str]], detected_host_ids: list[str]
) -> InstallSelection:
    return InstallSelection(
        action="error",
        selected_host_ids=[],
        candidate_host_ids=[],
        detected_host_ids=detected_host_ids,
        needs_confirmation=False,
        errors=list(errors),
    )


def _add_universal_host(selected: list[object], hosts: list[object]) -> list[object]:
    result = list(selected)
    if any(host.id == "universal" for host in result):
        return result
    universal = next((host for host in hosts if host.id == "universal"), None)
    if universal is not None:
        result.append(universal)
    return result


def _workflow_value(value: object, key: str) -> object:
    if isinstance(value, dict):
        return value.get(key)
    return getattr(value, key)


def _coerce_optional_text(value: object) -> str | None:
    return value if isinstance(value, str) else None


def _coerce_flag_values(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [item for item in value if isinstance(item, str)]
    if isinstance(value, tuple):
        return [item for item in value if isinstance(item, str)]
    if isinstance(value, str):
        return [value]
    return []


class _LineReader:
    def __init__(self, source: object | None) -> None:
        self._lines: list[str] = list(self._iter_lines(source))
        self._index = 0

    def read_line(self) -> str | None:
        if self._index >= len(self._lines):
            return None
        line = self._lines[self._index]
        self._index += 1
        return line

    def _iter_lines(self, source: object | None) -> Iterable[str]:
        if source is None:
            return []
        if isinstance(source, bytes):
            return self._split_text(source.decode("utf-8"))
        if isinstance(source, str):
            return self._split_text(source)
        if hasattr(source, "read"):
            contents = source.read()
            if isinstance(contents, bytes):
                return self._split_text(contents.decode("utf-8"))
            return self._split_text(str(contents))
        if isinstance(source, io.StringIO):
            return self._split_text(source.getvalue())
        if isinstance(source, Iterable):
            chunks: list[str] = []
            for item in source:
                if isinstance(item, bytes):
                    chunks.append(item.decode("utf-8"))
                else:
                    chunks.append(str(item))
            return self._split_text("".join(chunks))
        return []

    @staticmethod
    def _split_text(text: str) -> list[str]:
        if text == "":
            return []
        lines = text.splitlines()
        if text.endswith(("\n", "\r")):
            return [line.rstrip("\r") for line in lines]
        if not lines:
            return [text.rstrip("\r")]
        return [line.rstrip("\r") for line in lines]


class _OutputWriter:
    def __init__(self, target: object | None) -> None:
        self._target = target

    def write(self, chunk: str) -> None:
        if self._target is None:
            return
        self._target.write(chunk)


def _coerce_output(output: object | None) -> _OutputWriter:
    return _OutputWriter(output)
