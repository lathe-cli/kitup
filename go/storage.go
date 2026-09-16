package kitup

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
)

type metadata = InstalledMetadata

func inspectTarget(targetDir, appID, skillName string) (InstalledMetadata, string) {
	meta, present, managed := readMetadata(targetDir)
	switch {
	case !present:
		return meta, "missing"
	case !managed || meta.SkillName != skillName:
		return meta, "unmanaged"
	case meta.AppID != appID:
		return meta, "owner-mismatch"
	default:
		return meta, ""
	}
}

func ReadInstalledMetadata(targetDir string) (InstalledMetadata, bool, error) {
	meta, present, managed := readMetadata(targetDir)
	if !present {
		return InstalledMetadata{}, false, nil
	}
	if !managed {
		return InstalledMetadata{}, false, errors.New("unmanaged install metadata")
	}
	return meta, true, nil
}

func writeManagedSkill(bundle normalizedSkillBundle, targetDir, appID, skillName, hash string, bundleMeta bundleMetadata, replace bool) error {
	tmp, err := makeStagingDir(targetDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copySkillBundle(bundle, tmp); err != nil {
		return err
	}
	if err := writeMetadata(tmp, appID, skillName, hash, bundleMeta); err != nil {
		return err
	}
	backup := tmp + "-backup"
	if replace {
		if err := os.Rename(targetDir, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, targetDir); err != nil {
		if replace && !exists(targetDir) && exists(backup) {
			_ = os.Rename(backup, targetDir)
		}
		return err
	}
	if replace {
		return os.RemoveAll(backup)
	}
	return nil
}

func makeStagingDir(targetDir string) (string, error) {
	parent := filepath.Dir(targetDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(targetDir)+".kitup-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func removeManagedSkill(targetDir, appID, skillName string) (string, error) {
	quarantine, err := makeStagingDir(targetDir)
	if err != nil {
		return "", err
	}
	if err := os.Remove(quarantine); err != nil {
		return "", err
	}
	if err := os.Rename(targetDir, quarantine); err != nil {
		return "", err
	}
	_, reason := inspectTarget(quarantine, appID, skillName)
	if reason == "missing" {
		reason = "unmanaged"
	}
	if reason != "" {
		if exists(targetDir) {
			return "", fmt.Errorf("cannot restore changed install; preserved at %s", quarantine)
		}
		if err := os.Rename(quarantine, targetDir); err != nil {
			return "", err
		}
		return reason, nil
	}
	return "", os.RemoveAll(quarantine)
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
		if err := os.WriteFile(to, file.Contents, mode); err != nil {
			return err
		}
		if err := os.Chmod(to, mode); err != nil {
			return err
		}
	}
	return nil
}

func repairSkillBundleModes(bundle normalizedSkillBundle, dest string, write bool) (bool, error) {
	repaired := false
	for _, file := range bundle.Files {
		to := filepath.Join(dest, filepath.FromSlash(file.Path))
		info, err := os.Lstat(to)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, err
		}
		if info.Mode().IsRegular() && info.Mode().Perm() != file.Mode.Perm() {
			repaired = true
			if write {
				if err := os.Chmod(to, file.Mode.Perm()); err != nil {
					return false, err
				}
			}
		}
	}
	return repaired, nil
}

func writeMetadata(targetDir, appID, skillName, hash string, bundleMeta bundleMetadata) error {
	meta := newInstalledMetadata(appID, skillName, hash, bundleMeta)
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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return metadata{}, true, false
	}
	var meta metadata
	fields := map[string]any{
		"schemaVersion": &meta.SchemaVersion,
		"appId":         &meta.AppID,
		"skillName":     &meta.SkillName,
		"source":        &meta.Source,
		"hash":          &meta.Hash,
		"sourceId":      &meta.SourceID,
		"version":       &meta.Version,
		"cliVersion":    &meta.CLIVersion,
		"cliRevision":   &meta.CLIRevision,
		"provenance":    &meta.Provenance,
	}
	for name, destination := range fields {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if err := json.Unmarshal(value, destination); err != nil {
			return metadata{}, true, false
		}
	}
	if !isOwnedMetadata(meta) {
		return metadata{}, true, false
	}
	return meta, true, true
}

func newInstalledMetadata(appID, skillName, hash string, bundleMeta bundleMetadata) InstalledMetadata {
	source := bundleMeta.Source
	if source == "" {
		source = "bundled"
	}
	return InstalledMetadata{
		SchemaVersion: 1,
		AppID:         appID,
		SkillName:     skillName,
		Source:        source,
		Hash:          hash,
		SourceID:      bundleMeta.SourceID,
		Version:       bundleMeta.Version,
		CLIVersion:    bundleMeta.CLIVersion,
		CLIRevision:   bundleMeta.CLIRevision,
		Provenance:    maps.Clone(bundleMeta.Provenance),
	}
}

func installedMetadataEqual(left, right InstalledMetadata) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.AppID == right.AppID &&
		left.SkillName == right.SkillName &&
		left.Source == right.Source &&
		left.Hash == right.Hash &&
		left.SourceID == right.SourceID &&
		left.Version == right.Version &&
		left.CLIVersion == right.CLIVersion &&
		left.CLIRevision == right.CLIRevision &&
		maps.Equal(left.Provenance, right.Provenance)
}

func isOwnedMetadata(meta metadata) bool {
	return meta.SchemaVersion == 1 &&
		meta.AppID != "" &&
		isValidSkillName(meta.SkillName) &&
		(meta.Source == "bundled" || meta.Source == "github") &&
		meta.Hash != ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
