package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallRequiresConfirmationForJSONAgent(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"install", "pdf", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected confirmation exit code 2, got %d stderr=%s", code, stderr.String())
	}
	var response struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.OK || response.Error.Code != "confirmation_required" {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
}

func TestMainConfigLoadFailureHonorsJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), bytes.Repeat([]byte("x"), 1024*1024+1), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"status", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected config load failure")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr for json failure, got %q", stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"code": "size_limit_exceeded"`)) {
		t.Fatalf("expected structured size limit error: %s", stdout.String())
	}
}

func TestMainConfigLoadFailureHonorsNDJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), bytes.Repeat([]byte("x"), 1024*1024+1), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--ndjson"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected config load failure")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr for ndjson failure, got %q", stderr.String())
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &event); err != nil {
		t.Fatalf("invalid ndjson event: %v\n%s", err, stdout.String())
	}
	if event.Event != "failed" || event.Data.Error.Code != "size_limit_exceeded" {
		t.Fatalf("unexpected event: %s", stdout.String())
	}
}

func TestRunRejectsJSONAndNDJSONTogether(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--json", "--ndjson"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected mutually exclusive output failure")
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &event); err != nil {
		t.Fatalf("invalid ndjson event: %v\n%s", err, stdout.String())
	}
	if event.Event != "failed" || event.Data.Error.Code != "invalid_argument" {
		t.Fatalf("unexpected event: %s", stdout.String())
	}
}

func TestInstallWithYesAndStatusJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"install", "pdf", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"status", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status failed with code %d stderr=%s", code, stderr.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Installed int `json:"installed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid status json: %v\n%s", err, stdout.String())
	}
	if !response.OK || response.Data.Installed != 1 {
		t.Fatalf("unexpected status: %s", stdout.String())
	}
}

func TestMainDoesNotNeedExistingHome(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	t.Setenv("AGTX_HOME", root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"list", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list failed with code %d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatalf("list should not create AGTX_HOME")
	}
}

func TestConfigInitAndRegistrySourcesJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "init", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config init failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"registry", "sources", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("registry sources failed with code %d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"kind": "builtin"`)) {
		t.Fatalf("expected builtin registry source: %s", stdout.String())
	}
}

func TestSearchRejectsInvalidLimit(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"search", "pdf", "--limit", "wat", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for invalid limit")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"code": "invalid_argument"`)) {
		t.Fatalf("expected invalid argument response: %s", stdout.String())
	}
}

func TestInstallPlanDoesNotRequireConfirmation(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"install", "pdf", "--plan", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("plan failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"action": "install"`)) {
		t.Fatalf("expected install plan: %s", stdout.String())
	}
}

func TestConfigSetUnsetAndRegistryValidate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	registryPath := filepath.Join(root, "registry.json")
	if err := os.WriteFile(registryPath, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "set", "registry_files", registryPath, "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config set failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"registry", "validate", registryPath, "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("registry validate failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"ok": true`)) {
		t.Fatalf("expected valid registry: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"config", "unset", "registry_files", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config unset failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
}

func TestUninstallRequiresConfirmationAndPlanWorks(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"install", "pdf", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install failed: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"uninstall", "pdf", "--plan", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"action": "uninstall"`)) {
		t.Fatalf("uninstall plan failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"uninstall", "pdf", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 2 || !bytes.Contains(stdout.Bytes(), []byte(`confirmation_required`)) {
		t.Fatalf("expected confirmation requirement code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestDoctorAndVerifyJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"doctor", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doctor failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"checks"`)) {
		t.Fatalf("expected doctor checks: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"install", "pdf", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install failed: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"verify", "pdf", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("verify failed with code %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"name": "pdf"`)) {
		t.Fatalf("expected verify result: %s", stdout.String())
	}
}

func TestRunInputFileHonorsSizeLimit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	inputPath := filepath.Join(root, "input.json")
	if err := os.WriteFile(inputPath, []byte("too large"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--input", inputPath, "--output-limit-bytes", "4", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected input size failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"code": "size_limit_exceeded"`)) {
		t.Fatalf("expected size limit response: %s", stdout.String())
	}
}
