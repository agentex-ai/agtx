package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type builtinOCRStatus struct {
	Runtime          string               `json:"runtime"`
	Backend          string               `json:"backend"`
	ModelProfile     string               `json:"model_profile"`
	NoPython         bool                 `json:"no_python"`
	AdapterLinked    bool                 `json:"adapter_linked"`
	RuntimeLibrary   string               `json:"runtime_library,omitempty"`
	RuntimeVersion   string               `json:"runtime_version,omitempty"`
	ModelDir         string               `json:"model_dir"`
	ModelFiles       builtinOCRModelFiles `json:"model_files"`
	Settings         builtinOCRSettings   `json:"settings"`
	RequiredFiles    []string             `json:"required_files"`
	MissingFiles     []string             `json:"missing_files,omitempty"`
	DetectionModel   *builtinOCRModelInfo `json:"detection_model,omitempty"`
	RecognitionModel *builtinOCRModelInfo `json:"recognition_model,omitempty"`
	Available        bool                 `json:"available"`
	Warnings         []string             `json:"warnings,omitempty"`
	Error            string               `json:"error,omitempty"`
	NextActions      []string             `json:"next_actions,omitempty"`
}

type builtinOCRConfig struct {
	Backend        string
	ModelProfile   string
	ModelDir       string
	RuntimeDir     string
	RuntimeLibrary string
	ModelFiles     builtinOCRModelFiles
	DetInputName   string
	DetOutputName  string
	RecInputName   string
	RecOutputName  string
	Settings       builtinOCRSettings
}

type builtinOCRSettings struct {
	DetLimitSideLen int     `json:"det_limit_side_len"`
	DetThreshold    float64 `json:"det_threshold"`
	BoxThreshold    float64 `json:"box_threshold"`
	UnclipRatio     float64 `json:"unclip_ratio"`
	MaxCandidates   int     `json:"max_candidates"`
	TextScore       float64 `json:"text_score"`
	RecWidth        int     `json:"rec_width,omitempty"`
	RecHeight       int     `json:"rec_height,omitempty"`
	RecMaxWidth     int     `json:"rec_max_width,omitempty"`
}

type builtinOCRModelFiles struct {
	Detector          string `json:"detector"`
	DetectorWeights   string `json:"detector_weights,omitempty"`
	Recognizer        string `json:"recognizer"`
	RecognizerWeights string `json:"recognizer_weights,omitempty"`
	Keys              string `json:"keys"`
}

type builtinOCRModelInfo struct {
	Path    string                 `json:"path"`
	Inputs  []builtinOCRTensorInfo `json:"inputs,omitempty"`
	Outputs []builtinOCRTensorInfo `json:"outputs,omitempty"`
}

type builtinOCRTensorInfo struct {
	Name       string  `json:"name"`
	ValueType  string  `json:"value_type,omitempty"`
	DataType   string  `json:"data_type,omitempty"`
	Dimensions []int64 `json:"dimensions,omitempty"`
}

type builtinOCRAdapterProbe struct {
	RuntimeVersion   string
	DetectionModel   *builtinOCRModelInfo
	RecognitionModel *builtinOCRModelInfo
	Warnings         []string
	Error            string
}

type builtinOCRRequest struct {
	InputPath string
	Input     []byte
	Args      []string
	TimeoutMS int64
}

type builtinOCRAdapter interface {
	Linked() bool
	Probe(context.Context, builtinOCRConfig) builtinOCRAdapterProbe
	Run(context.Context, builtinOCRConfig, builtinOCRRequest) (RunResult, error)
}

func (s *Service) builtinRegistrySkill(name string) (SkillManifest, bool) {
	skill, ok := s.Registry.Find(name)
	if !ok || skill.Builtin == nil {
		return SkillManifest{}, false
	}
	return skill, true
}

func (s *Service) runBuiltinManifest(ctx context.Context, manifest SkillManifest, options RunOptions, start time.Time) (RunResult, error) {
	result := RunResult{
		Name:             manifest.Name,
		Version:          manifest.Version,
		Stub:             false,
		ScenarioID:       options.ScenarioID,
		InvocationID:     NewTraceID(),
		TimeoutMS:        options.Timeout.Milliseconds(),
		OutputLimitBytes: options.OutputLimitBytes,
	}
	runResult, err := s.runBuiltinSkill(ctx, manifest, options)
	runResult.Name = manifest.Name
	runResult.Version = manifest.Version
	runResult.Stub = false
	runResult.ScenarioID = options.ScenarioID
	runResult.InvocationID = result.InvocationID
	runResult.DurationMS = time.Since(start).Milliseconds()
	runResult.TimeoutMS = options.Timeout.Milliseconds()
	runResult.OutputLimitBytes = options.OutputLimitBytes
	if err != nil {
		return runResult, err
	}
	if builtinRunShouldBill(manifest, options) {
		runResult.UsageEvents = s.recordRunUsage(ctx, manifest, runResult)
	}
	if len(runResult.UsageEvents) > 0 {
		if err := s.withMutationLock(func() error {
			_, err := s.appendBillingRecords(billingRecordsForUsage(manifest, runResult, runResult.UsageEvents))
			return err
		}); err != nil {
			return runResult, err
		}
	}
	return runResult, nil
}

func (s *Service) runBuiltinSkill(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	switch canonicalSkillName(manifest.Name) {
	case deepResearchSkillName:
		return s.runBuiltinDeepResearch(ctx, manifest, options)
	case "web_search":
		return s.runBuiltinWebSearch(ctx, manifest, options)
	case "docx", "xlsx", "pptx":
		return s.runBuiltinOffice(ctx, manifest, options)
	case "pdf":
		return s.runBuiltinPDF(ctx, manifest, options)
	case "web_fetch":
		return s.runBuiltinWebFetch(ctx, manifest, options)
	case "ocr":
		return s.runBuiltinOCR(ctx, manifest, options)
	default:
		return RunResult{}, NewError(CodeNotImplemented, "built-in skill runtime is not implemented", map[string]any{"skill": manifest.Name})
	}
}

func builtinRunShouldBill(manifest SkillManifest, options RunOptions) bool {
	if canonicalSkillName(manifest.Name) == "ocr" && hasBuiltinOCRProbeArg(options.Args) {
		return false
	}
	if canonicalSkillName(manifest.Name) == "ocr" && hasBuiltinOCRDownloadArg(options.Args) {
		return false
	}
	if canonicalSkillName(manifest.Name) == "ocr" && hasBuiltinOCRRuntimeDownloadArg(options.Args) {
		return false
	}
	return true
}

func (s *Service) runBuiltinOCR(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	default:
	}
	if hasBuiltinOCRRuntimeDownloadArg(options.Args) {
		result, err := s.downloadBuiltinOCRRuntime(ctx, options)
		if err != nil {
			return RunResult{ExitCode: -1}, err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return RunResult{ExitCode: -1}, err
		}
		return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
	}
	if hasBuiltinOCRDownloadArg(options.Args) {
		result, err := s.downloadBuiltinOCRAssets(ctx, options)
		if err != nil {
			return RunResult{ExitCode: -1}, err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return RunResult{ExitCode: -1}, err
		}
		return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
	}
	status := s.builtinOCRStatus(ctx, options)
	if hasBuiltinOCRProbeArg(options.Args) {
		data, err := json.Marshal(status)
		if err != nil {
			return RunResult{ExitCode: -1}, err
		}
		return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
	}
	if strings.TrimSpace(ocrInputArg(options.Args)) == "" && len(options.Input) == 0 {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "OCR input is required", map[string]any{"skill": manifest.Name, "expected": "image_or_pdf_page_path_or_input_bytes"})
	}
	if !status.Available {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "native OCR backend is not configured", status)
	}
	config := s.builtinOCRConfig(options)
	return builtinOCRAdapterFor(status.Backend).Run(ctx, config, builtinOCRRequest{InputPath: ocrInputArg(options.Args), Input: options.Input, Args: options.Args, TimeoutMS: options.Timeout.Milliseconds()})
}

func (s *Service) builtinOCRStatus(ctx context.Context, options RunOptions) builtinOCRStatus {
	config := s.builtinOCRConfig(options)
	adapter := builtinOCRAdapterFor(config.Backend)
	status := builtinOCRStatus{
		Runtime:        "agtx-native-ocr-v1",
		Backend:        config.Backend,
		ModelProfile:   config.ModelProfile,
		NoPython:       true,
		AdapterLinked:  adapter.Linked(),
		RuntimeLibrary: config.RuntimeLibrary,
		ModelDir:       config.ModelDir,
		ModelFiles:     config.ModelFiles,
		Settings:       config.Settings,
		RequiredFiles:  config.requiredFiles(),
	}
	for _, path := range status.RequiredFiles {
		if _, err := os.Stat(path); err != nil {
			status.MissingFiles = append(status.MissingFiles, path)
		}
	}
	if !status.AdapterLinked {
		status.Error = "native OCR adapter is not linked into this build"
	} else if strings.TrimSpace(status.RuntimeLibrary) == "" {
		status.Error = "native OCR runtime library is missing"
	} else if len(status.MissingFiles) > 0 {
		status.Error = "native OCR model files are missing"
	} else {
		probe := adapter.Probe(ctx, config)
		status.RuntimeVersion = probe.RuntimeVersion
		status.DetectionModel = probe.DetectionModel
		status.RecognitionModel = probe.RecognitionModel
		status.Warnings = append(status.Warnings, probe.Warnings...)
		status.Error = probe.Error
	}
	status.Available = status.Error == ""
	if !status.Available {
		status.NextActions = builtinOCRNextActions(status)
	}
	return status
}

func (s *Service) builtinOCRConfig(options RunOptions) builtinOCRConfig {
	backend := strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "backend", os.Getenv("AGTX_OCR_BACKEND"))))
	if backend == "" || backend == "auto" {
		backend = "onnxruntime"
	}
	if backend != "onnxruntime" && backend != "ncnn" {
		backend = "onnxruntime"
	}
	profile := strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "model-profile", "")))
	if profile == "" {
		profile = strings.ToLower(strings.TrimSpace(ocrOptionValue(options.Args, "model_profile", "")))
	}
	if profile == "" || profile == "auto" {
		profile = "ppocrv6"
	}
	modelDir := strings.TrimSpace(ocrOptionValue(options.Args, "model-dir", os.Getenv("AGTX_OCR_MODEL_DIR")))
	if modelDir == "" {
		modelDir = strings.TrimSpace(ocrOptionValue(options.Args, "model_dir", ""))
	}
	if modelDir == "" {
		modelDir = filepath.Join(s.Paths.ConfigDir, "builtin", "ocr")
	}
	runtimeDir := strings.TrimSpace(ocrOptionValue(options.Args, "runtime-dir", os.Getenv("AGTX_OCR_RUNTIME_DIR")))
	if runtimeDir == "" {
		runtimeDir = strings.TrimSpace(ocrOptionValue(options.Args, "runtime_dir", ""))
	}
	config := builtinOCRConfig{
		Backend:        backend,
		ModelProfile:   profile,
		ModelDir:       modelDir,
		RuntimeDir:     runtimeDir,
		RuntimeLibrary: builtinOCRRuntimeLibrary(backend, modelDir, runtimeDir),
		DetInputName:   strings.TrimSpace(ocrOptionValue(options.Args, "det-input", os.Getenv("AGTX_OCR_DET_INPUT"))),
		DetOutputName:  strings.TrimSpace(ocrOptionValue(options.Args, "det-output", os.Getenv("AGTX_OCR_DET_OUTPUT"))),
		RecInputName:   strings.TrimSpace(ocrOptionValue(options.Args, "rec-input", os.Getenv("AGTX_OCR_REC_INPUT"))),
		RecOutputName:  strings.TrimSpace(ocrOptionValue(options.Args, "rec-output", os.Getenv("AGTX_OCR_REC_OUTPUT"))),
		Settings: builtinOCRSettings{
			DetLimitSideLen: ocrOptionInt(options.Args, "det-limit-side-len", os.Getenv("AGTX_OCR_DET_LIMIT_SIDE_LEN"), 736),
			DetThreshold:    ocrOptionFloat(options.Args, "det-threshold", os.Getenv("AGTX_OCR_DET_THRESHOLD"), 0.3),
			BoxThreshold:    ocrOptionFloat(options.Args, "box-threshold", os.Getenv("AGTX_OCR_BOX_THRESHOLD"), 0.5),
			UnclipRatio:     ocrOptionFloat(options.Args, "unclip-ratio", os.Getenv("AGTX_OCR_UNCLIP_RATIO"), 1.6),
			MaxCandidates:   ocrOptionInt(options.Args, "max-candidates", os.Getenv("AGTX_OCR_MAX_CANDIDATES"), 1000),
			TextScore:       ocrOptionFloat(options.Args, "text-score", os.Getenv("AGTX_OCR_TEXT_SCORE"), 0.5),
			RecWidth:        ocrOptionInt(options.Args, "rec-width", os.Getenv("AGTX_OCR_REC_WIDTH"), 0),
			RecHeight:       ocrOptionInt(options.Args, "rec-height", os.Getenv("AGTX_OCR_REC_HEIGHT"), 0),
			RecMaxWidth:     ocrOptionInt(options.Args, "rec-max-width", os.Getenv("AGTX_OCR_REC_MAX_WIDTH"), 1600),
		},
	}
	config.ModelFiles = builtinOCRModelFiles{
		Detector:   builtinOCRModelFilePath(options.Args, modelDir, backend, profile, "det", os.Getenv("AGTX_OCR_DET_MODEL")),
		Recognizer: builtinOCRModelFilePath(options.Args, modelDir, backend, profile, "rec", os.Getenv("AGTX_OCR_REC_MODEL")),
		Keys:       builtinOCRKeysPath(options.Args, modelDir),
	}
	if backend == "ncnn" {
		config.ModelFiles.DetectorWeights = firstOCRModelCandidate(modelDir, []string{profile + "-det.bin", "det.bin"})
		config.ModelFiles.RecognizerWeights = firstOCRModelCandidate(modelDir, []string{profile + "-rec.bin", "rec.bin"})
	}
	return config
}

func (c builtinOCRConfig) requiredFiles() []string {
	files := []string{c.ModelFiles.Detector, c.ModelFiles.Recognizer, c.ModelFiles.Keys}
	if c.ModelFiles.DetectorWeights != "" {
		files = append(files, c.ModelFiles.DetectorWeights)
	}
	if c.ModelFiles.RecognizerWeights != "" {
		files = append(files, c.ModelFiles.RecognizerWeights)
	}
	return files
}

func builtinOCRNextActions(status builtinOCRStatus) []string {
	actions := []string{}
	if !status.AdapterLinked {
		actions = append(actions, "build agtx with -tags ocr_onnxruntime for the ONNX Runtime adapter, or add an ncnn adapter build")
	}
	if strings.TrimSpace(status.RuntimeLibrary) == "" {
		actions = append(actions, "run agtx run rapidocr -- --download-runtime, place the native runtime library under AGTX_OCR_RUNTIME_DIR, or set AGTX_OCR_ONNXRUNTIME_LIBRARY/AGTX_OCR_NCNN_LIBRARY")
	}
	if len(status.MissingFiles) > 0 {
		actions = append(actions, "place PP-OCR model files under AGTX_OCR_MODEL_DIR or set AGTX_OCR_DET_MODEL, AGTX_OCR_REC_MODEL, and AGTX_OCR_KEYS")
	}
	actions = append(actions, "set AGTX_OCR_BACKEND to onnxruntime or ncnn; Python and NPM runtimes are not used")
	return actions
}

func builtinOCRModelFilePath(args []string, modelDir, backend, profile, kind, fallback string) string {
	value := strings.TrimSpace(ocrOptionValue(args, kind+"-model", fallback))
	if value == "" {
		value = strings.TrimSpace(ocrOptionValue(args, kind+"_model", ""))
	}
	if value != "" {
		return resolveOCRModelPath(modelDir, value)
	}
	for _, candidate := range builtinOCRModelFileCandidates(backend, profile, kind) {
		path := filepath.Join(modelDir, candidate)
		if fileExists(path) {
			return path
		}
	}
	candidates := builtinOCRModelFileCandidates(backend, profile, kind)
	if len(candidates) == 0 {
		return filepath.Join(modelDir, profile+"-"+kind+".onnx")
	}
	return filepath.Join(modelDir, candidates[0])
}

func builtinOCRKeysPath(args []string, modelDir string) string {
	value := strings.TrimSpace(ocrOptionValue(args, "keys", os.Getenv("AGTX_OCR_KEYS")))
	if value != "" {
		return resolveOCRModelPath(modelDir, value)
	}
	for _, candidate := range builtinOCRKeysCandidates() {
		path := filepath.Join(modelDir, candidate)
		if fileExists(path) {
			return path
		}
	}
	return filepath.Join(modelDir, "keys.txt")
}

func builtinOCRKeysCandidates() []string {
	return []string{
		"keys.txt",
		"ppocr_keys_v1.txt",
		"ppocrv5_dict.txt",
		"dict.txt",
		"inference.yml",
		"inference.yaml",
		filepath.Join("PP-OCRv6_tiny_rec_onnx", "inference.yml"),
		filepath.Join("PP-OCRv6_small_rec_onnx", "inference.yml"),
		filepath.Join("PP-OCRv6_medium_rec_onnx", "inference.yml"),
	}
}

func builtinOCRModelFileCandidates(backend, profile, kind string) []string {
	if backend == "ncnn" {
		return []string{profile + "-" + kind + ".param", kind + ".param"}
	}
	names := []string{profile + "-" + kind + ".onnx", kind + ".onnx"}
	switch profile {
	case "ppocrv6":
		names = append(names,
			"PP-OCRv6_tiny_"+kind+".onnx",
			"PP-OCRv6_small_"+kind+".onnx",
			"PP-OCRv6_medium_"+kind+".onnx",
			filepath.Join("PP-OCRv6_tiny_"+kind+"_onnx", "inference.onnx"),
			filepath.Join("PP-OCRv6_small_"+kind+"_onnx", "inference.onnx"),
			filepath.Join("PP-OCRv6_medium_"+kind+"_onnx", "inference.onnx"),
			"ch_PP-OCRv6_"+kind+".onnx",
			"ch_PP-OCRv6_server_"+kind+".onnx",
			"ch_PP-OCRv6_mobile_"+kind+".onnx",
		)
	case "ppocrv5":
		names = append(names, "ch_PP-OCRv5_"+kind+".onnx", "ch_PP-OCRv5_server_"+kind+".onnx", "ch_PP-OCRv5_mobile_"+kind+".onnx")
	case "ppocrv4":
		names = append(names, "ch_PP-OCRv4_"+kind+".onnx", "ch_PP-OCRv4_det_infer.onnx", "ch_PP-OCRv4_rec_infer.onnx")
	}
	return names
}

func firstOCRModelCandidate(modelDir string, candidates []string) string {
	for _, candidate := range candidates {
		path := filepath.Join(modelDir, candidate)
		if fileExists(path) {
			return path
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return filepath.Join(modelDir, candidates[0])
}

func resolveOCRModelPath(modelDir, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(modelDir, value)
}

func builtinOCRRuntimeLibrary(backend, modelDir, runtimeDir string) string {
	if backend == "ncnn" {
		if path := strings.TrimSpace(os.Getenv("AGTX_OCR_NCNN_LIBRARY")); path != "" && fileExists(path) {
			return path
		}
		return firstExistingFile(nativeLibraryCandidates("ncnn", modelDir, runtimeDir))
	}
	if path := strings.TrimSpace(os.Getenv("AGTX_OCR_ONNXRUNTIME_LIBRARY")); path != "" && fileExists(path) {
		return path
	}
	return firstExistingFile(nativeLibraryCandidates("onnxruntime", modelDir, runtimeDir))
}

func nativeLibraryCandidates(name, modelDir, runtimeDir string) []string {
	filename := nativeLibraryFilename(name)
	dirs := []string{}
	if strings.TrimSpace(runtimeDir) != "" {
		dirs = append(dirs, runtimeDir)
	}
	if runtimeDir := strings.TrimSpace(os.Getenv("AGTX_OCR_RUNTIME_DIR")); runtimeDir != "" {
		dirs = append(dirs, runtimeDir)
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "native"), filepath.Join(filepath.Dir(exe), "ocr"))
	}
	if strings.TrimSpace(modelDir) != "" {
		dirs = append(dirs, filepath.Join(modelDir, "runtime"), modelDir)
	}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, filepath.Join(dir, filename))
	}
	return out
}

func nativeLibraryFilename(name string) string {
	switch runtime.GOOS {
	case "windows":
		return name + ".dll"
	case "darwin":
		return "lib" + name + ".dylib"
	default:
		return "lib" + name + ".so"
	}
}

func firstExistingFile(paths []string) string {
	for _, path := range paths {
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasBuiltinOCRProbeArg(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "probe", "--probe", "--native-backend-status", "native-backend-status":
			return true
		}
	}
	return false
}

func hasBuiltinOCRDownloadArg(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "download-models", "--download-models", "init-assets", "--init-assets", "install-models", "--install-models":
			return true
		}
	}
	return false
}

func hasBuiltinOCRRuntimeDownloadArg(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "download-runtime", "--download-runtime", "init-runtime", "--init-runtime", "install-runtime", "--install-runtime":
			return true
		}
	}
	return false
}

func ocrInputArg(args []string) string {
	skipNext := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if builtinOCRArgTakesValue(arg) {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") || strings.EqualFold(arg, "probe") || strings.EqualFold(arg, "native-backend-status") {
			continue
		}
		return arg
	}
	return ""
}

func builtinOCRArgTakesValue(arg string) bool {
	switch arg {
	case "--backend", "--model-profile", "--model_profile", "--model-dir", "--model_dir", "--model-size", "--model_size", "--runtime-version", "--runtime_version", "--runtime-dir", "--runtime_dir", "--det-model", "--det_model", "--rec-model", "--rec_model", "--keys", "--det-input", "--det_input", "--det-output", "--det_output", "--rec-input", "--rec_input", "--rec-output", "--rec_output", "--det-limit-side-len", "--det_limit_side_len", "--det-threshold", "--det_threshold", "--box-threshold", "--box_threshold", "--unclip-ratio", "--unclip_ratio", "--max-candidates", "--max_candidates", "--text-score", "--text_score", "--rec-width", "--rec_width", "--rec-height", "--rec_height", "--rec-max-width", "--rec_max_width":
		return true
	default:
		return false
	}
}

func ocrOptionInt(args []string, name, fallback string, defaultValue int) int {
	value := strings.TrimSpace(ocrOptionValue(args, name, fallback))
	if value == "" {
		value = strings.TrimSpace(ocrOptionValue(args, strings.ReplaceAll(name, "-", "_"), ""))
	}
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func ocrOptionFloat(args []string, name, fallback string, defaultValue float64) float64 {
	value := strings.TrimSpace(ocrOptionValue(args, name, fallback))
	if value == "" {
		value = strings.TrimSpace(ocrOptionValue(args, strings.ReplaceAll(name, "-", "_"), ""))
	}
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func ocrOptionValue(args []string, name, fallback string) string {
	dash := "--" + name
	underscore := strings.ReplaceAll(dash, "-", "_")
	key := strings.ReplaceAll(name, "-", "_") + "="
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, dash+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, dash+"="))
		}
		if strings.HasPrefix(arg, underscore+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, underscore+"="))
		}
		if strings.HasPrefix(arg, key) {
			return strings.TrimSpace(strings.TrimPrefix(arg, key))
		}
		if (arg == dash || arg == underscore) && index+1 < len(args) {
			return strings.TrimSpace(args[index+1])
		}
	}
	return fallback
}
