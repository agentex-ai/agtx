package core

import (
	"os"
	"path/filepath"
	"runtime"
)

type Paths struct {
	ConfigDir   string
	ConfigFile  string
	CacheDir    string
	SkillsDir   string
	LogsDir     string
	RegistryDir string
}

func DefaultPaths() (Paths, error) {
	if root := os.Getenv("AGTX_HOME"); root != "" {
		root = filepath.Clean(root)
		return Paths{
			ConfigDir:   filepath.Join(root, "config"),
			ConfigFile:  filepath.Join(root, "config", "config.json"),
			CacheDir:    filepath.Join(root, "cache"),
			SkillsDir:   filepath.Join(root, "skills"),
			LogsDir:     filepath.Join(root, "logs"),
			RegistryDir: filepath.Join(root, "cache", "registry"),
		}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}

	switch runtime.GOOS {
	case "darwin":
		configDir := filepath.Join(home, "Library", "Application Support", "agtx")
		cacheDir := filepath.Join(home, "Library", "Caches", "agtx")
		return Paths{
			ConfigDir:   configDir,
			ConfigFile:  filepath.Join(configDir, "config.json"),
			CacheDir:    cacheDir,
			SkillsDir:   filepath.Join(configDir, "skills"),
			LogsDir:     filepath.Join(home, "Library", "Logs", "agtx"),
			RegistryDir: filepath.Join(cacheDir, "registry"),
		}, nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		configDir := filepath.Join(appData, "agtx")
		cacheDir := filepath.Join(localAppData, "agtx", "Cache")
		return Paths{
			ConfigDir:   configDir,
			ConfigFile:  filepath.Join(configDir, "config.json"),
			CacheDir:    cacheDir,
			SkillsDir:   filepath.Join(configDir, "skills"),
			LogsDir:     filepath.Join(localAppData, "agtx", "Logs"),
			RegistryDir: filepath.Join(cacheDir, "registry"),
		}, nil
	default:
		configBase := os.Getenv("XDG_CONFIG_HOME")
		if configBase == "" {
			configBase = filepath.Join(home, ".config")
		}
		cacheBase := os.Getenv("XDG_CACHE_HOME")
		if cacheBase == "" {
			cacheBase = filepath.Join(home, ".cache")
		}
		stateBase := os.Getenv("XDG_STATE_HOME")
		if stateBase == "" {
			stateBase = filepath.Join(home, ".local", "state")
		}
		configDir := filepath.Join(configBase, "agtx")
		cacheDir := filepath.Join(cacheBase, "agtx")
		return Paths{
			ConfigDir:   configDir,
			ConfigFile:  filepath.Join(configDir, "config.json"),
			CacheDir:    cacheDir,
			SkillsDir:   filepath.Join(configDir, "skills"),
			LogsDir:     filepath.Join(stateBase, "agtx", "logs"),
			RegistryDir: filepath.Join(cacheDir, "registry"),
		}, nil
	}
}

func PathsForRoot(root string) Paths {
	root = filepath.Clean(root)
	return Paths{
		ConfigDir:   filepath.Join(root, "config"),
		ConfigFile:  filepath.Join(root, "config", "config.json"),
		CacheDir:    filepath.Join(root, "cache"),
		SkillsDir:   filepath.Join(root, "skills"),
		LogsDir:     filepath.Join(root, "logs"),
		RegistryDir: filepath.Join(root, "cache", "registry"),
	}
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.ConfigDir, p.CacheDir, p.SkillsDir, p.LogsDir, p.RegistryDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
