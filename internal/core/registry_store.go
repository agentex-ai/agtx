package core

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sha256Pattern = regexp.MustCompile(`^[A-Fa-f0-9]{64}$`)
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)

type RegistrySource struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	URL    string `json:"url,omitempty"`
	Loaded bool   `json:"loaded"`
	Error  string `json:"error,omitempty"`
}

type RegistryRefreshResult struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
}

type RegistryValidation struct {
	Path     string   `json:"path"`
	OK       bool     `json:"ok"`
	Skills   int      `json:"skills"`
	Warnings []string `json:"warnings,omitempty"`
}

func LoadRegistry(paths Paths, config Config) (Registry, []RegistrySource) {
	registry := DefaultRegistry()
	sources := []RegistrySource{{Kind: "builtin", Loaded: true}}

	candidates := make([]string, 0, len(config.RegistryFiles)+1)
	candidates = append(candidates, config.RegistryFiles...)
	candidates = append(candidates, filepath.Join(paths.RegistryDir, defaultRegistryCache))
	for _, path := range candidates {
		if strings.TrimSpace(path) == "" {
			continue
		}
		source := RegistrySource{Kind: "file", Path: path}
		next, err := readRegistryFile(path, config.RegistryMaxBytes)
		if err != nil {
			if !os.IsNotExist(err) {
				source.Error = err.Error()
				sources = append(sources, source)
			}
			continue
		}
		registry = mergeRegistry(registry, next)
		source.Loaded = true
		sources = append(sources, source)
	}
	if config.RegistryURL != "" {
		sources = append(sources, RegistrySource{Kind: "remote_configured", URL: config.RegistryURL, Loaded: false})
	}
	return registry, sources
}

func RefreshRegistry(ctx context.Context, paths Paths, config Config) (RegistryRefreshResult, error) {
	config = normalizeConfig(config)
	if strings.TrimSpace(config.RegistryURL) == "" {
		return RegistryRefreshResult{}, NewError(CodeInvalidArgument, "registry_url is not configured", map[string]any{"config": paths.ConfigFile})
	}
	if err := validateRegistryURL(config.RegistryURL); err != nil {
		return RegistryRefreshResult{}, err
	}
	timeout := time.Duration(config.RegistryDownloadTimeoutMS) * time.Millisecond
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, config.RegistryURL, nil)
	if err != nil {
		return RegistryRefreshResult{}, err
	}
	attachAuthHeader(req, config, loadRequestAuth(ctx, paths, config))
	res, err := outboundHTTPClient.Do(req)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return RegistryRefreshResult{}, NewError(CodeTimeout, "registry refresh timed out", map[string]any{"url": safeURLForDetails(config.RegistryURL), "timeout_ms": timeout.Milliseconds()})
		}
		return RegistryRefreshResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := readAllLimited(res.Body, defaultAuthMaxBytes, "registry error")
		return RegistryRefreshResult{}, withProRecoveryDetails(remoteHTTPError("registry refresh failed", config.RegistryURL, res.StatusCode, res.Status, data), paths, config)
	}
	data, err := readAllLimited(res.Body, config.RegistryMaxBytes, "registry")
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return RegistryRefreshResult{}, NewError(CodeTimeout, "registry refresh timed out", map[string]any{"url": safeURLForDetails(config.RegistryURL), "timeout_ms": timeout.Milliseconds()})
		}
		return RegistryRefreshResult{}, err
	}
	if _, err := decodeRegistry(data); err != nil {
		return RegistryRefreshResult{}, err
	}
	path := filepath.Join(paths.RegistryDir, defaultRegistryCache)
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return RegistryRefreshResult{}, err
	}
	return RegistryRefreshResult{Source: config.RegistryURL, Path: path, Bytes: len(data)}, nil
}

func readRegistryFile(path string, limit int64) (Registry, error) {
	data, err := readFileLimited(path, limit, "registry")
	if err != nil {
		return Registry{}, err
	}
	return decodeRegistry(data)
}

func ValidateRegistryFile(path string) (RegistryValidation, error) {
	registry, err := readRegistryFile(path, defaultRegistryMaxBytes)
	if err != nil {
		return RegistryValidation{Path: path, OK: false}, err
	}
	warnings := validateRegistry(registry)
	return RegistryValidation{Path: path, OK: len(warnings) == 0, Skills: len(registry.Skills), Warnings: warnings}, nil
}

func decodeRegistry(data []byte) (Registry, error) {
	var registry Registry
	if err := decodeJSONStrict(data, &registry); err != nil {
		return Registry{}, NewError(CodeInvalidArgument, "invalid registry manifest", err.Error())
	}
	if registry.SchemaVersion == 0 {
		registry.SchemaVersion = 1
	}
	for index, skill := range registry.Skills {
		if strings.TrimSpace(skill.Name) == "" || strings.TrimSpace(skill.Version) == "" {
			return Registry{}, NewError(CodeInvalidArgument, "registry skill requires name and version", map[string]any{"index": index})
		}
		if _, err := validateSkillManifest(skill); err != nil {
			return Registry{}, err
		}
	}
	return registry, nil
}

func validateRegistry(registry Registry) []string {
	warnings := []string{}
	seen := map[string]bool{}
	for _, skill := range registry.Skills {
		key := normalizeName(skill.Name)
		if seen[key] {
			warnings = append(warnings, "duplicate skill: "+skill.Name)
		}
		seen[key] = true
		skillWarnings, err := validateSkillManifest(skill)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		warnings = append(warnings, skillWarnings...)
	}
	return warnings
}

func validateSkillManifest(skill SkillManifest) ([]string, error) {
	warnings := []string{}
	if strings.TrimSpace(skill.Name) == "" {
		return warnings, NewError(CodeInvalidArgument, "skill name is required", nil)
	}
	if err := validateSkillName(skill.Name); err != nil {
		return warnings, err
	}
	if strings.TrimSpace(skill.Version) == "" {
		return warnings, NewError(CodeInvalidArgument, "skill version is required", map[string]any{"skill": skill.Name})
	}
	if err := validatePathSegment("skill version", skill.Version); err != nil {
		return warnings, err
	}
	if len(skill.Platforms) == 0 {
		warnings = append(warnings, "skill has no platforms: "+skill.Name)
	}
	if err := validateBuiltinInfo(skill); err != nil {
		return warnings, err
	}
	for _, bundle := range skill.Platforms {
		if err := validatePlatformBundle(skill, bundle); err != nil {
			return warnings, err
		}
	}
	if err := validateCapabilityInfo(skill); err != nil {
		return warnings, err
	}
	if err := validateBillingInfo(skill); err != nil {
		return warnings, err
	}
	if err := validateAttributionInfo(skill); err != nil {
		return warnings, err
	}
	if err := validateSupportInfo(skill); err != nil {
		return warnings, err
	}
	return warnings, nil
}

func validateBuiltinInfo(skill SkillManifest) error {
	if skill.Builtin == nil {
		return nil
	}
	if skill.Stub {
		return NewError(CodeInvalidArgument, "built-in skill must not be marked as a stub", map[string]any{"skill": skill.Name})
	}
	if strings.TrimSpace(skill.Builtin.Runtime) == "" {
		return NewError(CodeInvalidArgument, "built-in skill requires runtime", map[string]any{"skill": skill.Name})
	}
	if strings.TrimSpace(skill.Builtin.Runtime) != skill.Builtin.Runtime || strings.ContainsRune(skill.Builtin.Runtime, 0) {
		return NewError(CodeInvalidArgument, "built-in runtime is invalid", map[string]any{"skill": skill.Name})
	}
	for _, backend := range skill.Builtin.Backends {
		switch strings.TrimSpace(backend) {
		case "onnxruntime", "ncnn", "net_http", "search_http", "research_workflow", "wav_audio", "transcript_notes", "procedural_png", "prompt_manifest", "openxml", "pdf_text", "manifest_scan", "archive_scan", "policy_rules":
		default:
			return NewError(CodeInvalidArgument, "unsupported built-in backend", map[string]any{"skill": skill.Name, "backend": backend})
		}
		if strings.TrimSpace(backend) != backend {
			return NewError(CodeInvalidArgument, "built-in backend must not contain leading or trailing whitespace", map[string]any{"skill": skill.Name, "backend": backend})
		}
	}
	for _, profile := range skill.Builtin.ModelProfiles {
		switch strings.TrimSpace(profile) {
		case "rapidocr", "ppocrv6", "ppocrv5", "ppocrv4", "readability_v1", "search_results_v1", "extractive_research_v1", "wav_inspect_v1", "meeting_notes_v1", "procedural_image_v1", "media_plan_v1", "docx_v1", "xlsx_v1", "pptx_v1", "pdf_text_v1", "security_audit_v1":
		default:
			return NewError(CodeInvalidArgument, "unsupported built-in model profile", map[string]any{"skill": skill.Name, "model_profile": profile})
		}
		if strings.TrimSpace(profile) != profile {
			return NewError(CodeInvalidArgument, "built-in model profile must not contain leading or trailing whitespace", map[string]any{"skill": skill.Name, "model_profile": profile})
		}
	}
	return nil
}

func validateCapabilityInfo(skill SkillManifest) error {
	if skill.Capability == nil {
		return nil
	}
	switch strings.TrimSpace(skill.Capability.Class) {
	case "", "tool", "workflow", "model_adapter", "connector", "content", "commerce":
		return nil
	default:
		return NewError(CodeInvalidArgument, "unsupported capability class", map[string]any{"skill": skill.Name, "class": skill.Capability.Class})
	}
}

func validateBillingInfo(skill SkillManifest) error {
	if skill.Billing == nil {
		return nil
	}
	for _, meter := range skill.Billing.Meters {
		if err := validateBillingMeter(skill, meter); err != nil {
			return err
		}
	}
	if skill.Billing.RevenueShare != nil {
		if skill.Billing.RevenueShare.ISV < 0 || skill.Billing.RevenueShare.Platform < 0 {
			return NewError(CodeInvalidArgument, "revenue share must be non-negative", map[string]any{"skill": skill.Name})
		}
	}
	return nil
}

func validateBillingMeter(skill SkillManifest, meter BillingMeter) error {
	switch strings.TrimSpace(meter.Meter) {
	case "call", "task", "page", "minute", "token", "credit", "seat", "storage_gb_day", "success", "scan":
	default:
		return NewError(CodeInvalidArgument, "unsupported billing meter", map[string]any{"skill": skill.Name, "meter": meter.Meter})
	}
	if strings.TrimSpace(meter.Meter) != meter.Meter {
		return NewError(CodeInvalidArgument, "billing meter must not contain leading or trailing whitespace", map[string]any{"skill": skill.Name, "meter": meter.Meter})
	}
	if meter.UnitPrice < 0 || meter.FreeQuota < 0 {
		return NewError(CodeInvalidArgument, "billing prices and quotas must be non-negative", map[string]any{"skill": skill.Name, "meter": meter.Meter})
	}
	if strings.ContainsRune(meter.Currency, 0) {
		return NewError(CodeInvalidArgument, "billing currency contains NUL byte", map[string]any{"skill": skill.Name})
	}
	return nil
}

func validateAttributionInfo(skill SkillManifest) error {
	if skill.Attribution == nil {
		return nil
	}
	for _, event := range skill.Attribution.Events {
		switch strings.TrimSpace(event) {
		case "lead_created", "account_created", "activation_completed", "checkout_started", "purchase_completed", "subscription_started", "subscription_renewed":
		default:
			return NewError(CodeInvalidArgument, "unsupported attribution event", map[string]any{"skill": skill.Name, "event": event})
		}
	}
	for name, days := range skill.Attribution.DefaultWindowDays {
		if strings.TrimSpace(name) == "" || days <= 0 {
			return NewError(CodeInvalidArgument, "attribution windows must be positive", map[string]any{"skill": skill.Name, "window": name})
		}
	}
	if skill.Attribution.DefaultCPSRate < 0 {
		return NewError(CodeInvalidArgument, "default CPS rate must be non-negative", map[string]any{"skill": skill.Name})
	}
	return nil
}

func validateSupportInfo(skill SkillManifest) error {
	if skill.Support != nil {
		if strings.TrimSpace(skill.Support.URL) != "" {
			if err := validateServiceURL("support url", skill.Support.URL); err != nil {
				return err
			}
		}
		if strings.TrimSpace(skill.Support.PrivacyURL) != "" {
			if err := validateServiceURL("privacy url", skill.Support.PrivacyURL); err != nil {
				return err
			}
		}
		if strings.ContainsRune(skill.Support.IncidentEmail, 0) {
			return NewError(CodeInvalidArgument, "incident email contains NUL byte", map[string]any{"skill": skill.Name})
		}
	}
	if !requiresISVSupport(skill) {
		return nil
	}
	if skill.Support == nil {
		return NewError(CodeInvalidArgument, "third-party monetized skill requires support metadata", map[string]any{"skill": skill.Name})
	}
	if strings.TrimSpace(skill.Support.URL) == "" {
		return NewError(CodeInvalidArgument, "third-party monetized skill requires support url", map[string]any{"skill": skill.Name})
	}
	if strings.TrimSpace(skill.Support.PrivacyURL) == "" {
		return NewError(CodeInvalidArgument, "third-party monetized skill requires privacy url", map[string]any{"skill": skill.Name})
	}
	if strings.TrimSpace(skill.Support.IncidentEmail) == "" {
		return NewError(CodeInvalidArgument, "third-party monetized skill requires incident email", map[string]any{"skill": skill.Name})
	}
	if !strings.Contains(skill.Support.IncidentEmail, "@") {
		return NewError(CodeInvalidArgument, "incident email is invalid", map[string]any{"skill": skill.Name})
	}
	return nil
}

func requiresISVSupport(skill SkillManifest) bool {
	vendor := strings.ToLower(strings.TrimSpace(skill.VendorID))
	if vendor == "" || vendor == "agentex" {
		return false
	}
	if skill.Billing != nil && len(skill.Billing.Meters) > 0 {
		return true
	}
	if skill.Attribution != nil && len(skill.Attribution.Events) > 0 {
		return true
	}
	return skill.Capability != nil && strings.TrimSpace(skill.Capability.Class) == "commerce"
}

func validatePlatformBundle(skill SkillManifest, bundle PlatformBundle) error {
	if strings.TrimSpace(bundle.OS) == "" || strings.TrimSpace(bundle.Arch) == "" {
		return NewError(CodeInvalidArgument, "platform requires os and arch", map[string]any{"skill": skill.Name})
	}
	if err := validatePathSegment("platform os", bundle.OS); err != nil {
		return err
	}
	if err := validatePathSegment("platform arch", bundle.Arch); err != nil {
		return err
	}
	if skill.Stub || skill.Builtin != nil {
		return nil
	}
	if strings.TrimSpace(bundle.URL) == "" {
		return NewError(CodeInvalidArgument, "non-stub platform requires url", map[string]any{"skill": skill.Name})
	}
	if err := validateBundleURL(bundle.URL); err != nil {
		return err
	}
	if !sha256Pattern.MatchString(strings.TrimSpace(bundle.SHA256)) {
		return NewError(CodeInvalidArgument, "non-stub platform requires a 64-character sha256", map[string]any{"skill": skill.Name})
	}
	if err := validateArchiveType(bundle.Archive, bundle.URL); err != nil {
		return err
	}
	if strings.TrimSpace(bundle.Entrypoint) == "" {
		return NewError(CodeInvalidArgument, "non-stub platform requires entrypoint", map[string]any{"skill": skill.Name})
	}
	if _, err := cleanArchiveRelativePath(bundle.Entrypoint, "entrypoint"); err != nil {
		return err
	}
	return nil
}

func validateBundleURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed != raw {
		return NewError(CodeInvalidArgument, "bundle url must not contain leading or trailing whitespace", map[string]any{"url": raw})
	}
	if strings.ContainsRune(raw, 0) {
		return NewError(CodeInvalidArgument, "bundle url contains NUL byte", nil)
	}
	if !strings.Contains(raw, "://") {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return NewError(CodeInvalidArgument, "bundle url is invalid", map[string]any{"url": raw})
	}
	switch parsed.Scheme {
	case "file":
		if parsed.Host != "" && parsed.Host != "localhost" {
			return NewError(CodeInvalidArgument, "file bundle url must be local", map[string]any{"url": raw})
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return NewError(CodeInvalidArgument, "file bundle url must not include query or fragment", map[string]any{"url": raw})
		}
		if parsed.Path == "" {
			return NewError(CodeInvalidArgument, "file bundle url requires path", map[string]any{"url": raw})
		}
	case "http", "https":
		if parsed.Host == "" {
			return NewError(CodeInvalidArgument, "bundle url requires host", map[string]any{"url": raw})
		}
		if parsed.User != nil {
			return NewError(CodeInvalidArgument, "bundle url must not include user info", map[string]any{"url": safeURLForDetails(raw)})
		}
		if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
			return NewError(CodeInvalidArgument, "http bundle url must use localhost or loopback; use https for remote bundles", map[string]any{"url": raw})
		}
	default:
		return NewError(CodeInvalidArgument, "unsupported bundle url scheme", map[string]any{"scheme": parsed.Scheme})
	}
	return nil
}

func validateRegistryURL(raw string) error {
	return validateServiceURL("registry_url", raw)
}

func validateServiceURL(label, raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed != raw {
		return NewError(CodeInvalidArgument, label+" must not contain leading or trailing whitespace", map[string]any{"url": raw})
	}
	if strings.ContainsRune(raw, 0) {
		return NewError(CodeInvalidArgument, label+" contains NUL byte", nil)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return NewError(CodeInvalidArgument, label+" is invalid", map[string]any{"url": raw})
	}
	switch parsed.Scheme {
	case "http", "https":
		if parsed.Host == "" {
			return NewError(CodeInvalidArgument, label+" requires host", map[string]any{"url": raw})
		}
		if isConfiguredServiceURL(label) && parsed.User != nil {
			return NewError(CodeInvalidArgument, label+" must not include user info", map[string]any{"url": safeURLForDetails(raw)})
		}
		if isConfiguredServiceURL(label) && (parsed.RawQuery != "" || parsed.Fragment != "") {
			return NewError(CodeInvalidArgument, label+" must not include query or fragment", map[string]any{"url": safeURLForDetails(raw)})
		}
		if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
			return NewError(CodeInvalidArgument, label+" must use https unless it targets localhost or loopback", map[string]any{"url": raw})
		}
	default:
		return NewError(CodeInvalidArgument, "unsupported "+label+" scheme", map[string]any{"scheme": parsed.Scheme})
	}
	return nil
}

func isConfiguredServiceURL(label string) bool {
	return label == "registry_url" || label == "pro_api_url"
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateArchiveType(archiveType, rawURL string) error {
	kind := strings.ToLower(strings.TrimSpace(archiveType))
	if strings.TrimSpace(archiveType) != archiveType {
		return NewError(CodeInvalidArgument, "archive type must not contain leading or trailing whitespace", map[string]any{"archive": archiveType})
	}
	if kind == "" {
		inferred, err := inferArchiveType(rawURL)
		if err != nil {
			return err
		}
		kind = inferred
	}
	switch kind {
	case "zip", "tar.gz", "tgz":
		return nil
	default:
		return NewError(CodeInvalidArgument, "unsupported archive type", map[string]any{"archive": archiveType})
	}
}

func inferArchiveType(rawURL string) (string, error) {
	path := rawURL
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Path != "" {
		path = parsed.Path
	}
	path = strings.ToLower(path)
	switch {
	case strings.HasSuffix(path, ".zip"):
		return "zip", nil
	case strings.HasSuffix(path, ".tar.gz"), strings.HasSuffix(path, ".tgz"):
		return "tar.gz", nil
	default:
		return "", NewError(CodeInvalidArgument, "unknown archive type", map[string]any{"url": rawURL})
	}
}

func mergeRegistry(base, overlay Registry) Registry {
	merged := Registry{SchemaVersion: 1, Skills: append([]SkillManifest{}, base.Skills...)}
	positions := map[string]int{}
	for index, skill := range merged.Skills {
		positions[normalizeName(skill.Name)] = index
	}
	for _, skill := range overlay.Skills {
		key := normalizeName(skill.Name)
		if index, ok := positions[key]; ok {
			merged.Skills[index] = skill
			continue
		}
		positions[key] = len(merged.Skills)
		merged.Skills = append(merged.Skills, skill)
	}
	return merged
}
