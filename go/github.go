package kitup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func resolveGitHubBundle(opts GitHubBundleOptions) (normalizedSkillBundle, bundleMetadata, error) {
	root := strings.Trim(opts.Path, "/")
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
	if err := getJSON(apiBase+"/repos/"+url.PathEscape(opts.Owner)+"/"+url.PathEscape(opts.Repo)+"/commits/"+url.PathEscape(opts.Ref), &commit); err != nil {
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
	if err := getJSON(apiBase+"/repos/"+url.PathEscape(opts.Owner)+"/"+url.PathEscape(opts.Repo)+"/git/trees/"+url.PathEscape(commit.Commit.Tree.Sha)+"?recursive=1", &tree); err != nil {
		return normalizedSkillBundle{}, bundleMetadata{}, err
	}
	prefix := root + "/"
	files := []SkillFile{}
	for _, item := range tree.Tree {
		if item.Type != "blob" || !strings.HasPrefix(item.Path, prefix) {
			continue
		}
		contents, err := getBytes(rawBase + "/" + url.PathEscape(opts.Owner) + "/" + url.PathEscape(opts.Repo) + "/" + url.PathEscape(commit.Sha) + "/" + escapePath(item.Path))
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

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
