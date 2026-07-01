package kitup

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type goldenFile struct {
	Cases []goldenCase `json:"cases"`
}

type goldenCase struct {
	ID        string         `json:"id"`
	Operation string         `json:"operation"`
	Options   map[string]any `json:"options"`
	Given     map[string]any `json:"given"`
	Expected  map[string]any `json:"expected"`
}

func TestGoldenCases(t *testing.T) {
	var file goldenFile
	readJSON(t, "../testdata/cases/bundled-skill-install.json", &file)
	for _, tc := range file.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			workspace := filepath.Join(root, "workspace")
			must(t, os.MkdirAll(home, 0o755))
			must(t, os.MkdirAll(workspace, 0o755))
			setupGiven(t, tc, home, workspace)
			runCase(t, tc, home, workspace)
		})
	}
}

func runCase(t *testing.T, tc goldenCase, home, workspace string) {
	opts := expandValue(tc.Options, home, workspace).(map[string]any)
	switch tc.Operation {
	case "resolve-hosts":
		hostsFile := repoPathFromCase(tc.Given["hostsFile"].(string))
		hosts, err := LoadHostSpec(hostsFile)
		must(t, err)
		resolved, errs := ResolveHosts(agentSelector(opts["agents"]), hosts)
		if expected, ok := tc.Expected["count"]; ok && int(expected.(float64)) != len(resolved) {
			t.Fatalf("count: got %d want %v", len(resolved), expected)
		}
		if expected, ok := tc.Expected["hostIds"]; ok {
			equal(t, hostIDs(resolved), expected)
		}
		if expected, ok := tc.Expected["resolvedHostIds"]; ok {
			equal(t, hostIDs(resolved), expected)
		}
		if expected, ok := tc.Expected["errors"]; ok {
			equal(t, errs, expected)
		}
	case "validate":
		result := ValidateSkillBundle(skillBundleFromOptions(opts))
		equal(t, result.Valid, tc.Expected["valid"])
		equal(t, result.ErrorCode, tc.Expected["errorCode"])
	case "parse-install-flags":
		assertParsedFlags(t, ParseInstallFlags(InstallFlagValues{
			Scope:    stringValue(opts["scope"]),
			ScopeSet: boolValue(opts["scopeSet"]) || opts["scope"] != nil,
			Agents:   stringSlice(opts["agents"]),
			Yes:      boolValue(opts["yes"]),
			DryRun:   boolValue(opts["dryRun"]),
			Force:    boolValue(opts["force"]),
		}), tc.Expected["parsed"].(map[string]any))
	case "resolve-install-selection":
		selection, err := ResolveInstallSelection(InstallSelectionOptions{
			BaseOptions: baseOptions(home, workspace),
			Scope:       Scope(opts["scope"].(string)),
			Agents:      agentSelector(opts["agents"]),
			Yes:         boolValue(opts["yes"]),
			StdinTTY:    boolValue(opts["stdinTTY"]),
		})
		must(t, err)
		assertSelection(t, normalize(selection).(map[string]any), tc.Expected["selection"].(map[string]any))
	case "run-install-workflow":
		var out bytes.Buffer
		report, err := RunBundledSkillInstall(InstallWorkflowOptions{
			InstallOptions: InstallOptions{
				BaseOptions: baseOptions(home, workspace),
				AppID:       opts["appId"].(string),
				SkillBundle: skillBundleFromOptions(opts),
				Scope:       Scope(stringValue(opts["scope"])),
				Agents:      agentSelector(opts["agents"]),
				Force:       boolValue(opts["force"]),
			},
			Yes:          boolValue(opts["yes"]),
			DryRun:       boolValue(opts["dryRun"]),
			StdinTTY:     boolValue(opts["stdinTTY"]),
			DefaultScope: Scope(stringValue(opts["defaultScope"])),
			ScopeSet:     boolValue(opts["scopeSet"]) || opts["scope"] != nil,
			PromptScope:  boolValue(opts["promptScope"]),
			In:           strings.NewReader(stringValue(opts["input"])),
			Out:          &out,
		})
		must(t, err)
		assertWorkflow(t, normalize(report).(map[string]any), tc.Expected["workflow"])
		if expected, ok := tc.Expected["exit"]; ok {
			equal(t, ClassifyInstallWorkflowExit(report), expected)
		}
		assertOutput(t, out.String(), tc.Expected["output"])
		assertOutputContains(t, out.String(), tc.Expected["outputContains"])
		if expected, ok := tc.Expected["report"]; ok {
			equal(t, report.Report, expandValue(expected, home, workspace))
		}
		assertExpectedFiles(t, tc, home, workspace)
		assertExpectedMetadata(t, tc, home, workspace)
	default:
		if expected, ok := tc.Expected["detectedHosts"]; ok {
			hosts, err := DetectHosts(BaseOptions{Home: home, CWD: workspace, HostsFile: repoPathFromCase("spec/hosts.json")}, Scope(opts["scope"].(string)))
			must(t, err)
			equal(t, hostIDs(hosts), expected)
		}
		report := runReportCase(t, tc, opts, home, workspace)
		if expected, ok := tc.Expected["report"]; ok {
			equal(t, report, expandValue(expected, home, workspace))
		}
		assertExpectedWriteCounts(t, tc, report, home, workspace)
		assertExpectedFiles(t, tc, home, workspace)
		assertExpectedMetadata(t, tc, home, workspace)
	}
}

func runReportCase(t *testing.T, tc goldenCase, opts map[string]any, home, workspace string) any {
	base := baseOptions(home, workspace)
	switch tc.Operation {
	case "uninstall":
		report, err := UninstallBundledSkill(UninstallOptions{
			BaseOptions: base,
			AppID:       opts["appId"].(string),
			SkillName:   opts["skillName"].(string),
			Scope:       Scope(opts["scope"].(string)),
			Agents:      agentSelector(opts["agents"]),
		})
		must(t, err)
		return report
	case "install", "update", "plan":
		fn := InstallBundledSkill
		if tc.Operation == "update" {
			fn = UpdateBundledSkill
		}
		if tc.Operation == "plan" {
			fn = PlanBundledSkill
		}
		report, err := fn(InstallOptions{
			BaseOptions: base,
			AppID:       opts["appId"].(string),
			SkillBundle: skillBundleFromOptions(opts),
			Scope:       Scope(opts["scope"].(string)),
			Agents:      agentSelector(opts["agents"]),
		})
		must(t, err)
		return report
	default:
		t.Fatalf("unsupported operation: %s", tc.Operation)
		return nil
	}
}

func baseOptions(home, workspace string) BaseOptions {
	return BaseOptions{Home: home, CWD: workspace, HostsFile: repoPathFromCase("spec/hosts.json")}
}

func setupGiven(t *testing.T, tc goldenCase, home, workspace string) {
	for _, dir := range stringSlice(tc.Given["dirs"]) {
		must(t, os.MkdirAll(expandString(dir, home, workspace), 0o755))
	}
	if files, ok := tc.Given["files"].(map[string]any); ok {
		for path, value := range files {
			writeFixtureFile(t, expandString(path, home, workspace), value)
		}
	}
	if target, ok := tc.Given["copySkillBundleTo"].(string); ok {
		must(t, os.RemoveAll(expandString(target, home, workspace)))
		must(t, copySkillBundleDir(caseSkillBundleDir(tc), expandString(target, home, workspace)))
	}
	if meta, ok := tc.Given["metadata"].(map[string]any); ok {
		writeMetadataFixture(t, tc, home, workspace, meta)
	}
	if github, ok := tc.Given["github"].(map[string]any); ok {
		startGitHubFixture(t, github)
	}
}

func assertExpectedFiles(t *testing.T, tc goldenCase, home, workspace string) {
	for _, path := range stringSlice(tc.Expected["filesPresent"]) {
		if !exists(expandString(path, home, workspace)) {
			t.Fatalf("expected file to exist: %s", expandString(path, home, workspace))
		}
	}
	for _, path := range stringSlice(tc.Expected["filesAbsent"]) {
		if exists(expandString(path, home, workspace)) {
			t.Fatalf("expected file to be absent: %s", expandString(path, home, workspace))
		}
	}
}

func assertExpectedMetadata(t *testing.T, tc goldenCase, home, workspace string) {
	meta, ok := tc.Expected["metadata"].(map[string]any)
	if !ok {
		return
	}
	path := expandString(meta["path"].(string), home, workspace)
	var actual map[string]any
	readJSON(t, path, &actual)
	for key, value := range meta["fields"].(map[string]any) {
		equal(t, actual[key], value)
	}
	hash := expectedBundleHash(t, tc, meta["hash"].(string))
	equal(t, actual["hash"], hash)
}

func assertExpectedWriteCounts(t *testing.T, tc goldenCase, report any, home, workspace string) {
	expected, ok := tc.Expected["writeCountByTargetDir"]
	if !ok {
		return
	}
	actual := map[string]any{}
	reportMap := normalize(report).(map[string]any)
	for _, key := range []string{"installed", "updated"} {
		for _, item := range reportMap[key].([]any) {
			targetDir := item.(map[string]any)["targetDir"].(string)
			if actual[targetDir] == nil {
				actual[targetDir] = float64(0)
			}
			actual[targetDir] = actual[targetDir].(float64) + 1
		}
	}
	equal(t, actual, expandValue(expected, home, workspace))
}

func writeMetadataFixture(t *testing.T, tc goldenCase, home, workspace string, meta map[string]any) {
	hash := expectedBundleHash(t, tc, meta["hash"].(string))
	fields := map[string]any{}
	for key, value := range meta["fields"].(map[string]any) {
		fields[key] = value
	}
	fields["hash"] = hash
	writeFixtureFile(t, expandString(meta["path"].(string), home, workspace), fields)
}

func expectedBundleHash(t *testing.T, tc goldenCase, marker string) string {
	switch marker {
	case "from-skill-bundle-dir":
		hash, err := ComputeBundleContentHash(DirectoryBundle(caseSkillBundleDir(tc)))
		must(t, err)
		return hash
	case "from-skill-files":
		hash, err := ComputeBundleContentHash(FilesBundle(skillFiles(tc.Options["skillFiles"].([]any))))
		must(t, err)
		return hash
	case "from-github-bundle":
		hash, err := ComputeBundleContentHash(FilesBundle(githubSkillFiles(tc)))
		must(t, err)
		return hash
	default:
		return marker
	}
}

func assertSelection(t *testing.T, actual, expected map[string]any) {
	if expectedCount, ok := expected["selectedCount"]; ok {
		equal(t, float64(len(actual["selectedHostIds"].([]any))), expectedCount)
		delete(actual, "selectedHostIds")
	}
	if expectedCount, ok := expected["candidateCount"]; ok {
		equal(t, float64(len(actual["candidateHostIds"].([]any))), expectedCount)
		delete(actual, "candidateHostIds")
	}
	delete(expected, "selectedCount")
	delete(expected, "candidateCount")
	equal(t, actual, expected)
}

func assertWorkflow(t *testing.T, actual map[string]any, expected any) {
	expectedMap, ok := expected.(map[string]any)
	if !ok {
		return
	}
	for key, value := range expectedMap {
		equal(t, actual[key], value)
	}
}

func assertParsedFlags(t *testing.T, actual ParsedInstallFlags, expected map[string]any) {
	agentKind := actual.Agents.Kind
	agentIDs := actual.Agents.IDs
	if agentIDs == nil {
		agentIDs = []string{}
	}
	equal(t, map[string]any{
		"scope":     string(actual.Scope),
		"scopeSet":  actual.ScopeSet,
		"agentKind": agentKind,
		"agentIds":  agentIDs,
		"yes":       actual.Yes,
		"dryRun":    actual.DryRun,
		"force":     actual.Force,
		"errors":    actual.Errors,
	}, expected)
}

func assertOutputContains(t *testing.T, actual string, expected any) {
	for _, value := range stringSlice(expected) {
		if !strings.Contains(actual, value) {
			t.Fatalf("expected output to contain %q, got:\n%s", value, actual)
		}
	}
}

func assertOutput(t *testing.T, actual string, expected any) {
	if expected == nil {
		return
	}
	equal(t, actual, expected)
}

func writeFixtureFile(t *testing.T, path string, value any) {
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	switch value := value.(type) {
	case string:
		must(t, os.WriteFile(path, []byte(value), 0o644))
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		must(t, err)
		must(t, os.WriteFile(path, append(data, '\n'), 0o644))
	}
}

func agentSelector(value any) AgentSelector {
	if value == nil {
		return AutoAgents()
	}
	if text, ok := value.(string); ok {
		if text == "*" {
			return AllAgents()
		}
		return AutoAgents()
	}
	return ExplicitAgents(stringSlice(value)...)
}

func skillBundleFromOptions(opts map[string]any) SkillBundle {
	if files, ok := opts["skillFiles"].([]any); ok {
		return FilesBundle(skillFiles(files))
	}
	if dir, ok := opts["skillBundleDir"].(string); ok {
		return DirectoryBundle(repoPathFromCase(dir))
	}
	if bundle, ok := opts["githubBundle"].(map[string]any); ok {
		return GitHubBundle(GitHubBundleOptions{
			Owner: bundle["owner"].(string),
			Repo:  bundle["repo"].(string),
			Path:  bundle["path"].(string),
			Ref:   bundle["ref"].(string),
		})
	}
	return SkillBundle{}
}

func skillFiles(values []any) []SkillFile {
	files := make([]SkillFile, 0, len(values))
	for _, value := range values {
		item := value.(map[string]any)
		files = append(files, SkillFile{
			Path:     item["path"].(string),
			Contents: []byte(item["contents"].(string)),
		})
	}
	return files
}

func githubSkillFiles(tc goldenCase) []SkillFile {
	bundle := tc.Options["githubBundle"].(map[string]any)
	root := strings.Trim(bundle["path"].(string), "/") + "/"
	github := tc.Given["github"].(map[string]any)
	rawFiles := github["files"].(map[string]any)
	files := []SkillFile{}
	for path, contents := range rawFiles {
		if strings.HasPrefix(path, root) {
			files = append(files, SkillFile{Path: strings.TrimPrefix(path, root), Contents: []byte(contents.(string))})
		}
	}
	return files
}

func startGitHubFixture(t *testing.T, github map[string]any) {
	owner := github["owner"].(string)
	repo := github["repo"].(string)
	ref := github["ref"].(string)
	commit := github["commit"].(string)
	treeSha := github["treeSha"].(string)
	files := github["files"].(map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + owner + "/" + repo + "/commits/" + ref:
			writeResponseJSON(t, w, map[string]any{"sha": commit, "commit": map[string]any{"tree": map[string]any{"sha": treeSha}}})
		case "/repos/" + owner + "/" + repo + "/git/trees/" + treeSha:
			tree := []map[string]string{}
			for path := range files {
				mode := "100644"
				if strings.HasSuffix(path, ".sh") {
					mode = "100755"
				}
				tree = append(tree, map[string]string{"path": path, "type": "blob", "mode": mode})
			}
			writeResponseJSON(t, w, map[string]any{"tree": tree})
		default:
			prefix := "/" + owner + "/" + repo + "/" + commit + "/"
			if strings.HasPrefix(r.URL.Path, prefix) {
				path := strings.TrimPrefix(r.URL.Path, prefix)
				if contents, ok := files[path]; ok {
					_, _ = w.Write([]byte(contents.(string)))
					return
				}
			}
			http.NotFound(w, r)
		}
	}))
	t.Setenv("KITUP_GITHUB_API_BASE_URL", server.URL)
	t.Setenv("KITUP_GITHUB_RAW_BASE_URL", server.URL)
	t.Cleanup(server.Close)
}

func writeResponseJSON(t *testing.T, w http.ResponseWriter, value any) {
	w.Header().Set("content-type", "application/json")
	data, err := json.Marshal(value)
	must(t, err)
	_, _ = w.Write(data)
}

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func caseSkillBundleDir(tc goldenCase) string {
	if dir, ok := tc.Options["skillBundleDir"].(string); ok {
		return repoPathFromCase(dir)
	}
	return repoPathFromCase("testdata/skills/" + tc.Options["skillName"].(string))
}

func repoPathFromCase(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join("..", path)
}

func hostIDs(hosts []Host) []string {
	ids := []string{}
	for _, host := range hosts {
		ids = append(ids, host.ID)
	}
	return ids
}

func stringSlice(value any) []string {
	if value == nil {
		return nil
	}
	var out []string
	for _, item := range value.([]any) {
		out = append(out, item.(string))
	}
	return out
}

func expandValue(value any, home, workspace string) any {
	switch value := value.(type) {
	case string:
		return expandString(value, home, workspace)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = expandValue(item, home, workspace)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, item := range value {
			out[expandString(key, home, workspace)] = expandValue(item, home, workspace)
		}
		return out
	default:
		return value
	}
}

func expandString(value, home, workspace string) string {
	value = strings.ReplaceAll(value, "$HOME", home)
	return strings.ReplaceAll(value, "$WORKSPACE", workspace)
}

func equal(t *testing.T, got, want any) {
	t.Helper()
	got = normalize(got)
	want = normalize(want)
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("got:\n%s\nwant:\n%s", g, w)
	}
}

func normalize(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

func readJSON(t *testing.T, path string, out any) {
	data, err := os.ReadFile(path)
	must(t, err)
	must(t, json.Unmarshal(data, out))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
