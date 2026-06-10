package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (s *Service) Doctor() DoctorResult {
	checks := []DoctorCheck{
		s.checkDirectory("config_dir", s.Paths.ConfigDir, true),
		s.checkDirectory("cache_dir", s.Paths.CacheDir, true),
		s.checkDirectory("registry_dir", s.Paths.RegistryDir, true),
		s.checkDirectory("skills_dir", s.Paths.SkillsDir, true),
		s.checkDirectory("logs_dir", s.Paths.LogsDir, true),
		s.checkConfigFile(),
		s.checkRegistrySources(),
		s.checkRegistryManifests(),
		s.checkAuthFile(),
	}
	checks = append(checks, s.checkCommerceLedgers()...)
	checks = append(checks, s.checkInstalledSkills()...)
	return DoctorResult{OK: checksOK(checks), Summary: summarizeChecks(checks), Checks: checks}
}

func (s *Service) VerifySkill(name string) (VerifyResult, error) {
	if strings.TrimSpace(name) == "" {
		return VerifyResult{}, NewError(CodeInvalidArgument, "skill name is required", nil)
	}
	normalized := canonicalSkillName(name)
	result := VerifyResult{Name: normalized}
	checks := []DoctorCheck{}

	current, err := s.currentVersion(name)
	if err != nil {
		checks = append(checks, errorCheck("current_pointer", "skill is not installed", s.currentPath(name), err))
		result.Checks = checks
		result.Summary = summarizeChecks(checks)
		result.OK = false
		return result, err
	}
	result.Version = current
	result.Path = s.versionDir(name, current)
	checks = append(checks, okCheck("current_pointer", "current version is "+current, s.currentPath(name)))

	versions, err := s.installedVersions(name)
	if err != nil {
		checks = append(checks, errorCheck("installed_versions", "cannot list installed versions", s.skillDir(name), err))
		result.Checks = checks
		result.Summary = summarizeChecks(checks)
		result.OK = false
		return result, err
	}
	result.InstalledVersions = versions
	checks = append(checks, okCheck("installed_versions", "found installed versions", s.skillDir(name)).withDetails(map[string]any{"versions": versions}))

	currentFound := false
	for _, version := range versions {
		if version == current {
			currentFound = true
			break
		}
	}
	if currentFound {
		checks = append(checks, okCheck("current_version_dir", "current version directory exists", result.Path))
	} else {
		checks = append(checks, DoctorCheck{Name: "current_version_dir", OK: false, Severity: "error", Message: "current version directory is missing", Path: result.Path})
	}

	manifest, versionDir, manifestChecks, manifestErr := s.checkSkillVersion(name, current, true)
	checks = append(checks, manifestChecks...)
	result.Path = versionDir
	result.Stub = manifest.Stub

	result.Checks = checks
	result.Summary = summarizeChecks(checks)
	result.OK = checksOK(checks)
	if manifestErr != nil && IsErrorCode(manifestErr, CodeSizeLimitExceeded) {
		return result, manifestErr
	}
	if !result.OK {
		return result, NewError(CodeIntegrityFailed, "skill verification failed", map[string]any{"skill": normalized, "version": current, "summary": result.Summary, "checks": result.Checks})
	}
	return result, nil
}

func (s *Service) checkDirectory(name, path string, warnIfMissing bool) DoctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) && warnIfMissing {
			return DoctorCheck{Name: name, OK: true, Severity: "warning", Message: "directory does not exist yet; it will be created when needed", Path: path}
		}
		return errorCheck(name, "cannot access directory", path, err)
	}
	if !info.IsDir() {
		return DoctorCheck{Name: name, OK: false, Severity: "error", Message: "path is not a directory", Path: path}
	}
	return okCheck(name, "directory is accessible", path)
}

func (s *Service) checkConfigFile() DoctorCheck {
	if _, err := os.Stat(s.Paths.ConfigFile); err != nil {
		if os.IsNotExist(err) {
			return DoctorCheck{Name: "config_file", OK: true, Severity: "warning", Message: "config file does not exist; defaults are active", Path: s.Paths.ConfigFile}
		}
		return errorCheck("config_file", "cannot access config file", s.Paths.ConfigFile, err)
	}
	if _, err := LoadConfig(s.Paths.ConfigFile); err != nil {
		return errorCheck("config_file", "config file is invalid", s.Paths.ConfigFile, err)
	}
	return okCheck("config_file", "config file is valid", s.Paths.ConfigFile)
}

func (s *Service) checkAuthFile() DoctorCheck {
	if _, err := os.Stat(s.Paths.AuthFile); err != nil {
		if os.IsNotExist(err) {
			return DoctorCheck{Name: "auth_file", OK: true, Severity: "info", Message: "auth file does not exist; Pro is not logged in", Path: s.Paths.AuthFile}
		}
		return errorCheck("auth_file", "cannot access auth file", s.Paths.AuthFile, err)
	}
	auth, err := LoadAuth(s.Paths.AuthFile)
	if err != nil {
		return errorCheck("auth_file", "auth file is invalid; run agtx pro logout then agtx pro login", s.Paths.AuthFile, err)
	}
	if strings.TrimSpace(auth.AccessToken) == "" {
		return DoctorCheck{Name: "auth_file", OK: true, Severity: "info", Message: "auth file is valid; Pro is not logged in", Path: s.Paths.AuthFile}
	}
	details := map[string]any{
		"device_id":   auth.DeviceID,
		"device_name": auth.DeviceName,
		"expires_at":  auth.ExpiresAt,
		"pending":     auth.Pending != nil,
	}
	if accessTokenExpiredSoon(auth.ExpiresAt) {
		return DoctorCheck{Name: "auth_file", OK: true, Severity: "warning", Message: "auth file is valid but access token is expired or near expiry", Path: s.Paths.AuthFile, Details: details}
	}
	return okCheck("auth_file", "auth file is valid", s.Paths.AuthFile).withDetails(details)
}

func (s *Service) checkRegistrySources() DoctorCheck {
	loaded := 0
	errored := []RegistrySource{}
	for _, source := range s.RegistrySources {
		if source.Loaded {
			loaded++
		}
		if source.Error != "" {
			errored = append(errored, source)
		}
	}
	details := map[string]any{"sources": s.RegistrySources, "loaded": loaded}
	if len(errored) > 0 {
		return DoctorCheck{Name: "registry_sources", OK: true, Severity: "warning", Message: "one or more registry overlays could not be loaded", Details: details}
	}
	return DoctorCheck{Name: "registry_sources", OK: true, Severity: "info", Message: "registry sources loaded", Details: details}
}

func (s *Service) checkRegistryManifests() DoctorCheck {
	warnings := validateRegistry(s.Registry)
	if len(warnings) > 0 {
		return DoctorCheck{Name: "registry_manifests", OK: true, Severity: "warning", Message: "registry contains manifest warnings", Details: map[string]any{"warnings": warnings, "skills": len(s.Registry.Skills)}}
	}
	return DoctorCheck{Name: "registry_manifests", OK: true, Severity: "info", Message: "registry manifests are valid", Details: map[string]any{"skills": len(s.Registry.Skills)}}
}

func (s *Service) checkInstalledSkills() []DoctorCheck {
	root := s.Paths.SkillsDir
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []DoctorCheck{okCheck("installed_skills", "no installed skills", root)}
		}
		return []DoctorCheck{errorCheck("installed_skills", "cannot list installed skills", root, err)}
	}
	checks := []DoctorCheck{}
	skillDirs := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDirs++
		name := entry.Name()
		dir := s.directSkillDir(name)
		currentPath := filepath.Join(dir, "current")
		current, err := s.currentVersionInDir(name, dir)
		if err != nil {
			checks = append(checks, errorCheck("skill_current:"+name, "cannot read current version", currentPath, err))
			continue
		}
		checks = append(checks, okCheck("skill_current:"+name, "current version is "+current, currentPath))
		_, _, versionChecks, _ := s.checkSkillVersionInDir(name, current, true, dir)
		for _, check := range versionChecks {
			check.Name = name + ":" + check.Name
			checks = append(checks, check)
		}
	}
	if skillDirs == 0 {
		checks = append(checks, okCheck("installed_skills", "no installed skills", root))
	}
	return checks
}

func (s *Service) checkCommerceLedgers() []DoctorCheck {
	checks := []DoctorCheck{
		s.checkLedgerIntegrity("commerce_install_ledger", s.InstallRecordIntegrity),
		s.checkLedgerIntegrity("commerce_billing_ledger", s.BillingRecordIntegrity),
		s.checkLedgerIntegrity("commerce_receipt_ledger", s.CommerceReceiptIntegrity),
	}
	checks = append(checks, s.checkLedgerPrivatePaths()...)
	return checks
}

func (s *Service) checkLedgerIntegrity(name string, verify func() (LedgerIntegritySummary, error)) DoctorCheck {
	summary, err := verify()
	if err != nil {
		return errorCheck(name, "cannot verify local commerce ledger", "", err)
	}
	message := "local commerce ledger integrity is " + summary.Status
	check := DoctorCheck{Name: name, OK: true, Severity: "info", Message: message, Details: summary}
	switch summary.Status {
	case integrityStatusVerified, integrityStatusEmpty:
		return check
	case integrityStatusLegacyUnsigned:
		check.Severity = "warning"
		check.Message = "local commerce ledger contains legacy unsigned records"
		return check
	default:
		check.OK = false
		check.Severity = "error"
		if strings.TrimSpace(summary.Reason) != "" {
			check.Message = "local commerce ledger integrity failed: " + summary.Reason
		} else {
			check.Message = "local commerce ledger integrity failed"
		}
		return check
	}
}

func (s *Service) checkLedgerPrivatePaths() []DoctorCheck {
	checks := []DoctorCheck{}
	for _, dir := range s.ledgerPrivateDirs() {
		checks = append(checks, checkLedgerPrivatePath("commerce_ledger_dir", dir, true, ledgerPrivateDirMode))
	}
	for _, path := range s.ledgerSensitivePaths() {
		checks = append(checks, checkLedgerPrivatePath("commerce_ledger_file", path, false, ledgerPrivateFileMode))
	}
	return checks
}

func checkLedgerPrivatePath(name, path string, directory bool, expected os.FileMode) DoctorCheck {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			severity := "info"
			if directory {
				severity = "warning"
			}
			return DoctorCheck{Name: name, OK: true, Severity: severity, Message: "commerce ledger path does not exist yet", Path: path}
		}
		return errorCheck(name, "cannot access commerce ledger path", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return DoctorCheck{Name: name, OK: false, Severity: "error", Message: "commerce ledger path must not be a symlink", Path: path}
	}
	if directory && !info.IsDir() {
		return DoctorCheck{Name: name, OK: false, Severity: "error", Message: "commerce ledger path is not a directory", Path: path}
	}
	if !directory && info.IsDir() {
		return DoctorCheck{Name: name, OK: false, Severity: "error", Message: "commerce ledger file path is a directory", Path: path}
	}
	if runtime.GOOS == "windows" {
		return okCheck(name, "commerce ledger path exists; POSIX permission check skipped", path)
	}
	permission := info.Mode().Perm()
	if permission&^expected != 0 {
		return DoctorCheck{
			Name:     name,
			OK:       false,
			Severity: "error",
			Message:  "commerce ledger path is too permissive",
			Path:     path,
			Details: map[string]any{
				"actual_mode":   permission.String(),
				"expected_mode": expected.String(),
			},
		}
	}
	return okCheck(name, "commerce ledger path permissions are private", path).withDetails(map[string]any{
		"mode": permission.String(),
	})
}

func (s *Service) checkSkillVersion(name, version string, currentOnly bool) (SkillManifest, string, []DoctorCheck, error) {
	return s.checkSkillVersionInDir(name, version, currentOnly, s.skillDir(name))
}

func (s *Service) checkSkillVersionInDir(name, version string, currentOnly bool, dir string) (SkillManifest, string, []DoctorCheck, error) {
	versionDir := filepath.Join(dir, version)
	manifestPath := filepath.Join(versionDir, "manifest.json")
	checks := []DoctorCheck{}
	manifest, _, err := s.readManifestInDir(name, version, dir)
	if err != nil {
		checks = append(checks, errorCheck("manifest", "cannot read skill manifest", manifestPath, err))
		return SkillManifest{}, versionDir, checks, err
	}
	checks = append(checks, okCheck("manifest", "skill manifest is readable", manifestPath))

	warnings, err := validateSkillManifest(manifest)
	if err != nil {
		checks = append(checks, errorCheck("manifest_schema", "skill manifest is invalid", manifestPath, err))
	} else if len(warnings) > 0 {
		checks = append(checks, DoctorCheck{Name: "manifest_schema", OK: true, Severity: "warning", Message: "skill manifest has warnings", Path: manifestPath, Details: map[string]any{"warnings": warnings}})
	} else {
		checks = append(checks, okCheck("manifest_schema", "skill manifest schema is valid", manifestPath))
	}

	if canonicalSkillName(manifest.Name) != canonicalSkillName(name) {
		checks = append(checks, DoctorCheck{Name: "manifest_identity", OK: false, Severity: "error", Message: "manifest name does not match install directory", Path: manifestPath, Details: map[string]any{"expected": canonicalSkillName(name), "actual": manifest.Name}})
	} else if manifest.Version != version {
		checks = append(checks, DoctorCheck{Name: "manifest_identity", OK: false, Severity: "error", Message: "manifest version does not match install directory", Path: manifestPath, Details: map[string]any{"expected": version, "actual": manifest.Version}})
	} else {
		checks = append(checks, okCheck("manifest_identity", "manifest identity matches install path", manifestPath))
	}

	if manifest.Stub {
		checks = append(checks, DoctorCheck{Name: "entrypoint", OK: true, Severity: "info", Message: "stub skill does not require an entrypoint", Path: versionDir})
		return manifest, versionDir, checks, nil
	}

	bundle, ok := manifest.BundleFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		checks = append(checks, DoctorCheck{Name: "platform", OK: false, Severity: "error", Message: "installed skill does not support this platform", Path: manifestPath, Details: map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH}})
		return manifest, versionDir, checks, nil
	}
	checks = append(checks, okCheck("platform", "installed skill supports this platform", manifestPath).withDetails(map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH}))

	if strings.TrimSpace(bundle.Entrypoint) == "" {
		checks = append(checks, DoctorCheck{Name: "entrypoint", OK: false, Severity: "error", Message: "non-stub skill has no entrypoint", Path: manifestPath})
		return manifest, versionDir, checks, nil
	}
	entrypointPath, err := safeArchivePath(versionDir, bundle.Entrypoint)
	if err != nil {
		checks = append(checks, errorCheck("entrypoint", "entrypoint path is unsafe", manifestPath, err))
		return manifest, versionDir, checks, nil
	}
	info, err := os.Stat(entrypointPath)
	if err != nil {
		checks = append(checks, errorCheck("entrypoint", "entrypoint is missing", entrypointPath, err))
		return manifest, versionDir, checks, nil
	}
	if info.IsDir() {
		checks = append(checks, DoctorCheck{Name: "entrypoint", OK: false, Severity: "error", Message: "entrypoint is a directory", Path: entrypointPath})
		return manifest, versionDir, checks, nil
	}
	checks = append(checks, okCheck("entrypoint", "entrypoint exists", entrypointPath))
	if currentOnly {
		checks = append(checks, s.checkExecutableBit(entrypointPath))
	}
	return manifest, versionDir, checks, nil
}

func (s *Service) checkExecutableBit(path string) DoctorCheck {
	if runtime.GOOS == "windows" {
		return okCheck("entrypoint_executable", "windows executable bit check skipped", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return errorCheck("entrypoint_executable", "cannot inspect entrypoint permissions", path, err)
	}
	if info.Mode()&0o111 == 0 {
		return DoctorCheck{Name: "entrypoint_executable", OK: false, Severity: "error", Message: "entrypoint is not executable", Path: path}
	}
	return okCheck("entrypoint_executable", "entrypoint is executable", path)
}

func okCheck(name, message, path string) DoctorCheck {
	return DoctorCheck{Name: name, OK: true, Severity: "info", Message: message, Path: path}
}

func errorCheck(name, message, path string, err error) DoctorCheck {
	details := any(nil)
	if err != nil {
		details = map[string]any{"error": err.Error()}
	}
	return DoctorCheck{Name: name, OK: false, Severity: "error", Message: message, Path: path, Details: details}
}

func (c DoctorCheck) withDetails(details any) DoctorCheck {
	c.Details = details
	return c
}

func summarizeChecks(checks []DoctorCheck) DiagnosticSummary {
	summary := DiagnosticSummary{Checks: len(checks)}
	for _, check := range checks {
		if check.OK {
			summary.Passed++
		}
		switch check.Severity {
		case "warning":
			summary.Warnings++
		case "error":
			summary.Errors++
		}
	}
	return summary
}

func checksOK(checks []DoctorCheck) bool {
	for _, check := range checks {
		if !check.OK || check.Severity == "error" {
			return false
		}
	}
	return true
}
