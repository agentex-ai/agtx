//go:build darwin

package core

import (
	"os/exec"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterProSchemeDarwin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGTX_HOME", root)

	executablePath := filepath.Join(root, "bin", "agtx")
	if err := os.MkdirAll(filepath.Dir(executablePath), 0o755); err != nil {
		t.Fatalf("mkdir executable dir: %v", err)
	}
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}

	var calls [][]string
	restoreExec := proSchemeDarwinExecCommand
	restoreExecutable := proSchemeDarwinExecutable
	proSchemeDarwinExecutable = func() (string, error) {
		return executablePath, nil
	}
	proSchemeDarwinExecCommand = func(name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return helperCommand(t, name, args...)
	}
	defer func() {
		proSchemeDarwinExecCommand = restoreExec
		proSchemeDarwinExecutable = restoreExecutable
	}()

	result, err := registerProScheme()
	if err != nil {
		t.Fatalf("register pro scheme: %v", err)
	}
	if result.Scheme != "agtx" {
		t.Fatalf("expected agtx scheme result: %#v", result)
	}
	appPath := darwinProSchemeAppPath(PathsForRoot(root))
	if result.Command != appPath {
		t.Fatalf("expected app path command, got %#v", result)
	}
	dispatchPath := darwinProSchemeDispatchPath(appPath)
	dispatchBytes, err := os.ReadFile(dispatchPath)
	if err != nil {
		t.Fatalf("read dispatch script: %v", err)
	}
	dispatch := string(dispatchBytes)
	if !strings.Contains(dispatch, executablePath) || !strings.Contains(dispatch, "pro callback \"$1\"") {
		t.Fatalf("expected dispatch script to call agtx callback: %s", dispatch)
	}
	infoBytes, err := os.ReadFile(filepath.Join(appPath, "Contents", "Info.plist"))
	if err != nil {
		t.Fatalf("read info plist: %v", err)
	}
	info := string(infoBytes)
	if !strings.Contains(info, "<string>agtx</string>") || !strings.Contains(info, proSchemeBundleID) {
		t.Fatalf("expected agtx URL type in Info.plist: %s", info)
	}
	if len(calls) != 2 {
		t.Fatalf("expected osacompile and lsregister calls, got %#v", calls)
	}
	if calls[0][0] != "osacompile" || calls[1][0] != "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister" {
		t.Fatalf("unexpected command sequence: %#v", calls)
	}
}

func helperCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	commandArgs := []string{"-test.run=TestHelperProcess", "--", name}
	commandArgs = append(commandArgs, args...)
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	index := 0
	for index < len(args) && args[index] != "--" {
		index++
	}
	if index >= len(args) {
		os.Exit(2)
	}
	command := args[index+1]
	switch command {
	case "osacompile":
		outputIndex := -1
		for i := index + 2; i < len(args)-1; i++ {
			if args[i] == "-o" {
				outputIndex = i + 1
				break
			}
		}
		if outputIndex == -1 {
			os.Exit(2)
		}
		appPath := args[outputIndex]
		scriptPath := filepath.Join(appPath, "Contents", "Resources", "Scripts")
		if err := os.MkdirAll(scriptPath, 0o755); err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(filepath.Join(scriptPath, "main.scpt"), []byte("compiled"), 0o644); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	case "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister":
		os.Exit(0)
	default:
		os.Exit(5)
	}
}
