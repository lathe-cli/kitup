package kitup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestDetectionPathExistsRequiresSuccessfulStat(t *testing.T) {
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	target := filepath.Join(locked, "marker")
	if _, err := os.Stat(target); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Skip("filesystem does not expose an inaccessible path as a stat error")
	}
	if detectionPathExists(target) {
		t.Fatal("inaccessible detection path must not count as existing")
	}
}

func TestFSBundleTreatsEmbedReadOnlyModesAsDefaults(t *testing.T) {
	files, err := readFSBundleFiles(fstest.MapFS{
		"skills/basic/SKILL.md": {
			Data: []byte("---\nname: basic\ndescription: Basic skill.\n---\n"),
			Mode: 0o444,
		},
		"skills/basic/scripts/helper.sh": {
			Data: []byte("#!/usr/bin/env sh\n"),
			Mode: 0o444,
		},
	}, "skills/basic")
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]os.FileMode{}
	for _, file := range files {
		modes[file.Path] = file.Mode.Perm()
	}
	if got, want := modes["SKILL.md"], os.FileMode(0o644); got != want {
		t.Fatalf("SKILL.md mode: got %o want %o", got, want)
	}
	if got, want := modes["scripts/helper.sh"], os.FileMode(0o755); got != want {
		t.Fatalf("helper.sh mode: got %o want %o", got, want)
	}
}
