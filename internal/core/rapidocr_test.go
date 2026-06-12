package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFindsRapidOCRV6Aliases(t *testing.T) {
	registry := DefaultRegistry()
	for _, query := range []string{"rapidocr", "ppocrv6", "paddleocr v6"} {
		t.Run(query, func(t *testing.T) {
			results := registry.Search(query, 3)
			if len(results) == 0 {
				t.Fatal("expected search results")
			}
			if results[0].Skill.Name != "ocr" {
				t.Fatalf("expected ocr to rank first for %q, got %#v", query, results)
			}
		})
	}
}

func TestRunRapidOCRWithoutInstallUsesBuiltinNativeProbe(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "rapidocr", []string{"--probe"}, nil)
	if err != nil {
		t.Fatalf("run rapidocr probe: %v result=%#v", err, result)
	}
	if result.Name != "ocr" || result.Version != "0.6.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected built-in probe result: %#v", result)
	}
	if len(result.UsageEvents) != 0 {
		t.Fatalf("OCR probe must not bill usage events: %#v", result.UsageEvents)
	}
	var status struct {
		Runtime       string `json:"runtime"`
		NoPython      bool   `json:"no_python"`
		AdapterLinked bool   `json:"adapter_linked"`
		Available     bool   `json:"available"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		t.Fatalf("decode probe status: %v stdout=%s", err, result.Stdout)
	}
	if status.Runtime != "agtx-native-ocr-v1" || !status.NoPython || status.Available {
		t.Fatalf("unexpected native OCR status: %#v", status)
	}
}

func TestRapidOCRProbeReportsNativeModelOverrides(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	modelDir := t.TempDir()
	result, err := service.RunSkill(context.Background(), "rapidocr", []string{
		"--probe",
		"--model-dir", modelDir,
		"--det-model", "custom-det.onnx",
		"--rec-model", "custom-rec.onnx",
		"--keys", "custom-keys.txt",
		"--det-limit-side-len", "1024",
		"--det-threshold", "0.25",
		"--box-threshold", "0.45",
		"--unclip-ratio", "2.0",
		"--max-candidates", "33",
		"--text-score", "0.6",
		"--rec-width", "640",
		"--rec-height", "48",
	}, nil)
	if err != nil {
		t.Fatalf("run rapidocr probe: %v result=%#v", err, result)
	}
	var status struct {
		NoPython   bool `json:"no_python"`
		ModelFiles struct {
			Detector   string `json:"detector"`
			Recognizer string `json:"recognizer"`
			Keys       string `json:"keys"`
		} `json:"model_files"`
		Settings struct {
			DetLimitSideLen int     `json:"det_limit_side_len"`
			DetThreshold    float64 `json:"det_threshold"`
			BoxThreshold    float64 `json:"box_threshold"`
			UnclipRatio     float64 `json:"unclip_ratio"`
			MaxCandidates   int     `json:"max_candidates"`
			TextScore       float64 `json:"text_score"`
			RecWidth        int     `json:"rec_width"`
			RecHeight       int     `json:"rec_height"`
		} `json:"settings"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		t.Fatalf("decode probe status: %v stdout=%s", err, result.Stdout)
	}
	if !status.NoPython {
		t.Fatalf("expected native no-python status: %#v", status)
	}
	if status.ModelFiles.Detector != filepath.Join(modelDir, "custom-det.onnx") || status.ModelFiles.Recognizer != filepath.Join(modelDir, "custom-rec.onnx") || status.ModelFiles.Keys != filepath.Join(modelDir, "custom-keys.txt") {
		t.Fatalf("unexpected model file overrides: %#v", status.ModelFiles)
	}
	if status.Settings.DetLimitSideLen != 1024 || status.Settings.DetThreshold != 0.25 || status.Settings.BoxThreshold != 0.45 || status.Settings.UnclipRatio != 2.0 || status.Settings.MaxCandidates != 33 || status.Settings.TextScore != 0.6 || status.Settings.RecWidth != 640 || status.Settings.RecHeight != 48 {
		t.Fatalf("unexpected OCR settings: %#v", status.Settings)
	}
}

func TestRapidOCRProbeFindsPPOCRV6HuggingFaceLayout(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	modelDir := t.TempDir()
	detPath := filepath.Join(modelDir, "PP-OCRv6_tiny_det_onnx", "inference.onnx")
	recPath := filepath.Join(modelDir, "PP-OCRv6_tiny_rec_onnx", "inference.onnx")
	keysPath := filepath.Join(modelDir, "PP-OCRv6_tiny_rec_onnx", "inference.yml")
	for _, path := range []string{detPath, recPath, keysPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.RunSkill(context.Background(), "ppocrv6", []string{"--probe", "--model-dir", modelDir}, nil)
	if err != nil {
		t.Fatalf("run ppocrv6 probe: %v result=%#v", err, result)
	}
	var status struct {
		ModelFiles struct {
			Detector   string `json:"detector"`
			Recognizer string `json:"recognizer"`
			Keys       string `json:"keys"`
		} `json:"model_files"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		t.Fatalf("decode probe status: %v stdout=%s", err, result.Stdout)
	}
	if status.ModelFiles.Detector != detPath || status.ModelFiles.Recognizer != recPath || status.ModelFiles.Keys != keysPath {
		t.Fatalf("unexpected Hugging Face layout paths: %#v", status.ModelFiles)
	}
}

func TestRapidOCRProbeFindsRuntimeDirLibrary(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	modelDir := t.TempDir()
	runtimeDir := t.TempDir()
	libraryPath := filepath.Join(runtimeDir, nativeLibraryFilename("onnxruntime"))
	if err := os.WriteFile(libraryPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := service.RunSkill(context.Background(), "rapidocr", []string{"--probe", "--model-dir", modelDir, "--runtime-dir", runtimeDir}, nil)
	if err != nil {
		t.Fatalf("run rapidocr probe: %v result=%#v", err, result)
	}
	var status struct {
		RuntimeLibrary string `json:"runtime_library"`
		Available      bool   `json:"available"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		t.Fatalf("decode probe status: %v stdout=%s", err, result.Stdout)
	}
	if status.RuntimeLibrary != libraryPath || status.Available {
		t.Fatalf("unexpected runtime library probe status: %#v", status)
	}
}

func TestRapidOCRDownloadModelsDryRunPlansNativeAssets(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	modelDir := t.TempDir()
	result, err := service.RunSkill(context.Background(), "rapidocr", []string{"--download-models", "--dry-run", "--model-dir", modelDir, "--model-size", "small"}, nil)
	if err != nil {
		t.Fatalf("run rapidocr download dry-run: %v result=%#v", err, result)
	}
	if result.Name != "ocr" || result.Version != "0.6.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected built-in download result: %#v", result)
	}
	if len(result.UsageEvents) != 0 {
		t.Fatalf("OCR model setup must not bill usage events: %#v", result.UsageEvents)
	}
	var download struct {
		ModelProfile string `json:"model_profile"`
		ModelSize    string `json:"model_size"`
		NoPython     bool   `json:"no_python"`
		DryRun       bool   `json:"dry_run"`
		Assets       []struct {
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &download); err != nil {
		t.Fatalf("decode download result: %v stdout=%s", err, result.Stdout)
	}
	if download.ModelProfile != "ppocrv6" || download.ModelSize != "small" || !download.NoPython || !download.DryRun || len(download.Assets) != 4 {
		t.Fatalf("unexpected download plan: %#v", download)
	}
	for _, asset := range download.Assets {
		if asset.Status != "planned" || !strings.Contains(asset.URL, "huggingface.co/PaddlePaddle/PP-OCRv6_small_") || !strings.HasPrefix(asset.Path, modelDir) {
			t.Fatalf("unexpected asset plan entry: %#v", asset)
		}
	}
}

func TestRapidOCRDownloadRuntimeDryRunPlansNativeRuntime(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	modelDir := t.TempDir()
	runtimeDir := filepath.Join(modelDir, "ort")
	result, err := service.RunSkill(context.Background(), "rapidocr", []string{"--download-runtime", "--dry-run", "--model-dir", modelDir, "--runtime-dir", runtimeDir}, nil)
	if err != nil {
		t.Fatalf("run rapidocr runtime dry-run: %v result=%#v", err, result)
	}
	if result.Name != "ocr" || result.Version != "0.6.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected built-in runtime download result: %#v", result)
	}
	if len(result.UsageEvents) != 0 {
		t.Fatalf("OCR runtime setup must not bill usage events: %#v", result.UsageEvents)
	}
	var download struct {
		Backend        string `json:"backend"`
		RuntimeVersion string `json:"runtime_version"`
		RuntimeDir     string `json:"runtime_dir"`
		RuntimeLibrary string `json:"runtime_library"`
		URL            string `json:"url"`
		Status         string `json:"status"`
		NoPython       bool   `json:"no_python"`
		DryRun         bool   `json:"dry_run"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &download); err != nil {
		t.Fatalf("decode runtime download result: %v stdout=%s", err, result.Stdout)
	}
	if download.Backend != "onnxruntime" || download.RuntimeVersion != defaultONNXRuntimeVersion || download.RuntimeDir != runtimeDir || download.Status != "planned" || !download.NoPython || !download.DryRun {
		t.Fatalf("unexpected runtime plan: %#v", download)
	}
	if !strings.Contains(download.URL, "github.com/microsoft/onnxruntime/releases/download/v"+defaultONNXRuntimeVersion+"/onnxruntime-") {
		t.Fatalf("unexpected runtime URL: %q", download.URL)
	}
	if download.RuntimeLibrary != filepath.Join(runtimeDir, nativeLibraryFilename("onnxruntime")) {
		t.Fatalf("unexpected runtime library path: %q", download.RuntimeLibrary)
	}
}

func TestONNXRuntimeArchiveInfoUsesPublishedCPUAssetNames(t *testing.T) {
	cases := []struct {
		GoOS    string
		GoArch  string
		Archive string
		Member  string
	}{
		{"windows", "amd64", "onnxruntime-win-x64-1.26.0.zip", "onnxruntime-win-x64-1.26.0/lib/onnxruntime.dll"},
		{"windows", "arm64", "onnxruntime-win-arm64-1.26.0.zip", "onnxruntime-win-arm64-1.26.0/lib/onnxruntime.dll"},
		{"linux", "amd64", "onnxruntime-linux-x64-1.26.0.tgz", "onnxruntime-linux-x64-1.26.0/lib/libonnxruntime.so.1.26.0"},
		{"linux", "arm64", "onnxruntime-linux-aarch64-1.26.0.tgz", "onnxruntime-linux-aarch64-1.26.0/lib/libonnxruntime.so.1.26.0"},
		{"darwin", "arm64", "onnxruntime-osx-arm64-1.26.0.tgz", "onnxruntime-osx-arm64-1.26.0/lib/libonnxruntime.1.26.0.dylib"},
	}
	for _, tc := range cases {
		t.Run(tc.GoOS+"_"+tc.GoArch, func(t *testing.T) {
			archive, member, err := onnxRuntimeArchiveInfoFor(tc.GoOS, tc.GoArch, "1.26.0")
			if err != nil {
				t.Fatalf("archive info: %v", err)
			}
			if archive != tc.Archive || member != tc.Member {
				t.Fatalf("unexpected archive info: got %q %q want %q %q", archive, member, tc.Archive, tc.Member)
			}
		})
	}
	if _, _, err := onnxRuntimeArchiveInfoFor("darwin", "amd64", "1.26.0"); !IsErrorCode(err, CodePlatformUnsupported) {
		t.Fatalf("expected macOS Intel v1.26.0 to report unsupported published archive, got %v", err)
	}
	if archive, member, err := onnxRuntimeArchiveInfoFor("darwin", "amd64", "1.23.2"); err != nil || archive != "onnxruntime-osx-x86_64-1.23.2.tgz" || member != "onnxruntime-osx-x86_64-1.23.2/lib/libonnxruntime.1.23.2.dylib" {
		t.Fatalf("unexpected macOS Intel v1.23.2 archive info: archive=%q member=%q err=%v", archive, member, err)
	}
}

func TestRunRapidOCRWithoutBackendDoesNotFallBackToStubOrPython(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "ppocrv6", []string{"sample.png"}, nil)
	if !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected native backend configuration error, got %v result=%#v", err, result)
	}
	if result.Name != "ocr" || result.Version != "0.6.0" || result.Stub {
		t.Fatalf("expected non-stub built-in OCR result, got %#v", result)
	}
	status, ok := ErrorFrom(err).Details.(builtinOCRStatus)
	if !ok {
		t.Fatalf("expected OCR status details, got %#v", ErrorFrom(err).Details)
	}
	if !status.NoPython || status.Available || status.Backend != "onnxruntime" {
		t.Fatalf("unexpected backend status: %#v", status)
	}
}
