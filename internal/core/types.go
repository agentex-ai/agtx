package core

import "time"

type Registry struct {
	SchemaVersion int             `json:"schema_version"`
	Skills        []SkillManifest `json:"skills"`
}

type SkillManifest struct {
	SchemaVersion int              `json:"schema_version"`
	Name          string           `json:"name"`
	Version       string           `json:"version"`
	Summary       string           `json:"summary"`
	Description   string           `json:"description"`
	Tags          []string         `json:"tags,omitempty"`
	Keywords      []string         `json:"keywords,omitempty"`
	Permissions   []Permission     `json:"permissions,omitempty"`
	Platforms     []PlatformBundle `json:"platforms,omitempty"`
	InputSchema   map[string]any   `json:"input_schema,omitempty"`
	OutputSchema  map[string]any   `json:"output_schema,omitempty"`
	Signature     *SignatureInfo   `json:"signature,omitempty"`
	Stub          bool             `json:"stub"`
}

type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type PlatformBundle struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	URL        string `json:"url,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Archive    string `json:"archive,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
}

type SignatureInfo struct {
	Algorithm string `json:"algorithm,omitempty"`
	KeyID     string `json:"key_id,omitempty"`
	Value     string `json:"value,omitempty"`
}

type SearchResult struct {
	Skill   SkillManifest `json:"skill"`
	Score   int           `json:"score"`
	Matched []string      `json:"matched,omitempty"`
}

type InstallResult struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	Status          string `json:"status"`
	Path            string `json:"path"`
	PreviousVersion string `json:"previous_version,omitempty"`
	Stub            bool   `json:"stub"`
}

type MutationPlan struct {
	Action  string          `json:"action"`
	Changes []PlannedChange `json:"changes"`
}

type PlannedChange struct {
	Name           string   `json:"name"`
	CurrentVersion string   `json:"current_version,omitempty"`
	TargetVersion  string   `json:"target_version,omitempty"`
	Status         string   `json:"status"`
	Stub           bool     `json:"stub"`
	Permissions    []string `json:"permissions,omitempty"`
	Path           string   `json:"path,omitempty"`
}

type RollbackResult struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	PreviousVersion string `json:"previous_version"`
	Path            string `json:"path"`
}

type UninstallResult struct {
	Name            string   `json:"name"`
	RemovedVersions []string `json:"removed_versions"`
	Status          string   `json:"status"`
}

type InstalledSkill struct {
	Name     string        `json:"name"`
	Version  string        `json:"version"`
	Path     string        `json:"path"`
	Current  bool          `json:"current"`
	Manifest SkillManifest `json:"manifest"`
}

type RunResult struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Stub             bool   `json:"stub"`
	ExitCode         int    `json:"exit_code"`
	Stdout           string `json:"stdout,omitempty"`
	Stderr           string `json:"stderr,omitempty"`
	StdoutTruncated  bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated  bool   `json:"stderr_truncated,omitempty"`
	DurationMS       int64  `json:"duration_ms"`
	TimedOut         bool   `json:"timed_out,omitempty"`
	OutputLimitBytes int64  `json:"output_limit_bytes,omitempty"`
	TimeoutMS        int64  `json:"timeout_ms,omitempty"`
}

type RunOptions struct {
	Args             []string
	Input            []byte
	Timeout          time.Duration
	OutputLimitBytes int64
}

type Status struct {
	Version         string           `json:"version"`
	GOOS            string           `json:"goos"`
	GOARCH          string           `json:"goarch"`
	ConfigDir       string           `json:"config_dir"`
	ConfigFile      string           `json:"config_file"`
	CacheDir        string           `json:"cache_dir"`
	SkillsDir       string           `json:"skills_dir"`
	LogsDir         string           `json:"logs_dir"`
	RegistrySkills  int              `json:"registry_skills"`
	RegistrySources []RegistrySource `json:"registry_sources,omitempty"`
	Installed       int              `json:"installed"`
	DependencyMode  string           `json:"dependency_mode"`
	Channel         string           `json:"channel"`
	Telemetry       string           `json:"telemetry"`
}

type DiagnosticSummary struct {
	Checks   int `json:"checks"`
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

type DoctorCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Details  any    `json:"details,omitempty"`
}

type DoctorResult struct {
	OK      bool              `json:"ok"`
	Summary DiagnosticSummary `json:"summary"`
	Checks  []DoctorCheck     `json:"checks"`
}

type VerifyResult struct {
	OK                bool              `json:"ok"`
	Name              string            `json:"name"`
	Version           string            `json:"version,omitempty"`
	Path              string            `json:"path,omitempty"`
	Stub              bool              `json:"stub,omitempty"`
	InstalledVersions []string          `json:"installed_versions,omitempty"`
	Summary           DiagnosticSummary `json:"summary"`
	Checks            []DoctorCheck     `json:"checks"`
}

type ListOptions struct {
	Installed bool
	Available bool
}

type ListResult struct {
	Installed []InstalledSkill `json:"installed,omitempty"`
	Available []SkillManifest  `json:"available,omitempty"`
}
