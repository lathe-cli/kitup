package kitup

import (
	"io"
	"io/fs"
)

type Scope string

const (
	UserScope    Scope = "user"
	ProjectScope Scope = "project"
)

type AgentSelector struct {
	Kind string
	IDs  []string
}

type InstallFlagValues struct {
	Scope    string
	ScopeSet bool
	Agents   []string
	Yes      bool
	DryRun   bool
	Force    bool
}

type ParsedInstallFlags struct {
	Scope    Scope
	ScopeSet bool
	Agents   AgentSelector
	Yes      bool
	DryRun   bool
	Force    bool
	Errors   []map[string]any
}

type InstallWorkflowExit struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Host struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"displayName"`
	Aliases          []string `json:"aliases,omitempty"`
	ProjectSkillsDir []string `json:"projectSkillsDirs"`
	UserSkillsDir    []string `json:"userSkillsDirs"`
	Detect           []string `json:"detect"`
	Status           string   `json:"status"`
	Notes            []string `json:"notes,omitempty"`
}

type BaseOptions struct {
	Home      string
	CWD       string
	HostsFile string
}

type InstallOptions struct {
	BaseOptions
	AppID       string
	SkillBundle SkillBundle
	Scope       Scope
	Agents      AgentSelector
	Force       bool
}

type UninstallOptions struct {
	BaseOptions
	AppID     string
	SkillName string
	Scope     Scope
	Agents    AgentSelector
}

type StatusOptions UninstallOptions

type InstallSelectionOptions struct {
	BaseOptions
	Scope        Scope
	Agents       AgentSelector
	Yes          bool
	StdinTTY     bool
	CurrentAgent string
}

type InstallWorkflowOptions struct {
	InstallOptions
	Yes          bool
	DryRun       bool
	StdinTTY     bool
	CurrentAgent string
	DefaultScope Scope
	ScopeSet     bool
	PromptScope  bool
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
}

type SkillInfo struct {
	Valid       bool   `json:"valid"`
	SkillName   string `json:"skillName,omitempty"`
	Description string `json:"description,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
}

type SkillFile struct {
	Path     string
	Contents []byte
	Mode     fs.FileMode
}

type SkillBundle struct {
	kind    string
	dir     string
	fsys    fs.FS
	root    string
	files   []SkillFile
	github  GitHubBundleOptions
	meta    BundledSkillMetadata
	metaSet bool
}

type BundledSkillMetadata struct {
	SourceID    string
	CLIVersion  string
	CLIRevision string
	Provenance  map[string]string
}

type GitHubBundleOptions struct {
	Owner string
	Repo  string
	Path  string
	Ref   string
}

type TargetGroup struct {
	HostIDs   []string
	SkillName string
	TargetDir string
}

type TargetResult struct {
	HostID    string   `json:"hostId,omitempty"`
	HostIDs   []string `json:"hostIds,omitempty"`
	SkillName string   `json:"skillName"`
	TargetDir string   `json:"targetDir"`
}

type TargetStatus struct {
	TargetResult
	Reason string `json:"reason"`
}

type ReportError struct {
	Agent     string `json:"agent,omitempty"`
	Flag      string `json:"flag,omitempty"`
	HostID    string `json:"hostId,omitempty"`
	Reason    string `json:"reason"`
	Scope     Scope  `json:"scope,omitempty"`
	SkillName string `json:"skillName,omitempty"`
	Value     string `json:"value,omitempty"`
}

type InstallReport struct {
	Installed []TargetResult `json:"installed"`
	Updated   []TargetResult `json:"updated"`
	Skipped   []TargetStatus `json:"skipped"`
	Conflicts []TargetStatus `json:"conflicts"`
	Errors    []ReportError  `json:"errors"`
}

type UninstallReport struct {
	Removed   []TargetResult `json:"removed"`
	Skipped   []TargetStatus `json:"skipped"`
	Conflicts []TargetStatus `json:"conflicts"`
	Errors    []ReportError  `json:"errors"`
}

type InstalledMetadata struct {
	SchemaVersion int               `json:"schemaVersion"`
	AppID         string            `json:"appId"`
	SkillName     string            `json:"skillName"`
	Source        string            `json:"source"`
	Hash          string            `json:"hash"`
	SourceID      string            `json:"sourceId,omitempty"`
	Version       string            `json:"version,omitempty"`
	CLIVersion    string            `json:"cliVersion,omitempty"`
	CLIRevision   string            `json:"cliRevision,omitempty"`
	Provenance    map[string]string `json:"provenance,omitempty"`
}

type InstalledTarget struct {
	TargetResult
	Metadata InstalledMetadata `json:"metadata"`
}

type StatusReport struct {
	Installed []InstalledTarget `json:"installed"`
	Missing   []TargetResult    `json:"missing"`
	Conflicts []TargetStatus    `json:"conflicts"`
	Errors    []ReportError     `json:"errors"`
}

type InstallSelection struct {
	Action            string           `json:"action"`
	SelectedHostIDs   []string         `json:"selectedHostIds"`
	CandidateHostIDs  []string         `json:"candidateHostIds"`
	DetectedHostIDs   []string         `json:"detectedHostIds"`
	NeedsConfirmation bool             `json:"needsConfirmation"`
	Errors            []map[string]any `json:"errors"`
}

type InstallWorkflowReport struct {
	Selection InstallSelection `json:"selection"`
	Scope     Scope            `json:"scope"`
	Plan      InstallReport    `json:"plan"`
	Report    InstallReport    `json:"report"`
	Canceled  bool             `json:"canceled"`
	DryRun    bool             `json:"dryRun"`
}
