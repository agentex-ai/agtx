package core

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
)

const defaultRegistryCache = "registry.json"
const defaultConfigMaxBytes = 1 * 1024 * 1024
const defaultCurrentMaxBytes = 4 * 1024
const defaultManifestMaxBytes = 1 * 1024 * 1024
const defaultRegistryMaxBytes = 16 * 1024 * 1024
const defaultPackageMaxBytes = 256 * 1024 * 1024
const defaultExtractedMaxBytes = 1024 * 1024 * 1024
const defaultExtractedMaxFiles = 8192
const defaultRegistryDownloadTimeoutMS = 30000
const defaultPackageDownloadTimeoutMS = 30000

type Config struct {
	SchemaVersion             int      `json:"schema_version"`
	RegistryURL               string   `json:"registry_url,omitempty"`
	RegistryFiles             []string `json:"registry_files,omitempty"`
	Channel                   string   `json:"channel"`
	Telemetry                 string   `json:"telemetry"`
	LockTimeoutMS             int      `json:"lock_timeout_ms"`
	StaleLockMS               int      `json:"stale_lock_ms"`
	RunTimeoutMS              int      `json:"run_timeout_ms"`
	RunOutputLimitBytes       int64    `json:"run_output_limit_bytes"`
	RegistryMaxBytes          int64    `json:"registry_max_bytes"`
	RegistryDownloadTimeoutMS int      `json:"registry_download_timeout_ms"`
	PackageMaxBytes           int64    `json:"package_max_bytes"`
	PackageDownloadTimeoutMS  int      `json:"package_download_timeout_ms"`
	ExtractedMaxBytes         int64    `json:"extracted_max_bytes"`
	ExtractedMaxFiles         int      `json:"extracted_max_files"`
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion:             1,
		Channel:                   "stable",
		Telemetry:                 "off",
		LockTimeoutMS:             5000,
		StaleLockMS:               600000,
		RunTimeoutMS:              120000,
		RunOutputLimitBytes:       4 * 1024 * 1024,
		RegistryMaxBytes:          defaultRegistryMaxBytes,
		RegistryDownloadTimeoutMS: defaultRegistryDownloadTimeoutMS,
		PackageMaxBytes:           defaultPackageMaxBytes,
		PackageDownloadTimeoutMS:  defaultPackageDownloadTimeoutMS,
		ExtractedMaxBytes:         defaultExtractedMaxBytes,
		ExtractedMaxFiles:         defaultExtractedMaxFiles,
	}
}

func LoadConfig(path string) (Config, error) {
	data, err := readFileLimited(path, defaultConfigMaxBytes, "config")
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), err
	}
	config, err := decodeConfig(data)
	if err != nil {
		return DefaultConfig(), NewError(CodeInvalidArgument, "invalid agtx config", map[string]any{"path": path, "error": err.Error()})
	}
	return config, nil
}

func SaveConfig(path string, config Config) error {
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return err
	}
	config.SchemaVersion = 1
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

func normalizeConfig(config Config) Config {
	if config.SchemaVersion == 0 {
		config.SchemaVersion = 1
	}
	if config.Channel == "" {
		config.Channel = "stable"
	}
	if config.Telemetry == "" {
		config.Telemetry = "off"
	}
	if config.LockTimeoutMS <= 0 {
		config.LockTimeoutMS = 5000
	}
	if config.StaleLockMS <= 0 {
		config.StaleLockMS = 600000
	}
	if config.RunTimeoutMS <= 0 {
		config.RunTimeoutMS = 120000
	}
	if config.RunOutputLimitBytes <= 0 {
		config.RunOutputLimitBytes = 4 * 1024 * 1024
	}
	if config.RegistryMaxBytes <= 0 {
		config.RegistryMaxBytes = defaultRegistryMaxBytes
	}
	if config.RegistryDownloadTimeoutMS <= 0 {
		config.RegistryDownloadTimeoutMS = defaultRegistryDownloadTimeoutMS
	}
	if config.PackageMaxBytes <= 0 {
		config.PackageMaxBytes = defaultPackageMaxBytes
	}
	if config.PackageDownloadTimeoutMS <= 0 {
		config.PackageDownloadTimeoutMS = defaultPackageDownloadTimeoutMS
	}
	if config.ExtractedMaxBytes <= 0 {
		config.ExtractedMaxBytes = defaultExtractedMaxBytes
	}
	if config.ExtractedMaxFiles <= 0 {
		config.ExtractedMaxFiles = defaultExtractedMaxFiles
	}
	slices.Sort(config.RegistryFiles)
	config.RegistryFiles = slices.Compact(config.RegistryFiles)
	return config
}

type configFile struct {
	SchemaVersion             *int      `json:"schema_version"`
	RegistryURL               *string   `json:"registry_url,omitempty"`
	RegistryFiles             *[]string `json:"registry_files,omitempty"`
	Channel                   *string   `json:"channel"`
	Telemetry                 *string   `json:"telemetry"`
	LockTimeoutMS             *int      `json:"lock_timeout_ms"`
	StaleLockMS               *int      `json:"stale_lock_ms"`
	RunTimeoutMS              *int      `json:"run_timeout_ms"`
	RunOutputLimitBytes       *int64    `json:"run_output_limit_bytes"`
	RegistryMaxBytes          *int64    `json:"registry_max_bytes"`
	RegistryDownloadTimeoutMS *int      `json:"registry_download_timeout_ms"`
	PackageMaxBytes           *int64    `json:"package_max_bytes"`
	PackageDownloadTimeoutMS  *int      `json:"package_download_timeout_ms"`
	ExtractedMaxBytes         *int64    `json:"extracted_max_bytes"`
	ExtractedMaxFiles         *int      `json:"extracted_max_files"`
}

func decodeConfig(data []byte) (Config, error) {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(data, &present); err != nil {
		return Config{}, err
	}
	var file configFile
	if err := decodeJSONStrict(data, &file); err != nil {
		return Config{}, err
	}
	config := DefaultConfig()
	if err := rejectNullConfigValues(present); err != nil {
		return Config{}, err
	}
	if file.SchemaVersion != nil {
		config.SchemaVersion = *file.SchemaVersion
	}
	if file.RegistryURL != nil {
		config.RegistryURL = *file.RegistryURL
	}
	if file.RegistryFiles != nil {
		config.RegistryFiles = append([]string(nil), (*file.RegistryFiles)...)
	}
	if file.Channel != nil {
		config.Channel = *file.Channel
	}
	if file.Telemetry != nil {
		config.Telemetry = *file.Telemetry
	}
	if file.LockTimeoutMS != nil {
		config.LockTimeoutMS = *file.LockTimeoutMS
	}
	if file.StaleLockMS != nil {
		config.StaleLockMS = *file.StaleLockMS
	}
	if file.RunTimeoutMS != nil {
		config.RunTimeoutMS = *file.RunTimeoutMS
	}
	if file.RunOutputLimitBytes != nil {
		config.RunOutputLimitBytes = *file.RunOutputLimitBytes
	}
	if file.RegistryMaxBytes != nil {
		config.RegistryMaxBytes = *file.RegistryMaxBytes
	}
	if file.RegistryDownloadTimeoutMS != nil {
		config.RegistryDownloadTimeoutMS = *file.RegistryDownloadTimeoutMS
	}
	if file.PackageMaxBytes != nil {
		config.PackageMaxBytes = *file.PackageMaxBytes
	}
	if file.PackageDownloadTimeoutMS != nil {
		config.PackageDownloadTimeoutMS = *file.PackageDownloadTimeoutMS
	}
	if file.ExtractedMaxBytes != nil {
		config.ExtractedMaxBytes = *file.ExtractedMaxBytes
	}
	if file.ExtractedMaxFiles != nil {
		config.ExtractedMaxFiles = *file.ExtractedMaxFiles
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return normalizeConfig(config), nil
}

func rejectNullConfigValues(values map[string]json.RawMessage) error {
	for key, value := range values {
		if strings.TrimSpace(string(value)) == "null" {
			return NewError(CodeInvalidArgument, "config value cannot be null", map[string]any{"key": key})
		}
	}
	return nil
}

func validateConfig(config Config) error {
	if config.SchemaVersion != 1 {
		return NewError(CodeInvalidArgument, "unsupported config schema_version", map[string]any{"schema_version": config.SchemaVersion})
	}
	if config.RegistryURL != "" {
		if err := validateRegistryURL(config.RegistryURL); err != nil {
			return err
		}
	}
	for _, path := range config.RegistryFiles {
		if strings.TrimSpace(path) == "" {
			return NewError(CodeInvalidArgument, "registry_files entries cannot be empty", nil)
		}
		if strings.TrimSpace(path) != path {
			return NewError(CodeInvalidArgument, "registry_files entries must not contain leading or trailing whitespace", map[string]any{"path": path})
		}
		if strings.ContainsRune(path, 0) {
			return NewError(CodeInvalidArgument, "registry_files entries must not contain NUL bytes", nil)
		}
	}
	if err := validatePathSegment("channel", config.Channel); err != nil {
		return err
	}
	if config.Telemetry != "off" && config.Telemetry != "desensitized" {
		return NewError(CodeInvalidArgument, "telemetry must be off or desensitized", map[string]any{"value": config.Telemetry})
	}
	if config.LockTimeoutMS <= 0 {
		return NewError(CodeInvalidArgument, "lock_timeout_ms must be a positive integer", map[string]any{"value": config.LockTimeoutMS})
	}
	if config.StaleLockMS <= 0 {
		return NewError(CodeInvalidArgument, "stale_lock_ms must be a positive integer", map[string]any{"value": config.StaleLockMS})
	}
	if config.RunTimeoutMS <= 0 {
		return NewError(CodeInvalidArgument, "run_timeout_ms must be a positive integer", map[string]any{"value": config.RunTimeoutMS})
	}
	if config.RunOutputLimitBytes <= 0 {
		return NewError(CodeInvalidArgument, "run_output_limit_bytes must be a positive integer", map[string]any{"value": config.RunOutputLimitBytes})
	}
	if config.RegistryMaxBytes <= 0 {
		return NewError(CodeInvalidArgument, "registry_max_bytes must be a positive integer", map[string]any{"value": config.RegistryMaxBytes})
	}
	if config.RegistryDownloadTimeoutMS <= 0 {
		return NewError(CodeInvalidArgument, "registry_download_timeout_ms must be a positive integer", map[string]any{"value": config.RegistryDownloadTimeoutMS})
	}
	if config.PackageMaxBytes <= 0 {
		return NewError(CodeInvalidArgument, "package_max_bytes must be a positive integer", map[string]any{"value": config.PackageMaxBytes})
	}
	if config.PackageDownloadTimeoutMS <= 0 {
		return NewError(CodeInvalidArgument, "package_download_timeout_ms must be a positive integer", map[string]any{"value": config.PackageDownloadTimeoutMS})
	}
	if config.ExtractedMaxBytes <= 0 {
		return NewError(CodeInvalidArgument, "extracted_max_bytes must be a positive integer", map[string]any{"value": config.ExtractedMaxBytes})
	}
	if config.ExtractedMaxFiles <= 0 {
		return NewError(CodeInvalidArgument, "extracted_max_files must be a positive integer", map[string]any{"value": config.ExtractedMaxFiles})
	}
	return nil
}

func SetConfigValue(config Config, key, value string) (Config, error) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	switch key {
	case "registry_url":
		if value != "" {
			if err := validateRegistryURL(value); err != nil {
				return config, err
			}
		}
		config.RegistryURL = value
	case "registry_files":
		if value == "" {
			config.RegistryFiles = nil
		} else {
			config.RegistryFiles = splitList(value)
		}
	case "channel":
		if value == "" {
			return config, NewError(CodeInvalidArgument, "channel cannot be empty", nil)
		}
		config.Channel = value
	case "telemetry":
		if value != "off" && value != "desensitized" {
			return config, NewError(CodeInvalidArgument, "telemetry must be off or desensitized", map[string]any{"value": value})
		}
		config.Telemetry = value
	case "lock_timeout_ms":
		parsed, err := parsePositiveInt(key, value)
		if err != nil {
			return config, err
		}
		config.LockTimeoutMS = parsed
	case "stale_lock_ms":
		parsed, err := parsePositiveInt(key, value)
		if err != nil {
			return config, err
		}
		config.StaleLockMS = parsed
	case "run_timeout_ms":
		parsed, err := parsePositiveInt(key, value)
		if err != nil {
			return config, err
		}
		config.RunTimeoutMS = parsed
	case "run_output_limit_bytes":
		parsed, err := parsePositiveInt64(key, value)
		if err != nil {
			return config, err
		}
		config.RunOutputLimitBytes = parsed
	case "registry_max_bytes":
		parsed, err := parsePositiveInt64(key, value)
		if err != nil {
			return config, err
		}
		config.RegistryMaxBytes = parsed
	case "registry_download_timeout_ms":
		parsed, err := parsePositiveInt(key, value)
		if err != nil {
			return config, err
		}
		config.RegistryDownloadTimeoutMS = parsed
	case "package_max_bytes":
		parsed, err := parsePositiveInt64(key, value)
		if err != nil {
			return config, err
		}
		config.PackageMaxBytes = parsed
	case "package_download_timeout_ms":
		parsed, err := parsePositiveInt(key, value)
		if err != nil {
			return config, err
		}
		config.PackageDownloadTimeoutMS = parsed
	case "extracted_max_bytes":
		parsed, err := parsePositiveInt64(key, value)
		if err != nil {
			return config, err
		}
		config.ExtractedMaxBytes = parsed
	case "extracted_max_files":
		parsed, err := parsePositiveInt(key, value)
		if err != nil {
			return config, err
		}
		config.ExtractedMaxFiles = parsed
	default:
		return config, NewError(CodeInvalidArgument, "unknown config key", map[string]any{"key": key})
	}
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return config, err
	}
	return config, nil
}

func UnsetConfigValue(config Config, key string) (Config, error) {
	switch strings.TrimSpace(key) {
	case "registry_url":
		config.RegistryURL = ""
	case "registry_files":
		config.RegistryFiles = nil
	case "channel":
		config.Channel = "stable"
	case "telemetry":
		config.Telemetry = "off"
	case "lock_timeout_ms":
		config.LockTimeoutMS = 5000
	case "stale_lock_ms":
		config.StaleLockMS = 600000
	case "run_timeout_ms":
		config.RunTimeoutMS = 120000
	case "run_output_limit_bytes":
		config.RunOutputLimitBytes = 4 * 1024 * 1024
	case "registry_max_bytes":
		config.RegistryMaxBytes = defaultRegistryMaxBytes
	case "registry_download_timeout_ms":
		config.RegistryDownloadTimeoutMS = defaultRegistryDownloadTimeoutMS
	case "package_max_bytes":
		config.PackageMaxBytes = defaultPackageMaxBytes
	case "package_download_timeout_ms":
		config.PackageDownloadTimeoutMS = defaultPackageDownloadTimeoutMS
	case "extracted_max_bytes":
		config.ExtractedMaxBytes = defaultExtractedMaxBytes
	case "extracted_max_files":
		config.ExtractedMaxFiles = defaultExtractedMaxFiles
	default:
		return config, NewError(CodeInvalidArgument, "unknown config key", map[string]any{"key": key})
	}
	config = normalizeConfig(config)
	if err := validateConfig(config); err != nil {
		return config, err
	}
	return config, nil
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parsePositiveInt(key, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, NewError(CodeInvalidArgument, key+" must be a positive integer", map[string]any{"value": value})
	}
	return parsed, nil
}

func parsePositiveInt64(key, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, NewError(CodeInvalidArgument, key+" must be a positive integer", map[string]any{"value": value})
	}
	return parsed, nil
}
