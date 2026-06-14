package core

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFindsSecurityAuditWorkflow(t *testing.T) {
	registry := DefaultRegistry()
	results := registry.Search("scan skill package risky permissions", 3)
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].Skill.Name != "security_audit" {
		t.Fatalf("expected security_audit to rank first, got %#v", results)
	}
}

func TestRunSecurityAuditScansManifestRisk(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	manifest := `{
  "schema_version": 1,
  "name": "risky_skill",
  "version": "0.1.0",
  "permissions": [
    {"name":"network"},
    {"name":"filesystem_write"},
    {"name":"credential_store"}
  ],
  "platforms": [
    {"os":"windows","arch":"amd64","url":"http://example.com/risky.zip","entrypoint":"../escape.exe"}
  ]
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "security_audit", []string{"--manifest", manifestPath, "--policy", "strict", "--allowed-permissions", "network"}, nil)
	if err != nil {
		t.Fatalf("run security_audit: %v result=%#v", err, result)
	}
	if result.Name != "security_audit" || result.Version != "0.1.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Meter != "scan" {
		t.Fatalf("expected scan usage event: %#v", result.UsageEvents)
	}
	var output builtinSecurityAuditOutput
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode security audit output: %v stdout=%s", err, result.Stdout)
	}
	if output.RiskLevel != "critical" || output.Summary.Critical == 0 || output.Summary.High == 0 {
		t.Fatalf("expected critical/high findings: %#v", output)
	}
	for _, id := range []string{"insecure_download_url", "package_missing_sha256", "unsafe_entrypoint", "permission_not_allowed"} {
		if !securityAuditOutputHasFinding(output, id) {
			t.Fatalf("expected finding %s in %#v", id, output.Findings)
		}
	}
}

func TestRunSecurityAuditScansZipPackageSignals(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "skill.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zipper := zip.NewWriter(file)
	writeZipFile(t, zipper, "manifest.json", `{"name":"zip_skill","version":"0.1.0","permissions":[{"name":"local_process"}]}`)
	writeZipFile(t, zipper, "package.json", `{"scripts":{"postinstall":"node install.js"},"dependencies":{"left-pad":"1.3.0"}}`)
	writeZipFile(t, zipper, ".env", "OPENAI_API_KEY=secret")
	if err := zipper.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "security_audit", []string{zipPath}, nil)
	if err != nil {
		t.Fatalf("run security_audit zip: %v result=%#v", err, result)
	}
	var output builtinSecurityAuditOutput
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode security audit zip output: %v stdout=%s", err, result.Stdout)
	}
	for _, id := range []string{"package_install_script", "sensitive_file_name", "secret_reference", "risky_permission_local_process"} {
		if !securityAuditOutputHasFinding(output, id) {
			t.Fatalf("expected finding %s in %#v", id, output.Findings)
		}
	}
	if len(output.Dependencies) == 0 || !strings.Contains(result.Stdout, "left-pad") {
		t.Fatalf("expected dependency summary: %#v stdout=%s", output.Dependencies, result.Stdout)
	}
}

func writeZipFile(t *testing.T, writer *zip.Writer, name, body string) {
	t.Helper()
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := entry.Write([]byte(body)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

func securityAuditOutputHasFinding(output builtinSecurityAuditOutput, id string) bool {
	for _, finding := range output.Findings {
		if finding.ID == id {
			return true
		}
	}
	return false
}
