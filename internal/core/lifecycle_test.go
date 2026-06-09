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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestInstallListAndRunStub(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	results, err := service.InstallSkills(context.Background(), []string{"pdf"})
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "pdf" || !results[0].Stub {
		t.Fatalf("unexpected install result: %#v", results)
	}
	if _, err := os.Stat(filepath.Join(results[0].Path, "manifest.json")); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}

	installed, err := service.ListInstalled()
	if err != nil {
		t.Fatalf("list installed failed: %v", err)
	}
	if len(installed) != 1 || installed[0].Name != "pdf" {
		t.Fatalf("unexpected installed skills: %#v", installed)
	}

	_, err = service.RunSkill(context.Background(), "pdf", nil, nil)
	if !IsErrorCode(err, CodeNotImplemented) {
		t.Fatalf("expected not_implemented, got %v", err)
	}
}

func TestRollbackToInstalledVersion(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	old, ok := DefaultRegistry().Find("pdf")
	if !ok {
		t.Fatal("default pdf skill missing")
	}
	old.Version = "0.0.9"
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{old}}
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install old version failed: %v", err)
	}
	result, err := service.RollbackSkill("pdf", "0.1.0")
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if result.Version != "0.1.0" || result.PreviousVersion != "0.0.9" {
		t.Fatalf("unexpected rollback result: %#v", result)
	}
}

func TestPlanInstallDoesNotCreateState(t *testing.T) {
	root := t.TempDir()
	service := NewService(PathsForRoot(root))
	plan, err := service.PlanInstall([]string{"pdf"})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Status != "install" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if _, err := os.Stat(filepath.Join(root, "skills")); err == nil {
		t.Fatalf("plan should not create skills dir")
	}
}

func TestInstallLocalZipPackageAndRun(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "echo.zip")
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	result, err := service.RunSkill(context.Background(), "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("run local package failed: %v", err)
	}
	if result.ExitCode != 0 || result.Stdout == "" {
		t.Fatalf("unexpected run result: %#v", result)
	}
}

func TestInstallInfersArchiveTypeWithURLQuery(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, err := writer.Write(archiveBytes); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer server.Close()
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo_query",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        server.URL + "/echo.zip?download=1",
			SHA256:     sum,
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo_query"}); err != nil {
		t.Fatalf("install with query url failed: %v", err)
	}
}

func TestInstallDownloadUnauthorizedIncludesProHints(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"code":"unauthorized","message":"login required"}}`))
	}))
	defer server.Close()

	service := NewService(PathsForRoot(root))
	service.Config.ProAPIURL = server.URL
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "pro_echo",
		Version:       "1.0.0",
		Summary:       "Pro Echo",
		Description:   "Requires auth before package download",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        server.URL + "/echo.zip",
			SHA256:     strings.Repeat("a", 64),
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	_, err := service.InstallSkills(context.Background(), []string{"pro_echo"})
	if !IsErrorCode(err, CodeUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	coreErr := ErrorFrom(err)
	details, ok := coreErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected detail map, got %#v", coreErr.Details)
	}
	setup, ok := details["pro_setup"].(ProSetupResult)
	if !ok || setup.ProAPIURL != server.URL || setup.Authenticated {
		t.Fatalf("expected unauthenticated pro setup details, got %#v", details["pro_setup"])
	}
	actions, ok := details["next_actions"].([]ProSetupAction)
	if !ok || !containsSetupAction(actions, "restart_login") {
		t.Fatalf("expected restart_login recovery action, got %#v", details["next_actions"])
	}
}

func TestInstallLocalFileURLPackageAndRun(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "file_url",
		Version:       "1.0.0",
		Summary:       "File URL",
		Description:   "File URL package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        fileURLForPath(archivePath),
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"file_url"}); err != nil {
		t.Fatalf("install file url package failed: %v", err)
	}
}

func TestInstallRejectsFileURLWithQuery(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "file_url_query",
		Version:       "1.0.0",
		Summary:       "File URL Query",
		Description:   "File URL query package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        fileURLForPath(archivePath) + "?download=1",
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"file_url_query"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for file url query, got %v", err)
	}
}

func TestInstallRejectsInvalidManifestBeforeDownload(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "bad_manifest",
		Version:       "1.0.0",
		Summary:       "Bad",
		Description:   "Bad manifest",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        "https://example.invalid/bad.zip",
			SHA256:     "not-a-sha",
			Archive:    "zip",
			Entrypoint: "bin/tool",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"bad_manifest"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument before download, got %v", err)
	}
}

func TestInstallRejectsUnsupportedBundleURLScheme(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "ftp_bundle",
		Version:       "1.0.0",
		Summary:       "FTP",
		Description:   "Unsupported scheme",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        "ftp://example.invalid/tool.zip",
			SHA256:     strings.Repeat("a", 64),
			Archive:    "zip",
			Entrypoint: "bin/tool",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"ftp_bundle"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for ftp scheme, got %v", err)
	}
}

func TestInstallRejectsRemoteHTTPBundleURL(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "http_bundle",
		Version:       "1.0.0",
		Summary:       "HTTP",
		Description:   "Plain remote HTTP bundle",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        "http://packages.example.com/tool.zip",
			SHA256:     strings.Repeat("a", 64),
			Archive:    "zip",
			Entrypoint: "bin/tool",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"http_bundle"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for remote http bundle, got %v", err)
	}
}

func TestInstallRejectsUnsafeEntrypointManifest(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "unsafe_entry",
		Version:       "1.0.0",
		Summary:       "Unsafe",
		Description:   "Unsafe entrypoint",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        "https://example.invalid/tool.zip",
			SHA256:     strings.Repeat("a", 64),
			Archive:    "zip",
			Entrypoint: "../tool",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"unsafe_entry"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for unsafe entrypoint, got %v", err)
	}
}

func TestRunSkillRejectsMissingEntrypoint(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	if err := os.Remove(filepath.Join(service.versionDir("echo", "1.0.0"), entrypoint)); err != nil {
		t.Fatalf("remove entrypoint: %v", err)
	}
	_, err := service.RunSkill(context.Background(), "echo", nil, nil)
	if !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for missing entrypoint, got %v", err)
	}
	if !strings.Contains(ErrorFrom(err).Message, "entrypoint") {
		t.Fatalf("expected entrypoint message, got %v", err)
	}
}

func TestRunSkillRejectsBackslashEntrypoint(t *testing.T) {
	root := t.TempDir()
	entrypoint := "bin/echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "bin/echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeMultiFilePackage(t, archivePath, map[string]string{entrypoint: scriptContent("echo agtx:$1")})
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("echo", "1.0.0"), "manifest.json")
	manifest, _, err := service.readManifest("echo", "1.0.0")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest.Platforms[0].Entrypoint = strings.ReplaceAll(entrypoint, "/", `\`)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "echo", nil, nil); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for backslash entrypoint, got %v", err)
	}
}

func TestRunSkillRejectsManifestNameMismatch(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("echo", "1.0.0"), "manifest.json")
	manifest, _, err := service.readManifest("echo", "1.0.0")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest.Name = "other"
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "echo", nil, nil); !IsErrorCode(err, CodeIntegrityFailed) {
		t.Fatalf("expected integrity failure for manifest name mismatch, got %v", err)
	}
}

func TestRunSkillRejectsManifestVersionMismatch(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("echo", "1.0.0"), "manifest.json")
	manifest, _, err := service.readManifest("echo", "1.0.0")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest.Version = "2.0.0"
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "echo", nil, nil); !IsErrorCode(err, CodeIntegrityFailed) {
		t.Fatalf("expected integrity failure for manifest version mismatch, got %v", err)
	}
}

func TestRunSkillRejectsUnknownManifestFields(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("echo", "1.0.0"), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1,"name":"echo","version":"1.0.0","summary":"Echo","description":"Echo","platforms":[{"os":"`+runtime.GOOS+`","arch":"`+runtime.GOARCH+`","url":"`+archivePath+`","sha256":"`+sum+`","archive":"zip","entrypoint":"`+entrypoint+`","extra":"nope"}],"stub":false}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "echo", nil, nil); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for unknown manifest field, got %v", err)
	}
}

func TestRunSkillRejectsTrailingManifestJSONValue(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("echo", "1.0.0"), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1,"name":"echo","version":"1.0.0","summary":"Echo","description":"Echo","platforms":[{"os":"`+runtime.GOOS+`","arch":"`+runtime.GOARCH+`","url":"`+archivePath+`","sha256":"`+sum+`","archive":"zip","entrypoint":"`+entrypoint+`"}],"stub":false} {"schema_version":1}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "echo", nil, nil); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for trailing manifest json, got %v", err)
	}
}

func TestRunSkillRejectsNonExecutableEntrypoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has no executable bit")
	}
	root := t.TempDir()
	entrypoint := "echo.sh"
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); err != nil {
		t.Fatalf("install local package failed: %v", err)
	}
	entrypointPath := filepath.Join(service.versionDir("echo", "1.0.0"), entrypoint)
	if err := os.Chmod(entrypointPath, 0o644); err != nil {
		t.Fatalf("chmod entrypoint: %v", err)
	}
	_, err := service.RunSkill(context.Background(), "echo", nil, nil)
	if !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for non-executable entrypoint, got %v", err)
	}
	if !strings.Contains(ErrorFrom(err).Message, "not executable") {
		t.Fatalf("expected not executable message, got %v", err)
	}
}

func TestRunSkillTimeoutAndOutputLimit(t *testing.T) {
	root := t.TempDir()
	entrypoint := "chatty.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "chatty.bat"
	}
	archivePath := filepath.Join(root, "chatty.zip")
	sum := writePackage(t, archivePath, entrypoint, scriptContent("echo 1234567890"))
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "chatty",
		Version:       "1.0.0",
		Summary:       "Chatty",
		Description:   "Chatty test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"chatty"}); err != nil {
		t.Fatalf("install chatty failed: %v", err)
	}
	_, err := service.RunSkillWithOptions(context.Background(), "chatty", RunOptions{OutputLimitBytes: 4, Timeout: time.Second})
	if !IsErrorCode(err, CodeOutputLimitExceeded) {
		t.Fatalf("expected output limit error, got %v", err)
	}
}

func TestRunSkillPassesConfiguredAgentName(t *testing.T) {
	root := t.TempDir()
	entrypoint := "agent.sh"
	command := "agentenv"
	if runtime.GOOS == "windows" {
		entrypoint = "agent.bat"
	}
	archivePath := filepath.Join(root, "agent.zip")
	sum := writePackage(t, archivePath, entrypoint, scriptContent(command))
	service := NewService(PathsForRoot(root))
	service.Config.AgentName = "Codex"
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "agent",
		Version:       "1.0.0",
		Summary:       "Agent",
		Description:   "Agent env test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"agent"}); err != nil {
		t.Fatalf("install agent failed: %v", err)
	}
	result, err := service.RunSkill(context.Background(), "agent", nil, nil)
	if err != nil {
		t.Fatalf("run agent failed: %v", err)
	}
	if normalizeTestOutputLines(result.Stdout) != strings.Join([]string{"Codex", "by Codex", "Codex"}, "\n") {
		t.Fatalf("expected attribution env in skill env, got %q", result.Stdout)
	}
	result, err = service.RunSkillWithOptions(context.Background(), "agent", RunOptions{AgentName: "Cursor"})
	if err != nil {
		t.Fatalf("run agent with override failed: %v", err)
	}
	if normalizeTestOutputLines(result.Stdout) != strings.Join([]string{"Cursor", "by Cursor", "Cursor"}, "\n") {
		t.Fatalf("expected per-run attribution env override, got %q", result.Stdout)
	}
	if _, err := service.RunSkillWithOptions(context.Background(), "agent", RunOptions{AgentName: " Cursor "}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid per-run agent name, got %v", err)
	}
}

func TestRunSkillAppliesOfficeAttribution(t *testing.T) {
	root := t.TempDir()
	templatePath := filepath.Join(root, "template.docx")
	writeMinimalOfficeDocument(t, templatePath)
	outputPath := filepath.Join(root, "out.docx")
	entrypoint := "copydoc.sh"
	command := "copydoc"
	if runtime.GOOS == "windows" {
		entrypoint = "copydoc.bat"
	}
	archivePath := filepath.Join(root, "copydoc.zip")
	sum := writePackage(t, archivePath, entrypoint, scriptContent(command))
	service := NewService(PathsForRoot(root))
	service.Config.AgentName = "Codex"
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "copydoc",
		Version:       "1.0.0",
		Summary:       "Copy doc",
		Description:   "Copy doc test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"copydoc"}); err != nil {
		t.Fatalf("install copydoc failed: %v", err)
	}
	runResult, err := service.RunSkill(context.Background(), "copydoc", []string{templatePath, "--output", outputPath}, nil)
	if err != nil {
		t.Fatalf("run copydoc failed: %v stdout=%q stderr=%q", err, runResult.Stdout, runResult.Stderr)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected copied office document: %v stdout=%q stderr=%q", err, runResult.Stdout, runResult.Stderr)
	}
	if len(runResult.AttributedFiles) != 1 || runResult.AttributedFiles[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected attributed output file, got %#v", runResult.AttributedFiles)
	}
	coreXML := readZipFileStringForTest(t, outputPath, "docProps/core.xml")
	if !strings.Contains(coreXML, "<dc:creator>Codex</dc:creator>") || !strings.Contains(coreXML, "<cp:lastModifiedBy>Codex</cp:lastModifiedBy>") {
		t.Fatalf("expected office attribution metadata, got:\n%s", coreXML)
	}
	if !strings.Contains(coreXML, "by Codex") {
		t.Fatalf("expected byline description, got:\n%s", coreXML)
	}
	templateCoreXML := readZipFileStringForTest(t, templatePath, "docProps/core.xml")
	if strings.Contains(templateCoreXML, "Codex") {
		t.Fatalf("expected source template to remain unattributed, got:\n%s", templateCoreXML)
	}
}

func TestInstallRejectsPackageAboveSizeLimit(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Config.PackageMaxBytes = 8
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); !IsErrorCode(err, CodeSizeLimitExceeded) {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestInstallHTTPPackageDownloadTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(200 * time.Millisecond)
		_, _ = writer.Write([]byte("late"))
	}))
	defer server.Close()
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.PackageDownloadTimeoutMS = 20
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "slow",
		Version:       "1.0.0",
		Summary:       "Slow",
		Description:   "Slow package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        server.URL + "/slow.zip",
			SHA256:     strings.Repeat("a", 64),
			Archive:    "zip",
			Entrypoint: "tool",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"slow"}); !IsErrorCode(err, CodeTimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRefreshRegistryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(200 * time.Millisecond)
		_, _ = writer.Write([]byte(`{"schema_version":1,"skills":[]}`))
	}))
	defer server.Close()
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.RegistryURL = server.URL + "/registry.json"
	service.Config.RegistryDownloadTimeoutMS = 20
	if _, err := service.RefreshRegistry(context.Background()); !IsErrorCode(err, CodeTimeout) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRefreshRegistryWritesCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"remote","version":"1.0.0","summary":"Remote","description":"Remote","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`))
	}))
	defer server.Close()
	root := t.TempDir()
	service := NewService(PathsForRoot(root))
	service.Config.RegistryURL = server.URL + "/registry.json"
	service.Config.RegistryDownloadTimeoutMS = 1000
	result, err := service.RefreshRegistry(context.Background())
	if err != nil {
		t.Fatalf("refresh registry failed: %v", err)
	}
	if result.Bytes == 0 || result.Path == "" {
		t.Fatalf("unexpected refresh result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "cache", "registry", "registry.json")); err != nil {
		t.Fatalf("registry cache was not written: %v", err)
	}
	if _, ok := service.Registry.Find("remote"); !ok {
		t.Fatalf("expected refreshed skill in registry")
	}
}

func TestRefreshRegistrySubscriptionErrorIncludesProHints(t *testing.T) {
	root := t.TempDir()
	paths := PathsForRoot(root)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusPaymentRequired)
		_, _ = writer.Write([]byte(`{"error":{"code":"subscription_required","message":"active subscription required"}}`))
	}))
	defer server.Close()

	if err := SaveAuth(paths.AuthFile, AuthState{SchemaVersion: 1, AccessToken: "access", RefreshToken: "refresh", DeviceID: "device-1"}); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	config := DefaultConfig()
	config.RegistryURL = server.URL + "/registry.json"
	config.ProAPIURL = server.URL

	_, err := RefreshRegistry(context.Background(), paths, config)
	if !IsErrorCode(err, CodeSubscriptionRequired) {
		t.Fatalf("expected subscription_required, got %v", err)
	}
	coreErr := ErrorFrom(err)
	details, ok := coreErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected detail map, got %#v", coreErr.Details)
	}
	setup, ok := details["pro_setup"].(ProSetupResult)
	if !ok || !setup.Authenticated || setup.ProAPIURL != server.URL {
		t.Fatalf("expected authenticated pro setup details, got %#v", details["pro_setup"])
	}
	actions, ok := details["next_actions"].([]ProSetupAction)
	if !ok || !containsSetupAction(actions, "check_status") {
		t.Fatalf("expected check_status recovery action, got %#v", details["next_actions"])
	}
}

func TestInstallRejectsPackageAboveExtractedSizeLimit(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Config.ExtractedMaxBytes = 4
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo",
		Version:       "1.0.0",
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo"}); !IsErrorCode(err, CodeSizeLimitExceeded) {
		t.Fatalf("expected extracted size limit error, got %v", err)
	}
}

func TestInstallRejectsPackageAboveExtractedFileLimit(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "many.zip")
	sum := writeMultiFilePackage(t, archivePath, map[string]string{
		entrypoint:        scriptContent("echo agtx:$1"),
		"extra-one.txt":   "one",
		"extra-two.txt":   "two",
		"extra-three.txt": "three",
	})
	service := NewService(PathsForRoot(root))
	service.Config.ExtractedMaxFiles = 2
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "many",
		Version:       "1.0.0",
		Summary:       "Many files",
		Description:   "Many files test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"many"}); !IsErrorCode(err, CodeSizeLimitExceeded) {
		t.Fatalf("expected extracted file limit error, got %v", err)
	}
}

func TestInstallRejectsZipSymlink(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "symlink.zip")
	sum := writeZipSymlinkPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "symlink",
		Version:       "1.0.0",
		Summary:       "Symlink",
		Description:   "Symlink test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"symlink"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for symlink package, got %v", err)
	}
}

func TestInstallRejectsTarSymlink(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "symlink.tar.gz")
	sum := writeTarSymlinkPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "tarsymlink",
		Version:       "1.0.0",
		Summary:       "Tar symlink",
		Description:   "Tar symlink test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "tar.gz",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"tarsymlink"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for tar symlink package, got %v", err)
	}
}

func TestInstallRejectsDuplicateArchivePath(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "duplicate.zip")
	sum := writeDuplicatePathZipPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "duplicate",
		Version:       "1.0.0",
		Summary:       "Duplicate",
		Description:   "Duplicate path test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"duplicate"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for duplicate path package, got %v", err)
	}
}

func TestInstallRejectsBackslashArchivePath(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "backslash.zip")
	sum := writeMultiFilePackage(t, archivePath, map[string]string{`bin\echo.sh`: scriptContent("echo agtx:$1")})
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "backslash",
		Version:       "1.0.0",
		Summary:       "Backslash",
		Description:   "Backslash path test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: "bin/echo.sh",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"backslash"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for backslash path package, got %v", err)
	}
}

func TestArchivePathCleaningUsesForwardSlashSemantics(t *testing.T) {
	root := t.TempDir()
	entrypoint := "bin/echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "bin/echo.bat"
	}
	archivePath := filepath.Join(root, "clean.zip")
	sum := writeMultiFilePackage(t, archivePath, map[string]string{
		"./bin/../" + entrypoint: scriptContent("echo agtx:$1"),
	})
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "clean",
		Version:       "1.0.0",
		Summary:       "Clean",
		Description:   "Clean path test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"clean"}); err != nil {
		t.Fatalf("install clean path package failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(service.versionDir("clean", "1.0.0"), filepath.FromSlash(entrypoint))); err != nil {
		t.Fatalf("expected cleaned entrypoint: %v", err)
	}
}

func TestInstallRejectsForwardSlashParentEscape(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "escape.zip")
	sum := writeMultiFilePackage(t, archivePath, map[string]string{"../escape.sh": scriptContent("echo nope")})
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "escape",
		Version:       "1.0.0",
		Summary:       "Escape",
		Description:   "Escape path test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: "escape.sh",
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"escape"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for parent escape package, got %v", err)
	}
}

func TestInstallSanitizesArchivedFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows mode bits are not stable")
	}
	root := t.TempDir()
	entrypoint := "bin/echo.sh"
	archivePath := filepath.Join(root, "modes.zip")
	sum := writeModePackage(t, archivePath, map[string]fileSpec{
		entrypoint:    {content: scriptContent("echo agtx:$1"), mode: 0o4777},
		"data/secret": {content: "secret", mode: 0o666},
	})
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "modes",
		Version:       "1.0.0",
		Summary:       "Modes",
		Description:   "Mode sanitization test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"modes"}); err != nil {
		t.Fatalf("install modes package failed: %v", err)
	}
	entryInfo, err := os.Stat(filepath.Join(service.versionDir("modes", "1.0.0"), filepath.FromSlash(entrypoint)))
	if err != nil {
		t.Fatalf("stat entrypoint: %v", err)
	}
	if entryInfo.Mode().Perm() != 0o755 || entryInfo.Mode()&os.ModeSetuid != 0 {
		t.Fatalf("entrypoint mode was not sanitized: %s", entryInfo.Mode())
	}
	dataInfo, err := os.Stat(filepath.Join(service.versionDir("modes", "1.0.0"), "data", "secret"))
	if err != nil {
		t.Fatalf("stat data file: %v", err)
	}
	if dataInfo.Mode().Perm() != 0o644 {
		t.Fatalf("data mode was not sanitized: %s", dataInfo.Mode())
	}
}

func TestInstallRejectsTruncatedTarEntry(t *testing.T) {
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "truncated.tar.gz")
	sum := writeTruncatedTarPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "truncated",
		Version:       "1.0.0",
		Summary:       "Truncated",
		Description:   "Truncated tar test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "tar.gz",
			Entrypoint: entrypoint,
		}},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"truncated"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for truncated package, got %v", err)
	}
}

func TestUninstallSkill(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	plan, err := service.PlanUninstall("pdf", true)
	if err != nil {
		t.Fatalf("plan uninstall failed: %v", err)
	}
	if plan.Action != "uninstall" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	result, err := service.UninstallSkill("pdf", true)
	if err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if len(result.RemovedVersions) != 1 {
		t.Fatalf("unexpected uninstall result: %#v", result)
	}
	if _, err := service.RunSkill(context.Background(), "pdf", nil, nil); !IsErrorCode(err, CodeNotInstalled) {
		t.Fatalf("expected not installed after uninstall, got %v", err)
	}
}

func TestDoctorOnEmptyHomeIsNonMutating(t *testing.T) {
	root := t.TempDir()
	service := NewService(PathsForRoot(filepath.Join(root, "missing")))
	result := service.Doctor()
	if !result.OK {
		t.Fatalf("doctor should tolerate empty home: %#v", result)
	}
	if result.Summary.Warnings == 0 {
		t.Fatalf("expected warnings for missing optional paths: %#v", result)
	}
	if _, err := os.Stat(service.Paths.ConfigDir); err == nil {
		t.Fatalf("doctor should not create config dir")
	}
}

func TestVerifyInstalledStubSkill(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	result, err := service.VerifySkill("pdf")
	if err != nil {
		t.Fatalf("verify failed: %v result=%#v", err, result)
	}
	if !result.OK || !result.Stub || result.Version != "0.1.0" {
		t.Fatalf("unexpected verify result: %#v", result)
	}
}

func TestVerifyDetectsCorruptManifest(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("pdf", "0.1.0"), "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
	result, err := service.VerifySkill("pdf")
	if !IsErrorCode(err, CodeIntegrityFailed) {
		t.Fatalf("expected integrity failure, got %v result=%#v", err, result)
	}
	if result.OK || result.Summary.Errors == 0 {
		t.Fatalf("expected verification errors: %#v", result)
	}
}

func TestRunRejectsOversizedCurrentPointer(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := os.WriteFile(service.currentPath("pdf"), bytes.Repeat([]byte("1"), defaultCurrentMaxBytes+1), 0o644); err != nil {
		t.Fatalf("write oversized current pointer: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "pdf", nil, nil); !IsErrorCode(err, CodeSizeLimitExceeded) {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestRunRejectsUnsafeCurrentPointerVersion(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := os.WriteFile(service.currentPath("pdf"), []byte("../0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write unsafe current pointer: %v", err)
	}
	if _, err := service.RunSkill(context.Background(), "pdf", nil, nil); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestVerifyRejectsOversizedInstalledManifest(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	manifestPath := filepath.Join(service.versionDir("pdf", "0.1.0"), "manifest.json")
	if err := os.WriteFile(manifestPath, bytes.Repeat([]byte("x"), defaultManifestMaxBytes+1), 0o644); err != nil {
		t.Fatalf("write oversized manifest: %v", err)
	}
	result, err := service.VerifySkill("pdf")
	if !IsErrorCode(err, CodeSizeLimitExceeded) {
		t.Fatalf("expected size limit error, got %v result=%#v", err, result)
	}
	if result.OK || result.Summary.Errors == 0 {
		t.Fatalf("expected verification errors: %#v", result)
	}
}

func TestStaleLockIsRemoved(t *testing.T) {
	root := t.TempDir()
	service := NewService(PathsForRoot(root))
	service.Config.StaleLockMS = 1
	service.Config.LockTimeoutMS = 1000
	if err := service.Paths.Ensure(); err != nil {
		t.Fatalf("ensure paths: %v", err)
	}
	lockPath := filepath.Join(service.Paths.ConfigDir, "agtx.lock")
	if err := os.WriteFile(lockPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("chtimes lock: %v", err)
	}
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install should remove stale lock: %v", err)
	}
}

func writeEchoPackage(t *testing.T, archivePath, entrypoint string) string {
	t.Helper()
	return writePackage(t, archivePath, entrypoint, scriptContent("echo agtx:$1"))
}

func fileURLForPath(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file:///" + slashPath
	}
	return "file://" + slashPath
}

func writePackage(t *testing.T, archivePath, entrypoint, content string) string {
	t.Helper()
	return writeMultiFilePackage(t, archivePath, map[string]string{entrypoint: content})
}

func writeMinimalOfficeDocument(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create office document: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`,
		"docProps/core.xml": `<?xml version="1.0" encoding="UTF-8"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>Template</dc:title>
</cp:coreProperties>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p></w:body>
</w:document>`,
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("create office zip entry: %v", err)
		}
		if _, err := writer.Write([]byte(entries[name])); err != nil {
			t.Fatalf("write office zip entry: %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close office zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close office document: %v", err)
	}
}

func readZipFileStringForTest(t *testing.T, archivePath, name string) string {
	t.Helper()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry: %v", err)
		}
		defer handle.Close()
		data, err := io.ReadAll(handle)
		if err != nil {
			t.Fatalf("read zip entry: %v", err)
		}
		return string(data)
	}
	t.Fatalf("zip entry %s not found", name)
	return ""
}

type fileSpec struct {
	content string
	mode    os.FileMode
}

func writeMultiFilePackage(t *testing.T, archivePath string, files map[string]string) string {
	t.Helper()
	specs := make(map[string]fileSpec, len(files))
	for name, content := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".bat") {
			mode = 0o755
		}
		specs[name] = fileSpec{content: content, mode: mode}
	}
	return writeModePackage(t, archivePath, specs)
}

func writeModePackage(t *testing.T, archivePath string, files map[string]fileSpec) string {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(files[name].mode)
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := writer.Write([]byte(files[name].content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeZipSymlinkPackage(t *testing.T, archivePath, entrypoint string) string {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "link-to-entrypoint", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		t.Fatalf("create symlink zip entry: %v", err)
	}
	if _, err := writer.Write([]byte(entrypoint)); err != nil {
		t.Fatalf("write symlink zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeTarSymlinkPackage(t *testing.T, archivePath, entrypoint string) string {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     "link-to-entrypoint",
		Typeflag: tar.TypeSymlink,
		Linkname: entrypoint,
		Mode:     0o777,
	}); err != nil {
		t.Fatalf("write symlink tar header: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeDuplicatePathZipPackage(t *testing.T, archivePath, entrypoint string) string {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	for index, content := range []string{scriptContent("echo first"), scriptContent("echo second")} {
		header := &zip.FileHeader{Name: entrypoint, Method: zip.Deflate}
		header.SetMode(0o755)
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			t.Fatalf("create duplicate zip entry %d: %v", index, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write duplicate zip entry %d: %v", index, err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeTruncatedTarPackage(t *testing.T, archivePath, entrypoint string) string {
	t.Helper()
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte(scriptContent("echo short"))
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     entrypoint,
		Typeflag: tar.TypeReg,
		Mode:     0o755,
		Size:     int64(len(content) + 128),
	}); err != nil {
		t.Fatalf("write truncated tar header: %v", err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatalf("write truncated tar body: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func scriptContent(unixCommand string) string {
	if runtime.GOOS == "windows" {
		switch unixCommand {
		case "echo agtx:$1":
			return "@echo off\r\necho agtx:%1\r\n"
		case "echo 1234567890":
			return "@echo off\r\necho 1234567890\r\n"
		case "echo %AGTX_AGENT_NAME%":
			return "@echo off\r\necho %AGTX_AGENT_NAME%\r\n"
		case "agentenv":
			return "@echo off\r\necho %AGTX_AGENT_NAME%\r\necho %AGTX_BYLINE%\r\necho %AGTX_GENERATED_BY%\r\n"
		case "copydoc":
			return "@echo off\r\ncopy /Y \"%~1\" \"%~3\" >nul\r\n"
		default:
			return "@echo off\r\n" + unixCommand + "\r\n"
		}
	}
	if unixCommand == "agentenv" {
		return "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$AGTX_AGENT_NAME\" \"$AGTX_BYLINE\" \"$AGTX_GENERATED_BY\"\n"
	}
	if unixCommand == "copydoc" {
		return "#!/bin/sh\nset -eu\ncp \"$1\" \"$3\"\n"
	}
	return "#!/bin/sh\n" + unixCommand + "\n"
}

func normalizeTestOutputLines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(value)
}
