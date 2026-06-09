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
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const Version = "0.1.0-dev"

var pathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

type Service struct {
	Paths           Paths
	Config          Config
	Auth            AuthState
	Registry        Registry
	RegistrySources []RegistrySource
}

func NewService(paths Paths) *Service {
	config := DefaultConfig()
	registry, sources := LoadRegistry(paths, config)
	auth, _ := LoadAuth(paths.AuthFile)
	return &Service{Paths: paths, Config: config, Auth: auth, Registry: registry, RegistrySources: sources}
}

func NewDefaultService() (*Service, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	config, err := LoadConfig(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	auth, _ := LoadAuth(paths.AuthFile)
	registry, sources := LoadRegistry(paths, config)
	return &Service{Paths: paths, Config: config, Auth: auth, Registry: registry, RegistrySources: sources}, nil
}

func (s *Service) InitConfig(force bool) (Config, error) {
	if !force {
		if _, err := os.Stat(s.Paths.ConfigFile); err == nil {
			return s.Config, nil
		} else if err != nil && !os.IsNotExist(err) {
			return s.Config, err
		}
	}
	config := s.Config
	if config.SchemaVersion == 0 {
		config = DefaultConfig()
	}
	if err := SaveConfig(s.Paths.ConfigFile, config); err != nil {
		return config, err
	}
	return config, nil
}

func (s *Service) SetConfig(key, value string) (Config, error) {
	config, err := SetConfigValue(s.Config, key, value)
	if err != nil {
		return s.Config, err
	}
	if err := SaveConfig(s.Paths.ConfigFile, config); err != nil {
		return s.Config, err
	}
	s.Config = config
	s.Registry, s.RegistrySources = LoadRegistry(s.Paths, s.Config)
	return config, nil
}

func (s *Service) UnsetConfig(key string) (Config, error) {
	config, err := UnsetConfigValue(s.Config, key)
	if err != nil {
		return s.Config, err
	}
	if err := SaveConfig(s.Paths.ConfigFile, config); err != nil {
		return s.Config, err
	}
	s.Config = config
	s.Registry, s.RegistrySources = LoadRegistry(s.Paths, s.Config)
	return config, nil
}

func (s *Service) RefreshRegistry(ctx context.Context) (RegistryRefreshResult, error) {
	var result RegistryRefreshResult
	err := s.withMutationLock(func() error {
		refreshed, err := RefreshRegistry(ctx, s.Paths, s.Config)
		if err != nil {
			return err
		}
		result = refreshed
		registry, sources := LoadRegistry(s.Paths, s.Config)
		s.Registry = registry
		s.RegistrySources = sources
		return nil
	})
	return result, err
}

func (s *Service) ValidateRegistry(path string) (RegistryValidation, error) {
	return ValidateRegistryFile(path)
}

func (s *Service) Search(query string, limit int) []SearchResult {
	return s.Registry.Search(query, limit)
}

func (s *Service) List(options ListOptions) (ListResult, error) {
	if !options.Installed && !options.Available {
		options.Installed = true
		options.Available = true
	}
	result := ListResult{}
	if options.Available {
		result.Available = append(result.Available, s.Registry.Skills...)
	}
	if options.Installed {
		installed, err := s.ListInstalled()
		if err != nil {
			return result, err
		}
		result.Installed = installed
	}
	return result, nil
}

func (s *Service) ListInstalled() ([]InstalledSkill, error) {
	entries, err := os.ReadDir(s.Paths.SkillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	installed := make([]InstalledSkill, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		version, err := s.currentVersion(name)
		if err != nil {
			continue
		}
		manifest, path, err := s.readManifest(name, version)
		if err != nil {
			continue
		}
		installed = append(installed, InstalledSkill{Name: name, Version: version, Path: path, Current: true, Manifest: manifest})
	}
	sort.Slice(installed, func(i, j int) bool { return installed[i].Name < installed[j].Name })
	return installed, nil
}

func (s *Service) InstallSkills(ctx context.Context, names []string) ([]InstallResult, error) {
	if len(names) == 0 {
		return nil, NewError(CodeInvalidArgument, "at least one skill name is required", nil)
	}
	results := make([]InstallResult, 0, len(names))
	err := s.withMutationLock(func() error {
		if err := s.ensureCommerceLedgersAppendable(); err != nil {
			return err
		}
		for _, name := range names {
			result, err := s.installSkill(ctx, name)
			if err != nil {
				return err
			}
			results = append(results, result)
			if result.Status == "installed" {
				if _, err := s.appendInstallRecord(installRecordForSkill(result, s.Auth.DeviceID)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return results, err
}

func (s *Service) PlanInstall(names []string) (MutationPlan, error) {
	if len(names) == 0 {
		return MutationPlan{}, NewError(CodeInvalidArgument, "at least one skill name is required", nil)
	}
	plan := MutationPlan{Action: "install", Changes: make([]PlannedChange, 0, len(names))}
	for _, name := range names {
		skill, ok := s.Registry.Find(name)
		if !ok {
			return plan, NewError(CodeNotFound, "skill not found", map[string]any{"skill": name})
		}
		if _, ok := skill.BundleFor(runtime.GOOS, runtime.GOARCH); !ok {
			return plan, NewError(CodePlatformUnsupported, "skill does not support this platform", map[string]any{"skill": skill.Name, "goos": runtime.GOOS, "goarch": runtime.GOARCH})
		}
		current, _ := s.currentVersion(skill.Name)
		status := "install"
		if current == skill.Version {
			status = "already_installed"
		} else if current != "" {
			status = "switch_version"
		}
		plan.Changes = append(plan.Changes, PlannedChange{
			Name:           skill.Name,
			CurrentVersion: current,
			TargetVersion:  skill.Version,
			Status:         status,
			Stub:           skill.Stub,
			Permissions:    permissionNames(skill.Permissions),
			Commerce:       commerceSummary(skill),
			Path:           s.versionDir(skill.Name, skill.Version),
		})
	}
	return plan, nil
}

func (s *Service) installSkill(ctx context.Context, name string) (InstallResult, error) {
	skill, ok := s.Registry.Find(name)
	if !ok {
		return InstallResult{}, NewError(CodeNotFound, "skill not found", map[string]any{"skill": name})
	}
	if _, err := validateSkillManifest(skill); err != nil {
		return InstallResult{}, err
	}
	bundle, ok := skill.BundleFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return InstallResult{}, NewError(CodePlatformUnsupported, "skill does not support this platform", map[string]any{"skill": skill.Name, "goos": runtime.GOOS, "goarch": runtime.GOARCH})
	}

	previous, _ := s.currentVersion(skill.Name)
	versionDir := s.versionDir(skill.Name, skill.Version)
	if previous == skill.Version {
		return InstallResult{Name: skill.Name, Version: skill.Version, Status: "already_installed", Path: versionDir, PreviousVersion: previous, Stub: skill.Stub}, nil
	}

	if _, err := os.Stat(filepath.Join(versionDir, "manifest.json")); err != nil {
		if !os.IsNotExist(err) {
			return InstallResult{}, err
		}
		if err := os.RemoveAll(versionDir); err != nil {
			return InstallResult{}, err
		}
		if err := s.materializeSkill(ctx, skill, bundle, versionDir); err != nil {
			return InstallResult{}, err
		}
	}
	if err := writeFileAtomic(s.currentPath(skill.Name), []byte(skill.Version+"\n"), 0o644); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Name: skill.Name, Version: skill.Version, Status: "installed", Path: versionDir, PreviousVersion: previous, Stub: skill.Stub}, nil
}

func (s *Service) UpgradeSkills(ctx context.Context, names []string) ([]InstallResult, error) {
	var results []InstallResult
	err := s.withMutationLock(func() error {
		if len(names) == 0 {
			installed, err := s.ListInstalled()
			if err != nil {
				return err
			}
			for _, skill := range installed {
				names = append(names, skill.Name)
			}
		}
		if len(names) == 0 {
			return NewError(CodeNotInstalled, "no installed skills to upgrade", nil)
		}
		results = make([]InstallResult, 0, len(names))
		for _, name := range names {
			current, err := s.currentVersion(name)
			if err != nil {
				return err
			}
			skill, ok := s.Registry.Find(name)
			if !ok {
				return NewError(CodeNotFound, "skill not found in registry", map[string]any{"skill": name})
			}
			if current == skill.Version {
				results = append(results, InstallResult{Name: skill.Name, Version: skill.Version, Status: "already_current", Path: s.versionDir(skill.Name, skill.Version), PreviousVersion: current, Stub: skill.Stub})
				continue
			}
			result, err := s.installSkill(ctx, name)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
		return nil
	})
	return results, err
}

func (s *Service) PlanUpgrade(names []string) (MutationPlan, error) {
	if len(names) == 0 {
		installed, err := s.ListInstalled()
		if err != nil {
			return MutationPlan{}, err
		}
		for _, skill := range installed {
			names = append(names, skill.Name)
		}
	}
	if len(names) == 0 {
		return MutationPlan{}, NewError(CodeNotInstalled, "no installed skills to upgrade", nil)
	}
	plan := MutationPlan{Action: "upgrade", Changes: make([]PlannedChange, 0, len(names))}
	for _, name := range names {
		current, err := s.currentVersion(name)
		if err != nil {
			return plan, err
		}
		skill, ok := s.Registry.Find(name)
		if !ok {
			return plan, NewError(CodeNotFound, "skill not found in registry", map[string]any{"skill": name})
		}
		status := "upgrade"
		if current == skill.Version {
			status = "already_current"
		}
		plan.Changes = append(plan.Changes, PlannedChange{
			Name:           skill.Name,
			CurrentVersion: current,
			TargetVersion:  skill.Version,
			Status:         status,
			Stub:           skill.Stub,
			Permissions:    permissionNames(skill.Permissions),
			Commerce:       commerceSummary(skill),
			Path:           s.versionDir(skill.Name, skill.Version),
		})
	}
	return plan, nil
}

func (s *Service) RollbackSkill(name, targetVersion string) (RollbackResult, error) {
	var result RollbackResult
	err := s.withMutationLock(func() error {
		current, err := s.currentVersion(name)
		if err != nil {
			return err
		}
		versions, err := s.installedVersions(name)
		if err != nil {
			return err
		}
		if targetVersion == "" {
			for i := len(versions) - 1; i >= 0; i-- {
				if versions[i] != current {
					targetVersion = versions[i]
					break
				}
			}
		}
		if targetVersion == "" {
			return NewError(CodeNoRollbackTarget, "no previous installed version is available", map[string]any{"skill": name, "current": current})
		}
		found := false
		for _, version := range versions {
			if version == targetVersion {
				found = true
				break
			}
		}
		if !found {
			return NewError(CodeNoRollbackTarget, "requested rollback version is not installed", map[string]any{"skill": name, "version": targetVersion})
		}
		if err := writeFileAtomic(s.currentPath(name), []byte(targetVersion+"\n"), 0o644); err != nil {
			return err
		}
		result = RollbackResult{Name: name, Version: targetVersion, PreviousVersion: current, Path: s.versionDir(name, targetVersion)}
		return nil
	})
	return result, err
}

func (s *Service) UninstallSkill(name string, allVersions bool) (UninstallResult, error) {
	var result UninstallResult
	err := s.withMutationLock(func() error {
		current, err := s.currentVersion(name)
		if err != nil {
			return err
		}
		versions, err := s.installedVersions(name)
		if err != nil {
			return err
		}
		targets := []string{current}
		if allVersions {
			targets = versions
		}
		for _, version := range targets {
			if err := os.RemoveAll(s.versionDir(name, version)); err != nil {
				return err
			}
		}
		remaining, err := s.installedVersions(name)
		if err != nil && !IsErrorCode(err, CodeNotInstalled) {
			return err
		}
		if len(remaining) == 0 {
			if err := os.RemoveAll(filepath.Join(s.Paths.SkillsDir, normalizeName(name))); err != nil {
				return err
			}
		} else {
			next := remaining[len(remaining)-1]
			if next == current && len(remaining) > 1 {
				next = remaining[len(remaining)-2]
			}
			if err := writeFileAtomic(s.currentPath(name), []byte(next+"\n"), 0o644); err != nil {
				return err
			}
		}
		result = UninstallResult{Name: normalizeName(name), RemovedVersions: targets, Status: "uninstalled"}
		return nil
	})
	return result, err
}

func (s *Service) PlanUninstall(name string, allVersions bool) (MutationPlan, error) {
	current, err := s.currentVersion(name)
	if err != nil {
		return MutationPlan{}, err
	}
	versions, err := s.installedVersions(name)
	if err != nil {
		return MutationPlan{}, err
	}
	target := current
	if allVersions {
		target = strings.Join(versions, ",")
	}
	return MutationPlan{
		Action: "uninstall",
		Changes: []PlannedChange{{
			Name:           normalizeName(name),
			CurrentVersion: current,
			TargetVersion:  target,
			Status:         "uninstall",
			Path:           filepath.Join(s.Paths.SkillsDir, normalizeName(name)),
		}},
	}, nil
}

func (s *Service) PlanRollback(name, targetVersion string) (MutationPlan, error) {
	current, err := s.currentVersion(name)
	if err != nil {
		return MutationPlan{}, err
	}
	versions, err := s.installedVersions(name)
	if err != nil {
		return MutationPlan{}, err
	}
	if targetVersion == "" {
		for i := len(versions) - 1; i >= 0; i-- {
			if versions[i] != current {
				targetVersion = versions[i]
				break
			}
		}
	}
	if targetVersion == "" {
		return MutationPlan{}, NewError(CodeNoRollbackTarget, "no previous installed version is available", map[string]any{"skill": name, "current": current})
	}
	found := false
	for _, version := range versions {
		if version == targetVersion {
			found = true
			break
		}
	}
	if !found {
		return MutationPlan{}, NewError(CodeNoRollbackTarget, "requested rollback version is not installed", map[string]any{"skill": name, "version": targetVersion})
	}
	return MutationPlan{
		Action: "rollback",
		Changes: []PlannedChange{{
			Name:           normalizeName(name),
			CurrentVersion: current,
			TargetVersion:  targetVersion,
			Status:         "rollback",
			Path:           s.versionDir(name, targetVersion),
		}},
	}, nil
}

func (s *Service) RunSkill(ctx context.Context, name string, args []string, input []byte) (RunResult, error) {
	return s.RunSkillWithOptions(ctx, name, RunOptions{Args: args, Input: input})
}

func (s *Service) RunSkillWithOptions(ctx context.Context, name string, options RunOptions) (RunResult, error) {
	start := time.Now()
	scenarioID, err := canonicalRunScenarioID(options.ScenarioID)
	if err != nil {
		return RunResult{}, err
	}
	options.ScenarioID = scenarioID
	if options.Timeout <= 0 {
		options.Timeout = time.Duration(s.Config.RunTimeoutMS) * time.Millisecond
	}
	if options.OutputLimitBytes <= 0 {
		options.OutputLimitBytes = s.Config.RunOutputLimitBytes
	}
	if strings.TrimSpace(options.AgentName) == "" {
		options.AgentName = s.Config.AgentName
	}
	if err := validateAgentName(options.AgentName); err != nil {
		return RunResult{}, err
	}
	version, err := s.currentVersion(name)
	if err != nil {
		return RunResult{}, err
	}
	manifest, versionDir, err := s.readManifest(name, version)
	if err != nil {
		return RunResult{}, err
	}
	if err := validateInstalledManifestIdentity(name, version, manifest); err != nil {
		return RunResult{Name: manifest.Name, Version: manifest.Version, Stub: manifest.Stub}, err
	}
	if _, err := validateSkillManifest(manifest); err != nil {
		return RunResult{Name: manifest.Name, Version: manifest.Version, Stub: manifest.Stub}, err
	}
	result := RunResult{Name: manifest.Name, Version: manifest.Version, Stub: manifest.Stub, ScenarioID: scenarioID, InvocationID: NewTraceID()}
	if manifest.Stub {
		result.DurationMS = time.Since(start).Milliseconds()
		return result, NewError(CodeNotImplemented, "skill is installed as a v1 stub; native package is not published yet", map[string]any{"skill": manifest.Name, "version": manifest.Version})
	}
	bundle, ok := manifest.BundleFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return result, NewError(CodePlatformUnsupported, "installed skill does not support this platform", map[string]any{"skill": manifest.Name, "goos": runtime.GOOS, "goarch": runtime.GOARCH})
	}
	if strings.TrimSpace(bundle.Entrypoint) == "" {
		return result, NewError(CodeInvalidArgument, "skill manifest has no entrypoint", map[string]any{"skill": manifest.Name})
	}
	runResult, err := runExecutable(ctx, versionDir, bundle.Entrypoint, options)
	runResult.Name = manifest.Name
	runResult.Version = manifest.Version
	runResult.Stub = false
	runResult.ScenarioID = scenarioID
	runResult.InvocationID = result.InvocationID
	runResult.DurationMS = time.Since(start).Milliseconds()
	runResult.TimeoutMS = options.Timeout.Milliseconds()
	runResult.OutputLimitBytes = options.OutputLimitBytes
	if err != nil {
		return runResult, err
	}
	runResult.AttributedFiles = applyOfficeAttributionForRun(versionDir, options, runResult)
	runResult.UsageEvents = s.recordRunUsage(ctx, manifest, runResult)
	if len(runResult.UsageEvents) > 0 {
		if err := s.withMutationLock(func() error {
			_, err := s.appendBillingRecords(billingRecordsForUsage(manifest, runResult, runResult.UsageEvents))
			return err
		}); err != nil {
			return runResult, err
		}
	}
	return runResult, nil
}

func canonicalRunScenarioID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	scenario, ok := findCapabilityScenario(id)
	if !ok {
		return "", NewError(CodeNotFound, "capability scenario not found", map[string]any{"scenario": id, "supported_scenarios": capabilityScenarioIDs()})
	}
	return scenario.ID, nil
}

func (s *Service) Status() (Status, error) {
	installed, err := s.ListInstalled()
	if err != nil {
		return Status{}, err
	}
	return Status{
		Version:         Version,
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		ConfigDir:       s.Paths.ConfigDir,
		ConfigFile:      s.Paths.ConfigFile,
		CacheDir:        s.Paths.CacheDir,
		SkillsDir:       s.Paths.SkillsDir,
		LogsDir:         s.Paths.LogsDir,
		RegistrySkills:  len(s.Registry.Skills),
		RegistrySources: s.RegistrySources,
		Installed:       len(installed),
		DependencyMode:  "go-stdlib-first,cgo-disabled-release,no-third-party-runtime",
		Channel:         s.Config.Channel,
		Telemetry:       s.Config.Telemetry,
	}, nil
}

func (s *Service) materializeSkill(ctx context.Context, skill SkillManifest, bundle PlatformBundle, versionDir string) error {
	if err := validatePlatformBundle(skill, bundle); err != nil {
		return err
	}
	parent := filepath.Dir(versionDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(parent, "."+filepath.Base(versionDir)+".tmp-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempDir)
		}
	}()

	if !skill.Stub && bundle.URL != "" {
		if bundle.SHA256 == "" {
			return NewError(CodeIntegrityFailed, "non-stub packages must declare sha256", map[string]any{"skill": skill.Name})
		}
		if strings.TrimSpace(bundle.Entrypoint) == "" {
			return NewError(CodeInvalidArgument, "non-stub packages must declare entrypoint", map[string]any{"skill": skill.Name})
		}
		archiveBytes, err := fetchBundleBytes(ctx, bundle.URL, s.Config, s.Paths, time.Duration(s.Config.PackageDownloadTimeoutMS)*time.Millisecond)
		if err != nil {
			return err
		}
		if err := verifySHA256(archiveBytes, bundle.SHA256); err != nil {
			return err
		}
		if err := extractArchive(bytes.NewReader(archiveBytes), int64(len(archiveBytes)), bundle.Archive, bundle.URL, tempDir, s.Config.ExtractedMaxBytes, s.Config.ExtractedMaxFiles); err != nil {
			return err
		}
		if bundle.Entrypoint != "" {
			entrypointPath, err := safeArchivePath(tempDir, bundle.Entrypoint)
			if err != nil {
				return err
			}
			if info, err := os.Stat(entrypointPath); err != nil {
				return NewError(CodeInvalidArgument, "skill entrypoint is missing from package", map[string]any{"entrypoint": bundle.Entrypoint})
			} else if info.IsDir() {
				return NewError(CodeInvalidArgument, "skill entrypoint is a directory", map[string]any{"entrypoint": bundle.Entrypoint})
			}
		}
	}

	manifestBytes, err := json.MarshalIndent(skill, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "manifest.json"), append(manifestBytes, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tempDir, "README.txt"), []byte("This skill is installed by agtx. v1 registry entries may be stubs until native packages are published.\n"), 0o644); err != nil {
		return err
	}

	if err := os.Rename(tempDir, versionDir); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s *Service) withMutationLock(fn func() error) error {
	if err := s.Paths.Ensure(); err != nil {
		return err
	}
	lockPath := filepath.Join(s.Paths.ConfigDir, "agtx.lock")
	timeout := time.Duration(s.Config.LockTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	staleAfter := time.Duration(s.Config.StaleLockMS) * time.Millisecond
	if staleAfter <= 0 {
		staleAfter = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	var lockFile *os.File
	var err error
	for time.Now().Before(deadline) {
		lockFile, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return err
		}
		if staleLock(lockPath, staleAfter) {
			_ = os.Remove(lockPath)
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lockFile == nil {
		return NewError(CodeLockBusy, "another agtx mutation is already in progress", map[string]any{"lock": lockPath})
	}
	_, _ = fmt.Fprintf(lockFile, "pid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	_ = lockFile.Close()
	defer os.Remove(lockPath)
	return fn()
}

func staleLock(lockPath string, staleAfter time.Duration) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) > staleAfter
}

func (s *Service) currentVersion(name string) (string, error) {
	if err := validateSkillName(name); err != nil {
		return "", err
	}
	data, err := readFileLimited(s.currentPath(name), defaultCurrentMaxBytes, "current pointer")
	if err != nil {
		if os.IsNotExist(err) {
			return "", NewError(CodeNotInstalled, "skill is not installed", map[string]any{"skill": name})
		}
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", NewError(CodeNotInstalled, "skill has no current version", map[string]any{"skill": name})
	}
	if err := validatePathSegment("current version", version); err != nil {
		return "", err
	}
	return version, nil
}

func (s *Service) installedVersions(name string) ([]string, error) {
	if err := validateSkillName(name); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.Paths.SkillsDir, normalizeName(name))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewError(CodeNotInstalled, "skill is not installed", map[string]any{"skill": name})
		}
		return nil, err
	}
	versions := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), "manifest.json")); err == nil {
			versions = append(versions, entry.Name())
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersion(versions[i], versions[j]) < 0
	})
	return versions, nil
}

func (s *Service) readManifest(name, version string) (SkillManifest, string, error) {
	versionDir := s.versionDir(name, version)
	if err := validateSkillName(name); err != nil {
		return SkillManifest{}, versionDir, err
	}
	if err := validatePathSegment("skill version", version); err != nil {
		return SkillManifest{}, versionDir, err
	}
	data, err := readFileLimited(filepath.Join(versionDir, "manifest.json"), defaultManifestMaxBytes, "skill manifest")
	if err != nil {
		return SkillManifest{}, versionDir, err
	}
	var manifest SkillManifest
	if err := decodeJSONStrict(data, &manifest); err != nil {
		return SkillManifest{}, versionDir, NewError(CodeInvalidArgument, "invalid skill manifest", err.Error())
	}
	return manifest, versionDir, nil
}

func decodeJSONStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return NewError(CodeInvalidArgument, "JSON input must contain exactly one value", nil)
	}
	return nil
}

func validateInstalledManifestIdentity(name, version string, manifest SkillManifest) error {
	if normalizeName(manifest.Name) != normalizeName(name) {
		return NewError(CodeIntegrityFailed, "installed skill manifest name does not match install directory", map[string]any{"expected": normalizeName(name), "actual": manifest.Name})
	}
	if manifest.Version != version {
		return NewError(CodeIntegrityFailed, "installed skill manifest version does not match current pointer", map[string]any{"expected": version, "actual": manifest.Version})
	}
	return nil
}

func (s *Service) currentPath(name string) string {
	return filepath.Join(s.Paths.SkillsDir, normalizeName(name), "current")
}

func (s *Service) versionDir(name, version string) string {
	return filepath.Join(s.Paths.SkillsDir, normalizeName(name), version)
}

func validateSkillName(name string) error {
	normalized := normalizeName(name)
	if normalized == "" {
		return NewError(CodeInvalidArgument, "skill name is required", nil)
	}
	return validatePathSegment("skill name", normalized)
}

func validatePathSegment(label, value string) error {
	if strings.TrimSpace(value) == "" {
		return NewError(CodeInvalidArgument, label+" is required", nil)
	}
	if strings.TrimSpace(value) != value {
		return NewError(CodeInvalidArgument, label+" must not contain leading or trailing whitespace", map[string]any{"value": value})
	}
	if value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, "\\") || strings.ContainsRune(value, 0) {
		return NewError(CodeInvalidArgument, label+" must be a safe path segment", map[string]any{"value": value})
	}
	if !pathSegmentPattern.MatchString(value) {
		return NewError(CodeInvalidArgument, label+" contains unsupported characters", map[string]any{"value": value})
	}
	return nil
}

func (m SkillManifest) BundleFor(goos, goarch string) (PlatformBundle, bool) {
	for _, bundle := range m.Platforms {
		if bundle.OS == goos && bundle.Arch == goarch {
			return bundle, true
		}
	}
	return PlatformBundle{}, false
}

func fetchBundleBytes(ctx context.Context, url string, config Config, paths Paths, timeout time.Duration) ([]byte, error) {
	if path, ok, err := localBundlePath(url); err != nil {
		return nil, err
	} else if ok {
		return readFileLimited(path, config.PackageMaxBytes, "package")
	}
	if !strings.Contains(url, "://") {
		return readFileLimited(url, config.PackageMaxBytes, "package")
	}
	requestCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	attachAuthHeader(req, config, loadRequestAuth(ctx, paths, config))
	res, err := outboundHTTPClient.Do(req)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return nil, NewError(CodeTimeout, "package download timed out", map[string]any{"url": safeURLForDetails(url), "timeout_ms": timeout.Milliseconds()})
		}
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := readAllLimited(res.Body, defaultAuthMaxBytes, "package error")
		return nil, withProRecoveryDetails(remoteHTTPError("download failed", url, res.StatusCode, res.Status, data), paths, config)
	}
	data, err := readAllLimited(res.Body, config.PackageMaxBytes, "package")
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return nil, NewError(CodeTimeout, "package download timed out", map[string]any{"url": safeURLForDetails(url), "timeout_ms": timeout.Milliseconds()})
		}
		return nil, err
	}
	return data, nil
}

func localBundlePath(raw string) (string, bool, error) {
	if !strings.HasPrefix(raw, "file://") {
		return "", false, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, NewError(CodeInvalidArgument, "file bundle url is invalid", map[string]any{"url": raw})
	}
	if err := validateBundleURL(raw); err != nil {
		return "", false, err
	}
	path := parsed.Path
	if runtime.GOOS == "windows" && strings.HasPrefix(path, "/") && len(path) >= 3 && path[2] == ':' {
		path = strings.TrimPrefix(path, "/")
	}
	return filepath.FromSlash(path), true, nil
}

func verifySHA256(data []byte, expected string) error {
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return NewError(CodeIntegrityFailed, "sha256 mismatch", map[string]any{"expected": expected, "actual": actual})
	}
	return nil
}

type extractBudget struct {
	byteLimit int64
	fileLimit int
	usedBytes int64
	usedFiles int
}

func (b *extractBudget) reserveFile(size int64, path string) error {
	if b.fileLimit > 0 && b.usedFiles >= b.fileLimit {
		return NewError(CodeSizeLimitExceeded, "extracted package exceeds configured file count limit", map[string]any{"path": path, "files": b.usedFiles + 1, "limit": b.fileLimit})
	}
	b.usedFiles++
	if b.byteLimit > 0 && size > 0 {
		if size > b.byteLimit-b.usedBytes {
			return NewError(CodeSizeLimitExceeded, "extracted package exceeds configured size limit", map[string]any{"path": path, "size": b.usedBytes + size, "limit": b.byteLimit})
		}
		b.usedBytes += size
	}
	return nil
}

func extractArchive(reader io.ReaderAt, size int64, archiveType, url, dest string, extractedLimit int64, extractedFileLimit int) error {
	kind := strings.ToLower(strings.TrimSpace(archiveType))
	if extractedLimit <= 0 {
		extractedLimit = defaultExtractedMaxBytes
	}
	if extractedFileLimit <= 0 {
		extractedFileLimit = defaultExtractedMaxFiles
	}
	budget := &extractBudget{byteLimit: extractedLimit, fileLimit: extractedFileLimit}
	if kind == "" {
		inferred, err := inferArchiveType(url)
		if err != nil {
			return err
		}
		kind = inferred
	}
	switch kind {
	case "zip":
		return extractZip(reader, size, dest, budget)
	case "tar.gz", "tgz":
		section := io.NewSectionReader(reader, 0, size)
		return extractTarGz(section, dest, budget)
	default:
		return NewError(CodeInvalidArgument, "unsupported archive type", map[string]any{"archive": archiveType})
	}
}

func extractZip(reader io.ReaderAt, size int64, dest string, budget *extractBudget) error {
	zr, err := zip.NewReader(reader, size)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, file := range zr.File {
		target, err := safeArchivePath(dest, file.Name)
		if err != nil {
			return err
		}
		if err := rejectDuplicateArchivePath(seen, file.Name); err != nil {
			return err
		}
		mode := file.FileInfo().Mode()
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			return NewError(CodeInvalidArgument, "archive contains unsupported file type", map[string]any{"path": file.Name, "mode": mode.String()})
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := budget.reserveFile(int64(file.UncompressedSize64), target); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		err = writeExtractedFile(target, src, mode, int64(file.UncompressedSize64))
		_ = src.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(reader io.Reader, dest string, budget *extractBudget) error {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeArchivePath(dest, header.Name)
		if err != nil {
			return err
		}
		if err := rejectDuplicateArchivePath(seen, header.Name); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := budget.reserveFile(header.Size, target); err != nil {
				return err
			}
			if err := writeExtractedFile(target, tr, os.FileMode(header.Mode), header.Size); err != nil {
				return err
			}
		default:
			return NewError(CodeInvalidArgument, "archive contains unsupported file type", map[string]any{"path": header.Name, "typeflag": header.Typeflag})
		}
	}
}

func safeArchivePath(dest, name string) (string, error) {
	clean, err := cleanArchiveRelativePath(name, "archive path")
	if err != nil {
		return "", err
	}
	target := filepath.Join(append([]string{dest}, strings.Split(clean, "/")...)...)
	if rel, err := filepath.Rel(dest, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", NewError(CodeInvalidArgument, "archive path escapes destination", map[string]any{"path": name})
	}
	return target, nil
}

func cleanArchiveRelativePath(name, label string) (string, error) {
	if strings.TrimSpace(name) == "" || name == "." {
		return "", NewError(CodeInvalidArgument, label+" is empty", map[string]any{"path": name})
	}
	if strings.TrimSpace(name) != name {
		return "", NewError(CodeInvalidArgument, label+" must not contain leading or trailing whitespace", map[string]any{"path": name})
	}
	if strings.ContainsRune(name, 0) {
		return "", NewError(CodeInvalidArgument, label+" contains NUL byte", map[string]any{"path": name})
	}
	if strings.Contains(name, "\\") {
		return "", NewError(CodeInvalidArgument, label+" must use forward slashes", map[string]any{"path": name})
	}
	clean := pathpkg.Clean(name)
	if clean == "." || pathpkg.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", NewError(CodeInvalidArgument, label+" is unsafe", map[string]any{"path": name})
	}
	return clean, nil
}

func rejectDuplicateArchivePath(seen map[string]bool, original string) error {
	key := pathpkg.Clean(original)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	if seen[key] {
		return NewError(CodeInvalidArgument, "archive contains duplicate path", map[string]any{"path": original})
	}
	seen[key] = true
	return nil
}

func writeExtractedFile(path string, src io.Reader, mode os.FileMode, expectedSize int64) error {
	mode = sanitizeExtractedFileMode(mode)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	var copyErr error
	if expectedSize >= 0 {
		_, copyErr = io.CopyN(file, src, expectedSize)
		if copyErr == io.EOF || copyErr == io.ErrUnexpectedEOF {
			copyErr = NewError(CodeInvalidArgument, "archive entry is truncated", map[string]any{"path": path, "expected_size": expectedSize})
		}
	} else {
		_, copyErr = io.Copy(file, src)
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func sanitizeExtractedFileMode(mode os.FileMode) os.FileMode {
	permission := mode.Perm()
	if permission == 0 {
		permission = 0o755
	}
	if permission&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func permissionNames(permissions []Permission) []string {
	names := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		names = append(names, permission.Name)
	}
	sort.Strings(names)
	return names
}

func commerceSummary(skill SkillManifest) *CommerceSummary {
	summary := &CommerceSummary{
		VendorID: skill.VendorID,
	}
	if skill.Capability != nil {
		summary.CapabilityClass = skill.Capability.Class
	}
	if skill.Billing != nil {
		for _, meter := range skill.Billing.Meters {
			if strings.TrimSpace(meter.Meter) != "" {
				summary.BillingMeters = append(summary.BillingMeters, meter.Meter)
			}
		}
		sort.Strings(summary.BillingMeters)
	}
	if skill.Attribution != nil {
		summary.AttributionEvents = append(summary.AttributionEvents, skill.Attribution.Events...)
		sort.Strings(summary.AttributionEvents)
	}
	if skill.Support != nil {
		summary.SupportURL = skill.Support.URL
	}
	if summary.VendorID == "" &&
		summary.CapabilityClass == "" &&
		len(summary.BillingMeters) == 0 &&
		len(summary.AttributionEvents) == 0 &&
		summary.SupportURL == "" {
		return nil
	}
	return summary
}
