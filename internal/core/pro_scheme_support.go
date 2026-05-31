package core

import (
	"path/filepath"
	"strings"
)

const (
	proSchemeName            = "agtx"
	proSchemeAppDisplayName  = "agtx Callback"
	proSchemeBundleID        = "io.agentex.agtx.callback"
	proSchemeDispatchName    = "agtx-pro-dispatch.sh"
	proSchemeCallbackLogName = "pro-callback.log"
)

func proSchemeCanAutoRegister(goos string) bool {
	switch goos {
	case "windows", "darwin":
		return true
	default:
		return false
	}
}

func proSchemeCommandHint(goos string) string {
	if !proSchemeCanAutoRegister(goos) {
		return ""
	}
	return "agtx pro register-scheme"
}

func proSchemeCallbackCommand(executable string) string {
	return `"` + executable + `" pro callback "%1"`
}

func darwinProSchemeAppPath(paths Paths) string {
	return filepath.Join(paths.ConfigDir, "agtx-callback.app")
}

func darwinProSchemeDispatchPath(appPath string) string {
	return filepath.Join(appPath, "Contents", "Resources", proSchemeDispatchName)
}

func darwinProSchemeLogPath(paths Paths) string {
	return filepath.Join(paths.LogsDir, proSchemeCallbackLogName)
}

func darwinProSchemeAppleScript() string {
	return `on open location callbackURI
	do shell script quoted form of POSIX path of (path to resource "` + proSchemeDispatchName + `") & " " & quoted form of callbackURI
end open location
`
}

func darwinProSchemeDispatchScript(executable, logPath string) string {
	return "#!/bin/sh\nset -eu\nexec " + shellSingleQuote(executable) + " pro callback \"$1\" >> " + shellSingleQuote(logPath) + " 2>&1\n"
}

func darwinProSchemeURLTypesXML() string {
	return `<array>
<dict>
  <key>CFBundleTypeRole</key>
  <string>Viewer</string>
  <key>CFBundleURLName</key>
  <string>agtx Pro Callback</string>
  <key>CFBundleURLSchemes</key>
  <array>
    <string>` + proSchemeName + `</string>
  </array>
</dict>
</array>`
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
