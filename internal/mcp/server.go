package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/agentex-ai/agtx/internal/agent"
	"github.com/agentex-ai/agtx/internal/core"
)

const maxMCPMessageBytes = 8 * 1024 * 1024

type server struct {
	service *core.Service
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
	framed  bool
}

type toolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type toolArguments struct {
	values  map[string]json.RawMessage
	tool    string
	allowed map[string]bool
}

func Run(service *core.Service, stdin io.Reader, stdout, stderr io.Writer) int {
	s := &server{service: service, in: stdin, out: stdout, errOut: stderr}
	if err := s.loop(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func (s *server) loop() error {
	reader := bufio.NewReader(s.in)
	for {
		message, framed, err := readMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(message)) == 0 {
			continue
		}
		s.framed = framed
		if err := s.handleLine(message); err != nil {
			fmt.Fprintln(s.errOut, err)
		}
	}
}

func readMessage(reader *bufio.Reader) ([]byte, bool, error) {
	if err := discardMessageWhitespace(reader); err != nil {
		return nil, false, err
	}
	first, err := reader.Peek(1)
	if err != nil {
		return nil, false, err
	}
	if first[0] == '{' || first[0] == '[' {
		line, err := readLineLimited(reader, maxMCPMessageBytes)
		if err != nil {
			return nil, false, err
		}
		return bytes.TrimSpace(line), false, nil
	}

	headers := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, false, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, false, fmt.Errorf("invalid MCP header: %s", line)
		}
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	length, err := strconv.Atoi(headers["content-length"])
	if err != nil || length < 0 {
		return nil, false, fmt.Errorf("invalid MCP content-length")
	}
	if length > maxMCPMessageBytes {
		return nil, false, fmt.Errorf("MCP message exceeds size limit")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, false, err
	}
	return body, true, nil
}

func discardMessageWhitespace(reader *bufio.Reader) error {
	for {
		next, err := reader.Peek(1)
		if err != nil {
			return err
		}
		switch next[0] {
		case ' ', '\t', '\r', '\n':
			if _, err := reader.ReadByte(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func readLineLimited(reader *bufio.Reader, limit int) ([]byte, error) {
	var buffer bytes.Buffer
	for {
		part, err := reader.ReadSlice('\n')
		if err != nil && err != bufio.ErrBufferFull && err != io.EOF {
			return nil, err
		}
		if buffer.Len()+len(part) > limit {
			return nil, fmt.Errorf("MCP message exceeds size limit")
		}
		if _, writeErr := buffer.Write(part); writeErr != nil {
			return nil, writeErr
		}
		if err == nil || err == io.EOF {
			return buffer.Bytes(), nil
		}
	}
}

func (s *server) handleLine(line []byte) error {
	trimmed := bytes.TrimSpace(line)
	if !json.Valid(trimmed) {
		return s.writeError(nil, -32700, "parse error", map[string]any{"error": "invalid JSON", "expected": "json_object"})
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return s.writeError(nil, -32600, "invalid request", map[string]any{
			"error":            "JSON-RPC batches are not implemented in agtx mcp v1",
			"expected":         "single_jsonrpc_request",
			"supported_fields": rpcEnvelopeFields(),
		})
	}
	var request rpcRequest
	if err := decodeJSONStrict(trimmed, &request); err != nil {
		return s.writeError(nil, -32600, "invalid request", invalidRPCEnvelopeError(err))
	}
	id := request.ID
	hasID := len(id) > 0
	if data, ok := validateRequest(request); !ok {
		if hasID {
			return s.writeError(id, -32600, "invalid request", data)
		}
		return s.writeError(nil, -32600, "invalid request", data)
	}
	if strings.TrimSpace(request.Method) == "" {
		if hasID {
			return s.writeError(id, -32600, "invalid request", map[string]any{"error": "missing method", "supported_methods": supportedMethods()})
		}
		return nil
	}

	switch request.Method {
	case "initialize":
		if !hasID {
			return nil
		}
		return s.writeResult(id, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "agtx",
				"version": core.Version,
			},
		})
	case "notifications/initialized":
		return nil
	case "tools/list":
		if !hasID {
			return nil
		}
		return s.writeResult(id, map[string]any{"tools": tools()})
	case "tools/call":
		if !hasID {
			return nil
		}
		result, err := s.callTool(request.Params)
		if err != nil {
			return s.writeResult(id, toolError(err, nil))
		}
		return s.writeResult(id, result)
	default:
		if hasID {
			return s.writeError(id, -32601, "method not found", map[string]any{"method": request.Method, "supported_methods": supportedMethods()})
		}
		return nil
	}
}

func supportedMethods() []string {
	return []string{"initialize", "notifications/initialized", "tools/list", "tools/call"}
}

func invalidRPCEnvelopeError(err error) map[string]any {
	return map[string]any{
		"error":            err.Error(),
		"expected":         "object",
		"supported_fields": rpcEnvelopeFields(),
	}
}

func rpcEnvelopeFields() []string {
	return []string{"jsonrpc", "id", "method", "params"}
}

func validateRequest(request rpcRequest) (any, bool) {
	if request.JSONRPC != "2.0" {
		return map[string]any{
			"error":    "jsonrpc must be 2.0",
			"field":    "jsonrpc",
			"expected": "2.0",
			"actual":   request.JSONRPC,
		}, false
	}
	if len(request.ID) > 0 && !isValidRequestID(request.ID) {
		return map[string]any{
			"error":    "id must be string, number, or null",
			"field":    "id",
			"expected": []string{"string", "number", "null"},
		}, false
	}
	if len(request.Params) > 0 {
		trimmed := bytes.TrimSpace(request.Params)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && trimmed[0] != '{' {
			return map[string]any{
				"error":    "params must be an object or null",
				"field":    "params",
				"expected": []string{"object", "null"},
			}, false
		}
	}
	return nil, true
}

func isValidRequestID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch trimmed[0] {
	case '"':
		var value string
		return decodeJSONStrict(trimmed, &value) == nil
	case 'n':
		return bytes.Equal(trimmed, []byte("null"))
	default:
		var value float64
		return decodeJSONStrict(trimmed, &value) == nil
	}
}

func (s *server) callTool(params json.RawMessage) (map[string]any, error) {
	var request toolCallRequest
	if err := decodeJSONStrict(params, &request); err != nil {
		return nil, invalidToolCallParamsError(err)
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return nil, core.NewError(core.CodeInvalidArgument, "tool name is required", map[string]any{"supported_tools": toolNames()})
	}
	allowed, ok := allowedToolArguments(request.Name)
	if !ok {
		return nil, unknownToolError(request.Name)
	}
	args, err := parseToolArguments(request.Name, request.Arguments, allowed)
	if err != nil {
		return nil, err
	}

	switch request.Name {
	case "search_skills":
		query, err := args.String("query")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(query) == "" {
			return nil, args.missingRequiredArgument("query", "non_empty_string")
		}
		limit, err := args.PositiveInt("limit", 10)
		if err != nil {
			return nil, err
		}
		return toolJSON(s.service.Search(query, limit)), nil
	case "list_skills":
		installed, err := args.Bool("installed", false)
		if err != nil {
			return nil, err
		}
		available, err := args.Bool("available", false)
		if err != nil {
			return nil, err
		}
		result, err := s.service.List(core.ListOptions{Installed: installed, Available: available})
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "list_capability_packs":
		packs, err := s.service.ListCapabilityPacks()
		if err != nil {
			return nil, err
		}
		return toolJSON(packs), nil
	case "list_capability_scenarios":
		packID, err := args.String("pack_id")
		if err != nil {
			return nil, err
		}
		scenarios, err := s.service.ListCapabilityScenarios()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(packID) != "" {
			pack, err := s.service.GetCapabilityPack(packID)
			if err != nil {
				return nil, err
			}
			scenarios = capabilityScenariosForPack(scenarios, pack.Pack)
		}
		return toolJSON(scenarios), nil
	case "get_capability_scenario":
		scenario, err := args.String("scenario")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(scenario) == "" {
			return nil, args.missingRequiredArgument("scenario", "non_empty_string")
		}
		view, err := s.service.GetCapabilityScenario(scenario)
		if err != nil {
			return nil, err
		}
		return toolJSON(view), nil
	case "plan_capability_pack_install":
		pack, err := args.String("pack")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(pack) == "" {
			return nil, args.missingRequiredArgument("pack", "non_empty_string")
		}
		plan, err := s.service.PlanCapabilityPackInstall(pack)
		if err != nil {
			return nil, err
		}
		return toolJSON(plan), nil
	case "install_capability_pack":
		pack, err := args.String("pack")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(pack) == "" {
			return nil, args.missingRequiredArgument("pack", "non_empty_string")
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("install_capability_pack requires yes=true")
		}
		result, err := s.service.InstallCapabilityPack(context.Background(), pack)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "plan_capability_scenario_install":
		scenario, err := args.String("scenario")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(scenario) == "" {
			return nil, args.missingRequiredArgument("scenario", "non_empty_string")
		}
		plan, err := s.service.PlanCapabilityScenarioInstall(scenario)
		if err != nil {
			return nil, err
		}
		return toolJSON(plan), nil
	case "install_capability_scenario":
		scenario, err := args.String("scenario")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(scenario) == "" {
			return nil, args.missingRequiredArgument("scenario", "non_empty_string")
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("install_capability_scenario requires yes=true")
		}
		result, err := s.service.InstallCapabilityScenario(context.Background(), scenario)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "get_capability_scenario_ledger":
		scenario, err := args.String("scenario")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(scenario) == "" {
			return nil, args.missingRequiredArgument("scenario", "non_empty_string")
		}
		options, err := recordQueryOptions(args, 100)
		if err != nil {
			return nil, err
		}
		ledger, err := s.service.CapabilityScenarioLedger(scenario, options)
		if err != nil {
			return nil, err
		}
		return toolJSON(ledger), nil
	case "list_install_records":
		options, err := recordQueryOptions(args, 100)
		if err != nil {
			return nil, err
		}
		records, err := s.service.ListInstallRecordsWithIntegrity(options)
		if err != nil {
			return nil, err
		}
		return toolJSON(records), nil
	case "list_billing_records":
		options, err := recordQueryOptions(args, 100)
		if err != nil {
			return nil, err
		}
		records, err := s.service.ListBillingRecords(options)
		if err != nil {
			return nil, err
		}
		return toolJSON(records), nil
	case "list_commerce_receipts":
		options, err := recordQueryOptions(args, 100)
		if err != nil {
			return nil, err
		}
		records, err := s.service.ListCommerceReceipts(options)
		if err != nil {
			return nil, err
		}
		return toolJSON(records), nil
	case "get_commerce_snapshot":
		options, err := recordQueryOptions(args, 200)
		if err != nil {
			return nil, err
		}
		snapshot, err := s.service.CommerceSnapshot(options)
		if err != nil {
			return nil, err
		}
		return toolJSON(snapshot), nil
	case "get_commerce_integrity":
		result, err := s.service.CommerceIntegrity()
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "get_commerce_proof":
		challenge, err := args.String("challenge")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(challenge) == "" {
			return nil, args.missingRequiredArgument("challenge", "non_empty_string")
		}
		result, err := s.service.CommerceProof(challenge)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "submit_commerce_proof":
		challenge, err := args.String("challenge")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(challenge) == "" {
			return nil, args.missingRequiredArgument("challenge", "non_empty_string")
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("submit_commerce_proof requires yes=true")
		}
		result, err := s.service.SubmitCommerceProof(context.Background(), challenge)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "get_status":
		status, err := s.service.Status()
		if err != nil {
			return nil, err
		}
		return toolJSON(status), nil
	case "list_config_keys":
		return toolJSON(core.ConfigKeys()), nil
	case "list_registry_sources":
		return toolJSON(s.service.RegistrySources), nil
	case "get_pro_status":
		status, err := s.service.ProStatus(context.Background())
		if err != nil {
			return nil, err
		}
		return toolJSON(status), nil
	case "get_pro_setup":
		result, err := s.service.ProSetup(context.Background())
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "start_pro_login":
		result, err := s.service.ProLoginStart(context.Background())
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "complete_pro_login":
		callbackURI, err := args.String("callback_uri")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(callbackURI) == "" {
			return nil, args.missingRequiredArgument("callback_uri", "non_empty_string")
		}
		result, err := s.service.ProCallback(context.Background(), callbackURI)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "list_pro_devices":
		devices, err := s.service.ProDevices(context.Background())
		if err != nil {
			return nil, err
		}
		return toolJSON(devices), nil
	case "revoke_pro_device":
		device, err := args.String("device_id")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(device) == "" {
			return nil, args.missingRequiredArgument("device_id", "non_empty_string")
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("revoke_pro_device requires yes=true")
		}
		result, err := s.service.ProRevokeDevice(context.Background(), device)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "logout_pro":
		result, err := s.service.ProLogout()
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "register_pro_scheme":
		result, err := s.service.ProRegisterScheme()
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "doctor":
		return toolJSON(s.service.Doctor()), nil
	case "list_agent_targets":
		return toolJSON(agent.Targets()), nil
	case "get_agent_target":
		target, err := args.String("target")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(target) == "" {
			return nil, args.missingRequiredArgument("target", "non_empty_string")
		}
		info, ok := agent.LookupTarget(target)
		if !ok {
			return nil, args.unsupportedValue("unsupported agent target", "target", target, "supported_targets", agent.SupportedTargets())
		}
		return toolJSON(info), nil
	case "verify_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skill) == "" {
			return nil, args.missingRequiredArgument("skill", "non_empty_string")
		}
		result, err := s.service.VerifySkill(skill)
		if err != nil {
			return toolError(err, result), nil
		}
		return toolJSON(result), nil
	case "refresh_registry":
		result, err := s.service.RefreshRegistry(context.Background())
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "validate_registry":
		path, err := args.String("path")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(path) == "" {
			return nil, args.missingRequiredArgument("path", "non_empty_string")
		}
		result, err := s.service.ValidateRegistry(path)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "plan_install":
		skills, err := args.StringSlice("skills")
		if err != nil {
			return nil, err
		}
		if len(skills) == 0 {
			skill, err := args.String("skill")
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(skill) != "" {
				skills = []string{skill}
			}
		}
		if len(skills) == 0 {
			return nil, args.missingRequiredArguments("at least one skill name is required", []string{"skill", "skills"}, "non_empty_string_or_array_of_strings")
		}
		plan, err := s.service.PlanInstall(skills)
		if err != nil {
			return nil, err
		}
		return toolJSON(plan), nil
	case "install_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skill) == "" {
			return nil, args.missingRequiredArgument("skill", "non_empty_string")
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("install_skill requires yes=true")
		}
		result, err := s.service.InstallSkills(context.Background(), []string{skill})
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "upgrade_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		planOnly, err := args.Bool("plan", false)
		if err != nil {
			return nil, err
		}
		if planOnly {
			var names []string
			if strings.TrimSpace(skill) != "" {
				names = []string{skill}
			}
			plan, err := s.service.PlanUpgrade(names)
			if err != nil {
				return nil, err
			}
			return toolJSON(plan), nil
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("upgrade_skill requires yes=true")
		}
		var names []string
		if skill != "" {
			names = []string{skill}
		}
		result, err := s.service.UpgradeSkills(context.Background(), names)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "rollback_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skill) == "" {
			return nil, args.missingRequiredArgument("skill", "non_empty_string")
		}
		to, err := args.String("to")
		if err != nil {
			return nil, err
		}
		planOnly, err := args.Bool("plan", false)
		if err != nil {
			return nil, err
		}
		if planOnly {
			plan, err := s.service.PlanRollback(skill, to)
			if err != nil {
				return nil, err
			}
			return toolJSON(plan), nil
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("rollback_skill requires yes=true")
		}
		result, err := s.service.RollbackSkill(skill, to)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "uninstall_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skill) == "" {
			return nil, args.missingRequiredArgument("skill", "non_empty_string")
		}
		allVersions, err := args.Bool("all_versions", false)
		if err != nil {
			return nil, err
		}
		planOnly, err := args.Bool("plan", false)
		if err != nil {
			return nil, err
		}
		if planOnly {
			plan, err := s.service.PlanUninstall(skill, allVersions)
			if err != nil {
				return nil, err
			}
			return toolJSON(plan), nil
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, args.confirmationRequired("uninstall_skill requires yes=true")
		}
		result, err := s.service.UninstallSkill(skill, allVersions)
		if err != nil {
			return nil, err
		}
		return toolJSON(result), nil
	case "run_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skill) == "" {
			return nil, args.missingRequiredArgument("skill", "non_empty_string")
		}
		skillArgs, err := args.StringSlice("args")
		if err != nil {
			return nil, err
		}
		input, err := args.String("input")
		if err != nil {
			return nil, err
		}
		inputBase64, err := args.String("input_base64")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(input) != "" && strings.TrimSpace(inputBase64) != "" {
			return nil, args.mutuallyExclusiveArguments("input and input_base64 cannot both be set", "input", "input_base64")
		}
		inputBytes := []byte(input)
		if strings.TrimSpace(inputBase64) != "" {
			inputBytes, err = base64.StdEncoding.DecodeString(inputBase64)
			if err != nil {
				return nil, args.invalidBase64Argument("input_base64", err)
			}
		}
		inputPath, err := args.String("input_path")
		if err != nil {
			return nil, err
		}
		ocrArgs, err := args.OCRArgs(skill)
		if err != nil {
			return nil, err
		}
		skillArgs = append(skillArgs, ocrArgs...)
		if strings.TrimSpace(inputPath) != "" {
			skillArgs = append(skillArgs, inputPath)
		}
		timeoutMS, err := args.PositiveInt64("timeout_ms", int64(s.service.Config.RunTimeoutMS))
		if err != nil {
			return nil, err
		}
		outputLimitBytes, err := args.PositiveInt64("output_limit_bytes", s.service.Config.RunOutputLimitBytes)
		if err != nil {
			return nil, err
		}
		scenarioID, err := args.String("scenario_id")
		if err != nil {
			return nil, err
		}
		agentName, err := args.String("agent_name")
		if err != nil {
			return nil, err
		}
		result, err := s.service.RunSkillWithOptions(context.Background(), skill, core.RunOptions{
			Args:             skillArgs,
			Input:            inputBytes,
			Timeout:          time.Duration(timeoutMS) * time.Millisecond,
			OutputLimitBytes: outputLimitBytes,
			ScenarioID:       scenarioID,
			AgentName:        agentName,
		})
		if err != nil {
			return toolError(err, result), nil
		}
		return toolJSON(result), nil
	}
	return nil, unknownToolError(request.Name)
}

func recordQueryOptions(args toolArguments, fallbackLimit int) (core.RecordQueryOptions, error) {
	packID, err := args.String("pack_id")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	scenarioID, err := args.String("scenario_id")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	skill, err := args.String("skill")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	status, err := args.String("status")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	recordType, err := args.String("type")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	currency, err := args.String("currency")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	from, err := args.String("from")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	to, err := args.String("to")
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	limit, err := args.PositiveInt("limit", fallbackLimit)
	if err != nil {
		return core.RecordQueryOptions{}, err
	}
	options := core.RecordQueryOptions{PackID: packID, ScenarioID: scenarioID, Skill: skill, Status: status, Type: recordType, Currency: currency, From: from, To: to, Limit: limit}
	if err := core.ValidateRecordQueryOptions(options); err != nil {
		return core.RecordQueryOptions{}, err
	}
	return options, nil
}

func (s *server) writeResult(id json.RawMessage, result any) error {
	response := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	err = s.writeMessage(data)
	return err
}

func (s *server) writeError(id json.RawMessage, code int, message string, data any) error {
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error": map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		},
	}
	if id != nil {
		response["id"] = json.RawMessage(id)
	}
	bytes, err := json.Marshal(response)
	if err != nil {
		return err
	}
	err = s.writeMessage(bytes)
	return err
}

func (s *server) writeMessage(data []byte) error {
	if s.framed {
		_, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n%s", len(data), data)
		return err
	}
	_, err := fmt.Fprintln(s.out, string(data))
	return err
}

func toolJSON(value any) map[string]any {
	bytes, _ := json.MarshalIndent(value, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(bytes)}},
		"structuredContent": value,
		"isError":           false,
	}
}

func toolError(err error, data any) map[string]any {
	response := core.NewErrorResponseWithData(err, data, nil)
	bytes, _ := json.MarshalIndent(response, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(bytes)}},
		"structuredContent": response,
		"isError":           true,
	}
}

func tools() []map[string]any {
	return []map[string]any{
		tool("search_skills", "Search agtx skills by natural-language query.", objectSchema(
			map[string]any{
				"query": nonEmptyStringSchema("Natural-language query used to rank skills."),
				"limit": positiveIntegerSchema("Maximum number of matches to return."),
			},
			[]string{"query"},
			nil,
		), arraySchema(searchResultSchema(), "Ranked skill matches.")),
		tool("list_skills", "List installed and/or available agtx skills.", objectSchema(
			map[string]any{
				"installed": booleanSchema("Include locally installed skills."),
				"available": booleanSchema("Include registry skills available for installation."),
			},
			nil,
			nil,
		), listResultSchema()),
		tool("list_capability_packs", "List first-wave website capability packs plus standard and advanced bundles with install state for website and marketplace views.", objectSchema(nil, nil, nil), arraySchema(capabilityPackViewSchema(), "Capability packs visible to local website integrations.")),
		tool("list_capability_scenarios", "List real task scenarios mapped to standard or advanced capability packs with readiness, missing skills, install plans, and billing preview.", objectSchema(
			map[string]any{
				"pack_id": stringSchema("Optional capability pack id or alias filter such as standard, advanced, putong, or gaoji."),
			},
			nil,
			nil,
		), arraySchema(capabilityScenarioViewSchema(), "Task scenarios with recommended capability-pack state.")),
		tool("get_capability_scenario", "Return one real task scenario by id or alias with recommended pack, missing skills, install plan, and billing preview.", objectSchema(
			map[string]any{
				"scenario": nonEmptyStringSchema("Scenario id or alias such as invoice_processing, contract, meeting_deck, or marketing."),
			},
			[]string{"scenario"},
			nil,
		), capabilityScenarioViewSchema()),
		tool("plan_capability_pack_install", "Preview capability-pack skill changes and billing records without mutating local state.", objectSchema(
			map[string]any{
				"pack": nonEmptyStringSchema("Capability pack id or tier such as standard, advanced, putong, or gaoji."),
			},
			[]string{"pack"},
			nil,
		), capabilityPackInstallPlanSchema()),
		tool("install_capability_pack", "Install a capability pack and record local billing/install history. Requires yes=true.", objectSchema(
			map[string]any{
				"pack": nonEmptyStringSchema("Capability pack id or tier such as standard, advanced, putong, or gaoji."),
				"yes":  booleanSchema("Must be true to perform the pack install; omit or false to receive confirmation_required."),
			},
			[]string{"pack"},
			nil,
		), capabilityPackInstallResultSchema()),
		tool("plan_capability_scenario_install", "Preview the recommended pack install and billing records for a real task scenario without mutating local state.", objectSchema(
			map[string]any{
				"scenario": nonEmptyStringSchema("Scenario id or alias such as invoice_processing, contract, meeting_deck, or marketing."),
			},
			[]string{"scenario"},
			nil,
		), capabilityScenarioInstallPlanSchema()),
		tool("install_capability_scenario", "Install the recommended capability pack for a real task scenario and tag local install/billing history with scenario_id. Requires yes=true.", objectSchema(
			map[string]any{
				"scenario": nonEmptyStringSchema("Scenario id or alias such as invoice_processing, contract, meeting_deck, or marketing."),
				"yes":      booleanSchema("Must be true to perform the scenario install; omit or false to receive confirmation_required."),
			},
			[]string{"scenario"},
			nil,
		), capabilityScenarioInstallResultSchema()),
		tool("get_capability_scenario_ledger", "Return one real task scenario with matching install records, billing records, totals, latest install, and usage/install split for website account views.", scenarioLedgerInputSchema(), capabilityScenarioLedgerSchema()),
		tool("list_install_records", "List local capability pack and skill install records with ledger integrity for website account/history views.", recordQueryInputSchema(), installRecordListResultSchema()),
		tool("list_billing_records", "List local billing records and totals produced by pack installs or skill usage.", recordQueryInputSchema(), billingRecordListResultSchema()),
		tool("list_commerce_receipts", "List local Pro server receipts for submitted commerce proofs with local ledger integrity.", recordQueryInputSchema(), commerceReceiptListResultSchema()),
		tool("get_commerce_snapshot", "Return capability packs, install records, billing records, and commerce receipts in one website-friendly snapshot.", recordQueryInputSchema(), commerceSnapshotSchema()),
		tool("get_commerce_integrity", "Return local commerce ledger integrity, anchor, and private-file checks for website account/security views.", objectSchema(nil, nil, nil), commerceIntegritySchema()),
		tool("get_commerce_proof", "Return a challenge-bound Ed25519 proof over local commerce ledger integrity for website account/security verification.", objectSchema(
			map[string]any{
				"challenge": nonEmptyStringSchema("Website-provided nonce or challenge that must be echoed and signed in the proof."),
			},
			[]string{"challenge"},
			nil,
		), commerceProofSchema()),
		tool("submit_commerce_proof", "Submit a challenge-bound commerce proof to Pro for a server receipt and store the signed receipt locally. Requires yes=true.", objectSchema(
			map[string]any{
				"challenge": nonEmptyStringSchema("Website-provided nonce or challenge that must be echoed and signed in the proof."),
				"yes":       booleanSchema("Must be true to submit the proof to Pro and write the local receipt record."),
			},
			[]string{"challenge"},
			nil,
		), commerceReceiptSubmitResultSchema()),
		tool("list_agent_targets", "List supported agent integration targets and their setup metadata.", objectSchema(nil, nil, nil), arraySchema(agentTargetSchema(), "Supported agent integration targets.")),
		tool("get_agent_target", "Return setup metadata and snippets for one supported agent target.", objectSchema(
			map[string]any{
				"target": nonEmptyStringSchema("Supported agent target name or alias."),
			},
			[]string{"target"},
			nil,
		), agentTargetSchema()),
		tool("get_status", "Return local agtx status and paths.", objectSchema(nil, nil, nil), statusSchema()),
		tool("list_config_keys", "List supported agtx config keys, value types, defaults, and allowed values.", objectSchema(nil, nil, nil), arraySchema(configKeyInfoSchema(), "Supported agtx config keys.")),
		tool("list_registry_sources", "List registry sources consulted for the active registry view.", objectSchema(nil, nil, nil), arraySchema(registrySourceSchema(), "Registry sources consulted for the current view.")),
		tool("get_pro_status", "Return local Pro authentication and subscription status.", objectSchema(nil, nil, nil), proStatusSchema()),
		tool("get_pro_setup", "Return a no-side-effect Pro setup checklist and next actions for humans or agents.", objectSchema(nil, nil, nil), proSetupSchema()),
		tool("start_pro_login", "Create a Pro login URL and pending PKCE state without opening a browser.", objectSchema(nil, nil, nil), proLoginStartSchema()),
		tool("complete_pro_login", "Complete Pro login from an agtx:// callback URI.", objectSchema(
			map[string]any{
				"callback_uri": nonEmptyStringSchema("agtx://pro/callback URI returned by the login flow."),
			},
			[]string{"callback_uri"},
			nil,
		), proCallbackSchema()),
		tool("list_pro_devices", "List active and revoked Pro devices for the authenticated subscription.", objectSchema(nil, nil, nil), arraySchema(proDeviceSchema(), "Pro devices associated with this subscription.")),
		tool("revoke_pro_device", "Revoke one Pro device. Requires yes=true.", objectSchema(
			map[string]any{
				"device_id": nonEmptyStringSchema("Pro device identifier to revoke."),
				"yes":       booleanSchema("Must be true to revoke the device."),
			},
			[]string{"device_id"},
			nil,
		), proDeviceSchema()),
		tool("logout_pro", "Remove the local Pro auth state.", objectSchema(nil, nil, nil), proLogoutSchema()),
		tool("register_pro_scheme", "Register the agtx:// callback scheme with the local OS when supported.", objectSchema(nil, nil, nil), proSchemeSchema()),
		tool("doctor", "Run local agtx diagnostics without mutating state.", objectSchema(nil, nil, nil), doctorResultSchema()),
		tool("verify_skill", "Verify an installed skill manifest, current pointer, platform, and entrypoint.", objectSchema(
			map[string]any{
				"skill": nonEmptyStringSchema("Installed skill name to verify."),
			},
			[]string{"skill"},
			nil,
		), verifyResultSchema()),
		tool("refresh_registry", "Refresh the cached registry from configured registry_url.", objectSchema(nil, nil, nil), registryRefreshResultSchema()),
		tool("validate_registry", "Validate a local registry manifest file without loading or installing it.", objectSchema(
			map[string]any{
				"path": nonEmptyStringSchema("Local registry manifest path to validate."),
			},
			[]string{"path"},
			nil,
		), registryValidationSchema()),
		tool("plan_install", "Return the install plan for one or more skills without mutating local state.", objectSchema(
			map[string]any{
				"skill":  nonEmptyStringSchema("Single skill name to plan."),
				"skills": stringArraySchema("One or more skill names to plan.", true),
			},
			nil,
			map[string]any{
				"anyOf": []map[string]any{
					{"required": []string{"skill"}},
					{"required": []string{"skills"}},
				},
			},
		), mutationPlanSchema()),
		tool("install_skill", "Install one skill. Requires yes=true.", objectSchema(
			map[string]any{
				"skill": nonEmptyStringSchema("Registry skill name to install."),
				"yes":   booleanSchema("Must be true to perform the install; omit or false to receive confirmation_required."),
			},
			[]string{"skill"},
			nil,
		), arraySchema(installResultSchema(), "Install results for requested skills.")),
		tool("upgrade_skill", "Upgrade one skill, or all installed skills if skill is empty. Requires yes=true, or plan=true for dry run.", objectSchema(
			map[string]any{
				"skill": nonEmptyStringSchema("Installed skill name to upgrade. Omit to target all installed skills."),
				"yes":   booleanSchema("Must be true to perform the upgrade when plan is not true."),
				"plan":  booleanSchema("When true, return the upgrade plan without mutating local state."),
			},
			nil,
			nil,
		), anyOfSchema("Upgrade plan or results.", mutationPlanSchema(), arraySchema(installResultSchema(), "Upgrade results."))),
		tool("rollback_skill", "Rollback one skill. Requires yes=true, or plan=true for dry run.", objectSchema(
			map[string]any{
				"skill": nonEmptyStringSchema("Installed skill name to roll back."),
				"to":    nonEmptyStringSchema("Specific installed version to switch to. Omit to use the previous installed version."),
				"yes":   booleanSchema("Must be true to perform the rollback when plan is not true."),
				"plan":  booleanSchema("When true, return the rollback plan without mutating local state."),
			},
			[]string{"skill"},
			nil,
		), anyOfSchema("Rollback plan or result.", mutationPlanSchema(), rollbackResultSchema())),
		tool("uninstall_skill", "Uninstall one skill. Requires yes=true, or plan=true for dry run.", objectSchema(
			map[string]any{
				"skill":        nonEmptyStringSchema("Installed skill name to uninstall."),
				"all_versions": booleanSchema("When true, remove all installed versions instead of only the current one."),
				"yes":          booleanSchema("Must be true to perform the uninstall when plan is not true."),
				"plan":         booleanSchema("When true, return the uninstall plan without mutating local state."),
			},
			[]string{"skill"},
			nil,
		), anyOfSchema("Uninstall plan or result.", mutationPlanSchema(), uninstallResultSchema())),
		tool("run_skill", "Run an installed skill.", objectSchema(
			map[string]any{
				"skill":              nonEmptyStringSchema("Installed skill name to execute."),
				"args":               stringArraySchema("Positional and flag arguments passed directly to the skill entrypoint.", false),
				"input":              stringSchema("UTF-8 input payload forwarded to the skill stdin."),
				"input_base64":       stringSchema("Base64-encoded binary input forwarded to the skill stdin, useful for image bytes."),
				"input_path":         stringSchema("Local input file path appended to the skill arguments, useful for built-in document and OCR skills."),
				"timeout_ms":         positiveIntegerSchema("Execution timeout in milliseconds."),
				"output_limit_bytes": positiveIntegerSchema("Maximum captured stdout and stderr bytes."),
				"scenario_id":        stringSchema("Optional real task scenario id used to tag usage billing records."),
				"agent_name":         stringSchema("Optional per-run agent display name used for generated artifact attribution. Overrides config agent_name for this invocation."),
				"ocr":                ocrRunOptionsSchema(),
			},
			[]string{"skill"},
			nil,
		), runResultSchema()),
	}
}

func invalidToolCallParamsError(err error) *core.Error {
	return core.NewError(core.CodeInvalidArgument, "invalid tools/call params", map[string]any{
		"expected":         "object",
		"error":            err.Error(),
		"supported_params": []string{"name", "arguments"},
	})
}

func toolNames() []string {
	items := tools()
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item["name"].(string)
		if ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

func unknownToolError(name string) *core.Error {
	return core.NewError(core.CodeNotFound, "unknown tool", map[string]any{
		"tool":            name,
		"supported_tools": toolNames(),
	})
}

func tool(name, description string, inputSchema, outputSchema map[string]any) map[string]any {
	value := map[string]any{
		"name":              name,
		"description":       description,
		"inputSchema":       inputSchema,
		"errorOutputSchema": errorResponseSchema(outputSchema),
	}
	if outputSchema != nil {
		value["outputSchema"] = outputSchema
	}
	return value
}

func capabilityScenariosForPack(scenarios []core.CapabilityScenarioView, pack core.CapabilityPack) []core.CapabilityScenarioView {
	if strings.TrimSpace(pack.ID) == "" {
		return scenarios
	}
	filtered := scenarios[:0]
	for _, scenario := range scenarios {
		if capabilityScenarioMatchesPack(scenario, pack) {
			filtered = append(filtered, scenario)
		}
	}
	return filtered
}

func capabilityScenarioMatchesPack(scenario core.CapabilityScenarioView, pack core.CapabilityPack) bool {
	if strings.EqualFold(strings.TrimSpace(scenario.RecommendedPack.Pack.ID), strings.TrimSpace(pack.ID)) || strings.EqualFold(strings.TrimSpace(scenario.Scenario.RecommendedPackID), strings.TrimSpace(pack.ID)) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(pack.Tier)) {
	case "standard", "advanced":
		return false
	}
	for _, packSkill := range pack.SkillNames {
		for _, recommendedSkill := range scenario.RecommendedPack.Pack.SkillNames {
			if strings.EqualFold(strings.TrimSpace(packSkill), strings.TrimSpace(recommendedSkill)) {
				return true
			}
		}
		for _, scenarioSkill := range scenario.Scenario.Skills {
			if strings.EqualFold(strings.TrimSpace(packSkill), strings.TrimSpace(scenarioSkill.Name)) {
				return true
			}
		}
	}
	return false
}

func objectSchema(properties map[string]any, required []string, extras map[string]any) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = slices.Clone(required)
	}
	for key, value := range extras {
		schema[key] = value
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{"type": "string"}, description)
}

func stringEnumSchema(description string, values ...string) map[string]any {
	schema := stringSchema(description)
	enum := make([]string, 0, len(values))
	enum = append(enum, values...)
	schema["enum"] = enum
	return schema
}

func nonEmptyStringSchema(description string) map[string]any {
	schema := stringSchema(description)
	schema["minLength"] = 1
	return schema
}

func positiveIntegerSchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{"type": "integer", "minimum": 1}, description)
}

func numberSchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{"type": "number"}, description)
}

func booleanSchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{"type": "boolean"}, description)
}

func stringArraySchema(description string, minItems bool) map[string]any {
	schema := schemaWithDescription(map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}, description)
	if minItems {
		schema["minItems"] = 1
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := schemaWithDescription(map[string]any{
		"type":  "array",
		"items": items,
	}, description)
	return schema
}

func anyOfSchema(description string, options ...map[string]any) map[string]any {
	variants := make([]map[string]any, 0, len(options))
	for _, option := range options {
		if option != nil {
			variants = append(variants, option)
		}
	}
	return schemaWithDescription(map[string]any{"anyOf": variants}, description)
}

func anySchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{}, description)
}

func nonNegativeIntegerSchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{"type": "integer", "minimum": 0}, description)
}

func ocrRunOptionsSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"probe":              booleanSchema("Return native OCR backend status without running inference."),
			"download_runtime":   booleanSchema("Download and extract the native ONNX Runtime CPU shared library for this platform."),
			"download_models":    booleanSchema("Download RapidOCR ONNX detector, recognizer, and recognition keys."),
			"dry_run":            booleanSchema("Plan OCR runtime or model downloads without writing files."),
			"keep_archive":       booleanSchema("Keep the downloaded ONNX Runtime archive after extracting the shared library."),
			"backend":            stringEnumSchema("Native OCR backend.", "auto", "onnxruntime", "ncnn"),
			"model_profile":      stringEnumSchema("OCR model family.", "auto", "rapidocr", "ppocrv6", "ppocrv5", "ppocrv4"),
			"model_size":         stringEnumSchema("OCR ONNX asset size used by the model downloader.", "auto", "mobile", "tiny", "small", "medium"),
			"model_dir":          stringSchema("Local OCR model directory."),
			"runtime_dir":        stringSchema("Local native inference runtime directory."),
			"runtime_version":    stringSchema("ONNX Runtime version used by the runtime downloader."),
			"det_model":          stringSchema("Detector model path, absolute or relative to model_dir."),
			"rec_model":          stringSchema("Recognizer model path, absolute or relative to model_dir."),
			"keys":               stringSchema("Recognizer keys dictionary path, absolute or relative to model_dir."),
			"det_input":          stringSchema("Optional detector ONNX input name override."),
			"det_output":         stringSchema("Optional detector ONNX output name override."),
			"rec_input":          stringSchema("Optional recognizer ONNX input name override."),
			"rec_output":         stringSchema("Optional recognizer ONNX output name override."),
			"det_limit_side_len": positiveIntegerSchema("Detector resize side limit."),
			"det_threshold":      numberSchema("Detector binary map threshold."),
			"box_threshold":      numberSchema("Minimum average score for a detected text box."),
			"unclip_ratio":       numberSchema("Expansion ratio for detector boxes before recognition crops."),
			"max_candidates":     positiveIntegerSchema("Maximum detector boxes to send to recognition."),
			"text_score":         numberSchema("Minimum recognizer confidence for returned text lines."),
			"rec_width":          positiveIntegerSchema("Fixed recognizer crop width."),
			"rec_height":         positiveIntegerSchema("Recognizer crop height override."),
			"rec_max_width":      positiveIntegerSchema("Maximum crop width for dynamic-width recognizer models."),
		},
		nil,
		nil,
	)
}

func agentTargetSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"target":            nonEmptyStringSchema("Canonical agent target identifier."),
			"display_name":      stringSchema("Human-friendly agent name."),
			"aliases":           stringArraySchema("Accepted aliases for this target.", false),
			"summary":           stringSchema("Short description of the integration style."),
			"config_hint":       stringSchema("Short heading for the config snippet."),
			"config_format":     stringSchema("Config snippet format such as toml or json."),
			"config_path_hints": stringArraySchema("Likely config file locations or scopes.", false),
			"config_snippet":    stringSchema("Paste-ready config snippet."),
			"command_hint":      stringSchema("Short heading for a command-based step."),
			"command_snippet":   stringSchema("Shell command to run for setup."),
			"rule_hint":         stringSchema("Short heading for an instruction/rule step."),
			"rule_path_hints":   stringArraySchema("Likely rule file locations or scopes.", false),
			"rule_snippet":      stringSchema("Suggested instruction text."),
			"setup_steps":       arraySchema(agentStepSchema(), "Ordered setup steps for guided integration."),
		},
		[]string{"target"},
		nil,
	)
}

func agentStepSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":           nonEmptyStringSchema("Stable step identifier."),
			"kind":         stringSchema("Step category such as config, command, or rule."),
			"title":        stringSchema("Display title for this step."),
			"summary":      stringSchema("Human summary of the step."),
			"format":       stringSchema("Snippet format such as json, toml, shell, or text."),
			"path_hints":   stringArraySchema("Likely files or scopes touched by this step.", false),
			"platforms":    stringArraySchema("Platforms where this step applies.", false),
			"applies_when": arraySchema(agentConditionSchema(), "Conditions that determine when this step applies."),
			"writes_files": arraySchema(agentArtifactSchema(), "Files this step is expected to write or modify."),
			"artifacts":    arraySchema(agentArtifactSchema(), "Artifacts produced by this step."),
			"snippet":      stringSchema("Paste-ready snippet or command content."),
			"optional":     booleanSchema("Whether this step is recommended rather than required."),
			"priority":     positiveIntegerSchema("Suggested execution/display order."),
			"blocking":     booleanSchema("Whether later steps depend on this step."),
			"verification": agentVerificationSchema(),
		},
		[]string{"id"},
		nil,
	)
}

func agentConditionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"field":  nonEmptyStringSchema("Condition field name."),
			"any_of": stringArraySchema("Accepted values for the condition.", false),
			"note":   stringSchema("Human explanation of the branch."),
		},
		[]string{"field"},
		nil,
	)
}

func agentArtifactSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"kind":          stringSchema("Artifact category."),
			"paths":         stringArraySchema("Expected file paths or scopes.", false),
			"summary":       stringSchema("Human summary of the artifact."),
			"consumable_by": stringArraySchema("Actors or systems expected to consume the artifact.", false),
		},
		nil,
		nil,
	)
}

func agentVerificationSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"kind":        stringSchema("Verification type such as manual or command."),
			"command":     stringSchema("Command to run for verification when applicable."),
			"expectation": stringSchema("Expected result after the step completes."),
		},
		nil,
		nil,
	)
}

func diagnosticSummarySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"checks":   positiveIntegerSchema("Total number of diagnostic checks."),
			"passed":   positiveIntegerSchema("Number of passing checks."),
			"warnings": schemaWithDescription(map[string]any{"type": "integer", "minimum": 0}, "Number of warning checks."),
			"errors":   schemaWithDescription(map[string]any{"type": "integer", "minimum": 0}, "Number of failing checks."),
		},
		nil,
		nil,
	)
}

func doctorCheckSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":     nonEmptyStringSchema("Stable check identifier."),
			"ok":       booleanSchema("Whether the check passed."),
			"severity": stringSchema("Severity such as info, warning, or error."),
			"message":  stringSchema("Human-readable diagnostic message."),
			"path":     stringSchema("Relevant filesystem path when applicable."),
			"details":  schemaWithDescription(map[string]any{}, "Optional structured diagnostic details."),
		},
		[]string{"name"},
		nil,
	)
}

func doctorResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"ok":      booleanSchema("Whether all required checks passed."),
			"summary": diagnosticSummarySchema(),
			"checks":  arraySchema(doctorCheckSchema(), "Diagnostic checks emitted by doctor."),
		},
		nil,
		nil,
	)
}

func verifyResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"ok":                 booleanSchema("Whether verification succeeded."),
			"name":               nonEmptyStringSchema("Verified skill name."),
			"version":            stringSchema("Resolved skill version."),
			"path":               stringSchema("Filesystem path for the active version."),
			"stub":               booleanSchema("Whether the skill is currently a stub install."),
			"installed_versions": stringArraySchema("Installed versions found on disk.", false),
			"summary":            diagnosticSummarySchema(),
			"checks":             arraySchema(doctorCheckSchema(), "Verification checks and findings."),
		},
		[]string{"name"},
		nil,
	)
}

func runResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":               nonEmptyStringSchema("Executed skill name."),
			"version":            stringSchema("Resolved installed skill version."),
			"stub":               booleanSchema("Whether the installed skill is a stub."),
			"scenario_id":        stringSchema("Canonical real task scenario id used to tag usage billing records."),
			"invocation_id":      stringSchema("Stable invocation id shared with usage events."),
			"exit_code":          schemaWithDescription(map[string]any{"type": "integer"}, "Process exit code returned by the skill."),
			"stdout":             stringSchema("Captured standard output."),
			"stderr":             stringSchema("Captured standard error."),
			"stdout_truncated":   booleanSchema("Whether stdout was truncated by output limits."),
			"stderr_truncated":   booleanSchema("Whether stderr was truncated by output limits."),
			"duration_ms":        schemaWithDescription(map[string]any{"type": "integer", "minimum": 0}, "Execution duration in milliseconds."),
			"timed_out":          booleanSchema("Whether execution timed out."),
			"output_limit_bytes": positiveIntegerSchema("Configured output capture limit in bytes."),
			"timeout_ms":         positiveIntegerSchema("Configured timeout in milliseconds."),
			"attributed_files":   stringArraySchema("Office document output paths that agtx successfully annotated with generated artifact attribution.", false),
			"usage_events":       arraySchema(usageEventResultSchema(), "Billing usage events produced by this successful invocation."),
		},
		[]string{"name", "exit_code"},
		nil,
	)
}

func usageEventResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"event_id":           nonEmptyStringSchema("Idempotent usage event id."),
			"pack_id":            nonEmptyStringSchema("Capability pack or skill id."),
			"scenario_id":        stringSchema("Canonical real task scenario id used to tag this usage event."),
			"version_id":         stringSchema("Capability pack version id."),
			"vendor_id":          stringSchema("Vendor id declared by the skill manifest."),
			"meter":              nonEmptyStringSchema("Billing meter such as call, task, page, minute, token, credit, seat, storage_gb_day, or success."),
			"quantity":           numberSchema("Meter quantity recorded for this invocation."),
			"currency":           stringSchema("ISO 4217 currency or AGTX_CREDIT."),
			"unit_price_minor":   nonNegativeIntegerSchema("Unit price in minor currency units or credit units."),
			"gross_amount_minor": nonNegativeIntegerSchema("Gross event amount in minor currency units or credit units."),
			"status":             nonEmptyStringSchema("Usage recording status such as local_only, recorded, or report_failed."),
			"error":              stringSchema("Best-effort reporting error when the run itself still succeeded."),
		},
		[]string{"event_id", "pack_id", "meter", "quantity", "status"},
		nil,
	)
}

func statusSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"version":          stringSchema("agtx build version."),
			"goos":             nonEmptyStringSchema("Resolved operating system."),
			"goarch":           nonEmptyStringSchema("Resolved CPU architecture."),
			"config_dir":       stringSchema("Config directory path."),
			"config_file":      stringSchema("Config file path."),
			"cache_dir":        stringSchema("Cache directory path."),
			"skills_dir":       stringSchema("Installed skills directory path."),
			"logs_dir":         stringSchema("Logs directory path."),
			"registry_skills":  nonNegativeIntegerSchema("Number of registry skills currently loaded."),
			"registry_sources": arraySchema(registrySourceSchema(), "Registry sources consulted for the current view."),
			"installed":        nonNegativeIntegerSchema("Number of installed skills."),
			"dependency_mode":  stringSchema("Dependency strategy summary."),
			"channel":          stringSchema("Configured release channel."),
			"telemetry":        stringSchema("Configured telemetry mode."),
		},
		nil,
		nil,
	)
}

func configKeyInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"key":         nonEmptyStringSchema("Config key accepted by agtx config set/unset."),
			"type":        nonEmptyStringSchema("Expected value type such as url, enum, string_list, or positive_integer."),
			"default":     anySchema("Default value after config init or unset."),
			"description": stringSchema("Human-readable summary of the setting."),
			"allowed":     stringArraySchema("Allowed values for enum-like settings.", false),
			"mutable":     booleanSchema("Whether this key can be changed with config set/unset."),
		},
		[]string{"key", "type", "description", "mutable"},
		nil,
	)
}

func registrySourceSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"kind":   nonEmptyStringSchema("Registry source kind."),
			"path":   stringSchema("Filesystem path for a file-backed source."),
			"url":    stringSchema("Configured URL for a remote source."),
			"loaded": booleanSchema("Whether this source was loaded into the active registry view."),
			"error":  stringSchema("Load error when this source could not be read."),
		},
		[]string{"kind", "loaded"},
		nil,
	)
}

func registryRefreshResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"source": stringSchema("Registry URL used for the refresh."),
			"path":   stringSchema("Cached registry file path."),
			"bytes":  nonNegativeIntegerSchema("Downloaded registry bytes written to cache."),
		},
		[]string{"source", "path", "bytes"},
		nil,
	)
}

func registryValidationSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"path":     nonEmptyStringSchema("Validated registry manifest path."),
			"ok":       booleanSchema("Whether the registry passed validation without warnings."),
			"skills":   nonNegativeIntegerSchema("Number of skills declared in the registry."),
			"warnings": stringArraySchema("Validation warnings emitted for non-fatal issues.", false),
		},
		[]string{"path", "ok", "skills"},
		nil,
	)
}

func proStatusSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"authenticated":       booleanSchema("Whether local Pro auth is available."),
			"subscription":        stringSchema("Subscription status reported by the Pro service."),
			"plan":                stringSchema("Plan name reported by the Pro service."),
			"device_id":           stringSchema("Current local device identifier."),
			"device_name":         stringSchema("Current local device name."),
			"expires_at":          stringSchema("Access token expiry timestamp."),
			"device_limit":        nonNegativeIntegerSchema("Maximum active devices allowed by the subscription."),
			"auth_path":           stringSchema("Local auth.json path."),
			"devices":             arraySchema(proDeviceSchema(), "Known devices for this subscription."),
			"recommended_actions": arraySchema(proSetupActionSchema(), "Ordered recommended next actions for incomplete local Pro state."),
			"current_status":      stringArraySchema("Current local Pro status markers such as authenticated, not_authenticated, pending_login, or auth_invalid.", false),
		},
		nil,
		nil,
	)
}

func proSetupSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"authenticated":        booleanSchema("Whether local Pro auth is currently available."),
			"has_pending_login":    booleanSchema("Whether auth.json currently holds a pending PKCE login flow."),
			"callback_scheme":      nonEmptyStringSchema("Expected callback URI scheme."),
			"callback_uri_example": stringSchema("Example callback URI used to complete login."),
			"auth_path":            stringSchema("Local auth.json path."),
			"config_path":          stringSchema("Local config.json path."),
			"pro_api_url":          stringSchema("Configured or derived Pro API URL."),
			"registry_url":         stringSchema("Configured registry URL."),
			"platform":             nonEmptyStringSchema("Current OS/architecture tuple."),
			"can_register_scheme":  booleanSchema("Whether agtx can attempt callback-scheme registration automatically on this platform."),
			"scheme_command_hint":  stringSchema("CLI command hint for callback-scheme registration."),
			"recommended_actions":  arraySchema(proSetupActionSchema(), "Ordered recommended next actions."),
			"current_status":       stringArraySchema("Current status markers for the local Pro setup state.", false),
		},
		[]string{"authenticated", "has_pending_login", "callback_scheme", "auth_path", "config_path", "platform", "can_register_scheme"},
		nil,
	)
}

func proSetupActionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":           nonEmptyStringSchema("Stable action identifier."),
			"title":        nonEmptyStringSchema("Short title for the action."),
			"summary":      stringSchema("Human-readable action summary."),
			"blocking":     booleanSchema("Whether this action should be completed before the next login/install step."),
			"command":      stringSchema("Equivalent CLI command when available."),
			"mcp_tool":     stringSchema("Equivalent MCP tool name when available."),
			"arguments":    anySchema("Suggested MCP arguments when the action can be automated."),
			"applies_when": stringArraySchema("Status markers that make this action relevant.", false),
		},
		[]string{"id", "title", "blocking"},
		nil,
	)
}

func proLoginStartSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"login_url":    nonEmptyStringSchema("URL the user should open to complete Pro login."),
			"state":        stringSchema("Opaque login state stored in auth.json."),
			"device_id":    stringSchema("Local device identifier used for this login attempt."),
			"redirect_uri": stringSchema("Callback URI expected by the CLI."),
			"auth_path":    stringSchema("Local auth.json path containing pending login state."),
		},
		[]string{"login_url"},
		nil,
	)
}

func proCallbackSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"authenticated": booleanSchema("Whether Pro login completed successfully."),
			"device_id":     stringSchema("Resolved current device identifier."),
			"device_name":   stringSchema("Resolved current device name."),
			"expires_at":    stringSchema("Access token expiry timestamp."),
			"registry_url":  stringSchema("Registry URL returned by the Pro service."),
			"pro_api_url":   stringSchema("Pro API URL used for the login flow."),
			"device_limit":  nonNegativeIntegerSchema("Maximum active devices allowed by the subscription."),
			"subscription":  stringSchema("Subscription status returned by the Pro service."),
			"auth_path":     stringSchema("Local auth.json path updated by this callback."),
		},
		nil,
		nil,
	)
}

func proDeviceSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":           nonEmptyStringSchema("Device identifier."),
			"name":         stringSchema("Human-readable device name."),
			"current":      booleanSchema("Whether this is the current local device."),
			"last_seen_at": stringSchema("Last-seen timestamp reported by the Pro service."),
			"activated_at": stringSchema("Activation timestamp reported by the Pro service."),
			"revoked_at":   stringSchema("Revocation timestamp when this device is revoked."),
		},
		[]string{"id"},
		nil,
	)
}

func proLogoutSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"logged_out": booleanSchema("Whether the local Pro auth state was removed."),
			"auth_path":  stringSchema("Local auth.json path that was removed or reset."),
		},
		[]string{"logged_out", "auth_path"},
		nil,
	)
}

func proSchemeSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"scheme":  nonEmptyStringSchema("Registered callback scheme name."),
			"command": stringSchema("Command associated with the callback scheme when available."),
		},
		[]string{"scheme"},
		nil,
	)
}

func permissionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":        nonEmptyStringSchema("Permission name."),
			"description": stringSchema("Permission summary."),
		},
		[]string{"name"},
		nil,
	)
}

func platformBundleSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"os":         nonEmptyStringSchema("Target operating system."),
			"arch":       nonEmptyStringSchema("Target CPU architecture."),
			"url":        stringSchema("Archive source URL or local path."),
			"sha256":     stringSchema("Expected archive SHA-256 digest."),
			"archive":    stringSchema("Archive format such as zip or tar.gz."),
			"entrypoint": stringSchema("Relative executable path inside the archive."),
		},
		[]string{"os", "arch"},
		nil,
	)
}

func signatureInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"algorithm": stringSchema("Reserved signature algorithm name."),
			"key_id":    stringSchema("Reserved signature key identifier."),
			"value":     stringSchema("Reserved signature value."),
		},
		nil,
		nil,
	)
}

func skillManifestSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version": positiveIntegerSchema("Manifest schema version."),
			"name":           nonEmptyStringSchema("Skill name."),
			"version":        nonEmptyStringSchema("Skill version."),
			"vendor_id":      stringSchema("ISV or first-party vendor identifier."),
			"capability":     capabilityInfoSchema(),
			"summary":        stringSchema("Short skill summary."),
			"description":    stringSchema("Longer skill description."),
			"tags":           stringArraySchema("Search and category tags.", false),
			"keywords":       stringArraySchema("Natural-language search keywords.", false),
			"permissions":    arraySchema(permissionSchema(), "Declared permissions."),
			"platforms":      arraySchema(platformBundleSchema(), "Supported platform bundles."),
			"input_schema":   anySchema("Skill-specific input schema."),
			"output_schema":  anySchema("Skill-specific output schema."),
			"billing":        billingInfoSchema(),
			"attribution":    attributionInfoSchema(),
			"support":        supportInfoSchema(),
			"signature":      signatureInfoSchema(),
			"builtin":        builtinInfoSchema(),
			"stub":           booleanSchema("Whether the skill is currently a stub package."),
		},
		[]string{"name", "version"},
		nil,
	)
}

func builtinInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"runtime":        stringSchema("Built-in runtime identifier provided by agtx."),
			"backends":       stringArraySchema("Native inference backends supported by the built-in runtime.", false),
			"model_profiles": stringArraySchema("Model profiles supported by the built-in runtime.", false),
			"no_python":      booleanSchema("Whether this built-in runtime avoids Python and NPM runtimes."),
		},
		nil,
		nil,
	)
}

func capabilityInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"class":    stringSchema("Capability class such as tool, workflow, connector, model_adapter, content, or commerce."),
			"use_when": stringSchema("Agent-readable trigger guidance."),
		},
		nil,
		nil,
	)
}

func billingInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"meters":        arraySchema(billingMeterSchema(), "Supported billing meters."),
			"revenue_share": revenueShareSchema(),
		},
		nil,
		nil,
	)
}

func billingMeterSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"meter":                nonEmptyStringSchema("Billing meter such as call, task, page, minute, token, credit, seat, storage_gb_day, or success."),
			"unit_price":           numberSchema("Unit price in currency or credit units."),
			"currency":             stringSchema("ISO 4217 currency or AGTX_CREDIT."),
			"free_quota":           numberSchema("Included free quota for this meter."),
			"hard_limit_supported": booleanSchema("Whether agents can enforce a hard spending cap."),
			"refund_policy":        stringSchema("Refund or failed-invocation billing policy."),
		},
		[]string{"meter"},
		nil,
	)
}

func revenueShareSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"isv":      numberSchema("ISV revenue share percentage."),
			"platform": numberSchema("Platform revenue share percentage."),
			"basis":    stringSchema("Revenue share basis."),
		},
		nil,
		nil,
	)
}

func attributionInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"events":              stringArraySchema("Supported CPA/CPS attribution events.", false),
			"default_window_days": anySchema("Default attribution windows by type, such as cpa and cps."),
			"default_cps_rate":    numberSchema("Default CPS commission percentage."),
			"renewal_cps":         stringSchema("Renewal CPS policy."),
		},
		nil,
		nil,
	)
}

func supportInfoSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"url":            stringSchema("Vendor support URL."),
			"privacy_url":    stringSchema("Vendor privacy policy URL."),
			"incident_email": stringSchema("Security or incident contact email."),
		},
		nil,
		nil,
	)
}

func searchResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"skill":   skillManifestSchema(),
			"score":   nonNegativeIntegerSchema("Search ranking score."),
			"matched": stringArraySchema("Matched query terms or keywords.", false),
		},
		[]string{"skill", "score"},
		nil,
	)
}

func installedSkillSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":     nonEmptyStringSchema("Installed skill name."),
			"version":  nonEmptyStringSchema("Current installed version."),
			"path":     stringSchema("Filesystem path for the installed version."),
			"current":  booleanSchema("Whether this version is the active current pointer."),
			"manifest": skillManifestSchema(),
		},
		[]string{"name", "version", "current", "manifest"},
		nil,
	)
}

func listResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"installed": arraySchema(installedSkillSchema(), "Installed skills visible on disk."),
			"available": arraySchema(skillManifestSchema(), "Registry skills available for installation."),
		},
		nil,
		nil,
	)
}

func recordQueryInputSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"pack_id":     stringSchema("Optional capability pack id filter such as standard or advanced."),
			"scenario_id": stringSchema("Optional real task scenario id filter such as invoice_processing or meeting_to_presentation."),
			"skill":       stringSchema("Optional skill name filter."),
			"status":      stringSchema("Optional record status filter."),
			"type":        stringSchema("Optional billing record type filter such as pack_install or skill_usage."),
			"currency":    stringSchema("Optional billing currency filter such as USD or AGTX_CREDIT."),
			"from":        stringSchema("Optional inclusive RFC3339 start timestamp."),
			"to":          stringSchema("Optional inclusive RFC3339 end timestamp."),
			"limit":       positiveIntegerSchema("Maximum number of records to return."),
		},
		nil,
		nil,
	)
}

func scenarioLedgerInputSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"scenario":    nonEmptyStringSchema("Scenario id or alias such as invoice_processing, invoice, contract, or meeting_deck."),
			"pack_id":     stringSchema("Optional capability pack id filter such as standard or advanced."),
			"scenario_id": stringSchema("Optional real task scenario id filter; the scenario argument is used as the canonical filter."),
			"skill":       stringSchema("Optional skill name filter."),
			"status":      stringSchema("Optional record status filter."),
			"type":        stringSchema("Optional billing record type filter such as pack_install or skill_usage."),
			"currency":    stringSchema("Optional billing currency filter such as USD or AGTX_CREDIT."),
			"from":        stringSchema("Optional inclusive RFC3339 start timestamp."),
			"to":          stringSchema("Optional inclusive RFC3339 end timestamp."),
			"limit":       positiveIntegerSchema("Maximum number of records to return."),
		},
		[]string{"scenario"},
		nil,
	)
}

func capabilityPackSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version":   positiveIntegerSchema("Capability pack schema version."),
			"id":               nonEmptyStringSchema("Stable capability pack id."),
			"name":             nonEmptyStringSchema("Human-readable capability pack name."),
			"tier":             nonEmptyStringSchema("Pack tier such as first_wave, standard, or advanced."),
			"capability_class": stringSchema("Commerce capability class such as tool, workflow, or content."),
			"use_when":         stringSchema("Agent-readable trigger condition for this pack."),
			"summary":          stringSchema("Short pack summary."),
			"description":      stringSchema("Longer pack description."),
			"inputs":           stringArraySchema("Agent-readable input contract entries.", false),
			"outputs":          stringArraySchema("Agent-readable output contract entries.", false),
			"tags":             stringArraySchema("Capability tags used for discovery and website filtering.", false),
			"skill_names":      stringArraySchema("Skill names included in this pack.", false),
			"billing":          billingInfoSchema(),
			"support":          supportInfoSchema(),
		},
		[]string{"id", "name", "tier", "skill_names"},
		nil,
	)
}

func capabilityPackViewSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"pack":         capabilityPackSchema(),
			"installed":    booleanSchema("Whether all skills in this pack are currently installed."),
			"installed_at": stringSchema("Timestamp of the latest local pack install record."),
			"updated_at":   stringSchema("Timestamp of the latest local pack update/install record."),
			"skills":       arraySchema(capabilityPackSkillSchema(), "Per-skill availability and install state within the pack."),
		},
		[]string{"pack", "installed", "skills"},
		nil,
	)
}

func capabilityPackSkillSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":              nonEmptyStringSchema("Skill name included in the pack."),
			"available_version": stringSchema("Registry version available for installation."),
			"installed_version": stringSchema("Current installed version when present."),
			"installed":         booleanSchema("Whether the skill is currently installed."),
			"stub":              booleanSchema("Whether the installed skill is a stub."),
			"path":              stringSchema("Filesystem path for the installed skill version."),
			"manifest":          skillManifestSchema(),
		},
		[]string{"name", "installed"},
		nil,
	)
}

func capabilityScenarioSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version":      positiveIntegerSchema("Capability scenario schema version."),
			"id":                  nonEmptyStringSchema("Stable task scenario id."),
			"name":                nonEmptyStringSchema("Human-readable scenario name."),
			"summary":             stringSchema("Short scenario summary."),
			"description":         stringSchema("Longer scenario description."),
			"industry":            stringSchema("Industry or team workflow category."),
			"recommended_pack_id": nonEmptyStringSchema("Recommended capability pack id for this scenario."),
			"task_profile":        capabilityTaskProfileSchema(),
			"inputs":              arraySchema(capabilityScenarioIOSchema(), "Expected task inputs for this scenario."),
			"deliverables":        arraySchema(capabilityScenarioIOSchema(), "Expected task deliverables for this scenario."),
			"workflow":            arraySchema(capabilityScenarioStepSchema(), "Business workflow steps for the scenario."),
			"skills":              arraySchema(capabilityScenarioSkillSchema(), "Scenario skills with role, priority, stage, and reason."),
			"acceptance_criteria": stringArraySchema("Checks that should be satisfied before the scenario is treated as complete.", false),
			"execution_notes":     stringArraySchema("Coordination notes for agents running this scenario.", false),
		},
		[]string{"id", "name", "recommended_pack_id", "task_profile", "skills"},
		nil,
	)
}

func capabilityTaskProfileSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"intent":              nonEmptyStringSchema("Task intent represented by the scenario."),
			"domains":             stringArraySchema("Workflow domains covered by the scenario.", false),
			"needs":               stringArraySchema("Capabilities or outcomes needed by the task.", false),
			"risk_level":          stringSchema("Risk level such as low, medium, or high."),
			"requires_user_input": booleanSchema("Whether a human input or approval step is expected."),
		},
		[]string{"intent", "requires_user_input"},
		nil,
	)
}

func capabilityScenarioIOSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":          nonEmptyStringSchema("Stable input or deliverable id."),
			"label":       nonEmptyStringSchema("Human-readable input or deliverable label."),
			"description": stringSchema("Short explanation of the input or deliverable."),
			"formats":     stringArraySchema("Accepted or produced formats.", false),
			"required":    booleanSchema("Whether this item is required for the scenario."),
		},
		[]string{"id", "label", "required"},
		nil,
	)
}

func capabilityScenarioStepSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":          nonEmptyStringSchema("Stable workflow step id."),
			"title":       nonEmptyStringSchema("Human-readable workflow step title."),
			"stage":       nonEmptyStringSchema("Execution stage for the step."),
			"description": stringSchema("Short explanation of the step."),
			"skills":      stringArraySchema("Skills used by this workflow step.", false),
		},
		[]string{"id", "title", "stage"},
		nil,
	)
}

func capabilityScenarioSkillSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":      nonEmptyStringSchema("Skill name used by the scenario."),
			"role":      nonEmptyStringSchema("Scenario role such as discovery, implementation, validation, asset_creation, handoff, or fallback."),
			"priority":  nonEmptyStringSchema("Priority such as required, recommended, conditional, or fallback."),
			"stage":     nonEmptyStringSchema("Execution stage such as task_profile, editing, verification, or handoff."),
			"reason":    stringSchema("Why this skill belongs in the scenario."),
			"condition": stringSchema("Condition that activates a conditional skill."),
		},
		[]string{"name", "role", "priority", "stage"},
		nil,
	)
}

func capabilityScenarioViewSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"scenario":               capabilityScenarioSchema(),
			"recommended_pack":       capabilityPackViewSchema(),
			"install_plan":           capabilityPackInstallPlanSchema(),
			"required_skills":        arraySchema(capabilityScenarioSkillSchema(), "Required scenario skills."),
			"missing_skills":         arraySchema(capabilityPackSkillSchema(), "Recommended-pack skills missing locally."),
			"installed_skills":       arraySchema(capabilityPackSkillSchema(), "Recommended-pack skills already installed locally."),
			"ready":                  booleanSchema("Whether required scenario skills are installed locally."),
			"billing_preview_totals": arraySchema(billingTotalSchema(), "Install billing preview totals for the recommended pack."),
			"warnings":               stringArraySchema("Warnings for the scenario install/readiness plan.", false),
		},
		[]string{"scenario", "recommended_pack", "install_plan", "ready"},
		nil,
	)
}

func capabilityPackInstallResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"pack":            capabilityPackViewSchema(),
			"results":         arraySchema(installResultSchema(), "Install results for every skill in the pack."),
			"install_record":  installRecordSchema(),
			"billing_records": arraySchema(billingRecordSchema(), "Billing records created by the pack install."),
		},
		[]string{"pack", "results"},
		nil,
	)
}

func capabilityPackInstallPlanSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"action":          nonEmptyStringSchema("Capability-pack mutation action such as install_pack."),
			"pack":            capabilityPackViewSchema(),
			"changes":         arraySchema(plannedChangeSchema(), "Planned skill changes for the pack."),
			"billing_preview": arraySchema(billingRecordSchema(), "Billing records expected if the pack install proceeds."),
			"totals":          arraySchema(billingTotalSchema(), "Billing preview totals grouped by currency."),
			"requires":        stringArraySchema("Preconditions required before mutation, such as confirmation.", false),
			"warnings":        stringArraySchema("Warnings for the pack install plan.", false),
		},
		[]string{"action", "pack", "changes"},
		nil,
	)
}

func capabilityScenarioInstallPlanSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"action":    nonEmptyStringSchema("Scenario mutation action, always install_scenario."),
			"scenario":  capabilityScenarioViewSchema(),
			"pack_plan": capabilityPackInstallPlanSchema(),
			"requires":  stringArraySchema("Preconditions required before mutation, such as confirmation.", false),
			"warnings":  stringArraySchema("Warnings for the scenario install plan.", false),
		},
		[]string{"action", "scenario", "pack_plan"},
		nil,
	)
}

func capabilityScenarioInstallResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"scenario":     capabilityScenarioViewSchema(),
			"pack_install": capabilityPackInstallResultSchema(),
		},
		[]string{"scenario", "pack_install"},
		nil,
	)
}

func capabilityScenarioLedgerSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version":       positiveIntegerSchema("Scenario ledger schema version."),
			"generated_at":         nonEmptyStringSchema("Ledger generation timestamp."),
			"scenario":             capabilityScenarioViewSchema(),
			"latest_install":       installRecordSchema(),
			"install_records":      arraySchema(installRecordSchema(), "Scenario install records matching the filters."),
			"billing":              billingRecordListResultSchema(),
			"usage_records":        arraySchema(billingRecordSchema(), "Scenario skill-usage billing records matching the filters."),
			"pack_install_records": arraySchema(billingRecordSchema(), "Scenario pack-install billing records matching the filters."),
		},
		[]string{"schema_version", "generated_at", "scenario", "billing"},
		nil,
	)
}

func installResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":             nonEmptyStringSchema("Installed skill name."),
			"version":          nonEmptyStringSchema("Resolved installed version."),
			"status":           nonEmptyStringSchema("Install status such as installed or already_installed."),
			"path":             stringSchema("Filesystem path for the installed version."),
			"previous_version": stringSchema("Previous current version before this install."),
			"stub":             booleanSchema("Whether the installed package is a stub."),
		},
		[]string{"name", "version", "status", "path", "stub"},
		nil,
	)
}

func installRecordSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"record_id":   nonEmptyStringSchema("Stable local install record id."),
			"action":      nonEmptyStringSchema("Install action such as install_skill or install_pack."),
			"pack_id":     stringSchema("Capability pack id for pack installs."),
			"pack_tier":   stringSchema("Capability pack tier for pack installs."),
			"scenario_id": stringSchema("Real task scenario id for scenario-driven installs."),
			"skill_name":  stringSchema("Skill name for direct skill installs."),
			"skills":      arraySchema(installRecordSkillSchema(), "Skill-level install results captured in this record."),
			"status":      nonEmptyStringSchema("Install status."),
			"device_id":   stringSchema("Local Pro device id when available."),
			"occurred_at": nonEmptyStringSchema("Install record timestamp."),
			"integrity":   recordIntegritySchema(),
		},
		[]string{"record_id", "action", "status", "occurred_at"},
		nil,
	)
}

func installRecordListResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"records":   arraySchema(installRecordSchema(), "Local install records."),
			"integrity": ledgerIntegritySummarySchema(),
		},
		[]string{"records"},
		nil,
	)
}

func installRecordSkillSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":             nonEmptyStringSchema("Installed skill name."),
			"version":          stringSchema("Installed version."),
			"previous_version": stringSchema("Previous active version before install."),
			"status":           nonEmptyStringSchema("Install result status."),
			"path":             stringSchema("Filesystem path for the installed version."),
			"stub":             booleanSchema("Whether the installed skill is a stub."),
		},
		[]string{"name", "status"},
		nil,
	)
}

func billingRecordSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"record_id":          nonEmptyStringSchema("Stable local billing record id."),
			"type":               nonEmptyStringSchema("Billing record type such as pack_install or skill_usage."),
			"pack_id":            stringSchema("Associated capability pack id."),
			"pack_tier":          stringSchema("Associated capability pack tier."),
			"scenario_id":        stringSchema("Real task scenario id for scenario-driven installs."),
			"skill_name":         stringSchema("Associated skill name for usage billing."),
			"version_id":         stringSchema("Skill or pack version id."),
			"vendor_id":          stringSchema("Vendor id declared by the manifest."),
			"meter":              nonEmptyStringSchema("Billing meter."),
			"quantity":           numberSchema("Billed quantity."),
			"currency":           stringSchema("Currency or AGTX_CREDIT."),
			"unit_price_minor":   nonNegativeIntegerSchema("Unit price in minor currency units or credit units."),
			"gross_amount_minor": nonNegativeIntegerSchema("Gross billed amount."),
			"status":             nonEmptyStringSchema("Billing record status."),
			"invocation_id":      stringSchema("Run invocation id for usage billing."),
			"usage_event_id":     stringSchema("Usage event id for usage billing."),
			"error":              stringSchema("Best-effort billing/reporting error."),
			"occurred_at":        nonEmptyStringSchema("Billing record timestamp."),
			"integrity":          recordIntegritySchema(),
		},
		[]string{"record_id", "type", "meter", "quantity", "status", "occurred_at"},
		nil,
	)
}

func billingRecordListResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"records":   arraySchema(billingRecordSchema(), "Local billing records."),
			"totals":    arraySchema(billingTotalSchema(), "Billing totals grouped by currency."),
			"integrity": ledgerIntegritySummarySchema(),
		},
		[]string{"records"},
		nil,
	)
}

func recordIntegritySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"algorithm":     stringSchema("Local ledger integrity algorithm."),
			"ledger":        stringSchema("Ledger file covered by this integrity entry."),
			"key_id":        stringSchema("Local integrity key identifier."),
			"sequence":      positiveIntegerSchema("Ledger sequence number."),
			"previous_hash": stringSchema("Previous record hash in the local chain."),
			"hash":          stringSchema("Current record HMAC hash."),
			"signed_at":     stringSchema("Timestamp when this record was locally signed."),
			"verified_at":   stringSchema("Timestamp when this record was verified for the response."),
			"status":        stringSchema("Integrity status such as verified, failed, or legacy_unsigned."),
			"reason":        stringSchema("Integrity failure or legacy reason."),
			"head_hash":     stringSchema("Current signed ledger head hash."),
			"head_matched":  booleanSchema("Whether this record hash matches the signed ledger head."),
		},
		nil,
		nil,
	)
}

func ledgerIntegritySummarySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"ledger":          nonEmptyStringSchema("Ledger file covered by the summary."),
			"algorithm":       stringSchema("Local ledger integrity algorithm."),
			"status":          nonEmptyStringSchema("Overall ledger integrity status."),
			"records":         nonNegativeIntegerSchema("Number of records inspected."),
			"verified":        nonNegativeIntegerSchema("Number of verified signed records."),
			"failed":          nonNegativeIntegerSchema("Number of records with failed integrity checks."),
			"legacy_unsigned": nonNegativeIntegerSchema("Number of records written before local integrity signing."),
			"anchors":         nonNegativeIntegerSchema("Number of local ledger anchors inspected."),
			"anchor_matched":  booleanSchema("Whether local ledger anchors match the current ledger head."),
			"key_id":          stringSchema("Local integrity key identifier."),
			"last_hash":       stringSchema("Last record hash observed in the ledger."),
			"head_hash":       stringSchema("Signed ledger head hash."),
			"head_matched":    booleanSchema("Whether the last record hash matches the signed ledger head."),
			"verified_at":     stringSchema("Timestamp when the ledger was verified."),
			"reason":          stringSchema("First failure reason, when any."),
		},
		[]string{"ledger", "status", "records", "verified", "failed", "legacy_unsigned", "anchors", "anchor_matched", "head_matched"},
		nil,
	)
}

func billingTotalSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"currency":           nonEmptyStringSchema("Currency code or AGTX_CREDIT."),
			"records":            nonNegativeIntegerSchema("Number of billing records in this currency."),
			"gross_amount_minor": nonNegativeIntegerSchema("Gross amount across records."),
		},
		[]string{"currency", "records", "gross_amount_minor"},
		nil,
	)
}

func commerceSnapshotSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version":  positiveIntegerSchema("Commerce snapshot schema version."),
			"generated_at":    nonEmptyStringSchema("Snapshot generation timestamp."),
			"packs":           arraySchema(capabilityPackViewSchema(), "Capability packs with install state."),
			"scenarios":       arraySchema(capabilityScenarioViewSchema(), "Task scenarios mapped to capability packs."),
			"install_records": installRecordListResultSchema(),
			"billing":         billingRecordListResultSchema(),
			"receipts":        commerceReceiptListResultSchema(),
			"integrity":       arraySchema(ledgerIntegritySummarySchema(), "Local ledger integrity summaries."),
		},
		[]string{"schema_version", "generated_at", "packs", "billing", "receipts"},
		nil,
	)
}

func commerceIntegritySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version": positiveIntegerSchema("Commerce integrity result schema version."),
			"generated_at":   nonEmptyStringSchema("Integrity verification timestamp."),
			"ok":             booleanSchema("Whether all commerce ledger integrity and private-path checks passed."),
			"summary":        diagnosticSummarySchema(),
			"ledgers":        arraySchema(ledgerIntegritySummarySchema(), "Local commerce ledger integrity summaries."),
			"checks":         arraySchema(doctorCheckSchema(), "Commerce ledger integrity and private-path checks."),
		},
		[]string{"schema_version", "generated_at", "ok", "summary", "ledgers", "checks"},
		nil,
	)
}

func commerceProofPayloadSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version": positiveIntegerSchema("Commerce proof payload schema version."),
			"generated_at":   nonEmptyStringSchema("Proof payload generation timestamp."),
			"challenge":      nonEmptyStringSchema("Website-provided nonce or challenge covered by the signature."),
			"subject":        nonEmptyStringSchema("Signed proof subject."),
			"trust_level":    nonEmptyStringSchema("Local proof trust level."),
			"receipt_status": nonEmptyStringSchema("Whether the proof has only local signing or a server receipt."),
			"algorithm":      nonEmptyStringSchema("Signature algorithm covered by the payload."),
			"key_id":         nonEmptyStringSchema("Local proof signing key identifier."),
			"public_key":     nonEmptyStringSchema("Base64 Ed25519 public key covered by the payload."),
			"device_id":      stringSchema("Optional local Pro device id."),
			"ok":             booleanSchema("Whether all commerce ledger integrity and private-path checks passed."),
			"summary":        diagnosticSummarySchema(),
			"ledgers":        arraySchema(ledgerIntegritySummarySchema(), "Local commerce ledger integrity summaries covered by the proof."),
			"checks":         arraySchema(doctorCheckSchema(), "Commerce ledger checks covered by the proof."),
		},
		[]string{"schema_version", "generated_at", "challenge", "subject", "trust_level", "receipt_status", "algorithm", "key_id", "public_key", "ok", "summary", "ledgers", "checks"},
		nil,
	)
}

func commerceProofSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version": positiveIntegerSchema("Commerce proof schema version."),
			"generated_at":   nonEmptyStringSchema("Proof generation timestamp."),
			"challenge":      nonEmptyStringSchema("Website-provided nonce or challenge covered by the proof."),
			"subject":        nonEmptyStringSchema("Signed proof subject."),
			"trust_level":    nonEmptyStringSchema("Local proof trust level."),
			"receipt_status": nonEmptyStringSchema("Whether the proof has only local signing or a server receipt."),
			"algorithm":      nonEmptyStringSchema("Signature algorithm."),
			"key_id":         nonEmptyStringSchema("Local proof signing key identifier."),
			"public_key":     nonEmptyStringSchema("Base64 Ed25519 public key."),
			"payload_hash":   nonEmptyStringSchema("SHA-256 hash of the canonical proof payload."),
			"signature":      nonEmptyStringSchema("Base64 Ed25519 signature over the canonical proof payload."),
			"payload":        commerceProofPayloadSchema(),
		},
		[]string{"schema_version", "generated_at", "challenge", "subject", "trust_level", "receipt_status", "algorithm", "key_id", "public_key", "payload_hash", "signature", "payload"},
		nil,
	)
}

func commerceReceiptSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version":     positiveIntegerSchema("Commerce receipt schema version."),
			"receipt_id":         nonEmptyStringSchema("Stable server receipt id."),
			"status":             nonEmptyStringSchema("Receipt status such as server_received."),
			"received_at":        nonEmptyStringSchema("Server receipt timestamp."),
			"issuer":             stringSchema("Receipt issuer."),
			"server_ledger_id":   stringSchema("Server-side ledger identifier."),
			"server_sequence":    nonNegativeIntegerSchema("Server-side receipt sequence when available."),
			"algorithm":          nonEmptyStringSchema("Server receipt signature algorithm."),
			"key_id":             nonEmptyStringSchema("Server receipt signing key id."),
			"public_key":         nonEmptyStringSchema("Base64 server receipt Ed25519 public key."),
			"proof_payload_hash": nonEmptyStringSchema("SHA-256 hash of the submitted commerce proof payload."),
			"proof_signature":    nonEmptyStringSchema("Base64 local proof signature acknowledged by the server."),
			"proof_key_id":       nonEmptyStringSchema("Local proof signing key acknowledged by the server."),
			"challenge":          nonEmptyStringSchema("Website-provided challenge covered by the submitted proof."),
			"device_id":          stringSchema("Optional Pro device id acknowledged by the server."),
			"server_signature":   nonEmptyStringSchema("Base64 server signature over the canonical receipt payload."),
			"integrity":          recordIntegritySchema(),
		},
		[]string{"schema_version", "receipt_id", "status", "received_at", "algorithm", "key_id", "public_key", "proof_payload_hash", "proof_signature", "proof_key_id", "challenge", "server_signature"},
		nil,
	)
}

func commerceReceiptVerificationSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version":           positiveIntegerSchema("Commerce receipt verification schema version."),
			"verified_at":              nonEmptyStringSchema("Verification timestamp."),
			"ok":                       booleanSchema("Whether the receipt matches the proof and server signature."),
			"receipt_matched":          booleanSchema("Whether required receipt envelope fields are present and valid."),
			"proof_matched":            booleanSchema("Whether receipt fields match the submitted proof."),
			"proof_signature_matched":  booleanSchema("Whether the original local proof signature verifies."),
			"server_signature_matched": booleanSchema("Whether the server receipt signature verifies."),
			"expected_payload_hash":    stringSchema("Proof payload hash expected from the submitted proof."),
			"actual_payload_hash":      stringSchema("Proof payload hash recorded in the receipt."),
			"receipt_id":               stringSchema("Receipt id that was verified."),
			"status":                   stringSchema("Receipt status that was verified."),
			"reason":                   stringSchema("Verification failure reason, when any."),
		},
		[]string{"schema_version", "verified_at", "ok", "receipt_matched", "proof_matched", "proof_signature_matched", "server_signature_matched"},
		nil,
	)
}

func commerceReceiptListResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"records":   arraySchema(commerceReceiptSchema(), "Local commerce proof server receipts."),
			"integrity": ledgerIntegritySummarySchema(),
		},
		[]string{"records"},
		nil,
	)
}

func commerceReceiptSubmitResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"schema_version": positiveIntegerSchema("Commerce receipt submit result schema version."),
			"submitted_at":   nonEmptyStringSchema("Proof submission timestamp."),
			"proof":          commerceProofSchema(),
			"receipt":        commerceReceiptSchema(),
			"verification":   commerceReceiptVerificationSchema(),
		},
		[]string{"schema_version", "submitted_at", "proof", "receipt", "verification"},
		nil,
	)
}

func plannedChangeSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":            nonEmptyStringSchema("Skill name affected by the mutation."),
			"current_version": stringSchema("Currently active installed version."),
			"target_version":  stringSchema("Target version after the mutation."),
			"status":          nonEmptyStringSchema("Mutation status such as install, rollback, or already_current."),
			"stub":            booleanSchema("Whether the target package is a stub."),
			"permissions":     stringArraySchema("Permissions requested by the target package.", false),
			"commerce":        commerceSummarySchema(),
			"path":            stringSchema("Filesystem path affected by the mutation."),
		},
		[]string{"name", "status"},
		nil,
	)
}

func commerceSummarySchema() map[string]any {
	return objectSchema(
		map[string]any{
			"vendor_id":          stringSchema("ISV or first-party vendor identifier."),
			"capability_class":   stringSchema("Capability class such as tool, workflow, connector, model_adapter, content, or commerce."),
			"billing_meters":     stringArraySchema("Billing meters declared by the target package.", false),
			"attribution_events": stringArraySchema("CPA/CPS attribution events declared by the target package.", false),
			"support_url":        stringSchema("Vendor support URL."),
		},
		nil,
		nil,
	)
}

func mutationPlanSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"action":  nonEmptyStringSchema("Mutation action such as install, upgrade, rollback, or uninstall."),
			"changes": arraySchema(plannedChangeSchema(), "Planned filesystem and version changes."),
		},
		[]string{"action", "changes"},
		nil,
	)
}

func rollbackResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":             nonEmptyStringSchema("Rolled back skill name."),
			"version":          nonEmptyStringSchema("New active version after rollback."),
			"previous_version": nonEmptyStringSchema("Version that was active before rollback."),
			"path":             stringSchema("Filesystem path for the active rolled back version."),
		},
		[]string{"name", "version", "previous_version", "path"},
		nil,
	)
}

func uninstallResultSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"name":             nonEmptyStringSchema("Uninstalled skill name."),
			"removed_versions": stringArraySchema("Removed skill versions.", false),
			"status":           nonEmptyStringSchema("Uninstall status."),
		},
		[]string{"name", "removed_versions", "status"},
		nil,
	)
}

func errorResponseSchema(dataSchema map[string]any) map[string]any {
	if dataSchema == nil {
		dataSchema = anySchema("Optional partial tool data captured before the failure.")
	}
	return objectSchema(
		map[string]any{
			"ok":       schemaWithDescription(map[string]any{"type": "boolean", "const": false}, "Always false for tool failures."),
			"data":     dataSchema,
			"warnings": stringArraySchema("Optional warning messages emitted alongside the failure.", false),
			"error":    coreErrorSchema(),
			"trace_id": nonEmptyStringSchema("Trace identifier for correlating the failure."),
		},
		[]string{"ok", "error", "trace_id"},
		nil,
	)
}

func coreErrorSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"code":    nonEmptyStringSchema("Stable agtx error code."),
			"message": nonEmptyStringSchema("Human-readable error message."),
			"details": anySchema("Optional structured error details."),
		},
		[]string{"code", "message"},
		nil,
	)
}

func schemaWithDescription(schema map[string]any, description string) map[string]any {
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func allowedToolArguments(name string) (map[string]bool, bool) {
	switch name {
	case "search_skills":
		return toolArgumentSet("query", "limit"), true
	case "list_skills":
		return toolArgumentSet("installed", "available"), true
	case "list_capability_packs":
		return toolArgumentSet(), true
	case "list_capability_scenarios":
		return toolArgumentSet("pack_id"), true
	case "get_capability_scenario":
		return toolArgumentSet("scenario"), true
	case "plan_capability_pack_install":
		return toolArgumentSet("pack"), true
	case "install_capability_pack":
		return toolArgumentSet("pack", "yes"), true
	case "plan_capability_scenario_install":
		return toolArgumentSet("scenario"), true
	case "install_capability_scenario":
		return toolArgumentSet("scenario", "yes"), true
	case "get_capability_scenario_ledger":
		return toolArgumentSet("scenario", "pack_id", "scenario_id", "skill", "status", "type", "currency", "from", "to", "limit"), true
	case "list_install_records", "list_billing_records", "list_commerce_receipts", "get_commerce_snapshot":
		return toolArgumentSet("pack_id", "scenario_id", "skill", "status", "type", "currency", "from", "to", "limit"), true
	case "get_commerce_proof":
		return toolArgumentSet("challenge"), true
	case "submit_commerce_proof":
		return toolArgumentSet("challenge", "yes"), true
	case "list_agent_targets":
		return toolArgumentSet(), true
	case "get_agent_target":
		return toolArgumentSet("target"), true
	case "get_status", "list_config_keys", "list_registry_sources", "get_pro_status", "get_pro_setup", "start_pro_login", "list_pro_devices", "logout_pro", "register_pro_scheme", "doctor", "refresh_registry", "get_commerce_integrity":
		return toolArgumentSet(), true
	case "validate_registry":
		return toolArgumentSet("path"), true
	case "complete_pro_login":
		return toolArgumentSet("callback_uri"), true
	case "revoke_pro_device":
		return toolArgumentSet("device_id", "yes"), true
	case "verify_skill":
		return toolArgumentSet("skill"), true
	case "plan_install":
		return toolArgumentSet("skill", "skills"), true
	case "install_skill":
		return toolArgumentSet("skill", "yes"), true
	case "upgrade_skill":
		return toolArgumentSet("skill", "yes", "plan"), true
	case "rollback_skill":
		return toolArgumentSet("skill", "to", "yes", "plan"), true
	case "uninstall_skill":
		return toolArgumentSet("skill", "all_versions", "yes", "plan"), true
	case "run_skill":
		return toolArgumentSet("skill", "args", "input", "input_base64", "input_path", "timeout_ms", "output_limit_bytes", "scenario_id", "agent_name", "ocr"), true
	default:
		return nil, false
	}
}

func toolArgumentSet(names ...string) map[string]bool {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	return allowed
}

func parseToolArguments(tool string, raw json.RawMessage, allowed map[string]bool) (toolArguments, error) {
	args := toolArguments{values: map[string]json.RawMessage{}, tool: tool, allowed: allowed}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return args, nil
	}
	if err := decodeJSONStrict(trimmed, &args.values); err != nil {
		return toolArguments{}, core.NewError(core.CodeInvalidArgument, "invalid tool arguments", map[string]any{
			"tool":                tool,
			"expected":            "object",
			"error":               err.Error(),
			"supported_arguments": toolArgumentNames(allowed),
		})
	}
	for name := range args.values {
		if !allowed[name] {
			return toolArguments{}, core.NewError(core.CodeInvalidArgument, "unknown tool argument", map[string]any{
				"tool":                tool,
				"argument":            name,
				"supported_arguments": toolArgumentNames(allowed),
			})
		}
	}
	return args, nil
}

func toolArgumentNames(allowed map[string]bool) []string {
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (a toolArguments) String(name string) (string, error) {
	raw, ok := a.values[name]
	if !ok {
		return "", nil
	}
	var value string
	if err := decodeJSONStrict(raw, &value); err != nil {
		return "", a.invalidArgumentType(name, "string", err)
	}
	return value, nil
}

func (a toolArguments) StringSlice(name string) ([]string, error) {
	raw, ok := a.values[name]
	if !ok {
		return nil, nil
	}
	var value []string
	if err := decodeJSONStrict(raw, &value); err != nil {
		return nil, a.invalidArgumentType(name, "array of strings", err)
	}
	return value, nil
}

func (a toolArguments) Bool(name string, fallback bool) (bool, error) {
	raw, ok := a.values[name]
	if !ok {
		return fallback, nil
	}
	var value bool
	if err := decodeJSONStrict(raw, &value); err != nil {
		return false, a.invalidArgumentType(name, "boolean", err)
	}
	return value, nil
}

func (a toolArguments) PositiveInt(name string, fallback int) (int, error) {
	raw, ok := a.values[name]
	if !ok {
		return fallback, nil
	}
	var value int
	if err := decodeJSONStrict(raw, &value); err != nil {
		return 0, a.invalidArgumentType(name, "integer", err)
	}
	if value <= 0 {
		return 0, a.invalidPositiveInteger(name, value)
	}
	return value, nil
}

func (a toolArguments) PositiveInt64(name string, fallback int64) (int64, error) {
	raw, ok := a.values[name]
	if !ok {
		return fallback, nil
	}
	var value int64
	if err := decodeJSONStrict(raw, &value); err != nil {
		return 0, a.invalidArgumentType(name, "integer", err)
	}
	if value <= 0 {
		return 0, a.invalidPositiveInteger(name, value)
	}
	return value, nil
}

func (a toolArguments) OCRArgs(skill string) ([]string, error) {
	raw, ok := a.values["ocr"]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	if !isOCRSkillName(skill) {
		return nil, a.unsupportedValue("ocr options are only supported for the built-in OCR skill and its aliases", "ocr", "set", "supported_skills", []string{"ocr", "rapidocr", "ppocrv6", "paddleocr"})
	}
	var options ocrToolOptions
	if err := decodeJSONStrict(raw, &options); err != nil {
		return nil, a.invalidArgumentType("ocr", "object", err)
	}
	return options.args()
}

type ocrToolOptions struct {
	Probe           bool     `json:"probe,omitempty"`
	DownloadRuntime bool     `json:"download_runtime,omitempty"`
	DownloadModels  bool     `json:"download_models,omitempty"`
	DryRun          bool     `json:"dry_run,omitempty"`
	KeepArchive     bool     `json:"keep_archive,omitempty"`
	Backend         string   `json:"backend,omitempty"`
	ModelProfile    string   `json:"model_profile,omitempty"`
	ModelSize       string   `json:"model_size,omitempty"`
	ModelDir        string   `json:"model_dir,omitempty"`
	RuntimeDir      string   `json:"runtime_dir,omitempty"`
	RuntimeVersion  string   `json:"runtime_version,omitempty"`
	DetModel        string   `json:"det_model,omitempty"`
	RecModel        string   `json:"rec_model,omitempty"`
	Keys            string   `json:"keys,omitempty"`
	DetInput        string   `json:"det_input,omitempty"`
	DetOutput       string   `json:"det_output,omitempty"`
	RecInput        string   `json:"rec_input,omitempty"`
	RecOutput       string   `json:"rec_output,omitempty"`
	DetLimitSideLen *int     `json:"det_limit_side_len,omitempty"`
	DetThreshold    *float64 `json:"det_threshold,omitempty"`
	BoxThreshold    *float64 `json:"box_threshold,omitempty"`
	UnclipRatio     *float64 `json:"unclip_ratio,omitempty"`
	MaxCandidates   *int     `json:"max_candidates,omitempty"`
	TextScore       *float64 `json:"text_score,omitempty"`
	RecWidth        *int     `json:"rec_width,omitempty"`
	RecHeight       *int     `json:"rec_height,omitempty"`
	RecMaxWidth     *int     `json:"rec_max_width,omitempty"`
}

func (o ocrToolOptions) args() ([]string, error) {
	actionCount := 0
	for _, enabled := range []bool{o.Probe, o.DownloadRuntime, o.DownloadModels} {
		if enabled {
			actionCount++
		}
	}
	if actionCount > 1 {
		return nil, core.NewError(core.CodeInvalidArgument, "only one OCR action can be requested at a time", map[string]any{"argument": "ocr", "actions": []string{"probe", "download_runtime", "download_models"}})
	}
	args := []string{}
	if o.Probe {
		args = append(args, "--probe")
	}
	if o.DownloadRuntime {
		args = append(args, "--download-runtime")
	}
	if o.DownloadModels {
		args = append(args, "--download-models")
	}
	if o.DryRun {
		args = append(args, "--dry-run")
	}
	if o.KeepArchive {
		args = append(args, "--keep-archive")
	}
	if err := appendOCREnumOption(&args, "backend", o.Backend, []string{"auto", "onnxruntime", "ncnn"}); err != nil {
		return nil, err
	}
	if err := appendOCREnumOption(&args, "model-profile", o.ModelProfile, []string{"auto", "rapidocr", "ppocrv6", "ppocrv5", "ppocrv4"}); err != nil {
		return nil, err
	}
	if err := appendOCREnumOption(&args, "model-size", o.ModelSize, []string{"auto", "mobile", "tiny", "small", "medium"}); err != nil {
		return nil, err
	}
	appendOCRStringOption(&args, "model-dir", o.ModelDir)
	appendOCRStringOption(&args, "runtime-dir", o.RuntimeDir)
	appendOCRStringOption(&args, "runtime-version", o.RuntimeVersion)
	appendOCRStringOption(&args, "det-model", o.DetModel)
	appendOCRStringOption(&args, "rec-model", o.RecModel)
	appendOCRStringOption(&args, "keys", o.Keys)
	appendOCRStringOption(&args, "det-input", o.DetInput)
	appendOCRStringOption(&args, "det-output", o.DetOutput)
	appendOCRStringOption(&args, "rec-input", o.RecInput)
	appendOCRStringOption(&args, "rec-output", o.RecOutput)
	if err := appendOCRIntOption(&args, "det-limit-side-len", o.DetLimitSideLen); err != nil {
		return nil, err
	}
	if err := appendOCRFloatOption(&args, "det-threshold", o.DetThreshold); err != nil {
		return nil, err
	}
	if err := appendOCRFloatOption(&args, "box-threshold", o.BoxThreshold); err != nil {
		return nil, err
	}
	if err := appendOCRFloatOption(&args, "unclip-ratio", o.UnclipRatio); err != nil {
		return nil, err
	}
	if err := appendOCRIntOption(&args, "max-candidates", o.MaxCandidates); err != nil {
		return nil, err
	}
	if err := appendOCRFloatOption(&args, "text-score", o.TextScore); err != nil {
		return nil, err
	}
	if err := appendOCRIntOption(&args, "rec-width", o.RecWidth); err != nil {
		return nil, err
	}
	if err := appendOCRIntOption(&args, "rec-height", o.RecHeight); err != nil {
		return nil, err
	}
	if err := appendOCRIntOption(&args, "rec-max-width", o.RecMaxWidth); err != nil {
		return nil, err
	}
	return args, nil
}

func appendOCRStringOption(args *[]string, name, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*args = append(*args, "--"+name, value)
}

func appendOCREnumOption(args *[]string, name, value string, supported []string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	for _, supportedValue := range supported {
		if value == supportedValue {
			appendOCRStringOption(args, name, value)
			return nil
		}
	}
	return core.NewError(core.CodeInvalidArgument, "unsupported OCR option value", map[string]any{"argument": "ocr." + strings.ReplaceAll(name, "-", "_"), "value": value, "supported_values": supported})
}

func appendOCRIntOption(args *[]string, name string, value *int) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return core.NewError(core.CodeInvalidArgument, "OCR option must be a positive integer", map[string]any{"argument": "ocr." + strings.ReplaceAll(name, "-", "_"), "value": *value, "expected": "positive_integer"})
	}
	*args = append(*args, "--"+name, strconv.Itoa(*value))
	return nil
}

func appendOCRFloatOption(args *[]string, name string, value *float64) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return core.NewError(core.CodeInvalidArgument, "OCR option must be a positive number", map[string]any{"argument": "ocr." + strings.ReplaceAll(name, "-", "_"), "value": *value, "expected": "positive_number"})
	}
	*args = append(*args, "--"+name, strconv.FormatFloat(*value, 'g', -1, 64))
	return nil
}

func isOCRSkillName(skill string) bool {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(skill), "-", "_")) {
	case "ocr", "rapidocr", "rapid_ocr", "rapidocr_v6", "rapid_ocr_v6", "paddleocr", "paddle_ocr", "ppocr", "pp_ocr", "ppocrv6", "pp_ocrv6", "pp_ocr_v6", "ocr_v6":
		return true
	default:
		return false
	}
}

func (a toolArguments) invalidBase64Argument(name string, err error) error {
	return core.NewError(core.CodeInvalidArgument, name+" must be valid base64", map[string]any{
		"tool":                a.tool,
		"argument":            name,
		"expected":            "base64_string",
		"error":               err.Error(),
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) mutuallyExclusiveArguments(message string, names ...string) error {
	return core.NewError(core.CodeInvalidArgument, message, map[string]any{
		"tool":                a.tool,
		"arguments":           slices.Clone(names),
		"expected":            "only_one_argument",
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) invalidArgumentType(name, expected string, err error) error {
	return core.NewError(core.CodeInvalidArgument, "invalid tool argument type", map[string]any{
		"tool":                a.tool,
		"argument":            name,
		"expected":            expected,
		"error":               err.Error(),
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) invalidPositiveInteger(name string, value any) error {
	return core.NewError(core.CodeInvalidArgument, name+" must be a positive integer", map[string]any{
		"tool":                a.tool,
		"argument":            name,
		"value":               value,
		"expected":            "positive_integer",
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) missingRequiredArgument(name, expected string) error {
	return core.NewError(core.CodeInvalidArgument, name+" is required", map[string]any{
		"tool":                a.tool,
		"argument":            name,
		"expected":            expected,
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) missingRequiredArguments(message string, names []string, expected string) error {
	return core.NewError(core.CodeInvalidArgument, message, map[string]any{
		"tool":                a.tool,
		"arguments":           slices.Clone(names),
		"expected":            expected,
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) confirmationRequired(message string) error {
	return core.NewError(core.CodeConfirmationRequired, message, map[string]any{
		"tool":                a.tool,
		"argument":            "yes",
		"expected":            true,
		"retry_with":          map[string]any{"yes": true},
		"supported_arguments": toolArgumentNames(a.allowed),
	})
}

func (a toolArguments) unsupportedValue(message, name string, value any, supportedName string, supported []string) error {
	return core.NewError(core.CodeInvalidArgument, message, map[string]any{
		"tool":                a.tool,
		"argument":            name,
		"value":               value,
		supportedName:         slices.Clone(supported),
		"supported_arguments": toolArgumentNames(a.allowed),
	})
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
		return fmt.Errorf("JSON input must contain exactly one value")
	}
	return nil
}
