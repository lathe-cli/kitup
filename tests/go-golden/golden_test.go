package kitup

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type goldenRequest struct {
	ID        string            `json:"id"`
	Operation string            `json:"operation"`
	Options   map[string]any    `json:"options"`
	Given     map[string]any    `json:"given"`
	Detect    bool              `json:"detect"`
	Env       map[string]string `json:"env"`
}

func TestGoldenCases(t *testing.T) {
	input := os.Getenv("KITUP_GOLDEN_INPUT")
	if input == "" {
		root := os.Getenv("KITUP_TEST_REPO_ROOT")
		if root == "" {
			t.Skip("shared golden cases require KITUP_TEST_REPO_ROOT")
		}
		command := exec.Command("node", filepath.Join(root, "scripts/golden.mjs"), "--", os.Args[0], "-test.run=^TestGoldenCases$", "-test.v")
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		must(t, command.Run())
		return
	}
	data, err := os.ReadFile(input)
	must(t, err)
	var requests []goldenRequest
	must(t, json.Unmarshal(data, &requests))
	results := make([]map[string]any, 0, len(requests))
	for _, request := range requests {
		for _, name := range []string{"KITUP_GITHUB_API_BASE_URL", "KITUP_GITHUB_RAW_BASE_URL"} {
			if value, ok := request.Env[name]; ok {
				t.Setenv(name, value)
			} else {
				must(t, os.Unsetenv(name))
			}
		}
		result, err := dispatchGolden(request)
		if result == nil {
			result = map[string]any{}
		}
		result["id"] = request.ID
		if err != nil {
			result["threw"], result["error"] = true, err.Error()
		}
		results = append(results, result)
	}
	data, err = json.Marshal(results)
	must(t, err)
	must(t, os.WriteFile(os.Getenv("KITUP_GOLDEN_OUTPUT"), data, 0o600))
}

func dispatchGolden(request goldenRequest) (map[string]any, error) {
	opts := request.Options
	base := BaseOptions{
		Home: stringValue(opts["home"]), CWD: stringValue(opts["cwd"]), HostsFile: stringValue(opts["hostsFile"]),
	}
	lifecycle := UninstallOptions{
		BaseOptions: base, AppID: stringValue(opts["appId"]), SkillName: stringValue(opts["skillName"]),
		Scope: Scope(stringValue(opts["scope"])), Agents: agentSelector(opts["agents"]),
	}
	install := InstallOptions{
		BaseOptions: base, AppID: lifecycle.AppID, SkillBundle: skillBundleFromOptions(opts),
		Scope: lifecycle.Scope, Agents: lifecycle.Agents, Force: boolValue(opts["force"]),
	}
	switch request.Operation {
	case "resolve-hosts":
		hosts, err := LoadHostSpec(stringValue(request.Given["hostsFile"]))
		if err != nil {
			return nil, err
		}
		selected, errs := ResolveHosts(lifecycle.Agents, hosts)
		return map[string]any{"count": len(selected), "hostIds": hostIDList(selected), "resolvedHostIds": hostIDList(selected), "errors": errs}, nil
	case "validate":
		info := ValidateSkillBundle(install.SkillBundle)
		return map[string]any{"valid": info.Valid, "errorCode": info.ErrorCode}, nil
	case "parse-install-flags":
		parsed := ParseInstallFlags(InstallFlagValues{
			Scope: stringValue(opts["scope"]), ScopeSet: boolValue(opts["scopeSet"]) || opts["scope"] != nil,
			Agents: stringSlice(opts["agents"]), Yes: boolValue(opts["yes"]), DryRun: boolValue(opts["dryRun"]), Force: install.Force,
		})
		ids := append([]string{}, parsed.Agents.IDs...)
		return map[string]any{"parsed": map[string]any{
			"scope": parsed.Scope, "scopeSet": parsed.ScopeSet, "agentKind": parsed.Agents.Kind, "agentIds": ids,
			"yes": parsed.Yes, "dryRun": parsed.DryRun, "force": parsed.Force, "errors": parsed.Errors,
		}}, nil
	case "resolve-install-selection":
		selection, err := ResolveInstallSelection(InstallSelectionOptions{
			BaseOptions: base, Scope: lifecycle.Scope, Agents: lifecycle.Agents,
			Yes: boolValue(opts["yes"]), StdinTTY: boolValue(opts["stdinTTY"]),
		})
		return map[string]any{"selection": selection}, err
	case "resolve-install-targets":
		targets, _, _, err := ResolveInstallTargets(base, lifecycle.Agents, lifecycle.Scope, lifecycle.SkillName)
		result := []map[string]any{}
		for _, target := range targets {
			result = append(result, map[string]any{"hostIds": target.HostIDs, "skillName": target.SkillName, "targetDir": target.TargetDir})
		}
		return map[string]any{"targets": result}, err
	case "run-install-workflow":
		var out bytes.Buffer
		report, err := RunBundledSkillInstall(InstallWorkflowOptions{
			InstallOptions: install, Yes: boolValue(opts["yes"]), DryRun: boolValue(opts["dryRun"]), StdinTTY: boolValue(opts["stdinTTY"]),
			DefaultScope: Scope(stringValue(opts["defaultScope"])), ScopeSet: boolValue(opts["scopeSet"]) || opts["scope"] != nil,
			PromptScope: boolValue(opts["promptScope"]), In: strings.NewReader(stringValue(opts["input"])), Out: &out,
		})
		return map[string]any{"workflow": report, "report": report.Report, "exit": ClassifyInstallWorkflowExit(report), "output": out.String()}, err
	}
	result := map[string]any{}
	if request.Detect {
		hosts, err := DetectHosts(base, lifecycle.Scope)
		if err != nil {
			return result, err
		}
		result["detectedHosts"] = hostIDList(hosts)
	}
	var err error
	switch request.Operation {
	case "status":
		result["report"], err = StatusBundledSkill(StatusOptions(lifecycle))
	case "uninstall":
		result["report"], err = UninstallBundledSkill(lifecycle)
	default:
		operation := map[string]func(InstallOptions) (InstallReport, error){"install": InstallBundledSkill, "update": UpdateBundledSkill, "plan": PlanBundledSkill}[request.Operation]
		result["report"], err = operation(install)
	}
	return result, err
}

func agentSelector(value any) AgentSelector {
	if value == "*" {
		return AllAgents()
	}
	if _, ok := value.([]any); ok {
		return ExplicitAgents(stringSlice(value)...)
	}
	return AutoAgents()
}

func skillBundleFromOptions(opts map[string]any) SkillBundle {
	var bundle SkillBundle
	if files, ok := opts["skillFiles"].([]any); ok {
		items := make([]SkillFile, 0, len(files))
		for _, raw := range files {
			item := raw.(map[string]any)
			mode, _ := item["mode"].(float64)
			items = append(items, SkillFile{Path: item["path"].(string), Contents: []byte(item["contents"].(string)), Mode: os.FileMode(mode)})
		}
		bundle = FilesBundle(items)
	} else if dir, ok := opts["skillBundleDir"].(string); ok {
		bundle = DirectoryBundle(dir)
	} else if github, ok := opts["githubBundle"].(map[string]any); ok {
		bundle = GitHubBundle(GitHubBundleOptions{
			Owner: github["owner"].(string), Repo: github["repo"].(string), Path: github["path"].(string), Ref: github["ref"].(string),
		})
	}
	if meta, ok := opts["bundleMetadata"].(map[string]any); ok {
		provenance := map[string]string{}
		if values, ok := meta["provenance"].(map[string]any); ok {
			for key, value := range values {
				provenance[key] = value.(string)
			}
		}
		bundle = WithBundleMetadata(bundle, BundledSkillMetadata{
			SourceID: stringValue(meta["sourceId"]), CLIVersion: stringValue(meta["cliVersion"]),
			CLIRevision: stringValue(meta["cliRevision"]), Provenance: provenance,
		})
	}
	return bundle
}

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringSlice(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.(string))
	}
	return result
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
