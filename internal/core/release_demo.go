package core

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DemoReleaseOptions struct {
	OutDir        string
	BaseURL       string
	Version       string
	PackagePrefix string
	Skills        []string
	Platforms     []string
	Accounts      []string
}

type DemoReleaseResult struct {
	OutDir       string                `json:"out_dir"`
	RegistryPath string                `json:"registry_path"`
	RegistryKey  string                `json:"registry_key"`
	Registry     Registry              `json:"registry"`
	Registries   []DemoReleaseRegistry `json:"registries,omitempty"`
	Packages     []DemoReleasePackage  `json:"packages"`
	UploadHints  []string              `json:"upload_hints,omitempty"`
}

type DemoReleaseRegistry struct {
	AccountMode string   `json:"account_mode"`
	Path        string   `json:"path"`
	Key         string   `json:"key"`
	Registry    Registry `json:"registry"`
}

type DemoReleasePackage struct {
	Skill      string `json:"skill"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Path       string `json:"path"`
	Key        string `json:"key"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Archive    string `json:"archive"`
	Entrypoint string `json:"entrypoint"`
	Bytes      int64  `json:"bytes"`
}

type demoReleasePlatform struct {
	OS   string
	Arch string
}

type demoArchiveFile struct {
	data []byte
	mode os.FileMode
}

func CreateDemoRelease(options DemoReleaseOptions) (DemoReleaseResult, error) {
	options = normalizeDemoReleaseOptions(options)
	if strings.TrimSpace(options.OutDir) == "" {
		return DemoReleaseResult{}, NewError(CodeInvalidArgument, "release output directory is required", map[string]any{"field": "out"})
	}
	platforms, err := parseDemoReleasePlatforms(options.Platforms)
	if err != nil {
		return DemoReleaseResult{}, err
	}
	accounts, err := parseDemoReleaseAccounts(options.Accounts)
	if err != nil {
		return DemoReleaseResult{}, err
	}
	if err := os.MkdirAll(options.OutDir, 0o755); err != nil {
		return DemoReleaseResult{}, err
	}

	registry := Registry{SchemaVersion: 1, Skills: make([]SkillManifest, 0, len(options.Skills))}
	result := DemoReleaseResult{
		OutDir:       options.OutDir,
		RegistryPath: filepath.Join(options.OutDir, "registry", "pro.json"),
		RegistryKey:  "registry/pro.json",
		Packages:     make([]DemoReleasePackage, 0, len(options.Skills)*len(platforms)),
	}

	for _, name := range options.Skills {
		skill, ok := DefaultRegistry().Find(name)
		if !ok {
			return DemoReleaseResult{}, NewError(CodeNotFound, "skill not found in default registry", map[string]any{"skill": name})
		}
		released, packages, err := createDemoSkillPackages(options, platforms, skill)
		if err != nil {
			return DemoReleaseResult{}, err
		}
		registry.Skills = append(registry.Skills, released)
		result.Packages = append(result.Packages, packages...)
	}
	sort.Slice(registry.Skills, func(i, j int) bool { return registry.Skills[i].Name < registry.Skills[j].Name })
	sort.Slice(result.Packages, func(i, j int) bool {
		left := result.Packages[i]
		right := result.Packages[j]
		if left.Skill != right.Skill {
			return left.Skill < right.Skill
		}
		if left.OS != right.OS {
			return left.OS < right.OS
		}
		return left.Arch < right.Arch
	})

	registries, err := writeDemoReleaseRegistries(options, accounts, registry)
	if err != nil {
		return DemoReleaseResult{}, err
	}
	result.Registries = registries
	for _, item := range registries {
		if item.AccountMode == "pro" {
			result.RegistryPath = item.Path
			result.RegistryKey = item.Key
			result.Registry = item.Registry
			break
		}
	}
	if result.RegistryPath == "" && len(registries) > 0 {
		result.RegistryPath = registries[0].Path
		result.RegistryKey = registries[0].Key
		result.Registry = registries[0].Registry
	}
	result.UploadHints = demoReleaseUploadHints(result)
	return result, nil
}

func normalizeDemoReleaseOptions(options DemoReleaseOptions) DemoReleaseOptions {
	options.OutDir = strings.TrimSpace(options.OutDir)
	options.BaseURL = strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	options.PackagePrefix = strings.Trim(strings.TrimSpace(options.PackagePrefix), "/")
	if strings.TrimSpace(options.Version) == "" {
		options.Version = "0.1.0-demo"
	}
	options.Version = strings.TrimSpace(options.Version)
	if len(options.Skills) == 0 {
		options.Skills = []string{"all"}
	}
	if len(options.Platforms) == 0 {
		options.Platforms = defaultCapabilityReleasePlatforms()
	}
	if len(options.Accounts) == 0 {
		options.Accounts = []string{"normal", "pro"}
	}
	options.Skills = expandDemoReleaseSkills(options.Skills)
	return options
}

func expandDemoReleaseSkills(values []string) []string {
	seen := map[string]bool{}
	var skills []string
	appendSkill := func(name string) {
		name = canonicalSkillName(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		skills = append(skills, name)
	}
	for _, value := range values {
		switch normalizeName(value) {
		case "all", "implemented", "ready":
			for _, skill := range DefaultRegistry().Skills {
				appendSkill(skill.Name)
			}
		case "documents", "document":
			for _, skill := range []string{"docx", "xlsx", "pptx", "pdf"} {
				appendSkill(skill)
			}
		case "web":
			for _, skill := range []string{"web_search", "web_fetch"} {
				appendSkill(skill)
			}
		default:
			appendSkill(value)
		}
	}
	sort.Strings(skills)
	return skills
}

func createDemoSkillPackages(options DemoReleaseOptions, platforms []demoReleasePlatform, skill SkillManifest) (SkillManifest, []DemoReleasePackage, error) {
	if err := validatePathSegment("skill version", options.Version); err != nil {
		return SkillManifest{}, nil, err
	}
	prefix, err := cleanDemoPackagePrefix(options.PackagePrefix)
	if err != nil {
		return SkillManifest{}, nil, err
	}
	skill.Version = options.Version
	skill.Stub = false
	skill.Builtin = nil
	skill.Platforms = nil
	if skill.Signature == nil {
		skill.Signature = &SignatureInfo{}
	}
	if strings.TrimSpace(skill.Signature.Algorithm) == "" || strings.TrimSpace(skill.Signature.Algorithm) == "reserved" {
		skill.Signature.Algorithm = "demo-unsigned"
	}

	packages := make([]DemoReleasePackage, 0, len(platforms))
	for _, platform := range platforms {
		pkg, bundle, err := createDemoPlatformPackage(options, prefix, skill, platform)
		if err != nil {
			return SkillManifest{}, nil, err
		}
		skill.Platforms = append(skill.Platforms, bundle)
		packages = append(packages, pkg)
	}
	return skill, packages, nil
}

func createDemoPlatformPackage(options DemoReleaseOptions, prefix string, skill SkillManifest, platform demoReleasePlatform) (DemoReleasePackage, PlatformBundle, error) {
	entrypoint := demoEntrypoint(skill.Name, platform.OS)
	key := strings.Join([]string{prefix, skill.Name, options.Version, platform.OS + "-" + platform.Arch + ".zip"}, "/")
	path := filepath.Join(options.OutDir, filepath.FromSlash(key))
	files := map[string]demoArchiveFile{
		"README.txt": {data: []byte(demoPackageReadme(skill.Name)), mode: 0o644},
		entrypoint:   {data: []byte(demoEntrypointScript(skill.Name, platform.OS)), mode: 0o755},
	}
	sum, size, err := writeDemoReleaseArchive(path, files)
	if err != nil {
		return DemoReleasePackage{}, PlatformBundle{}, err
	}
	url := path
	if options.BaseURL != "" {
		url = options.BaseURL + "/" + key
	}
	bundle := PlatformBundle{OS: platform.OS, Arch: platform.Arch, URL: url, SHA256: sum, Archive: "zip", Entrypoint: entrypoint}
	pkg := DemoReleasePackage{Skill: skill.Name, Version: skill.Version, OS: platform.OS, Arch: platform.Arch, Path: path, Key: key, URL: url, SHA256: sum, Archive: "zip", Entrypoint: entrypoint, Bytes: size}
	return pkg, bundle, nil
}

func parseDemoReleasePlatforms(values []string) ([]demoReleasePlatform, error) {
	seen := map[string]bool{}
	platforms := make([]demoReleasePlatform, 0, len(values))
	for _, raw := range values {
		for _, value := range splitDemoCSV(raw) {
			value = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "/")
			if value == "" {
				continue
			}
			parts := strings.Split(value, "/")
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return nil, NewError(CodeInvalidArgument, "platform must use os/arch syntax", map[string]any{"platform": value, "example": "windows/amd64,darwin/arm64"})
			}
			platform := demoReleasePlatform{OS: strings.TrimSpace(parts[0]), Arch: strings.TrimSpace(parts[1])}
			if err := validatePathSegment("platform os", platform.OS); err != nil {
				return nil, err
			}
			if err := validatePathSegment("platform arch", platform.Arch); err != nil {
				return nil, err
			}
			key := platform.OS + "/" + platform.Arch
			if seen[key] {
				continue
			}
			seen[key] = true
			platforms = append(platforms, platform)
		}
	}
	if len(platforms) == 0 {
		return nil, NewError(CodeInvalidArgument, "at least one platform is required", map[string]any{"field": "platforms"})
	}
	return platforms, nil
}

func parseDemoReleaseAccounts(values []string) ([]string, error) {
	seen := map[string]bool{}
	accounts := make([]string, 0, len(values))
	for _, raw := range values {
		for _, value := range splitDemoCSV(raw) {
			mode := normalizeDemoAccountMode(value)
			if mode == "" || seen[mode] {
				continue
			}
			switch mode {
			case "normal", "pro":
				seen[mode] = true
				accounts = append(accounts, mode)
			default:
				return nil, NewError(CodeInvalidArgument, "account mode must be normal or pro", map[string]any{"account": value})
			}
		}
	}
	if len(accounts) == 0 {
		return nil, NewError(CodeInvalidArgument, "at least one account mode is required", map[string]any{"field": "accounts"})
	}
	return accounts, nil
}

func normalizeDemoAccountMode(value string) string {
	switch normalizeName(value) {
	case "public", "free", "ordinary", "basic", "standard", "normal":
		return "normal"
	case "pro", "premium", "advanced":
		return "pro"
	default:
		return normalizeName(value)
	}
}

func splitDemoCSV(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
}

func writeDemoReleaseRegistries(options DemoReleaseOptions, accounts []string, registry Registry) ([]DemoReleaseRegistry, error) {
	registries := make([]DemoReleaseRegistry, 0, len(accounts))
	for _, account := range accounts {
		key := "registry/" + demoRegistryFileName(account)
		path := filepath.Join(options.OutDir, filepath.FromSlash(key))
		if err := writeDemoRegistryFile(path, registry); err != nil {
			return nil, err
		}
		registries = append(registries, DemoReleaseRegistry{AccountMode: account, Path: path, Key: key, Registry: registry})
	}
	return registries, nil
}

func demoRegistryFileName(account string) string {
	if account == "pro" {
		return "pro.json"
	}
	return "normal.json"
}

func writeDemoRegistryFile(path string, registry Registry) error {
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

func demoEntrypoint(skill, goos string) string {
	name := normalizeName(skill)
	if goos == "windows" {
		return "bin/" + name + ".cmd"
	}
	return "bin/" + name
}

func demoEntrypointScript(skill, goos string) string {
	payload := map[string]any{"ok": true, "skill": normalizeName(skill), "demo": true, "message": "demo capability package executed"}
	encoded, _ := json.Marshal(payload)
	if goos == "windows" {
		return "@echo off\r\necho " + string(encoded) + "\r\n"
	}
	return "#!/bin/sh\nprintf '%s\\n' '" + string(encoded) + "'\n"
}

func demoPackageReadme(skill string) string {
	return "Agentex demo capability package: " + normalizeName(skill) + "\n\n" +
		"This archive is a non-stub package used to exercise registry download, install, verify, run, billing-record, and telemetry-record paths.\n" +
		"Replace this demo entrypoint with the final native capability implementation before production publication.\n"
}

func writeDemoReleaseArchive(path string, files map[string]demoArchiveFile) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", 0, err
	}
	tempName := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempName)
		}
	}()
	zw := zip.NewWriter(temp)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := files[name]
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(file.mode)
		writer, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			_ = temp.Close()
			return "", 0, err
		}
		if _, err := writer.Write(file.data); err != nil {
			_ = zw.Close()
			_ = temp.Close()
			return "", 0, err
		}
	}
	if err := zw.Close(); err != nil {
		_ = temp.Close()
		return "", 0, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", 0, err
	}
	if err := temp.Close(); err != nil {
		return "", 0, err
	}
	data, err := os.ReadFile(tempName)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(data)
	if err := renameReplacing(tempName, path); err != nil {
		return "", 0, err
	}
	cleanup = false
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(sum[:]), int64(len(data)), nil
}

func cleanDemoPackagePrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "packages", nil
	}
	parts := strings.Split(prefix, "/")
	for _, part := range parts {
		if err := validatePathSegment("package prefix", part); err != nil {
			return "", err
		}
	}
	return strings.Join(parts, "/"), nil
}

func demoReleaseUploadHints(result DemoReleaseResult) []string {
	hints := make([]string, 0, len(result.Registries)+len(result.Packages))
	for _, registry := range result.Registries {
		hints = append(hints, "wrangler r2 object put agentex-packages/"+registry.Key+" --file "+registry.Path)
	}
	for _, pkg := range result.Packages {
		hints = append(hints, "wrangler r2 object put agentex-packages/"+pkg.Key+" --file "+pkg.Path)
	}
	return hints
}
