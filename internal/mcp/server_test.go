package mcp

import (
	"bytes"
	"context"
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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentex-ai/agtx/internal/core"
)

type mcpProPreviewResponse struct {
	Result struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			CurrentStatus      []string              `json:"current_status"`
			RecommendedActions []core.ProSetupAction `json:"recommended_actions"`
		} `json:"structuredContent"`
	} `json:"result"`
}

func runMCPToolNoArgs(t *testing.T, service *core.Service, name string, response any) string {
	t.Helper()
	return runMCPTool(t, service, name, `{}`, response)
}

func runMCPTool(t *testing.T, service *core.Service, name, arguments string, response any) string {
	t.Helper()
	return runMCP(t, service, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":`+strconv.Quote(name)+`,"arguments":`+arguments+`}}`+"\n", response)
}

func runMCP(t *testing.T, service *core.Service, input string, response any) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run(service, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mcp failed: code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if response != nil {
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), response); err != nil {
			t.Fatalf("invalid json: %v\n%s", err, stdout.String())
		}
	}
	return stdout.String()
}

func runMCPLines(t *testing.T, service *core.Service, input string, want int) []string {
	t.Helper()
	output := runMCP(t, service, input, nil)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != want {
		t.Fatalf("expected %d MCP responses, got %d: %s", want, len(lines), output)
	}
	return lines
}

func testMCPService(t *testing.T) *core.Service {
	t.Helper()
	return core.NewService(core.PathsForRoot(t.TempDir()))
}

func TestMCPToolsList(t *testing.T) {
	service := testMCPService(t)
	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`+"\n", &response)
	if len(response.Result.Tools) == 0 {
		t.Fatalf("expected tools in response: %s", output)
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
	service := testMCPService(t)
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
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`+"\n", &response)

	schemas := map[string]map[string]any{}
	for _, tool := range response.Result.Tools {
		schemas[tool.Name+".input"] = tool.InputSchema
		schemas[tool.Name+".output"] = tool.OutputSchema
		schemas[tool.Name+".error"] = tool.ErrorOutputSchema
	}
	schema := func(name string) map[string]any {
		t.Helper()
		if schemas[name] == nil {
			t.Fatalf("expected %s schema in discovery metadata: %s", name, output)
		}
		return schemas[name]
	}
	runSchema := schema("run_skill.input")
	planSchema := schema("plan_install.input")
	agentSchema := schema("get_agent_target.output")
	doctorSchema := schema("doctor.output")
	verifySchema := schema("verify_skill.output")
	runOutputSchema := schema("run_skill.output")
	runErrorSchema := schema("run_skill.error")
	searchSchema := schema("search_skills.output")
	listSchema := schema("list_skills.output")
	packListSchema := schema("list_capability_packs.output")
	scenarioListSchema := schema("list_capability_scenarios.output")
	scenarioSchema := schema("get_capability_scenario.output")
	packPlanSchema := schema("plan_capability_pack_install.output")
	packInstallSchema := schema("install_capability_pack.output")
	scenarioPlanSchema := schema("plan_capability_scenario_install.output")
	scenarioInstallSchema := schema("install_capability_scenario.output")
	scenarioLedgerSchema := schema("get_capability_scenario_ledger.output")
	installRecordsSchema := schema("list_install_records.output")
	billingRecordsSchema := schema("list_billing_records.output")
	commerceSnapshotSchema := schema("get_commerce_snapshot.output")
	commerceIntegritySchema := schema("get_commerce_integrity.output")
	commerceProofSchema := schema("get_commerce_proof.output")
	configKeysSchema := schema("list_config_keys.output")
	registrySourcesSchema := schema("list_registry_sources.output")
	statusSchema := schema("get_status.output")
	proStatusSchema := schema("get_pro_status.output")
	proSetupSchema := schema("get_pro_setup.output")
	proLoginStartSchema := schema("start_pro_login.output")
	proLoginCompleteSchema := schema("complete_pro_login.output")
	proDevicesSchema := schema("list_pro_devices.output")
	revokeProDeviceSchema := schema("revoke_pro_device.output")
	logoutProSchema := schema("logout_pro.output")
	registerProSchemeSchema := schema("register_pro_scheme.output")
	refreshSchema := schema("refresh_registry.output")
	validateRegistrySchema := schema("validate_registry.output")
	planOutputSchema := schema("plan_install.output")
	installSchema := schema("install_skill.output")
	installErrorSchema := schema("install_skill.error")
	upgradeSchema := schema("upgrade_skill.output")
	rollbackSchema := schema("rollback_skill.output")
	uninstallSchema := schema("uninstall_skill.output")
	verifyErrorSchema := schema("verify_skill.error")
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
	if _, ok := properties["scenario_id"].(map[string]any); !ok {
		t.Fatalf("expected run_skill scenario_id input schema: %#v", properties)
	}
	if _, ok := properties["agent_name"].(map[string]any); !ok {
		t.Fatalf("expected run_skill agent_name input schema: %#v", properties)
	}
	if _, ok := properties["input_base64"].(map[string]any); !ok {
		t.Fatalf("expected run_skill input_base64 input schema: %#v", properties)
	}
	if _, ok := properties["input_path"].(map[string]any); !ok {
		t.Fatalf("expected run_skill input_path input schema: %#v", properties)
	}
	ocrSchema, ok := properties["ocr"].(map[string]any)
	if !ok {
		t.Fatalf("expected run_skill ocr input schema: %#v", properties)
	}
	ocrProps, ok := ocrSchema["properties"].(map[string]any)
	if !ok || ocrSchema["additionalProperties"] != false || ocrProps["download_models"] == nil || ocrProps["dry_run"] == nil || ocrProps["model_size"] == nil || ocrProps["backend"] == nil || ocrProps["det_limit_side_len"] == nil {
		t.Fatalf("expected strict run_skill ocr option schema: %#v", ocrSchema)
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
	packItems, ok := packListSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_capability_packs item schema: %#v", packListSchema)
	}
	packProps, ok := packItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected pack view properties: %#v", packItems)
	}
	packEnvelope, ok := packProps["pack"].(map[string]any)
	if !ok {
		t.Fatalf("expected pack schema in pack view: %#v", packProps)
	}
	packEnvelopeProps, ok := packEnvelope["properties"].(map[string]any)
	if !ok || packEnvelopeProps["capability_class"] == nil || packEnvelopeProps["use_when"] == nil || packEnvelopeProps["inputs"] == nil || packEnvelopeProps["outputs"] == nil || packEnvelopeProps["tags"] == nil {
		t.Fatalf("expected pack contract metadata schema: %#v", packEnvelope)
	}
	scenarioItems, ok := scenarioListSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_capability_scenarios item schema: %#v", scenarioListSchema)
	}
	scenarioListProps, ok := scenarioItems["properties"].(map[string]any)
	if !ok || scenarioListProps["scenario"] == nil || scenarioListProps["recommended_pack"] == nil || scenarioListProps["install_plan"] == nil {
		t.Fatalf("expected scenario view properties: %#v", scenarioItems)
	}
	scenarioProps, ok := scenarioSchema["properties"].(map[string]any)
	if !ok || scenarioProps["missing_skills"] == nil || scenarioProps["billing_preview_totals"] == nil {
		t.Fatalf("expected get_capability_scenario output properties: %#v", scenarioSchema)
	}
	scenarioEnvelope, ok := scenarioListProps["scenario"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested scenario schema: %#v", scenarioListProps)
	}
	scenarioEnvelopeProps, ok := scenarioEnvelope["properties"].(map[string]any)
	if !ok || scenarioEnvelopeProps["inputs"] == nil || scenarioEnvelopeProps["deliverables"] == nil || scenarioEnvelopeProps["workflow"] == nil || scenarioEnvelopeProps["acceptance_criteria"] == nil {
		t.Fatalf("expected scenario workflow metadata schema: %#v", scenarioEnvelope)
	}
	packPlanProps, ok := packPlanSchema["properties"].(map[string]any)
	if !ok || packPlanProps["billing_preview"] == nil || packPlanProps["requires"] == nil {
		t.Fatalf("expected plan_capability_pack_install billing preview schema: %#v", packPlanSchema)
	}
	packInstallProps, ok := packInstallSchema["properties"].(map[string]any)
	if !ok || packInstallProps["billing_records"] == nil {
		t.Fatalf("expected install_capability_pack billing records schema: %#v", packInstallSchema)
	}
	scenarioPlanProps, ok := scenarioPlanSchema["properties"].(map[string]any)
	if !ok || scenarioPlanProps["scenario"] == nil || scenarioPlanProps["pack_plan"] == nil {
		t.Fatalf("expected plan_capability_scenario_install schema: %#v", scenarioPlanSchema)
	}
	scenarioInstallProps, ok := scenarioInstallSchema["properties"].(map[string]any)
	if !ok || scenarioInstallProps["scenario"] == nil || scenarioInstallProps["pack_install"] == nil {
		t.Fatalf("expected install_capability_scenario schema: %#v", scenarioInstallSchema)
	}
	scenarioLedgerProps, ok := scenarioLedgerSchema["properties"].(map[string]any)
	if !ok || scenarioLedgerProps["scenario"] == nil || scenarioLedgerProps["latest_install"] == nil || scenarioLedgerProps["install_records"] == nil || scenarioLedgerProps["billing"] == nil || scenarioLedgerProps["usage_records"] == nil || scenarioLedgerProps["pack_install_records"] == nil {
		t.Fatalf("expected get_capability_scenario_ledger schema: %#v", scenarioLedgerSchema)
	}
	installRecordsProps, ok := installRecordsSchema["properties"].(map[string]any)
	if !ok || installRecordsProps["records"] == nil || installRecordsProps["integrity"] == nil {
		t.Fatalf("expected list_install_records object schema: %#v", installRecordsSchema)
	}
	installRecordsArray, ok := installRecordsProps["records"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_install_records records schema: %#v", installRecordsSchema)
	}
	installRecordItems, ok := installRecordsArray["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_install_records item schema: %#v", installRecordsArray)
	}
	installRecordProps, ok := installRecordItems["properties"].(map[string]any)
	if !ok || installRecordProps["scenario_id"] == nil || installRecordProps["integrity"] == nil {
		t.Fatalf("expected install record properties: %#v", installRecordItems)
	}
	billingProps, ok := billingRecordsSchema["properties"].(map[string]any)
	if !ok || billingProps["totals"] == nil || billingProps["integrity"] == nil {
		t.Fatalf("expected list_billing_records totals schema: %#v", billingRecordsSchema)
	}
	billingRecordsArray, ok := billingProps["records"].(map[string]any)
	if !ok {
		t.Fatalf("expected list_billing_records records schema: %#v", billingRecordsSchema)
	}
	billingRecordItems, ok := billingRecordsArray["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected billing record items schema: %#v", billingRecordsArray)
	}
	billingRecordProps, ok := billingRecordItems["properties"].(map[string]any)
	if !ok || billingRecordProps["scenario_id"] == nil {
		t.Fatalf("expected billing record scenario_id schema: %#v", billingRecordItems)
	}
	snapshotProps, ok := commerceSnapshotSchema["properties"].(map[string]any)
	if !ok || snapshotProps["packs"] == nil || snapshotProps["scenarios"] == nil || snapshotProps["install_records"] == nil || snapshotProps["billing"] == nil || snapshotProps["integrity"] == nil {
		t.Fatalf("expected commerce snapshot schemas: %#v", commerceSnapshotSchema)
	}
	commerceIntegrityProps, ok := commerceIntegritySchema["properties"].(map[string]any)
	if !ok || commerceIntegrityProps["summary"] == nil || commerceIntegrityProps["ledgers"] == nil || commerceIntegrityProps["checks"] == nil {
		t.Fatalf("expected commerce integrity schemas: %#v", commerceIntegritySchema)
	}
	commerceProofProps, ok := commerceProofSchema["properties"].(map[string]any)
	if !ok || commerceProofProps["challenge"] == nil || commerceProofProps["payload_hash"] == nil || commerceProofProps["signature"] == nil || commerceProofProps["payload"] == nil {
		t.Fatalf("expected commerce proof schemas: %#v", commerceProofSchema)
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
	if _, ok := proStatusProps["recommended_actions"].(map[string]any); !ok {
		t.Fatalf("expected get_pro_status recommended_actions schema: %#v", proStatusProps)
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
	if _, ok := runOutputProps["scenario_id"].(map[string]any); !ok {
		t.Fatalf("expected run output scenario_id schema: %#v", runOutputProps)
	}
	if _, ok := runOutputProps["attributed_files"].(map[string]any); !ok {
		t.Fatalf("expected run output attributed_files schema: %#v", runOutputProps)
	}
	usageEvents, ok := runOutputProps["usage_events"].(map[string]any)
	if !ok {
		t.Fatalf("expected run usage_events schema: %#v", runOutputProps)
	}
	usageItems, ok := usageEvents["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected run usage event item schema: %#v", usageEvents)
	}
	usageProps, ok := usageItems["properties"].(map[string]any)
	if !ok || usageProps["scenario_id"] == nil {
		t.Fatalf("expected run usage event scenario_id schema: %#v", usageItems)
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
	if !ok || runErrorDataProps["exit_code"] == nil || runErrorDataProps["scenario_id"] == nil {
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
	service := testMCPService(t)
	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_agent_targets","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_agent_target","arguments":{"target":"cc"}}}`+"\n",
		2,
	)

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
	service := testMCPService(t)
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Authenticated bool   `json:"authenticated"`
				AuthPath      string `json:"auth_path"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	output := runMCPToolNoArgs(t, service, "get_pro_status", &response)
	if response.Result.IsError || response.Result.StructuredContent.Authenticated || response.Result.StructuredContent.AuthPath == "" {
		t.Fatalf("expected unauthenticated pro status: %s", output)
	}
}

func TestMCPListConfigKeysTool(t *testing.T) {
	service := testMCPService(t)
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
	output := runMCPToolNoArgs(t, service, "list_config_keys", &response)
	if response.Result.IsError || len(response.Result.StructuredContent) == 0 {
		t.Fatalf("expected config keys result: %s", output)
	}
	foundRegistryURL := false
	for _, item := range response.Result.StructuredContent {
		if item.Key == "registry_url" {
			foundRegistryURL = item.Type == "url" && item.Mutable
			break
		}
	}
	if !foundRegistryURL {
		t.Fatalf("expected registry_url key metadata: %s", output)
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

	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_registry_sources","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"validate_registry","arguments":{"path":"`+strings.ReplaceAll(registryPath, `\`, `\\`)+`"}}}`+"\n",
		2,
	)

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
	service := testMCPService(t)
	service.Config.ProAPIURL = "https://pro.example.com"
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
						NextActions []core.ProSetupAction `json:"next_actions"`
					} `json:"details"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	output := runMCPToolNoArgs(t, service, "list_pro_devices", &response)
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "unauthorized" {
		t.Fatalf("expected unauthorized structured error: %s", output)
	}
	if response.Result.StructuredContent.Error.Details.ProSetup.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected pro setup in details: %s", output)
	}
	if !slices.ContainsFunc(response.Result.StructuredContent.Error.Details.NextActions, func(action core.ProSetupAction) bool { return action.ID == "restart_login" }) {
		t.Fatalf("expected restart_login in details: %s", output)
	}
}

func TestMCPGetProSetupTool(t *testing.T) {
	service := testMCPService(t)
	service.Config.ProAPIURL = "https://pro.example.com"
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Authenticated      bool                  `json:"authenticated"`
				HasPendingLogin    bool                  `json:"has_pending_login"`
				CanRegisterScheme  bool                  `json:"can_register_scheme"`
				ProAPIURL          string                `json:"pro_api_url"`
				CurrentStatus      []string              `json:"current_status"`
				RecommendedActions []core.ProSetupAction `json:"recommended_actions"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	output := runMCPToolNoArgs(t, service, "get_pro_setup", &response)
	if response.Result.IsError {
		t.Fatalf("expected success result: %s", output)
	}
	if response.Result.StructuredContent.Authenticated || response.Result.StructuredContent.HasPendingLogin {
		t.Fatalf("expected setup preview before login: %s", output)
	}
	if response.Result.StructuredContent.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected configured pro api url: %s", output)
	}
	if !slices.Contains(response.Result.StructuredContent.CurrentStatus, "pro_api_configured") || !slices.Contains(response.Result.StructuredContent.CurrentStatus, "not_authenticated") {
		t.Fatalf("expected setup statuses: %s", output)
	}
	if !slices.ContainsFunc(response.Result.StructuredContent.RecommendedActions, func(action core.ProSetupAction) bool { return action.ID == "start_login" }) {
		t.Fatalf("expected start_login action: %s", output)
	}
}

func TestMCPGetProSetupToolInvalidAuthPreview(t *testing.T) {
	service := testMCPService(t)
	service.Config.ProAPIURL = "https://pro.example.com"
	if err := os.MkdirAll(filepath.Dir(service.Paths.AuthFile), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(service.Paths.AuthFile, []byte(`{"schema_version":1,"access_token":"secret","extra":true}`), 0o600); err != nil {
		t.Fatalf("write invalid auth: %v", err)
	}

	var response mcpProPreviewResponse
	output := runMCPToolNoArgs(t, service, "get_pro_setup", &response)
	if response.Result.IsError || !slices.Contains(response.Result.StructuredContent.CurrentStatus, "auth_invalid") {
		t.Fatalf("expected auth_invalid preview: %s", output)
	}
	if !slices.ContainsFunc(response.Result.StructuredContent.RecommendedActions, func(action core.ProSetupAction) bool { return action.ID == "reset_local_auth" }) {
		t.Fatalf("expected reset_local_auth action: %s", output)
	}
}

func TestMCPGetProStatusPendingLoginPreview(t *testing.T) {
	service := testMCPService(t)
	service.Config.ProAPIURL = "https://pro.example.com"
	if _, err := service.ProLoginStart(context.Background()); err != nil {
		t.Fatalf("pro login start: %v", err)
	}

	var response mcpProPreviewResponse
	output := runMCPToolNoArgs(t, service, "get_pro_status", &response)
	if response.Result.IsError || !slices.Contains(response.Result.StructuredContent.CurrentStatus, "pending_login") {
		t.Fatalf("expected pending_login preview: %s", output)
	}
	if !slices.ContainsFunc(response.Result.StructuredContent.RecommendedActions, func(action core.ProSetupAction) bool { return action.ID == "complete_login" }) {
		t.Fatalf("expected complete_login action: %s", output)
	}
}

func TestMCPGetProStatusInvalidAuthPreview(t *testing.T) {
	service := testMCPService(t)
	if err := os.MkdirAll(filepath.Dir(service.Paths.AuthFile), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(service.Paths.AuthFile, []byte(`{"schema_version":1,"access_token":"secret","extra":true}`), 0o600); err != nil {
		t.Fatalf("write invalid auth: %v", err)
	}

	var response mcpProPreviewResponse
	output := runMCPToolNoArgs(t, service, "get_pro_status", &response)
	if response.Result.IsError || !slices.Contains(response.Result.StructuredContent.CurrentStatus, "auth_invalid") {
		t.Fatalf("expected auth_invalid preview: %s", output)
	}
	if !slices.ContainsFunc(response.Result.StructuredContent.RecommendedActions, func(action core.ProSetupAction) bool { return action.ID == "reset_local_auth" }) {
		t.Fatalf("expected reset_local_auth action: %s", output)
	}
}

func TestMCPStartProLoginTool(t *testing.T) {
	service := testMCPService(t)
	service.Config.ProAPIURL = "https://pro.example.com"
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
	output := runMCPToolNoArgs(t, service, "start_pro_login", &response)
	if response.Result.IsError || !strings.Contains(response.Result.StructuredContent.LoginURL, "https://pro.example.com/v1/cli/login/start?") || response.Result.StructuredContent.State == "" || response.Result.StructuredContent.AuthPath == "" {
		t.Fatalf("expected login start result: %s", output)
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

	service := testMCPService(t)
	service.Config.ProAPIURL = server.URL
	login, err := service.ProLoginStart(context.Background())
	if err != nil {
		t.Fatalf("login start: %v", err)
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
	output := runMCPTool(t, service, "complete_pro_login", `{"callback_uri":"agtx://pro/callback?code=abc&state=`+login.State+`"}`, &response)
	if response.Result.IsError || !response.Result.StructuredContent.Authenticated || response.Result.StructuredContent.DeviceLimit != 3 || response.Result.StructuredContent.Subscription != "active" || response.Result.StructuredContent.RegistryURL == "" {
		t.Fatalf("expected complete_pro_login result: %s", output)
	}
	if tokenRequest != "" {
		t.Fatalf("token exchange should not send bearer auth, got %q", tokenRequest)
	}
}

func TestMCPLogoutProTool(t *testing.T) {
	service := testMCPService(t)
	service.Auth = core.AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device-1"}
	if err := core.SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
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
	output := runMCPToolNoArgs(t, service, "logout_pro", &response)
	if response.Result.IsError || !response.Result.StructuredContent.LoggedOut || response.Result.StructuredContent.AuthPath == "" {
		t.Fatalf("expected logout_pro result: %s", output)
	}
	if _, err := os.Stat(service.Paths.AuthFile); !os.IsNotExist(err) {
		t.Fatalf("expected auth file removed, got err=%v", err)
	}
}

func TestMCPRegisterProSchemeTool(t *testing.T) {
	service := testMCPService(t)
	restore := core.SwapProRegisterSchemeHookForTest(func() (core.ProSchemeResult, error) {
		return core.ProSchemeResult{Scheme: "agtx", Command: `"C:\agtx.exe" pro callback "%1"`}, nil
	})
	defer restore()
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
	output := runMCPToolNoArgs(t, service, "register_pro_scheme", &response)
	if response.Result.IsError {
		t.Fatalf("expected success result: %s", output)
	}
	if response.Result.StructuredContent.Scheme != "agtx" {
		t.Fatalf("expected agtx scheme result: %s", output)
	}
}

func TestMCPRevokeProDeviceRequiresYes(t *testing.T) {
	service := testMCPService(t)
	output := runMCPTool(t, service, "revoke_pro_device", `{"device_id":"device-1"}`, nil)
	if !strings.Contains(output, "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", output)
	}
	assertMCPConfirmationDetails(t, []byte(output), "revoke_pro_device", "device_id")
}

func TestMCPGetAgentTargetRejectsUnknownTarget(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(targets, "codex") || !slices.Contains(targets, "cursor") {
		t.Fatalf("expected supported targets: %#v", details["supported_targets"])
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !slices.Contains(args, "target") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPInstallRequiresYes(t *testing.T) {
	service := testMCPService(t)
	var response struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	output := runMCPTool(t, service, "install_skill", `{"skill":"pdf"}`, &response)
	if !response.Result.IsError || !strings.Contains(response.Result.Content[0].Text, "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", output)
	}
	if response.Result.Content[0].Text == "" {
		t.Fatalf("expected text content")
	}
	assertMCPConfirmationDetails(t, []byte(output), "install_skill", "skill")
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
	hasArg := func(want string) bool {
		return slices.ContainsFunc(args, func(value any) bool { return value == want })
	}
	if !ok || !hasArg("yes") || !hasArg(supportedArgument) {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPToolErrorIncludesStructuredContent(t *testing.T) {
	service := testMCPService(t)
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
	output := runMCPTool(t, service, "install_skill", `{"skill":"pdf"}`, &response)
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "confirmation_required" {
		t.Fatalf("expected structured confirmation error: %s", output)
	}
}

func TestMCPContentLengthFraming(t *testing.T) {
	service := testMCPService(t)
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
	service := testMCPService(t)
	output := runMCP(t, service, "\n  \t\n  {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"search_skills\",\"arguments\":{\"query\":\"pdf\",\"limit\":1}}}\n", nil)
	if !strings.Contains(output, "pdf") {
		t.Fatalf("expected pdf response after blank and indented lines: %s", output)
	}
}

func TestMCPRejectsOversizedContentLength(t *testing.T) {
	service := testMCPService(t)
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
	service := testMCPService(t)
	output := runMCPTool(t, service, "plan_install", `{"skill":"pdf"}`, nil)
	if !strings.Contains(output, `\"action\": \"install\"`) {
		t.Fatalf("expected install plan: %s", output)
	}
}

func TestMCPCapabilityPackCommerceSnapshot(t *testing.T) {
	service := testMCPService(t)
	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"install_capability_pack","arguments":{"pack":"gaoji","yes":true}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_commerce_snapshot","arguments":{"pack_id":"advanced","type":"pack_install","currency":"USD","limit":10}}}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_commerce_integrity","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_commerce_proof","arguments":{"challenge":"mcp-nonce"}}}`+"\n",
		4,
	)
	var install struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Pack struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
					Installed bool `json:"installed"`
				} `json:"pack"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &install); err != nil {
		t.Fatalf("invalid install json: %v\n%s", err, lines[0])
	}
	if install.Result.IsError || install.Result.StructuredContent.Pack.Pack.ID != "advanced" || !install.Result.StructuredContent.Pack.Installed {
		t.Fatalf("expected installed advanced pack response: %s", lines[0])
	}
	var snapshot struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Packs []struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
					Installed bool `json:"installed"`
				} `json:"packs"`
				InstallRecords struct {
					Records []struct {
						PackID string `json:"pack_id"`
					} `json:"records"`
					Integrity struct {
						Status string `json:"status"`
					} `json:"integrity"`
				} `json:"install_records"`
				Billing struct {
					Records []struct {
						PackID string `json:"pack_id"`
						Meter  string `json:"meter"`
					} `json:"records"`
				} `json:"billing"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &snapshot); err != nil {
		t.Fatalf("invalid snapshot json: %v\n%s", err, lines[1])
	}
	if snapshot.Result.IsError {
		t.Fatalf("snapshot should not be an error: %s", lines[1])
	}
	if len(snapshot.Result.StructuredContent.Packs) != 1 {
		t.Fatalf("expected filtered advanced capability pack in snapshot: %s", lines[1])
	}
	if !snapshot.Result.StructuredContent.Packs[0].Installed || snapshot.Result.StructuredContent.Packs[0].Pack.ID != "advanced" {
		t.Fatalf("expected installed advanced pack: %s", lines[1])
	}
	if len(snapshot.Result.StructuredContent.InstallRecords.Records) != 1 || snapshot.Result.StructuredContent.InstallRecords.Records[0].PackID != "advanced" || snapshot.Result.StructuredContent.InstallRecords.Integrity.Status == "" {
		t.Fatalf("expected advanced install record: %s", lines[1])
	}
	if len(snapshot.Result.StructuredContent.Billing.Records) != 1 || snapshot.Result.StructuredContent.Billing.Records[0].PackID != "advanced" || snapshot.Result.StructuredContent.Billing.Records[0].Meter != "seat" {
		t.Fatalf("expected advanced billing records: %s", lines[1])
	}
	var integrity struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				OK      bool `json:"ok"`
				Ledgers []struct {
					Status string `json:"status"`
				} `json:"ledgers"`
				Checks []struct {
					Name string `json:"name"`
				} `json:"checks"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &integrity); err != nil {
		t.Fatalf("invalid integrity json: %v\n%s", err, lines[2])
	}
	if integrity.Result.IsError || !integrity.Result.StructuredContent.OK || len(integrity.Result.StructuredContent.Ledgers) != 3 || len(integrity.Result.StructuredContent.Checks) == 0 {
		t.Fatalf("expected commerce integrity result: %s", lines[2])
	}
	for _, ledger := range integrity.Result.StructuredContent.Ledgers {
		if ledger.Status == "" {
			t.Fatalf("expected ledger integrity status: %s", lines[2])
		}
	}
	var proof struct {
		Result struct {
			IsError           bool               `json:"isError"`
			StructuredContent core.CommerceProof `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &proof); err != nil {
		t.Fatalf("invalid proof json: %v\n%s", err, lines[3])
	}
	if proof.Result.IsError || !core.VerifyCommerceProof(proof.Result.StructuredContent, "mcp-nonce").OK {
		t.Fatalf("expected commerce proof result: %s", lines[3])
	}
}

func TestMCPSubmitProofStoresAndListsReceipt(t *testing.T) {
	paths := core.PathsForRoot(t.TempDir())
	service := core.NewService(paths)
	receiptPublicKey, receiptPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate receipt key: %v", err)
	}
	var submitCalls int
	var gotPath string
	var gotAuth string
	var gotDevice string
	var gotRequest testCommerceProofSubmitRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		submitCalls++
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
		if !gotRequest.Verification.OK || !core.VerifyCommerceProof(gotRequest.Proof, "mcp-submit-nonce").OK {
			http.Error(writer, "invalid commerce proof", http.StatusBadRequest)
			return
		}
		receipt := testSignedCommerceReceipt(t, gotRequest.Proof, receiptPublicKey, receiptPrivateKey, 1)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(testCommerceProofSubmitResponse{OK: true, Receipt: receipt})
	}))
	defer server.Close()

	service.Config.ProAPIURL = server.URL
	service.Auth = core.AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device-1"}
	if err := core.SaveAuth(paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}

	output := runMCPTool(t, service, "submit_commerce_proof", `{"challenge":"mcp-submit-nonce"}`, nil)
	if submitCalls != 0 {
		t.Fatalf("submit without yes should not call Pro, got %d calls", submitCalls)
	}
	if !strings.Contains(output, "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", output)
	}
	assertMCPConfirmationDetails(t, []byte(output), "submit_commerce_proof", "challenge")

	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"submit_commerce_proof","arguments":{"challenge":"mcp-submit-nonce","yes":true}}}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_commerce_receipts","arguments":{"status":"server_received","limit":10}}}`+"\n",
		2,
	)
	if submitCalls != 1 || gotPath != "/v1/commerce/proofs" || gotAuth != "Bearer access" || gotDevice != "device-1" {
		t.Fatalf("unexpected proof submit request: calls=%d path=%q auth=%q device=%q", submitCalls, gotPath, gotAuth, gotDevice)
	}
	var submit struct {
		Result struct {
			IsError           bool                             `json:"isError"`
			StructuredContent core.CommerceReceiptSubmitResult `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &submit); err != nil {
		t.Fatalf("invalid submit receipt json: %v\n%s", err, lines[0])
	}
	if submit.Result.IsError || !submit.Result.StructuredContent.Verification.OK || submit.Result.StructuredContent.Receipt.ReceiptID == "" || submit.Result.StructuredContent.Receipt.Status != "server_received" {
		t.Fatalf("unexpected submit receipt response: %s", lines[0])
	}
	if submit.Result.StructuredContent.Receipt.Integrity == nil || submit.Result.StructuredContent.Receipt.Integrity.Status == "" {
		t.Fatalf("expected locally signed receipt integrity: %#v", submit.Result.StructuredContent.Receipt)
	}
	if !core.VerifyCommerceReceipt(submit.Result.StructuredContent.Proof, submit.Result.StructuredContent.Receipt).OK {
		t.Fatalf("receipt should verify against submitted proof: %#v", submit.Result.StructuredContent)
	}
	var receipts struct {
		Result struct {
			IsError           bool                           `json:"isError"`
			StructuredContent core.CommerceReceiptListResult `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &receipts); err != nil {
		t.Fatalf("invalid list receipts json: %v\n%s", err, lines[1])
	}
	if receipts.Result.IsError || len(receipts.Result.StructuredContent.Records) != 1 || receipts.Result.StructuredContent.Records[0].ReceiptID != submit.Result.StructuredContent.Receipt.ReceiptID || receipts.Result.StructuredContent.Integrity == nil || receipts.Result.StructuredContent.Integrity.Status == "" {
		t.Fatalf("unexpected list receipts response: %s", lines[1])
	}
}

func TestMCPPlanCapabilityPackInstall(t *testing.T) {
	service := testMCPService(t)
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Action string `json:"action"`
				Pack   struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
				} `json:"pack"`
				Changes []struct {
					Name string `json:"name"`
				} `json:"changes"`
				BillingPreview []struct {
					PackID string `json:"pack_id"`
					Meter  string `json:"meter"`
				} `json:"billing_preview"`
				Requires []string `json:"requires"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	output := runMCPTool(t, service, "plan_capability_pack_install", `{"pack":"standard"}`, &response)
	if response.Result.IsError || response.Result.StructuredContent.Action != "install_pack" || response.Result.StructuredContent.Pack.Pack.ID != "standard" || len(response.Result.StructuredContent.Changes) == 0 || len(response.Result.StructuredContent.BillingPreview) != 2 || !slices.Contains(response.Result.StructuredContent.Requires, "confirmation") {
		t.Fatalf("unexpected pack plan response: %s", output)
	}
}

func TestMCPWebsiteCapabilityPackPlan(t *testing.T) {
	service := testMCPService(t)
	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"plan_capability_pack_install","arguments":{"pack":"pdf"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"plan_capability_pack_install","arguments":{"pack":"imagen"}}}`+"\n",
		2,
	)
	var pdf struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Pack struct {
					Pack struct {
						ID              string `json:"id"`
						CapabilityClass string `json:"capability_class"`
						UseWhen         string `json:"use_when"`
					} `json:"pack"`
				} `json:"pack"`
				Changes []struct {
					Name string `json:"name"`
				} `json:"changes"`
				BillingPreview []struct {
					PackID string `json:"pack_id"`
					Meter  string `json:"meter"`
				} `json:"billing_preview"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &pdf); err != nil {
		t.Fatalf("invalid pdf plan json: %v\n%s", err, lines[0])
	}
	if pdf.Result.IsError || pdf.Result.StructuredContent.Pack.Pack.ID != "pdf" || pdf.Result.StructuredContent.Pack.Pack.CapabilityClass != "tool" || pdf.Result.StructuredContent.Pack.Pack.UseWhen == "" || len(pdf.Result.StructuredContent.Changes) != 1 || len(pdf.Result.StructuredContent.BillingPreview) != 1 || pdf.Result.StructuredContent.BillingPreview[0].PackID != "pdf" || pdf.Result.StructuredContent.BillingPreview[0].Meter != "page" {
		t.Fatalf("unexpected pdf plan response: %s", lines[0])
	}
	var media struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Pack struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
				} `json:"pack"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &media); err != nil {
		t.Fatalf("invalid media plan json: %v\n%s", err, lines[1])
	}
	if media.Result.IsError || media.Result.StructuredContent.Pack.Pack.ID != "imagen" {
		t.Fatalf("expected imagen pack plan: %s", lines[1])
	}
}

func TestMCPScenarioInstallPlanAndRecords(t *testing.T) {
	service := testMCPService(t)
	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"plan_capability_scenario_install","arguments":{"scenario":"invoice"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"install_capability_scenario","arguments":{"scenario":"invoice","yes":true}}}`+"\n"+
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_install_records","arguments":{"scenario_id":"invoice_processing"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_billing_records","arguments":{"scenario_id":"invoice_processing","type":"pack_install","limit":10}}}`+"\n"+
			`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_capability_scenario_ledger","arguments":{"scenario":"invoice","type":"pack_install","limit":10}}}`+"\n",
		5,
	)

	var plan struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
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
					Requires []string `json:"requires"`
				} `json:"pack_plan"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &plan); err != nil {
		t.Fatalf("invalid scenario plan json: %v\n%s", err, lines[0])
	}
	if plan.Result.IsError || plan.Result.StructuredContent.Action != "install_scenario" || plan.Result.StructuredContent.Scenario.Scenario.ID != "invoice_processing" || plan.Result.StructuredContent.PackPlan.Pack.Pack.ID != "standard" || len(plan.Result.StructuredContent.PackPlan.BillingPreview) != 2 || plan.Result.StructuredContent.PackPlan.BillingPreview[0].ScenarioID != "invoice_processing" || !slices.Contains(plan.Result.StructuredContent.PackPlan.Requires, "confirmation") {
		t.Fatalf("unexpected scenario plan response: %s", lines[0])
	}

	var install struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
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
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &install); err != nil {
		t.Fatalf("invalid scenario install json: %v\n%s", err, lines[1])
	}
	if install.Result.IsError || install.Result.StructuredContent.Scenario.Scenario.ID != "invoice_processing" || !install.Result.StructuredContent.Scenario.Ready || install.Result.StructuredContent.PackInstall.InstallRecord.Action != "install_scenario" || install.Result.StructuredContent.PackInstall.InstallRecord.PackID != "standard" || install.Result.StructuredContent.PackInstall.InstallRecord.ScenarioID != "invoice_processing" || len(install.Result.StructuredContent.PackInstall.BillingRecords) != 2 || install.Result.StructuredContent.PackInstall.BillingRecords[0].ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario install response: %s", lines[1])
	}

	var installs struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Records []struct {
					Action     string `json:"action"`
					ScenarioID string `json:"scenario_id"`
				} `json:"records"`
				Integrity struct {
					Status string `json:"status"`
				} `json:"integrity"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &installs); err != nil {
		t.Fatalf("invalid scenario install records json: %v\n%s", err, lines[2])
	}
	if installs.Result.IsError || len(installs.Result.StructuredContent.Records) != 1 || installs.Result.StructuredContent.Records[0].Action != "install_scenario" || installs.Result.StructuredContent.Records[0].ScenarioID != "invoice_processing" || installs.Result.StructuredContent.Integrity.Status == "" {
		t.Fatalf("unexpected scenario install records: %s", lines[2])
	}

	var billing struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Records []struct {
					ScenarioID string `json:"scenario_id"`
					Type       string `json:"type"`
				} `json:"records"`
				Totals []struct {
					Currency string `json:"currency"`
				} `json:"totals"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &billing); err != nil {
		t.Fatalf("invalid scenario billing records json: %v\n%s", err, lines[3])
	}
	if billing.Result.IsError || len(billing.Result.StructuredContent.Records) != 2 || billing.Result.StructuredContent.Records[0].ScenarioID != "invoice_processing" || billing.Result.StructuredContent.Records[0].Type != "pack_install" || len(billing.Result.StructuredContent.Totals) != 2 {
		t.Fatalf("unexpected scenario billing records: %s", lines[3])
	}

	var ledger struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
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
				} `json:"billing"`
				PackInstallRecords []struct {
					Type string `json:"type"`
				} `json:"pack_install_records"`
				UsageRecords []struct {
					Type string `json:"type"`
				} `json:"usage_records"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[4]), &ledger); err != nil {
		t.Fatalf("invalid scenario ledger json: %v\n%s", err, lines[4])
	}
	if ledger.Result.IsError || ledger.Result.StructuredContent.Scenario.Scenario.ID != "invoice_processing" || ledger.Result.StructuredContent.LatestInstall.Action != "install_scenario" || ledger.Result.StructuredContent.LatestInstall.ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario ledger: %s", lines[4])
	}
	if len(ledger.Result.StructuredContent.InstallRecords) != 1 || len(ledger.Result.StructuredContent.Billing.Records) != 2 || len(ledger.Result.StructuredContent.PackInstallRecords) != 2 || len(ledger.Result.StructuredContent.UsageRecords) != 0 {
		t.Fatalf("unexpected scenario ledger records: %s", lines[4])
	}
}

func TestMCPCapabilityScenarios(t *testing.T) {
	service := testMCPService(t)
	lines := runMCPLines(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_capability_scenarios","arguments":{"pack_id":"advanced"}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_capability_scenario","arguments":{"scenario":"invoice"}}}`+"\n",
		2,
	)
	var list struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent []struct {
				Scenario struct {
					ID string `json:"id"`
				} `json:"scenario"`
				RecommendedPack struct {
					Pack struct {
						ID string `json:"id"`
					} `json:"pack"`
				} `json:"recommended_pack"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &list); err != nil {
		t.Fatalf("invalid scenario list json: %v\n%s", err, lines[0])
	}
	if list.Result.IsError || len(list.Result.StructuredContent) == 0 {
		t.Fatalf("expected advanced scenarios: %s", lines[0])
	}
	for _, scenario := range list.Result.StructuredContent {
		if scenario.RecommendedPack.Pack.ID != "advanced" {
			t.Fatalf("expected only advanced scenarios: %s", lines[0])
		}
	}
	var detail struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
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
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &detail); err != nil {
		t.Fatalf("invalid scenario detail json: %v\n%s", err, lines[1])
	}
	if detail.Result.IsError || detail.Result.StructuredContent.Scenario.ID != "invoice_processing" || detail.Result.StructuredContent.RecommendedPack.Pack.ID != "standard" {
		t.Fatalf("expected invoice scenario detail: %s", lines[1])
	}
	if detail.Result.StructuredContent.Ready || detail.Result.StructuredContent.InstallPlan.Action != "install_pack" || len(detail.Result.StructuredContent.MissingSkills) == 0 || len(detail.Result.StructuredContent.BillingPreviewTotals) != 2 {
		t.Fatalf("expected invoice scenario readiness and billing preview: %s", lines[1])
	}
	if len(detail.Result.StructuredContent.Scenario.Inputs) == 0 || len(detail.Result.StructuredContent.Scenario.Deliverables) == 0 || len(detail.Result.StructuredContent.Scenario.Workflow) == 0 || len(detail.Result.StructuredContent.Scenario.AcceptanceCriteria) == 0 {
		t.Fatalf("expected invoice scenario workflow metadata: %s", lines[1])
	}
}

func TestMCPRunSkillAcceptsScenarioID(t *testing.T) {
	service := testMCPService(t)
	if _, err := service.InstallSkills(context.Background(), []string{"web_search"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				OK   bool `json:"ok"`
				Data struct {
					Name       string `json:"name"`
					ScenarioID string `json:"scenario_id"`
				} `json:"data"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	output := runMCPTool(t, service, "run_skill", `{"skill":"web_search","scenario_id":"invoice"}`, &response)
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "invalid_argument" {
		t.Fatalf("expected structured run error for missing search query: %s", output)
	}
	if response.Result.StructuredContent.Data.Name != "web_search" || response.Result.StructuredContent.Data.ScenarioID != "invoice_processing" {
		t.Fatalf("expected canonical scenario id in partial run data: %s", output)
	}
}

func TestMCPRunSkillAcceptsStructuredRapidOCROptions(t *testing.T) {
	service := testMCPService(t)
	request := map[string]any{
		"name": "run_skill",
		"arguments": map[string]any{
			"skill": "rapidocr",
			"ocr": map[string]any{
				"download_models": true,
				"dry_run":         true,
				"model_dir":       t.TempDir(),
			},
		},
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	s := &server{service: service}
	response, err := s.callTool(data)
	if err != nil {
		t.Fatalf("run structured ocr request: %v", err)
	}
	if isError, _ := response["isError"].(bool); isError {
		t.Fatalf("expected successful OCR dry-run response: %#v", response)
	}
	result, ok := response["structuredContent"].(core.RunResult)
	if !ok {
		t.Fatalf("expected run result structured content: %#v", response["structuredContent"])
	}
	if result.Name != "ocr" || result.Version != "0.6.0" || result.Stub || len(result.UsageEvents) != 0 {
		t.Fatalf("unexpected OCR run result: %#v", result)
	}
	var download struct {
		ModelProfile string `json:"model_profile"`
		ModelSize    string `json:"model_size"`
		NoPython     bool   `json:"no_python"`
		DryRun       bool   `json:"dry_run"`
		Assets       []struct {
			Status string `json:"status"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &download); err != nil {
		t.Fatalf("decode OCR download result: %v stdout=%s", err, result.Stdout)
	}
	if download.ModelProfile != "rapidocr" || download.ModelSize != "mobile" || !download.NoPython || !download.DryRun || len(download.Assets) != 3 {
		t.Fatalf("unexpected OCR dry-run payload: %#v", download)
	}
	for _, asset := range download.Assets {
		if asset.Status != "planned" {
			t.Fatalf("expected planned OCR asset: %#v", asset)
		}
	}
}

func TestMCPRejectsOCROptionsForNonOCRSkill(t *testing.T) {
	s := &server{service: testMCPService(t)}
	_, err := s.callTool(json.RawMessage(`{"name":"run_skill","arguments":{"skill":"pdf","ocr":{"probe":true}}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "ocr options are only supported for the built-in OCR skill and its aliases" {
		t.Fatalf("unexpected error: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "run_skill" || details["argument"] != "ocr" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
	skills, ok := details["supported_skills"].([]string)
	if !ok || !slices.Contains(skills, "rapidocr") || !slices.Contains(skills, "ppocrv6") {
		t.Fatalf("expected supported OCR aliases: %#v", details["supported_skills"])
	}
}

func TestMCPRejectsMutuallyExclusiveSkillInputs(t *testing.T) {
	s := &server{service: testMCPService(t)}
	_, err := s.callTool(json.RawMessage(`{"name":"run_skill","arguments":{"skill":"rapidocr","input":"text","input_base64":"dGV4dA=="}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "input and input_base64 cannot both be set" {
		t.Fatalf("unexpected error: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "run_skill" || details["expected"] != "only_one_argument" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
}

func TestMCPRejectsInvalidBase64SkillInput(t *testing.T) {
	s := &server{service: testMCPService(t)}
	_, err := s.callTool(json.RawMessage(`{"name":"run_skill","arguments":{"skill":"rapidocr","input_base64":"not base64"}}`))
	if !core.IsErrorCode(err, core.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	coreErr := core.ErrorFrom(err)
	if coreErr.Message != "input_base64 must be valid base64" {
		t.Fatalf("unexpected error: %#v", coreErr)
	}
	details, ok := coreErr.Details.(map[string]any)
	if !ok || details["tool"] != "run_skill" || details["argument"] != "input_base64" || details["expected"] != "base64_string" {
		t.Fatalf("unexpected details: %#v", coreErr.Details)
	}
}

func TestMCPUninstallRequiresYes(t *testing.T) {
	service := testMCPService(t)
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	output := runMCPTool(t, service, "uninstall_skill", `{"skill":"pdf"}`, nil)
	if !strings.Contains(output, "confirmation_required") {
		t.Fatalf("expected confirmation error: %s", output)
	}
	assertMCPConfirmationDetails(t, []byte(output), "uninstall_skill", "skill")
}

func TestMCPDoctorAndVerifySkill(t *testing.T) {
	service := testMCPService(t)
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	output := runMCP(t, service,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"verify_skill","arguments":{"skill":"pdf"}}}`+"\n",
		nil,
	)
	if !strings.Contains(output, `\"checks\"`) || !strings.Contains(output, `\"name\": \"pdf\"`) {
		t.Fatalf("expected doctor and verify responses: %s", output)
	}
}

func TestMCPVerifyErrorPreservesPartialResult(t *testing.T) {
	service := testMCPService(t)
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	manifestPath := filepath.Join(service.Paths.SkillsDir, "pdf", "0.2.0", "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
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
	output := runMCPTool(t, service, "verify_skill", `{"skill":"pdf"}`, &response)
	if !response.Result.IsError || response.Result.StructuredContent.OK || response.Result.StructuredContent.Error.Code != "integrity_failed" {
		t.Fatalf("expected structured verify error: %s", output)
	}
	if response.Result.StructuredContent.Data.Name != "pdf" || len(response.Result.StructuredContent.Data.Checks) == 0 {
		t.Fatalf("expected partial verify data: %s", output)
	}
}

func TestMCPRejectsUnknownToolArgument(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(args, "query") || !slices.Contains(args, "limit") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsNonObjectToolArguments(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(args, "query") || !slices.Contains(args, "limit") {
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
			s := &server{service: testMCPService(t)}
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
			if !ok || !slices.Contains(args, tt.argument) || !slices.Contains(args, tt.supportedArgument) {
				t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
			}
		})
	}
}

func TestMCPRejectsMissingPlanInstallSkillArguments(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(names, "skill") || !slices.Contains(names, "skills") {
		t.Fatalf("expected argument alternatives: %#v", details["arguments"])
	}
	args, ok := details["supported_arguments"].([]string)
	if !ok || !slices.Contains(args, "skill") || !slices.Contains(args, "skills") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsMissingToolNameWithSupportedTools(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(tools, "search_skills") || !slices.Contains(tools, "run_skill") {
		t.Fatalf("expected supported_tools details: %#v", details["supported_tools"])
	}
}

func TestMCPRejectsUnknownToolWithSupportedTools(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(tools, "search_skills") || !slices.Contains(tools, "run_skill") {
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
	if !ok || !slices.Contains(keys, "registry_url") || !slices.Contains(keys, "package_max_bytes") {
		t.Fatalf("expected supported_keys details: %#v", details["supported_keys"])
	}
}

func TestMCPRejectsWrongArgumentType(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(args, "skill") || !slices.Contains(args, "yes") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsNonPositiveIntegerArgument(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(args, "timeout_ms") || !slices.Contains(args, "output_limit_bytes") {
		t.Fatalf("expected supported_arguments details: %#v", details["supported_arguments"])
	}
}

func TestMCPRejectsTrailingArgumentJSONValue(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(params, "name") || !slices.Contains(params, "arguments") {
		t.Fatalf("expected supported_params details: %#v", details["supported_params"])
	}
}

func TestMCPRejectsUnknownToolCallParamWithSupportedParams(t *testing.T) {
	s := &server{service: testMCPService(t)}
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
	if !ok || !slices.Contains(params, "name") || !slices.Contains(params, "arguments") {
		t.Fatalf("expected supported_params details: %#v", details["supported_params"])
	}
}

func TestMCPRejectsInvalidJSONRPCVersion(t *testing.T) {
	service := testMCPService(t)
	output := runMCP(t, service, `{"jsonrpc":"1.0","id":1,"method":"tools/list","params":{}}`+"\n", nil)
	if !strings.Contains(output, `"message":"invalid request"`) || !strings.Contains(output, `jsonrpc must be 2.0`) {
		t.Fatalf("expected invalid request response: %s", output)
	}
	assertMCPInvalidRequestDetails(t, []byte(output), "jsonrpc", "jsonrpc must be 2.0")
}

func TestMCPRejectsUnknownMethodWithSupportedMethods(t *testing.T) {
	service := testMCPService(t)
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Method           string   `json:"method"`
				SupportedMethods []string `json:"supported_methods"`
			} `json:"data"`
		} `json:"error"`
	}
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":1,"method":"tools/lost","params":{}}`+"\n", &response)
	if response.Error.Code != -32601 || response.Error.Data.Method != "tools/lost" {
		t.Fatalf("unexpected unknown method response: %s", output)
	}
	if !slices.Contains(response.Error.Data.SupportedMethods, "tools/list") || !slices.Contains(response.Error.Data.SupportedMethods, "tools/call") {
		t.Fatalf("expected supported methods: %s", output)
	}
}

func TestMCPRejectsMissingMethodWithSupportedMethods(t *testing.T) {
	service := testMCPService(t)
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Error            string   `json:"error"`
				SupportedMethods []string `json:"supported_methods"`
			} `json:"data"`
		} `json:"error"`
	}
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":1,"params":{}}`+"\n", &response)
	if response.Error.Code != -32600 || response.Error.Data.Error != "missing method" {
		t.Fatalf("unexpected missing method response: %s", output)
	}
	if !slices.Contains(response.Error.Data.SupportedMethods, "initialize") || !slices.Contains(response.Error.Data.SupportedMethods, "tools/call") {
		t.Fatalf("expected supported methods: %s", output)
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
	service := testMCPService(t)
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":{},"method":"tools/list","params":{}}`+"\n", nil)
	if !strings.Contains(output, `id must be string, number, or null`) {
		t.Fatalf("expected invalid id response: %s", output)
	}
	assertMCPInvalidRequestDetails(t, []byte(output), "id", "id must be string, number, or null")
}

func TestMCPRejectsNonObjectParams(t *testing.T) {
	service := testMCPService(t)
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[]}`+"\n", nil)
	if !strings.Contains(output, `params must be an object or null`) {
		t.Fatalf("expected invalid params response: %s", output)
	}
	assertMCPInvalidRequestDetails(t, []byte(output), "params", "params must be an object or null")
}

func TestMCPRejectsInvalidJSONWithStructuredParseError(t *testing.T) {
	service := testMCPService(t)
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Error    string `json:"error"`
				Expected string `json:"expected"`
			} `json:"data"`
		} `json:"error"`
	}
	output := runMCP(t, service, `{"jsonrpc":"2.0"`+"\n", &response)
	if response.Error.Code != -32700 || response.Error.Data.Error != "invalid JSON" || response.Error.Data.Expected != "json_object" {
		t.Fatalf("unexpected parse error details: %s", output)
	}
}

func TestMCPRejectsBatchWithSupportedEnvelopeFields(t *testing.T) {
	service := testMCPService(t)
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
	output := runMCP(t, service, `[{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}]`+"\n", &response)
	if response.Error.Code != -32600 || response.Error.Data.Expected != "single_jsonrpc_request" {
		t.Fatalf("unexpected batch error details: %s", output)
	}
	if !slices.Contains(response.Error.Data.SupportedFields, "jsonrpc") || !slices.Contains(response.Error.Data.SupportedFields, "params") {
		t.Fatalf("expected supported_fields details: %s", output)
	}
}

func TestMCPRejectsUnknownTopLevelField(t *testing.T) {
	service := testMCPService(t)
	output := runMCP(t, service, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{},"extra":true}`+"\n", nil)
	if !strings.Contains(output, `"message":"invalid request"`) || !strings.Contains(output, `unknown field \"extra\"`) {
		t.Fatalf("expected strict top-level decode failure: %s", output)
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
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &response); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, output)
	}
	if response.Error.Data.Expected != "object" || !strings.Contains(response.Error.Data.Error, `unknown field "extra"`) {
		t.Fatalf("unexpected top-level field details: %s", output)
	}
	if !slices.Contains(response.Error.Data.SupportedFields, "jsonrpc") || !slices.Contains(response.Error.Data.SupportedFields, "method") {
		t.Fatalf("expected supported_fields details: %s", output)
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
