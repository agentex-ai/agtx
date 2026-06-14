package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultSecurityAuditMaxBytes          int64 = 8 * 1024 * 1024
	defaultSecurityAuditMaxFileBytes      int64 = 512 * 1024
	defaultSecurityAuditMaxArchiveFiles         = 500
	defaultSecurityAuditMaxDirectoryFiles       = 500
)

type builtinSecurityAuditInput struct {
	Manifest           json.RawMessage `json:"manifest,omitempty"`
	ManifestText       string          `json:"manifest_text,omitempty"`
	ManifestPath       string          `json:"manifest_path,omitempty"`
	RegistryEntry      json.RawMessage `json:"registry_entry,omitempty"`
	Path               string          `json:"path,omitempty"`
	PackagePath        string          `json:"package_path,omitempty"`
	URL                string          `json:"url,omitempty"`
	DownloadURL        string          `json:"download_url,omitempty"`
	ReleaseNotes       string          `json:"release_notes,omitempty"`
	PolicyProfile      string          `json:"policy_profile,omitempty"`
	AllowedPermissions []string        `json:"allowed_permissions,omitempty"`
	ExpectedSHA256     string          `json:"expected_sha256,omitempty"`
	MaxBytes           int64           `json:"max_bytes,omitempty"`
}

type builtinSecurityAuditOutput struct {
	Kind            string                           `json:"kind"`
	RiskLevel       string                           `json:"risk_level"`
	Score           int                              `json:"score"`
	Summary         builtinSecurityAuditSummary      `json:"summary"`
	Findings        []builtinSecurityAuditFinding    `json:"findings,omitempty"`
	Artifacts       []builtinSecurityAuditArtifact   `json:"artifacts,omitempty"`
	Permissions     []builtinSecurityAuditPermission `json:"permissions,omitempty"`
	Dependencies    []builtinSecurityAuditDependency `json:"dependencies,omitempty"`
	Recommendations []string                         `json:"recommendations,omitempty"`
	Warnings        []string                         `json:"warnings,omitempty"`
	Report          string                           `json:"report"`
	Metadata        map[string]any                   `json:"metadata,omitempty"`
}

type builtinSecurityAuditSummary struct {
	Critical      int `json:"critical"`
	High          int `json:"high"`
	Medium        int `json:"medium"`
	Low           int `json:"low"`
	Informational int `json:"informational"`
	Artifacts     int `json:"artifacts"`
	Permissions   int `json:"permissions"`
	Dependencies  int `json:"dependencies"`
}

type builtinSecurityAuditFinding struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Evidence       string `json:"evidence,omitempty"`
	Location       string `json:"location,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

type builtinSecurityAuditArtifact struct {
	Type      string `json:"type"`
	Path      string `json:"path,omitempty"`
	Size      int64  `json:"size,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Files     int    `json:"files,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type builtinSecurityAuditPermission struct {
	Name        string `json:"name"`
	Risk        string `json:"risk"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
}

type builtinSecurityAuditDependency struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

type securityAuditState struct {
	input        builtinSecurityAuditInput
	findings     []builtinSecurityAuditFinding
	artifacts    []builtinSecurityAuditArtifact
	permissions  []builtinSecurityAuditPermission
	dependencies []builtinSecurityAuditDependency
	warnings     []string
	seenFindings map[string]bool
	seenDeps     map[string]bool
	seenPerms    map[string]bool
}

func builtInSecurityAuditSkill(skill SkillManifest) SkillManifest {
	skill.Version = "0.1.0"
	skill.Summary = "Security audit and scanning"
	skill.Description = "Audit skill-store submissions, capability manifests, package archives, permissions, dependencies, download URLs, hashes, and sensitive behaviors before install, upgrade, or publication."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "manifest", "permissions", "supply-chain")
	skill.Permissions = []Permission{
		{Name: "local_read", Description: "Reads local manifests, directories, or package archives selected by the user for static analysis."},
	}
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "Security audit input for a capability pack, skill manifest, local package, or store submission.",
		"additionalProperties": true,
		"properties": map[string]any{
			"manifest":            map[string]any{"type": "object", "description": "Skill manifest or registry entry to audit."},
			"manifest_text":       map[string]any{"type": "string", "description": "Raw JSON manifest text to audit."},
			"manifest_path":       map[string]any{"type": "string", "description": "Local manifest JSON path."},
			"path":                map[string]any{"type": "string", "description": "Local manifest, package archive, package file, or directory to inspect."},
			"package_path":        map[string]any{"type": "string", "description": "Local zip, tar.gz, tgz, or package directory to inspect."},
			"url":                 map[string]any{"type": "string", "description": "Download or source URL to audit without automatically downloading remote packages."},
			"download_url":        map[string]any{"type": "string", "description": "Declared package download URL."},
			"expected_sha256":     map[string]any{"type": "string", "description": "Expected package SHA-256 for local package verification."},
			"allowed_permissions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"policy_profile":      map[string]any{"type": "string", "description": "Optional policy profile label such as store, strict, local, or relaxed."},
			"release_notes":       map[string]any{"type": "string", "description": "Release notes or changelog text to scan for risky change signals."},
			"max_bytes":           map[string]any{"type": "integer", "description": "Maximum bytes to read from a selected local file."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Security audit report with risk findings, artifacts, and human review recommendations.",
		"properties": map[string]any{
			"kind":            map[string]any{"type": "string"},
			"risk_level":      map[string]any{"type": "string"},
			"score":           map[string]any{"type": "integer"},
			"summary":         map[string]any{"type": "object"},
			"findings":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"artifacts":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"permissions":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"dependencies":    map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"recommendations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"warnings":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"report":          map[string]any{"type": "string"},
			"metadata":        map[string]any{"type": "object"},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-security-audit-v1",
		Backends:      []string{"manifest_scan", "archive_scan", "policy_rules"},
		ModelProfiles: []string{"security_audit_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func (s *Service) runBuiltinSecurityAudit(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	default:
	}
	input, err := parseBuiltinSecurityAuditInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	if !securityAuditInputHasTarget(input) {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "security_audit requires a manifest, package path, URL, release notes, or stdin JSON/text", map[string]any{"expected": "--manifest, --path, --package, --url, JSON input, or manifest text"})
	}
	state := newSecurityAuditState(input)
	state.audit(ctx)
	output := state.output()
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinSecurityAuditInput(options RunOptions) (builtinSecurityAuditInput, error) {
	input := builtinSecurityAuditInput{
		ManifestPath:   firstNonEmpty(webFetchOptionValue(options.Args, "manifest", ""), webFetchOptionValue(options.Args, "manifest-path", ""), webFetchOptionValue(options.Args, "manifest_path", "")),
		Path:           webFetchOptionValue(options.Args, "path", ""),
		PackagePath:    firstNonEmpty(webFetchOptionValue(options.Args, "package", ""), webFetchOptionValue(options.Args, "package-path", ""), webFetchOptionValue(options.Args, "package_path", "")),
		URL:            webFetchOptionValue(options.Args, "url", ""),
		DownloadURL:    firstNonEmpty(webFetchOptionValue(options.Args, "download-url", ""), webFetchOptionValue(options.Args, "download_url", "")),
		ExpectedSHA256: firstNonEmpty(webFetchOptionValue(options.Args, "expected-sha256", ""), webFetchOptionValue(options.Args, "sha256", "")),
		PolicyProfile:  firstNonEmpty(webFetchOptionValue(options.Args, "policy", ""), webFetchOptionValue(options.Args, "policy-profile", ""), webFetchOptionValue(options.Args, "policy_profile", "")),
		ReleaseNotes:   firstNonEmpty(webFetchOptionValue(options.Args, "release-notes", ""), webFetchOptionValue(options.Args, "release_notes", "")),
		MaxBytes:       int64(webFetchOptionInt(options.Args, "max-bytes", 0)),
	}
	allowed := firstNonEmpty(webFetchOptionValue(options.Args, "allowed-permissions", ""), webFetchOptionValue(options.Args, "allowed_permissions", ""))
	if allowed != "" {
		input.AllowedPermissions = splitSecurityAuditList(allowed)
	}
	if len(options.Input) > 0 {
		var payload builtinSecurityAuditInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && securityAuditInputHasValues(payload) {
			mergeSecurityAuditInput(&input, payload)
		} else {
			trimmed := strings.TrimSpace(string(options.Input))
			if trimmed != "" {
				input.ManifestText = trimmed
			}
		}
	}
	if input.Path == "" {
		input.Path = firstSecurityAuditPathArg(options.Args)
	}
	if input.URL == "" {
		input.URL = firstSecurityAuditURLArg(options.Args)
	}
	return input, nil
}

func securityAuditInputHasValues(input builtinSecurityAuditInput) bool {
	return len(input.Manifest) > 0 || len(input.RegistryEntry) > 0 || strings.TrimSpace(input.ManifestText) != "" || strings.TrimSpace(input.ManifestPath) != "" || strings.TrimSpace(input.Path) != "" || strings.TrimSpace(input.PackagePath) != "" || strings.TrimSpace(input.URL) != "" || strings.TrimSpace(input.DownloadURL) != "" || strings.TrimSpace(input.ReleaseNotes) != "" || strings.TrimSpace(input.PolicyProfile) != "" || len(input.AllowedPermissions) > 0 || strings.TrimSpace(input.ExpectedSHA256) != "" || input.MaxBytes > 0
}

func securityAuditInputHasTarget(input builtinSecurityAuditInput) bool {
	return len(input.Manifest) > 0 || len(input.RegistryEntry) > 0 || strings.TrimSpace(input.ManifestText) != "" || strings.TrimSpace(input.ManifestPath) != "" || strings.TrimSpace(input.Path) != "" || strings.TrimSpace(input.PackagePath) != "" || strings.TrimSpace(input.URL) != "" || strings.TrimSpace(input.DownloadURL) != "" || strings.TrimSpace(input.ReleaseNotes) != ""
}

func mergeSecurityAuditInput(input *builtinSecurityAuditInput, payload builtinSecurityAuditInput) {
	if len(input.Manifest) == 0 {
		input.Manifest = payload.Manifest
	}
	if len(input.RegistryEntry) == 0 {
		input.RegistryEntry = payload.RegistryEntry
	}
	if strings.TrimSpace(input.ManifestText) == "" {
		input.ManifestText = payload.ManifestText
	}
	if strings.TrimSpace(input.ManifestPath) == "" {
		input.ManifestPath = payload.ManifestPath
	}
	if strings.TrimSpace(input.Path) == "" {
		input.Path = payload.Path
	}
	if strings.TrimSpace(input.PackagePath) == "" {
		input.PackagePath = payload.PackagePath
	}
	if strings.TrimSpace(input.URL) == "" {
		input.URL = payload.URL
	}
	if strings.TrimSpace(input.DownloadURL) == "" {
		input.DownloadURL = payload.DownloadURL
	}
	if strings.TrimSpace(input.ReleaseNotes) == "" {
		input.ReleaseNotes = payload.ReleaseNotes
	}
	if strings.TrimSpace(input.PolicyProfile) == "" {
		input.PolicyProfile = payload.PolicyProfile
	}
	if len(input.AllowedPermissions) == 0 {
		input.AllowedPermissions = payload.AllowedPermissions
	}
	if strings.TrimSpace(input.ExpectedSHA256) == "" {
		input.ExpectedSHA256 = payload.ExpectedSHA256
	}
	if input.MaxBytes <= 0 {
		input.MaxBytes = payload.MaxBytes
	}
}

func newSecurityAuditState(input builtinSecurityAuditInput) *securityAuditState {
	return &securityAuditState{
		input:        input,
		seenFindings: map[string]bool{},
		seenDeps:     map[string]bool{},
		seenPerms:    map[string]bool{},
	}
}

func (s *securityAuditState) audit(ctx context.Context) {
	maxBytes := s.maxBytes()
	if strings.TrimSpace(s.input.ManifestPath) != "" {
		s.auditManifestPath(s.input.ManifestPath, maxBytes)
	}
	if len(s.input.Manifest) > 0 {
		s.auditManifestBytes(s.input.Manifest, "manifest")
	}
	if len(s.input.RegistryEntry) > 0 {
		s.auditManifestBytes(s.input.RegistryEntry, "registry_entry")
	}
	if strings.TrimSpace(s.input.ManifestText) != "" {
		s.auditManifestBytes([]byte(s.input.ManifestText), "manifest_text")
	}
	if strings.TrimSpace(s.input.Path) != "" {
		s.auditPath(ctx, s.input.Path, maxBytes)
	}
	if strings.TrimSpace(s.input.PackagePath) != "" && cleanAbsPath(s.input.PackagePath) != cleanAbsPath(s.input.Path) {
		s.auditPackagePath(ctx, s.input.PackagePath, maxBytes)
	}
	for _, rawURL := range []string{s.input.URL, s.input.DownloadURL} {
		if strings.TrimSpace(rawURL) != "" {
			s.auditURL(rawURL, "input")
		}
	}
	if strings.TrimSpace(s.input.ReleaseNotes) != "" {
		s.scanTextForSensitiveSignals(s.input.ReleaseNotes, "release_notes")
	}
	s.applyPolicyProfile()
}

func (s *securityAuditState) maxBytes() int64 {
	if s.input.MaxBytes > 0 {
		return s.input.MaxBytes
	}
	return defaultSecurityAuditMaxBytes
}

func (s *securityAuditState) auditManifestPath(path string, maxBytes int64) {
	data, err := readFileLimited(path, maxBytes, "security audit manifest")
	if err != nil {
		s.addFinding("manifest_read_failed", "high", "Manifest could not be read", err.Error(), path, "Confirm the manifest path and retry the audit with a readable file.")
		return
	}
	s.addArtifact(builtinSecurityAuditArtifact{Type: "manifest", Path: path, Size: int64(len(data)), SHA256: sha256Hex(data)})
	s.auditManifestBytes(data, path)
}

func (s *securityAuditState) auditManifestBytes(data []byte, location string) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		s.addFinding("manifest_invalid_json", "high", "Manifest JSON is invalid", err.Error(), location, "Publish only valid JSON manifests so agents and installers can enforce policy.")
		s.scanTextForSensitiveSignals(string(data), location)
		return
	}
	if len(manifest) == 0 {
		s.addFinding("manifest_empty", "medium", "Manifest is empty", "{}", location, "Provide name, version, permissions, platform bundles, support, and integrity metadata.")
		return
	}
	s.auditManifestFields(manifest, location)
	if data, err := json.Marshal(manifest); err == nil {
		s.scanTextForSensitiveSignals(string(data), location)
	}
}

func (s *securityAuditState) auditManifestFields(manifest map[string]any, location string) {
	if stringFromMap(manifest, "name") == "" && stringFromMap(manifest, "id") == "" {
		s.addFinding("manifest_missing_name", "medium", "Manifest has no name or id", "name/id is empty", location, "Add a stable capability or skill id before publication.")
	}
	if stringFromMap(manifest, "version") == "" && numberFromMap(manifest, "schema_version") == "" {
		s.addFinding("manifest_missing_version", "medium", "Manifest has no version metadata", "version/schema_version is empty", location, "Add semantic versioning and schema version metadata.")
	}
	if stringFromMap(manifest, "vendor_id") == "" && stringFromMap(manifest, "vendor") == "" {
		s.addFinding("manifest_missing_vendor", "low", "Manifest has no vendor identity", "vendor_id/vendor is empty", location, "Publish vendor identity so users can evaluate provenance and support ownership.")
	}
	if _, ok := manifest["signature"]; !ok {
		s.addFinding("manifest_missing_signature", "low", "Manifest has no signature block", "signature is absent", location, "Attach signature metadata for store review and reproducible release verification.")
	}
	permissions := permissionsFromManifest(manifest, location)
	for _, permission := range permissions {
		s.addPermission(permission)
		s.auditPermission(permission, location)
	}
	s.auditManifestDownloads(manifest, location)
	s.auditManifestDependencies(manifest, location)
	s.auditManifestSupport(manifest, location)
}

func (s *securityAuditState) auditManifestDownloads(manifest map[string]any, location string) {
	platforms, _ := manifest["platforms"].([]any)
	for _, item := range platforms {
		platform, ok := item.(map[string]any)
		if !ok {
			continue
		}
		platformLabel := strings.Join([]string{stringFromMap(platform, "os"), stringFromMap(platform, "arch")}, "/")
		urlValue := stringFromMap(platform, "url")
		shaValue := stringFromMap(platform, "sha256")
		entrypoint := stringFromMap(platform, "entrypoint")
		if urlValue != "" {
			s.auditURL(urlValue, location+" platforms "+platformLabel)
			if shaValue == "" {
				s.addFinding("package_missing_sha256", "high", "Platform package has no sha256", urlValue, location, "Require SHA-256 for every downloadable platform artifact before install or publication.")
			}
		}
		if entrypoint != "" {
			if err := securityAuditValidateRelativePath(entrypoint); err != nil {
				s.addFinding("unsafe_entrypoint", "high", "Platform entrypoint path is unsafe", entrypoint+": "+err.Error(), location, "Use a safe forward-slash relative entrypoint inside the package archive.")
			}
		}
	}
	for _, key := range []string{"url", "download_url", "source_url", "manifest_url"} {
		if value := stringFromMap(manifest, key); value != "" {
			s.auditURL(value, location+" "+key)
		}
	}
}

func (s *securityAuditState) auditManifestDependencies(manifest map[string]any, location string) {
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies"} {
		collectDependenciesFromValue(manifest[key], key, location, s.addDependency)
	}
	if builtin, ok := manifest["builtin"].(map[string]any); ok {
		for _, key := range []string{"backends", "model_profiles"} {
			collectDependenciesFromValue(builtin[key], "builtin."+key, location, s.addDependency)
		}
	}
}

func (s *securityAuditState) auditManifestSupport(manifest map[string]any, location string) {
	support, _ := manifest["support"].(map[string]any)
	incident := ""
	if support != nil {
		incident = stringFromMap(support, "incident_email")
	}
	if incident == "" && stringFromMap(manifest, "incident_email") == "" {
		s.addFinding("missing_incident_contact", "low", "No incident contact is declared", "incident_email is absent", location, "Publish a security or incident contact for vulnerability reports.")
	}
}

func (s *securityAuditState) auditPermission(permission builtinSecurityAuditPermission, location string) {
	if len(s.input.AllowedPermissions) > 0 && !permissionAllowed(permission.Name, s.input.AllowedPermissions) {
		s.addFinding("permission_not_allowed", "high", "Permission is not allowed by policy", permission.Name, location, "Remove the permission or update the policy after human approval.")
	}
	severity, title := permissionRisk(permission.Name)
	if severity == "" {
		return
	}
	s.addFinding("risky_permission_"+normalizeName(permission.Name), severity, title, permission.Name, location, "Review whether this permission is necessary and whether it can be narrowed or made opt-in.")
}

func (s *securityAuditState) auditPath(ctx context.Context, rawPath string, maxBytes int64) {
	path := filepath.Clean(rawPath)
	info, err := os.Stat(path)
	if err != nil {
		s.addFinding("path_read_failed", "high", "Selected path could not be inspected", err.Error(), rawPath, "Confirm the local path and retry the audit.")
		return
	}
	if info.IsDir() {
		s.auditDirectory(ctx, path, maxBytes)
		return
	}
	if looksLikeManifestPath(path) {
		s.auditManifestPath(path, maxBytes)
		return
	}
	s.auditPackagePath(ctx, path, maxBytes)
}

func (s *securityAuditState) auditPackagePath(ctx context.Context, rawPath string, maxBytes int64) {
	path := filepath.Clean(rawPath)
	data, err := readFileLimited(path, maxBytes, "security audit package")
	if err != nil {
		s.addFinding("package_read_failed", "high", "Package could not be read", err.Error(), rawPath, "Confirm the package path and size limit, then retry the audit.")
		return
	}
	artifact := builtinSecurityAuditArtifact{Type: "package", Path: path, Size: int64(len(data)), SHA256: sha256Hex(data)}
	if expected := strings.TrimSpace(s.input.ExpectedSHA256); expected != "" && !strings.EqualFold(expected, artifact.SHA256) {
		s.addFinding("sha256_mismatch", "critical", "Package SHA-256 does not match expected value", "expected="+expected+" actual="+artifact.SHA256, path, "Stop installation and verify the package source before continuing.")
	}
	s.addArtifact(artifact)
	s.inspectArchiveBytes(ctx, path, data)
}

func (s *securityAuditState) auditDirectory(ctx context.Context, root string, maxBytes int64) {
	artifact := builtinSecurityAuditArtifact{Type: "directory", Path: root}
	files := 0
	truncated := false
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			s.warnings = append(s.warnings, "could not inspect "+path+": "+err.Error())
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		files++
		if files > defaultSecurityAuditMaxDirectoryFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		s.inspectPackageEntry(ctx, rel, fileSize(entry), func(limit int64) ([]byte, error) {
			if limit <= 0 || limit > maxBytes {
				limit = minInt64(defaultSecurityAuditMaxFileBytes, maxBytes)
			}
			return readFileLimited(path, limit, "security audit package file")
		})
		return nil
	})
	if err != nil && err != context.Canceled {
		s.warnings = append(s.warnings, "directory scan stopped: "+err.Error())
	}
	artifact.Files = files
	artifact.Truncated = truncated
	s.addArtifact(artifact)
}

func (s *securityAuditState) inspectArchiveBytes(ctx context.Context, path string, data []byte) {
	archiveKind := inferSecurityAuditArchive(path)
	switch archiveKind {
	case "zip":
		s.inspectZip(ctx, path, data)
	case "tar.gz":
		s.inspectTarGz(ctx, path, data)
	case "json":
		s.auditManifestBytes(data, path)
	default:
		s.scanTextForSensitiveSignals(string(data[:minInt(len(data), int(defaultSecurityAuditMaxFileBytes))]), path)
	}
}

func (s *securityAuditState) inspectZip(ctx context.Context, path string, data []byte) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		s.addFinding("archive_invalid", "high", "Package archive could not be decoded", err.Error(), path, "Publish valid zip or tar.gz archives only.")
		return
	}
	seen := map[string]bool{}
	for index, file := range reader.File {
		if index >= defaultSecurityAuditMaxArchiveFiles {
			s.warnings = append(s.warnings, "archive scan reached file limit")
			return
		}
		select {
		case <-ctx.Done():
			s.warnings = append(s.warnings, "archive scan timed out or was cancelled")
			return
		default:
		}
		name := file.Name
		s.inspectArchivePath(name, seen, path)
		if file.FileInfo().IsDir() {
			continue
		}
		entry := file
		s.inspectPackageEntry(ctx, name, int64(entry.UncompressedSize64), func(limit int64) ([]byte, error) {
			if limit <= 0 || limit > defaultSecurityAuditMaxFileBytes {
				limit = defaultSecurityAuditMaxFileBytes
			}
			reader, err := entry.Open()
			if err != nil {
				return nil, err
			}
			defer reader.Close()
			return readAllLimited(reader, limit, "security audit archive entry")
		})
	}
}

func (s *securityAuditState) inspectTarGz(ctx context.Context, path string, data []byte) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		s.addFinding("archive_invalid", "high", "Package archive could not be decoded", err.Error(), path, "Publish valid zip or tar.gz archives only.")
		return
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	seen := map[string]bool{}
	files := 0
	for {
		select {
		case <-ctx.Done():
			s.warnings = append(s.warnings, "archive scan timed out or was cancelled")
			return
		default:
		}
		header, err := reader.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			s.addFinding("archive_invalid", "high", "Package archive could not be decoded", err.Error(), path, "Publish valid zip or tar.gz archives only.")
			return
		}
		files++
		if files > defaultSecurityAuditMaxArchiveFiles {
			s.warnings = append(s.warnings, "archive scan reached file limit")
			return
		}
		s.inspectArchivePath(header.Name, seen, path)
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			if header.Typeflag != tar.TypeDir {
				s.addFinding("unsupported_archive_entry", "medium", "Archive contains unsupported entry type", header.Name, path, "Publish archives containing only regular files and directories.")
			}
			continue
		}
		entryName := header.Name
		entrySize := header.Size
		entryData, readErr := readAllLimited(reader, minInt64(defaultSecurityAuditMaxFileBytes, entrySize), "security audit archive entry")
		if readErr != nil && !IsErrorCode(readErr, CodeSizeLimitExceeded) {
			s.warnings = append(s.warnings, "could not read archive entry "+entryName+": "+readErr.Error())
			continue
		}
		s.inspectPackageEntryData(ctx, entryName, entrySize, entryData)
	}
}

func (s *securityAuditState) inspectArchivePath(name string, seen map[string]bool, location string) {
	if err := securityAuditValidateRelativePath(name); err != nil {
		s.addFinding("unsafe_archive_path", "high", "Archive path is unsafe", name+": "+err.Error(), location, "Remove absolute, parent-directory, backslash, or empty archive paths.")
	}
	key := pathpkg.Clean(name)
	if seen[key] {
		s.addFinding("duplicate_archive_path", "medium", "Archive contains duplicate normalized path", name, location, "Deduplicate archive entries before publication.")
	}
	seen[key] = true
}

func (s *securityAuditState) inspectPackageEntry(ctx context.Context, name string, size int64, read func(int64) ([]byte, error)) {
	select {
	case <-ctx.Done():
		s.warnings = append(s.warnings, "package scan timed out or was cancelled")
		return
	default:
	}
	base := strings.ToLower(pathpkg.Base(filepath.ToSlash(name)))
	if suspiciousPathName(base) {
		s.addFinding("sensitive_file_name", "high", "Package contains a sensitive-looking file", name, name, "Remove secrets, credentials, private keys, and local environment files from packages.")
	}
	if executableOrScriptName(base) {
		s.addFinding("executable_or_script", "medium", "Package contains executable or script content", name, name, "Review executable files and ensure entrypoints are declared in the manifest with hashes.")
	}
	if !shouldReadPackageEntry(base, size) {
		return
	}
	data, err := read(defaultSecurityAuditMaxFileBytes)
	if err != nil {
		if IsErrorCode(err, CodeSizeLimitExceeded) {
			s.warnings = append(s.warnings, "skipped large archive entry: "+name)
			return
		}
		s.warnings = append(s.warnings, "could not read package entry "+name+": "+err.Error())
		return
	}
	s.inspectPackageEntryData(ctx, name, size, data)
}

func (s *securityAuditState) inspectPackageEntryData(ctx context.Context, name string, size int64, data []byte) {
	select {
	case <-ctx.Done():
		s.warnings = append(s.warnings, "package scan timed out or was cancelled")
		return
	default:
	}
	lower := strings.ToLower(filepath.ToSlash(name))
	base := pathpkg.Base(lower)
	if base == "manifest.json" || strings.HasSuffix(lower, "/manifest.json") || base == "skill.json" || base == "plugin.json" {
		s.auditManifestBytes(data, name)
	}
	if base == "package.json" {
		s.auditPackageJSON(data, name)
	}
	if base == "go.mod" || base == "cargo.toml" || base == "requirements.txt" || base == "pyproject.toml" {
		s.auditDependencyFile(data, name)
	}
	s.scanTextForSensitiveSignals(string(data), name)
	if size > defaultSecurityAuditMaxFileBytes {
		s.warnings = append(s.warnings, "scanned only the first bytes of large file: "+name)
	}
}

func (s *securityAuditState) auditPackageJSON(data []byte, location string) {
	var packageJSON map[string]any
	if err := json.Unmarshal(data, &packageJSON); err != nil {
		s.addFinding("package_json_invalid", "medium", "package.json could not be parsed", err.Error(), location, "Publish valid package metadata when JavaScript dependencies are present.")
		return
	}
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		collectDependenciesFromValue(packageJSON[key], key, location, s.addDependency)
	}
	if scripts, ok := packageJSON["scripts"].(map[string]any); ok {
		for _, name := range []string{"preinstall", "install", "postinstall", "prepare"} {
			if script := strings.TrimSpace(stringValue(scripts[name])); script != "" {
				s.addFinding("package_install_script", "high", "package.json runs an install-time script", name+": "+script, location, "Require human review for install-time scripts and prefer script-free packages.")
			}
		}
	}
}

func (s *securityAuditState) auditDependencyFile(data []byte, location string) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.Contains(line, "require ") || strings.Contains(line, "=") || strings.Contains(line, ">=") || strings.Contains(line, "==") {
			s.addDependency(builtinSecurityAuditDependency{Name: firstDependencyToken(line), Source: location})
		}
	}
}

func (s *securityAuditState) auditURL(raw, location string) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		s.addFinding("invalid_url", "medium", "URL is invalid", raw, location, "Use absolute HTTPS URLs for remote packages and registries.")
		return
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return
		}
		s.addFinding("insecure_download_url", "high", "Remote URL does not use HTTPS", raw, location, "Use HTTPS for remote package, manifest, and source URLs.")
	case "file":
		s.addFinding("file_url", "medium", "Manifest references a local file URL", raw, location, "Use store-hosted immutable artifacts for published packages.")
	default:
		s.addFinding("unsupported_url_scheme", "medium", "URL scheme is unusual for package delivery", raw, location, "Use HTTPS unless a store policy explicitly allows this scheme.")
	}
}

func (s *securityAuditState) scanTextForSensitiveSignals(text, location string) {
	lower := strings.ToLower(text)
	for _, signal := range securityAuditSignals() {
		if strings.Contains(lower, signal.Pattern) {
			s.addFinding(signal.ID, signal.Severity, signal.Title, signal.Pattern, location, signal.Recommendation)
		}
	}
}

func (s *securityAuditState) applyPolicyProfile() {
	profile := normalizeName(s.input.PolicyProfile)
	if profile == "" {
		return
	}
	switch profile {
	case "strict", "store", "skill_store", "marketplace":
		if len(s.permissions) == 0 {
			s.addFinding("policy_no_permissions_declared", "medium", "Policy expects explicit permissions", "no permissions declared", "policy", "Declare the permission surface explicitly even when it is empty.")
		}
		if len(s.artifacts) == 0 && strings.TrimSpace(s.input.URL) == "" && strings.TrimSpace(s.input.DownloadURL) == "" {
			s.addFinding("policy_no_artifact", "medium", "Policy expects an auditable artifact or URL", "no package artifact or URL", "policy", "Provide a package archive, manifest URL, or download URL for store review.")
		}
	case "relaxed", "local":
	default:
		s.warnings = append(s.warnings, "unknown policy profile: "+s.input.PolicyProfile)
	}
}

func (s *securityAuditState) output() builtinSecurityAuditOutput {
	summary := builtinSecurityAuditSummary{Artifacts: len(s.artifacts), Permissions: len(s.permissions), Dependencies: len(s.dependencies)}
	score := 0
	for _, finding := range s.findings {
		switch normalizeName(finding.Severity) {
		case "critical":
			summary.Critical++
			score += 100
		case "high":
			summary.High++
			score += 60
		case "medium":
			summary.Medium++
			score += 30
		case "low":
			summary.Low++
			score += 10
		default:
			summary.Informational++
		}
	}
	sortSecurityAuditOutput(s)
	risk := riskLevel(summary)
	recommendations := securityAuditRecommendations(summary)
	return builtinSecurityAuditOutput{
		Kind:            "security_audit",
		RiskLevel:       risk,
		Score:           score,
		Summary:         summary,
		Findings:        s.findings,
		Artifacts:       s.artifacts,
		Permissions:     s.permissions,
		Dependencies:    s.dependencies,
		Recommendations: recommendations,
		Warnings:        dedupeStrings(s.warnings),
		Report:          buildSecurityAuditReport(risk, summary, s.findings, recommendations),
		Metadata: map[string]any{
			"method":              "static_manifest_archive_policy_scan",
			"no_python":           true,
			"remote_downloads":    false,
			"policy_profile":      strings.TrimSpace(s.input.PolicyProfile),
			"allowed_permissions": s.input.AllowedPermissions,
		},
	}
}

func (s *securityAuditState) addFinding(id, severity, title, evidence, location, recommendation string) {
	id = normalizeName(id)
	key := strings.Join([]string{id, severity, title, evidence, location}, "\x00")
	if s.seenFindings[key] {
		return
	}
	s.seenFindings[key] = true
	s.findings = append(s.findings, builtinSecurityAuditFinding{ID: id, Severity: normalizeName(severity), Title: title, Evidence: evidence, Location: location, Recommendation: recommendation})
}

func (s *securityAuditState) addArtifact(artifact builtinSecurityAuditArtifact) {
	if artifact.Type == "" && artifact.Path == "" {
		return
	}
	s.artifacts = append(s.artifacts, artifact)
}

func (s *securityAuditState) addPermission(permission builtinSecurityAuditPermission) {
	permission.Name = strings.TrimSpace(permission.Name)
	if permission.Name == "" {
		return
	}
	if permission.Risk == "" {
		permission.Risk, _ = permissionRisk(permission.Name)
	}
	if permission.Risk == "" {
		permission.Risk = "low"
	}
	key := normalizeName(permission.Name) + "\x00" + permission.Source
	if s.seenPerms[key] {
		return
	}
	s.seenPerms[key] = true
	s.permissions = append(s.permissions, permission)
}

func (s *securityAuditState) addDependency(dep builtinSecurityAuditDependency) {
	dep.Name = strings.TrimSpace(dep.Name)
	if dep.Name == "" {
		return
	}
	key := normalizeName(dep.Name) + "\x00" + strings.TrimSpace(dep.Version) + "\x00" + strings.TrimSpace(dep.Source)
	if s.seenDeps[key] {
		return
	}
	s.seenDeps[key] = true
	s.dependencies = append(s.dependencies, dep)
}

func permissionsFromManifest(manifest map[string]any, location string) []builtinSecurityAuditPermission {
	value, ok := manifest["permissions"]
	if !ok {
		return nil
	}
	out := []builtinSecurityAuditPermission{}
	items, ok := value.([]any)
	if !ok {
		return out
	}
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			out = append(out, builtinSecurityAuditPermission{Name: typed, Source: location})
		case map[string]any:
			out = append(out, builtinSecurityAuditPermission{Name: stringFromMap(typed, "name"), Description: stringFromMap(typed, "description"), Source: location})
		}
	}
	return out
}

func permissionRisk(name string) (string, string) {
	normalized := normalizeName(name)
	switch {
	case containsAny(normalized, "credential", "credentials", "secret", "secrets", "token", "keychain", "cookie", "browser_session"):
		return "critical", "Permission can access secrets or credentials"
	case containsAny(normalized, "shell", "exec", "subprocess", "process_spawn", "local_process"):
		return "high", "Permission can execute local processes"
	case containsAny(normalized, "filesystem_write", "file_write", "write_files", "full_disk", "home_directory", "delete"):
		return "high", "Permission can mutate or broadly access local files"
	case containsAny(normalized, "network", "http", "socket", "browser", "clipboard", "microphone", "camera", "screen"):
		return "medium", "Permission expands data or device access"
	default:
		return "", ""
	}
}

type securityAuditSignal struct {
	ID             string
	Severity       string
	Title          string
	Pattern        string
	Recommendation string
}

func securityAuditSignals() []securityAuditSignal {
	return []securityAuditSignal{
		{ID: "secret_reference", Severity: "high", Title: "Content references secrets or tokens", Pattern: "api_key", Recommendation: "Do not publish secrets, token names with values, or credential-handling code without review."},
		{ID: "secret_reference", Severity: "high", Title: "Content references secrets or tokens", Pattern: "secret_access_key", Recommendation: "Do not publish secrets, token names with values, or credential-handling code without review."},
		{ID: "secret_reference", Severity: "high", Title: "Content references secrets or tokens", Pattern: "private_key", Recommendation: "Remove private keys and credential material from packages."},
		{ID: "dangerous_shell", Severity: "high", Title: "Content contains dangerous shell behavior", Pattern: "rm -rf", Recommendation: "Remove destructive shell commands or require explicit human approval."},
		{ID: "dangerous_shell", Severity: "high", Title: "Content contains dangerous shell behavior", Pattern: "curl | sh", Recommendation: "Avoid pipe-to-shell installers in capability packages."},
		{ID: "dangerous_shell", Severity: "high", Title: "Content contains dangerous shell behavior", Pattern: "invoke-webrequest", Recommendation: "Review PowerShell download/execute flows before publication."},
		{ID: "dynamic_execution", Severity: "medium", Title: "Content uses dynamic execution", Pattern: "eval(", Recommendation: "Avoid dynamic code execution or isolate it behind a documented policy."},
		{ID: "dynamic_execution", Severity: "medium", Title: "Content uses dynamic execution", Pattern: "child_process", Recommendation: "Review child process usage and declare local_process permissions."},
		{ID: "dynamic_execution", Severity: "medium", Title: "Content uses dynamic execution", Pattern: "subprocess", Recommendation: "Review subprocess usage and declare local_process permissions."},
		{ID: "encoded_payload", Severity: "medium", Title: "Content may decode or execute encoded payloads", Pattern: "base64 -d", Recommendation: "Review encoded payload handling and publish source-readable packages."},
	}
}

func collectDependenciesFromValue(value any, source, location string, add func(builtinSecurityAuditDependency)) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			add(builtinSecurityAuditDependency{Name: key, Version: stringValue(typed[key]), Source: location + " " + source})
		}
	case []any:
		for _, item := range typed {
			switch dep := item.(type) {
			case string:
				add(builtinSecurityAuditDependency{Name: dep, Source: location + " " + source})
			case map[string]any:
				add(builtinSecurityAuditDependency{Name: stringFromMap(dep, "name"), Version: stringFromMap(dep, "version"), Source: location + " " + source})
			}
		}
	case string:
		for _, dep := range splitSecurityAuditList(typed) {
			add(builtinSecurityAuditDependency{Name: dep, Source: location + " " + source})
		}
	}
}

func splitSecurityAuditList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstSecurityAuditPathArg(args []string) string {
	skipNext := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if securityAuditArgTakesValue(arg) {
			skipNext = !strings.Contains(arg, "=")
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "://") {
			continue
		}
		return arg
	}
	return ""
}

func firstSecurityAuditURLArg(args []string) string {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "file://") {
			return arg
		}
	}
	return ""
}

func securityAuditArgTakesValue(arg string) bool {
	switch arg {
	case "--manifest", "__manifest", "--manifest-path", "__manifest_path", "--manifest_path", "--path", "__path", "--package", "__package", "--package-path", "__package_path", "--package_path", "--url", "__url", "--download-url", "__download_url", "--download_url", "--expected-sha256", "__expected_sha256", "--sha256", "__sha256", "--policy", "__policy", "--policy-profile", "__policy_profile", "--policy_profile", "--release-notes", "__release_notes", "--release_notes", "--allowed-permissions", "__allowed_permissions", "--allowed_permissions", "--max-bytes", "__max_bytes", "--max_bytes":
		return true
	default:
		return false
	}
}

func looksLikeManifestPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "manifest.json" || base == "skill.json" || base == "plugin.json" || strings.HasSuffix(base, ".manifest.json")
}

func inferSecurityAuditArchive(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".json"):
		return "json"
	default:
		return ""
	}
}

func shouldReadPackageEntry(base string, size int64) bool {
	if size > defaultSecurityAuditMaxFileBytes {
		return false
	}
	if suspiciousPathName(base) {
		return true
	}
	if strings.HasSuffix(base, ".json") || strings.HasSuffix(base, ".toml") || strings.HasSuffix(base, ".txt") || strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".go") || strings.HasSuffix(base, ".js") || strings.HasSuffix(base, ".ts") || strings.HasSuffix(base, ".py") || strings.HasSuffix(base, ".sh") || strings.HasSuffix(base, ".ps1") || strings.HasSuffix(base, ".bat") || strings.HasSuffix(base, ".cmd") {
		return true
	}
	switch base {
	case "dockerfile", "makefile", "go.mod", "go.sum", "cargo.toml", "cargo.lock", "requirements.txt", "pyproject.toml", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return true
	default:
		return false
	}
}

func suspiciousPathName(base string) bool {
	return base == ".env" || strings.Contains(base, "secret") || strings.Contains(base, "credential") || strings.Contains(base, "private_key") || strings.Contains(base, "id_rsa") || strings.Contains(base, "token")
}

func executableOrScriptName(base string) bool {
	for _, suffix := range []string{".exe", ".dll", ".dylib", ".so", ".sh", ".ps1", ".bat", ".cmd", ".vbs", ".js", ".py"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func securityAuditValidateRelativePath(name string) error {
	_, err := cleanArchiveRelativePath(name, "package path")
	return err
}

func permissionAllowed(name string, allowed []string) bool {
	needle := normalizeName(name)
	for _, value := range allowed {
		if normalizeName(value) == needle {
			return true
		}
	}
	return false
}

func riskLevel(summary builtinSecurityAuditSummary) string {
	switch {
	case summary.Critical > 0:
		return "critical"
	case summary.High > 0:
		return "high"
	case summary.Medium > 0:
		return "medium"
	case summary.Low > 0:
		return "low"
	default:
		return "clear"
	}
}

func securityAuditRecommendations(summary builtinSecurityAuditSummary) []string {
	recommendations := []string{}
	if summary.Critical > 0 || summary.High > 0 {
		recommendations = append(recommendations, "Pause installation or publication until high-risk findings are resolved or explicitly approved by a human reviewer.")
	}
	if summary.Medium > 0 {
		recommendations = append(recommendations, "Review medium-risk findings before automated rollout and document accepted risks.")
	}
	if summary.Permissions > 0 {
		recommendations = append(recommendations, "Minimize requested permissions and keep sensitive capabilities opt-in.")
	}
	if summary.Artifacts == 0 {
		recommendations = append(recommendations, "Provide a manifest, package archive, directory, or immutable URL for a stronger audit.")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "No blocking findings were detected by the static scanner; keep human review for production store publication.")
	}
	return recommendations
}

func buildSecurityAuditReport(risk string, summary builtinSecurityAuditSummary, findings []builtinSecurityAuditFinding, recommendations []string) string {
	var builder strings.Builder
	builder.WriteString("# Security Audit\n\n")
	builder.WriteString("Risk level: ")
	builder.WriteString(risk)
	builder.WriteString("\n")
	builder.WriteString("Findings: critical=")
	builder.WriteString(intString(summary.Critical))
	builder.WriteString(" high=")
	builder.WriteString(intString(summary.High))
	builder.WriteString(" medium=")
	builder.WriteString(intString(summary.Medium))
	builder.WriteString(" low=")
	builder.WriteString(intString(summary.Low))
	builder.WriteString("\n\n")
	if len(findings) == 0 {
		builder.WriteString("## Findings\n- No findings detected by static scan.\n")
	} else {
		builder.WriteString("## Findings\n")
		for _, finding := range findings {
			builder.WriteString("- [")
			builder.WriteString(finding.Severity)
			builder.WriteString("] ")
			builder.WriteString(finding.Title)
			if finding.Location != "" {
				builder.WriteString(" (")
				builder.WriteString(finding.Location)
				builder.WriteString(")")
			}
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n## Recommendations\n")
	for _, recommendation := range recommendations {
		builder.WriteString("- ")
		builder.WriteString(recommendation)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func sortSecurityAuditOutput(s *securityAuditState) {
	sort.SliceStable(s.findings, func(i, j int) bool {
		left := severityRank(s.findings[i].Severity)
		right := severityRank(s.findings[j].Severity)
		if left == right {
			if s.findings[i].ID == s.findings[j].ID {
				return s.findings[i].Location < s.findings[j].Location
			}
			return s.findings[i].ID < s.findings[j].ID
		}
		return left < right
	})
	sort.SliceStable(s.dependencies, func(i, j int) bool {
		if s.dependencies[i].Name == s.dependencies[j].Name {
			return s.dependencies[i].Source < s.dependencies[j].Source
		}
		return s.dependencies[i].Name < s.dependencies[j].Name
	})
	sort.SliceStable(s.permissions, func(i, j int) bool {
		if severityRank(s.permissions[i].Risk) == severityRank(s.permissions[j].Risk) {
			return s.permissions[i].Name < s.permissions[j].Name
		}
		return severityRank(s.permissions[i].Risk) < severityRank(s.permissions[j].Risk)
	})
}

func severityRank(value string) int {
	switch normalizeName(value) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func cleanAbsPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func stringFromMap(values map[string]any, key string) string {
	return strings.TrimSpace(stringValue(values[key]))
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprint(typed)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func numberFromMap(values map[string]any, key string) string {
	return stringValue(values[key])
}

func fileSize(entry os.DirEntry) int64 {
	info, err := entry.Info()
	if err != nil {
		return 0
	}
	return info.Size()
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func firstDependencyToken(line string) string {
	line = strings.TrimSpace(line)
	for _, separator := range []string{"==", ">=", "<=", "=", " ", "\t"} {
		if index := strings.Index(line, separator); index > 0 {
			return strings.Trim(line[:index], " \t\"'")
		}
	}
	return strings.Trim(line, " \t\"'")
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	if negative {
		digits = append(digits, '-')
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
