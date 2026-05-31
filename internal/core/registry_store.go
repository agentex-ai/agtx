package core

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sha256Pattern = regexp.MustCompile(`^[A-Fa-f0-9]{64}$`)

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
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return RegistryRefreshResult{}, NewError(CodeTimeout, "registry refresh timed out", map[string]any{"url": config.RegistryURL, "timeout_ms": timeout.Milliseconds()})
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
			return RegistryRefreshResult{}, NewError(CodeTimeout, "registry refresh timed out", map[string]any{"url": config.RegistryURL, "timeout_ms": timeout.Milliseconds()})
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
	for _, bundle := range skill.Platforms {
		if err := validatePlatformBundle(skill, bundle); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
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
	if skill.Stub {
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
	default:
		return NewError(CodeInvalidArgument, "unsupported "+label+" scheme", map[string]any{"scheme": parsed.Scheme})
	}
	return nil
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
