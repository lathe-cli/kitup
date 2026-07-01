package kitupcobra

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kitup "github.com/samzong/kitup/go"
)

func TestSkillCommandInstallsWithCoreFlags(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	cmd := NewSkillCommand(Options{
		AppID:  "example-cli",
		Bundle: kitup.DirectoryBundle(filepath.Join("..", "testdata", "skills", "basic")),
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
		Bundle:   kitup.DirectoryBundle(filepath.Join("..", "testdata", "skills", "basic")),
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
		Bundle: kitup.DirectoryBundle(filepath.Join("..", "testdata", "skills", "basic")),
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
		Bundle: kitup.DirectoryBundle(filepath.Join("..", "testdata", "skills", "basic")),
		Home:   t.TempDir(),
	})
	cmd.SetArgs([]string{"--scope", "bad"})

	err := cmd.Execute()
	if err == nil || err.Error() != kitup.InstallUX.InvalidFlags {
		t.Fatalf("got %v, want %q", err, kitup.InstallUX.InvalidFlags)
	}
}
