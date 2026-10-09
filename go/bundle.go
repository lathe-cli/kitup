package kitup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"sort"
	"strings"
)

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

func WithBundleMetadata(bundle SkillBundle, meta BundledSkillMetadata) SkillBundle {
	meta.Provenance = maps.Clone(meta.Provenance)
	bundle.meta = meta
	bundle.metaSet = true
	return bundle
}

type bundleMetadata struct {
	Source      string
	SourceID    string
	Version     string
	CLIVersion  string
	CLIRevision string
	Provenance  map[string]string
	Explicit    bool
}

type normalizedSkillBundle struct {
	Files  []SkillFile
	ByPath map[string]SkillFile
}

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func isValidSkillName(skillName string) bool {
	return skillNamePattern.MatchString(skillName)
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
	text := strings.ReplaceAll(string(file.Contents), "\r\n", "\n")
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
	if !isValidSkillName(name) || len(description) < 1 || len(description) > 1024 {
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
	var normalized normalizedSkillBundle
	var meta bundleMetadata
	var err error
	switch bundle.kind {
	case "github":
		normalized, meta, err = resolveGitHubBundle(bundle.github)
	default:
		normalized, err = readSkillBundle(bundle)
		meta = bundleMetadata{Source: "bundled"}
	}
	if err != nil {
		return normalizedSkillBundle{}, bundleMetadata{}, err
	}
	if bundle.meta.SourceID != "" {
		meta.SourceID = bundle.meta.SourceID
	}
	meta.CLIVersion = bundle.meta.CLIVersion
	meta.CLIRevision = bundle.meta.CLIRevision
	if len(bundle.meta.Provenance) > 0 {
		meta.Provenance = maps.Clone(meta.Provenance)
		if meta.Provenance == nil {
			meta.Provenance = map[string]string{}
		}
		maps.Copy(meta.Provenance, bundle.meta.Provenance)
	}
	meta.Explicit = bundle.metaSet
	return normalized, meta, nil
}

func readSkillBundle(bundle SkillBundle) (normalizedSkillBundle, error) {
	var files []SkillFile
	var err error
	switch bundle.kind {
	case "directory":
		files, err = readDirectoryBundleFiles(bundle.dir)
	case "fs":
		files, err = readFSBundleFiles(bundle.fsys, bundle.root)
	case "files":
		files = bundle.files
	case "github":
		normalized, _, err := resolveGitHubBundle(bundle.github)
		return normalized, err
	default:
		return normalizedSkillBundle{}, errors.New("missing skill bundle")
	}
	if err != nil {
		return normalizedSkillBundle{}, err
	}
	return normalizeSkillFiles(files)
}

func readDirectoryBundleFiles(root string) ([]SkillFile, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return nil, err
	}
	return walkBundleFiles(os.DirFS(root), ".", false)
}

func readFSBundleFiles(fsys fs.FS, root string) ([]SkillFile, error) {
	if fsys == nil {
		return nil, errors.New("missing skill fs")
	}
	root = strings.Trim(root, "/")
	if root == "" {
		root = "."
	}
	if !fs.ValidPath(root) {
		return nil, errors.New("invalid skill fs root")
	}
	return walkBundleFiles(fsys, root, true)
}

func walkBundleFiles(fsys fs.FS, root string, embedded bool) ([]SkillFile, error) {
	var files []SkillFile
	err := fs.WalkDir(fsys, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}
		if skipName(entry.Name()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		rel := strings.TrimPrefix(path, root+"/")
		contents, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if embedded && mode == 0o444 {
			mode = defaultBundleFileMode(rel)
		}
		files = append(files, SkillFile{Path: rel, Contents: contents, Mode: mode})
		return nil
	})
	return files, err
}

func normalizeSkillFiles(files []SkillFile) (normalizedSkillBundle, error) {
	byPath := map[string]SkillFile{}
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
			mode = defaultBundleFileMode(normalizedPath)
		}
		byPath[normalizedPath] = SkillFile{Path: normalizedPath, Contents: file.Contents, Mode: mode}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	normalized := normalizedSkillBundle{Files: make([]SkillFile, 0, len(paths)), ByPath: byPath}
	for _, path := range paths {
		normalized.Files = append(normalized.Files, byPath[path])
	}
	return normalized, nil
}

func defaultBundleFileMode(path string) fs.FileMode {
	if strings.HasPrefix(path, "scripts/") {
		return 0o755
	}
	return 0o644
}

func normalizeBundlePath(value string) (string, bool, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
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
	return value, true, nil
}

func skipName(name string) bool {
	return name == ".git" || name == ".kitup.json" || name == ".DS_Store" || strings.HasSuffix(name, ".swp") || strings.HasSuffix(name, "~")
}
