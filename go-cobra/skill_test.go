package kitupcobra

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kitup "github.com/lathe-cli/kitup/go"
)

func TestSkillCommandInstallsWithCoreFlags(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	cmd := NewSkillCommand(Options{
		AppID:  "example-cli",
		Bundle: basicBundle(),
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
		Bundle:   basicBundle(),
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
		Bundle: basicBundle(),
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
		Bundle: basicBundle(),
		Home:   t.TempDir(),
	})
	cmd.SetArgs([]string{"--scope", "bad"})

	err := cmd.Execute()
	if err == nil || err.Error() != kitup.InstallUX.InvalidFlags {
		t.Fatalf("got %v, want %q", err, kitup.InstallUX.InvalidFlags)
	}
}

func TestSkillCommandStatusJSON(t *testing.T) {
	home := t.TempDir()
	installBasic(t, home)
	var out bytes.Buffer
	cmd := NewSkillCommand(Options{
		AppID:     "example-cli",
		SkillName: "basic",
		Bundle:    basicBundle(),
		Home:      home,
		Out:       &out,
	})
	cmd.SetArgs([]string{"status", "--agent", "codex", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report kitup.StatusReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Installed) != 1 || report.Installed[0].Metadata.AppID != "example-cli" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestUninstallCommandRequiresYesWithoutTTY(t *testing.T) {
	home := t.TempDir()
	installBasic(t, home)
	cmd := NewUninstallCommand(Options{
		AppID:     "example-cli",
		SkillName: "basic",
		Home:      home,
		In:        strings.NewReader(""),
	})
	cmd.SetArgs([]string{"--agent", "codex"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-interactive uninstall to require confirmation bypass")
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "basic", ".kitup.json")); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallCommandJSONKeepsPromptOffStdout(t *testing.T) {
	home := t.TempDir()
	installBasic(t, home)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewUninstallCommand(Options{
		AppID:     "example-cli",
		SkillName: "basic",
		Home:      home,
		StdinTTY:  true,
		In:        strings.NewReader("y\n"),
		Out:       &out,
		Err:       &stderr,
	})
	cmd.SetArgs([]string{"--agent", "codex", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report kitup.UninstallReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Removed) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if strings.Contains(out.String(), "Remove ") || !strings.Contains(stderr.String(), "Remove 1 installed target") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "basic")); !os.IsNotExist(err) {
		t.Fatalf("expected target removed, got %v", err)
	}
}

func installBasic(t *testing.T, home string) {
	t.Helper()
	report, err := kitup.InstallBundledSkill(kitup.InstallOptions{
		BaseOptions: kitup.BaseOptions{Home: home},
		AppID:       "example-cli",
		SkillBundle: basicBundle(),
		Scope:       kitup.UserScope,
		Agents:      kitup.ExplicitAgents("codex"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Installed) != 1 {
		t.Fatalf("unexpected install report: %+v", report)
	}
}

func basicBundle() kitup.SkillBundle {
	return kitup.FilesBundle([]kitup.SkillFile{{
		Path:     "SKILL.md",
		Contents: []byte("---\nname: basic\ndescription: Basic fixture.\n---\n"),
	}})
}
