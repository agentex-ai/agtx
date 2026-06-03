package mcp

import (
	"bufio"
	"bytes"
	"context"
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
	allowed map[string]struct{}
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
		timeoutMS, err := args.PositiveInt64("timeout_ms", int64(s.service.Config.RunTimeoutMS))
		if err != nil {
			return nil, err
		}
		outputLimitBytes, err := args.PositiveInt64("output_limit_bytes", s.service.Config.RunOutputLimitBytes)
		if err != nil {
			return nil, err
		}
		result, err := s.service.RunSkillWithOptions(context.Background(), skill, core.RunOptions{
			Args:             skillArgs,
			Input:            []byte(input),
			Timeout:          time.Duration(timeoutMS) * time.Millisecond,
			OutputLimitBytes: outputLimitBytes,
		})
		if err != nil {
			return toolError(err, result), nil
		}
		return toolJSON(result), nil
	}
	return nil, unknownToolError(request.Name)
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
				"timeout_ms":         positiveIntegerSchema("Execution timeout in milliseconds."),
				"output_limit_bytes": positiveIntegerSchema("Maximum captured stdout and stderr bytes."),
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
		schema["required"] = append([]string{}, required...)
	}
	for key, value := range extras {
		schema[key] = value
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return schemaWithDescription(map[string]any{"type": "string"}, description)
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
			"exit_code":          schemaWithDescription(map[string]any{"type": "integer"}, "Process exit code returned by the skill."),
			"stdout":             stringSchema("Captured standard output."),
			"stderr":             stringSchema("Captured standard error."),
			"stdout_truncated":   booleanSchema("Whether stdout was truncated by output limits."),
			"stderr_truncated":   booleanSchema("Whether stderr was truncated by output limits."),
			"duration_ms":        schemaWithDescription(map[string]any{"type": "integer", "minimum": 0}, "Execution duration in milliseconds."),
			"timed_out":          booleanSchema("Whether execution timed out."),
			"output_limit_bytes": positiveIntegerSchema("Configured output capture limit in bytes."),
			"timeout_ms":         positiveIntegerSchema("Configured timeout in milliseconds."),
		},
		[]string{"name", "exit_code"},
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
			"authenticated": booleanSchema("Whether local Pro auth is available."),
			"subscription":  stringSchema("Subscription status reported by the Pro service."),
			"plan":          stringSchema("Plan name reported by the Pro service."),
			"device_id":     stringSchema("Current local device identifier."),
			"device_name":   stringSchema("Current local device name."),
			"expires_at":    stringSchema("Access token expiry timestamp."),
			"device_limit":  nonNegativeIntegerSchema("Maximum active devices allowed by the subscription."),
			"auth_path":     stringSchema("Local auth.json path."),
			"devices":       arraySchema(proDeviceSchema(), "Known devices for this subscription."),
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
			"stub":           booleanSchema("Whether the skill is currently a stub package."),
		},
		[]string{"name", "version"},
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

func allowedToolArguments(name string) (map[string]struct{}, bool) {
	switch name {
	case "search_skills":
		return toolArgumentSet("query", "limit"), true
	case "list_skills":
		return toolArgumentSet("installed", "available"), true
	case "list_agent_targets":
		return toolArgumentSet(), true
	case "get_agent_target":
		return toolArgumentSet("target"), true
	case "get_status", "list_config_keys", "list_registry_sources", "get_pro_status", "get_pro_setup", "start_pro_login", "list_pro_devices", "logout_pro", "register_pro_scheme", "doctor", "refresh_registry":
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
		return toolArgumentSet("skill", "args", "input", "timeout_ms", "output_limit_bytes"), true
	default:
		return nil, false
	}
}

func toolArgumentSet(names ...string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	return allowed
}

func parseToolArguments(tool string, raw json.RawMessage, allowed map[string]struct{}) (toolArguments, error) {
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
		if _, ok := allowed[name]; !ok {
			return toolArguments{}, core.NewError(core.CodeInvalidArgument, "unknown tool argument", map[string]any{
				"tool":                tool,
				"argument":            name,
				"supported_arguments": toolArgumentNames(allowed),
			})
		}
	}
	return args, nil
}

func toolArgumentNames(allowed map[string]struct{}) []string {
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
		"arguments":           append([]string{}, names...),
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
		supportedName:         append([]string{}, supported...),
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
