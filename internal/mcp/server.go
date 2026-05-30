package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return s.writeError(nil, -32700, "parse error", err.Error())
	}
	if _, batch := raw["0"]; batch {
		return s.writeError(nil, -32600, "invalid request", "JSON-RPC batches are not implemented in agtx mcp v1")
	}
	id, hasID := raw["id"]
	var method string
	if err := json.Unmarshal(raw["method"], &method); err != nil || method == "" {
		if hasID {
			return s.writeError(id, -32600, "invalid request", "missing method")
		}
		return nil
	}
	params := raw["params"]

	switch method {
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
		result, err := s.callTool(params)
		if err != nil {
			return s.writeResult(id, toolError(err, nil))
		}
		return s.writeResult(id, result)
	default:
		if hasID {
			return s.writeError(id, -32601, "method not found", method)
		}
		return nil
	}
}

func (s *server) callTool(params json.RawMessage) (map[string]any, error) {
	var request toolCallRequest
	if err := decodeJSONStrict(params, &request); err != nil {
		return nil, core.NewError(core.CodeInvalidArgument, "invalid tools/call params", err.Error())
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return nil, core.NewError(core.CodeInvalidArgument, "tool name is required", nil)
	}
	allowed, ok := allowedToolArguments(request.Name)
	if !ok {
		return nil, core.NewError(core.CodeNotFound, "unknown tool", map[string]any{"tool": request.Name})
	}
	args, err := parseToolArguments(request.Arguments, allowed)
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
			return nil, core.NewError(core.CodeInvalidArgument, "query is required", nil)
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
	case "doctor":
		return toolJSON(s.service.Doctor()), nil
	case "verify_skill":
		skill, err := args.String("skill")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(skill) == "" {
			return nil, core.NewError(core.CodeInvalidArgument, "skill is required", nil)
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
			return nil, core.NewError(core.CodeInvalidArgument, "skill is required", nil)
		}
		yes, err := args.Bool("yes", false)
		if err != nil {
			return nil, err
		}
		if !yes {
			return nil, core.NewError(core.CodeConfirmationRequired, "install_skill requires yes=true", map[string]any{"retry_with": map[string]any{"yes": true}})
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
			return nil, core.NewError(core.CodeConfirmationRequired, "upgrade_skill requires yes=true", map[string]any{"retry_with": map[string]any{"yes": true}})
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
			return nil, core.NewError(core.CodeInvalidArgument, "skill is required", nil)
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
			return nil, core.NewError(core.CodeConfirmationRequired, "rollback_skill requires yes=true", map[string]any{"retry_with": map[string]any{"yes": true}})
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
			return nil, core.NewError(core.CodeInvalidArgument, "skill is required", nil)
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
			return nil, core.NewError(core.CodeConfirmationRequired, "uninstall_skill requires yes=true", map[string]any{"retry_with": map[string]any{"yes": true}})
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
			return nil, core.NewError(core.CodeInvalidArgument, "skill is required", nil)
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
	return nil, core.NewError(core.CodeNotFound, "unknown tool", map[string]any{"tool": request.Name})
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
		tool("search_skills", "Search agtx skills by natural-language query.", map[string]any{"query": stringSchema(), "limit": integerSchema()}),
		tool("list_skills", "List installed and/or available agtx skills.", map[string]any{"installed": booleanSchema(), "available": booleanSchema()}),
		tool("get_status", "Return local agtx status and paths.", map[string]any{}),
		tool("doctor", "Run local agtx diagnostics without mutating state.", map[string]any{}),
		tool("verify_skill", "Verify an installed skill manifest, current pointer, platform, and entrypoint.", map[string]any{"skill": stringSchema()}),
		tool("refresh_registry", "Refresh the cached registry from configured registry_url.", map[string]any{}),
		tool("plan_install", "Return the install plan for one or more skills without mutating local state.", map[string]any{"skill": stringSchema(), "skills": arraySchema()}),
		tool("install_skill", "Install one skill. Requires yes=true.", map[string]any{"skill": stringSchema(), "yes": booleanSchema()}),
		tool("upgrade_skill", "Upgrade one skill, or all installed skills if skill is empty. Requires yes=true, or plan=true for dry run.", map[string]any{"skill": stringSchema(), "yes": booleanSchema(), "plan": booleanSchema()}),
		tool("rollback_skill", "Rollback one skill. Requires yes=true, or plan=true for dry run.", map[string]any{"skill": stringSchema(), "to": stringSchema(), "yes": booleanSchema(), "plan": booleanSchema()}),
		tool("uninstall_skill", "Uninstall one skill. Requires yes=true, or plan=true for dry run.", map[string]any{"skill": stringSchema(), "all_versions": booleanSchema(), "yes": booleanSchema(), "plan": booleanSchema()}),
		tool("run_skill", "Run an installed skill.", map[string]any{"skill": stringSchema(), "args": arraySchema(), "input": stringSchema(), "timeout_ms": integerSchema(), "output_limit_bytes": integerSchema()}),
	}
}

func tool(name, description string, properties map[string]any) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": properties,
		},
	}
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func integerSchema() map[string]any {
	return map[string]any{"type": "integer", "minimum": 0}
}

func booleanSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func arraySchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func stringArg(args map[string]json.RawMessage, name string) string {
	var value string
	_ = json.Unmarshal(args[name], &value)
	return value
}

func intArg(args map[string]json.RawMessage, name string, fallback int) int {
	var value int
	if err := json.Unmarshal(args[name], &value); err != nil {
		return fallback
	}
	return value
}

func int64Arg(args map[string]json.RawMessage, name string, fallback int64) int64 {
	var value int64
	if err := json.Unmarshal(args[name], &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

func boolArg(args map[string]json.RawMessage, name string, fallback bool) bool {
	var value bool
	if err := json.Unmarshal(args[name], &value); err != nil {
		return fallback
	}
	return value
}

func stringSliceArg(args map[string]json.RawMessage, name string) []string {
	var value []string
	_ = json.Unmarshal(args[name], &value)
	return value
}
