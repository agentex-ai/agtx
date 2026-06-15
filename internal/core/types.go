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
	Builtin       *BuiltinInfo     `json:"builtin,omitempty"`
	Stub          bool             `json:"stub"`
}

type BuiltinInfo struct {
	Runtime       string   `json:"runtime,omitempty"`
	Backends      []string `json:"backends,omitempty"`
	ModelProfiles []string `json:"model_profiles,omitempty"`
	NoPython      bool     `json:"no_python,omitempty"`
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
	SchemaVersion   int          `json:"schema_version"`
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Tier            string       `json:"tier"`
	CapabilityClass string       `json:"capability_class,omitempty"`
	UseWhen         string       `json:"use_when,omitempty"`
	Summary         string       `json:"summary"`
	Description     string       `json:"description"`
	Inputs          []string     `json:"inputs,omitempty"`
	Outputs         []string     `json:"outputs,omitempty"`
	Tags            []string     `json:"tags,omitempty"`
	SkillNames      []string     `json:"skill_names"`
	Billing         *BillingInfo `json:"billing,omitempty"`
	Support         *SupportInfo `json:"support,omitempty"`
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

type CapabilityScenario struct {
	SchemaVersion      int                       `json:"schema_version"`
	ID                 string                    `json:"id"`
	Name               string                    `json:"name"`
	Summary            string                    `json:"summary"`
	Description        string                    `json:"description"`
	Industry           string                    `json:"industry,omitempty"`
	RecommendedPackID  string                    `json:"recommended_pack_id"`
	TaskProfile        CapabilityTaskProfile     `json:"task_profile"`
	Inputs             []CapabilityScenarioIO    `json:"inputs,omitempty"`
	Deliverables       []CapabilityScenarioIO    `json:"deliverables,omitempty"`
	Workflow           []CapabilityScenarioStep  `json:"workflow,omitempty"`
	Skills             []CapabilityScenarioSkill `json:"skills"`
	AcceptanceCriteria []string                  `json:"acceptance_criteria,omitempty"`
	ExecutionNotes     []string                  `json:"execution_notes,omitempty"`
}

type CapabilityTaskProfile struct {
	Intent            string   `json:"intent"`
	Domains           []string `json:"domains,omitempty"`
	Needs             []string `json:"needs,omitempty"`
	RiskLevel         string   `json:"risk_level,omitempty"`
	RequiresUserInput bool     `json:"requires_user_input"`
}

type CapabilityScenarioSkill struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Priority  string `json:"priority"`
	Stage     string `json:"stage"`
	Reason    string `json:"reason"`
	Condition string `json:"condition,omitempty"`
}

type CapabilityScenarioIO struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Formats     []string `json:"formats,omitempty"`
	Required    bool     `json:"required"`
}

type CapabilityScenarioStep struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Stage       string   `json:"stage"`
	Description string   `json:"description,omitempty"`
	Skills      []string `json:"skills,omitempty"`
}

type CapabilityScenarioView struct {
	Scenario             CapabilityScenario        `json:"scenario"`
	RecommendedPack      CapabilityPackView        `json:"recommended_pack"`
	InstallPlan          CapabilityPackInstallPlan `json:"install_plan"`
	RequiredSkills       []CapabilityScenarioSkill `json:"required_skills,omitempty"`
	MissingSkills        []CapabilityPackSkill     `json:"missing_skills,omitempty"`
	InstalledSkills      []CapabilityPackSkill     `json:"installed_skills,omitempty"`
	Ready                bool                      `json:"ready"`
	BillingPreviewTotals []BillingTotal            `json:"billing_preview_totals,omitempty"`
	Warnings             []string                  `json:"warnings,omitempty"`
}

type CapabilityScenarioInstallPlan struct {
	Action   string                    `json:"action"`
	Scenario CapabilityScenarioView    `json:"scenario"`
	PackPlan CapabilityPackInstallPlan `json:"pack_plan"`
	Requires []string                  `json:"requires,omitempty"`
	Warnings []string                  `json:"warnings,omitempty"`
}

type CapabilityScenarioInstallResult struct {
	Scenario    CapabilityScenarioView      `json:"scenario"`
	PackInstall CapabilityPackInstallResult `json:"pack_install"`
}

type CapabilityScenarioLedger struct {
	SchemaVersion      int                     `json:"schema_version"`
	GeneratedAt        string                  `json:"generated_at"`
	Scenario           CapabilityScenarioView  `json:"scenario"`
	LatestInstall      *InstallRecord          `json:"latest_install,omitempty"`
	InstallRecords     []InstallRecord         `json:"install_records,omitempty"`
	Billing            BillingRecordListResult `json:"billing"`
	UsageRecords       []BillingRecord         `json:"usage_records,omitempty"`
	PackInstallRecords []BillingRecord         `json:"pack_install_records,omitempty"`
}

type RecordQueryOptions struct {
	PackID     string
	ScenarioID string
	Skill      string
	Status     string
	Type       string
	Currency   string
	From       string
	To         string
	Limit      int
}

type InstallRecord struct {
	RecordID   string               `json:"record_id"`
	Action     string               `json:"action"`
	PackID     string               `json:"pack_id,omitempty"`
	PackTier   string               `json:"pack_tier,omitempty"`
	ScenarioID string               `json:"scenario_id,omitempty"`
	SkillName  string               `json:"skill_name,omitempty"`
	Skills     []InstallRecordSkill `json:"skills,omitempty"`
	Status     string               `json:"status"`
	DeviceID   string               `json:"device_id,omitempty"`
	OccurredAt string               `json:"occurred_at"`
	Integrity  *RecordIntegrity     `json:"integrity,omitempty"`
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
	RecordID         string           `json:"record_id"`
	Type             string           `json:"type"`
	PackID           string           `json:"pack_id,omitempty"`
	PackTier         string           `json:"pack_tier,omitempty"`
	ScenarioID       string           `json:"scenario_id,omitempty"`
	SkillName        string           `json:"skill_name,omitempty"`
	VersionID        string           `json:"version_id,omitempty"`
	VendorID         string           `json:"vendor_id,omitempty"`
	Meter            string           `json:"meter"`
	Quantity         float64          `json:"quantity"`
	Currency         string           `json:"currency,omitempty"`
	UnitPriceMinor   int64            `json:"unit_price_minor,omitempty"`
	GrossAmountMinor int64            `json:"gross_amount_minor,omitempty"`
	Status           string           `json:"status"`
	InvocationID     string           `json:"invocation_id,omitempty"`
	UsageEventID     string           `json:"usage_event_id,omitempty"`
	Error            string           `json:"error,omitempty"`
	OccurredAt       string           `json:"occurred_at"`
	Integrity        *RecordIntegrity `json:"integrity,omitempty"`
}

type BillingRecordListResult struct {
	Records   []BillingRecord         `json:"records"`
	Totals    []BillingTotal          `json:"totals,omitempty"`
	Integrity *LedgerIntegritySummary `json:"integrity,omitempty"`
}

type InstallRecordListResult struct {
	Records   []InstallRecord         `json:"records"`
	Integrity *LedgerIntegritySummary `json:"integrity,omitempty"`
}

type RecordIntegrity struct {
	Algorithm    string `json:"algorithm,omitempty"`
	Ledger       string `json:"ledger,omitempty"`
	KeyID        string `json:"key_id,omitempty"`
	Sequence     int    `json:"sequence,omitempty"`
	PreviousHash string `json:"previous_hash,omitempty"`
	Hash         string `json:"hash,omitempty"`
	SignedAt     string `json:"signed_at,omitempty"`
	VerifiedAt   string `json:"verified_at,omitempty"`
	Status       string `json:"status,omitempty"`
	Reason       string `json:"reason,omitempty"`
	HeadHash     string `json:"head_hash,omitempty"`
	HeadMatched  bool   `json:"head_matched,omitempty"`
}

type LedgerIntegritySummary struct {
	Ledger         string `json:"ledger"`
	Algorithm      string `json:"algorithm,omitempty"`
	Status         string `json:"status"`
	Records        int    `json:"records"`
	Verified       int    `json:"verified"`
	Failed         int    `json:"failed"`
	LegacyUnsigned int    `json:"legacy_unsigned"`
	Anchors        int    `json:"anchors"`
	AnchorMatched  bool   `json:"anchor_matched"`
	KeyID          string `json:"key_id,omitempty"`
	LastHash       string `json:"last_hash,omitempty"`
	HeadHash       string `json:"head_hash,omitempty"`
	HeadMatched    bool   `json:"head_matched"`
	VerifiedAt     string `json:"verified_at,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type BillingTotal struct {
	Currency         string `json:"currency"`
	Records          int    `json:"records"`
	GrossAmountMinor int64  `json:"gross_amount_minor"`
}

type CapabilityCommerceSnapshot struct {
	SchemaVersion  int                       `json:"schema_version"`
	GeneratedAt    string                    `json:"generated_at"`
	Packs          []CapabilityPackView      `json:"packs"`
	Scenarios      []CapabilityScenarioView  `json:"scenarios,omitempty"`
	InstallRecords InstallRecordListResult   `json:"install_records"`
	Billing        BillingRecordListResult   `json:"billing"`
	Receipts       CommerceReceiptListResult `json:"receipts"`
	Integrity      []LedgerIntegritySummary  `json:"integrity,omitempty"`
}

type CommerceIntegrityResult struct {
	SchemaVersion int                      `json:"schema_version"`
	GeneratedAt   string                   `json:"generated_at"`
	OK            bool                     `json:"ok"`
	Summary       DiagnosticSummary        `json:"summary"`
	Ledgers       []LedgerIntegritySummary `json:"ledgers"`
	Checks        []DoctorCheck            `json:"checks"`
}

type CommerceProof struct {
	SchemaVersion int                  `json:"schema_version"`
	GeneratedAt   string               `json:"generated_at"`
	Challenge     string               `json:"challenge"`
	Subject       string               `json:"subject"`
	TrustLevel    string               `json:"trust_level"`
	ReceiptStatus string               `json:"receipt_status"`
	Algorithm     string               `json:"algorithm"`
	KeyID         string               `json:"key_id"`
	PublicKey     string               `json:"public_key"`
	PayloadHash   string               `json:"payload_hash"`
	Signature     string               `json:"signature"`
	Payload       CommerceProofPayload `json:"payload"`
}

type CommerceReceipt struct {
	SchemaVersion    int              `json:"schema_version"`
	ReceiptID        string           `json:"receipt_id"`
	Status           string           `json:"status"`
	ReceivedAt       string           `json:"received_at"`
	Issuer           string           `json:"issuer,omitempty"`
	ServerLedgerID   string           `json:"server_ledger_id,omitempty"`
	ServerSequence   int64            `json:"server_sequence,omitempty"`
	Algorithm        string           `json:"algorithm"`
	KeyID            string           `json:"key_id"`
	PublicKey        string           `json:"public_key"`
	ProofPayloadHash string           `json:"proof_payload_hash"`
	ProofSignature   string           `json:"proof_signature"`
	ProofKeyID       string           `json:"proof_key_id"`
	Challenge        string           `json:"challenge"`
	DeviceID         string           `json:"device_id,omitempty"`
	ServerSignature  string           `json:"server_signature"`
	Integrity        *RecordIntegrity `json:"integrity,omitempty"`
}

type CommerceReceiptVerificationResult struct {
	SchemaVersion          int                         `json:"schema_version"`
	VerifiedAt             string                      `json:"verified_at"`
	OK                     bool                        `json:"ok"`
	ReceiptMatched         bool                        `json:"receipt_matched"`
	ProofMatched           bool                        `json:"proof_matched"`
	ProofSignatureMatched  bool                        `json:"proof_signature_matched"`
	ServerSignatureMatched bool                        `json:"server_signature_matched"`
	ServerKeyTrusted       bool                        `json:"server_key_trusted"`
	TrustStatus            string                      `json:"trust_status,omitempty"`
	ExpectedPayloadHash    string                      `json:"expected_payload_hash,omitempty"`
	ActualPayloadHash      string                      `json:"actual_payload_hash,omitempty"`
	ReceiptID              string                      `json:"receipt_id,omitempty"`
	Status                 string                      `json:"status,omitempty"`
	Reason                 string                      `json:"reason,omitempty"`
	Trust                  *CommerceReceiptTrustResult `json:"trust,omitempty"`
}

type CommerceReceiptTrustResult struct {
	SchemaVersion    int    `json:"schema_version"`
	OK               bool   `json:"ok"`
	Status           string `json:"status"`
	Issuer           string `json:"issuer,omitempty"`
	KeyID            string `json:"key_id,omitempty"`
	PublicKey        string `json:"public_key,omitempty"`
	ReceiptAlgorithm string `json:"receipt_algorithm,omitempty"`
	BoundAt          string `json:"bound_at,omitempty"`
	LastSeenAt       string `json:"last_seen_at,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type CommerceReceiptListResult struct {
	Records   []CommerceReceipt           `json:"records"`
	Integrity *LedgerIntegritySummary     `json:"integrity,omitempty"`
	Trust     *CommerceReceiptTrustResult `json:"trust,omitempty"`
}

type CommerceReceiptSubmitResult struct {
	SchemaVersion int                               `json:"schema_version"`
	SubmittedAt   string                            `json:"submitted_at"`
	Proof         CommerceProof                     `json:"proof"`
	Receipt       CommerceReceipt                   `json:"receipt"`
	Verification  CommerceReceiptVerificationResult `json:"verification"`
}

type CommerceProofPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	GeneratedAt   string                   `json:"generated_at"`
	Challenge     string                   `json:"challenge"`
	Subject       string                   `json:"subject"`
	TrustLevel    string                   `json:"trust_level"`
	ReceiptStatus string                   `json:"receipt_status"`
	Algorithm     string                   `json:"algorithm"`
	KeyID         string                   `json:"key_id"`
	PublicKey     string                   `json:"public_key"`
	DeviceID      string                   `json:"device_id,omitempty"`
	OK            bool                     `json:"ok"`
	Summary       DiagnosticSummary        `json:"summary"`
	Ledgers       []LedgerIntegritySummary `json:"ledgers"`
	Checks        []DoctorCheck            `json:"checks"`
}

type CommerceProofVerificationResult struct {
	SchemaVersion      int    `json:"schema_version"`
	VerifiedAt         string `json:"verified_at"`
	OK                 bool   `json:"ok"`
	AlgorithmMatched   bool   `json:"algorithm_matched"`
	ChallengeMatched   bool   `json:"challenge_matched"`
	PayloadHashMatched bool   `json:"payload_hash_matched"`
	SignatureMatched   bool   `json:"signature_matched"`
	EnvelopeMatched    bool   `json:"envelope_matched"`
	ExpectedChallenge  string `json:"expected_challenge,omitempty"`
	ActualChallenge    string `json:"actual_challenge,omitempty"`
	KeyID              string `json:"key_id,omitempty"`
	PayloadHash        string `json:"payload_hash,omitempty"`
	CalculatedHash     string `json:"calculated_hash,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type CommerceSnapshotExportResult struct {
	Path     string                     `json:"path"`
	Snapshot CapabilityCommerceSnapshot `json:"snapshot"`
}

type RunResult struct {
	Name             string             `json:"name"`
	Version          string             `json:"version"`
	Stub             bool               `json:"stub"`
	ScenarioID       string             `json:"scenario_id,omitempty"`
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
	AttributedFiles  []string           `json:"attributed_files,omitempty"`
	UsageEvents      []UsageEventResult `json:"usage_events,omitempty"`
}

type RunOptions struct {
	Args             []string
	Input            []byte
	Timeout          time.Duration
	OutputLimitBytes int64
	ScenarioID       string
	AgentName        string
}

type UsageEventResult struct {
	EventID          string  `json:"event_id"`
	PackID           string  `json:"pack_id"`
	ScenarioID       string  `json:"scenario_id,omitempty"`
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
	Authenticated      bool             `json:"authenticated"`
	Subscription       string           `json:"subscription,omitempty"`
	Plan               string           `json:"plan,omitempty"`
	DeviceID           string           `json:"device_id,omitempty"`
	DeviceName         string           `json:"device_name,omitempty"`
	ExpiresAt          string           `json:"expires_at,omitempty"`
	DeviceLimit        int              `json:"device_limit,omitempty"`
	AuthPath           string           `json:"auth_path,omitempty"`
	Devices            []ProDevice      `json:"devices,omitempty"`
	RecommendedActions []ProSetupAction `json:"recommended_actions,omitempty"`
	CurrentStatus      []string         `json:"current_status,omitempty"`
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

