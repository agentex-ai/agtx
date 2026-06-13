package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultONNXRuntimeVersion = "1.26.0"

type builtinOCRRuntimeDownloadResult struct {
	Runtime        string `json:"runtime"`
	Backend        string `json:"backend"`
	RuntimeVersion string `json:"runtime_version"`
	RuntimeDir     string `json:"runtime_dir"`
	RuntimeLibrary string `json:"runtime_library"`
	URL            string `json:"url"`
	ArchivePath    string `json:"archive_path,omitempty"`
	ArchiveKept    bool   `json:"archive_kept,omitempty"`
	Status         string `json:"status"`
	Bytes          int64  `json:"bytes,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	NoPython       bool   `json:"no_python"`
	DryRun         bool   `json:"dry_run,omitempty"`
}

func (s *Service) downloadBuiltinOCRRuntime(ctx context.Context, options RunOptions) (builtinOCRRuntimeDownloadResult, error) {
	config := s.builtinOCRConfig(options)
	if config.Backend != "onnxruntime" {
		return builtinOCRRuntimeDownloadResult{}, NewError(CodeInvalidArgument, "built-in OCR runtime download supports only onnxruntime", map[string]any{"backend": config.Backend, "supported_backend": "onnxruntime"})
	}
	version := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "runtime-version", os.Getenv("AGTX_OCR_ONNXRUNTIME_VERSION")))), "v")
	if version == "" {
		version = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "runtime_version", ""))), "v")
	}
	if version == "" || version == "auto" {
		version = defaultONNXRuntimeVersion
	}
	runtimeDir := strings.TrimSpace(ocrOptionValue(options.Args, "runtime-dir", os.Getenv("AGTX_OCR_RUNTIME_DIR")))
	if runtimeDir == "" {
		runtimeDir = strings.TrimSpace(ocrOptionValue(options.Args, "runtime_dir", ""))
	}
	if runtimeDir == "" {
		runtimeDir = filepath.Join(config.ModelDir, "runtime")
	}
	archiveName, libraryInArchive, err := onnxRuntimeArchiveInfo(version)
	if err != nil {
		return builtinOCRRuntimeDownloadResult{}, err
	}
	archiveURL := "https://github.com/microsoft/onnxruntime/releases/download/v" + version + "/" + archiveName
	libraryPath := filepath.Join(runtimeDir, nativeLibraryFilename("onnxruntime"))
	result := builtinOCRRuntimeDownloadResult{
		Runtime:        "agtx-native-ocr-v1",
		Backend:        config.Backend,
		RuntimeVersion: version,
		RuntimeDir:     runtimeDir,
		RuntimeLibrary: libraryPath,
		URL:            archiveURL,
		NoPython:       true,
		DryRun:         hasBuiltinOCRDryRunArg(options.Args),
	}
	if result.DryRun {
		result.Status = "planned"
		return result, nil
	}
	if info, err := os.Stat(libraryPath); err == nil && !info.IsDir() && info.Size() > 0 {
		result.Status = "exists"
		result.Bytes = info.Size()
		return result, nil
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return result, err
	}
	archivePath := filepath.Join(runtimeDir, archiveName)
	bytesWritten, digest, err := downloadBuiltinOCRAsset(ctx, builtinOCRAssetSource{Kind: "onnxruntime_archive", URL: archiveURL, Path: archivePath})
	if err != nil {
		return result, err
	}
	result.ArchivePath = archivePath
	result.Bytes = bytesWritten
	result.SHA256 = digest
	if err := extractONNXRuntimeLibrary(archivePath, libraryInArchive, libraryPath); err != nil {
		return result, err
	}
	if hasBuiltinOCRKeepArchiveArg(options.Args) {
		result.ArchiveKept = true
	} else {
		if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
			return result, err
		}
		result.ArchivePath = ""
	}
	result.Status = "downloaded"
	return result, nil
}

func hasBuiltinOCRKeepArchiveArg(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--keep-archive", "keep-archive", "--keep_archive", "keep_archive":
			return true
		}
	}
	return false
}

func onnxRuntimeArchiveInfo(version string) (string, string, error) {
	return onnxRuntimeArchiveInfoFor(runtime.GOOS, runtime.GOARCH, version)
}

func onnxRuntimeArchiveInfoFor(goos, goarch, version string) (string, string, error) {
	if goos == "darwin" && goarch == "amd64" && onnxRuntimeVersionAtLeast(version, "1.24.0") {
		return "", "", NewError(CodePlatformUnsupported, "Microsoft ONNX Runtime CPU archives are not published for macOS Intel at this version", map[string]any{
			"goos":            goos,
			"goarch":          goarch,
			"runtime_version": version,
			"next_actions": []string{
				"set AGTX_OCR_ONNXRUNTIME_LIBRARY to a compatible local libonnxruntime.dylib",
				"use --runtime-version 1.23.2 only if that runtime is compatible with this build",
				"build or install ONNX Runtime locally and point agtx at the shared library",
			},
		})
	}
	platform, err := onnxRuntimePlatform(goos, goarch)
	if err != nil {
		return "", "", err
	}
	base := "onnxruntime-" + platform + "-" + version
	library := nativeLibraryArchiveFilename(goos, "onnxruntime", version)
	if goos == "windows" {
		return base + ".zip", path.Join(base, "lib", library), nil
	}
	return base + ".tgz", path.Join(base, "lib", library), nil
}

func onnxRuntimePlatform(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return "win-x64", nil
	case "windows/arm64":
		return "win-arm64", nil
	case "darwin/amd64":
		return "osx-x86_64", nil
	case "darwin/arm64":
		return "osx-arm64", nil
	case "linux/amd64":
		return "linux-x64", nil
	case "linux/arm64":
		return "linux-aarch64", nil
	default:
		return "", NewError(CodePlatformUnsupported, "ONNX Runtime download does not support this platform", map[string]any{"goos": goos, "goarch": goarch})
	}
}

func nativeLibraryArchiveFilename(goos, name, version string) string {
	switch goos {
	case "windows":
		return name + ".dll"
	case "darwin":
		return "lib" + name + "." + version + ".dylib"
	default:
		return "lib" + name + ".so." + version
	}
}

func onnxRuntimeVersionAtLeast(version, minimum string) bool {
	current, ok := parseONNXRuntimeVersion(version)
	if !ok {
		return false
	}
	required, ok := parseONNXRuntimeVersion(minimum)
	if !ok {
		return false
	}
	for index := range current {
		if current[index] > required[index] {
			return true
		}
		if current[index] < required[index] {
			return false
		}
	}
	return true
}

func parseONNXRuntimeVersion(version string) ([3]int, bool) {
	var parsed [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	if len(parts) != 3 {
		return parsed, false
	}
	for index, part := range parts {
		var value int
		if part == "" {
			return parsed, false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return parsed, false
			}
			value = value*10 + int(ch-'0')
		}
		parsed[index] = value
	}
	return parsed, true
}

func extractONNXRuntimeLibrary(archivePath, memberName, dst string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZipMember(archivePath, memberName, dst)
	}
	return extractTGZMember(archivePath, memberName, dst)
}

func extractZipMember(archivePath, memberName, dst string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if cleanArchiveName(file.Name) != memberName {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		defer src.Close()
		return writeRuntimeLibrary(dst, src)
	}
	return fmt.Errorf("%s not found in %s", memberName, archivePath)
}

func extractTGZMember(archivePath, memberName, dst string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if cleanArchiveName(header.Name) != memberName || header.FileInfo().IsDir() {
			continue
		}
		return writeRuntimeLibrary(dst, reader)
	}
	return fmt.Errorf("%s not found in %s", memberName, archivePath)
}

func writeRuntimeLibrary(dst string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".agtx-onnxruntime-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, dst)
}

func cleanArchiveName(name string) string {
	parsed, err := url.PathUnescape(strings.ReplaceAll(name, "\\", "/"))
	if err == nil {
		name = parsed
	}
	return path.Clean(strings.TrimPrefix(name, "/"))
}
