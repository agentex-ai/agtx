package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/agentex-ai/agtx/internal/core"
	"github.com/agentex-ai/agtx/internal/mcp"
)

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	if args[0] == "--version" || args[0] == "-v" || args[0] == "version" {
		fmt.Fprintln(stdout, core.Version)
		return 0
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		printHelp(stdout)
		return 0
	}

	argsForOutputMode := argsBeforeDoubleDash(args)
	jsonOut := wantsJSONOutput(argsForOutputMode)
	ndjsonOut := wantsNDJSONOutput(argsForOutputMode)
	service, err := core.NewDefaultService()
	if err != nil {
		return failAgent(stdout, stderr, jsonOut, ndjsonOut, err)
	}
	ctx := context.Background()

	switch args[0] {
	case "config":
		return runConfig(service, args[1:], stdout, stderr)
	case "search":
		return runSearch(service, args[1:], stdout, stderr)
	case "install":
		return runInstall(ctx, service, args[1:], stdin, stdout, stderr)
	case "run":
		return runSkill(ctx, service, args[1:], stdin, stdout, stderr)
	case "uninstall":
		return runUninstall(service, args[1:], stdin, stdout, stderr)
	case "list":
		return runList(service, args[1:], stdout, stderr)
	case "upgrade":
		return runUpgrade(ctx, service, args[1:], stdin, stdout, stderr)
	case "rollback":
		return runRollback(service, args[1:], stdin, stdout, stderr)
	case "status":
		return runStatus(service, args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(service, args[1:], stdout, stderr)
	case "verify":
		return runVerify(service, args[1:], stdout, stderr)
	case "registry":
		return runRegistry(ctx, service, args[1:], stdout, stderr)
	case "commerce":
		return runCommerce(ctx, service, args[1:], stdin, stdout, stderr)
	case "pro":
		return runPro(ctx, service, args[1:], stdin, stdout, stderr)
	case "mcp":
		return mcp.Run(service, stdin, stdout, stderr)
	case "agent":
		return runAgent(args[1:], stdout, stderr)
	default:
		err := core.NewError(core.CodeInvalidArgument, "unknown command", map[string]any{"command": args[0], "supported_commands": supportedCommands()})
		if jsonOut || ndjsonOut {
			return failAgent(stdout, stderr, jsonOut, ndjsonOut, err)
		}
		fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		printHelp(stderr)
		return 1
	}
}

func supportedCommands() []string {
	return []string{"config", "search", "install", "run", "uninstall", "list", "upgrade", "rollback", "status", "doctor", "verify", "registry", "commerce", "pro", "mcp", "agent"}
}

func runConfig(service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := wantsJSONOutput(args)
	if len(args) == 0 || onlyJSONFlag(args) {
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "config subcommand is required", map[string]any{"supported_subcommands": configSubcommands()}))
	}
	switch args[0] {
	case "init":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		force := takeBoolFlag(&rest, "--force", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected config init arguments", rest, configInitFlags()))
		}
		config, err := service.InitConfig(force)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(map[string]any{"path": service.Paths.ConfigFile, "config": config}, nil), 0)
		}
		fmt.Fprintf(stdout, "config: %s\n", service.Paths.ConfigFile)
		return 0
	case "path":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected config path arguments", rest, jsonOnlyFlags()))
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(map[string]any{"path": service.Paths.ConfigFile}, nil), 0)
		}
		fmt.Fprintln(stdout, service.Paths.ConfigFile)
		return 0
	case "show":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected config show arguments", rest, jsonOnlyFlags()))
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(service.Config, nil), 0)
		}
		fmt.Fprintf(stdout, "channel: %s\ntelemetry: %s\nregistry_url: %s\n", service.Config.Channel, service.Config.Telemetry, service.Config.RegistryURL)
		fmt.Fprintf(stdout, "pro_api_url: %s\n", service.Config.ProAPIURL)
		fmt.Fprintf(stdout, "agent_name: %s\n", service.Config.AgentName)
		fmt.Fprintf(stdout, "lock_timeout_ms: %d\nstale_lock_ms: %d\n", service.Config.LockTimeoutMS, service.Config.StaleLockMS)
		fmt.Fprintf(stdout, "run_timeout_ms: %d\nrun_output_limit_bytes: %d\n", service.Config.RunTimeoutMS, service.Config.RunOutputLimitBytes)
		fmt.Fprintf(stdout, "registry_max_bytes: %d\nregistry_download_timeout_ms: %d\npackage_max_bytes: %d\npackage_download_timeout_ms: %d\nextracted_max_bytes: %d\nextracted_max_files: %d\n", service.Config.RegistryMaxBytes, service.Config.RegistryDownloadTimeoutMS, service.Config.PackageMaxBytes, service.Config.PackageDownloadTimeoutMS, service.Config.ExtractedMaxBytes, service.Config.ExtractedMaxFiles)
		for _, file := range service.Config.RegistryFiles {
			fmt.Fprintf(stdout, "registry_file: %s\n", file)
		}
		return 0
	case "keys":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected config keys arguments", rest, jsonOnlyFlags()))
		}
		keys := core.ConfigKeys()
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(keys, nil), 0)
		}
		for _, item := range keys {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", item.Key, item.Type, configDefaultString(item.Default), item.Description)
		}
		return 0
	case "set":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) != 2 {
			return fail(stdout, stderr, jsonOut, argumentCountError("config set requires key and value", []string{"key", "value"}, jsonOnlyFlags()))
		}
		config, err := service.SetConfig(rest[0], rest[1])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(map[string]any{"path": service.Paths.ConfigFile, "config": config}, nil), 0)
		}
		fmt.Fprintf(stdout, "%s updated\n", rest[0])
		return 0
	case "unset":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("config unset requires key", []string{"key"}, jsonOnlyFlags()))
		}
		config, err := service.UnsetConfig(rest[0])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(map[string]any{"path": service.Paths.ConfigFile, "config": config}, nil), 0)
		}
		fmt.Fprintf(stdout, "%s reset\n", rest[0])
		return 0
	default:
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "unknown config subcommand", map[string]any{"subcommand": args[0], "supported_subcommands": configSubcommands()}))
	}
}

func configSubcommands() []string {
	return []string{"init", "path", "show", "keys", "set", "unset"}
}

func configInitFlags() []string {
	return []string{"--json", "--force"}
}

func runSearch(service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	limit := takeIntFlag(&args, "--limit", 10, flagSet(searchFlags()))
	if hasInternalInvalidFlag(args) {
		return fail(stdout, stderr, jsonOut, internalFlagError(args, searchFlags()))
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return fail(stdout, stderr, jsonOut, argumentCountError("search query is required", []string{"query"}, searchFlags()))
	}
	results := service.Search(query, limit)
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(results, nil), 0)
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "No matching skills found.")
		return 0
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", result.Skill.Name, result.Skill.Version, result.Skill.Summary)
	}
	return 0
}

func searchFlags() []string {
	return []string{"--json", "--limit"}
}

func installFlags() []string {
	return []string{"--json", "--yes", "-y", "--plan"}
}

func runInstall(ctx context.Context, service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	yes := takeBoolFlag(&args, "--yes", "-y")
	planOnly := takeBoolFlag(&args, "--plan", "")
	if len(args) == 0 {
		return fail(stdout, stderr, jsonOut, argumentCountError("at least one skill name is required", []string{"skill..."}, installFlags()))
	}
	if planOnly {
		plan, err := service.PlanInstall(args)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(plan, nil), 0)
		}
		printPlan(stdout, plan)
		return 0
	}
	if err := confirmMutation("install", args, yes, jsonOut, stdin, stdout); err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	results, err := service.InstallSkills(ctx, args)
	if err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(results, nil), 0)
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "%s %s %s", result.Name, result.Version, result.Status)
		if result.Stub {
			fmt.Fprint(stdout, " (stub)")
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func runSkill(ctx context.Context, service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	args, passthrough := splitArgsAfterDoubleDash(args)
	jsonOut := takeBoolFlag(&args, "--json", "")
	ndjsonOut := takeBoolFlag(&args, "--ndjson", "")
	runKnownFlags := flagSet(runFlags())
	inputPath := takeStringFlag(&args, "--input", "", runKnownFlags)
	timeoutMS := takeIntFlag(&args, "--timeout-ms", service.Config.RunTimeoutMS, runKnownFlags)
	outputLimit := takeInt64Flag(&args, "--output-limit-bytes", service.Config.RunOutputLimitBytes, runKnownFlags)
	scenarioID := takeStringFlag(&args, "--scenario-id", "", runKnownFlags)
	agentName := takeStringFlag(&args, "--agent-name", "", runKnownFlags)
	if jsonOut && ndjsonOut {
		return failRun(stdout, stderr, jsonOut, ndjsonOut, mutuallyExclusiveFlagsError("--json", "--ndjson", runFlags()), core.RunResult{})
	}
	if len(args) == 0 {
		return failRun(stdout, stderr, jsonOut, ndjsonOut, argumentCountError("skill name is required", []string{"skill"}, runFlags()), core.RunResult{})
	}
	if hasInternalInvalidFlag(args) {
		return failRun(stdout, stderr, jsonOut, ndjsonOut, internalFlagError(args, runFlags()), core.RunResult{})
	}
	name := args[0]
	skillArgs := append([]string{}, args[1:]...)
	skillArgs = append(skillArgs, passthrough...)
	input, err := readInput(stdin, inputPath, outputLimit)
	if err != nil {
		return failRun(stdout, stderr, jsonOut, ndjsonOut, err, core.RunResult{})
	}
	if ndjsonOut {
		writeEvent(stdout, "started", map[string]any{"skill": name, "args": skillArgs})
	}
	result, err := service.RunSkillWithOptions(ctx, name, core.RunOptions{
		Args:             skillArgs,
		Input:            input,
		Timeout:          time.Duration(timeoutMS) * time.Millisecond,
		OutputLimitBytes: outputLimit,
		ScenarioID:       scenarioID,
		AgentName:        agentName,
	})
	if err != nil {
		if ndjsonOut {
			return failRun(stdout, stderr, jsonOut, ndjsonOut, err, result)
		}
		return fail(stdout, stderr, jsonOut, err)
	}
	if ndjsonOut {
		writeEvent(stdout, "completed", result)
		return 0
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(result, nil), 0)
	}
	if result.Stdout != "" {
		fmt.Fprint(stdout, result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(stderr, result.Stderr)
	}
	return result.ExitCode
}

func runFlags() []string {
	return []string{"--json", "--ndjson", "--input", "--timeout-ms", "--output-limit-bytes", "--scenario-id", "--agent-name"}
}

func uninstallFlags() []string {
	return []string{"--json", "--yes", "-y", "--plan", "--all-versions"}
}

func runUninstall(service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	yes := takeBoolFlag(&args, "--yes", "-y")
	planOnly := takeBoolFlag(&args, "--plan", "")
	allVersions := takeBoolFlag(&args, "--all-versions", "")
	if len(args) != 1 {
		return fail(stdout, stderr, jsonOut, argumentCountError("uninstall requires exactly one skill name", []string{"skill"}, uninstallFlags()))
	}
	if planOnly {
		plan, err := service.PlanUninstall(args[0], allVersions)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(plan, nil), 0)
		}
		printPlan(stdout, plan)
		return 0
	}
	if err := confirmMutation("uninstall", []string{args[0]}, yes, jsonOut, stdin, stdout); err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	result, err := service.UninstallSkill(args[0], allVersions)
	if err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(result, nil), 0)
	}
	fmt.Fprintf(stdout, "%s %s\n", result.Name, result.Status)
	return 0
}

func runList(service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	installed := takeBoolFlag(&args, "--installed", "")
	available := takeBoolFlag(&args, "--available", "")
	if len(args) > 0 {
		return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected list arguments", args, listFlags()))
	}
	result, err := service.List(core.ListOptions{Installed: installed, Available: available})
	if err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(result, nil), 0)
	}
	if len(result.Installed) > 0 {
		fmt.Fprintln(stdout, "Installed:")
		for _, skill := range result.Installed {
			fmt.Fprintf(stdout, "  %s\t%s\n", skill.Name, skill.Version)
		}
	}
	if len(result.Available) > 0 {
		fmt.Fprintln(stdout, "Available:")
		for _, skill := range result.Available {
			fmt.Fprintf(stdout, "  %s\t%s\t%s\n", skill.Name, skill.Version, skill.Summary)
		}
	}
	if len(result.Installed) == 0 && len(result.Available) == 0 {
		fmt.Fprintln(stdout, "No skills found.")
	}
	return 0
}

func listFlags() []string {
	return []string{"--json", "--installed", "--available"}
}

func upgradeFlags() []string {
	return []string{"--json", "--yes", "-y", "--plan"}
}

func runUpgrade(ctx context.Context, service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	yes := takeBoolFlag(&args, "--yes", "-y")
	planOnly := takeBoolFlag(&args, "--plan", "")
	targets := args
	if len(targets) == 0 {
		targets = []string{"all installed skills"}
	}
	if planOnly {
		plan, err := service.PlanUpgrade(args)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(plan, nil), 0)
		}
		printPlan(stdout, plan)
		return 0
	}
	if err := confirmMutation("upgrade", targets, yes, jsonOut, stdin, stdout); err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	results, err := service.UpgradeSkills(ctx, args)
	if err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(results, nil), 0)
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "%s %s %s\n", result.Name, result.Version, result.Status)
	}
	return 0
}

func runRollback(service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	yes := takeBoolFlag(&args, "--yes", "-y")
	planOnly := takeBoolFlag(&args, "--plan", "")
	targetVersion := takeStringFlag(&args, "--to", "", flagSet(rollbackFlags()))
	if hasInternalInvalidFlag(args) {
		return fail(stdout, stderr, jsonOut, internalFlagError(args, rollbackFlags()))
	}
	if len(args) != 1 {
		return fail(stdout, stderr, jsonOut, argumentCountError("rollback requires exactly one skill name", []string{"skill"}, rollbackFlags()))
	}
	if planOnly {
		plan, err := service.PlanRollback(args[0], targetVersion)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(plan, nil), 0)
		}
		printPlan(stdout, plan)
		return 0
	}
	if err := confirmMutation("rollback", []string{args[0]}, yes, jsonOut, stdin, stdout); err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	result, err := service.RollbackSkill(args[0], targetVersion)
	if err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(result, nil), 0)
	}
	fmt.Fprintf(stdout, "%s rolled back from %s to %s\n", result.Name, result.PreviousVersion, result.Version)
	return 0
}

func rollbackFlags() []string {
	return []string{"--json", "--yes", "-y", "--plan", "--to"}
}

func runStatus(service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	if len(args) > 0 {
		return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected status arguments", args, jsonOnlyFlags()))
	}
	status, err := service.Status()
	if err != nil {
		return fail(stdout, stderr, jsonOut, err)
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(status, nil), 0)
	}
	fmt.Fprintf(stdout, "agtx %s\nplatform: %s/%s\nskills: %d available, %d installed\nchannel: %s\ntelemetry: %s\nconfig: %s\ncache: %s\nlogs: %s\n",
		status.Version, status.GOOS, status.GOARCH, status.RegistrySkills, status.Installed, status.Channel, status.Telemetry, status.ConfigFile, status.CacheDir, status.LogsDir)
	return 0
}

func runDoctor(service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	if len(args) > 0 {
		return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected doctor arguments", args, jsonOnlyFlags()))
	}
	result := service.Doctor()
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(result, doctorWarnings(result.Checks)), diagnosticExitCode(result.OK))
	}
	printDiagnostics(stdout, result.Checks)
	fmt.Fprintf(stdout, "summary: %d checks, %d warnings, %d errors\n", result.Summary.Checks, result.Summary.Warnings, result.Summary.Errors)
	return diagnosticExitCode(result.OK)
}

func runVerify(service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := takeBoolFlag(&args, "--json", "")
	if len(args) != 1 {
		return fail(stdout, stderr, jsonOut, argumentCountError("verify requires exactly one skill name", []string{"skill"}, jsonOnlyFlags()))
	}
	result, err := service.VerifySkill(args[0])
	if jsonOut {
		if err != nil {
			return writeJSON(stdout, core.NewErrorResponse(err, doctorWarnings(result.Checks)), 1)
		}
		return writeJSON(stdout, core.NewResponse(result, doctorWarnings(result.Checks)), diagnosticExitCode(result.OK))
	}
	printDiagnostics(stdout, result.Checks)
	fmt.Fprintf(stdout, "summary: %d checks, %d warnings, %d errors\n", result.Summary.Checks, result.Summary.Warnings, result.Summary.Errors)
	if err != nil {
		fmt.Fprintln(stderr, core.ErrorFrom(err).Message)
		return 1
	}
	return diagnosticExitCode(result.OK)
}

func runRegistry(ctx context.Context, service *core.Service, args []string, stdout, stderr io.Writer) int {
	jsonOut := wantsJSONOutput(args)
	if len(args) == 0 || onlyJSONFlag(args) {
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "registry subcommand is required", map[string]any{"supported_subcommands": registrySubcommands()}))
	}
	switch args[0] {
	case "sources":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected registry sources arguments", rest, jsonOnlyFlags()))
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(service.RegistrySources, nil), 0)
		}
		for _, source := range service.RegistrySources {
			label := source.Path
			if label == "" {
				label = source.URL
			}
			if label == "" {
				label = source.Kind
			}
			status := "skipped"
			if source.Loaded {
				status = "loaded"
			}
			if source.Error != "" {
				status = "error: " + source.Error
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", source.Kind, status, label)
		}
		return 0
	case "refresh":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected registry refresh arguments", rest, jsonOnlyFlags()))
		}
		result, err := service.RefreshRegistry(ctx)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "refreshed %d bytes from %s into %s\n", result.Bytes, result.Source, result.Path)
		return 0
	case "validate":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("registry validate requires path", []string{"path"}, jsonOnlyFlags()))
		}
		result, err := service.ValidateRegistry(rest[0])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, result.Warnings), 0)
		}
		fmt.Fprintf(stdout, "registry: %s\nskills: %d\nok: %t\n", result.Path, result.Skills, result.OK)
		for _, warning := range result.Warnings {
			fmt.Fprintf(stdout, "warning: %s\n", warning)
		}
		return 0
	case "status":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(registryStatusFlags())
		path := takeStringFlag(&rest, "--file", "", known)
		platformsValue := takeStringFlag(&rest, "--platforms", "", known)
		accountsValue := takeStringFlag(&rest, "--accounts", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, registryStatusFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected registry status arguments", rest, registryStatusFlags()))
		}
		options := core.RegistryImplementationStatusOptions{Platforms: splitListFlag(platformsValue), Accounts: splitListFlag(accountsValue)}
		var result core.RegistryImplementationStatus
		var err error
		if strings.TrimSpace(path) != "" {
			result, err = core.BuildRegistryImplementationStatusForFile(path, options)
		} else {
			result, err = core.BuildRegistryImplementationStatus(service.Registry, options)
		}
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		printRegistryImplementationStatus(stdout, result)
		return 0
	case "demo-release":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(registryDemoReleaseFlags())
		outDir := takeStringFlag(&rest, "--out", "", known)
		baseURL := takeStringFlag(&rest, "--base-url", "", known)
		version := takeStringFlag(&rest, "--version", "", known)
		packagePrefix := takeStringFlag(&rest, "--package-prefix", "", known)
		skillsValue := takeStringFlag(&rest, "--skills", "", known)
		platformsValue := takeStringFlag(&rest, "--platforms", "", known)
		accountsValue := takeStringFlag(&rest, "--accounts", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, registryDemoReleaseFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected registry demo-release arguments", rest, registryDemoReleaseFlags()))
		}
		result, err := core.CreateDemoRelease(core.DemoReleaseOptions{
			OutDir:        outDir,
			BaseURL:       baseURL,
			Version:       version,
			PackagePrefix: packagePrefix,
			Skills:        splitListFlag(skillsValue),
			Platforms:     splitListFlag(platformsValue),
			Accounts:      splitListFlag(accountsValue),
		})
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "registry: %s\n", result.RegistryPath)
		for _, registry := range result.Registries {
			fmt.Fprintf(stdout, "registry_%s: %s\n", registry.AccountMode, registry.Path)
		}
		for _, pkg := range result.Packages {
			fmt.Fprintf(stdout, "package: %s\t%s\t%s\n", pkg.Skill, pkg.Key, pkg.Path)
		}
		for _, hint := range result.UploadHints {
			fmt.Fprintf(stdout, "upload: %s\n", hint)
		}
		return 0
	default:
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "unknown registry subcommand", map[string]any{"subcommand": args[0], "supported_subcommands": registrySubcommands()}))
	}
}

func registrySubcommands() []string {
	return []string{"sources", "refresh", "validate", "status", "demo-release"}
}

func registryStatusFlags() []string {
	return []string{"--json", "--file", "--platforms", "--accounts"}
}

func registryDemoReleaseFlags() []string {
	return []string{"--json", "--out", "--base-url", "--version", "--package-prefix", "--skills", "--platforms", "--accounts"}
}

func runCommerce(ctx context.Context, service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut := wantsJSONOutput(args)
	if len(args) == 0 || onlyJSONFlag(args) {
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "commerce subcommand is required", map[string]any{"supported_subcommands": commerceSubcommands()}))
	}
	switch args[0] {
	case "packs":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(commercePackFlags())
		packID := takeStringFlag(&rest, "--pack-id", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commercePackFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce packs arguments", rest, commercePackFlags()))
		}
		packs, err := service.ListCapabilityPacks()
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if strings.TrimSpace(packID) != "" {
			pack, err := service.GetCapabilityPack(packID)
			if err != nil {
				return fail(stdout, stderr, jsonOut, err)
			}
			filtered := packs[:0]
			for _, view := range packs {
				if strings.EqualFold(strings.TrimSpace(view.Pack.ID), strings.TrimSpace(pack.Pack.ID)) {
					filtered = append(filtered, view)
				}
			}
			packs = filtered
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(packs, nil), 0)
		}
		printCapabilityPacks(stdout, packs)
		return 0
	case "scenarios":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(commerceScenarioFlags())
		scenarioID := takeStringFlag(&rest, "--scenario-id", "", known)
		packID := takeStringFlag(&rest, "--pack-id", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceScenarioFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce scenarios arguments", rest, commerceScenarioFlags()))
		}
		scenarios, err := capabilityScenariosForCLI(service, scenarioID, packID)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(scenarios, nil), 0)
		}
		printCapabilityScenarios(stdout, scenarios)
		return 0
	case "install-pack":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		yes := takeBoolFlag(&rest, "--yes", "-y")
		planOnly := takeBoolFlag(&rest, "--plan", "")
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("commerce install-pack requires pack id", []string{"pack"}, commerceInstallPackFlags()))
		}
		if planOnly {
			plan, err := service.PlanCapabilityPackInstall(rest[0])
			if err != nil {
				return fail(stdout, stderr, jsonOut, err)
			}
			if jsonOut {
				return writeJSON(stdout, core.NewResponse(plan, plan.Warnings), 0)
			}
			printCapabilityPackPlan(stdout, plan)
			return 0
		}
		if err := confirmMutation("install-pack", []string{rest[0]}, yes, jsonOut, stdin, stdout); err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		result, err := service.InstallCapabilityPack(ctx, rest[0])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		printCapabilityPackInstall(stdout, result)
		return 0
	case "install-scenario":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		yes := takeBoolFlag(&rest, "--yes", "-y")
		planOnly := takeBoolFlag(&rest, "--plan", "")
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("commerce install-scenario requires scenario id", []string{"scenario"}, commerceInstallScenarioFlags()))
		}
		if planOnly {
			plan, err := service.PlanCapabilityScenarioInstall(rest[0])
			if err != nil {
				return fail(stdout, stderr, jsonOut, err)
			}
			if jsonOut {
				return writeJSON(stdout, core.NewResponse(plan, plan.Warnings), 0)
			}
			printCapabilityScenarioPlan(stdout, plan)
			return 0
		}
		if err := confirmMutation("install-scenario", []string{rest[0]}, yes, jsonOut, stdin, stdout); err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		result, err := service.InstallCapabilityScenario(ctx, rest[0])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		printCapabilityScenarioInstall(stdout, result)
		return 0
	case "scenario-ledger":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		options := takeRecordQueryFlags(&rest, commerceScenarioLedgerFlags())
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceScenarioLedgerFlags()))
		}
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("commerce scenario-ledger requires scenario id", []string{"scenario"}, commerceScenarioLedgerFlags()))
		}
		ledger, err := service.CapabilityScenarioLedger(rest[0], options)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(ledger, nil), 0)
		}
		printCapabilityScenarioLedger(stdout, ledger)
		return 0
	case "install-records":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		options := takeRecordQueryFlags(&rest, commerceRecordFlags())
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceRecordFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce install-records arguments", rest, commerceRecordFlags()))
		}
		result, err := service.ListInstallRecordsWithIntegrity(options)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		printInstallRecords(stdout, result.Records)
		printLedgerIntegrity(stdout, result.Integrity)
		return 0
	case "billing-records":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		options := takeRecordQueryFlags(&rest, commerceRecordFlags())
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceRecordFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce billing-records arguments", rest, commerceRecordFlags()))
		}
		records, err := service.ListBillingRecords(options)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(records, nil), 0)
		}
		printBillingRecords(stdout, records)
		printLedgerIntegrity(stdout, records.Integrity)
		return 0
	case "receipts":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		options := takeRecordQueryFlags(&rest, commerceReceiptFlags())
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceReceiptFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce receipts arguments", rest, commerceReceiptFlags()))
		}
		records, err := service.ListCommerceReceipts(options)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(records, nil), 0)
		}
		printCommerceReceipts(stdout, records)
		printLedgerIntegrity(stdout, records.Integrity)
		return 0
	case "integrity":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce integrity arguments", rest, commerceIntegrityFlags()))
		}
		result, err := service.CommerceIntegrity()
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, doctorWarnings(result.Checks)), diagnosticExitCode(result.OK))
		}
		printCommerceIntegrity(stdout, result)
		return diagnosticExitCode(result.OK)
	case "proof":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(commerceProofFlags())
		challenge := takeStringFlag(&rest, "--challenge", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceProofFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce proof arguments", rest, commerceProofFlags()))
		}
		result, err := service.CommerceProof(challenge)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, doctorWarnings(result.Payload.Checks)), diagnosticExitCode(result.Payload.OK))
		}
		printCommerceProof(stdout, result)
		return diagnosticExitCode(result.Payload.OK)
	case "submit-proof":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		yes := takeBoolFlag(&rest, "--yes", "-y")
		known := flagSet(commerceSubmitProofFlags())
		challenge := takeStringFlag(&rest, "--challenge", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceSubmitProofFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce submit-proof arguments", rest, commerceSubmitProofFlags()))
		}
		if err := confirmMutation("submit-proof", []string{challenge}, yes, jsonOut, stdin, stdout); err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		result, err := service.SubmitCommerceProof(ctx, challenge)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		printCommerceReceiptSubmit(stdout, result)
		return 0
	case "snapshot":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(commerceSnapshotFlags())
		outPath := takeStringFlag(&rest, "--out", "", known)
		options := takeRecordQueryFlagsWithKnown(&rest, known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceSnapshotFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce snapshot arguments", rest, commerceSnapshotFlags()))
		}
		if outPath != "" {
			result, err := service.ExportCommerceSnapshot(outPath, options)
			if err != nil {
				return fail(stdout, stderr, jsonOut, err)
			}
			if jsonOut {
				return writeJSON(stdout, core.NewResponse(result, nil), 0)
			}
			fmt.Fprintf(stdout, "commerce snapshot exported: %s\n", result.Path)
			printCommerceSnapshot(stdout, result.Snapshot)
			return 0
		}
		snapshot, err := service.CommerceSnapshot(options)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(snapshot, nil), 0)
		}
		printCommerceSnapshot(stdout, snapshot)
		return 0
	case "serve":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		known := flagSet(commerceServeFlags())
		addr := takeStringFlag(&rest, "--addr", "127.0.0.1:8765", known)
		allowedOrigin := takeStringFlag(&rest, "--allow-origin", "", known)
		if hasInternalInvalidFlag(rest) {
			return fail(stdout, stderr, jsonOut, internalFlagError(rest, commerceServeFlags()))
		}
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected commerce serve arguments", rest, commerceServeFlags()))
		}
		if err := serveCommerceHTTP(ctx, service, addr, allowedOrigin, jsonOut, stdout); err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		return 0
	default:
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "unknown commerce subcommand", map[string]any{"subcommand": args[0], "supported_subcommands": commerceSubcommands()}))
	}
}

func commerceSubcommands() []string {
	return []string{"packs", "scenarios", "install-pack", "install-scenario", "scenario-ledger", "install-records", "billing-records", "receipts", "integrity", "proof", "submit-proof", "snapshot", "serve"}
}

func commercePackFlags() []string {
	return []string{"--json", "--pack-id"}
}

func commerceInstallPackFlags() []string {
	return []string{"--json", "--yes", "-y", "--plan"}
}

func commerceInstallScenarioFlags() []string {
	return []string{"--json", "--yes", "-y", "--plan"}
}

func commerceScenarioFlags() []string {
	return []string{"--json", "--scenario-id", "--pack-id"}
}

func commerceScenarioLedgerFlags() []string {
	return []string{"--json", "--pack-id", "--scenario-id", "--skill", "--status", "--type", "--currency", "--from", "--to", "--limit"}
}

func commerceRecordFlags() []string {
	return []string{"--json", "--pack-id", "--scenario-id", "--skill", "--status", "--type", "--currency", "--from", "--to", "--limit"}
}

func commerceReceiptFlags() []string {
	return []string{"--json", "--status", "--from", "--to", "--limit"}
}

func commerceIntegrityFlags() []string {
	return []string{"--json"}
}

func commerceProofFlags() []string {
	return []string{"--json", "--challenge"}
}

func commerceSubmitProofFlags() []string {
	return []string{"--json", "--challenge", "--yes", "-y"}
}

func commerceSnapshotFlags() []string {
	return []string{"--json", "--pack-id", "--scenario-id", "--skill", "--status", "--type", "--currency", "--from", "--to", "--limit", "--out"}
}

func commerceServeFlags() []string {
	return []string{"--json", "--addr", "--allow-origin"}
}

func takeRecordQueryFlags(args *[]string, flags []string) core.RecordQueryOptions {
	known := flagSet(flags)
	return takeRecordQueryFlagsWithKnown(args, known)
}

func takeRecordQueryFlagsWithKnown(args *[]string, known map[string]bool) core.RecordQueryOptions {
	packID := takeStringFlag(args, "--pack-id", "", known)
	scenarioID := takeStringFlag(args, "--scenario-id", "", known)
	skill := takeStringFlag(args, "--skill", "", known)
	status := takeStringFlag(args, "--status", "", known)
	recordType := takeStringFlag(args, "--type", "", known)
	currency := takeStringFlag(args, "--currency", "", known)
	from := takeStringFlag(args, "--from", "", known)
	to := takeStringFlag(args, "--to", "", known)
	limit := takeIntFlag(args, "--limit", 100, known)
	return core.RecordQueryOptions{PackID: packID, ScenarioID: scenarioID, Skill: skill, Status: status, Type: recordType, Currency: currency, From: from, To: to, Limit: limit}
}

func capabilityScenariosForCLI(service *core.Service, scenarioID, packID string) ([]core.CapabilityScenarioView, error) {
	var scenarios []core.CapabilityScenarioView
	if strings.TrimSpace(scenarioID) != "" {
		scenario, err := service.GetCapabilityScenario(scenarioID)
		if err != nil {
			return nil, err
		}
		scenarios = []core.CapabilityScenarioView{scenario}
	} else {
		var err error
		scenarios, err = service.ListCapabilityScenarios()
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(packID) == "" {
		return scenarios, nil
	}
	pack, err := service.GetCapabilityPack(packID)
	if err != nil {
		return nil, err
	}
	filtered := scenarios[:0]
	for _, scenario := range scenarios {
		if scenarioMatchesPackForCLI(scenario, pack.Pack) {
			filtered = append(filtered, scenario)
		}
	}
	return filtered, nil
}

func scenarioMatchesPackForCLI(scenario core.CapabilityScenarioView, pack core.CapabilityPack) bool {
	if strings.EqualFold(strings.TrimSpace(scenario.RecommendedPack.Pack.ID), strings.TrimSpace(pack.ID)) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(pack.Tier)) {
	case "standard", "advanced":
		return false
	}
	recommended := scenario.RecommendedPack.Pack.SkillNames
	for _, packSkill := range pack.SkillNames {
		for _, recommendedSkill := range recommended {
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

func serveCommerceHTTP(ctx context.Context, service *core.Service, addr, allowedOrigin string, jsonOut bool, stdout io.Writer) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return core.NewError(core.CodeInvalidArgument, "--addr requires a value", map[string]any{"flag": "--addr", "supported_flags": commerceServeFlags()})
	}
	if err := validateCommerceServeAddr(addr); err != nil {
		return err
	}
	if err := core.ValidateCommerceAllowedOrigin(allowedOrigin); err != nil {
		return err
	}
	mutationToken, err := core.NewSecretToken(32)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           service.CommerceHTTPHandler(core.CommerceHTTPOptions{AllowedOrigin: strings.TrimSpace(allowedOrigin), MutationToken: mutationToken}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	defer server.Close()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	actualAddr := listener.Addr().String()
	if jsonOut {
		response := core.NewResponse(map[string]any{
			"addr":            actualAddr,
			"base_url":        "http://" + actualAddr,
			"allow_origin":    allowedOrigin,
			"mutation_header": "X-AGTX-Commerce-Token",
			"mutation_token":  mutationToken,
			"endpoints":       core.CommerceHTTPEndpoints(),
			"dashboard_url":   "http://" + actualAddr + "/commerce",
			"snapshot_url":    "http://" + actualAddr + "/v1/commerce/snapshot",
			"healthcheck_url": "http://" + actualAddr + "/healthz",
		}, nil)
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(response); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "commerce HTTP API listening on http://%s\n", actualAddr)
		fmt.Fprintf(stdout, "dashboard: http://%s/commerce\n", actualAddr)
		fmt.Fprintf(stdout, "snapshot: http://%s/v1/commerce/snapshot\n", actualAddr)
		fmt.Fprintf(stdout, "mutation_token: %s\n", mutationToken)
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func validateCommerceServeAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return core.NewError(core.CodeInvalidArgument, "--addr must be host:port", map[string]any{"flag": "--addr", "addr": addr, "error": err.Error()})
	}
	if strings.TrimSpace(host) == "" {
		return core.NewError(core.CodeInvalidArgument, "--addr must use an explicit loopback host", map[string]any{"flag": "--addr", "addr": addr})
	}
	normalizedHost := strings.ToLower(strings.Trim(host, "[]"))
	if normalizedHost == "localhost" {
		return nil
	}
	ip := net.ParseIP(normalizedHost)
	if ip == nil || !ip.IsLoopback() {
		return core.NewError(core.CodeInvalidArgument, "commerce serve only binds loopback addresses", map[string]any{"flag": "--addr", "addr": addr, "allowed_examples": []string{"127.0.0.1:8765", "[::1]:8765", "localhost:8765"}})
	}
	return nil
}

func runPro(ctx context.Context, service *core.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	jsonOut := wantsJSONOutput(args)
	if len(args) == 0 || onlyJSONFlag(args) {
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "pro subcommand is required", map[string]any{"supported_subcommands": proSubcommands()}))
	}
	switch args[0] {
	case "login":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		openBrowser := takeBoolFlag(&rest, "--open", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected pro login arguments", rest, proLoginFlags()))
		}
		result, err := service.ProLoginStart(ctx)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "Open this URL to sign in:\n%s\n\n", result.LoginURL)
		if openBrowser {
			if err := openURL(result.LoginURL); err != nil {
				fmt.Fprintf(stderr, "warning: could not open browser: %s\n", err)
			}
		}
		fmt.Fprintf(stdout, "If the agtx:// callback is not registered, copy the callback URI and run:\nagtx pro callback <callback-uri>\n")
		return 0
	case "callback":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("pro callback requires callback uri", []string{"callback_uri"}, jsonOnlyFlags()))
		}
		result, err := service.ProCallback(ctx, rest[0])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "Pro login complete for %s (%s)\n", result.DeviceName, result.DeviceID)
		return 0
	case "status":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected pro status arguments", rest, jsonOnlyFlags()))
		}
		result, err := service.ProStatus(ctx)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		if !result.Authenticated {
			fmt.Fprintln(stdout, "Pro: not logged in")
			for _, status := range result.CurrentStatus {
				fmt.Fprintf(stdout, "status: %s\n", status)
			}
			if len(result.RecommendedActions) > 0 {
				fmt.Fprintln(stdout, "next:")
				for _, action := range result.RecommendedActions {
					fmt.Fprintf(stdout, "  - %s: %s\n", action.ID, action.Title)
					if action.Command != "" {
						fmt.Fprintf(stdout, "    command: %s\n", action.Command)
					}
					if action.MCPTool != "" {
						fmt.Fprintf(stdout, "    mcp: %s\n", action.MCPTool)
					}
				}
			}
			return 0
		}
		fmt.Fprintf(stdout, "Pro: logged in\nsubscription: %s\nplan: %s\ndevice: %s (%s)\n", valueOrDash(result.Subscription), valueOrDash(result.Plan), result.DeviceName, result.DeviceID)
		if result.DeviceLimit > 0 {
			fmt.Fprintf(stdout, "device_limit: %d\n", result.DeviceLimit)
		}
		for _, status := range result.CurrentStatus {
			fmt.Fprintf(stdout, "status: %s\n", status)
		}
		if len(result.RecommendedActions) > 0 {
			fmt.Fprintln(stdout, "next:")
			for _, action := range result.RecommendedActions {
				fmt.Fprintf(stdout, "  - %s: %s\n", action.ID, action.Title)
				if action.Command != "" {
					fmt.Fprintf(stdout, "    command: %s\n", action.Command)
				}
				if action.MCPTool != "" {
					fmt.Fprintf(stdout, "    mcp: %s\n", action.MCPTool)
				}
			}
		}
		return 0
	case "setup":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected pro setup arguments", rest, jsonOnlyFlags()))
		}
		result, err := service.ProSetup(ctx)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "authenticated: %t\npending_login: %t\nplatform: %s\n", result.Authenticated, result.HasPendingLogin, result.Platform)
		if result.ProAPIURL != "" {
			fmt.Fprintf(stdout, "pro_api_url: %s\n", result.ProAPIURL)
		}
		if result.RegistryURL != "" {
			fmt.Fprintf(stdout, "registry_url: %s\n", result.RegistryURL)
		}
		for _, status := range result.CurrentStatus {
			fmt.Fprintf(stdout, "status: %s\n", status)
		}
		if len(result.RecommendedActions) > 0 {
			fmt.Fprintln(stdout, "next:")
			for _, action := range result.RecommendedActions {
				fmt.Fprintf(stdout, "  - %s: %s\n", action.ID, action.Title)
				if action.Command != "" {
					fmt.Fprintf(stdout, "    command: %s\n", action.Command)
				}
				if action.MCPTool != "" {
					fmt.Fprintf(stdout, "    mcp: %s\n", action.MCPTool)
				}
			}
		}
		return 0
	case "devices":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected pro devices arguments", rest, jsonOnlyFlags()))
		}
		devices, err := service.ProDevices(ctx)
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(devices, nil), 0)
		}
		for _, device := range devices {
			current := ""
			if device.Current {
				current = "\tcurrent"
			}
			fmt.Fprintf(stdout, "%s\t%s%s\n", device.ID, device.Name, current)
		}
		return 0
	case "revoke":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		yes := takeBoolFlag(&rest, "--yes", "-y")
		if len(rest) != 1 {
			return fail(stdout, stderr, jsonOut, argumentCountError("pro revoke requires device id", []string{"device_id"}, proRevokeFlags()))
		}
		if err := confirmMutation("pro-revoke", []string{rest[0]}, yes, jsonOut, stdin, stdout); err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		device, err := service.ProRevokeDevice(ctx, rest[0])
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(device, nil), 0)
		}
		fmt.Fprintf(stdout, "revoked %s\t%s\n", device.ID, device.Name)
		return 0
	case "logout":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected pro logout arguments", rest, jsonOnlyFlags()))
		}
		result, err := service.ProLogout()
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "logged out: %s\n", result.AuthPath)
		return 0
	case "register-scheme":
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) > 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("unexpected pro register-scheme arguments", rest, jsonOnlyFlags()))
		}
		result, err := service.ProRegisterScheme()
		if err != nil {
			return fail(stdout, stderr, jsonOut, err)
		}
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(result, nil), 0)
		}
		fmt.Fprintf(stdout, "%s:// registered: %s\n", result.Scheme, result.Command)
		return 0
	default:
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "unknown pro subcommand", map[string]any{"subcommand": args[0], "supported_subcommands": proSubcommands()}))
	}
}

func proSubcommands() []string {
	return []string{"login", "callback", "status", "setup", "devices", "revoke", "logout", "register-scheme"}
}

func proLoginFlags() []string {
	return []string{"--json", "--open"}
}

func proRevokeFlags() []string {
	return []string{"--json", "--yes", "-y"}
}

func openURL(rawURL string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

func runAgent(args []string, stdout, stderr io.Writer) int {
	jsonOut := wantsJSONOutput(args)
	if len(args) < 1 || onlyJSONFlag(args) {
		if !jsonOut {
			printAgentUsage(stderr)
			return 1
		}
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "agent subcommand is required", map[string]any{"supported_subcommands": agentSubcommands()}))
	}
	if args[0] == "targets" {
		rest := args[1:]
		jsonOut := takeBoolFlag(&rest, "--json", "")
		if len(rest) != 0 {
			return fail(stdout, stderr, jsonOut, unexpectedArgumentsError("agent targets does not accept positional arguments", rest, jsonOnlyFlags()))
		}
		targets := agentTargets()
		if jsonOut {
			return writeJSON(stdout, core.NewResponse(targets, nil), 0)
		}
		for _, target := range targets {
			fmt.Fprintf(stdout, "%s\t%s\n", target.Target, target.Summary)
		}
		return 0
	}
	if args[0] != "init" {
		if !jsonOut {
			printAgentUsage(stderr)
			return 1
		}
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "unknown agent subcommand", map[string]any{"subcommand": args[0], "supported_subcommands": agentSubcommands()}))
	}
	args = args[1:]
	printOnly := takeBoolFlag(&args, "--print", "")
	jsonOut = takeBoolFlag(&args, "--json", "")
	if printOnly && jsonOut {
		return fail(stdout, stderr, jsonOut, mutuallyExclusiveFlagsError("--print", "--json", agentInitFlags()))
	}
	if len(args) != 1 {
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, "agent init requires exactly one target", map[string]any{
			"expected_args":     []string{"target"},
			"supported_flags":   agentInitFlags(),
			"supported_targets": supportedAgentTargets(),
		}))
	}
	if !printOnly && !jsonOut {
		fmt.Fprintln(stderr, "agtx does not modify agent configs automatically; rerun with --print or --json")
		return 1
	}
	info, err := agentSnippet(args[0])
	if err != nil {
		return fail(stdout, stderr, jsonOut, core.NewError(core.CodeInvalidArgument, err.Error(), map[string]any{"supported_targets": supportedAgentTargets()}))
	}
	if jsonOut {
		return writeJSON(stdout, core.NewResponse(info, nil), 0)
	}
	fmt.Fprint(stdout, renderAgentSnippet(info))
	return 0
}

func agentSubcommands() []string {
	return []string{"init", "targets"}
}

func agentInitFlags() []string {
	return []string{"--print", "--json"}
}

func printAgentUsage(stderr io.Writer) {
	fmt.Fprintf(stderr, "usage: agtx agent init <%s> [--print|--json]\n", strings.Join(supportedAgentTargets(), "|"))
	fmt.Fprintln(stderr, "       agtx agent targets [--json]")
}

func jsonOnlyFlags() []string {
	return []string{"--json"}
}

func flagSet(flags []string) map[string]bool {
	set := make(map[string]bool, len(flags))
	for _, flag := range flags {
		set[flag] = true
	}
	return set
}

func unexpectedArgumentsError(message string, args, supportedFlags []string) error {
	return core.NewError(core.CodeInvalidArgument, message, map[string]any{
		"args":            args,
		"supported_flags": supportedFlags,
	})
}

func argumentCountError(message string, expectedArgs, supportedFlags []string) error {
	return core.NewError(core.CodeInvalidArgument, message, map[string]any{
		"expected_args":   expectedArgs,
		"supported_flags": supportedFlags,
	})
}

func mutuallyExclusiveFlagsError(left, right string, supportedFlags []string) error {
	return core.NewError(core.CodeInvalidArgument, left+" and "+right+" are mutually exclusive", map[string]any{
		"flags":           []string{left, right},
		"supported_flags": supportedFlags,
	})
}

func confirmMutation(action string, targets []string, yes, jsonOut bool, stdin io.Reader, stdout io.Writer) error {
	if yes {
		return nil
	}
	if jsonOut || !isTerminal(stdin) {
		return core.NewError(core.CodeConfirmationRequired, action+" requires explicit confirmation", map[string]any{
			"action":          action,
			"targets":         targets,
			"expected":        "--yes",
			"retry_with":      "--yes",
			"supported_flags": mutationFlags(action),
		})
	}
	fmt.Fprintf(stdout, "%s %s? [y/N] ", action, strings.Join(targets, ", "))
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "y" || line == "yes" {
		return nil
	}
	return core.NewError(core.CodeConfirmationRequired, action+" cancelled", map[string]any{"action": action})
}

func mutationFlags(action string) []string {
	switch action {
	case "install":
		return installFlags()
	case "uninstall":
		return uninstallFlags()
	case "upgrade":
		return upgradeFlags()
	case "rollback":
		return rollbackFlags()
	case "install-pack":
		return commerceInstallPackFlags()
	case "install-scenario":
		return commerceInstallScenarioFlags()
	case "submit-proof":
		return commerceSubmitProofFlags()
	case "pro-revoke":
		return proRevokeFlags()
	default:
		return []string{"--json", "--yes", "-y"}
	}
}

func fail(stdout, stderr io.Writer, jsonOut bool, err error) int {
	if jsonOut {
		code := 1
		if core.IsErrorCode(err, core.CodeConfirmationRequired) {
			code = 2
		}
		return writeJSON(stdout, core.NewErrorResponse(err, nil), code)
	}
	fmt.Fprintln(stderr, core.ErrorFrom(err).Message)
	if core.IsErrorCode(err, core.CodeConfirmationRequired) {
		return 2
	}
	return 1
}

func failAgent(stdout, stderr io.Writer, jsonOut, ndjsonOut bool, err error) int {
	if ndjsonOut {
		writeEvent(stdout, "failed", map[string]any{"error": core.ErrorFrom(err)})
		if core.IsErrorCode(err, core.CodeConfirmationRequired) {
			return 2
		}
		return 1
	}
	return fail(stdout, stderr, jsonOut, err)
}

func failRun(stdout, stderr io.Writer, jsonOut, ndjsonOut bool, err error, result core.RunResult) int {
	if ndjsonOut {
		writeEvent(stdout, "failed", map[string]any{"error": core.ErrorFrom(err), "result": result})
		if core.IsErrorCode(err, core.CodeConfirmationRequired) {
			return 2
		}
		return 1
	}
	return fail(stdout, stderr, jsonOut, err)
}

func writeJSON(stdout io.Writer, response core.Response, exitCode int) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return 1
	}
	return exitCode
}

func writeEvent(stdout io.Writer, event string, data any) {
	payload := map[string]any{
		"event":    event,
		"data":     data,
		"trace_id": core.NewTraceID(),
		"time":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	bytes, _ := json.Marshal(payload)
	fmt.Fprintln(stdout, string(bytes))
}

func printPlan(stdout io.Writer, plan core.MutationPlan) {
	fmt.Fprintf(stdout, "%s plan:\n", plan.Action)
	for _, change := range plan.Changes {
		fmt.Fprintf(stdout, "  %s\t%s", change.Name, change.Status)
		if change.CurrentVersion != "" || change.TargetVersion != "" {
			fmt.Fprintf(stdout, "\t%s -> %s", valueOrDash(change.CurrentVersion), valueOrDash(change.TargetVersion))
		}
		if change.Stub {
			fmt.Fprint(stdout, "\tstub")
		}
		if len(change.Permissions) > 0 {
			fmt.Fprintf(stdout, "\tpermissions=%s", strings.Join(change.Permissions, ","))
		}
		if change.Commerce != nil {
			if change.Commerce.VendorID != "" {
				fmt.Fprintf(stdout, "\tvendor=%s", change.Commerce.VendorID)
			}
			if change.Commerce.CapabilityClass != "" {
				fmt.Fprintf(stdout, "\tclass=%s", change.Commerce.CapabilityClass)
			}
			if len(change.Commerce.BillingMeters) > 0 {
				fmt.Fprintf(stdout, "\tmeters=%s", strings.Join(change.Commerce.BillingMeters, ","))
			}
			if len(change.Commerce.AttributionEvents) > 0 {
				fmt.Fprintf(stdout, "\tattribution=%s", strings.Join(change.Commerce.AttributionEvents, ","))
			}
		}
		fmt.Fprintln(stdout)
	}
}

func printRegistryImplementationStatus(stdout io.Writer, status core.RegistryImplementationStatus) {
	if status.Source != "" {
		fmt.Fprintf(stdout, "registry: %s\n", status.Source)
	}
	fmt.Fprintf(stdout, "skills: %d\nimplemented: %d\npartial: %d\nstub: %d\nincomplete: %d\n", status.Total, status.Implemented, status.Partial, status.Stub, status.Incomplete)
	if len(status.RequestedPlatforms) > 0 {
		fmt.Fprintf(stdout, "platforms: %s\n", strings.Join(status.RequestedPlatforms, ","))
	}
	if len(status.AccountModes) > 0 {
		fmt.Fprintf(stdout, "accounts: %s\n", strings.Join(status.AccountModes, ","))
	}
	for _, coverage := range status.PlatformCoverage {
		fmt.Fprintf(stdout, "platform\t%s\timplemented=%d\tstub=%d\tincomplete=%d\tmissing=%d\n", coverage.Platform, coverage.Implemented, coverage.Stub, coverage.Incomplete, coverage.Missing)
	}
	for _, skill := range status.Skills {
		fmt.Fprintf(stdout, "%s\t%s\t%s", skill.Name, skill.Version, skill.Status)
		if len(skill.RunnablePlatforms) > 0 {
			fmt.Fprintf(stdout, "\trunnable=%s", strings.Join(skill.RunnablePlatforms, ","))
		}
		if len(skill.MissingPlatforms) > 0 {
			fmt.Fprintf(stdout, "\tmissing_platforms=%s", strings.Join(skill.MissingPlatforms, ","))
		}
		fmt.Fprintln(stdout)
	}
}

func printCapabilityPacks(stdout io.Writer, packs []core.CapabilityPackView) {
	if len(packs) == 0 {
		fmt.Fprintln(stdout, "No capability packs found.")
		return
	}
	for _, view := range packs {
		status := "available"
		if view.Installed {
			status = "installed"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s", view.Pack.ID, view.Pack.Tier, status, view.Pack.Summary)
		if view.InstalledAt != "" {
			fmt.Fprintf(stdout, "\tinstalled_at=%s", view.InstalledAt)
		}
		fmt.Fprintln(stdout)
		for _, skill := range view.Skills {
			skillStatus := "missing"
			version := valueOrDash(skill.AvailableVersion)
			if skill.Installed {
				skillStatus = "installed"
				version = valueOrDash(skill.InstalledVersion)
			}
			fmt.Fprintf(stdout, "  %s\t%s\t%s\n", skill.Name, version, skillStatus)
		}
	}
}

func printCapabilityScenarios(stdout io.Writer, scenarios []core.CapabilityScenarioView) {
	if len(scenarios) == 0 {
		fmt.Fprintln(stdout, "No capability scenarios found.")
		return
	}
	for _, view := range scenarios {
		status := "ready"
		if !view.Ready {
			status = "needs_install"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\tpack=%s\tmissing=%d\t%s\n", view.Scenario.ID, view.Scenario.Industry, status, view.RecommendedPack.Pack.ID, len(view.MissingSkills), view.Scenario.Summary)
		for _, input := range view.Scenario.Inputs {
			required := "optional"
			if input.Required {
				required = "required"
			}
			fmt.Fprintf(stdout, "  input\t%s\t%s", input.ID, required)
			if len(input.Formats) > 0 {
				fmt.Fprintf(stdout, "\tformats=%s", strings.Join(input.Formats, ","))
			}
			fmt.Fprintf(stdout, "\t%s\n", input.Label)
		}
		for _, deliverable := range view.Scenario.Deliverables {
			required := "optional"
			if deliverable.Required {
				required = "required"
			}
			fmt.Fprintf(stdout, "  deliverable\t%s\t%s", deliverable.ID, required)
			if len(deliverable.Formats) > 0 {
				fmt.Fprintf(stdout, "\tformats=%s", strings.Join(deliverable.Formats, ","))
			}
			fmt.Fprintf(stdout, "\t%s\n", deliverable.Label)
		}
		for _, step := range view.Scenario.Workflow {
			fmt.Fprintf(stdout, "  step\t%s\t%s\t%s", step.ID, step.Stage, step.Title)
			if len(step.Skills) > 0 {
				fmt.Fprintf(stdout, "\tskills=%s", strings.Join(step.Skills, ","))
			}
			fmt.Fprintln(stdout)
		}
		for _, skill := range view.Scenario.Skills {
			line := fmt.Sprintf("  %s\t%s\t%s\t%s", skill.Name, skill.Priority, skill.Role, skill.Stage)
			if skill.Condition != "" {
				line += "\tif=" + skill.Condition
			}
			fmt.Fprintln(stdout, line)
		}
		for _, criterion := range view.Scenario.AcceptanceCriteria {
			fmt.Fprintf(stdout, "  acceptance\t%s\n", criterion)
		}
		for _, total := range view.BillingPreviewTotals {
			fmt.Fprintf(stdout, "  billing_preview\t%s\t%d records\t%d\n", total.Currency, total.Records, total.GrossAmountMinor)
		}
		for _, warning := range view.Warnings {
			fmt.Fprintf(stdout, "  warning\t%s\n", warning)
		}
	}
}

func printCapabilityPackInstall(stdout io.Writer, result core.CapabilityPackInstallResult) {
	fmt.Fprintf(stdout, "%s %s installed=%t\n", result.Pack.Pack.ID, result.Pack.Pack.Tier, result.Pack.Installed)
	if result.InstallRecord != nil && result.InstallRecord.ScenarioID != "" {
		fmt.Fprintf(stdout, "  install_record\t%s\t%s\tscenario=%s\n", result.InstallRecord.Action, result.InstallRecord.Status, result.InstallRecord.ScenarioID)
	}
	for _, item := range result.Results {
		fmt.Fprintf(stdout, "  %s\t%s\t%s\n", item.Name, item.Version, item.Status)
	}
	for _, record := range result.BillingRecords {
		fmt.Fprintf(stdout, "  billing\t%s\t%s\t%s\t%d", record.Type, record.Meter, valueOrDash(record.Currency), record.GrossAmountMinor)
		if record.ScenarioID != "" {
			fmt.Fprintf(stdout, "\tscenario=%s", record.ScenarioID)
		}
		fmt.Fprintln(stdout)
	}
}

func printCapabilityPackPlan(stdout io.Writer, plan core.CapabilityPackInstallPlan) {
	fmt.Fprintf(stdout, "%s %s plan:\n", plan.Pack.Pack.ID, plan.Pack.Pack.Tier)
	for _, change := range plan.Changes {
		fmt.Fprintf(stdout, "  %s\t%s\t%s -> %s\n", change.Name, change.Status, valueOrDash(change.CurrentVersion), valueOrDash(change.TargetVersion))
	}
	for _, record := range plan.BillingPreview {
		fmt.Fprintf(stdout, "  billing_preview\t%s\t%s\t%s\t%d\n", record.Type, record.Meter, valueOrDash(record.Currency), record.GrossAmountMinor)
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(stdout, "  warning\t%s\n", warning)
	}
}

func printCapabilityScenarioPlan(stdout io.Writer, plan core.CapabilityScenarioInstallPlan) {
	fmt.Fprintf(stdout, "%s plan:\n", plan.Scenario.Scenario.ID)
	fmt.Fprintf(stdout, "  scenario\t%s\t%s\n", plan.Scenario.Scenario.Name, plan.Scenario.Scenario.Industry)
	fmt.Fprintf(stdout, "  pack\t%s\t%s\tready=%t\n", plan.PackPlan.Pack.Pack.ID, plan.PackPlan.Pack.Pack.Tier, plan.Scenario.Ready)
	for _, change := range plan.PackPlan.Changes {
		fmt.Fprintf(stdout, "  %s\t%s\t%s -> %s\n", change.Name, change.Status, valueOrDash(change.CurrentVersion), valueOrDash(change.TargetVersion))
	}
	for _, record := range plan.PackPlan.BillingPreview {
		fmt.Fprintf(stdout, "  billing_preview\t%s\t%s\t%s\t%d", record.Type, record.Meter, valueOrDash(record.Currency), record.GrossAmountMinor)
		if record.ScenarioID != "" {
			fmt.Fprintf(stdout, "\tscenario=%s", record.ScenarioID)
		}
		fmt.Fprintln(stdout)
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(stdout, "  warning\t%s\n", warning)
	}
}

func printCapabilityScenarioInstall(stdout io.Writer, result core.CapabilityScenarioInstallResult) {
	fmt.Fprintf(stdout, "%s installed pack=%s ready=%t\n", result.Scenario.Scenario.ID, result.PackInstall.Pack.Pack.ID, result.Scenario.Ready)
	if result.PackInstall.InstallRecord != nil {
		record := result.PackInstall.InstallRecord
		fmt.Fprintf(stdout, "  install_record\t%s\t%s\tpack=%s", record.Action, record.Status, record.PackID)
		if record.ScenarioID != "" {
			fmt.Fprintf(stdout, "\tscenario=%s", record.ScenarioID)
		}
		fmt.Fprintln(stdout)
	}
	for _, item := range result.PackInstall.Results {
		fmt.Fprintf(stdout, "  %s\t%s\t%s\n", item.Name, item.Version, item.Status)
	}
	for _, record := range result.PackInstall.BillingRecords {
		fmt.Fprintf(stdout, "  billing\t%s\t%s\t%s\t%d", record.Type, record.Meter, valueOrDash(record.Currency), record.GrossAmountMinor)
		if record.ScenarioID != "" {
			fmt.Fprintf(stdout, "\tscenario=%s", record.ScenarioID)
		}
		fmt.Fprintln(stdout)
	}
}

func printCapabilityScenarioLedger(stdout io.Writer, ledger core.CapabilityScenarioLedger) {
	fmt.Fprintf(stdout, "%s ledger %s\n", ledger.Scenario.Scenario.ID, ledger.GeneratedAt)
	fmt.Fprintf(stdout, "pack: %s\tready=%t\n", ledger.Scenario.RecommendedPack.Pack.ID, ledger.Scenario.Ready)
	if ledger.LatestInstall != nil {
		fmt.Fprintf(stdout, "latest_install: %s\t%s\t%s\n", ledger.LatestInstall.OccurredAt, ledger.LatestInstall.Action, ledger.LatestInstall.Status)
	} else {
		fmt.Fprintln(stdout, "latest_install: -")
	}
	fmt.Fprintf(stdout, "install_records: %d\n", len(ledger.InstallRecords))
	fmt.Fprintf(stdout, "billing_records: %d\n", len(ledger.Billing.Records))
	fmt.Fprintf(stdout, "pack_install_records: %d\n", len(ledger.PackInstallRecords))
	fmt.Fprintf(stdout, "usage_records: %d\n", len(ledger.UsageRecords))
	for _, total := range ledger.Billing.Totals {
		fmt.Fprintf(stdout, "billing_total: %s\t%d records\t%d\n", total.Currency, total.Records, total.GrossAmountMinor)
	}
}

func printInstallRecords(stdout io.Writer, records []core.InstallRecord) {
	if len(records) == 0 {
		fmt.Fprintln(stdout, "No install records found.")
		return
	}
	for _, record := range records {
		target := record.PackID
		if target == "" {
			target = record.SkillName
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s", record.OccurredAt, record.Action, valueOrDash(target), record.Status)
		if record.ScenarioID != "" {
			fmt.Fprintf(stdout, "\tscenario=%s", record.ScenarioID)
		}
		fmt.Fprintln(stdout)
		for _, skill := range record.Skills {
			fmt.Fprintf(stdout, "  %s\t%s\t%s\n", skill.Name, valueOrDash(skill.Version), skill.Status)
		}
	}
}

func printBillingRecords(stdout io.Writer, result core.BillingRecordListResult) {
	if len(result.Records) == 0 {
		fmt.Fprintln(stdout, "No billing records found.")
		return
	}
	for _, record := range result.Records {
		target := record.PackID
		if record.SkillName != "" {
			target = record.SkillName
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%.4g\t%s\t%d\t%s", record.OccurredAt, record.Type, valueOrDash(target), record.Meter, record.Quantity, valueOrDash(record.Currency), record.GrossAmountMinor, record.Status)
		if record.ScenarioID != "" {
			fmt.Fprintf(stdout, "\tscenario=%s", record.ScenarioID)
		}
		fmt.Fprintln(stdout)
	}
	if len(result.Totals) > 0 {
		fmt.Fprintln(stdout, "Totals:")
		for _, total := range result.Totals {
			fmt.Fprintf(stdout, "  %s\t%d records\t%d\n", total.Currency, total.Records, total.GrossAmountMinor)
		}
	}
}

func printCommerceReceipts(stdout io.Writer, result core.CommerceReceiptListResult) {
	if len(result.Records) == 0 {
		fmt.Fprintln(stdout, "No commerce receipts found.")
		return
	}
	for _, receipt := range result.Records {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", receipt.ReceivedAt, receipt.ReceiptID, receipt.Status, receipt.ProofPayloadHash, valueOrDash(receipt.ServerLedgerID))
	}
}

func printLedgerIntegrity(stdout io.Writer, integrity *core.LedgerIntegritySummary) {
	if integrity == nil {
		return
	}
	fmt.Fprintf(stdout, "integrity: %s\tledger=%s\tverified=%d\tfailed=%d\tlegacy_unsigned=%d\tanchors=%d\tanchor_matched=%t\n", integrity.Status, integrity.Ledger, integrity.Verified, integrity.Failed, integrity.LegacyUnsigned, integrity.Anchors, integrity.AnchorMatched)
	if integrity.Reason != "" {
		fmt.Fprintf(stdout, "integrity_reason: %s\n", integrity.Reason)
	}
}

func printCommerceSnapshot(stdout io.Writer, snapshot core.CapabilityCommerceSnapshot) {
	fmt.Fprintf(stdout, "commerce snapshot %s\n", snapshot.GeneratedAt)
	fmt.Fprintf(stdout, "packs: %d\n", len(snapshot.Packs))
	fmt.Fprintf(stdout, "install_records: %d\n", len(snapshot.InstallRecords.Records))
	fmt.Fprintf(stdout, "billing_records: %d\n", len(snapshot.Billing.Records))
	fmt.Fprintf(stdout, "receipts: %d\n", len(snapshot.Receipts.Records))
	for _, total := range snapshot.Billing.Totals {
		fmt.Fprintf(stdout, "billing_total: %s\t%d records\t%d\n", total.Currency, total.Records, total.GrossAmountMinor)
	}
	for _, integrity := range snapshot.Integrity {
		item := integrity
		printLedgerIntegrity(stdout, &item)
	}
}

func printCommerceIntegrity(stdout io.Writer, result core.CommerceIntegrityResult) {
	status := "ok"
	if !result.OK {
		status = "error"
	}
	fmt.Fprintf(stdout, "commerce integrity %s\t%s\n", result.GeneratedAt, status)
	for _, integrity := range result.Ledgers {
		item := integrity
		printLedgerIntegrity(stdout, &item)
	}
	fmt.Fprintf(stdout, "summary: %d checks, %d warnings, %d errors\n", result.Summary.Checks, result.Summary.Warnings, result.Summary.Errors)
}

func printCommerceProof(stdout io.Writer, proof core.CommerceProof) {
	status := "ok"
	if !proof.Payload.OK {
		status = "error"
	}
	fmt.Fprintf(stdout, "commerce proof %s\t%s\n", proof.GeneratedAt, status)
	fmt.Fprintf(stdout, "challenge: %s\n", proof.Challenge)
	fmt.Fprintf(stdout, "subject: %s\n", proof.Subject)
	fmt.Fprintf(stdout, "trust_level: %s\n", proof.TrustLevel)
	fmt.Fprintf(stdout, "receipt_status: %s\n", proof.ReceiptStatus)
	fmt.Fprintf(stdout, "algorithm: %s\n", proof.Algorithm)
	fmt.Fprintf(stdout, "key_id: %s\n", proof.KeyID)
	fmt.Fprintf(stdout, "payload_hash: %s\n", proof.PayloadHash)
	fmt.Fprintf(stdout, "signature: %s\n", proof.Signature)
	fmt.Fprintf(stdout, "summary: %d checks, %d warnings, %d errors\n", proof.Payload.Summary.Checks, proof.Payload.Summary.Warnings, proof.Payload.Summary.Errors)
}

func printCommerceReceiptSubmit(stdout io.Writer, result core.CommerceReceiptSubmitResult) {
	status := "verified"
	if !result.Verification.OK {
		status = "unverified"
	}
	fmt.Fprintf(stdout, "commerce receipt %s\t%s\n", result.SubmittedAt, status)
	fmt.Fprintf(stdout, "challenge: %s\n", result.Proof.Challenge)
	fmt.Fprintf(stdout, "receipt_id: %s\n", result.Receipt.ReceiptID)
	fmt.Fprintf(stdout, "receipt_status: %s\n", result.Receipt.Status)
	fmt.Fprintf(stdout, "received_at: %s\n", result.Receipt.ReceivedAt)
	fmt.Fprintf(stdout, "payload_hash: %s\n", result.Receipt.ProofPayloadHash)
	fmt.Fprintf(stdout, "server_ledger: %s\n", valueOrDash(result.Receipt.ServerLedgerID))
	if result.Verification.Reason != "" {
		fmt.Fprintf(stdout, "verification_reason: %s\n", result.Verification.Reason)
	}
}

func printDiagnostics(stdout io.Writer, checks []core.DoctorCheck) {
	for _, check := range checks {
		status := "ok"
		if check.Severity == "warning" {
			status = "warn"
		}
		if !check.OK || check.Severity == "error" {
			status = "error"
		}
		if check.Path != "" {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", status, check.Name, check.Message, check.Path)
		} else {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", status, check.Name, check.Message)
		}
	}
}

func doctorWarnings(checks []core.DoctorCheck) []string {
	warnings := []string{}
	for _, check := range checks {
		if check.Severity == "warning" {
			warnings = append(warnings, check.Name+": "+check.Message)
		}
	}
	return warnings
}

func diagnosticExitCode(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func configDefaultString(value any) string {
	switch typed := value.(type) {
	case nil:
		return "-"
	case string:
		return valueOrDash(typed)
	case []string:
		if len(typed) == 0 {
			return "-"
		}
		return strings.Join(typed, ",")
	default:
		return fmt.Sprint(typed)
	}
}

func readInput(stdin io.Reader, path string, limit int64) ([]byte, error) {
	switch path {
	case "":
		return nil, nil
	case "-":
		return readAllLimited(stdin, limit, "input")
	default:
		return readFileLimited(path, limit, "input")
	}
}

func readAllLimited(reader io.Reader, limit int64, label string) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(reader)
	}
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, core.NewError(core.CodeSizeLimitExceeded, label+" exceeds configured size limit", map[string]any{"size": len(data), "limit": limit})
	}
	return data, nil
}

func readFileLimited(path string, limit int64, label string) ([]byte, error) {
	if limit <= 0 {
		return os.ReadFile(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, core.NewError(core.CodeSizeLimitExceeded, label+" exceeds configured size limit", map[string]any{"path": path, "size": info.Size(), "limit": limit})
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readAllLimited(file, limit, label)
}

func takeBoolFlag(args *[]string, long, short string) bool {
	values := *args
	next := values[:0]
	found := false
	for _, arg := range values {
		if arg == long || short != "" && arg == short {
			found = true
			continue
		}
		next = append(next, arg)
	}
	*args = next
	return found
}

func takeStringFlag(args *[]string, name, fallback string, knownFlags map[string]bool) string {
	values := *args
	next := values[:0]
	result := fallback
	for i := 0; i < len(values); i++ {
		arg := values[i]
		if arg == name {
			if i+1 < len(values) {
				nextValue := values[i+1]
				if isKnownFlagToken(nextValue, knownFlags) {
					next = append(next, "__missing_"+name)
					continue
				}
				result = nextValue
				i++
			} else {
				next = append(next, "__missing_"+name)
			}
			continue
		}
		prefix := name + "="
		if strings.HasPrefix(arg, prefix) {
			result = strings.TrimPrefix(arg, prefix)
			if result == "" {
				next = append(next, "__missing_"+name)
				result = fallback
			}
			continue
		}
		next = append(next, arg)
	}
	*args = next
	return result
}

func splitListFlag(value string) []string {
	var out []string
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func hasInternalInvalidFlag(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "__invalid_") || strings.HasPrefix(arg, "__missing_") {
			return true
		}
	}
	return false
}

func takeIntFlag(args *[]string, name string, fallback int, knownFlags map[string]bool) int {
	hadFlag := hasFlag(*args, name)
	raw := takeStringFlag(args, name, "", knownFlags)
	if raw == "" {
		if hadFlag && !containsInternalFlag(*args, "__missing_"+name) {
			*args = append(*args, "__missing_"+name)
		}
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		if hadFlag {
			*args = append(*args, "__invalid_"+name)
		}
		return fallback
	}
	return value
}

func takeInt64Flag(args *[]string, name string, fallback int64, knownFlags map[string]bool) int64 {
	hadFlag := hasFlag(*args, name)
	raw := takeStringFlag(args, name, "", knownFlags)
	if raw == "" {
		if hadFlag && !containsInternalFlag(*args, "__missing_"+name) {
			*args = append(*args, "__missing_"+name)
		}
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		if hadFlag {
			*args = append(*args, "__invalid_"+name)
		}
		return fallback
	}
	return value
}

func splitArgsAfterDoubleDash(args []string) ([]string, []string) {
	for index, arg := range args {
		if arg == "--" {
			return append([]string{}, args[:index]...), append([]string{}, args[index+1:]...)
		}
	}
	return args, nil
}

func argsBeforeDoubleDash(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[:index]
		}
	}
	return args
}

func isKnownFlagToken(value string, knownFlags map[string]bool) bool {
	if value == "" || value == "-" || !strings.HasPrefix(value, "-") {
		return false
	}
	if knownFlags[value] {
		return true
	}
	for flag := range knownFlags {
		if strings.HasPrefix(value, flag+"=") {
			return true
		}
	}
	return false
}

func containsInternalFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func internalFlagError(args []string, supportedFlags []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "__missing_") {
			flag := strings.TrimPrefix(arg, "__missing_")
			return core.NewError(core.CodeInvalidArgument, flag+" requires a value", map[string]any{
				"flag":            flag,
				"reason":          "missing_value",
				"supported_flags": supportedFlags,
			})
		}
		if strings.HasPrefix(arg, "__invalid_") {
			flag := strings.TrimPrefix(arg, "__invalid_")
			return core.NewError(core.CodeInvalidArgument, flag+" must be a positive integer", map[string]any{
				"flag":            flag,
				"reason":          "invalid_positive_integer",
				"supported_flags": supportedFlags,
			})
		}
	}
	return core.NewError(core.CodeInvalidArgument, "invalid flag value", map[string]any{"supported_flags": supportedFlags})
}

func isTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func wantsJSONOutput(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func wantsNDJSONOutput(args []string) bool {
	for _, arg := range args {
		if arg == "--ndjson" {
			return true
		}
	}
	return false
}

func onlyJSONFlag(args []string) bool {
	if len(args) != 1 {
		return false
	}
	return args[0] == "--json"
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, `agtx manages native AI skills.

Usage:
  agtx search <query> [--json] [--limit N]
  agtx install <skill...> [--plan] [--yes] [--json]
  agtx run <skill> [args...] [--input file|-] [--timeout-ms N] [--output-limit-bytes N] [--json|--ndjson]
  agtx uninstall <skill> [--all-versions] [--plan] [--yes] [--json]
  agtx list [--installed|--available] [--json]
  agtx upgrade [skill...] [--plan] [--yes] [--json]
  agtx rollback <skill> [--to version] [--plan] [--yes] [--json]
  agtx status [--json]
  agtx doctor [--json]
  agtx verify <skill> [--json]
  agtx config init|show|path|keys|set|unset [--json]
  agtx registry sources|refresh|validate [--json]
  agtx registry status [--file path] [--platforms os/arch,csv] [--accounts normal,pro] [--json]
  agtx registry demo-release --out dir [--skills csv|all] [--platforms os/arch,csv] [--accounts normal,pro] [--base-url url] [--json]
  agtx commerce packs [--json]
  agtx commerce scenarios [--scenario-id id] [--pack-id id] [--json]
  agtx commerce install-pack <pack> [--plan] [--yes] [--json]
  agtx commerce install-scenario <scenario> [--plan] [--yes] [--json]
  agtx commerce scenario-ledger <scenario> [--type type] [--limit N] [--json]
  agtx commerce install-records|billing-records [--pack-id id] [--scenario-id id] [--skill name] [--limit N] [--json]
  agtx commerce receipts [--status status] [--from time] [--to time] [--limit N] [--json]
  agtx commerce integrity [--json]
  agtx commerce proof --challenge nonce [--json]
  agtx commerce submit-proof --challenge nonce --yes [--json]
  agtx commerce snapshot [--pack-id id] [--scenario-id id] [--skill name] [--limit N] [--out path] [--json]
  agtx commerce serve [--addr host:port] [--allow-origin origin] [--json]
  agtx pro login [--open] [--json]
  agtx pro callback <agtx://pro/callback?...> [--json]
  agtx pro status|setup|logout|devices|register-scheme [--json]
  agtx pro revoke <device-id> --yes [--json]
  agtx mcp
  agtx agent init <target> [--print|--json]
  agtx agent targets [--json]`)
}
