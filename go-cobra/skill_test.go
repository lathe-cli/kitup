package kitupcobra

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	kitup "github.com/lathe-cli/kitup/go"
)

func testBundle() kitup.SkillBundle {
	return kitup.FSBundle(fstest.MapFS{
		"basic/SKILL.md": {
			Data: []byte("---\nname: basic\ndescription: Basic skill.\n---\n"),
		},
	}, "basic")
}

func TestSkillCommandInstallsWithCoreFlags(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	cmd := NewSkillCommand(Options{
		AppID:  "example-cli",
		Bundle: testBundle(),
		Home:   home,
		Out:    &out,
	})
	cmd.SetArgs([]string{"install", "--agent", "codex", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "basic", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallCommandPromptsForScopeBeforeInstall(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	var out bytes.Buffer
	cmd := NewSkillCommand(Options{
		AppID:    "example-cli",
		Bundle:   testBundle(),
		Home:     home,
		CWD:      workspace,
		StdinTTY: true,
		In:       strings.NewReader("project\ny\n"),
		Out:      &out,
	})
	cmd.SetArgs([]string{"install", "--agent", "codex"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), kitup.InstallUX.SelectScope) {
		t.Fatalf("expected scope prompt, got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, ".agents", "skills", "basic", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "basic", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no user-scope install, got %v", err)
	}
}

func TestInstallCommandForceOverwritesUnmanaged(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents", "skills", "basic")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: basic\ndescription: unmanaged\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := NewSkillCommand(Options{
		AppID:  "example-cli",
		Bundle: testBundle(),
		Home:   home,
		Out:    &out,
	})
	cmd.SetArgs([]string{"install", "--agent", "codex", "--yes", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(target, ".kitup.json")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallCommandReturnsCoreFlagError(t *testing.T) {
	cmd := NewInstallCommand(Options{
		AppID:  "example-cli",
		Bundle: testBundle(),
		Home:   t.TempDir(),
	})
	cmd.SetArgs([]string{"--scope", "bad"})

	err := cmd.Execute()
	if err == nil || err.Error() != kitup.InstallUX.InvalidFlags {
		t.Fatalf("got %v, want %q", err, kitup.InstallUX.InvalidFlags)
	}
}

func TestStatusCommandJSONReportsInstalledMetadata(t *testing.T) {
	home := t.TempDir()
	bundle := kitup.WithBundleMetadata(testBundle(), kitup.BundledMetadata{
		CLIVersion: "1.2.3",
		Revision:   "abc123",
		SourceID:   "example-cli:embedded",
		Provenance: map[string]string{"channel": "release"},
	})
	install := NewSkillCommand(Options{AppID: "example-cli", Bundle: bundle, Home: home})
	install.SetArgs([]string{"install", "--agent", "codex", "--yes"})
	if err := install.Execute(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	status := NewSkillCommand(Options{AppID: "example-cli", Bundle: bundle, Home: home, Out: &out})
	status.SetArgs([]string{"status", "--agent", "codex", "--json"})
	if err := status.Execute(); err != nil {
		t.Fatal(err)
	}
	var report kitup.StatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Installed) != 1 {
		t.Fatalf("installed = %+v", report.Installed)
	}
	metadata := report.Installed[0].Metadata
	if metadata.CLIVersion != "1.2.3" || metadata.Revision != "abc123" || metadata.SourceID != "example-cli:embedded" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestUninstallCommandJSONRemovesOwnedSkill(t *testing.T) {
	home := t.TempDir()
	install := NewSkillCommand(Options{AppID: "example-cli", Bundle: testBundle(), Home: home})
	install.SetArgs([]string{"install", "--agent", "codex", "--yes"})
	if err := install.Execute(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	uninstall := NewSkillCommand(Options{AppID: "example-cli", Bundle: testBundle(), Home: home, Out: &out})
	uninstall.SetArgs([]string{"uninstall", "--agent", "codex", "--json"})
	if err := uninstall.Execute(); err != nil {
		t.Fatal(err)
	}
	var report kitup.UninstallReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Removed) != 1 {
		t.Fatalf("removed = %+v", report.Removed)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "basic")); !os.IsNotExist(err) {
		t.Fatalf("expected owned skill removal, got %v", err)
	}
}

func TestLifecycleCommandsReuseCurrentAgentTargetsWithoutSourceBundle(t *testing.T) {
	home := t.TempDir()
	install := NewSkillCommand(Options{
		AppID:        "example-cli",
		Bundle:       testBundle(),
		Home:         home,
		CurrentAgent: "claude-code",
	})
	install.SetArgs([]string{"install", "--yes"})
	if err := install.Execute(); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{
		filepath.Join(home, ".claude", "skills", "basic"),
		filepath.Join(home, ".agents", "skills", "basic"),
	} {
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("expected current-agent install at %s: %v", target, err)
		}
	}

	options := Options{
		AppID:        "example-cli",
		SkillName:    "basic",
		Home:         home,
		CurrentAgent: "claude-code",
	}
	var statusOut bytes.Buffer
	status := NewSkillCommand(options)
	status.SetOut(&statusOut)
	status.SetArgs([]string{"status", "--json"})
	if err := status.Execute(); err != nil {
		t.Fatal(err)
	}
	var statusReport kitup.StatusReport
	if err := json.Unmarshal(statusOut.Bytes(), &statusReport); err != nil {
		t.Fatal(err)
	}
	if len(statusReport.Installed) != 2 {
		t.Fatalf("installed = %+v", statusReport.Installed)
	}

	var uninstallOut bytes.Buffer
	uninstall := NewSkillCommand(options)
	uninstall.SetOut(&uninstallOut)
	uninstall.SetArgs([]string{"uninstall", "--json"})
	if err := uninstall.Execute(); err != nil {
		t.Fatal(err)
	}
	var uninstallReport kitup.UninstallReport
	if err := json.Unmarshal(uninstallOut.Bytes(), &uninstallReport); err != nil {
		t.Fatal(err)
	}
	if len(uninstallReport.Removed) != 2 {
		t.Fatalf("removed = %+v", uninstallReport.Removed)
	}
	for _, target := range []string{
		filepath.Join(home, ".claude", "skills", "basic"),
		filepath.Join(home, ".agents", "skills", "basic"),
	} {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("expected removal at %s, got %v", target, err)
		}
	}
}

func TestLifecycleCurrentAgentSupportsHostSpecWithoutUniversal(t *testing.T) {
	home := t.TempDir()
	hostsFile := filepath.Join(t.TempDir(), "hosts.json")
	hosts := `{
  "schemaVersion": 1,
  "hosts": [{
    "id": "custom-agent",
    "displayName": "Custom Agent",
    "projectSkillsDirs": [".custom/skills"],
    "userSkillsDirs": ["~/.custom/skills"],
    "detect": ["~/.custom"],
    "status": "verified"
  }]
}
`
	if err := os.WriteFile(hostsFile, []byte(hosts), 0o644); err != nil {
		t.Fatal(err)
	}
	install := NewSkillCommand(Options{
		AppID:        "example-cli",
		Bundle:       testBundle(),
		Home:         home,
		HostsFile:    hostsFile,
		CurrentAgent: "custom-agent",
	})
	install.SetArgs([]string{"install", "--yes"})
	if err := install.Execute(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".custom", "skills", "basic")
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}

	uninstall := NewSkillCommand(Options{
		AppID:        "example-cli",
		SkillName:    "basic",
		Home:         home,
		HostsFile:    hostsFile,
		CurrentAgent: "custom-agent",
	})
	uninstall.SetArgs([]string{"uninstall", "--json"})
	if err := uninstall.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected custom target removal, got %v", err)
	}
}

func TestUninstallCommandFailsClosedOnCorruptMetadata(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents", "skills", "basic")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".kitup.json"), []byte("{\"schemaVersion\":1,\"appId\":\"example-cli\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	uninstall := NewSkillCommand(Options{AppID: "example-cli", Bundle: testBundle(), Home: home, Out: &out})
	uninstall.SetArgs([]string{"uninstall", "--agent", "codex", "--json"})
	if err := uninstall.Execute(); err == nil {
		t.Fatal("expected corrupt metadata conflict")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("corrupt target was removed: %v", err)
	}
	var report kitup.UninstallReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Conflicts) != 1 || report.Conflicts[0].Reason != "unmanaged" {
		t.Fatalf("conflicts = %+v", report.Conflicts)
	}
	if NewUninstallCommand(Options{}).Flags().Lookup("force") != nil {
		t.Fatal("uninstall must not expose a force flag")
	}
}
