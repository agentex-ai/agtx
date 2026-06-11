package core

import (
	"sort"
	"strings"
)

type RegistryImplementationStatusOptions struct {
	Platforms []string
	Accounts  []string
}

type RegistryImplementationStatus struct {
	SchemaVersion      int                                    `json:"schema_version"`
	Source             string                                 `json:"source,omitempty"`
	RequestedPlatforms []string                               `json:"requested_platforms,omitempty"`
	AccountModes       []string                               `json:"account_modes,omitempty"`
	Total              int                                    `json:"total"`
	Implemented        int                                    `json:"implemented"`
	Partial            int                                    `json:"partial"`
	Stub               int                                    `json:"stub"`
	Incomplete         int                                    `json:"incomplete"`
	Missing            []string                               `json:"missing,omitempty"`
	Skills             []RegistrySkillImplementationStatus    `json:"skills"`
	PlatformCoverage   []RegistryPlatformImplementationStatus `json:"platform_coverage,omitempty"`
}

type RegistrySkillImplementationStatus struct {
	Name              string                              `json:"name"`
	Version           string                              `json:"version"`
	Status            string                              `json:"status"`
	Stub              bool                                `json:"stub"`
	RunnablePlatforms []string                            `json:"runnable_platforms,omitempty"`
	MissingPlatforms  []string                            `json:"missing_platforms,omitempty"`
	Platforms         []RegistrySkillPlatformBundleStatus `json:"platforms,omitempty"`
}

type RegistrySkillPlatformBundleStatus struct {
	Platform      string `json:"platform"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Runnable      bool   `json:"runnable"`
	Reason        string `json:"reason,omitempty"`
	HasURL        bool   `json:"has_url"`
	HasSHA256     bool   `json:"has_sha256"`
	HasArchive    bool   `json:"has_archive"`
	HasEntrypoint bool   `json:"has_entrypoint"`
}

type RegistryPlatformImplementationStatus struct {
	Platform    string `json:"platform"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Total       int    `json:"total"`
	Implemented int    `json:"implemented"`
	Stub        int    `json:"stub"`
	Incomplete  int    `json:"incomplete"`
	Missing     int    `json:"missing"`
}

func BuildRegistryImplementationStatus(registry Registry, options RegistryImplementationStatusOptions) (RegistryImplementationStatus, error) {
	platforms, err := registryStatusPlatforms(registry, options.Platforms)
	if err != nil {
		return RegistryImplementationStatus{}, err
	}
	accounts, err := registryStatusAccounts(options.Accounts)
	if err != nil {
		return RegistryImplementationStatus{}, err
	}

	result := RegistryImplementationStatus{
		SchemaVersion:      1,
		RequestedPlatforms: formatRegistryStatusPlatforms(platforms),
		AccountModes:       accounts,
		Total:              len(registry.Skills),
		Skills:             make([]RegistrySkillImplementationStatus, 0, len(registry.Skills)),
	}
	coverage := make([]RegistryPlatformImplementationStatus, len(platforms))
	for index, platform := range platforms {
		coverage[index] = RegistryPlatformImplementationStatus{Platform: platformKey(platform), OS: platform.OS, Arch: platform.Arch}
	}

	for _, skill := range registry.Skills {
		item := buildRegistrySkillImplementationStatus(skill, platforms, coverage)
		result.Skills = append(result.Skills, item)
		switch item.Status {
		case "implemented":
			result.Implemented++
		case "partial":
			result.Partial++
			result.Missing = append(result.Missing, skill.Name)
		case "stub":
			result.Stub++
			result.Missing = append(result.Missing, skill.Name)
		default:
			result.Incomplete++
			result.Missing = append(result.Missing, skill.Name)
		}
	}
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].Name < result.Skills[j].Name })
	sort.Strings(result.Missing)
	result.PlatformCoverage = coverage
	return result, nil
}

func BuildRegistryImplementationStatusForFile(path string, options RegistryImplementationStatusOptions) (RegistryImplementationStatus, error) {
	data, err := readFileLimited(path, defaultRegistryMaxBytes, "registry")
	if err != nil {
		return RegistryImplementationStatus{}, err
	}
	registry, err := decodeRegistry(data)
	if err != nil {
		return RegistryImplementationStatus{}, err
	}
	status, err := BuildRegistryImplementationStatus(registry, options)
	if err != nil {
		return RegistryImplementationStatus{}, err
	}
	status.Source = path
	return status, nil
}

func buildRegistrySkillImplementationStatus(skill SkillManifest, platforms []demoReleasePlatform, coverage []RegistryPlatformImplementationStatus) RegistrySkillImplementationStatus {
	item := RegistrySkillImplementationStatus{Name: skill.Name, Version: skill.Version, Stub: skill.Stub}
	available := map[string]PlatformBundle{}
	for _, bundle := range skill.Platforms {
		available[bundle.OS+"/"+bundle.Arch] = bundle
	}

	for index, platform := range platforms {
		coverage[index].Total++
		bundle, ok := available[platformKey(platform)]
		if !ok {
			coverage[index].Missing++
			item.MissingPlatforms = append(item.MissingPlatforms, platformKey(platform))
			item.Platforms = append(item.Platforms, RegistrySkillPlatformBundleStatus{Platform: platformKey(platform), OS: platform.OS, Arch: platform.Arch, Reason: "missing_platform"})
			continue
		}
		platformStatus := registryBundleImplementationStatus(skill, bundle)
		item.Platforms = append(item.Platforms, platformStatus)
		if platformStatus.Runnable {
			coverage[index].Implemented++
			item.RunnablePlatforms = append(item.RunnablePlatforms, platformStatus.Platform)
			continue
		}
		if skill.Stub {
			coverage[index].Stub++
		} else {
			coverage[index].Incomplete++
		}
	}

	sort.Strings(item.RunnablePlatforms)
	sort.Strings(item.MissingPlatforms)
	if skill.Stub {
		item.Status = "stub"
	} else if len(item.RunnablePlatforms) == len(platforms) {
		item.Status = "implemented"
	} else if len(item.RunnablePlatforms) > 0 {
		item.Status = "partial"
	} else {
		item.Status = "incomplete"
	}
	return item
}

func registryBundleImplementationStatus(skill SkillManifest, bundle PlatformBundle) RegistrySkillPlatformBundleStatus {
	status := RegistrySkillPlatformBundleStatus{
		Platform:      bundle.OS + "/" + bundle.Arch,
		OS:            bundle.OS,
		Arch:          bundle.Arch,
		HasURL:        strings.TrimSpace(bundle.URL) != "",
		HasSHA256:     sha256Pattern.MatchString(strings.TrimSpace(bundle.SHA256)),
		HasArchive:    strings.TrimSpace(bundle.Archive) != "",
		HasEntrypoint: strings.TrimSpace(bundle.Entrypoint) != "",
	}
	if skill.Stub {
		status.Reason = "stub"
		return status
	}
	if strings.TrimSpace(bundle.URL) == "" {
		status.Reason = "missing_url"
		return status
	}
	if err := validateBundleURL(bundle.URL); err != nil {
		status.Reason = "invalid_url"
		return status
	}
	if !status.HasSHA256 {
		status.Reason = "missing_sha256"
		return status
	}
	if err := validateArchiveType(bundle.Archive, bundle.URL); err != nil {
		status.Reason = "invalid_archive"
		return status
	}
	if strings.TrimSpace(bundle.Entrypoint) == "" {
		status.Reason = "missing_entrypoint"
		return status
	}
	if _, err := cleanArchiveRelativePath(bundle.Entrypoint, "entrypoint"); err != nil {
		status.Reason = "invalid_entrypoint"
		return status
	}
	status.Runnable = true
	return status
}

func registryStatusPlatforms(registry Registry, values []string) ([]demoReleasePlatform, error) {
	if len(values) > 0 {
		return parseDemoReleasePlatforms(values)
	}
	seen := map[string]bool{}
	var out []demoReleasePlatform
	for _, skill := range registry.Skills {
		for _, bundle := range skill.Platforms {
			platform := demoReleasePlatform{OS: strings.TrimSpace(bundle.OS), Arch: strings.TrimSpace(bundle.Arch)}
			if platform.OS == "" || platform.Arch == "" {
				continue
			}
			key := platformKey(platform)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, platform)
		}
	}
	if len(out) == 0 {
		out = []demoReleasePlatform{{OS: "windows", Arch: "amd64"}, {OS: "darwin", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"}}
	}
	sort.Slice(out, func(i, j int) bool { return platformKey(out[i]) < platformKey(out[j]) })
	return out, nil
}

func registryStatusAccounts(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"normal", "pro"}, nil
	}
	return parseDemoReleaseAccounts(values)
}

func formatRegistryStatusPlatforms(platforms []demoReleasePlatform) []string {
	out := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		out = append(out, platformKey(platform))
	}
	return out
}

func platformKey(platform demoReleasePlatform) string {
	return platform.OS + "/" + platform.Arch
}

func defaultCapabilityReleasePlatforms() []string {
	return []string{"windows/amd64", "darwin/amd64", "darwin/arm64"}
}
