from __future__ import annotations

from dataclasses import replace
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
    return [part for value in values for part in value.replace(",", " ").split()]


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
    return list(dict.fromkeys(agents))


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
    if len(detected_host_ids) == 1:
        return _install_selection(detected_host_ids, detected_host_ids, True)
    return InstallSelection(
        action="select-agents",
        selected_host_ids=[],
        candidate_host_ids=detected_host_ids or [host.id for host in spec.hosts],
        detected_host_ids=detected_host_ids,
        needs_confirmation=True,
        errors=[],
    )


def classify_install_workflow_exit(
    report: InstallWorkflowReport | dict[str, object],
) -> InstallWorkflowExit:
    if _workflow_value(report, "canceled"):
        code, message = "canceled", INSTALL_UX["canceled"]
    elif _workflow_value(_workflow_value(report, "selection"), "errors"):
        code, message = "selection-error", INSTALL_UX["selection_error"]
    elif _workflow_value(_workflow_value(report, "report"), "conflicts"):
        code, message = "conflict", INSTALL_UX["conflict"]
    elif _workflow_value(_workflow_value(report, "report"), "errors"):
        code, message = "error", INSTALL_UX["failed"]
    else:
        code, message = "ok", ""
    return InstallWorkflowExit(ok=code == "ok", code=code, message=message)


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
    writer = _OutputWriter(output)
    scope, selection = _resolve_workflow_scope(reader, writer, options)
    if selection is None:
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
    result = InstallWorkflowReport(
        selection=selection,
        scope=scope,
        plan=InstallReport(),
        report=InstallReport(),
        canceled=False,
        dry_run=options.dry_run,
    )
    if selection.action == "error":
        _render_selection_errors(writer, selection)
        return result
    if selection.action == "select-agents":
        hosts = load_host_spec(options.install.base.hosts_file).hosts
        selected = _prompt_agent_selection(reader, writer, selection, hosts)
        selection = _install_selection(
            selected, selection.detected_host_ids, options.stdin_tty and not options.yes
        )
        result.selection = selection
        if not selected:
            result.canceled = True
            return result

    install_options = replace(
        options.install, scope=scope, agents=selection.selected_host_ids
    )
    plan = plan_bundled_skill(install_options)
    result.plan = result.report = plan
    if not (plan.installed or plan.updated or plan.conflicts or plan.errors):
        return result
    if options.dry_run:
        _render_install_summary(writer, plan)
        return result
    if plan.conflicts or plan.errors:
        result.report = replace(plan, installed=[], updated=[])
        return result
    _render_install_summary(writer, plan)
    if selection.needs_confirmation and not _prompt_confirmation(reader, writer):
        result.report = InstallReport()
        result.canceled = True
        return result
    result.report = install_bundled_skill(install_options)
    return result


def _resolve_workflow_scope(
    reader: "_LineReader",
    output: object,
    options: InstallWorkflowOptions,
) -> tuple[Scope | str, InstallSelection | None]:
    default_scope = options.default_scope or "user"
    scope = options.install.scope or default_scope
    if options.scope_set or not options.prompt_scope:
        return scope, None
    if options.yes:
        return default_scope, None
    if not options.stdin_tty:
        return "", _error_selection([{"reason": "scope-selection-required"}], [])
    return _prompt_scope_selection(reader, output, default_scope), None


def _prompt_scope_selection(
    reader: "_LineReader",
    output: object,
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
    output: object,
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


def _prompt_confirmation(reader: "_LineReader", output: object) -> bool:
    output.write(INSTALL_UX["proceed"])
    line = (reader.read_line() or "").strip().lower()
    return line in {"y", "yes"}


def _render_install_summary(output: object, report: InstallReport) -> None:
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


def _render_selection_errors(output: object, selection: InstallSelection) -> None:
    for error in selection.errors:
        _write_line(output, f"{INSTALL_UX['error_prefix']} {error['reason']}")


def _write_line(output: object, line: str) -> None:
    output.write(f"{line}\n")


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


def _error_selection(
    errors: list[dict[str, str]], detected_host_ids: list[str]
) -> InstallSelection:
    return _install_selection([], detected_host_ids, False, errors)


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
    if isinstance(value, (list, tuple)):
        return [item for item in value if isinstance(item, str)]
    if isinstance(value, str):
        return [value]
    return []


class _LineReader:
    def __init__(self, source: object | None) -> None:
        self._stream = (
            source
            if source is not None
            and not isinstance(source, str | bytes)
            and hasattr(source, "readline")
            else None
        )
        self._lines = iter([] if self._stream is not None else self._iter_lines(source))

    def read_line(self) -> str | None:
        if self._stream is not None:
            line = self._stream.readline()
            if isinstance(line, bytes):
                line = line.decode("utf-8")
            line = str(line)
            if line == "":
                return None
            return line.rstrip("\n").rstrip("\r")
        return next(self._lines, None)

    def _iter_lines(self, source: object | None) -> Iterable[str]:
        if source is None:
            return []
        if isinstance(source, bytes):
            return source.decode("utf-8").splitlines()
        if isinstance(source, str):
            return source.splitlines()
        if hasattr(source, "read"):
            contents = source.read()
            if isinstance(contents, bytes):
                return contents.decode("utf-8").splitlines()
            return str(contents).splitlines()
        if isinstance(source, Iterable):
            chunks: list[str] = []
            for item in source:
                if isinstance(item, bytes):
                    chunks.append(item.decode("utf-8"))
                else:
                    chunks.append(str(item))
            return "".join(chunks).splitlines()
        return []


class _OutputWriter:
    def __init__(self, target: object | None) -> None:
        self._target = target

    def write(self, chunk: str) -> None:
        if self._target is not None:
            self._target.write(chunk)
