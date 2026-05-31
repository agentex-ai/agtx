//go:build darwin

package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	proSchemeDarwinExecCommand = exec.Command
	proSchemeDarwinExecutable  = os.Executable
)

func registerProScheme() (ProSchemeResult, error) {
	executable, err := proSchemeDarwinExecutable()
	if err != nil {
		return ProSchemeResult{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return ProSchemeResult{}, err
	}
	paths, err := DefaultPaths()
	if err != nil {
		return ProSchemeResult{}, err
	}
	if err := paths.Ensure(); err != nil {
		return ProSchemeResult{}, err
	}
	appPath := darwinProSchemeAppPath(paths)
	if err := os.RemoveAll(appPath); err != nil && !os.IsNotExist(err) {
		return ProSchemeResult{}, err
	}
	cmd := proSchemeDarwinExecCommand("osacompile", "-o", appPath, "-e", darwinProSchemeAppleScript())
	if output, err := cmd.CombinedOutput(); err != nil {
		return ProSchemeResult{}, NewError(CodeInternal, "failed to compile macOS callback applet", map[string]any{"error": err.Error(), "output": string(output)})
	}
	contentsDir := filepath.Join(appPath, "Contents")
	macosDir := filepath.Join(contentsDir, "MacOS")
	resourcesDir := filepath.Join(contentsDir, "Resources")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		return ProSchemeResult{}, err
	}
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		return ProSchemeResult{}, err
	}

	dispatchPath := darwinProSchemeDispatchPath(appPath)
	logPath := darwinProSchemeLogPath(paths)
	if err := os.WriteFile(dispatchPath, []byte(darwinProSchemeDispatchScript(executable, logPath)), 0o755); err != nil {
		return ProSchemeResult{}, err
	}
	infoPlist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleExecutable</key>
  <string>applet</string>
  <key>CFBundleIdentifier</key>
  <string>%s</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>%s</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>1.0</string>
  <key>CFBundleURLTypes</key>
  %s
  <key>CFBundleVersion</key>
  <string>1</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
`, proSchemeBundleID, proSchemeAppDisplayName, darwinProSchemeURLTypesXML())
	if err := os.WriteFile(filepath.Join(contentsDir, "Info.plist"), []byte(infoPlist), 0o644); err != nil {
		return ProSchemeResult{}, err
	}
	registerCmd := proSchemeDarwinExecCommand("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister", "-f", appPath)
	if output, err := registerCmd.CombinedOutput(); err != nil {
		return ProSchemeResult{}, NewError(CodeInternal, "failed to register macOS callback applet", map[string]any{"error": err.Error(), "output": string(output), "app_path": appPath})
	}
	return ProSchemeResult{Scheme: proSchemeName, Command: appPath}, nil
}
