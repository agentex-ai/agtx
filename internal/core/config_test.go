package core

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaultsWhenMissing(t *testing.T) {
	config, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if config.Channel != "stable" || config.Telemetry != "off" {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestLoadConfigRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), defaultConfigMaxBytes+1), 0o644); err != nil {
		t.Fatalf("write oversized config: %v", err)
	}
	if _, err := LoadConfig(path); !IsErrorCode(err, CodeSizeLimitExceeded) {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"telemetry":"off","typo":true}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestLoadConfigRejectsTrailingJSONValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1} {"schema_version":1}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadConfig(path); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	tests := map[string]string{
		"bad registry url":    `{"schema_version":1,"registry_url":"file:///tmp/registry.json"}`,
		"bad telemetry":       `{"schema_version":1,"telemetry":"verbose"}`,
		"bad timeout":         `{"schema_version":1,"run_timeout_ms":0}`,
		"bad schema version":  `{"schema_version":2}`,
		"null registry files": `{"schema_version":1,"registry_files":null}`,
		"empty registry file": `{"schema_version":1,"registry_files":[""]}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if _, err := LoadConfig(path); !IsErrorCode(err, CodeInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestLoadConfigAllowsPartialStrictConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"registry_url":"https://example.com/registry.json","registry_files":["b.json","a.json","a.json"],"telemetry":"desensitized","registry_download_timeout_ms":1234}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.Telemetry != "desensitized" || config.RegistryDownloadTimeoutMS != 1234 {
		t.Fatalf("unexpected loaded config: %#v", config)
	}
	if len(config.RegistryFiles) != 2 || config.RegistryFiles[0] != "a.json" || config.RegistryFiles[1] != "b.json" {
		t.Fatalf("registry files not normalized: %#v", config.RegistryFiles)
	}
}

func TestLoadRegistryOverlayFromConfigFile(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, "registry.json")
	data := []byte(`{
  "schema_version": 1,
  "skills": [
    {
      "schema_version": 1,
      "name": "pdf",
      "version": "9.9.9",
      "summary": "Custom PDF",
      "description": "overlay",
      "platforms": [{"os":"darwin","arch":"arm64"}],
      "stub": true
    },
    {
      "schema_version": 1,
      "name": "custom_skill",
      "version": "0.1.0",
      "summary": "Custom",
      "description": "custom",
      "platforms": [{"os":"darwin","arch":"arm64"}],
      "stub": true
    }
  ]
}`)
	if err := os.WriteFile(registryPath, data, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	paths := PathsForRoot(root)
	registry, sources := LoadRegistry(paths, Config{RegistryFiles: []string{registryPath}, Channel: "stable", Telemetry: "off"})
	if len(sources) < 2 || !sources[1].Loaded {
		t.Fatalf("expected loaded file source: %#v", sources)
	}
	pdf, ok := registry.Find("pdf")
	if !ok || pdf.Version != "9.9.9" {
		t.Fatalf("expected pdf overlay, got %#v", pdf)
	}
	if _, ok := registry.Find("custom_skill"); !ok {
		t.Fatalf("expected custom skill")
	}
}

func TestSetAndUnsetConfigValue(t *testing.T) {
	config, err := SetConfigValue(DefaultConfig(), "registry_files", "a.json,b.json")
	if err != nil {
		t.Fatalf("set registry files: %v", err)
	}
	if len(config.RegistryFiles) != 2 {
		t.Fatalf("unexpected registry files: %#v", config.RegistryFiles)
	}
	config, err = SetConfigValue(config, "telemetry", "desensitized")
	if err != nil {
		t.Fatalf("set telemetry: %v", err)
	}
	if config.Telemetry != "desensitized" {
		t.Fatalf("unexpected telemetry: %s", config.Telemetry)
	}
	config, err = UnsetConfigValue(config, "telemetry")
	if err != nil {
		t.Fatalf("unset telemetry: %v", err)
	}
	if config.Telemetry != "off" {
		t.Fatalf("unexpected telemetry after unset: %s", config.Telemetry)
	}
	config, err = SetConfigValue(config, "registry_url", "https://example.com/registry.json")
	if err != nil {
		t.Fatalf("set registry url: %v", err)
	}
	if config.RegistryURL != "https://example.com/registry.json" {
		t.Fatalf("unexpected registry url: %s", config.RegistryURL)
	}
	if _, err := SetConfigValue(config, "registry_url", "file:///tmp/registry.json"); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid registry url scheme, got %v", err)
	}
	config, err = SetConfigValue(config, "registry_download_timeout_ms", "1234")
	if err != nil {
		t.Fatalf("set registry download timeout: %v", err)
	}
	if config.RegistryDownloadTimeoutMS != 1234 {
		t.Fatalf("unexpected registry download timeout: %d", config.RegistryDownloadTimeoutMS)
	}
	config, err = UnsetConfigValue(config, "registry_download_timeout_ms")
	if err != nil {
		t.Fatalf("unset registry download timeout: %v", err)
	}
	if config.RegistryDownloadTimeoutMS != defaultRegistryDownloadTimeoutMS {
		t.Fatalf("unexpected registry download timeout after unset: %d", config.RegistryDownloadTimeoutMS)
	}
	config, err = SetConfigValue(config, "package_max_bytes", "1024")
	if err != nil {
		t.Fatalf("set package max bytes: %v", err)
	}
	if config.PackageMaxBytes != 1024 {
		t.Fatalf("unexpected package max bytes: %d", config.PackageMaxBytes)
	}
	config, err = UnsetConfigValue(config, "package_max_bytes")
	if err != nil {
		t.Fatalf("unset package max bytes: %v", err)
	}
	if config.PackageMaxBytes != defaultPackageMaxBytes {
		t.Fatalf("unexpected package max bytes after unset: %d", config.PackageMaxBytes)
	}
	config, err = SetConfigValue(config, "package_download_timeout_ms", "1234")
	if err != nil {
		t.Fatalf("set package download timeout: %v", err)
	}
	if config.PackageDownloadTimeoutMS != 1234 {
		t.Fatalf("unexpected package download timeout: %d", config.PackageDownloadTimeoutMS)
	}
	config, err = UnsetConfigValue(config, "package_download_timeout_ms")
	if err != nil {
		t.Fatalf("unset package download timeout: %v", err)
	}
	if config.PackageDownloadTimeoutMS != defaultPackageDownloadTimeoutMS {
		t.Fatalf("unexpected package download timeout after unset: %d", config.PackageDownloadTimeoutMS)
	}
	config, err = SetConfigValue(config, "extracted_max_bytes", "2048")
	if err != nil {
		t.Fatalf("set extracted max bytes: %v", err)
	}
	if config.ExtractedMaxBytes != 2048 {
		t.Fatalf("unexpected extracted max bytes: %d", config.ExtractedMaxBytes)
	}
	config, err = UnsetConfigValue(config, "extracted_max_bytes")
	if err != nil {
		t.Fatalf("unset extracted max bytes: %v", err)
	}
	if config.ExtractedMaxBytes != defaultExtractedMaxBytes {
		t.Fatalf("unexpected extracted max bytes after unset: %d", config.ExtractedMaxBytes)
	}
	config, err = SetConfigValue(config, "extracted_max_files", "64")
	if err != nil {
		t.Fatalf("set extracted max files: %v", err)
	}
	if config.ExtractedMaxFiles != 64 {
		t.Fatalf("unexpected extracted max files: %d", config.ExtractedMaxFiles)
	}
	config, err = UnsetConfigValue(config, "extracted_max_files")
	if err != nil {
		t.Fatalf("unset extracted max files: %v", err)
	}
	if config.ExtractedMaxFiles != defaultExtractedMaxFiles {
		t.Fatalf("unexpected extracted max files after unset: %d", config.ExtractedMaxFiles)
	}
}

func TestValidateRegistryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	result, err := ValidateRegistryFile(path)
	if err != nil {
		t.Fatalf("validate registry: %v", err)
	}
	if !result.OK || result.Skills != 1 {
		t.Fatalf("unexpected validation: %#v", result)
	}
}

func TestValidateRegistryRejectsTrailingJSONValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"skills":[]} {"schema_version":1,"skills":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestValidateRegistryRejectsUnsafeVersionPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"../1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestValidateRegistryRejectsUnsupportedPathSegmentCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo!","version":"1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
}

func TestLoadRegistryRespectsConfiguredSizeLimit(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, "registry.json")
	if err := os.WriteFile(registryPath, []byte(`{"schema_version":1,"skills":[]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	_, sources := LoadRegistry(PathsForRoot(root), Config{RegistryFiles: []string{registryPath}, RegistryMaxBytes: 8})
	if len(sources) < 2 || sources[1].Error == "" {
		t.Fatalf("expected registry size limit source error: %#v", sources)
	}
}
