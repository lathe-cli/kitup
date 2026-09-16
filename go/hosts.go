package kitup

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type hostSpec struct {
	SchemaVersion int    `json:"schemaVersion"`
	Hosts         []Host `json:"hosts"`
}

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
	if err := validateHostSpec(spec.Hosts); err != nil {
		return nil, err
	}
	return spec.Hosts, nil
}

func validateHostSpec(hosts []Host) error {
	for _, host := range hosts {
		installDirs := map[string]bool{}
		for _, path := range host.ProjectSkillsDir {
			if !isProjectHostPath(path) {
				return fmt.Errorf("invalid project path %q for host %q", path, host.ID)
			}
			installDirs[path] = true
		}
		for _, path := range host.UserSkillsDir {
			if !isHomeHostPath(path) {
				return fmt.Errorf("invalid user path %q for host %q", path, host.ID)
			}
			installDirs[path] = true
		}
		for _, path := range host.Detect {
			if !isHomeHostPath(path) && !isProjectHostPath(path) {
				return fmt.Errorf("invalid detect path %q for host %q", path, host.ID)
			}
			if !isGenericDetectPath(path) && installDirs[path] {
				return fmt.Errorf("detect path is an install target for host %q: %q", host.ID, path)
			}
		}
	}
	return nil
}

func isProjectHostPath(path string) bool {
	return path != "" && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~") && isSafeHostPath(path)
}

func isHomeHostPath(path string) bool {
	return strings.HasPrefix(path, "~/") && isSafeHostPath(path[2:])
}

func isSafeHostPath(path string) bool {
	return path != "." && fs.ValidPath(path) && !strings.ContainsAny(path, "\x00\\:")
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
		for _, path := range host.Detect {
			if isGenericDetectPath(path) {
				continue
			}
			if detectionPathExists(expandHostPath(path, home, cwd)) {
				detected = append(detected, host)
				break
			}
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
	return resolveInstallTargets(opts, agents, scope, skillName, "")
}

func resolveInstallTargets(opts BaseOptions, agents AgentSelector, scope Scope, skillName, uninstallAppID string) ([]TargetGroup, []map[string]any, []string, error) {
	if !isValidSkillName(skillName) {
		return nil, []map[string]any{{
			"skillName": skillName,
			"reason":    "invalid-skill-name",
		}}, nil, nil
	}
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
		roots := []string{}
		if uninstallAppID != "" {
			roots = uninstallScopePaths(host, scope, home, cwd, skillName, uninstallAppID)
		} else if root := chooseScopePath(host, scope, home, cwd, skillName); root != "" {
			roots = append(roots, root)
		}
		if len(roots) == 0 {
			errs = append(errs, map[string]any{
				"hostId": host.ID, "skillName": skillName, "scope": string(scope), "reason": "unsupported-scope",
			})
			continue
		}
		for _, root := range roots {
			targetDir := filepath.Join(root, skillName)
			group := byTarget[targetDir]
			if group == nil {
				group = &TargetGroup{SkillName: skillName, TargetDir: targetDir}
				byTarget[targetDir] = group
			}
			if !slices.Contains(group.HostIDs, host.ID) {
				group.HostIDs = append(group.HostIDs, host.ID)
			}
		}
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

func chooseScopePath(host Host, scope Scope, home, cwd, skillName string) string {
	fallback := ""
	for _, path := range scopePaths(host, scope) {
		root := expandHostPath(path, home, cwd)
		if !isDirectory(root) {
			continue
		}
		if fallback == "" {
			fallback = root
		}
		meta, _, managed := readMetadata(filepath.Join(root, skillName))
		if managed && meta.SkillName == skillName {
			return root
		}
	}
	if fallback != "" {
		return fallback
	}
	return canonicalScopePath(host, scope, home, cwd)
}

func uninstallScopePaths(host Host, scope Scope, home, cwd, skillName, appID string) []string {
	owned := []string{}
	for _, path := range scopePaths(host, scope) {
		root := expandHostPath(path, home, cwd)
		_, reason := inspectTarget(filepath.Join(root, skillName), appID, skillName)
		if reason == "" {
			owned = append(owned, root)
		}
	}
	if len(owned) > 0 {
		return owned
	}
	if fallback := chooseScopePath(host, scope, home, cwd, skillName); fallback != "" {
		return []string{fallback}
	}
	return nil
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

func isGenericDetectPath(path string) bool {
	switch path {
	case "~/.agents", "~/.agents/skills", "~/.config/agents",
		".agents", ".agents/skills", "package.json":
		return true
	}
	return false
}

func detectionPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
