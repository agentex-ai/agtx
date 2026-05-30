package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/agentex-ai/agtx/internal/core"
)

func TestMCPToolsList(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if len(response.Result.Tools) == 0 {
		t.Fatalf("expected tools in response: %s", stdout.String())
	}
}

func TestMCPInstallRequiresYes(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"install_skill","arguments":{"skill":"pdf"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !response.Result.IsError || !strings.Contains(response.Result.Content[0].Text, "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", stdout.String())
	}
	if response.Result.Content[0].Text == "" {
		t.Fatalf("expected text content")
	}
}

func TestMCPToolErrorIncludesStructuredContent(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"install_skill","arguments":{"skill":"pdf"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				OK    bool `json:"ok"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "confirmation_required" {
		t.Fatalf("expected structured confirmation error: %s", stdout.String())
	}
}

func TestMCPContentLengthFraming(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_skills","arguments":{"query":"pdf","limit":1}}}`
	input := strings.NewReader("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.HasPrefix(output, "Content-Length: ") {
		t.Fatalf("expected framed output: %s", output)
	}
	if !strings.Contains(output, "pdf") {
		t.Fatalf("expected pdf result: %s", output)
	}
}

func TestMCPRejectsOversizedContentLength(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader("Content-Length: " + strconv.Itoa(maxMCPMessageBytes+1) + "\r\n\r\n{}")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected mcp size failure")
	}
	if !strings.Contains(stderr.String(), "size limit") {
		t.Fatalf("expected size limit stderr, got %s", stderr.String())
	}
}

func TestMCPPlanInstall(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"plan_install","arguments":{"skill":"pdf"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `\"action\": \"install\"`) {
		t.Fatalf("expected install plan: %s", stdout.String())
	}
}

func TestMCPUninstallRequiresYes(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"uninstall_skill","arguments":{"skill":"pdf"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", stdout.String())
	}
}

func TestMCPDoctorAndVerifySkill(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor","arguments":{}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"verify_skill","arguments":{"skill":"pdf"}}}` + "\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `\"checks\"`) || !strings.Contains(stdout.String(), `\"name\": \"pdf\"`) {
		t.Fatalf("expected doctor and verify responses: %s", stdout.String())
	}
}

func TestMCPVerifyErrorPreservesPartialResult(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	manifestPath := filepath.Join(service.Paths.SkillsDir, "pdf", "0.1.0", "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"verify_skill","arguments":{"skill":"pdf"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				OK   bool `json:"ok"`
				Data struct {
					Name   string `json:"name"`
					Checks []struct {
						Name string `json:"name"`
					} `json:"checks"`
				} `json:"data"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "integrity_failed" {
		t.Fatalf("expected structured verify error: %s", stdout.String())
	}
	if response.Result.StructuredContent.Data.Name != "pdf" || len(response.Result.StructuredContent.Data.Checks) == 0 {
		t.Fatalf("expected partial verify data: %s", stdout.String())
	}
}
