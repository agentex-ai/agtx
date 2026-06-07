package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentex-ai/agtx/internal/core"
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
			Code    string `json:"code"`
			Details struct {
				Action         string   `json:"action"`
				Expected       string   `json:"expected"`
				RetryWith      string   `json:"retry_with"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.OK || response.Error.Code != "confirmation_required" {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
	if response.Error.Details.Action != "install" || response.Error.Details.Expected != "--yes" || response.Error.Details.RetryWith != "--yes" {
		t.Fatalf("unexpected confirmation details: %s", stdout.String())
	}
	if !containsString(response.Error.Details.SupportedFlags, "--yes") || !containsString(response.Error.Details.SupportedFlags, "--plan") {
		t.Fatalf("expected supported flags in confirmation details: %s", stdout.String())
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
				Code    string `json:"code"`
				Details struct {
					Flags          []string `json:"flags"`
					SupportedFlags []string `json:"supported_flags"`
				} `json:"details"`
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

func TestMainIgnoresInvalidAuthForNonProCommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "auth.json"), []byte(`{"schema_version":1,"extra":true}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"status", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("status should ignore invalid auth code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"ok": true`)) {
		t.Fatalf("expected ok status: %s", stdout.String())
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
				Code    string `json:"code"`
				Details struct {
					Flags          []string `json:"flags"`
					SupportedFlags []string `json:"supported_flags"`
				} `json:"details"`
			} `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &event); err != nil {
		t.Fatalf("invalid ndjson event: %v\n%s", err, stdout.String())
	}
	if event.Event != "failed" || event.Data.Error.Code != "invalid_argument" {
		t.Fatalf("unexpected event: %s", stdout.String())
	}
	if !containsString(event.Data.Error.Details.Flags, "--json") || !containsString(event.Data.Error.Details.Flags, "--ndjson") {
		t.Fatalf("expected mutually exclusive flag details: %s", stdout.String())
	}
	if !containsString(event.Data.Error.Details.SupportedFlags, "--timeout-ms") {
		t.Fatalf("expected supported flags in event: %s", stdout.String())
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

func TestConfigKeysJSONAndPlainText(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "keys", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config keys json failed code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data []struct {
			Key     string `json:"key"`
			Type    string `json:"type"`
			Mutable bool   `json:"mutable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid config keys json: %v\n%s", err, stdout.String())
	}
	if !response.OK || len(response.Data) == 0 {
		t.Fatalf("expected config key metadata: %s", stdout.String())
	}
	foundPackageMax := false
	for _, item := range response.Data {
		if item.Key == "package_max_bytes" {
			foundPackageMax = item.Type == "positive_integer" && item.Mutable
			break
		}
	}
	if !foundPackageMax {
		t.Fatalf("expected package_max_bytes metadata: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"config", "keys"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config keys plain failed code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "registry_url\turl") || !strings.Contains(stdout.String(), "package_max_bytes\tpositive_integer") {
		t.Fatalf("expected plain config keys table: %s", stdout.String())
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
	var response struct {
		Error struct {
			Details struct {
				Flag           string   `json:"flag"`
				Reason         string   `json:"reason"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid search error json: %v\n%s", err, stdout.String())
	}
	if response.Error.Details.Flag != "--limit" || response.Error.Details.Reason != "invalid_positive_integer" || !containsString(response.Error.Details.SupportedFlags, "--limit") {
		t.Fatalf("expected invalid limit details: %s", stdout.String())
	}
}

func TestRunRejectsMissingInputValue(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--input", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing input value failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"--input requires a value"`)) {
		t.Fatalf("expected missing input error: %s", stdout.String())
	}
	var response struct {
		Error struct {
			Details struct {
				Flag           string   `json:"flag"`
				Reason         string   `json:"reason"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid run error json: %v\n%s", err, stdout.String())
	}
	if response.Error.Details.Flag != "--input" || response.Error.Details.Reason != "missing_value" || !containsString(response.Error.Details.SupportedFlags, "--timeout-ms") {
		t.Fatalf("expected missing input details: %s", stdout.String())
	}
}

func TestRunAcceptsDashAsInputValue(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--input", "-", "--output-limit-bytes", "4", "--json"}, bytes.NewReader([]byte("too large")), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected input size failure from stdin")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"code": "size_limit_exceeded"`)) {
		t.Fatalf("expected size limit response from stdin input: %s", stdout.String())
	}
}

func TestRunRejectsMissingTimeoutValue(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--timeout-ms", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing timeout value failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"--timeout-ms requires a value"`)) {
		t.Fatalf("expected missing timeout error: %s", stdout.String())
	}
}

func TestRollbackRejectsMissingToValue(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"rollback", "pdf", "--to", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing rollback target failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"--to requires a value"`)) {
		t.Fatalf("expected missing rollback target error: %s", stdout.String())
	}
	var response struct {
		Error struct {
			Details struct {
				Flag           string   `json:"flag"`
				Reason         string   `json:"reason"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid rollback error json: %v\n%s", err, stdout.String())
	}
	if response.Error.Details.Flag != "--to" || response.Error.Details.Reason != "missing_value" || !containsString(response.Error.Details.SupportedFlags, "--plan") {
		t.Fatalf("expected missing rollback details: %s", stdout.String())
	}
}

func TestRunTreatsDoubleDashAsSkillArgSeparator(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"run", "pdf", "--", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected not installed failure")
	}
	if bytes.Contains(stdout.Bytes(), []byte(`"--json and --ndjson are mutually exclusive"`)) {
		t.Fatalf("expected --json after -- to be passed through as skill arg: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "skill is not installed") {
		t.Fatalf("expected skill lookup failure once parsing succeeds: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestMainConfigLoadFailureIgnoresJSONAfterDoubleDash(t *testing.T) {
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
	code := Main([]string{"run", "pdf", "--", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected config load failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected plain failure with no json stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "configured size limit") {
		t.Fatalf("expected plain stderr size limit error, got stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestMainConfigLoadFailureIgnoresNDJSONAfterDoubleDash(t *testing.T) {
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
	code := Main([]string{"run", "pdf", "--", "--ndjson"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected config load failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected plain failure with no ndjson stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "configured size limit") {
		t.Fatalf("expected plain stderr size limit error, got stdout=%s stderr=%s", stdout.String(), stderr.String())
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

func TestArgumentCountErrorsIncludeExpectedArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		expectedArg  string
		supportedArg string
	}{
		{name: "config set", args: []string{"config", "set", "registry_url", "--json"}, expectedArg: "value", supportedArg: "--json"},
		{name: "run", args: []string{"run", "--json"}, expectedArg: "skill", supportedArg: "--timeout-ms"},
		{name: "registry validate", args: []string{"registry", "validate", "--json"}, expectedArg: "path", supportedArg: "--json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGTX_HOME", t.TempDir())
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Main(test.args, bytes.NewReader(nil), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected argument count failure")
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected json error on stdout only, got stderr=%q", stderr.String())
			}
			var response struct {
				Error struct {
					Code    string `json:"code"`
					Details struct {
						ExpectedArgs   []string `json:"expected_args"`
						SupportedFlags []string `json:"supported_flags"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatalf("invalid argument count json: %v\n%s", err, stdout.String())
			}
			if response.Error.Code != "invalid_argument" || !containsString(response.Error.Details.ExpectedArgs, test.expectedArg) {
				t.Fatalf("expected argument metadata: %s", stdout.String())
			}
			if !containsString(response.Error.Details.SupportedFlags, test.supportedArg) {
				t.Fatalf("expected supported flags metadata: %s", stdout.String())
			}
		})
	}
}

func TestConfigUnknownKeyJSONIncludesSupportedKeys(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "set", "typo", "value", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unknown key failure")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected json error on stdout only, got stderr=%q", stderr.String())
	}
	var response struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Key           string   `json:"key"`
				SupportedKeys []string `json:"supported_keys"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid unknown key json: %v\n%s", err, stdout.String())
	}
	if response.OK || response.Error.Code != "invalid_argument" || response.Error.Details.Key != "typo" {
		t.Fatalf("unexpected unknown key response: %s", stdout.String())
	}
	if !containsString(response.Error.Details.SupportedKeys, "registry_url") || !containsString(response.Error.Details.SupportedKeys, "package_max_bytes") {
		t.Fatalf("expected supported keys in response: %s", stdout.String())
	}
}

func TestProLoginStartJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "set", "pro_api_url", "https://pro.example.com", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config set failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"pro", "login", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pro login failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"login_url": "https://pro.example.com/v1/cli/login/start?`)) {
		t.Fatalf("expected login URL: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "config", "auth.json")); err != nil {
		t.Fatalf("expected auth file: %v", err)
	}
}

func TestProLoginRejectsUnexpectedArgument(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"pro", "login", "extra", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unexpected argument failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"code": "invalid_argument"`)) {
		t.Fatalf("expected invalid argument response: %s", stdout.String())
	}
	var response struct {
		Error struct {
			Details struct {
				Args           []string `json:"args"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid pro login error json: %v\n%s", err, stdout.String())
	}
	if !containsString(response.Error.Details.Args, "extra") || !containsString(response.Error.Details.SupportedFlags, "--open") {
		t.Fatalf("expected unexpected argument details: %s", stdout.String())
	}
}

func TestListRejectsUnexpectedArgumentWithSupportedFlags(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"list", "extra", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unexpected argument failure")
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Args           []string `json:"args"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid list error json: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != "invalid_argument" || !containsString(response.Error.Details.Args, "extra") {
		t.Fatalf("unexpected list error response: %s", stdout.String())
	}
	if !containsString(response.Error.Details.SupportedFlags, "--installed") || !containsString(response.Error.Details.SupportedFlags, "--available") {
		t.Fatalf("expected list supported flags: %s", stdout.String())
	}
}

func TestProSetupJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "set", "pro_api_url", "https://pro.example.com", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config set failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"pro", "setup", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pro setup failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Authenticated      bool     `json:"authenticated"`
			HasPendingLogin    bool     `json:"has_pending_login"`
			ProAPIURL          string   `json:"pro_api_url"`
			CurrentStatus      []string `json:"current_status"`
			RecommendedActions []struct {
				ID      string `json:"id"`
				MCPTool string `json:"mcp_tool"`
			} `json:"recommended_actions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid pro setup json: %v\n%s", err, stdout.String())
	}
	if !response.OK || response.Data.Authenticated || response.Data.HasPendingLogin {
		t.Fatalf("expected unauthenticated setup response: %s", stdout.String())
	}
	if response.Data.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected pro api url in setup response: %s", stdout.String())
	}
	if !containsString(response.Data.CurrentStatus, "pro_api_configured") || !containsString(response.Data.CurrentStatus, "not_authenticated") {
		t.Fatalf("expected setup status markers: %s", stdout.String())
	}
	if !containsCLIAction(response.Data.RecommendedActions, "start_login") {
		t.Fatalf("expected start_login recommendation: %s", stdout.String())
	}
}

func TestProSetupPlainText(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "set", "pro_api_url", "https://pro.example.com", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config set failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"pro", "setup"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pro setup failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "authenticated: false") || !strings.Contains(output, "status: pro_api_configured") || !strings.Contains(output, "next:") || !strings.Contains(output, "mcp: start_pro_login") {
		t.Fatalf("expected setup text output: %s", output)
	}
}

func TestCommercePacksJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "packs", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce packs failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data []struct {
			Pack struct {
				ID              string   `json:"id"`
				Tier            string   `json:"tier"`
				CapabilityClass string   `json:"capability_class"`
				UseWhen         string   `json:"use_when"`
				Inputs          []string `json:"inputs"`
				Outputs         []string `json:"outputs"`
			} `json:"pack"`
			Installed bool `json:"installed"`
			Skills    []struct {
				Name string `json:"name"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid commerce packs json: %v\n%s", err, stdout.String())
	}
	if !response.OK || len(response.Data) != 13 || response.Data[0].Pack.ID != "web_search" || response.Data[9].Pack.ID != "pdf" || response.Data[11].Pack.ID != "standard" || response.Data[12].Pack.ID != "advanced" {
		t.Fatalf("unexpected packs response: %s", stdout.String())
	}
	if response.Data[0].Installed || len(response.Data[0].Skills) == 0 || response.Data[0].Pack.UseWhen == "" || len(response.Data[0].Pack.Inputs) == 0 || len(response.Data[0].Pack.Outputs) == 0 {
		t.Fatalf("expected uninstalled pack with skills: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "packs", "--pack-id", "pdf", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce packs filter failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var filtered struct {
		OK   bool `json:"ok"`
		Data []struct {
			Pack struct {
				ID string `json:"id"`
			} `json:"pack"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &filtered); err != nil {
		t.Fatalf("invalid filtered commerce packs json: %v\n%s", err, stdout.String())
	}
	if !filtered.OK || len(filtered.Data) != 1 || filtered.Data[0].Pack.ID != "pdf" {
		t.Fatalf("unexpected filtered packs response: %s", stdout.String())
	}
}

func TestCommerceScenariosJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "scenarios", "--scenario-id", "meeting_deck", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce scenarios failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data []struct {
			Scenario struct {
				ID                string `json:"id"`
				RecommendedPackID string `json:"recommended_pack_id"`
				Inputs            []struct {
					ID       string `json:"id"`
					Required bool   `json:"required"`
				} `json:"inputs"`
				Deliverables []struct {
					ID string `json:"id"`
				} `json:"deliverables"`
				Workflow []struct {
					ID string `json:"id"`
				} `json:"workflow"`
				AcceptanceCriteria []string `json:"acceptance_criteria"`
			} `json:"scenario"`
			RecommendedPack struct {
				Pack struct {
					ID string `json:"id"`
				} `json:"pack"`
			} `json:"recommended_pack"`
			InstallPlan struct {
				Action string `json:"action"`
			} `json:"install_plan"`
			MissingSkills []struct {
				Name string `json:"name"`
			} `json:"missing_skills"`
			Ready                bool `json:"ready"`
			BillingPreviewTotals []struct {
				Currency string `json:"currency"`
			} `json:"billing_preview_totals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid commerce scenarios json: %v\n%s", err, stdout.String())
	}
	if !response.OK || len(response.Data) != 1 || response.Data[0].Scenario.ID != "meeting_to_presentation" || response.Data[0].RecommendedPack.Pack.ID != "advanced" {
		t.Fatalf("unexpected scenario response: %s", stdout.String())
	}
	if response.Data[0].Ready || len(response.Data[0].MissingSkills) == 0 || response.Data[0].InstallPlan.Action != "install_pack" || len(response.Data[0].BillingPreviewTotals) != 2 {
		t.Fatalf("expected scenario install readiness and billing preview: %s", stdout.String())
	}
	if len(response.Data[0].Scenario.Inputs) == 0 || len(response.Data[0].Scenario.Deliverables) == 0 || len(response.Data[0].Scenario.Workflow) == 0 || len(response.Data[0].Scenario.AcceptanceCriteria) == 0 {
		t.Fatalf("expected scenario workflow metadata: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "scenarios", "--scenario-id", "invoice"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce scenarios text failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, expected := range []string{"input\tvendor_invoices", "deliverable\tinvoice_extract", "step\tintake_documents", "acceptance\tEvery payable line"} {
		if !bytes.Contains(stdout.Bytes(), []byte(expected)) {
			t.Fatalf("expected scenario text output to contain %q: %s", expected, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "scenarios", "--pack-id", "standard", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce scenario pack filter failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var filtered struct {
		OK   bool `json:"ok"`
		Data []struct {
			RecommendedPack struct {
				Pack struct {
					ID string `json:"id"`
				} `json:"pack"`
			} `json:"recommended_pack"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &filtered); err != nil {
		t.Fatalf("invalid filtered scenario json: %v\n%s", err, stdout.String())
	}
	if !filtered.OK || len(filtered.Data) == 0 {
		t.Fatalf("expected standard scenarios: %s", stdout.String())
	}
	for _, scenario := range filtered.Data {
		if scenario.RecommendedPack.Pack.ID != "standard" {
			t.Fatalf("pack filter returned non-standard scenario: %s", stdout.String())
		}
	}
}

func TestCommerceInstallPackRequiresConfirmation(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "install-pack", "standard", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected confirmation exit code, got %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Action         string   `json:"action"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid confirmation json: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != "confirmation_required" || response.Error.Details.Action != "install-pack" || !containsString(response.Error.Details.SupportedFlags, "--yes") {
		t.Fatalf("unexpected confirmation response: %s", stdout.String())
	}
}

func TestCommerceInstallPackPlanJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "install-pack", "standard", "--plan", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pack plan failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Action string `json:"action"`
			Pack   struct {
				Pack struct {
					ID string `json:"id"`
				} `json:"pack"`
			} `json:"pack"`
			Changes []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"changes"`
			BillingPreview []struct {
				Type   string `json:"type"`
				PackID string `json:"pack_id"`
			} `json:"billing_preview"`
			Requires []string `json:"requires"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid pack plan json: %v\n%s", err, stdout.String())
	}
	if !response.OK || response.Data.Action != "install_pack" || response.Data.Pack.Pack.ID != "standard" || len(response.Data.Changes) == 0 || len(response.Data.BillingPreview) != 2 || !containsString(response.Data.Requires, "confirmation") {
		t.Fatalf("unexpected pack plan response: %s", stdout.String())
	}
}

func TestCommerceInstallScenarioPlanAndRecordsJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "install-scenario", "invoice", "--plan", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scenario plan failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var plan struct {
		OK   bool `json:"ok"`
		Data struct {
			Action   string `json:"action"`
			Scenario struct {
				Scenario struct {
					ID string `json:"id"`
				} `json:"scenario"`
			} `json:"scenario"`
			PackPlan struct {
				Pack struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
				} `json:"pack"`
				BillingPreview []struct {
					ScenarioID string `json:"scenario_id"`
				} `json:"billing_preview"`
			} `json:"pack_plan"`
			Requires []string `json:"requires"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("invalid scenario plan json: %v\n%s", err, stdout.String())
	}
	if !plan.OK || plan.Data.Action != "install_scenario" || plan.Data.Scenario.Scenario.ID != "invoice_processing" || plan.Data.PackPlan.Pack.Pack.ID != "standard" || len(plan.Data.PackPlan.BillingPreview) != 2 || plan.Data.PackPlan.BillingPreview[0].ScenarioID != "invoice_processing" || !containsString(plan.Data.Requires, "confirmation") {
		t.Fatalf("unexpected scenario plan response: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "install-scenario", "invoice", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scenario install failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var install struct {
		OK   bool `json:"ok"`
		Data struct {
			Scenario struct {
				Scenario struct {
					ID string `json:"id"`
				} `json:"scenario"`
				Ready bool `json:"ready"`
			} `json:"scenario"`
			PackInstall struct {
				InstallRecord struct {
					Action     string `json:"action"`
					PackID     string `json:"pack_id"`
					ScenarioID string `json:"scenario_id"`
				} `json:"install_record"`
				BillingRecords []struct {
					ScenarioID string `json:"scenario_id"`
				} `json:"billing_records"`
			} `json:"pack_install"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &install); err != nil {
		t.Fatalf("invalid scenario install json: %v\n%s", err, stdout.String())
	}
	if !install.OK || install.Data.Scenario.Scenario.ID != "invoice_processing" || !install.Data.Scenario.Ready || install.Data.PackInstall.InstallRecord.Action != "install_scenario" || install.Data.PackInstall.InstallRecord.PackID != "standard" || install.Data.PackInstall.InstallRecord.ScenarioID != "invoice_processing" || len(install.Data.PackInstall.BillingRecords) != 2 || install.Data.PackInstall.BillingRecords[0].ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario install response: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "scenario-ledger", "invoice", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scenario ledger failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var ledger struct {
		OK   bool `json:"ok"`
		Data struct {
			Scenario struct {
				Scenario struct {
					ID string `json:"id"`
				} `json:"scenario"`
			} `json:"scenario"`
			LatestInstall struct {
				Action     string `json:"action"`
				ScenarioID string `json:"scenario_id"`
			} `json:"latest_install"`
			InstallRecords []struct {
				ScenarioID string `json:"scenario_id"`
			} `json:"install_records"`
			Billing struct {
				Records []struct {
					ScenarioID string `json:"scenario_id"`
				} `json:"records"`
				Totals []struct {
					Currency string `json:"currency"`
				} `json:"totals"`
			} `json:"billing"`
			PackInstallRecords []struct {
				Type string `json:"type"`
			} `json:"pack_install_records"`
			UsageRecords []struct {
				Type string `json:"type"`
			} `json:"usage_records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &ledger); err != nil {
		t.Fatalf("invalid scenario ledger json: %v\n%s", err, stdout.String())
	}
	if !ledger.OK || ledger.Data.Scenario.Scenario.ID != "invoice_processing" || ledger.Data.LatestInstall.Action != "install_scenario" || ledger.Data.LatestInstall.ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario ledger response: %s", stdout.String())
	}
	if len(ledger.Data.InstallRecords) != 1 || len(ledger.Data.Billing.Records) != 2 || len(ledger.Data.Billing.Totals) != 2 || len(ledger.Data.PackInstallRecords) != 2 || len(ledger.Data.UsageRecords) != 0 {
		t.Fatalf("expected scenario ledger records and totals: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "scenario-ledger", "invoice", "--type", "pack_install"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scenario ledger text failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, expected := range []string{"invoice_processing ledger", "latest_install:", "billing_records: 2", "pack_install_records: 2"} {
		if !bytes.Contains(stdout.Bytes(), []byte(expected)) {
			t.Fatalf("expected scenario ledger text output to contain %q: %s", expected, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "install-records", "--scenario-id", "invoice_processing", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scenario install records failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var installs struct {
		OK   bool `json:"ok"`
		Data struct {
			Records []struct {
				Action     string `json:"action"`
				ScenarioID string `json:"scenario_id"`
			} `json:"records"`
			Integrity struct {
				Status string `json:"status"`
			} `json:"integrity"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &installs); err != nil {
		t.Fatalf("invalid scenario install records json: %v\n%s", err, stdout.String())
	}
	if !installs.OK || len(installs.Data.Records) != 1 || installs.Data.Records[0].Action != "install_scenario" || installs.Data.Records[0].ScenarioID != "invoice_processing" || installs.Data.Integrity.Status == "" {
		t.Fatalf("unexpected scenario install records: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "billing-records", "--scenario-id", "invoice_processing", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("scenario billing records failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var billing struct {
		OK   bool `json:"ok"`
		Data struct {
			Records []struct {
				ScenarioID string `json:"scenario_id"`
			} `json:"records"`
			Totals []struct {
				Currency string `json:"currency"`
			} `json:"totals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &billing); err != nil {
		t.Fatalf("invalid scenario billing records json: %v\n%s", err, stdout.String())
	}
	if !billing.OK || len(billing.Data.Records) != 2 || billing.Data.Records[0].ScenarioID != "invoice_processing" || len(billing.Data.Totals) != 2 {
		t.Fatalf("unexpected scenario billing records: %s", stdout.String())
	}
}

func TestCommerceInstallPackSnapshotAndBillingJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "install-pack", "standard", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install pack failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"billing_records"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"pack_id": "standard"`)) {
		t.Fatalf("expected pack install billing result: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "billing-records", "--pack-id", "standard", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("billing records failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var billing struct {
		OK   bool `json:"ok"`
		Data struct {
			Records []struct {
				Type   string `json:"type"`
				PackID string `json:"pack_id"`
				Meter  string `json:"meter"`
			} `json:"records"`
			Totals []struct {
				Currency string `json:"currency"`
			} `json:"totals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &billing); err != nil {
		t.Fatalf("invalid billing json: %v\n%s", err, stdout.String())
	}
	if !billing.OK || len(billing.Data.Records) != 2 || billing.Data.Records[0].Type != "pack_install" || billing.Data.Records[0].PackID != "standard" || len(billing.Data.Totals) != 2 {
		t.Fatalf("unexpected billing response: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "billing-records", "--pack-id", "standard", "--type", "pack_install", "--currency", "USD", "--status", "local_only", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("filtered billing records failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var filteredBilling struct {
		OK   bool `json:"ok"`
		Data struct {
			Records []struct {
				Currency string `json:"currency"`
			} `json:"records"`
			Totals []struct {
				Currency string `json:"currency"`
			} `json:"totals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &filteredBilling); err != nil {
		t.Fatalf("invalid filtered billing json: %v\n%s", err, stdout.String())
	}
	if !filteredBilling.OK || len(filteredBilling.Data.Records) != 1 || filteredBilling.Data.Records[0].Currency != "USD" || len(filteredBilling.Data.Totals) != 1 {
		t.Fatalf("unexpected filtered billing response: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "snapshot", "--pack-id", "standard", "--limit", "5", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("snapshot failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"schema_version": 1`)) || !bytes.Contains(stdout.Bytes(), []byte(`"install_records"`)) {
		t.Fatalf("expected commerce snapshot: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "integrity", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce integrity failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var integrity struct {
		OK   bool `json:"ok"`
		Data struct {
			OK      bool `json:"ok"`
			Ledgers []struct {
				Status string `json:"status"`
			} `json:"ledgers"`
			Checks []struct {
				Name string `json:"name"`
			} `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &integrity); err != nil {
		t.Fatalf("invalid commerce integrity json: %v\n%s", err, stdout.String())
	}
	if !integrity.OK || !integrity.Data.OK || len(integrity.Data.Ledgers) != 3 || len(integrity.Data.Checks) == 0 {
		t.Fatalf("unexpected commerce integrity response: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "proof", "--challenge", "cli-nonce", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commerce proof failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var proof struct {
		OK   bool               `json:"ok"`
		Data core.CommerceProof `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &proof); err != nil {
		t.Fatalf("invalid commerce proof json: %v\n%s", err, stdout.String())
	}
	if !proof.OK || !core.VerifyCommerceProof(proof.Data, "cli-nonce").OK {
		t.Fatalf("unexpected commerce proof response: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	exportPath := filepath.Join(t.TempDir(), "commerce-snapshot.json")
	code = Main([]string{"commerce", "snapshot", "--pack-id", "standard", "--out", exportPath, "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("snapshot export failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var exportResponse struct {
		OK   bool `json:"ok"`
		Data struct {
			Path     string `json:"path"`
			Snapshot struct {
				SchemaVersion int `json:"schema_version"`
				Packs         []struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
				} `json:"packs"`
			} `json:"snapshot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &exportResponse); err != nil {
		t.Fatalf("invalid snapshot export json: %v\n%s", err, stdout.String())
	}
	if !exportResponse.OK || exportResponse.Data.Path != exportPath || exportResponse.Data.Snapshot.SchemaVersion != 1 {
		t.Fatalf("unexpected snapshot export response: %s", stdout.String())
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read exported snapshot: %v", err)
	}
	if !bytes.Contains(data, []byte(`"schema_version": 1`)) || !bytes.Contains(data, []byte(`"pack_id": "standard"`)) {
		t.Fatalf("unexpected exported snapshot: %s", string(data))
	}
}

func TestCommerceSubmitProofStoresAndListsReceiptJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	receiptPublicKey, receiptPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate receipt key: %v", err)
	}
	var gotPath string
	var gotAuth string
	var gotDevice string
	var gotRequest testCommerceProofSubmitRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		if request.Method != http.MethodPost || gotPath != "/v1/commerce/proofs" {
			http.Error(writer, "unexpected proof submit request", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&gotRequest); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if gotRequest.SchemaVersion != 1 || gotRequest.ClientVersion == "" || gotRequest.SubmittedAt == "" {
			http.Error(writer, "invalid submit envelope", http.StatusBadRequest)
			return
		}
		if !gotRequest.Verification.OK || !core.VerifyCommerceProof(gotRequest.Proof, "cli-submit-nonce").OK {
			http.Error(writer, "invalid commerce proof", http.StatusBadRequest)
			return
		}
		receipt := testSignedCommerceReceipt(t, gotRequest.Proof, receiptPublicKey, receiptPrivateKey, 1)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(testCommerceProofSubmitResponse{OK: true, Receipt: receipt})
	}))
	defer server.Close()

	paths := core.PathsForRoot(root)
	config := core.DefaultConfig()
	config.ProAPIURL = server.URL
	if err := core.SaveConfig(paths.ConfigFile, config); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := core.SaveAuth(paths.AuthFile, core.AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device-1"}); err != nil {
		t.Fatalf("save auth: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "install-pack", "standard", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install pack failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "submit-proof", "--challenge", "cli-submit-nonce", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected submit proof confirmation code 2, got %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var confirmation struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &confirmation); err != nil {
		t.Fatalf("invalid confirmation json: %v\n%s", err, stdout.String())
	}
	if confirmation.OK || confirmation.Error.Code != "confirmation_required" {
		t.Fatalf("unexpected confirmation response: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "submit-proof", "--challenge", "cli-submit-nonce", "--yes", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("submit proof failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if gotPath != "/v1/commerce/proofs" || gotAuth != "Bearer access" || gotDevice != "device-1" {
		t.Fatalf("unexpected proof submit request: path=%q auth=%q device=%q", gotPath, gotAuth, gotDevice)
	}
	var submit struct {
		OK   bool                             `json:"ok"`
		Data core.CommerceReceiptSubmitResult `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &submit); err != nil {
		t.Fatalf("invalid submit proof json: %v\n%s", err, stdout.String())
	}
	if !submit.OK || !submit.Data.Verification.OK || submit.Data.Receipt.ReceiptID == "" || submit.Data.Receipt.Status != "server_received" {
		t.Fatalf("unexpected submit proof response: %s", stdout.String())
	}
	if submit.Data.Receipt.Integrity == nil || submit.Data.Receipt.Integrity.Status == "" {
		t.Fatalf("expected locally signed receipt integrity: %#v", submit.Data.Receipt)
	}
	if !core.VerifyCommerceReceipt(submit.Data.Proof, submit.Data.Receipt).OK {
		t.Fatalf("receipt should verify against submitted proof: %#v", submit.Data)
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"commerce", "receipts", "--status", "server_received", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list receipts failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var receipts struct {
		OK   bool                           `json:"ok"`
		Data core.CommerceReceiptListResult `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &receipts); err != nil {
		t.Fatalf("invalid receipts json: %v\n%s", err, stdout.String())
	}
	if !receipts.OK || len(receipts.Data.Records) != 1 || receipts.Data.Records[0].ReceiptID != submit.Data.Receipt.ReceiptID || receipts.Data.Integrity == nil || receipts.Data.Integrity.Status == "" {
		t.Fatalf("unexpected receipts response: %s", stdout.String())
	}
}

func TestCommerceRecordLimitValidation(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "billing-records", "--limit", "0", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected invalid limit failure")
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Flag           string   `json:"flag"`
				SupportedFlags []string `json:"supported_flags"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid commerce error json: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != "invalid_argument" || response.Error.Details.Flag != "--limit" || !containsString(response.Error.Details.SupportedFlags, "--pack-id") {
		t.Fatalf("unexpected commerce limit error: %s", stdout.String())
	}
}

func TestCommerceRecordTimeFilterValidation(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"commerce", "install-records", "--from", "bad", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected invalid from failure")
	}
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid time filter error json: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != "invalid_argument" || response.Error.Details.Field != "from" {
		t.Fatalf("unexpected time filter error: %s", stdout.String())
	}
}

func TestHelpShowsDetailedProUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"--help"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("help failed code=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx pro login [--open] [--json]`)) {
		t.Fatalf("expected pro login usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx pro status|setup|logout|devices|register-scheme [--json]`)) {
		t.Fatalf("expected pro setup usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx pro revoke <device-id> [--json]`)) {
		t.Fatalf("expected pro revoke usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx commerce install-pack <pack> [--plan] [--yes] [--json]`)) {
		t.Fatalf("expected commerce usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx commerce install-scenario <scenario> [--plan] [--yes] [--json]`)) {
		t.Fatalf("expected commerce scenario install usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx commerce serve [--addr host:port] [--allow-origin origin] [--json]`)) {
		t.Fatalf("expected commerce serve usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx commerce proof --challenge nonce [--json]`)) {
		t.Fatalf("expected commerce proof usage: %s", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`agtx commerce submit-proof --challenge nonce --yes [--json]`)) {
		t.Fatalf("expected commerce submit-proof usage: %s", stdout.String())
	}
}

func TestProStatusWithoutLogin(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"pro", "status", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pro status failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"authenticated": false`)) {
		t.Fatalf("expected unauthenticated status: %s", stdout.String())
	}
}

func TestProDevicesUnauthorizedIncludesRecoveryHintsJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"config", "set", "pro_api_url", "https://pro.example.com", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("config set failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"pro", "devices", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unauthorized error")
	}
	var response struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ProSetup struct {
					ProAPIURL string `json:"pro_api_url"`
				} `json:"pro_setup"`
				NextActions []struct {
					ID string `json:"id"`
				} `json:"next_actions"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid pro devices json: %v\n%s", err, stdout.String())
	}
	if response.OK || response.Error.Code != "unauthorized" || response.Error.Details.ProSetup.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected unauthorized details with pro setup: %s", stdout.String())
	}
	foundRestart := false
	for _, action := range response.Error.Details.NextActions {
		if action.ID == "restart_login" {
			foundRestart = true
			break
		}
	}
	if !foundRestart {
		t.Fatalf("expected restart_login next action: %s", stdout.String())
	}
}

func TestProLogoutRemovesAuth(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)
	authPath := filepath.Join(root, "config", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.WriteFile(authPath, []byte(`{"schema_version":1,"access_token":"secret"}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"pro", "logout", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("pro logout failed code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Fatalf("expected auth removed, got %v", err)
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

func TestAgentInitJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"agent", "init", "codex", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent init json failed code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Target        string `json:"target"`
			ConfigFormat  string `json:"config_format"`
			ConfigSnippet string `json:"config_snippet"`
			RuleSnippet   string `json:"rule_snippet"`
			SetupSteps    []struct {
				ID          string   `json:"id"`
				Kind        string   `json:"kind"`
				Snippet     string   `json:"snippet"`
				Priority    int      `json:"priority"`
				Blocking    bool     `json:"blocking"`
				Platforms   []string `json:"platforms"`
				AppliesWhen []struct {
					Field string   `json:"field"`
					AnyOf []string `json:"any_of"`
				} `json:"applies_when"`
				WritesFiles []struct {
					Kind    string   `json:"kind"`
					Paths   []string `json:"paths"`
					Summary string   `json:"summary"`
				} `json:"writes_files"`
				Artifacts []struct {
					Kind         string   `json:"kind"`
					Summary      string   `json:"summary"`
					ConsumableBy []string `json:"consumable_by"`
				} `json:"artifacts"`
				Verification struct {
					Kind        string `json:"kind"`
					Expectation string `json:"expectation"`
				} `json:"verification"`
			} `json:"setup_steps"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid agent init json: %v\n%s", err, stdout.String())
	}
	if !response.OK || response.Data.Target != "codex" || response.Data.ConfigFormat != "toml" {
		t.Fatalf("unexpected response: %s", stdout.String())
	}
	if !strings.Contains(response.Data.ConfigSnippet, `[mcp_servers.agtx]`) {
		t.Fatalf("expected codex config snippet: %s", stdout.String())
	}
	if !strings.Contains(response.Data.RuleSnippet, `Use agtx through MCP`) {
		t.Fatalf("expected rule snippet: %s", stdout.String())
	}
	if len(response.Data.SetupSteps) < 2 || response.Data.SetupSteps[0].ID == "" || response.Data.SetupSteps[0].Snippet == "" {
		t.Fatalf("expected structured setup steps: %s", stdout.String())
	}
	if response.Data.SetupSteps[0].Priority <= 0 || !response.Data.SetupSteps[0].Blocking || response.Data.SetupSteps[0].Verification.Expectation == "" {
		t.Fatalf("expected setup step metadata: %s", stdout.String())
	}
	if len(response.Data.SetupSteps[0].Platforms) == 0 || len(response.Data.SetupSteps[0].AppliesWhen) == 0 {
		t.Fatalf("expected setup step platforms and conditions: %s", stdout.String())
	}
	if len(response.Data.SetupSteps[0].WritesFiles) == 0 || len(response.Data.SetupSteps[0].Artifacts) == 0 {
		t.Fatalf("expected setup step write/artifact metadata: %s", stdout.String())
	}
}

func TestAgentInitRejectsPrintAndJSONTogether(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"agent", "init", "codex", "--print", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected mutually exclusive flag failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"--print and --json are mutually exclusive"`)) {
		t.Fatalf("expected structured error: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestAgentInitUnsupportedTargetJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"agent", "init", "unknown", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unsupported target failure")
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"supported_targets"`)) {
		t.Fatalf("expected supported_targets details: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestAgentTargetsJSON(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"agent", "targets", "--json"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent targets failed code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var response struct {
		OK   bool `json:"ok"`
		Data []struct {
			Target          string   `json:"target"`
			Summary         string   `json:"summary"`
			ConfigPathHints []string `json:"config_path_hints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid agent targets json: %v\n%s", err, stdout.String())
	}
	if !response.OK || len(response.Data) == 0 {
		t.Fatalf("expected target list: %s", stdout.String())
	}
	foundCodex := false
	for _, target := range response.Data {
		if target.Target == "codex" {
			foundCodex = true
			if target.Summary == "" || len(target.ConfigPathHints) == 0 {
				t.Fatalf("expected codex summary and path hints: %s", stdout.String())
			}
		}
	}
	if !foundCodex {
		t.Fatalf("expected codex target: %s", stdout.String())
	}
}

func TestAgentTargetsPlainText(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"agent", "targets"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("agent targets failed code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "codex\t") || !strings.Contains(stdout.String(), "claude-code") {
		t.Fatalf("expected plain target listing: %s", stdout.String())
	}
}

func TestAgentPlainErrorsShowUsage(t *testing.T) {
	t.Setenv("AGTX_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Main([]string{"agent"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected agent usage failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage: agtx agent init") || !strings.Contains(stderr.String(), "agtx agent targets") {
		t.Fatalf("expected agent usage, got %q", stderr.String())
	}
}

func TestNestedCommandErrorsHonorJSON(t *testing.T) {
	tests := [][]string{
		{"config", "--json"},
		{"config", "unknown", "--json"},
		{"registry", "--json"},
		{"registry", "unknown", "--json"},
		{"pro", "--json"},
		{"pro", "unknown", "--json"},
		{"agent", "--json"},
		{"agent", "unknown", "--json"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("AGTX_HOME", t.TempDir())
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Main(args, bytes.NewReader(nil), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected failure for %v", args)
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected json error on stdout only, got stderr=%q", stderr.String())
			}
			var response struct {
				OK    bool `json:"ok"`
				Error struct {
					Code    string `json:"code"`
					Details struct {
						SupportedSubcommands []string `json:"supported_subcommands"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatalf("expected json error for %v: %v\nstdout=%s", args, err, stdout.String())
			}
			if response.OK || response.Error.Code != "invalid_argument" {
				t.Fatalf("unexpected json error for %v: %s", args, stdout.String())
			}
			if len(response.Error.Details.SupportedSubcommands) == 0 {
				t.Fatalf("expected supported_subcommands for %v: %s", args, stdout.String())
			}
		})
	}
}

func TestUnknownCommandErrorsIncludeSupportedCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "json", args: []string{"stats", "--json"}},
		{name: "ndjson", args: []string{"stats", "--ndjson"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGTX_HOME", t.TempDir())
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Main(test.args, bytes.NewReader(nil), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected failure")
			}
			if stderr.Len() != 0 {
				t.Fatalf("expected structured error on stdout only, got stderr=%q", stderr.String())
			}
			output := bytes.TrimSpace(stdout.Bytes())
			if test.name == "ndjson" {
				var event struct {
					Event string `json:"event"`
					Data  struct {
						Error struct {
							Code    string `json:"code"`
							Details struct {
								SupportedCommands []string `json:"supported_commands"`
							} `json:"details"`
						} `json:"error"`
					} `json:"data"`
				}
				if err := json.Unmarshal(output, &event); err != nil {
					t.Fatalf("invalid ndjson error: %v\n%s", err, stdout.String())
				}
				if event.Event != "failed" || event.Data.Error.Code != "invalid_argument" || !containsString(event.Data.Error.Details.SupportedCommands, "status") {
					t.Fatalf("unexpected ndjson error: %s", stdout.String())
				}
				return
			}
			var response struct {
				OK    bool `json:"ok"`
				Error struct {
					Code    string `json:"code"`
					Details struct {
						SupportedCommands []string `json:"supported_commands"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(output, &response); err != nil {
				t.Fatalf("invalid json error: %v\n%s", err, stdout.String())
			}
			if response.OK || response.Error.Code != "invalid_argument" || !containsString(response.Error.Details.SupportedCommands, "status") {
				t.Fatalf("unexpected json error: %s", stdout.String())
			}
		})
	}
}

func TestOutputModeFlagsDoNotAcceptAssignments(t *testing.T) {
	tests := [][]string{
		{"status", "--json=false"},
		{"run", "--ndjson=false", "pdf"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("AGTX_HOME", t.TempDir())
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Main(args, bytes.NewReader(nil), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected failure for %v", args)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected plain error, got stdout=%q", stdout.String())
			}
			if strings.TrimSpace(stderr.String()) == "" {
				t.Fatalf("expected plain stderr for %v", args)
			}
		})
	}
}

func TestConfigLoadFailureDoesNotHonorAssignedJSONFlag(t *testing.T) {
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
	code := Main([]string{"status", "--json=false"}, bytes.NewReader(nil), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected config load failure")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected plain failure, got stdout=%q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "configured size limit") {
		t.Fatalf("expected plain stderr size limit error, got %q", stderr.String())
	}
}

func TestDiscoveryHelperListsAreStable(t *testing.T) {
	lists := map[string][]string{
		"commands":                 supportedCommands(),
		"config subcommands":       configSubcommands(),
		"registry subcommands":     registrySubcommands(),
		"commerce subcommands":     commerceSubcommands(),
		"pro subcommands":          proSubcommands(),
		"agent subcommands":        agentSubcommands(),
		"search flags":             searchFlags(),
		"install flags":            installFlags(),
		"run flags":                runFlags(),
		"uninstall flags":          uninstallFlags(),
		"list flags":               listFlags(),
		"rollback flags":           rollbackFlags(),
		"config init flags":        configInitFlags(),
		"pro login flags":          proLoginFlags(),
		"commerce pack flags":      commercePackFlags(),
		"commerce scenario flags":  commerceScenarioFlags(),
		"commerce ledger flags":    commerceScenarioLedgerFlags(),
		"commerce record flags":    commerceRecordFlags(),
		"commerce receipt flags":   commerceReceiptFlags(),
		"commerce integrity flags": commerceIntegrityFlags(),
		"commerce proof flags":     commerceProofFlags(),
		"commerce submit flags":    commerceSubmitProofFlags(),
		"commerce snapshot flags":  commerceSnapshotFlags(),
		"commerce serve flags":     commerceServeFlags(),
		"agent init flags":         agentInitFlags(),
		"json only flags":          jsonOnlyFlags(),
	}
	for name, values := range lists {
		t.Run(name, func(t *testing.T) {
			if len(values) == 0 {
				t.Fatalf("%s must not be empty", name)
			}
			seen := map[string]bool{}
			for _, value := range values {
				if value == "" {
					t.Fatalf("%s contains empty value: %#v", name, values)
				}
				if seen[value] {
					t.Fatalf("%s contains duplicate value %s: %#v", name, value, values)
				}
				seen[value] = true
			}
		})
	}
	runSet := flagSet(runFlags())
	for _, flag := range []string{"--json", "--ndjson", "--input", "--timeout-ms", "--output-limit-bytes", "--scenario-id"} {
		if !runSet[flag] {
			t.Fatalf("run flag set missing %s", flag)
		}
	}
}

func containsCLIAction(actions []struct {
	ID      string `json:"id"`
	MCPTool string `json:"mcp_tool"`
}, want string) bool {
	for _, action := range actions {
		if action.ID == want {
			return true
		}
	}
	return false
}

type testCommerceProofSubmitRequest struct {
	SchemaVersion int                                  `json:"schema_version"`
	ClientVersion string                               `json:"client_version"`
	SubmittedAt   string                               `json:"submitted_at"`
	Proof         core.CommerceProof                   `json:"proof"`
	Verification  core.CommerceProofVerificationResult `json:"verification"`
}

type testCommerceProofSubmitResponse struct {
	OK      bool                 `json:"ok,omitempty"`
	Receipt core.CommerceReceipt `json:"receipt"`
}

func testSignedCommerceReceipt(t *testing.T, proof core.CommerceProof, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, sequence int64) core.CommerceReceipt {
	t.Helper()
	receipt := core.CommerceReceipt{
		SchemaVersion:    1,
		ReceiptID:        testCommerceReceiptIDForProof(proof),
		Status:           "server_received",
		ReceivedAt:       time.Now().UTC().Format(time.RFC3339),
		Issuer:           "agtx-test-pro",
		ServerLedgerID:   "test-commerce-receipts",
		ServerSequence:   sequence,
		Algorithm:        "ed25519-commerce-receipt-v1",
		KeyID:            "test-receipt-key",
		PublicKey:        base64.StdEncoding.EncodeToString(publicKey),
		ProofPayloadHash: proof.PayloadHash,
		ProofSignature:   proof.Signature,
		ProofKeyID:       proof.KeyID,
		Challenge:        proof.Challenge,
		DeviceID:         proof.Payload.DeviceID,
	}
	payload, err := testCommerceReceiptPayloadBytes(receipt)
	if err != nil {
		t.Fatalf("canonical receipt payload: %v", err)
	}
	receipt.ServerSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return receipt
}

func testCommerceReceiptPayloadBytes(receipt core.CommerceReceipt) ([]byte, error) {
	receipt.ServerSignature = ""
	receipt.Integrity = nil
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func testCommerceReceiptIDForProof(proof core.CommerceProof) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(proof.PayloadHash) + "\n" + strings.TrimSpace(proof.Signature)))
	return "receipt-" + hex.EncodeToString(hash[:12])
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
