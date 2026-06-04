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
	VendorID      string           `json:"vendor_id,omitempty"`
	Capability    *CapabilityInfo  `json:"capability,omitempty"`
	Summary       string           `json:"summary"`
	Description   string           `json:"description"`
	Tags          []string         `json:"tags,omitempty"`
	Keywords      []string         `json:"keywords,omitempty"`
	Permissions   []Permission     `json:"permissions,omitempty"`
	Platforms     []PlatformBundle `json:"platforms,omitempty"`
	InputSchema   map[string]any   `json:"input_schema,omitempty"`
	OutputSchema  map[string]any   `json:"output_schema,omitempty"`
	Billing       *BillingInfo     `json:"billing,omitempty"`
	Attribution   *AttributionInfo `json:"attribution,omitempty"`
	Support       *SupportInfo     `json:"support,omitempty"`
	Signature     *SignatureInfo   `json:"signature,omitempty"`
	Stub          bool             `json:"stub"`
}

type CapabilityInfo struct {
	Class   string `json:"class,omitempty"`
	UseWhen string `json:"use_when,omitempty"`
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

type BillingInfo struct {
	Meters       []BillingMeter `json:"meters,omitempty"`
	RevenueShare *RevenueShare  `json:"revenue_share,omitempty"`
}

type BillingMeter struct {
	Meter              string  `json:"meter"`
	UnitPrice          float64 `json:"unit_price,omitempty"`
	Currency           string  `json:"currency,omitempty"`
	FreeQuota          float64 `json:"free_quota,omitempty"`
	HardLimitSupported bool    `json:"hard_limit_supported,omitempty"`
	RefundPolicy       string  `json:"refund_policy,omitempty"`
}

type RevenueShare struct {
	ISV      float64 `json:"isv,omitempty"`
	Platform float64 `json:"platform,omitempty"`
	Basis    string  `json:"basis,omitempty"`
}

type AttributionInfo struct {
	Events            []string       `json:"events,omitempty"`
	DefaultWindowDays map[string]int `json:"default_window_days,omitempty"`
	DefaultCPSRate    float64        `json:"default_cps_rate,omitempty"`
	RenewalCPS        string         `json:"renewal_cps,omitempty"`
}

type SupportInfo struct {
	URL           string `json:"url,omitempty"`
	PrivacyURL    string `json:"privacy_url,omitempty"`
	IncidentEmail string `json:"incident_email,omitempty"`
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
	Name           string           `json:"name"`
	CurrentVersion string           `json:"current_version,omitempty"`
	TargetVersion  string           `json:"target_version,omitempty"`
	Status         string           `json:"status"`
	Stub           bool             `json:"stub"`
	Permissions    []string         `json:"permissions,omitempty"`
	Commerce       *CommerceSummary `json:"commerce,omitempty"`
	Path           string           `json:"path,omitempty"`
}

type CommerceSummary struct {
	VendorID          string   `json:"vendor_id,omitempty"`
	CapabilityClass   string   `json:"capability_class,omitempty"`
	BillingMeters     []string `json:"billing_meters,omitempty"`
	AttributionEvents []string `json:"attribution_events,omitempty"`
	SupportURL        string   `json:"support_url,omitempty"`
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

type CapabilityPack struct {
	SchemaVersion int          `json:"schema_version"`
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Tier          string       `json:"tier"`
	Summary       string       `json:"summary"`
	Description   string       `json:"description"`
	SkillNames    []string     `json:"skill_names"`
	Billing       *BillingInfo `json:"billing,omitempty"`
	Support       *SupportInfo `json:"support,omitempty"`
}

type CapabilityPackView struct {
	Pack        CapabilityPack        `json:"pack"`
	Installed   bool                  `json:"installed"`
	InstalledAt string                `json:"installed_at,omitempty"`
	UpdatedAt   string                `json:"updated_at,omitempty"`
	Skills      []CapabilityPackSkill `json:"skills"`
}

type CapabilityPackSkill struct {
	Name             string         `json:"name"`
	AvailableVersion string         `json:"available_version,omitempty"`
	InstalledVersion string         `json:"installed_version,omitempty"`
	Installed        bool           `json:"installed"`
	Stub             bool           `json:"stub,omitempty"`
	Path             string         `json:"path,omitempty"`
	Manifest         *SkillManifest `json:"manifest,omitempty"`
}

type CapabilityPackInstallResult struct {
	Pack           CapabilityPackView `json:"pack"`
	Results        []InstallResult    `json:"results"`
	InstallRecord  *InstallRecord     `json:"install_record,omitempty"`
	BillingRecords []BillingRecord    `json:"billing_records,omitempty"`
}

type CapabilityPackInstallPlan struct {
	Action         string             `json:"action"`
	Pack           CapabilityPackView `json:"pack"`
	Changes        []PlannedChange    `json:"changes"`
	BillingPreview []BillingRecord    `json:"billing_preview,omitempty"`
	Totals         []BillingTotal     `json:"totals,omitempty"`
	Requires       []string           `json:"requires,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
}

type RecordQueryOptions struct {
	PackID   string
	Skill    string
	Status   string
	Type     string
	Currency string
	From     string
	To       string
	Limit    int
}

type InstallRecord struct {
	RecordID   string               `json:"record_id"`
	Action     string               `json:"action"`
	PackID     string               `json:"pack_id,omitempty"`
	PackTier   string               `json:"pack_tier,omitempty"`
	SkillName  string               `json:"skill_name,omitempty"`
	Skills     []InstallRecordSkill `json:"skills,omitempty"`
	Status     string               `json:"status"`
	DeviceID   string               `json:"device_id,omitempty"`
	OccurredAt string               `json:"occurred_at"`
}

type InstallRecordSkill struct {
	Name            string `json:"name"`
	Version         string `json:"version,omitempty"`
	PreviousVersion string `json:"previous_version,omitempty"`
	Status          string `json:"status"`
	Path            string `json:"path,omitempty"`
	Stub            bool   `json:"stub,omitempty"`
}

type BillingRecord struct {
	RecordID         string  `json:"record_id"`
	Type             string  `json:"type"`
	PackID           string  `json:"pack_id,omitempty"`
	PackTier         string  `json:"pack_tier,omitempty"`
	SkillName        string  `json:"skill_name,omitempty"`
	VersionID        string  `json:"version_id,omitempty"`
	VendorID         string  `json:"vendor_id,omitempty"`
	Meter            string  `json:"meter"`
	Quantity         float64 `json:"quantity"`
	Currency         string  `json:"currency,omitempty"`
	UnitPriceMinor   int64   `json:"unit_price_minor,omitempty"`
	GrossAmountMinor int64   `json:"gross_amount_minor,omitempty"`
	Status           string  `json:"status"`
	InvocationID     string  `json:"invocation_id,omitempty"`
	UsageEventID     string  `json:"usage_event_id,omitempty"`
	Error            string  `json:"error,omitempty"`
	OccurredAt       string  `json:"occurred_at"`
}

type BillingRecordListResult struct {
	Records []BillingRecord `json:"records"`
	Totals  []BillingTotal  `json:"totals,omitempty"`
}

type BillingTotal struct {
	Currency         string `json:"currency"`
	Records          int    `json:"records"`
	GrossAmountMinor int64  `json:"gross_amount_minor"`
}

type CapabilityCommerceSnapshot struct {
	SchemaVersion  int                     `json:"schema_version"`
	GeneratedAt    string                  `json:"generated_at"`
	Packs          []CapabilityPackView    `json:"packs"`
	InstallRecords []InstallRecord         `json:"install_records,omitempty"`
	Billing        BillingRecordListResult `json:"billing"`
}

type CommerceSnapshotExportResult struct {
	Path     string                     `json:"path"`
	Snapshot CapabilityCommerceSnapshot `json:"snapshot"`
}

type RunResult struct {
	Name             string             `json:"name"`
	Version          string             `json:"version"`
	Stub             bool               `json:"stub"`
	InvocationID     string             `json:"invocation_id,omitempty"`
	ExitCode         int                `json:"exit_code"`
	Stdout           string             `json:"stdout,omitempty"`
	Stderr           string             `json:"stderr,omitempty"`
	StdoutTruncated  bool               `json:"stdout_truncated,omitempty"`
	StderrTruncated  bool               `json:"stderr_truncated,omitempty"`
	DurationMS       int64              `json:"duration_ms"`
	TimedOut         bool               `json:"timed_out,omitempty"`
	OutputLimitBytes int64              `json:"output_limit_bytes,omitempty"`
	TimeoutMS        int64              `json:"timeout_ms,omitempty"`
	UsageEvents      []UsageEventResult `json:"usage_events,omitempty"`
}

type RunOptions struct {
	Args             []string
	Input            []byte
	Timeout          time.Duration
	OutputLimitBytes int64
}

type UsageEventResult struct {
	EventID          string  `json:"event_id"`
	PackID           string  `json:"pack_id"`
	VersionID        string  `json:"version_id,omitempty"`
	VendorID         string  `json:"vendor_id,omitempty"`
	Meter            string  `json:"meter"`
	Quantity         float64 `json:"quantity"`
	Currency         string  `json:"currency,omitempty"`
	UnitPriceMinor   int64   `json:"unit_price_minor,omitempty"`
	GrossAmountMinor int64   `json:"gross_amount_minor,omitempty"`
	Status           string  `json:"status"`
	Error            string  `json:"error,omitempty"`
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

type ConfigKeyInfo struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Default     any      `json:"default,omitempty"`
	Description string   `json:"description"`
	Allowed     []string `json:"allowed,omitempty"`
	Mutable     bool     `json:"mutable"`
}

type ProLoginStartResult struct {
	LoginURL    string `json:"login_url"`
	State       string `json:"state,omitempty"`
	DeviceID    string `json:"device_id,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	AuthPath    string `json:"auth_path,omitempty"`
}

type ProCallbackResult struct {
	Authenticated bool   `json:"authenticated"`
	DeviceID      string `json:"device_id,omitempty"`
	DeviceName    string `json:"device_name,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	RegistryURL   string `json:"registry_url,omitempty"`
	ProAPIURL     string `json:"pro_api_url,omitempty"`
	DeviceLimit   int    `json:"device_limit,omitempty"`
	Subscription  string `json:"subscription,omitempty"`
	AuthPath      string `json:"auth_path,omitempty"`
}

type ProStatusResult struct {
	Authenticated bool        `json:"authenticated"`
	Subscription  string      `json:"subscription,omitempty"`
	Plan          string      `json:"plan,omitempty"`
	DeviceID      string      `json:"device_id,omitempty"`
	DeviceName    string      `json:"device_name,omitempty"`
	ExpiresAt     string      `json:"expires_at,omitempty"`
	DeviceLimit   int         `json:"device_limit,omitempty"`
	AuthPath      string      `json:"auth_path,omitempty"`
	Devices       []ProDevice `json:"devices,omitempty"`
}

type ProDevice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Current     bool   `json:"current,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
	ActivatedAt string `json:"activated_at,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
}

type ProLogoutResult struct {
	LoggedOut bool   `json:"logged_out"`
	AuthPath  string `json:"auth_path"`
}

type ProSetupResult struct {
	Authenticated      bool             `json:"authenticated"`
	HasPendingLogin    bool             `json:"has_pending_login"`
	CallbackScheme     string           `json:"callback_scheme"`
	CallbackURIExample string           `json:"callback_uri_example,omitempty"`
	AuthPath           string           `json:"auth_path"`
	ConfigPath         string           `json:"config_path"`
	ProAPIURL          string           `json:"pro_api_url,omitempty"`
	RegistryURL        string           `json:"registry_url,omitempty"`
	Platform           string           `json:"platform"`
	CanRegisterScheme  bool             `json:"can_register_scheme"`
	SchemeCommandHint  string           `json:"scheme_command_hint,omitempty"`
	RecommendedActions []ProSetupAction `json:"recommended_actions,omitempty"`
	CurrentStatus      []string         `json:"current_status,omitempty"`
}

type ProSetupAction struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary"`
	Blocking    bool           `json:"blocking"`
	Command     string         `json:"command,omitempty"`
	MCPTool     string         `json:"mcp_tool,omitempty"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	AppliesWhen []string       `json:"applies_when,omitempty"`
}
