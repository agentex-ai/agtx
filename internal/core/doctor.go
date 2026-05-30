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
	}
	checks = append(checks, s.checkInstalledSkills()...)
	return DoctorResult{OK: checksOK(checks), Summary: summarizeChecks(checks), Checks: checks}
}

func (s *Service) VerifySkill(name string) (VerifyResult, error) {
	if strings.TrimSpace(name) == "" {
		return VerifyResult{}, NewError(CodeInvalidArgument, "skill name is required", nil)
	}
	normalized := normalizeName(name)
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
		checks = append(checks, errorCheck("installed_versions", "cannot list installed versions", filepath.Join(s.Paths.SkillsDir, normalized), err))
		result.Checks = checks
		result.Summary = summarizeChecks(checks)
		result.OK = false
		return result, err
	}
	result.InstalledVersions = versions
	checks = append(checks, okCheck("installed_versions", "found installed versions", filepath.Join(s.Paths.SkillsDir, normalized)).withDetails(map[string]any{"versions": versions}))

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
		current, err := s.currentVersion(name)
		if err != nil {
			checks = append(checks, errorCheck("skill_current:"+name, "cannot read current version", s.currentPath(name), err))
			continue
		}
		checks = append(checks, okCheck("skill_current:"+name, "current version is "+current, s.currentPath(name)))
		_, _, versionChecks, _ := s.checkSkillVersion(name, current, true)
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

func (s *Service) checkSkillVersion(name, version string, currentOnly bool) (SkillManifest, string, []DoctorCheck, error) {
	versionDir := s.versionDir(name, version)
	manifestPath := filepath.Join(versionDir, "manifest.json")
	checks := []DoctorCheck{}
	manifest, _, err := s.readManifest(name, version)
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

	if normalizeName(manifest.Name) != normalizeName(name) {
		checks = append(checks, DoctorCheck{Name: "manifest_identity", OK: false, Severity: "error", Message: "manifest name does not match install directory", Path: manifestPath, Details: map[string]any{"expected": normalizeName(name), "actual": manifest.Name}})
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
