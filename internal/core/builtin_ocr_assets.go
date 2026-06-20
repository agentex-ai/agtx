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
	Kind    string   `json:"kind"`
	URL     string   `json:"url"`
	URLs    []string `json:"urls,omitempty"`
	Path    string   `json:"path"`
	Status  string   `json:"status"`
	Bytes   int64    `json:"bytes,omitempty"`
	SHA256  string   `json:"sha256,omitempty"`
	Sources int      `json:"sources,omitempty"`
}

type builtinOCRAssetSource struct {
	Kind   string
	URL    string
	URLs   []string
	Path   string
	SHA256 string
}

func (s *Service) downloadBuiltinOCRAssets(ctx context.Context, options RunOptions) (builtinOCRAssetDownloadResult, error) {
	config := s.builtinOCRConfig(options)
	modelSize := strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "model-size", os.Getenv("AGTX_OCR_MODEL_SIZE"))))
	if modelSize == "" {
		modelSize = strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "model_size", "")))
	}
	if modelSize == "" || modelSize == "auto" {
		if config.ModelProfile == "ppocrv6" {
			modelSize = "tiny"
		} else {
			modelSize = "mobile"
		}
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
		state := builtinOCRAssetDownloadState{Kind: asset.Kind, URL: asset.firstURL(), URLs: asset.urls(), Path: asset.Path, Status: "pending", SHA256: asset.SHA256, Sources: len(asset.urls())}
		if result.DryRun {
			state.Status = "planned"
			result.Assets = append(result.Assets, state)
			continue
		}
		if info, err := os.Stat(asset.Path); err == nil && !info.IsDir() && info.Size() > 0 {
			if asset.SHA256 == "" || fileSHA256Matches(asset.Path, asset.SHA256) {
				state.Status = "exists"
				state.Bytes = info.Size()
				result.Skipped = append(result.Skipped, asset.Path)
				result.Assets = append(result.Assets, state)
				continue
			}
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
	switch config.ModelProfile {
	case "rapidocr", "ppocrv4":
		if modelSize != "mobile" {
			return nil, NewError(CodeInvalidArgument, "RapidOCR model download uses the upstream mobile PP-OCRv4 profile", map[string]any{"model_size": modelSize, "supported_model_sizes": []string{"mobile"}})
		}
		return []builtinOCRAssetSource{
			{
				Kind:   "detector_model",
				URLs:   rapidOCRModelURLs("det", "ch_PP-OCRv4_det_mobile.onnx", "ch_PP-OCRv4_det_infer.onnx"),
				Path:   filepath.Join(config.ModelDir, "ch_PP-OCRv4_det_mobile.onnx"),
				SHA256: "d2a7720d45a54257208b1e13e36a8479894cb74155a5efe29462512d42f49da9",
			},
			{
				Kind:   "recognizer_model",
				URLs:   rapidOCRModelURLs("rec", "ch_PP-OCRv4_rec_mobile.onnx", "ch_PP-OCRv4_rec_infer.onnx"),
				Path:   filepath.Join(config.ModelDir, "ch_PP-OCRv4_rec_mobile.onnx"),
				SHA256: "48fc40f24f6d2a207a2b1091d3437eb3cc3eb6b676dc3ef9c37384005483683b",
			},
			{
				Kind:   "recognition_keys",
				URLs:   rapidOCRKeysURLs(),
				Path:   filepath.Join(config.ModelDir, "ppocr_keys_v1.txt"),
				SHA256: "28b2362ad4ab2dc38769aa72feb535e3a9ddb3fd2a7585a05920e6393b1dc7f7",
			},
		}, nil
	case "ppocrv6":
	default:
		return nil, NewError(CodeInvalidArgument, "built-in OCR model download supports RapidOCR latest and PP-OCRv6 compatibility assets only", map[string]any{"model_profile": config.ModelProfile, "supported_model_profiles": []string{"rapidocr", "ppocrv4", "ppocrv6"}})
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

func rapidOCRModelURLs(kind, modelscopeName, huggingFaceName string) []string {
	return []string{
		"https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.8.0/onnx/PP-OCRv4/" + kind + "/" + modelscopeName,
		"https://modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.8.0/onnx/PP-OCRv4/" + kind + "/" + modelscopeName,
		"https://huggingface.co/SWHL/RapidOCR/resolve/main/PP-OCRv4/" + huggingFaceName,
		"https://huggingface.co/pitapo/rapidocr/resolve/main/onnx/PP-OCRv4/" + kind + "/" + huggingFaceName,
	}
}

func rapidOCRKeysURLs() []string {
	return []string{
		"https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.8.0/paddle/PP-OCRv4/rec/ch_PP-OCRv4_rec_mobile/ppocr_keys_v1.txt",
		"https://gitee.com/paddlepaddle/PaddleOCR/raw/release/2.7/ppocr/utils/ppocr_keys_v1.txt",
		"https://raw.githubusercontent.com/PaddlePaddle/PaddleOCR/release/2.7/ppocr/utils/ppocr_keys_v1.txt",
		"https://cdn.jsdelivr.net/gh/PaddlePaddle/PaddleOCR@release/2.7/ppocr/utils/ppocr_keys_v1.txt",
	}
}

func (asset builtinOCRAssetSource) urls() []string {
	urls := append([]string{}, asset.URLs...)
	if asset.URL != "" {
		urls = append([]string{asset.URL}, urls...)
	}
	out := urls[:0]
	seen := map[string]bool{}
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, url)
	}
	return out
}

func (asset builtinOCRAssetSource) firstURL() string {
	urls := asset.urls()
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func downloadBuiltinOCRAsset(ctx context.Context, asset builtinOCRAssetSource) (int64, string, error) {
	if err := os.MkdirAll(filepath.Dir(asset.Path), 0o755); err != nil {
		return 0, "", err
	}
	var lastErr error
	for _, url := range asset.urls() {
		written, digest, err := downloadBuiltinOCRAssetFromURL(ctx, asset, url)
		if err == nil {
			return written, digest, nil
		}
		lastErr = err
	}
	return 0, "", NewError(CodeInvalidArgument, "built-in OCR asset download failed", map[string]any{"urls": asset.urls(), "path": asset.Path, "error": errorString(lastErr)})
}

func downloadBuiltinOCRAssetFromURL(ctx context.Context, asset builtinOCRAssetSource, url string) (int64, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return 0, "", NewError(CodeInvalidArgument, "built-in OCR asset download failed", map[string]any{"url": url, "path": asset.Path, "error": err.Error()})
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return 0, "", NewError(CodeInvalidArgument, "built-in OCR asset download returned an error", map[string]any{"url": url, "path": asset.Path, "status": response.Status})
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
	digest := hex.EncodeToString(hash.Sum(nil))
	if asset.SHA256 != "" && !strings.EqualFold(digest, asset.SHA256) {
		return 0, "", NewError(CodeInvalidArgument, "built-in OCR asset checksum mismatch", map[string]any{"url": url, "path": asset.Path, "sha256": digest, "expected_sha256": asset.SHA256})
	}
	if err := replaceFile(tmpPath, asset.Path); err != nil {
		return 0, "", err
	}
	return written, digest, nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func fileSHA256Matches(path, want string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), want)
}

func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
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
