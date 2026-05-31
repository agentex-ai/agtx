//go:build windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
)

var proSchemeExecCommand = exec.Command

func registerProScheme() (ProSchemeResult, error) {
	executable, err := os.Executable()
	if err != nil {
		return ProSchemeResult{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return ProSchemeResult{}, err
	}
	command := proSchemeCallbackCommand(executable)
	if err := proSchemeExecCommand("reg", "add", `HKCU\Software\Classes\agtx`, "/ve", "/d", "URL:agtx Protocol", "/f").Run(); err != nil {
		return ProSchemeResult{}, err
	}
	if err := proSchemeExecCommand("reg", "add", `HKCU\Software\Classes\agtx`, "/v", "URL Protocol", "/d", "", "/f").Run(); err != nil {
		return ProSchemeResult{}, err
	}
	if err := proSchemeExecCommand("reg", "add", `HKCU\Software\Classes\agtx\shell\open\command`, "/ve", "/d", command, "/f").Run(); err != nil {
		return ProSchemeResult{}, err
	}
	return ProSchemeResult{Scheme: "agtx", Command: command}, nil
}
