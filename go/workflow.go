package kitup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

type InstallUXText struct {
	SkillUse              string
	SkillShort            string
	InstallUse            string
	InstallShort          string
	ScopeFlag             string
	AgentFlag             string
	DryRunFlag            string
	YesFlag               string
	ForceFlag             string
	SelectScope           string
	ScopePrompt           string
	InvalidScopeSelection string
	SelectAgents          string
	AgentsPrompt          string
	InvalidAgentSelection string
	Proceed               string
	InstallSummary        string
	ErrorPrefix           string
	Canceled              string
	SelectionError        string
	Conflict              string
	Failed                string
	InvalidFlags          string
}

var InstallUX = InstallUXText{
	SkillUse:              "skill",
	SkillShort:            "Manage bundled Agent Skill",
	InstallUse:            "install",
	InstallShort:          "Install bundled Agent Skill",
	ScopeFlag:             "Install scope: user or project",
	AgentFlag:             "Target agent id. Repeat for multiple agents. Use '*' for all.",
	DryRunFlag:            "Show install plan without writing",
	YesFlag:               "Skip prompts and accept policy-selected targets",
	ForceFlag:             "Overwrite unsafe target conflicts",
	SelectScope:           "Select install scope:",
	ScopePrompt:           "Scope (user/project)",
	InvalidScopeSelection: "Invalid scope selection.",
	SelectAgents:          "Select agents:",
	AgentsPrompt:          "Agents (numbers, ids, comma-separated, empty cancels)",
	InvalidAgentSelection: "Invalid agent selection.",
	Proceed:               "Proceed? [y/N] ",
	InstallSummary:        "Install summary:",
	ErrorPrefix:           "kitup:",
	Canceled:              "Installation canceled.",
	SelectionError:        "Agent selection failed.",
	Conflict:              "Installation has conflicts.",
	Failed:                "Installation failed.",
	InvalidFlags:          "Invalid install flags.",
}

func AutoAgents() AgentSelector { return AgentSelector{Kind: "auto"} }

func AllAgents() AgentSelector { return AgentSelector{Kind: "*"} }

func ExplicitAgents(ids ...string) AgentSelector {
	return AgentSelector{Kind: "explicit", IDs: ids}
}

func ParseInstallFlags(flags InstallFlagValues) ParsedInstallFlags {
	errs := []map[string]any{}
	scope, scopeErrs := ParseScopeFlag(flags.Scope)
	errs = append(errs, scopeErrs...)
	agents, agentErrs := AgentSelectorFromFlags(flags.Agents)
	errs = append(errs, agentErrs...)
	return ParsedInstallFlags{Scope: scope, ScopeSet: flags.ScopeSet || flags.Scope != "", Agents: agents, Yes: flags.Yes, DryRun: flags.DryRun, Force: flags.Force, Errors: errs}
}

func AgentSelectorFromFlags(values []string) (AgentSelector, []map[string]any) {
	agents := splitFlagValues(values)
	if len(agents) == 0 {
		return AutoAgents(), []map[string]any{}
	}
	if slices.Contains(agents, "*") {
		errs := []map[string]any{}
		if len(agents) > 1 {
			errs = append(errs, map[string]any{"flag": "agent", "reason": "agent-star-must-be-alone", "value": strings.Join(agents, ",")})
		}
		return AllAgents(), errs
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, agent := range agents {
		if !seen[agent] {
			seen[agent] = true
			ids = append(ids, agent)
		}
	}
	return ExplicitAgents(ids...), []map[string]any{}
}

func ParseScopeFlag(value string) (Scope, []map[string]any) {
	if value == "" || value == string(UserScope) {
		return UserScope, []map[string]any{}
	}
	if value == string(ProjectScope) {
		return ProjectScope, []map[string]any{}
	}
	return UserScope, []map[string]any{{"flag": "scope", "reason": "invalid-scope", "value": value}}
}

func splitFlagValues(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
			out = append(out, part)
		}
	}
	return out
}

func ClassifyInstallWorkflowExit(report InstallWorkflowReport) InstallWorkflowExit {
	switch {
	case report.Canceled:
		return InstallWorkflowExit{OK: false, Code: "canceled", Message: InstallUX.Canceled}
	case len(report.Selection.Errors) > 0:
		return InstallWorkflowExit{OK: false, Code: "selection-error", Message: InstallUX.SelectionError}
	case len(report.Report.Conflicts) > 0:
		return InstallWorkflowExit{OK: false, Code: "conflict", Message: InstallUX.Conflict}
	case len(report.Report.Errors) > 0:
		return InstallWorkflowExit{OK: false, Code: "error", Message: InstallUX.Failed}
	default:
		return InstallWorkflowExit{OK: true, Code: "ok"}
	}
}

func InstallWorkflowError(report InstallWorkflowReport) error {
	exit := ClassifyInstallWorkflowExit(report)
	if exit.OK || exit.Code == "canceled" {
		return nil
	}
	return errors.New(exit.Message)
}

func InstallFlagError(errs []map[string]any) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.New(InstallUX.InvalidFlags)
}

func RunBundledSkillInstall(opts InstallWorkflowOptions) (InstallWorkflowReport, error) {
	in, out := opts.In, opts.Out
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if !opts.StdinTTY {
		if file, ok := in.(*os.File); ok {
			if info, err := file.Stat(); err == nil {
				opts.StdinTTY = info.Mode()&os.ModeCharDevice != 0
			}
		}
	}
	reader := bufio.NewReader(in)
	scope, selection, err := resolveWorkflowScope(reader, out, opts.Scope, opts.ScopeSet, opts.PromptScope, opts.DefaultScope, opts.Yes, opts.StdinTTY)
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	if len(selection.Errors) == 0 {
		selection, err = ResolveInstallSelection(InstallSelectionOptions{
			BaseOptions: opts.BaseOptions, Scope: scope, Agents: opts.Agents,
			Yes: opts.Yes, StdinTTY: opts.StdinTTY, CurrentAgent: opts.CurrentAgent,
		})
		if err != nil {
			return InstallWorkflowReport{}, err
		}
	}
	result := InstallWorkflowReport{Selection: selection, Scope: scope, Plan: emptyInstallReport(nil), Report: emptyInstallReport(nil), DryRun: opts.DryRun}
	if selection.Action == "error" {
		renderSelectionErrors(out, selection)
		return result, nil
	}
	if selection.Action == "select-agents" {
		hosts, err := LoadHostSpec(opts.HostsFile)
		if err != nil {
			return InstallWorkflowReport{}, err
		}
		selected, err := promptAgentSelection(reader, out, selection, hosts)
		if err != nil {
			return InstallWorkflowReport{}, err
		}
		result.Selection = installSelection(selected, selection.DetectedHostIDs, !opts.Yes && opts.StdinTTY, nil)
		if len(selected) == 0 {
			result.Canceled = true
			return result, nil
		}
	}
	installOpts := opts.InstallOptions
	installOpts.Agents = ExplicitAgents(result.Selection.SelectedHostIDs...)
	installOpts.Scope = scope
	plan, err := PlanBundledSkill(installOpts)
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	result.Plan, result.Report = plan, plan
	if len(plan.Installed)+len(plan.Updated)+len(plan.Conflicts)+len(plan.Errors) == 0 {
		return result, nil
	}
	if opts.DryRun {
		renderInstallSummary(out, plan)
		return result, nil
	}
	if len(plan.Conflicts)+len(plan.Errors) > 0 {
		result.Report.Installed, result.Report.Updated = []TargetResult{}, []TargetResult{}
		return result, nil
	}
	renderInstallSummary(out, plan)
	if result.Selection.NeedsConfirmation {
		confirmed, err := promptConfirmation(reader, out)
		if err != nil {
			return InstallWorkflowReport{}, err
		}
		if !confirmed {
			result.Canceled, result.Report = true, emptyInstallReport(nil)
			return result, nil
		}
	}
	result.Report, err = InstallBundledSkill(installOpts)
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	return result, nil
}

func resolveWorkflowScope(reader *bufio.Reader, out io.Writer, requested Scope, scopeSet, promptScope bool, defaultScope Scope, yes, stdinTTY bool) (Scope, InstallSelection, error) {
	if defaultScope == "" {
		defaultScope = UserScope
	}
	if requested == "" {
		requested = defaultScope
	}
	if scopeSet || !promptScope {
		return requested, InstallSelection{}, nil
	}
	if yes {
		return defaultScope, InstallSelection{}, nil
	}
	if !stdinTTY {
		return "", errorSelection([]map[string]any{{"reason": "scope-selection-required"}}, nil), nil
	}
	scope, err := promptScopeSelection(reader, out, defaultScope)
	return scope, InstallSelection{}, err
}

func promptScopeSelection(reader *bufio.Reader, out io.Writer, defaultScope Scope) (Scope, error) {
	for {
		fmt.Fprintln(out, InstallUX.SelectScope)
		fmt.Fprintf(out, "  1. %s\n", UserScope)
		fmt.Fprintf(out, "  2. %s\n", ProjectScope)
		fmt.Fprintf(out, "%s [%s]: ", InstallUX.ScopePrompt, defaultScope)
		line, err := readPromptLine(reader)
		if err != nil {
			return "", err
		}
		scope, ok := parseScopeSelection(line, defaultScope)
		if ok {
			return scope, nil
		}
		fmt.Fprintln(out, InstallUX.InvalidScopeSelection)
	}
}

func parseScopeSelection(line string, defaultScope Scope) (Scope, bool) {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return defaultScope, true
	case "1", "u", "user":
		return UserScope, true
	case "2", "p", "project":
		return ProjectScope, true
	default:
		return "", false
	}
}

func promptAgentSelection(reader *bufio.Reader, out io.Writer, selection InstallSelection, hosts []Host) ([]string, error) {
	candidates := hostsByID(hosts, selection.CandidateHostIDs)
	for {
		fmt.Fprintln(out, InstallUX.SelectAgents)
		for i, host := range candidates {
			fmt.Fprintf(out, "  %d. %s (%s)\n", i+1, host.DisplayName, host.ID)
		}
		suffix := ""
		if len(selection.SelectedHostIDs) > 0 {
			suffix = " [" + strings.Join(selection.SelectedHostIDs, ",") + "]"
		}
		fmt.Fprintf(out, "%s%s: ", InstallUX.AgentsPrompt, suffix)
		line, err := readPromptLine(reader)
		if err != nil {
			return nil, err
		}
		selected, ok := parseAgentSelection(line, selection, candidates)
		if ok {
			return selected, nil
		}
		fmt.Fprintln(out, InstallUX.InvalidAgentSelection)
	}
}

func parseAgentSelection(line string, selection InstallSelection, candidates []Host) ([]string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return selection.SelectedHostIDs, true
	}
	if line == "*" {
		return hostIDList(candidates), true
	}
	byName := map[string]string{}
	for index, host := range candidates {
		byName[strconv.Itoa(index+1)] = host.ID
		byName[host.ID] = host.ID
		for _, alias := range host.Aliases {
			byName[alias] = host.ID
		}
	}
	seen := map[string]bool{}
	selected := []string{}
	for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		id, ok := byName[part]
		if !ok {
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			selected = append(selected, id)
		}
	}
	return selected, true
}

func promptConfirmation(reader *bufio.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, InstallUX.Proceed)
	line, err := readPromptLine(reader)
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func readPromptLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func renderInstallSummary(out io.Writer, report InstallReport) {
	for _, item := range append(append([]TargetResult{}, report.Installed...), report.Updated...) {
		for _, host := range summaryHosts(item) {
			fmt.Fprintf(out, "  - %s -> %s (%s)\n", item.SkillName, item.TargetDir, host)
		}
	}
}

func summaryHosts(item TargetResult) []string {
	if item.HostID != "" {
		return []string{item.HostID}
	}
	return item.HostIDs
}

func renderSelectionErrors(out io.Writer, selection InstallSelection) {
	for _, err := range selection.Errors {
		fmt.Fprintf(out, "%s %s\n", InstallUX.ErrorPrefix, err["reason"])
	}
}
