//go:build ocr_onnxruntime && cgo

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

type onnxRuntimeOCRAdapter struct{}

type onnxOCRBox struct {
	X1    float64
	Y1    float64
	X2    float64
	Y2    float64
	Score float64
}

type onnxOCRLine struct {
	Text                string    `json:"text"`
	Confidence          float64   `json:"confidence"`
	DetectionConfidence float64   `json:"detection_confidence,omitempty"`
	BBox                []float64 `json:"bbox,omitempty"`
	Page                int       `json:"page,omitempty"`
}

type onnxOCRRunOutput struct {
	Text           string        `json:"text"`
	ModelProfile   string        `json:"model_profile"`
	Engine         string        `json:"engine"`
	DetectedBoxes  int           `json:"detected_boxes"`
	ProcessedBoxes int           `json:"processed_boxes"`
	Lines          []onnxOCRLine `json:"lines"`
	Warnings       []string      `json:"warnings,omitempty"`
}

var (
	onnxOCRMu          sync.Mutex
	onnxOCRInitialized bool
	onnxOCRLibrary     string
)

func builtinOCRAdapterFor(backend string) builtinOCRAdapter {
	if backend == "onnxruntime" {
		return onnxRuntimeOCRAdapter{}
	}
	return noopBuiltinOCRAdapter{backend: backend}
}

func (onnxRuntimeOCRAdapter) Linked() bool {
	return true
}

func (a onnxRuntimeOCRAdapter) Probe(ctx context.Context, config builtinOCRConfig) builtinOCRAdapterProbe {
	select {
	case <-ctx.Done():
		return builtinOCRAdapterProbe{Error: ctx.Err().Error()}
	default:
	}
	version, err := ensureONNXRuntime(config.RuntimeLibrary)
	if err != nil {
		return builtinOCRAdapterProbe{Error: fmt.Sprintf("initialize ONNX Runtime: %v", err)}
	}
	options, err := ort.NewSessionOptions()
	if err != nil {
		return builtinOCRAdapterProbe{RuntimeVersion: version, Error: fmt.Sprintf("create ONNX session options: %v", err)}
	}
	defer options.Destroy()
	if err := options.SetGraphOptimizationLevel(ort.GraphOptimizationLevelEnableBasic); err != nil {
		return builtinOCRAdapterProbe{RuntimeVersion: version, Error: fmt.Sprintf("configure ONNX graph optimization: %v", err)}
	}
	detInfo, err := onnxOCRModelInfo(config.ModelFiles.Detector, options)
	if err != nil {
		return builtinOCRAdapterProbe{RuntimeVersion: version, Error: fmt.Sprintf("load detector ONNX model: %v", err)}
	}
	recInfo, err := onnxOCRModelInfo(config.ModelFiles.Recognizer, options)
	if err != nil {
		return builtinOCRAdapterProbe{RuntimeVersion: version, DetectionModel: detInfo, Error: fmt.Sprintf("load recognizer ONNX model: %v", err)}
	}
	warnings := configuredONNXNameWarnings(config, detInfo, recInfo)
	warnings = append(warnings, "onnxruntime adapter is native-only; OCR uses local detector, recognizer, and CTC decoding without Python or NPM fallback")
	return builtinOCRAdapterProbe{
		RuntimeVersion:   version,
		DetectionModel:   detInfo,
		RecognitionModel: recInfo,
		Warnings:         warnings,
	}
}

func (a onnxRuntimeOCRAdapter) Run(ctx context.Context, config builtinOCRConfig, request builtinOCRRequest) (RunResult, error) {
	probe := a.Probe(ctx, config)
	if probe.Error != "" {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "native ONNX Runtime OCR backend is not ready", map[string]any{"backend": config.Backend, "model_profile": config.ModelProfile, "error": probe.Error, "no_python": true})
	}
	img, err := decodeONNXOCRImage(request)
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "OCR input image cannot be decoded", map[string]any{"input_path": request.InputPath, "error": err.Error(), "supported_formats": []string{"png", "jpeg", "gif"}})
	}
	keys, err := loadONNXOCRKeys(config.ModelFiles.Keys)
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "OCR recognition keys cannot be loaded", map[string]any{"path": config.ModelFiles.Keys, "error": err.Error()})
	}
	options, err := ort.NewSessionOptions()
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "native ONNX Runtime session options cannot be created", map[string]any{"error": err.Error(), "no_python": true})
	}
	defer options.Destroy()
	_ = options.SetGraphOptimizationLevel(ort.GraphOptimizationLevelEnableBasic)

	detInput, detOutput := onnxOCRModelIONames(config.DetInputName, config.DetOutputName, probe.DetectionModel)
	detSession, err := ort.NewDynamicAdvancedSession(config.ModelFiles.Detector, []string{detInput}, []string{detOutput}, options)
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "detector ONNX session cannot be created", map[string]any{"path": config.ModelFiles.Detector, "error": err.Error()})
	}
	defer detSession.Destroy()

	origBounds := img.Bounds()
	detW, detH := onnxOCRDetectorSize(origBounds.Dx(), origBounds.Dy(), config.Settings.DetLimitSideLen)
	detTensor, err := ort.NewTensor(ort.NewShape(1, 3, int64(detH), int64(detW)), onnxOCRImageToDetectorTensor(img, detW, detH))
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "detector input tensor cannot be created", map[string]any{"error": err.Error()})
	}
	defer detTensor.Destroy()
	detOutputs := []ort.Value{nil}
	if err := detSession.Run([]ort.Value{detTensor}, detOutputs); err != nil {
		destroyONNXValues(detOutputs)
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "detector ONNX inference failed", map[string]any{"error": err.Error(), "input": detInput, "output": detOutput})
	}
	defer destroyONNXValues(detOutputs)
	scores, scoreW, scoreH, err := onnxOCRScoreMap(detOutputs[0])
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "detector ONNX output cannot be decoded", map[string]any{"error": err.Error()})
	}
	boxes := onnxOCRDetectBoxes(scores, scoreW, scoreH, origBounds.Dx(), origBounds.Dy(), config.Settings)

	recInput, recOutput := onnxOCRModelIONames(config.RecInputName, config.RecOutputName, probe.RecognitionModel)
	recSession, err := ort.NewDynamicAdvancedSession(config.ModelFiles.Recognizer, []string{recInput}, []string{recOutput}, options)
	if err != nil {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "recognizer ONNX session cannot be created", map[string]any{"path": config.ModelFiles.Recognizer, "error": err.Error()})
	}
	defer recSession.Destroy()
	recH, baseRecW, recDynamicWidth := onnxOCRRecognitionSize(probe.RecognitionModel, config.Settings)

	output := onnxOCRRunOutput{ModelProfile: config.ModelProfile, Engine: "onnxruntime", Warnings: probe.Warnings}
	output.DetectedBoxes = len(boxes)
	if len(boxes) == 0 {
		output.Warnings = append(output.Warnings, "detector returned no text boxes")
	}
	for _, box := range boxes {
		select {
		case <-ctx.Done():
			return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": request.TimeoutMS})
		default:
		}
		output.ProcessedBoxes++
		recW := onnxOCRRecognitionWidthForBox(box, recH, baseRecW, recDynamicWidth, config.Settings)
		data := onnxOCRImageToRecognizerTensor(img, box, recW, recH)
		recTensor, err := ort.NewTensor(ort.NewShape(1, 3, int64(recH), int64(recW)), data)
		if err != nil {
			return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "recognizer input tensor cannot be created", map[string]any{"error": err.Error()})
		}
		recOutputs := []ort.Value{nil}
		err = recSession.Run([]ort.Value{recTensor}, recOutputs)
		recTensor.Destroy()
		if err != nil {
			destroyONNXValues(recOutputs)
			return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "recognizer ONNX inference failed", map[string]any{"error": err.Error(), "input": recInput, "output": recOutput})
		}
		text, confidence, err := onnxOCRDecodeRecognition(recOutputs[0], keys)
		destroyONNXValues(recOutputs)
		if err != nil {
			return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "recognizer ONNX output cannot be decoded", map[string]any{"error": err.Error()})
		}
		if strings.TrimSpace(text) == "" || confidence < config.Settings.TextScore {
			continue
		}
		line := onnxOCRLine{Text: text, Confidence: confidence, DetectionConfidence: box.Score, BBox: []float64{box.X1, box.Y1, box.X2, box.Y1, box.X2, box.Y2, box.X1, box.Y2}}
		output.Lines = append(output.Lines, line)
	}
	texts := make([]string, 0, len(output.Lines))
	for _, line := range output.Lines {
		texts = append(texts, line.Text)
	}
	if output.ProcessedBoxes > len(output.Lines) {
		output.Warnings = append(output.Warnings, "some detected boxes were empty or below text_score")
	}
	output.Text = strings.Join(texts, "\n")
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func ensureONNXRuntime(library string) (string, error) {
	onnxOCRMu.Lock()
	defer onnxOCRMu.Unlock()
	if onnxOCRInitialized {
		if onnxOCRLibrary != "" && library != "" && onnxOCRLibrary != library {
			return "", fmt.Errorf("ONNX Runtime already initialized with %s", onnxOCRLibrary)
		}
		return ort.GetVersion(), nil
	}
	if library != "" {
		ort.SetSharedLibraryPath(library)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return "", err
	}
	onnxOCRInitialized = true
	onnxOCRLibrary = library
	return ort.GetVersion(), nil
}

func onnxOCRModelInfo(path string, options *ort.SessionOptions) (*builtinOCRModelInfo, error) {
	inputs, outputs, err := ort.GetInputOutputInfoWithOptions(path, options)
	if err != nil {
		return nil, err
	}
	return &builtinOCRModelInfo{
		Path:    path,
		Inputs:  onnxOCRTensorInfos(inputs),
		Outputs: onnxOCRTensorInfos(outputs),
	}, nil
}

func onnxOCRTensorInfos(values []ort.InputOutputInfo) []builtinOCRTensorInfo {
	out := make([]builtinOCRTensorInfo, 0, len(values))
	for _, value := range values {
		dimensions := make([]int64, len(value.Dimensions))
		copy(dimensions, value.Dimensions)
		out = append(out, builtinOCRTensorInfo{
			Name:       value.Name,
			ValueType:  value.OrtValueType.String(),
			DataType:   value.DataType.String(),
			Dimensions: dimensions,
		})
	}
	return out
}

func configuredONNXNameWarnings(config builtinOCRConfig, detInfo, recInfo *builtinOCRModelInfo) []string {
	warnings := []string{}
	if config.DetInputName != "" && !ocrTensorNameExists(detInfo.Inputs, config.DetInputName) {
		warnings = append(warnings, "configured detector input name was not found in the model")
	}
	if config.DetOutputName != "" && !ocrTensorNameExists(detInfo.Outputs, config.DetOutputName) {
		warnings = append(warnings, "configured detector output name was not found in the model")
	}
	if config.RecInputName != "" && !ocrTensorNameExists(recInfo.Inputs, config.RecInputName) {
		warnings = append(warnings, "configured recognizer input name was not found in the model")
	}
	if config.RecOutputName != "" && !ocrTensorNameExists(recInfo.Outputs, config.RecOutputName) {
		warnings = append(warnings, "configured recognizer output name was not found in the model")
	}
	return warnings
}

func ocrTensorNameExists(values []builtinOCRTensorInfo, name string) bool {
	for _, value := range values {
		if value.Name == name {
			return true
		}
	}
	return false
}

func decodeONNXOCRImage(request builtinOCRRequest) (image.Image, error) {
	if len(request.Input) > 0 {
		if bytes.HasPrefix(bytes.TrimSpace(request.Input), []byte("%PDF-")) {
			return nil, fmt.Errorf("PDF bytes are not raster images; extract text with the pdf skill or render the target page to PNG/JPEG before OCR")
		}
		img, _, err := image.Decode(bytes.NewReader(request.Input))
		return img, err
	}
	if strings.TrimSpace(request.InputPath) == "" {
		return nil, fmt.Errorf("input path is required")
	}
	if strings.EqualFold(strings.TrimSpace(filepath.Ext(request.InputPath)), ".pdf") {
		return nil, fmt.Errorf("PDF files are not decoded directly by native OCR; extract text with the pdf skill or render the target page to PNG/JPEG first")
	}
	file, err := os.Open(request.InputPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	return img, err
}

func loadONNXOCRKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.HasSuffix(strings.ToLower(path), ".yml") || strings.HasSuffix(strings.ToLower(path), ".yaml") {
		keys, err := parseONNXOCRCharacterDict(text)
		if err != nil {
			return nil, err
		}
		return keys, nil
	}
	keys := strings.Split(text, "\n")
	if len(keys) > 0 && keys[len(keys)-1] == "" {
		keys = keys[:len(keys)-1]
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("keys file is empty")
	}
	return keys, nil
}

func parseONNXOCRCharacterDict(text string) ([]string, error) {
	lines := strings.Split(text, "\n")
	inDict := false
	dictIndent := 0
	keys := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if !inDict {
			if trimmed == "character_dict:" {
				inDict = true
				dictIndent = indent
			}
			continue
		}
		if indent <= dictIndent && !strings.HasPrefix(trimmed, "-") {
			break
		}
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		keys = append(keys, parseONNXOCRYAMLScalar(value))
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("character_dict is missing or empty")
	}
	return keys, nil
}

func parseONNXOCRYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		return value[1 : len(value)-1]
	}
	if value == "null" || value == "~" {
		return ""
	}
	return value
}

func onnxOCRModelIONames(inputOverride, outputOverride string, info *builtinOCRModelInfo) (string, string) {
	input := inputOverride
	output := outputOverride
	if input == "" && info != nil && len(info.Inputs) > 0 {
		input = info.Inputs[0].Name
	}
	if output == "" && info != nil && len(info.Outputs) > 0 {
		output = info.Outputs[0].Name
	}
	return input, output
}

func onnxOCRDetectorSize(width, height, limit int) (int, int) {
	if limit <= 0 {
		limit = 736
	}
	if width <= 0 || height <= 0 {
		return 32, 32
	}
	scale := 1.0
	if maxSide := maxInt(width, height); maxSide > limit {
		scale = float64(limit) / float64(maxSide)
	}
	outW := makeOCRDivisible(int(math.Round(float64(width)*scale)), 32)
	outH := makeOCRDivisible(int(math.Round(float64(height)*scale)), 32)
	return maxInt(outW, 32), maxInt(outH, 32)
}

func makeOCRDivisible(value, divisor int) int {
	if value <= 0 {
		return divisor
	}
	return int(math.Ceil(float64(value)/float64(divisor))) * divisor
}

func onnxOCRImageToDetectorTensor(img image.Image, width, height int) []float32 {
	data := make([]float32, 3*width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b := sampleOCRRGB(img, float64(x)*float64(img.Bounds().Dx())/float64(width), float64(y)*float64(img.Bounds().Dy())/float64(height))
			idx := y*width + x
			data[idx] = r/255.0/0.5 - 1.0
			data[width*height+idx] = g/255.0/0.5 - 1.0
			data[2*width*height+idx] = b/255.0/0.5 - 1.0
		}
	}
	return data
}

func onnxOCRImageToRecognizerTensor(img image.Image, box onnxOCRBox, width, height int) []float32 {
	data := make([]float32, 3*width*height)
	cropW := math.Max(1, box.X2-box.X1)
	cropH := math.Max(1, box.Y2-box.Y1)
	resizedW := onnxMinInt(width, maxInt(1, int(math.Ceil(float64(height)*cropW/cropH))))
	for y := 0; y < height; y++ {
		for x := 0; x < resizedW; x++ {
			srcX := box.X1 + (float64(x)+0.5)*cropW/float64(resizedW)
			srcY := box.Y1 + (float64(y)+0.5)*cropH/float64(height)
			r, g, b := sampleOCRRGB(img, srcX, srcY)
			idx := y*width + x
			data[idx] = r/255.0/0.5 - 1.0
			data[width*height+idx] = g/255.0/0.5 - 1.0
			data[2*width*height+idx] = b/255.0/0.5 - 1.0
		}
	}
	return data
}

func sampleOCRRGB(img image.Image, x, y float64) (float32, float32, float32) {
	bounds := img.Bounds()
	ix := bounds.Min.X + clampInt(int(math.Round(x)), 0, bounds.Dx()-1)
	iy := bounds.Min.Y + clampInt(int(math.Round(y)), 0, bounds.Dy()-1)
	r, g, b, _ := img.At(ix, iy).RGBA()
	return float32(r >> 8), float32(g >> 8), float32(b >> 8)
}

func onnxOCRScoreMap(value ort.Value) ([]float32, int, int, error) {
	tensor, ok := value.(*ort.Tensor[float32])
	if !ok {
		return nil, 0, 0, fmt.Errorf("expected float32 tensor, got %T", value)
	}
	shape := tensor.GetShape()
	data := tensor.GetData()
	switch len(shape) {
	case 4:
		if shape[1] == 1 {
			height, width := int(shape[2]), int(shape[3])
			return data[:width*height], width, height, nil
		}
		if shape[3] == 1 {
			height, width := int(shape[1]), int(shape[2])
			out := make([]float32, width*height)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					out[y*width+x] = data[(y*width+x)*int(shape[3])]
				}
			}
			return out, width, height, nil
		}
	case 3:
		height, width := int(shape[1]), int(shape[2])
		return data[:width*height], width, height, nil
	case 2:
		height, width := int(shape[0]), int(shape[1])
		return data[:width*height], width, height, nil
	}
	return nil, 0, 0, fmt.Errorf("unsupported detector output shape %v", []int64(shape))
}

func onnxOCRDetectBoxes(scores []float32, width, height, origW, origH int, settings builtinOCRSettings) []onnxOCRBox {
	binThreshold := settings.DetThreshold
	if binThreshold <= 0 {
		binThreshold = 0.3
	}
	boxThreshold := settings.BoxThreshold
	if boxThreshold <= 0 {
		boxThreshold = 0.5
	}
	maxCandidates := settings.MaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 1000
	}
	unclipRatio := settings.UnclipRatio
	if unclipRatio <= 0 {
		unclipRatio = 1.6
	}
	visited := make([]bool, len(scores))
	boxes := []onnxOCRBox{}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			start := y*width + x
			if visited[start] || float64(scores[start]) < binThreshold {
				continue
			}
			queue := []int{start}
			visited[start] = true
			minX, maxX, minY, maxY := x, x, y, y
			var sum float64
			count := 0
			for len(queue) > 0 {
				idx := queue[0]
				queue = queue[1:]
				cx, cy := idx%width, idx/width
				sum += float64(scores[idx])
				count++
				minX, maxX = onnxMinInt(minX, cx), maxInt(maxX, cx)
				minY, maxY = onnxMinInt(minY, cy), maxInt(maxY, cy)
				for _, n := range []int{idx - 1, idx + 1, idx - width, idx + width} {
					if n < 0 || n >= len(scores) || visited[n] || float64(scores[n]) < binThreshold {
						continue
					}
					nx, ny := n%width, n/width
					if absInt(nx-cx)+absInt(ny-cy) != 1 {
						continue
					}
					visited[n] = true
					queue = append(queue, n)
				}
			}
			if count < 8 || sum/float64(count) < boxThreshold || maxX-minX < 2 || maxY-minY < 2 {
				continue
			}
			pad := maxInt(2, int(math.Ceil(float64(maxInt(maxX-minX, maxY-minY))*(unclipRatio-1.0)/2.0)))
			minX, minY = maxInt(0, minX-pad), maxInt(0, minY-pad)
			maxX, maxY = onnxMinInt(width-1, maxX+pad), onnxMinInt(height-1, maxY+pad)
			boxes = append(boxes, onnxOCRBox{
				X1:    float64(minX) * float64(origW) / float64(width),
				Y1:    float64(minY) * float64(origH) / float64(height),
				X2:    float64(maxX+1) * float64(origW) / float64(width),
				Y2:    float64(maxY+1) * float64(origH) / float64(height),
				Score: sum / float64(count),
			})
		}
	}
	sort.SliceStable(boxes, func(i, j int) bool {
		if math.Abs(boxes[i].Y1-boxes[j].Y1) > 10 {
			return boxes[i].Y1 < boxes[j].Y1
		}
		return boxes[i].X1 < boxes[j].X1
	})
	if len(boxes) > maxCandidates {
		boxes = boxes[:maxCandidates]
	}
	return boxes
}

func onnxOCRRecognitionSize(info *builtinOCRModelInfo, settings builtinOCRSettings) (int, int, bool) {
	height, width := 48, 320
	if info == nil || len(info.Inputs) == 0 || len(info.Inputs[0].Dimensions) < 4 {
		height, width = overrideOCRRecognitionSize(height, width, settings)
		return height, width, false
	}
	dims := info.Inputs[0].Dimensions
	if dims[2] > 0 {
		height = int(dims[2])
	}
	dynamicWidth := dims[3] <= 0
	if dims[3] > 0 {
		width = int(dims[3])
	}
	if settings.RecWidth > 0 {
		dynamicWidth = false
	}
	height, width = overrideOCRRecognitionSize(height, width, settings)
	return height, width, dynamicWidth
}

func overrideOCRRecognitionSize(height, width int, settings builtinOCRSettings) (int, int) {
	if settings.RecHeight > 0 {
		height = settings.RecHeight
	}
	if settings.RecWidth > 0 {
		width = settings.RecWidth
	}
	return height, width
}

func onnxOCRRecognitionWidthForBox(box onnxOCRBox, height, baseWidth int, dynamic bool, settings builtinOCRSettings) int {
	if settings.RecWidth > 0 {
		return settings.RecWidth
	}
	if !dynamic {
		return baseWidth
	}
	cropW := math.Max(1, box.X2-box.X1)
	cropH := math.Max(1, box.Y2-box.Y1)
	width := makeOCRDivisible(int(math.Ceil(float64(height)*cropW/cropH)), 8)
	width = maxInt(baseWidth, width)
	maxWidth := settings.RecMaxWidth
	if maxWidth <= 0 {
		maxWidth = 1600
	}
	return onnxMinInt(width, maxWidth)
}

func onnxOCRDecodeRecognition(value ort.Value, keys []string) (string, float64, error) {
	tensor, ok := value.(*ort.Tensor[float32])
	if !ok {
		return "", 0, fmt.Errorf("expected float32 tensor, got %T", value)
	}
	shape := tensor.GetShape()
	data := tensor.GetData()
	steps, classes, offset := 0, 0, 0
	switch len(shape) {
	case 3:
		steps, classes = int(shape[1]), int(shape[2])
	case 2:
		steps, classes = int(shape[0]), int(shape[1])
	default:
		return "", 0, fmt.Errorf("unsupported recognizer output shape %v", []int64(shape))
	}
	parts := []string{}
	last := -1
	var confidenceSum float64
	confidenceCount := 0
	for step := 0; step < steps; step++ {
		bestIndex := 0
		bestScore := data[offset+step*classes]
		for class := 1; class < classes; class++ {
			score := data[offset+step*classes+class]
			if score > bestScore {
				bestIndex = class
				bestScore = score
			}
		}
		if bestIndex > 0 && bestIndex != last && bestIndex-1 < len(keys) {
			parts = append(parts, keys[bestIndex-1])
			confidenceSum += float64(bestScore)
			confidenceCount++
		}
		last = bestIndex
	}
	if confidenceCount == 0 {
		return "", 0, nil
	}
	return strings.Join(parts, ""), confidenceSum / float64(confidenceCount), nil
}

func destroyONNXValues(values []ort.Value) {
	for _, value := range values {
		if value != nil {
			_ = value.Destroy()
		}
	}
}

func onnxMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func clampInt(value, minValue, maxValue int) int {
	return maxInt(minValue, onnxMinInt(maxValue, value))
}
