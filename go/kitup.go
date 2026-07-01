package kitup

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Scope string

const (
	UserScope    Scope = "user"
	ProjectScope Scope = "project"
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

type AgentSelector struct {
	Kind string
	IDs  []string
}

type InstallFlagValues struct {
	Scope    string
	ScopeSet bool
	Agents   []string
	Yes      bool
	DryRun   bool
	Force    bool
}

type ParsedInstallFlags struct {
	Scope    Scope
	ScopeSet bool
	Agents   AgentSelector
	Yes      bool
	DryRun   bool
	Force    bool
	Errors   []map[string]any
}

type InstallWorkflowExit struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func AutoAgents() AgentSelector { return AgentSelector{Kind: "auto"} }
func AllAgents() AgentSelector  { return AgentSelector{Kind: "*"} }
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
	for _, agent := range agents {
		if agent == "*" {
			errs := []map[string]any{}
			if len(agents) > 1 {
				errs = append(errs, map[string]any{"flag": "agent", "reason": "agent-star-must-be-alone", "value": strings.Join(agents, ",")})
			}
			return AllAgents(), errs
		}
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
			if part != "" {
				out = append(out, part)
			}
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

type Host struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"displayName"`
	Aliases          []string `json:"aliases,omitempty"`
	ProjectSkillsDir []string `json:"projectSkillsDirs"`
	UserSkillsDir    []string `json:"userSkillsDirs"`
	Detect           []string `json:"detect"`
	Status           string   `json:"status"`
	Notes            []string `json:"notes,omitempty"`
}

type hostSpec struct {
	SchemaVersion int    `json:"schemaVersion"`
	Hosts         []Host `json:"hosts"`
}

type BaseOptions struct {
	Home      string
	CWD       string
	HostsFile string
}

type InstallOptions struct {
	BaseOptions
	AppID       string
	SkillBundle SkillBundle
	Scope       Scope
	Agents      AgentSelector
	Force       bool
}

type UninstallOptions struct {
	BaseOptions
	AppID     string
	SkillName string
	Scope     Scope
	Agents    AgentSelector
}

type InstallSelectionOptions struct {
	BaseOptions
	Scope        Scope
	Agents       AgentSelector
	Yes          bool
	StdinTTY     bool
	CurrentAgent string
}

type InstallWorkflowOptions struct {
	InstallOptions
	Yes          bool
	DryRun       bool
	StdinTTY     bool
	CurrentAgent string
	DefaultScope Scope
	ScopeSet     bool
	PromptScope  bool
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
}

type SkillInfo struct {
	Valid       bool   `json:"valid"`
	SkillName   string `json:"skillName,omitempty"`
	Description string `json:"description,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
}

type SkillFile struct {
	Path     string
	Contents []byte
	Mode     fs.FileMode
}

type SkillBundle struct {
	kind   string
	dir    string
	fsys   fs.FS
	root   string
	files  []SkillFile
	github GitHubBundleOptions
}

type GitHubBundleOptions struct {
	Owner string
	Repo  string
	Path  string
	Ref   string
}

func DirectoryBundle(dir string) SkillBundle {
	return SkillBundle{kind: "directory", dir: dir}
}

func FSBundle(fsys fs.FS, root string) SkillBundle {
	return SkillBundle{kind: "fs", fsys: fsys, root: root}
}

func FilesBundle(files []SkillFile) SkillBundle {
	return SkillBundle{kind: "files", files: files}
}

func GitHubBundle(opts GitHubBundleOptions) SkillBundle {
	return SkillBundle{kind: "github", github: opts}
}

type TargetGroup struct {
	HostIDs   []string
	SkillName string
	TargetDir string
}

type TargetResult struct {
	HostID    string   `json:"hostId,omitempty"`
	HostIDs   []string `json:"hostIds,omitempty"`
	SkillName string   `json:"skillName"`
	TargetDir string   `json:"targetDir"`
}

type TargetStatus struct {
	TargetResult
	Reason string `json:"reason"`
}

type ReportError struct {
	Agent     string `json:"agent,omitempty"`
	Flag      string `json:"flag,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	Reason    string `json:"reason"`
	Scope     Scope  `json:"scope,omitempty"`
	SkillName string `json:"skillName,omitempty"`
	Value     string `json:"value,omitempty"`
}

type InstallReport struct {
	Installed []TargetResult `json:"installed"`
	Updated   []TargetResult `json:"updated"`
	Skipped   []TargetStatus `json:"skipped"`
	Conflicts []TargetStatus `json:"conflicts"`
	Errors    []ReportError  `json:"errors"`
}

type UninstallReport struct {
	Removed   []TargetResult `json:"removed"`
	Skipped   []TargetStatus `json:"skipped"`
	Conflicts []TargetStatus `json:"conflicts"`
	Errors    []ReportError  `json:"errors"`
}

type InstallSelection struct {
	Action            string           `json:"action"`
	SelectedHostIDs   []string         `json:"selectedHostIds"`
	CandidateHostIDs  []string         `json:"candidateHostIds"`
	DetectedHostIDs   []string         `json:"detectedHostIds"`
	NeedsConfirmation bool             `json:"needsConfirmation"`
	Errors            []map[string]any `json:"errors"`
}

type InstallWorkflowReport struct {
	Selection InstallSelection `json:"selection"`
	Scope     Scope            `json:"scope"`
	Plan      InstallReport    `json:"plan"`
	Report    InstallReport    `json:"report"`
	Canceled  bool             `json:"canceled"`
	DryRun    bool             `json:"dryRun"`
}

type metadata struct {
	SchemaVersion int               `json:"schemaVersion"`
	AppID         string            `json:"appId"`
	SkillName     string            `json:"skillName"`
	Source        string            `json:"source"`
	Hash          string            `json:"hash"`
	SourceID      string            `json:"sourceId,omitempty"`
	Version       string            `json:"version,omitempty"`
	Provenance    map[string]string `json:"provenance,omitempty"`
}

type bundleMetadata struct {
	Source     string
	SourceID   string
	Version    string
	Provenance map[string]string
}

type bundleFile struct {
	Path     string
	Contents []byte
	Mode     fs.FileMode
}

type normalizedSkillBundle struct {
	Files  []bundleFile
	ByPath map[string]bundleFile
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func LoadHostSpec(hostsFile string) ([]Host, error) {
	data := []byte(defaultHostsSpecJSON)
	if hostsFile != "" {
		var err error
		data, err = os.ReadFile(hostsFile)
		if err != nil {
			return nil, err
		}
	}
	var spec hostSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	return spec.Hosts, nil
}

func ResolveHosts(agents AgentSelector, hosts []Host) ([]Host, []map[string]any) {
	if agents.Kind == "*" {
		return hosts, []map[string]any{}
	}
	if agents.Kind == "" || agents.Kind == "auto" {
		return []Host{}, []map[string]any{}
	}
	byName := map[string]Host{}
	for _, host := range hosts {
		byName[host.ID] = host
		for _, alias := range host.Aliases {
			byName[alias] = host
		}
	}
	seen := map[string]bool{}
	resolved := []Host{}
	errs := []map[string]any{}
	for _, id := range agents.IDs {
		host, ok := byName[id]
		if !ok {
			errs = append(errs, map[string]any{"agent": id, "reason": "unknown-host"})
			continue
		}
		if !seen[host.ID] {
			seen[host.ID] = true
			resolved = append(resolved, host)
		}
	}
	return resolved, errs
}

func DetectHosts(opts BaseOptions, scope Scope) ([]Host, error) {
	hosts, err := LoadHostSpec(opts.HostsFile)
	if err != nil {
		return nil, err
	}
	home, cwd := defaults(opts)
	detected := []Host{}
	for _, host := range hosts {
		if len(host.Detect) == 0 || isGenericDetectPath(host.Detect[0]) {
			continue
		}
		if exists(expandHostPath(host.Detect[0], home, cwd)) {
			detected = append(detected, host)
		}
	}
	if scope == "" {
		return detected, nil
	}
	sort.Slice(detected, func(i, j int) bool {
		a := canonicalScopePath(detected[i], scope, home, cwd)
		b := canonicalScopePath(detected[j], scope, home, cwd)
		if a == b {
			return detected[i].ID < detected[j].ID
		}
		return a < b
	})
	return detected, nil
}

func ResolveInstallSelection(opts InstallSelectionOptions) (InstallSelection, error) {
	hosts, err := LoadHostSpec(opts.HostsFile)
	if err != nil {
		return InstallSelection{}, err
	}
	explicitAgents := opts.Agents.Kind != "" && opts.Agents.Kind != "auto"
	if opts.CurrentAgent != "" && !explicitAgents {
		selected, errs := ResolveHosts(ExplicitAgents(opts.CurrentAgent), hosts)
		selected = addUniversalHost(selected, hosts)
		return installSelection(hostIDList(selected), nil, !opts.Yes && opts.StdinTTY, errs), nil
	}
	if explicitAgents {
		if opts.Agents.Kind == "*" {
			return installSelection(hostIDList(hosts), nil, !opts.Yes && opts.StdinTTY, nil), nil
		}
		selected, errs := ResolveHosts(opts.Agents, hosts)
		if len(errs) > 0 {
			return errorSelection(errs, nil), nil
		}
		return installSelection(hostIDList(selected), nil, !opts.Yes && opts.StdinTTY, nil), nil
	}
	detected, err := DetectHosts(opts.BaseOptions, opts.Scope)
	if err != nil {
		return InstallSelection{}, err
	}
	detectedIDs := hostIDList(detected)
	if !opts.StdinTTY && !opts.Yes {
		return errorSelection([]map[string]any{{"reason": "agent-selection-required"}}, detectedIDs), nil
	}
	if opts.Yes {
		if len(detected) == 0 {
			return errorSelection([]map[string]any{{"reason": "no-detected-hosts"}}, detectedIDs), nil
		}
		return installSelection(detectedIDs, detectedIDs, false, nil), nil
	}
	if len(detected) == 0 {
		return selectAgentsSelection(hostIDList(hosts), detectedIDs, []string{}), nil
	}
	if len(detected) == 1 {
		return installSelection(detectedIDs, detectedIDs, true, nil), nil
	}
	return selectAgentsSelection(detectedIDs, detectedIDs, []string{}), nil
}

func ResolveInstallTargets(opts BaseOptions, agents AgentSelector, scope Scope, skillName string) ([]TargetGroup, []map[string]any, []string, error) {
	hosts, err := LoadHostSpec(opts.HostsFile)
	if err != nil {
		return nil, nil, nil, err
	}
	home, cwd := defaults(opts)
	if agents.Kind == "" {
		agents = AutoAgents()
	}
	selected := []Host{}
	errs := []map[string]any{}
	if agents.Kind == "auto" {
		selected, err = DetectHosts(opts, scope)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		selected, errs = ResolveHosts(agents, hosts)
	}
	byTarget := map[string]*TargetGroup{}
	for _, host := range selected {
		root := chooseScopePath(host, scope, home, cwd)
		if root == "" {
			errs = append(errs, map[string]any{
				"hostId": host.ID, "skillName": skillName, "scope": string(scope), "reason": "unsupported-scope",
			})
			continue
		}
		targetDir := filepath.Join(root, skillName)
		group := byTarget[targetDir]
		if group == nil {
			group = &TargetGroup{SkillName: skillName, TargetDir: targetDir}
			byTarget[targetDir] = group
		}
		group.HostIDs = append(group.HostIDs, host.ID)
	}
	targets := []TargetGroup{}
	for _, target := range byTarget {
		targets = append(targets, *target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].TargetDir < targets[j].TargetDir })
	detected := []string{}
	for _, target := range targets {
		detected = append(detected, target.HostIDs...)
	}
	return targets, errs, detected, nil
}

func addUniversalHost(selected []Host, hosts []Host) []Host {
	for _, selectedHost := range selected {
		if selectedHost.ID == "universal" {
			return selected
		}
	}
	for _, host := range hosts {
		if host.ID == "universal" {
			return append(selected, host)
		}
	}
	return selected
}

func hostIDList(hosts []Host) []string {
	ids := make([]string, 0, len(hosts))
	for _, host := range hosts {
		ids = append(ids, host.ID)
	}
	return ids
}

func installSelection(selectedHostIDs, detectedHostIDs []string, needsConfirmation bool, errs []map[string]any) InstallSelection {
	if selectedHostIDs == nil {
		selectedHostIDs = []string{}
	}
	if detectedHostIDs == nil {
		detectedHostIDs = []string{}
	}
	if errs == nil {
		errs = []map[string]any{}
	}
	action := "install"
	if len(errs) > 0 {
		action = "error"
		needsConfirmation = false
	}
	return InstallSelection{Action: action, SelectedHostIDs: selectedHostIDs, CandidateHostIDs: []string{}, DetectedHostIDs: detectedHostIDs, NeedsConfirmation: needsConfirmation, Errors: errs}
}

func selectAgentsSelection(candidateHostIDs, detectedHostIDs, selectedHostIDs []string) InstallSelection {
	return InstallSelection{Action: "select-agents", SelectedHostIDs: selectedHostIDs, CandidateHostIDs: candidateHostIDs, DetectedHostIDs: detectedHostIDs, NeedsConfirmation: true, Errors: []map[string]any{}}
}

func errorSelection(errs []map[string]any, detectedHostIDs []string) InstallSelection {
	if detectedHostIDs == nil {
		detectedHostIDs = []string{}
	}
	return InstallSelection{Action: "error", SelectedHostIDs: []string{}, CandidateHostIDs: []string{}, DetectedHostIDs: detectedHostIDs, NeedsConfirmation: false, Errors: errs}
}

func ValidateSkillBundle(bundle SkillBundle) SkillInfo {
	normalized, err := readSkillBundle(bundle)
	if err != nil {
		return SkillInfo{Valid: false, ErrorCode: "invalid-skill-bundle"}
	}
	return validateNormalizedSkill(normalized)
}

func validateNormalizedSkill(bundle normalizedSkillBundle) SkillInfo {
	file, ok := bundle.ByPath["SKILL.md"]
	if !ok {
		return SkillInfo{Valid: false, ErrorCode: "missing-skill-md"}
	}
	text := string(file.Contents)
	if !strings.HasPrefix(text, "---\n") {
		return SkillInfo{Valid: false, ErrorCode: "invalid-frontmatter"}
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return SkillInfo{Valid: false, ErrorCode: "invalid-frontmatter"}
	}
	fields := parseFrontmatter(text[4 : 4+end])
	name := fields["name"]
	description := fields["description"]
	if !skillNamePattern.MatchString(name) || len(description) < 1 || len(description) > 1024 {
		return SkillInfo{Valid: false, ErrorCode: "invalid-frontmatter"}
	}
	return SkillInfo{Valid: true, SkillName: name, Description: description}
}

func ComputeBundleContentHash(bundle SkillBundle) (string, error) {
	normalized, err := readSkillBundle(bundle)
	if err != nil {
		return "", err
	}
	return contentHash(normalized), nil
}

func contentHash(bundle normalizedSkillBundle) string {
	hash := sha256.New()
	for _, file := range bundle.Files {
		hash.Write([]byte(file.Path))
		hash.Write([]byte{0})
		hash.Write(file.Contents)
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func InstallBundledSkill(opts InstallOptions) (InstallReport, error) {
	return installOrPlan(opts, true)
}

func PlanBundledSkill(opts InstallOptions) (InstallReport, error) {
	return installOrPlan(opts, false)
}

func RunBundledSkillInstall(opts InstallWorkflowOptions) (InstallWorkflowReport, error) {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	if !opts.StdinTTY {
		if file, ok := in.(*os.File); ok {
			if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
				opts.StdinTTY = true
			}
		} else if in == os.Stdin {
			if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
				opts.StdinTTY = true
			}
		}
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	reader := bufio.NewReader(in)
	scope, scopeError, err := resolveWorkflowScope(reader, out, opts.Scope, opts.ScopeSet, opts.PromptScope, opts.DefaultScope, opts.Yes, opts.StdinTTY)
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	if len(scopeError.Errors) > 0 {
		renderSelectionErrors(out, scopeError)
		empty := emptyInstallReport(nil)
		return InstallWorkflowReport{Selection: scopeError, Scope: scope, Plan: empty, Report: empty, DryRun: opts.DryRun}, nil
	}
	selection, err := ResolveInstallSelection(InstallSelectionOptions{
		BaseOptions:  opts.BaseOptions,
		Scope:        scope,
		Agents:       opts.Agents,
		Yes:          opts.Yes,
		StdinTTY:     opts.StdinTTY,
		CurrentAgent: opts.CurrentAgent,
	})
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	if selection.Action == "error" {
		renderSelectionErrors(out, selection)
		empty := emptyInstallReport(nil)
		return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: empty, Report: empty, DryRun: opts.DryRun}, nil
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
		selection = installSelection(selected, selection.DetectedHostIDs, !opts.Yes && opts.StdinTTY, nil)
		if len(selected) == 0 {
			empty := emptyInstallReport(nil)
			return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: empty, Report: empty, Canceled: true, DryRun: opts.DryRun}, nil
		}
	}
	installOpts := opts.InstallOptions
	installOpts.Agents = ExplicitAgents(selection.SelectedHostIDs...)
	installOpts.Scope = scope
	plan, err := PlanBundledSkill(installOpts)
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	if len(plan.Installed)+len(plan.Updated)+len(plan.Conflicts)+len(plan.Errors) == 0 {
		return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: plan, Report: plan, DryRun: opts.DryRun}, nil
	}
	if opts.DryRun {
		renderInstallSummary(out, plan)
		return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: plan, Report: plan, DryRun: true}, nil
	}
	if len(plan.Conflicts)+len(plan.Errors) > 0 {
		report := plan
		report.Installed = []TargetResult{}
		report.Updated = []TargetResult{}
		return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: plan, Report: report, DryRun: opts.DryRun}, nil
	}
	renderInstallSummary(out, plan)
	if len(plan.Installed)+len(plan.Updated) == 0 {
		return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: plan, Report: plan}, nil
	}
	if selection.NeedsConfirmation {
		confirmed, err := promptConfirmation(reader, out)
		if err != nil {
			return InstallWorkflowReport{}, err
		}
		if !confirmed {
			return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: plan, Report: emptyInstallReport(nil), Canceled: true}, nil
		}
	}
	report, err := InstallBundledSkill(installOpts)
	if err != nil {
		return InstallWorkflowReport{}, err
	}
	return InstallWorkflowReport{Selection: selection, Scope: scope, Plan: plan, Report: report}, nil
}

func UpdateBundledSkill(opts InstallOptions) (InstallReport, error) {
	return InstallBundledSkill(opts)
}

func UninstallBundledSkill(opts UninstallOptions) (UninstallReport, error) {
	targets, errs, _, err := ResolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, opts.SkillName)
	if err != nil {
		return UninstallReport{}, err
	}
	report := emptyUninstallReport(errs)
	for _, target := range targets {
		result := targetResult(target)
		meta, present, managed := readMetadata(target.TargetDir)
		switch {
		case !present:
			report.Skipped = append(report.Skipped, withReason(result, "missing"))
		case !managed:
			report.Conflicts = append(report.Conflicts, withReason(result, "unmanaged"))
		case meta.AppID != opts.AppID:
			report.Conflicts = append(report.Conflicts, withReason(result, "owner-mismatch"))
		default:
			if err := os.RemoveAll(target.TargetDir); err != nil {
				return report, err
			}
			report.Removed = append(report.Removed, result)
		}
	}
	return report, nil
}

func installOrPlan(opts InstallOptions, write bool) (InstallReport, error) {
	bundle, bundleMeta, err := resolveSkillBundle(opts.SkillBundle)
	if err != nil {
		reason := "invalid-skill-bundle"
		if opts.SkillBundle.kind == "github" {
			reason = "bundle-resolve-failed"
		}
		return emptyInstallReport([]map[string]any{{"reason": reason}}), nil
	}
	skill := validateNormalizedSkill(bundle)
	if !skill.Valid {
		return emptyInstallReport([]map[string]any{{"reason": skill.ErrorCode}}), nil
	}
	hash := contentHash(bundle)
	targets, errs, _, err := ResolveInstallTargets(opts.BaseOptions, opts.Agents, opts.Scope, skill.SkillName)
	if err != nil {
		return InstallReport{}, err
	}
	report := emptyInstallReport(errs)
	for _, target := range targets {
		result := targetResult(target)
		meta, present, managed := readMetadata(target.TargetDir)
		switch {
		case !present:
			if write {
				if err := copyManagedSkill(bundle, target.TargetDir, opts.AppID, skill.SkillName, hash, bundleMeta); err != nil {
					return report, err
				}
			}
			report.Installed = append(report.Installed, result)
		case !managed:
			if opts.Force {
				if write {
					if err := replaceManagedSkill(bundle, target.TargetDir, opts.AppID, skill.SkillName, hash, bundleMeta); err != nil {
						return report, err
					}
				}
				report.Updated = append(report.Updated, result)
				continue
			}
			report.Conflicts = append(report.Conflicts, withReason(result, "unmanaged"))
		case meta.AppID != opts.AppID:
			if opts.Force {
				if write {
					if err := replaceManagedSkill(bundle, target.TargetDir, opts.AppID, skill.SkillName, hash, bundleMeta); err != nil {
						return report, err
					}
				}
				report.Updated = append(report.Updated, result)
				continue
			}
			report.Conflicts = append(report.Conflicts, withReason(result, "owner-mismatch"))
		case meta.Hash == hash:
			report.Skipped = append(report.Skipped, withReason(result, "unchanged"))
		default:
			if write {
				if err := replaceManagedSkill(bundle, target.TargetDir, opts.AppID, skill.SkillName, hash, bundleMeta); err != nil {
					return report, err
				}
			}
			report.Updated = append(report.Updated, result)
		}
	}
	return report, nil
}

func copyManagedSkill(bundle normalizedSkillBundle, targetDir, appID, skillName, hash string, bundleMeta bundleMetadata) error {
	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}
	if err := copySkillBundle(bundle, targetDir); err != nil {
		return err
	}
	return writeMetadata(targetDir, appID, skillName, hash, bundleMeta)
}

func replaceManagedSkill(bundle normalizedSkillBundle, targetDir, appID, skillName, hash string, bundleMeta bundleMetadata) error {
	suffix := ".kitup-" + time.Now().Format("20060102150405.000000000")
	tmp := targetDir + suffix
	backup := targetDir + suffix + "-backup"
	_ = os.RemoveAll(tmp)
	if err := copySkillBundle(bundle, tmp); err != nil {
		return err
	}
	if err := writeMetadata(tmp, appID, skillName, hash, bundleMeta); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(targetDir, backup); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, targetDir); err != nil {
		_ = os.RemoveAll(tmp)
		if !exists(targetDir) && exists(backup) {
			_ = os.Rename(backup, targetDir)
		}
		return err
	}
	return os.RemoveAll(backup)
}

func copySkillBundle(bundle normalizedSkillBundle, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, file := range bundle.Files {
		to := filepath.Join(dest, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		mode := file.Mode.Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(to, file.Contents, mode); err != nil {
			return err
		}
	}
	return nil
}

func writeMetadata(targetDir, appID, skillName, hash string, bundleMeta bundleMetadata) error {
	meta := metadata{
		SchemaVersion: 1,
		AppID:         appID,
		SkillName:     skillName,
		Source:        bundleMeta.Source,
		Hash:          hash,
		SourceID:      bundleMeta.SourceID,
		Version:       bundleMeta.Version,
		Provenance:    bundleMeta.Provenance,
	}
	if meta.Source == "" {
		meta.Source = "bundled"
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, ".kitup.json"), append(data, '\n'), 0o644)
}

func readMetadata(targetDir string) (metadata, bool, bool) {
	if !exists(targetDir) {
		return metadata{}, false, false
	}
	data, err := os.ReadFile(filepath.Join(targetDir, ".kitup.json"))
	if err != nil {
		return metadata{}, true, false
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}, true, false
	}
	return meta, true, true
}

func targetResult(target TargetGroup) TargetResult {
	result := TargetResult{SkillName: target.SkillName, TargetDir: target.TargetDir}
	if len(target.HostIDs) == 1 {
		result.HostID = target.HostIDs[0]
	} else {
		result.HostIDs = target.HostIDs
	}
	return result
}

func withReason(result TargetResult, reason string) TargetStatus {
	return TargetStatus{TargetResult: result, Reason: reason}
}

func emptyInstallReport(errs []map[string]any) InstallReport {
	return InstallReport{
		Installed: []TargetResult{},
		Updated:   []TargetResult{},
		Skipped:   []TargetStatus{},
		Conflicts: []TargetStatus{},
		Errors:    reportErrors(errs),
	}
}

func emptyUninstallReport(errs []map[string]any) UninstallReport {
	return UninstallReport{
		Removed:   []TargetResult{},
		Skipped:   []TargetStatus{},
		Conflicts: []TargetStatus{},
		Errors:    reportErrors(errs),
	}
}

func reportErrors(errs []map[string]any) []ReportError {
	if errs == nil {
		return []ReportError{}
	}
	result := make([]ReportError, 0, len(errs))
	for _, err := range errs {
		result = append(result, ReportError{
			Agent:     stringField(err, "agent"),
			Flag:      stringField(err, "flag"),
			HostID:    stringField(err, "hostId"),
			Reason:    stringField(err, "reason"),
			Scope:     Scope(stringField(err, "scope")),
			SkillName: stringField(err, "skillName"),
			Value:     stringField(err, "value"),
		})
	}
	return result
}

func stringField(value map[string]any, key string) string {
	if text, ok := value[key].(string); ok {
		return text
	}
	return ""
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

func hostsByID(hosts []Host, ids []string) []Host {
	byID := map[string]Host{}
	for _, host := range hosts {
		byID[host.ID] = host
	}
	selected := []Host{}
	for _, id := range ids {
		if host, ok := byID[id]; ok {
			selected = append(selected, host)
		}
	}
	return selected
}

func canonicalScopePath(host Host, scope Scope, home, cwd string) string {
	paths := scopePaths(host, scope)
	if len(paths) == 0 {
		return ""
	}
	return expandHostPath(paths[0], home, cwd)
}

func chooseScopePath(host Host, scope Scope, home, cwd string) string {
	paths := scopePaths(host, scope)
	for _, path := range paths {
		expanded := expandHostPath(path, home, cwd)
		if exists(expanded) {
			return expanded
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return expandHostPath(paths[0], home, cwd)
}

func scopePaths(host Host, scope Scope) []string {
	if scope == UserScope {
		return host.UserSkillsDir
	}
	return host.ProjectSkillsDir
}

func expandHostPath(path, home, cwd string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return filepath.Join(cwd, path)
}

func defaults(opts BaseOptions) (string, string) {
	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	cwd := opts.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return home, cwd
}

func parseFrontmatter(content string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		before, after, ok := strings.Cut(line, ":")
		if ok {
			fields[before] = strings.TrimSpace(after)
		}
	}
	return fields
}

func resolveSkillBundle(bundle SkillBundle) (normalizedSkillBundle, bundleMetadata, error) {
	switch bundle.kind {
	case "github":
		return resolveGitHubBundle(bundle.github)
	default:
		normalized, err := readSkillBundle(bundle)
		if err != nil {
			return normalizedSkillBundle{}, bundleMetadata{}, err
		}
		return normalized, bundleMetadata{Source: "bundled"}, nil
	}
}

func resolveGitHubBundle(opts GitHubBundleOptions) (normalizedSkillBundle, bundleMetadata, error) {
	root := trimGitHubPath(opts.Path)
	if opts.Owner == "" || opts.Repo == "" || root == "" || opts.Ref == "" {
		return normalizedSkillBundle{}, bundleMetadata{}, errors.New("invalid github bundle")
	}
	apiBase := envBaseURL("KITUP_GITHUB_API_BASE_URL", "https://api.github.com")
	rawBase := envBaseURL("KITUP_GITHUB_RAW_BASE_URL", "https://raw.githubusercontent.com")
	var commit struct {
		Sha    string `json:"sha"`
		Commit struct {
			Tree struct {
				Sha string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	if err := getJSON(apiBase+"/repos/"+escapePathPart(opts.Owner)+"/"+escapePathPart(opts.Repo)+"/commits/"+escapePathPart(opts.Ref), &commit); err != nil {
		return normalizedSkillBundle{}, bundleMetadata{}, err
	}
	if commit.Sha == "" || commit.Commit.Tree.Sha == "" {
		return normalizedSkillBundle{}, bundleMetadata{}, errors.New("invalid github commit")
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Mode string `json:"mode"`
		} `json:"tree"`
	}
	if err := getJSON(apiBase+"/repos/"+escapePathPart(opts.Owner)+"/"+escapePathPart(opts.Repo)+"/git/trees/"+escapePathPart(commit.Commit.Tree.Sha)+"?recursive=1", &tree); err != nil {
		return normalizedSkillBundle{}, bundleMetadata{}, err
	}
	prefix := root + "/"
	files := []SkillFile{}
	for _, item := range tree.Tree {
		if item.Type != "blob" || !strings.HasPrefix(item.Path, prefix) {
			continue
		}
		contents, err := getBytes(rawBase + "/" + escapePathPart(opts.Owner) + "/" + escapePathPart(opts.Repo) + "/" + escapePathPart(commit.Sha) + "/" + escapePath(item.Path))
		if err != nil {
			return normalizedSkillBundle{}, bundleMetadata{}, err
		}
		mode := fs.FileMode(0o644)
		if item.Mode == "100755" {
			mode = 0o755
		}
		files = append(files, SkillFile{Path: strings.TrimPrefix(item.Path, prefix), Contents: contents, Mode: mode})
	}
	if len(files) == 0 {
		return normalizedSkillBundle{}, bundleMetadata{}, errors.New("github bundle path not found")
	}
	bundle, err := normalizeSkillFiles(files)
	if err != nil {
		return normalizedSkillBundle{}, bundleMetadata{}, err
	}
	return bundle, bundleMetadata{
		Source:   "github",
		SourceID: "github:" + opts.Owner + "/" + opts.Repo + "/" + root,
		Version:  opts.Ref,
		Provenance: map[string]string{
			"owner":          opts.Owner,
			"repo":           opts.Repo,
			"path":           root,
			"ref":            opts.Ref,
			"resolvedCommit": commit.Sha,
		},
	}, nil
}

func envBaseURL(name, fallback string) string {
	value := strings.TrimRight(os.Getenv(name), "/")
	if value == "" {
		return fallback
	}
	return value
}

func getJSON(url string, value any) error {
	data, err := getBytes(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func getBytes(value string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "kitup")
	client := http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("github request failed: %s", value)
	}
	return io.ReadAll(response.Body)
}

func trimGitHubPath(value string) string {
	return strings.Trim(value, "/")
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for index, part := range parts {
		parts[index] = escapePathPart(part)
	}
	return strings.Join(parts, "/")
}

func escapePathPart(value string) string {
	return url.PathEscape(value)
}

func readSkillBundle(bundle SkillBundle) (normalizedSkillBundle, error) {
	switch bundle.kind {
	case "directory":
		files, err := readDirectoryBundleFiles(bundle.dir)
		if err != nil {
			return normalizedSkillBundle{}, err
		}
		return normalizeSkillFiles(files)
	case "fs":
		files, err := readFSBundleFiles(bundle.fsys, bundle.root)
		if err != nil {
			return normalizedSkillBundle{}, err
		}
		return normalizeSkillFiles(files)
	case "files":
		return normalizeSkillFiles(bundle.files)
	case "github":
		normalized, _, err := resolveGitHubBundle(bundle.github)
		return normalized, err
	default:
		return normalizedSkillBundle{}, errors.New("missing skill bundle")
	}
}

func readDirectoryBundleFiles(root string) ([]SkillFile, error) {
	var files []SkillFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if skipName(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files = append(files, SkillFile{Path: filepath.ToSlash(rel), Contents: contents, Mode: info.Mode().Perm()})
		}
		return nil
	})
	return files, err
}

func readFSBundleFiles(fsys fs.FS, root string) ([]SkillFile, error) {
	if fsys == nil {
		return nil, errors.New("missing skill fs")
	}
	root = strings.Trim(root, "/")
	if root == "" {
		root = "."
	}
	if root != "." && !fs.ValidPath(root) {
		return nil, errors.New("invalid skill fs root")
	}
	var files []SkillFile
	err := fs.WalkDir(fsys, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if skipName(entry.Name()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			rel := path
			if root != "." {
				rel = strings.TrimPrefix(path, root+"/")
			}
			contents, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			files = append(files, SkillFile{Path: rel, Contents: contents, Mode: info.Mode().Perm()})
		}
		return nil
	})
	return files, err
}

func normalizeSkillFiles(files []SkillFile) (normalizedSkillBundle, error) {
	byPath := map[string]bundleFile{}
	for _, file := range files {
		normalizedPath, include, err := normalizeBundlePath(file.Path)
		if err != nil {
			return normalizedSkillBundle{}, err
		}
		if !include {
			continue
		}
		if _, ok := byPath[normalizedPath]; ok {
			return normalizedSkillBundle{}, errors.New("duplicate skill file: " + normalizedPath)
		}
		mode := file.Mode.Perm()
		if mode == 0 {
			mode = 0o644
		}
		byPath[normalizedPath] = bundleFile{Path: normalizedPath, Contents: file.Contents, Mode: mode}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	normalized := normalizedSkillBundle{Files: make([]bundleFile, 0, len(paths)), ByPath: byPath}
	for _, path := range paths {
		normalized.Files = append(normalized.Files, byPath[path])
	}
	return normalized, nil
}

func normalizeBundlePath(value string) (string, bool, error) {
	if value == "" || strings.Contains(value, "\\") || pathpkg.IsAbs(value) || strings.HasPrefix(value, "/") {
		return "", false, errors.New("invalid skill file path: " + value)
	}
	if len(value) > 1 && value[1] == ':' {
		return "", false, errors.New("invalid skill file path: " + value)
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false, errors.New("invalid skill file path: " + value)
		}
		if skipName(part) {
			return "", false, nil
		}
	}
	return strings.Join(parts, "/"), true, nil
}

func copySkillBundleDir(src, dest string) error {
	bundle, err := readSkillBundle(DirectoryBundle(src))
	if err != nil {
		return err
	}
	return copySkillBundle(bundle, dest)
}

func skipName(name string) bool {
	return name == ".git" || name == ".kitup.json" || name == ".DS_Store" || strings.HasSuffix(name, ".swp") || strings.HasSuffix(name, "~")
}

func isGenericDetectPath(path string) bool {
	return path == "~/.agents" || path == "~/.agents/skills" || path == "~/.config/agents"
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
