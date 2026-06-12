package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type builtinOCRAssetDownloadResult struct {
	Runtime      string                         `json:"runtime"`
	Backend      string                         `json:"backend"`
	ModelProfile string                         `json:"model_profile"`
	ModelSize    string                         `json:"model_size"`
	ModelDir     string                         `json:"model_dir"`
	NoPython     bool                           `json:"no_python"`
	DryRun       bool                           `json:"dry_run,omitempty"`
	Assets       []builtinOCRAssetDownloadState `json:"assets"`
	Downloaded   []string                       `json:"downloaded,omitempty"`
	Skipped      []string                       `json:"skipped,omitempty"`
}

type builtinOCRAssetDownloadState struct {
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type builtinOCRAssetSource struct {
	Kind string
	URL  string
	Path string
}

func (s *Service) downloadBuiltinOCRAssets(ctx context.Context, options RunOptions) (builtinOCRAssetDownloadResult, error) {
	config := s.builtinOCRConfig(options)
	modelSize := strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "model-size", os.Getenv("AGTX_OCR_MODEL_SIZE"))))
	if modelSize == "" {
		modelSize = strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "model_size", "")))
	}
	if modelSize == "" || modelSize == "auto" {
		modelSize = "tiny"
	}
	assets, err := builtinOCRAssetSources(config, modelSize)
	if err != nil {
		return builtinOCRAssetDownloadResult{}, err
	}
	result := builtinOCRAssetDownloadResult{
		Runtime:      "agtx-native-ocr-v1",
		Backend:      config.Backend,
		ModelProfile: config.ModelProfile,
		ModelSize:    modelSize,
		ModelDir:     config.ModelDir,
		NoPython:     true,
		DryRun:       hasBuiltinOCRDryRunArg(options.Args),
	}
	for _, asset := range assets {
		state := builtinOCRAssetDownloadState{Kind: asset.Kind, URL: asset.URL, Path: asset.Path, Status: "pending"}
		if result.DryRun {
			state.Status = "planned"
			result.Assets = append(result.Assets, state)
			continue
		}
		if info, err := os.Stat(asset.Path); err == nil && !info.IsDir() && info.Size() > 0 {
			state.Status = "exists"
			state.Bytes = info.Size()
			result.Skipped = append(result.Skipped, asset.Path)
			result.Assets = append(result.Assets, state)
			continue
		}
		bytesWritten, digest, err := downloadBuiltinOCRAsset(ctx, asset)
		if err != nil {
			return result, err
		}
		state.Status = "downloaded"
		state.Bytes = bytesWritten
		state.SHA256 = digest
		result.Downloaded = append(result.Downloaded, asset.Path)
		result.Assets = append(result.Assets, state)
	}
	return result, nil
}

func builtinOCRAssetSources(config builtinOCRConfig, modelSize string) ([]builtinOCRAssetSource, error) {
	if config.Backend != "onnxruntime" {
		return nil, NewError(CodeInvalidArgument, "built-in OCR model download supports only the onnxruntime backend", map[string]any{"backend": config.Backend, "supported_backend": "onnxruntime"})
	}
	if config.ModelProfile != "ppocrv6" {
		return nil, NewError(CodeInvalidArgument, "built-in OCR model download supports PP-OCRv6 assets only", map[string]any{"model_profile": config.ModelProfile, "supported_model_profile": "ppocrv6"})
	}
	switch modelSize {
	case "tiny", "small", "medium":
	default:
		return nil, NewError(CodeInvalidArgument, "unsupported PP-OCRv6 model size", map[string]any{"model_size": modelSize, "supported_model_sizes": []string{"tiny", "small", "medium"}})
	}
	detRepo := fmt.Sprintf("PP-OCRv6_%s_det_onnx", modelSize)
	recRepo := fmt.Sprintf("PP-OCRv6_%s_rec_onnx", modelSize)
	return []builtinOCRAssetSource{
		{Kind: "detector_model", URL: ppocrv6HuggingFaceURL(detRepo, "inference.onnx"), Path: filepath.Join(config.ModelDir, detRepo, "inference.onnx")},
		{Kind: "detector_config", URL: ppocrv6HuggingFaceURL(detRepo, "inference.yml"), Path: filepath.Join(config.ModelDir, detRepo, "inference.yml")},
		{Kind: "recognizer_model", URL: ppocrv6HuggingFaceURL(recRepo, "inference.onnx"), Path: filepath.Join(config.ModelDir, recRepo, "inference.onnx")},
		{Kind: "recognizer_config", URL: ppocrv6HuggingFaceURL(recRepo, "inference.yml"), Path: filepath.Join(config.ModelDir, recRepo, "inference.yml")},
	}, nil
}

func ppocrv6HuggingFaceURL(repo, name string) string {
	return "https://huggingface.co/PaddlePaddle/" + repo + "/resolve/main/" + name
}

func downloadBuiltinOCRAsset(ctx context.Context, asset builtinOCRAssetSource) (int64, string, error) {
	if err := os.MkdirAll(filepath.Dir(asset.Path), 0o755); err != nil {
		return 0, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return 0, "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return 0, "", NewError(CodeInvalidArgument, "built-in OCR asset download failed", map[string]any{"url": asset.URL, "path": asset.Path, "error": err.Error()})
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return 0, "", NewError(CodeInvalidArgument, "built-in OCR asset download returned an error", map[string]any{"url": asset.URL, "path": asset.Path, "status": response.Status})
	}
	tmp, err := os.CreateTemp(filepath.Dir(asset.Path), ".agtx-ocr-*.tmp")
	if err != nil {
		return 0, "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	hash := sha256.New()
	written, copyErr := io.Copy(tmp, io.TeeReader(response.Body, hash))
	closeErr := tmp.Close()
	if copyErr != nil {
		return 0, "", copyErr
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	if err := os.Rename(tmpPath, asset.Path); err != nil {
		return 0, "", err
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func hasBuiltinOCRDryRunArg(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--dry-run", "dry-run", "--plan", "plan":
			return true
		}
	}
	return false
}
