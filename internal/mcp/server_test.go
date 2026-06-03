package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestMCPToolDiscoveryMatchesArgumentAllowList(t *testing.T) {
	discovered := map[string]bool{}
	for _, item := range tools() {
		name, ok := item["name"].(string)
		if !ok || name == "" {
			t.Fatalf("tool is missing name: %#v", item)
		}
		if discovered[name] {
			t.Fatalf("duplicate tool name: %s", name)
		}
		discovered[name] = true
		if _, ok := allowedToolArguments(name); !ok {
			t.Fatalf("tool %s is discoverable but not callable", name)
		}
		inputSchema, ok := item["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s is missing input schema: %#v", name, item)
		}
		properties, ok := inputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s is missing input schema properties: %#v", name, inputSchema)
		}
		allowed, _ := allowedToolArguments(name)
		if len(properties) != len(allowed) {
			t.Fatalf("tool %s schema/allow-list length mismatch: schema=%v allowed=%v", name, propertyNames(properties), toolArgumentNames(allowed))
		}
		for property := range properties {
			if _, ok := allowed[property]; !ok {
				t.Fatalf("tool %s schema exposes unsupported argument %s", name, property)
			}
		}
		for argument := range allowed {
			if _, ok := properties[argument]; !ok {
				t.Fatalf("tool %s allow-list argument %s is missing from schema", name, argument)
			}
		}
	}
	for _, name := range toolNames() {
		if !discovered[name] {
			t.Fatalf("toolNames includes undiscovered tool: %s", name)
		}
	}
}

func propertyNames(properties map[string]any) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	return names
}

func TestMCPToolsListIncludesStrictSchemas(t *testing.T) {
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
				Name              string         `json:"name"`
				InputSchema       map[string]any `json:"inputSchema"`
				OutputSchema      map[string]any `json:"outputSchema"`
				ErrorOutputSchema map[string]any `json:"errorOutputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}

	var runSchema map[string]any
	var planSchema map[string]any
	var agentSchema map[string]any
	var doctorSchema map[string]any
	var verifySchema map[string]any
	var runOutputSchema map[string]any
	var runErrorSchema map[string]any
	var searchSchema map[string]any
	var listSchema map[string]any
	var configKeysSchema map[string]any
	var registrySourcesSchema map[string]any
	var statusSchema map[string]any
	var proStatusSchema map[string]any
	var proSetupSchema map[string]any
	var proLoginStartSchema map[string]any
	var proLoginCompleteSchema map[string]any
	var proDevicesSchema map[string]any
	var revokeProDeviceSchema map[string]any
	var logoutProSchema map[string]any
	var registerProSchemeSchema map[string]any
	var refreshSchema map[string]any
	var validateRegistrySchema map[string]any
	var planOutputSchema map[string]any
	var installSchema map[string]any
	var installErrorSchema map[string]any
	var upgradeSchema map[string]any
	var rollbackSchema map[string]any
	var uninstallSchema map[string]any
	var verifyErrorSchema map[string]any
	for _, tool := range response.Result.Tools {
		switch tool.Name {
		case "search_skills":
			searchSchema = tool.OutputSchema
		case "list_skills":
			listSchema = tool.OutputSchema
		case "list_config_keys":
			configKeysSchema = tool.OutputSchema
		case "list_registry_sources":
			registrySourcesSchema = tool.OutputSchema
		case "get_status":
			statusSchema = tool.OutputSchema
		case "get_pro_status":
			proStatusSchema = tool.OutputSchema
		case "get_pro_setup":
			proSetupSchema = tool.OutputSchema
		case "start_pro_login":
			proLoginStartSchema = tool.OutputSchema
		case "complete_pro_login":
			proLoginCompleteSchema = tool.OutputSchema
		case "list_pro_devices":
			proDevicesSchema = tool.OutputSchema
		case "revoke_pro_device":
			revokeProDeviceSchema = tool.OutputSchema
		case "logout_pro":
			logoutProSchema = tool.OutputSchema
		case "register_pro_scheme":
			registerProSchemeSchema = tool.OutputSchema
		case "refresh_registry":
			refreshSchema = tool.OutputSchema
		case "validate_registry":
			validateRegistrySchema = tool.OutputSchema
		case "run_skill":
			runSchema = tool.InputSchema
			runOutputSchema = tool.OutputSchema
			runErrorSchema = tool.ErrorOutputSchema
		case "plan_install":
			planSchema = tool.InputSchema
			planOutputSchema = tool.OutputSchema
		case "install_skill":
			installSchema = tool.OutputSchema
			installErrorSchema = tool.ErrorOutputSchema
		case "upgrade_skill":
			upgradeSchema = tool.OutputSchema
		case "rollback_skill":
			rollbackSchema = tool.OutputSchema
		case "uninstall_skill":
			uninstallSchema = tool.OutputSchema
		case "get_agent_target":
			agentSchema = tool.OutputSchema
		case "doctor":
			doctorSchema = tool.OutputSchema
		case "verify_skill":
			verifySchema = tool.OutputSchema
			verifyErrorSchema = tool.ErrorOutputSchema
		}
	}
	if searchSchema == nil || listSchema == nil || configKeysSchema == nil || registrySourcesSchema == nil || statusSchema == nil || proStatusSchema == nil || proSetupSchema == nil || proLoginStartSchema == nil || proLoginCompleteSchema == nil || proDevicesSchema == nil || revokeProDeviceSchema == nil || logoutProSchema == nil || registerProSchemeSchema == nil || refreshSchema == nil || validateRegistrySchema == nil || runSchema == nil || runOutputSchema == nil || runErrorSchema == nil || planSchema == nil || planOutputSchema == nil || installSchema == nil || installErrorSchema == nil || upgradeSchema == nil || rollbackSchema == nil || uninstallSchema == nil || agentSchema == nil || doctorSchema == nil || verifySchema == nil || verifyErrorSchema == nil {
		t.Fatalf("expected schemas for discovery metadata: %s", stdout.String())
	}
	if runSchema["additionalProperties"] != false {
		t.Fatalf("expected run_skill additionalProperties=false: %#v", runSchema)
	}
	required, ok := runSchema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "skill" {
		t.Fatalf("expected run_skill required skill: %#v", runSchema["required"])
	}
	properties, ok := runSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected run_skill properties: %#v", runSchema)
	}
	timeoutProp, ok := properties["timeout_ms"].(map[string]any)
	if !ok || timeoutProp["minimum"] != float64(1) {
		t.Fatalf("expected timeout_ms minimum=1: %#v", properties["timeout_ms"])
	}
	anyOf, ok := planSchema["anyOf"].([]any)
	if !ok || len(anyOf) != 2 {
		t.Fatalf("expected plan_install anyOf requirements: %#v", planSchema["anyOf"])
	}
	planOutputProps, ok := planOutputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected plan_install output properties: %#v", planOutputSchema)
	}
	if _, ok := planOutputProps["changes"].(map[string]any); !ok {
		t.Fatalf("expected plan_install changes schema: %#v", planOutputProps)
	}
	searchItems, ok := searchSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected search_skills array items schema: %#v", searchSchema)
	}
	searchProps, ok := searchItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected search result properties: %#v", searchItems)
	}
	if _, ok := searchProps["skill"].(map[string]any); !ok {
		t.Fatalf("expected search result skill schema: %#v", searchProps)
	}
	listProps, ok := listSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_skills output properties: %#v", listSchema)
	}
	if _, ok := listProps["installed"].(map[string]any); !ok {
		t.Fatalf("expected list_skills installed schema: %#v", listProps)
	}
	configKeysItems, ok := configKeysSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_config_keys array schema: %#v", configKeysSchema)
	}
	configKeysProps, ok := configKeysItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_config_keys item properties: %#v", configKeysItems)
	}
	if _, ok := configKeysProps["default"].(map[string]any); !ok {
		t.Fatalf("expected config key default schema: %#v", configKeysProps)
	}
	registrySourceItems, ok := registrySourcesSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_registry_sources array schema: %#v", registrySourcesSchema)
	}
	registrySourceProps, ok := registrySourceItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected registry source properties: %#v", registrySourceItems)
	}
	if _, ok := registrySourceProps["loaded"].(map[string]any); !ok {
		t.Fatalf("expected registry source loaded schema: %#v", registrySourceProps)
	}
	statusProps, ok := statusSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected get_status output properties: %#v", statusSchema)
	}
	if _, ok := statusProps["registry_sources"].(map[string]any); !ok {
		t.Fatalf("expected get_status registry_sources schema: %#v", statusProps)
	}
	proStatusProps, ok := proStatusSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected get_pro_status output properties: %#v", proStatusSchema)
	}
	if _, ok := proStatusProps["device_limit"].(map[string]any); !ok {
		t.Fatalf("expected get_pro_status device_limit schema: %#v", proStatusProps)
	}
	proSetupProps, ok := proSetupSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected get_pro_setup output properties: %#v", proSetupSchema)
	}
	if _, ok := proSetupProps["recommended_actions"].(map[string]any); !ok {
		t.Fatalf("expected get_pro_setup recommended_actions schema: %#v", proSetupProps)
	}
	if _, ok := proSetupProps["current_status"].(map[string]any); !ok {
		t.Fatalf("expected get_pro_setup current_status schema: %#v", proSetupProps)
	}
	proLoginProps, ok := proLoginStartSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected start_pro_login output properties: %#v", proLoginStartSchema)
	}
	if _, ok := proLoginProps["login_url"].(map[string]any); !ok {
		t.Fatalf("expected start_pro_login login_url schema: %#v", proLoginProps)
	}
	proCallbackProps, ok := proLoginCompleteSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected complete_pro_login output properties: %#v", proLoginCompleteSchema)
	}
	if _, ok := proCallbackProps["authenticated"].(map[string]any); !ok {
		t.Fatalf("expected complete_pro_login authenticated schema: %#v", proCallbackProps)
	}
	proDeviceItems, ok := proDevicesSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_pro_devices array schema: %#v", proDevicesSchema)
	}
	proDeviceProps, ok := proDeviceItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected pro device properties: %#v", proDeviceItems)
	}
	if _, ok := proDeviceProps["revoked_at"].(map[string]any); !ok {
		t.Fatalf("expected pro device revoked_at schema: %#v", proDeviceProps)
	}
	revokeProps, ok := revokeProDeviceSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected revoke_pro_device output properties: %#v", revokeProDeviceSchema)
	}
	if _, ok := revokeProps["current"].(map[string]any); !ok {
		t.Fatalf("expected revoke_pro_device current schema: %#v", revokeProps)
	}
	logoutProps, ok := logoutProSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected logout_pro output properties: %#v", logoutProSchema)
	}
	if _, ok := logoutProps["logged_out"].(map[string]any); !ok {
		t.Fatalf("expected logout_pro logged_out schema: %#v", logoutProps)
	}
	registerSchemeProps, ok := registerProSchemeSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected register_pro_scheme output properties: %#v", registerProSchemeSchema)
	}
	if _, ok := registerSchemeProps["scheme"].(map[string]any); !ok {
		t.Fatalf("expected register_pro_scheme scheme schema: %#v", registerSchemeProps)
	}
	refreshProps, ok := refreshSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected refresh_registry output properties: %#v", refreshSchema)
	}
	if _, ok := refreshProps["bytes"].(map[string]any); !ok {
		t.Fatalf("expected refresh_registry bytes schema: %#v", refreshProps)
	}
	validateRegistryProps, ok := validateRegistrySchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected validate_registry output properties: %#v", validateRegistrySchema)
	}
	if _, ok := validateRegistryProps["warnings"].(map[string]any); !ok {
		t.Fatalf("expected validate_registry warnings schema: %#v", validateRegistryProps)
	}
	agentProps, ok := agentSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected get_agent_target output properties: %#v", agentSchema)
	}
	setupSteps, ok := agentProps["setup_steps"].(map[string]any)
	if !ok {
		t.Fatalf("expected setup_steps schema: %#v", agentProps["setup_steps"])
	}
	stepItems, ok := setupSteps["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected setup_steps items schema: %#v", setupSteps)
	}
	stepProps, ok := stepItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected setup step properties: %#v", stepItems)
	}
	if _, ok := stepProps["writes_files"].(map[string]any); !ok {
		t.Fatalf("expected writes_files schema: %#v", stepProps)
	}
	if _, ok := stepProps["artifacts"].(map[string]any); !ok {
		t.Fatalf("expected artifacts schema: %#v", stepProps)
	}
	doctorProps, ok := doctorSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected doctor output properties: %#v", doctorSchema)
	}
	if _, ok := doctorProps["checks"].(map[string]any); !ok {
		t.Fatalf("expected doctor checks schema: %#v", doctorProps)
	}
	verifyProps, ok := verifySchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected verify output properties: %#v", verifySchema)
	}
	if _, ok := verifyProps["installed_versions"].(map[string]any); !ok {
		t.Fatalf("expected verify installed_versions schema: %#v", verifyProps)
	}
	installItems, ok := installSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected install_skill result array schema: %#v", installSchema)
	}
	installProps, ok := installItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected install result properties: %#v", installItems)
	}
	if _, ok := installProps["previous_version"].(map[string]any); !ok {
		t.Fatalf("expected install previous_version schema: %#v", installProps)
	}
	installErrorProps, ok := installErrorSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected install error output properties: %#v", installErrorSchema)
	}
	installErrorCore, ok := installErrorProps["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected install error schema: %#v", installErrorProps)
	}
	installErrorCoreProps, ok := installErrorCore["properties"].(map[string]any)
	if !ok || installErrorCoreProps["code"] == nil {
		t.Fatalf("expected install error code schema: %#v", installErrorCore)
	}
	upgradeAnyOf, ok := upgradeSchema["anyOf"].([]any)
	if !ok || len(upgradeAnyOf) != 2 {
		t.Fatalf("expected upgrade_skill output anyOf: %#v", upgradeSchema)
	}
	rollbackAnyOf, ok := rollbackSchema["anyOf"].([]any)
	if !ok || len(rollbackAnyOf) != 2 {
		t.Fatalf("expected rollback_skill output anyOf: %#v", rollbackSchema)
	}
	uninstallAnyOf, ok := uninstallSchema["anyOf"].([]any)
	if !ok || len(uninstallAnyOf) != 2 {
		t.Fatalf("expected uninstall_skill output anyOf: %#v", uninstallSchema)
	}
	runOutputProps, ok := runOutputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected run output properties: %#v", runOutputSchema)
	}
	if _, ok := runOutputProps["stdout_truncated"].(map[string]any); !ok {
		t.Fatalf("expected run output truncation schema: %#v", runOutputProps)
	}
	runErrorProps, ok := runErrorSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected run error output properties: %#v", runErrorSchema)
	}
	runErrorData, ok := runErrorProps["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected run error data schema: %#v", runErrorProps)
	}
	runErrorDataProps, ok := runErrorData["properties"].(map[string]any)
	if !ok || runErrorDataProps["exit_code"] == nil {
		t.Fatalf("expected run error partial result schema: %#v", runErrorData)
	}
	verifyErrorProps, ok := verifyErrorSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected verify error output properties: %#v", verifyErrorSchema)
	}
	verifyErrorData, ok := verifyErrorProps["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected verify error data schema: %#v", verifyErrorProps)
	}
	verifyErrorDataProps, ok := verifyErrorData["properties"].(map[string]any)
	if !ok || verifyErrorDataProps["checks"] == nil {
		t.Fatalf("expected verify error partial result schema: %#v", verifyErrorData)
	}
	if traceProp, ok := runErrorProps["trace_id"].(map[string]any); !ok || traceProp["minLength"] != float64(1) {
		t.Fatalf("expected run error trace_id schema: %#v", runErrorProps["trace_id"])
	}
}

func TestMCPAgentTargetTools(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_agent_targets","arguments":{}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_agent_target","arguments":{"target":"cc"}}}` + "\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two MCP responses, got %d: %s", len(lines), stdout.String())
	}

	var listResponse struct {
		Result struct {
			StructuredContent []struct {
				Target      string `json:"target"`
				DisplayName string `json:"display_name"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &listResponse); err != nil {
		t.Fatalf("invalid list_agent_targets json: %v\n%s", err, lines[0])
	}
	if len(listResponse.Result.StructuredContent) == 0 || listResponse.Result.StructuredContent[0].Target == "" {
		t.Fatalf("expected agent targets list: %s", lines[0])
	}

	var getResponse struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Target         string   `json:"target"`
				DisplayName    string   `json:"display_name"`
				Aliases        []string `json:"aliases"`
				CommandSnippet string   `json:"command_snippet"`
				SetupSteps     []struct {
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
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &getResponse); err != nil {
		t.Fatalf("invalid get_agent_target json: %v\n%s", err, lines[1])
	}
	if getResponse.Result.IsError || getResponse.Result.StructuredContent.Target != "claude-code" {
		t.Fatalf("expected claude-code target: %s", lines[1])
	}
	if !strings.Contains(getResponse.Result.StructuredContent.CommandSnippet, "claude mcp add agtx") {
		t.Fatalf("expected claude command snippet: %s", lines[1])
	}
	if len(getResponse.Result.StructuredContent.SetupSteps) < 2 || getResponse.Result.StructuredContent.SetupSteps[0].ID == "" {
		t.Fatalf("expected setup steps: %s", lines[1])
	}
	if getResponse.Result.StructuredContent.SetupSteps[0].Priority <= 0 || getResponse.Result.StructuredContent.SetupSteps[0].Verification.Expectation == "" {
		t.Fatalf("expected setup step verification metadata: %s", lines[1])
	}
	hasConditionedStep := false
	for _, step := range getResponse.Result.StructuredContent.SetupSteps {
		if len(step.Platforms) > 0 && len(step.AppliesWhen) > 0 {
			hasConditionedStep = true
			break
		}
	}
	if !hasConditionedStep {
		t.Fatalf("expected at least one conditioned setup step: %s", lines[1])
	}
	if len(getResponse.Result.StructuredContent.SetupSteps[0].Artifacts) == 0 {
		t.Fatalf("expected setup step artifacts: %s", lines[1])
	}
	hasWriteMetadata := false
	for _, step := range getResponse.Result.StructuredContent.SetupSteps {
		if len(step.WritesFiles) > 0 {
			hasWriteMetadata = true
			break
		}
	}
	if !hasWriteMetadata {
		t.Fatalf("expected at least one step with write metadata: %s", lines[1])
	}
}

func TestMCPProStatusToolUnauthenticated(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_pro_status","arguments":{}}}` + "\n")
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
				Authenticated bool   `json:"authenticated"`
				AuthPath      string `json:"auth_path"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError || response.Result.StructuredContent.Authenticated || response.Result.StructuredContent.AuthPath == "" {
		t.Fatalf("expected unauthenticated pro status: %s", stdout.String())
	}
}

func TestMCPListConfigKeysTool(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_config_keys","arguments":{}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent []struct {
				Key     string `json:"key"`
				Type    string `json:"type"`
				Mutable bool   `json:"mutable"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError || len(response.Result.StructuredContent) == 0 {
		t.Fatalf("expected config keys result: %s", stdout.String())
	}
	foundRegistryURL := false
	for _, item := range response.Result.StructuredContent {
		if item.Key == "registry_url" {
			foundRegistryURL = item.Type == "url" && item.Mutable
			break
		}
	}
	if !foundRegistryURL {
		t.Fatalf("expected registry_url key metadata: %s", stdout.String())
	}
}

func TestMCPRegistrySourceAndValidateTools(t *testing.T) {
	root := t.TempDir()
	registryPath := filepath.Join(root, "registry.json")
	if err := os.WriteFile(registryPath, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	paths := core.PathsForRoot(root)
	service := core.NewService(paths)
	config, err := core.SetConfigValue(service.Config, "registry_files", registryPath)
	if err != nil {
		t.Fatalf("set registry_files: %v", err)
	}
	service.Config = config
	service.Registry, service.RegistrySources = core.LoadRegistry(paths, config)

	input := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_registry_sources","arguments":{}}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"validate_registry","arguments":{"path":"` + strings.ReplaceAll(registryPath, `\`, `\\`) + `"}}}` + "\n",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two MCP responses, got %d: %s", len(lines), stdout.String())
	}

	var sourcesResponse struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent []struct {
				Kind   string `json:"kind"`
				Path   string `json:"path"`
				Loaded bool   `json:"loaded"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &sourcesResponse); err != nil {
		t.Fatalf("invalid list_registry_sources json: %v\n%s", err, lines[0])
	}
	foundFileSource := false
	for _, source := range sourcesResponse.Result.StructuredContent {
		if source.Kind == "file" && source.Path == registryPath && source.Loaded {
			foundFileSource = true
			break
		}
	}
	if sourcesResponse.Result.IsError || !foundFileSource {
		t.Fatalf("expected loaded registry file source: %s", lines[0])
	}

	var validateResponse struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Path   string `json:"path"`
				OK     bool   `json:"ok"`
				Skills int    `json:"skills"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &validateResponse); err != nil {
		t.Fatalf("invalid validate_registry json: %v\n%s", err, lines[1])
	}
	if validateResponse.Result.IsError || !validateResponse.Result.StructuredContent.OK || validateResponse.Result.StructuredContent.Path != registryPath || validateResponse.Result.StructuredContent.Skills != 1 {
		t.Fatalf("expected valid registry result: %s", lines[1])
	}
}

func TestMCPProDeviceErrorIncludesRecoveryHints(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_pro_devices","arguments":{}}}` + "\n")
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
					Code    string `json:"code"`
					Details struct {
						ProSetup struct {
							ProAPIURL string `json:"pro_api_url"`
						} `json:"pro_setup"`
						NextActions []struct {
							ID      string `json:"id"`
							MCPTool string `json:"mcp_tool"`
						} `json:"next_actions"`
					} `json:"details"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "unauthorized" {
		t.Fatalf("expected unauthorized structured error: %s", stdout.String())
	}
	if response.Result.StructuredContent.Error.Details.ProSetup.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected pro setup in details: %s", stdout.String())
	}
	foundRestart := false
	for _, action := range response.Result.StructuredContent.Error.Details.NextActions {
		if action.ID == "restart_login" {
			foundRestart = true
			break
		}
	}
	if !foundRestart {
		t.Fatalf("expected restart_login in details: %s", stdout.String())
	}
}

func TestMCPGetProSetupTool(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_pro_setup","arguments":{}}}` + "\n")
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
				Authenticated      bool     `json:"authenticated"`
				HasPendingLogin    bool     `json:"has_pending_login"`
				CanRegisterScheme  bool     `json:"can_register_scheme"`
				ProAPIURL          string   `json:"pro_api_url"`
				CurrentStatus      []string `json:"current_status"`
				RecommendedActions []struct {
					ID      string `json:"id"`
					Command string `json:"command"`
					MCPTool string `json:"mcp_tool"`
				} `json:"recommended_actions"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError {
		t.Fatalf("expected success result: %s", stdout.String())
	}
	if response.Result.StructuredContent.Authenticated || response.Result.StructuredContent.HasPendingLogin {
		t.Fatalf("expected setup preview before login: %s", stdout.String())
	}
	if response.Result.StructuredContent.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected configured pro api url: %s", stdout.String())
	}
	if !containsString(response.Result.StructuredContent.CurrentStatus, "pro_api_configured") || !containsString(response.Result.StructuredContent.CurrentStatus, "not_authenticated") {
		t.Fatalf("expected setup statuses: %s", stdout.String())
	}
	if !containsActionID(response.Result.StructuredContent.RecommendedActions, "start_login") {
		t.Fatalf("expected start_login action: %s", stdout.String())
	}
}

func TestMCPStartProLoginTool(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"start_pro_login","arguments":{}}}` + "\n")
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
				LoginURL string `json:"login_url"`
				State    string `json:"state"`
				AuthPath string `json:"auth_path"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError || !strings.Contains(response.Result.StructuredContent.LoginURL, "https://pro.example.com/v1/cli/login/start?") || response.Result.StructuredContent.State == "" || response.Result.StructuredContent.AuthPath == "" {
		t.Fatalf("expected login start result: %s", stdout.String())
	}
}

func TestMCPCompleteProLoginTool(t *testing.T) {
	var tokenRequest string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli/token":
			tokenRequest = request.Header.Get("Authorization")
			_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"registry_url":"http://` + request.Host + `/v1/registry","device_limit":3,"subscription":"active"}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	service := core.NewService(core.PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = server.URL
	login, err := service.ProLoginStart(context.Background())
	if err != nil {
		t.Fatalf("login start: %v", err)
	}

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"complete_pro_login","arguments":{"callback_uri":"agtx://pro/callback?code=abc&state=` + login.State + `"}}}` + "\n")
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
				Authenticated bool   `json:"authenticated"`
				DeviceLimit   int    `json:"device_limit"`
				Subscription  string `json:"subscription"`
				RegistryURL   string `json:"registry_url"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError || !response.Result.StructuredContent.Authenticated || response.Result.StructuredContent.DeviceLimit != 3 || response.Result.StructuredContent.Subscription != "active" || response.Result.StructuredContent.RegistryURL == "" {
		t.Fatalf("expected complete_pro_login result: %s", stdout.String())
	}
	if tokenRequest != "" {
		t.Fatalf("token exchange should not send bearer auth, got %q", tokenRequest)
	}
}

func TestMCPLogoutProTool(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	service.Auth = core.AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device-1"}
	if err := core.SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"logout_pro","arguments":{}}}` + "\n")
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
				LoggedOut bool   `json:"logged_out"`
				AuthPath  string `json:"auth_path"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError || !response.Result.StructuredContent.LoggedOut || response.Result.StructuredContent.AuthPath == "" {
		t.Fatalf("expected logout_pro result: %s", stdout.String())
	}
	if _, err := os.Stat(service.Paths.AuthFile); !os.IsNotExist(err) {
		t.Fatalf("expected auth file removed, got err=%v", err)
	}
}

func TestMCPRegisterProSchemeTool(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	restore := core.SwapProRegisterSchemeHookForTest(func() (core.ProSchemeResult, error) {
		return core.ProSchemeResult{Scheme: "agtx", Command: `"C:\agtx.exe" pro callback "%1"`}, nil
	})
	defer restore()
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"register_pro_scheme","arguments":{}}}` + "\n")
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
				Scheme string `json:"scheme"`
				Error  struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Result.IsError {
		t.Fatalf("expected success result: %s", stdout.String())
	}
	if response.Result.StructuredContent.Scheme != "agtx" {
		t.Fatalf("expected agtx scheme result: %s", stdout.String())
	}
}

func TestMCPRevokeProDeviceRequiresYes(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"revoke_pro_device","arguments":{"device_id":"device-1"}}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", stdout.String())
	}
	assertMCPConfirmationDetails(t, stdout.Bytes(), "revoke_pro_device", "device_id")
}

func TestMCPGetAgentTargetRejectsUnknownTarget(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"get_agent_target","arguments":{"target":"unknown"}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "unsupported agent target" {
		t.Fatalf("unexpected error: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "get_agent_target" || details["argument"] != "target" || details["value"] != "unknown" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	targets, ok := details["supported_targets"].([]string)
	if !ok || !containsString(targets, "codex") || !containsString(targets, "cursor") {
		t.Fatalf("expected supported targets: %#v", details["supported_targets"])
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !containsString(args, "target") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
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
	assertMCPConfirmationDetails(t, stdout.Bytes(), "install_skill", "skill")
}

func assertMCPConfirmationDetails(t *testing.T, output []byte, tool, supportedArgument string) {
	t.Helper()
	var response struct {
		Result struct {
			StructuredContent struct {
				Error struct {
					Details map[string]any `json:"details"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, string(output))
	}
	details := response.Result.StructuredContent.Error.Details
	if details["tool"] != tool || details["argument"] != "yes" || details["expected"] != true {
		t.Fatalf("unexpected confirmation details: %#v", details)
	}
	retry, ok := details["retry_with"].(map[string]any)
	if !ok || retry["yes"] != true {
		t.Fatalf("expected retry_with yes=true: %#v", details["retry_with"])
	}
	args, ok := details["supported_arguments"].([]any)
	if !ok || !containsAnyString(args, "yes") || !containsAnyString(args, supportedArgument) {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
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

func TestMCPLineModeAllowsBlankAndIndentedMessages(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader("\n  \t\n  {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"search_skills\",\"arguments\":{\"query\":\"pdf\",\"limit\":1}}}\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected no stderr, got %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "pdf") {
		t.Fatalf("expected pdf response after blank and indented lines: %s", stdout.String())
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
	assertMCPConfirmationDetails(t, stdout.Bytes(), "uninstall_skill", "skill")
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

func TestMCPRejectsUnknownToolArgument(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"search_skills","arguments":{"query":"pdf","typo":true}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "unknown tool argument" {
		t.Fatalf("unexpected message: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "search_skills" || details["argument"] != "typo" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !containsString(args, "query") || !containsString(args, "limit") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsNonObjectToolArguments(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"search_skills","arguments":["pdf"]}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "invalid tool arguments" {
		t.Fatalf("unexpected message: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "search_skills" || details["expected"] != "object" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !containsString(args, "query") || !containsString(args, "limit") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsMissingRequiredToolArgument(t *testing.T) {
	tests := []struct {
		name              string
		request           string
		message           string
		tool              string
		argument          string
		supportedArgument string
	}{
		{
			name:              "search query",
			request:           `{"name":"search_skills","arguments":{"limit":1}}`,
			message:           "query is required",
			tool:              "search_skills",
			argument:          "query",
			supportedArgument: "limit",
		},
		{
			name:              "run skill",
			request:           `{"name":"run_skill","arguments":{"args":["--help"]}}`,
			message:           "skill is required",
			tool:              "run_skill",
			argument:          "skill",
			supportedArgument: "args",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
			_, err := s.callTool(json.RawMessage(tt.request))
			if !core.IsErrorCode(err, core.CodeInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
			coreErr := core.ErrorFrom(err)
			if coreErr.Message != tt.message {
				t.Fatalf("unexpected message: %#v", coreErr)
			}
			details, ok := coreErr.Details.(map[string]any)
			if !ok || details["tool"] != tt.tool || details["argument"] != tt.argument || details["expected"] != "non_empty_string" {
				t.Fatalf("unexpected details: %#v", coreErr.Details)
			}
			args, ok := details["supported_arguments"].([]string)
			if !ok || !containsString(args, tt.argument) || !containsString(args, tt.supportedArgument) {
				t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
			}
		})
	}
}

func TestMCPRejectsMissingPlanInstallSkillArguments(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"plan_install","arguments":{}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "at least one skill name is required" {
		t.Fatalf("unexpected message: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "plan_install" || details["expected"] != "non_empty_string_or_array_of_strings" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	names, ok := details["arguments"].([]string)
	if !ok || !containsString(names, "skill") || !containsString(names, "skills") {
		t.Fatalf("expected argument alternatives: %#v", details["arguments"])
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !containsString(args, "skill") || !containsString(args, "skills") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsMissingToolNameWithSupportedTools(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"arguments":{}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "tool name is required" {
		t.Fatalf("unexpected message: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected details map: %#v", coreErr.Details)
	}
	tools, ok := details["supported_tools"].([]string)
	if !ok || !containsString(tools, "search_skills") || !containsString(tools, "run_skill") {
		t.Fatalf("expected supported_tools details: %#v", details["supported_tools"])
	}
}

func TestMCPRejectsUnknownToolWithSupportedTools(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"search_skillz","arguments":{"query":"pdf"}}`))
	if !core.IsErrorCode(err, core.CodeNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "unknown tool" {
		t.Fatalf("unexpected message: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "search_skillz" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	tools, ok := details["supported_tools"].([]string)
	if !ok || !containsString(tools, "search_skills") || !containsString(tools, "run_skill") {
		t.Fatalf("expected supported_tools details: %#v", details["supported_tools"])
	}
}

func TestMCPToolErrorPreservesSupportedKeysDetails(t *testing.T) {
	response := toolError(core.NewError(core.CodeInvalidArgument, "unknown config key", map[string]any{
		"key":            "typo",
		"supported_keys": core.ConfigKeyNames(),
	}), nil)
	content, ok := response["structuredContent"].(core.Response)
	if !ok || content.Error == nil {
		t.Fatalf("expected structured error response: %#v", response["structuredContent"])
	}
	details, ok := content.Error.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected error details map: %#v", content.Error.Details)
	}
	keys, ok := details["supported_keys"].([]string)
	if !ok || !containsString(keys, "registry_url") || !containsString(keys, "package_max_bytes") {
		t.Fatalf("expected supported_keys details: %#v", details["supported_keys"])
	}
}

func TestMCPRejectsWrongArgumentType(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"install_skill","arguments":{"skill":["pdf"],"yes":true}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "invalid tool argument type" {
		t.Fatalf("unexpected message: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "install_skill" || details["argument"] != "skill" || details["expected"] != "string" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !containsString(args, "skill") || !containsString(args, "yes") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsNonPositiveIntegerArgument(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"run_skill","arguments":{"skill":"pdf","timeout_ms":0}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if core.ErrorFrom(err).Message != "timeout_ms must be a positive integer" {
		t.Fatalf("unexpected error: %#v", core.ErrorFrom(err))
	}
	details, ok := core.ErrorFrom(err).Details.(map[string]any)
	if !ok || details["tool"] != "run_skill" || details["argument"] != "timeout_ms" || details["expected"] != "positive_integer" {
		t.Fatalf("unexpected details: %#v", core.ErrorFrom(err).Details)
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !containsString(args, "timeout_ms") || !containsString(args, "output_limit_bytes") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsTrailingArgumentJSONValue(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"search_skills","arguments":{"query":"pdf"} {"limit":1}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if core.ErrorFrom(err).Message != "invalid tools/call params" {
		t.Fatalf("unexpected error: %#v", core.ErrorFrom(err))
	}
	details, ok := core.ErrorFrom(err).Details.(map[string]any)
	if !ok || details["expected"] != "object" {
		t.Fatalf("unexpected details: %#v", core.ErrorFrom(err).Details)
	}
	params, ok := details["supported_params"].([]string)
	if !ok || !containsString(params, "name") || !containsString(params, "arguments") {
		t.Fatalf("expected supported_params details: %#v", details["supported_params"])
	}
}

func TestMCPRejectsUnknownToolCallParamWithSupportedParams(t *testing.T) {
	s := &server{service: core.NewService(core.PathsForRoot(t.TempDir()))}
	_, err := s.callTool(json.RawMessage(`{"name":"search_skills","arguments":{"query":"pdf"},"extra":true}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "invalid tools/call params" {
		t.Fatalf("unexpected error: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["expected"] != "object" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	params, ok := details["supported_params"].([]string)
	if !ok || !containsString(params, "name") || !containsString(params, "arguments") {
		t.Fatalf("expected supported_params details: %#v", details["supported_params"])
	}
}

func TestMCPRejectsInvalidJSONRPCVersion(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"1.0","id":1,"method":"tools/list","params":{}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"message":"invalid request"`) || !strings.Contains(stdout.String(), `jsonrpc must be 2.0`) {
		t.Fatalf("expected invalid request response: %s", stdout.String())
	}
	assertMCPInvalidRequestDetails(t, stdout.Bytes(), "jsonrpc", "jsonrpc must be 2.0")
}

func TestMCPRejectsUnknownMethodWithSupportedMethods(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/lost","params":{}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Method           string   `json:"method"`
				SupportedMethods []string `json:"supported_methods"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid unknown method response: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != -32601 || response.Error.Data.Method != "tools/lost" {
		t.Fatalf("unexpected unknown method response: %s", stdout.String())
	}
	if !containsString(response.Error.Data.SupportedMethods, "tools/list") || !containsString(response.Error.Data.SupportedMethods, "tools/call") {
		t.Fatalf("expected supported methods: %s", stdout.String())
	}
}

func TestMCPRejectsMissingMethodWithSupportedMethods(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"params":{}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Error            string   `json:"error"`
				SupportedMethods []string `json:"supported_methods"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid missing method response: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != -32600 || response.Error.Data.Error != "missing method" {
		t.Fatalf("unexpected missing method response: %s", stdout.String())
	}
	if !containsString(response.Error.Data.SupportedMethods, "initialize") || !containsString(response.Error.Data.SupportedMethods, "tools/call") {
		t.Fatalf("expected supported methods: %s", stdout.String())
	}
}

func TestMCPSupportedMethodsAreStable(t *testing.T) {
	seen := map[string]bool{}
	for _, method := range supportedMethods() {
		if method == "" {
			t.Fatalf("supportedMethods contains empty method: %#v", supportedMethods())
		}
		if seen[method] {
			t.Fatalf("supportedMethods contains duplicate method %s: %#v", method, supportedMethods())
		}
		seen[method] = true
	}
	for _, method := range []string{"initialize", "notifications/initialized", "tools/list", "tools/call"} {
		if !seen[method] {
			t.Fatalf("supportedMethods missing %s: %#v", method, supportedMethods())
		}
	}
}

func TestMCPRejectsInvalidRequestIDType(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":{},"method":"tools/list","params":{}}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `id must be string, number, or null`) {
		t.Fatalf("expected invalid id response: %s", stdout.String())
	}
	assertMCPInvalidRequestDetails(t, stdout.Bytes(), "id", "id must be string, number, or null")
}

func TestMCPRejectsNonObjectParams(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[]}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `params must be an object or null`) {
		t.Fatalf("expected invalid params response: %s", stdout.String())
	}
	assertMCPInvalidRequestDetails(t, stdout.Bytes(), "params", "params must be an object or null")
}

func TestMCPRejectsInvalidJSONWithStructuredParseError(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0"` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Error    string `json:"error"`
				Expected string `json:"expected"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != -32700 || response.Error.Data.Error != "invalid JSON" || response.Error.Data.Expected != "json_object" {
		t.Fatalf("unexpected parse error details: %s", stdout.String())
	}
}

func TestMCPRejectsBatchWithSupportedEnvelopeFields(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}]` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Error           string   `json:"error"`
				Expected        string   `json:"expected"`
				SupportedFields []string `json:"supported_fields"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Error.Code != -32600 || response.Error.Data.Expected != "single_jsonrpc_request" {
		t.Fatalf("unexpected batch error details: %s", stdout.String())
	}
	if !containsString(response.Error.Data.SupportedFields, "jsonrpc") || !containsString(response.Error.Data.SupportedFields, "params") {
		t.Fatalf("expected supported_fields details: %s", stdout.String())
	}
}

func TestMCPRejectsUnknownTopLevelField(t *testing.T) {
	service := core.NewService(core.PathsForRoot(t.TempDir()))
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{},"extra":true}` + "\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, input, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"message":"invalid request"`) || !strings.Contains(stdout.String(), `unknown field \"extra\"`) {
		t.Fatalf("expected strict top-level decode failure: %s", stdout.String())
	}
	var response struct {
		Error struct {
			Data struct {
				Error           string   `json:"error"`
				Expected        string   `json:"expected"`
				SupportedFields []string `json:"supported_fields"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, stdout.String())
	}
	if response.Error.Data.Expected != "object" || !strings.Contains(response.Error.Data.Error, `unknown field "extra"`) {
		t.Fatalf("unexpected top-level field details: %s", stdout.String())
	}
	if !containsString(response.Error.Data.SupportedFields, "jsonrpc") || !containsString(response.Error.Data.SupportedFields, "method") {
		t.Fatalf("expected supported_fields details: %s", stdout.String())
	}
}

func assertMCPInvalidRequestDetails(t *testing.T, output []byte, field, message string) {
	t.Helper()
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Error    string `json:"error"`
				Field    string `json:"field"`
				Expected any    `json:"expected"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, string(output))
	}
	if response.Error.Code != -32600 || response.Error.Data.Error != message || response.Error.Data.Field != field {
		t.Fatalf("unexpected invalid request details: %s", string(output))
	}
	if response.Error.Data.Expected == nil {
		t.Fatalf("expected field expectation in invalid request details: %s", string(output))
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsActionID(actions []struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	MCPTool string `json:"mcp_tool"`
}, want string) bool {
	for _, action := range actions {
		if action.ID == want {
			return true
		}
	}
	return false
}
