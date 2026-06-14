package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultImagenSize = 1024
const minImagenSize = 64
const maxImagenSize = 2048
const defaultImagenCount = 1
const maxImagenCount = 8

type builtinImagenInput struct {
	Prompt         string         `json:"prompt,omitempty"`
	Text           string         `json:"text,omitempty"`
	Action         string         `json:"action,omitempty"`
	Mode           string         `json:"mode,omitempty"`
	Style          string         `json:"style,omitempty"`
	NegativePrompt string         `json:"negative_prompt,omitempty"`
	OutputDir      string         `json:"output_dir,omitempty"`
	Output         string         `json:"output,omitempty"`
	Width          int            `json:"width,omitempty"`
	Height         int            `json:"height,omitempty"`
	Count          int            `json:"count,omitempty"`
	Seed           int64          `json:"seed,omitempty"`
	Format         string         `json:"format,omitempty"`
	Palette        []string       `json:"palette,omitempty"`
	Transparent    bool           `json:"transparent,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type builtinImagenOutput struct {
	Kind     string                 `json:"kind"`
	Action   string                 `json:"action"`
	Prompt   string                 `json:"prompt"`
	Style    string                 `json:"style,omitempty"`
	Seed     int64                  `json:"seed"`
	Assets   []builtinImagenAsset   `json:"assets"`
	Count    int                    `json:"count"`
	Warnings []string               `json:"warnings,omitempty"`
	Metadata map[string]any         `json:"metadata,omitempty"`
	Request  map[string]interface{} `json:"request,omitempty"`
}

type builtinImagenAsset struct {
	Index       int      `json:"index"`
	Path        string   `json:"path"`
	Format      string   `json:"format"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Bytes       int      `json:"bytes"`
	SHA256      string   `json:"sha256"`
	Seed        int64    `json:"seed"`
	Palette     []string `json:"palette,omitempty"`
	Description string   `json:"description,omitempty"`
}

func (s *Service) runBuiltinImagen(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	default:
	}
	input, err := parseBuiltinImagenInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	output, err := s.generateBuiltinImagen(ctx, input)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinImagenInput(options RunOptions) (builtinImagenInput, error) {
	input := builtinImagenInput{
		Prompt:         webFetchOptionValue(options.Args, "prompt", ""),
		Text:           webFetchOptionValue(options.Args, "text", ""),
		Action:         webFetchOptionValue(options.Args, "action", ""),
		Mode:           webFetchOptionValue(options.Args, "mode", ""),
		Style:          webFetchOptionValue(options.Args, "style", ""),
		NegativePrompt: webFetchOptionValue(options.Args, "negative-prompt", ""),
		OutputDir:      webFetchOptionValue(options.Args, "output-dir", ""),
		Output:         webFetchOptionValue(options.Args, "output", ""),
		Width:          webFetchOptionInt(options.Args, "width", 0),
		Height:         webFetchOptionInt(options.Args, "height", 0),
		Count:          webFetchOptionInt(options.Args, "count", 0),
		Seed:           int64(webFetchOptionInt(options.Args, "seed", 0)),
		Format:         webFetchOptionValue(options.Args, "format", ""),
		Transparent:    hasWebFetchBoolArg(options.Args, "transparent"),
	}
	if input.OutputDir == "" {
		input.OutputDir = webFetchOptionValue(options.Args, "output_dir", "")
	}
	if input.NegativePrompt == "" {
		input.NegativePrompt = webFetchOptionValue(options.Args, "negative_prompt", "")
	}
	if len(options.Input) > 0 {
		var payload builtinImagenInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && imagenPayloadHasValues(payload) {
			mergeImagenInput(&input, payload)
		} else if strings.TrimSpace(firstNonEmpty(input.Prompt, input.Text)) == "" {
			input.Prompt = strings.TrimSpace(string(options.Input))
		}
	}
	if strings.TrimSpace(firstNonEmpty(input.Prompt, input.Text)) == "" {
		input.Prompt = firstImagenPromptArg(options.Args)
	}
	if strings.TrimSpace(firstNonEmpty(input.Prompt, input.Text)) == "" {
		return input, NewError(CodeInvalidArgument, "imagen requires a prompt", map[string]any{"expected": "prompt argument, --prompt, JSON prompt, or stdin text"})
	}
	return input, nil
}

func imagenPayloadHasValues(input builtinImagenInput) bool {
	return strings.TrimSpace(firstNonEmpty(input.Prompt, input.Text, input.Action, input.Mode, input.Style, input.NegativePrompt, input.OutputDir, input.Output, input.Format)) != "" || input.Width > 0 || input.Height > 0 || input.Count > 0 || input.Seed != 0 || len(input.Palette) > 0 || input.Transparent || len(input.Metadata) > 0
}

func mergeImagenInput(input *builtinImagenInput, payload builtinImagenInput) {
	if strings.TrimSpace(firstNonEmpty(input.Prompt, input.Text)) == "" {
		input.Prompt = firstNonEmpty(payload.Prompt, payload.Text)
	}
	if strings.TrimSpace(input.Action) == "" {
		input.Action = payload.Action
	}
	if strings.TrimSpace(input.Mode) == "" {
		input.Mode = payload.Mode
	}
	if strings.TrimSpace(input.Style) == "" {
		input.Style = payload.Style
	}
	if strings.TrimSpace(input.NegativePrompt) == "" {
		input.NegativePrompt = payload.NegativePrompt
	}
	if strings.TrimSpace(input.OutputDir) == "" {
		input.OutputDir = payload.OutputDir
	}
	if strings.TrimSpace(input.Output) == "" {
		input.Output = payload.Output
	}
	if input.Width <= 0 {
		input.Width = payload.Width
	}
	if input.Height <= 0 {
		input.Height = payload.Height
	}
	if input.Count <= 0 {
		input.Count = payload.Count
	}
	if input.Seed == 0 {
		input.Seed = payload.Seed
	}
	if strings.TrimSpace(input.Format) == "" {
		input.Format = payload.Format
	}
	if len(input.Palette) == 0 {
		input.Palette = append(input.Palette, payload.Palette...)
	}
	input.Transparent = input.Transparent || payload.Transparent
	if len(input.Metadata) == 0 {
		input.Metadata = payload.Metadata
	}
}

func (s *Service) generateBuiltinImagen(ctx context.Context, input builtinImagenInput) (builtinImagenOutput, error) {
	prompt := strings.TrimSpace(firstNonEmpty(input.Prompt, input.Text))
	action := strings.ToLower(strings.TrimSpace(firstNonEmpty(input.Action, input.Mode)))
	if action == "" {
		action = "text_to_image"
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "png"
	}
	if format != "png" {
		return builtinImagenOutput{}, NewError(CodeInvalidArgument, "built-in imagen currently writes PNG assets", map[string]any{"format": format, "supported": "png"})
	}
	width := clampImagenSize(input.Width)
	height := clampImagenSize(input.Height)
	count := clampImagenCount(input.Count)
	seed := input.Seed
	if seed == 0 {
		seed = imagenSeed(prompt, input.Style, input.NegativePrompt)
	}
	outputDir, outputFile, err := s.imagenOutputPaths(input, count)
	if err != nil {
		return builtinImagenOutput{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return builtinImagenOutput{}, err
	}
	palette := imagenPalette(prompt, input.Style, input.Palette)
	warnings := []string{"built-in imagen uses deterministic local procedural PNG generation; install or configure a future model backend for photorealistic diffusion/image-to-video output"}
	if action == "image_to_video" || action == "text_to_video" || strings.Contains(action, "video") {
		warnings = append(warnings, "video generation is represented as storyboard PNG frames in the built-in runtime")
	}
	assets := make([]builtinImagenAsset, 0, count)
	for index := 0; index < count; index++ {
		select {
		case <-ctx.Done():
			return builtinImagenOutput{}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": 0})
		default:
		}
		assetSeed := seed + int64(index*7919)
		img := renderImagenPNG(width, height, prompt, input.Style, assetSeed, palette, input.Transparent)
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, img); err != nil {
			return builtinImagenOutput{}, err
		}
		path := outputFile
		if path == "" || count > 1 {
			path = filepath.Join(outputDir, imagenFilename(prompt, assetSeed, index))
		}
		if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
			return builtinImagenOutput{}, err
		}
		digest := sha256.Sum256(buffer.Bytes())
		assets = append(assets, builtinImagenAsset{
			Index:       index + 1,
			Path:        path,
			Format:      "png",
			Width:       width,
			Height:      height,
			Bytes:       buffer.Len(),
			SHA256:      hex.EncodeToString(digest[:]),
			Seed:        assetSeed,
			Palette:     palette,
			Description: imagenDescription(prompt, input.Style, action, index),
		})
	}
	manifestPath := filepath.Join(outputDir, "imagen-manifest-"+strconv.FormatInt(seed, 36)+".json")
	output := builtinImagenOutput{
		Kind:     "imagen",
		Action:   action,
		Prompt:   prompt,
		Style:    strings.TrimSpace(input.Style),
		Seed:     seed,
		Assets:   assets,
		Count:    len(assets),
		Warnings: warnings,
		Metadata: map[string]any{
			"no_python":     true,
			"method":        "procedural_png",
			"manifest_path": manifestPath,
		},
		Request: map[string]interface{}{
			"width":           width,
			"height":          height,
			"count":           count,
			"format":          format,
			"negative_prompt": strings.TrimSpace(input.NegativePrompt),
		},
	}
	if len(input.Metadata) > 0 {
		output.Metadata["request_metadata"] = input.Metadata
	}
	manifest, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return builtinImagenOutput{}, err
	}
	if err := os.WriteFile(manifestPath, append(manifest, '\n'), 0o644); err != nil {
		return builtinImagenOutput{}, err
	}
	return output, nil
}

func (s *Service) imagenOutputPaths(input builtinImagenInput, count int) (string, string, error) {
	output := strings.TrimSpace(input.Output)
	outputDir := strings.TrimSpace(input.OutputDir)
	if output != "" {
		if strings.ContainsRune(output, 0) || strings.TrimSpace(output) != output {
			return "", "", NewError(CodeInvalidArgument, "imagen output path is invalid", map[string]any{"path": output})
		}
		if count > 1 {
			return filepath.Dir(output), "", nil
		}
		return filepath.Dir(output), output, nil
	}
	if outputDir == "" {
		outputDir = filepath.Join(s.Paths.CacheDir, "artifacts", "imagen")
	}
	if strings.ContainsRune(outputDir, 0) || strings.TrimSpace(outputDir) != outputDir {
		return "", "", NewError(CodeInvalidArgument, "imagen output_dir is invalid", map[string]any{"output_dir": outputDir})
	}
	return outputDir, "", nil
}

func renderImagenPNG(width, height int, prompt, style string, seed int64, palette []string, transparent bool) image.Image {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	colors := parseImagenPalette(palette)
	if len(colors) < 3 {
		colors = []color.NRGBA{{R: 28, G: 32, B: 42, A: 255}, {R: 72, G: 148, B: 176, A: 255}, {R: 240, G: 186, B: 92, A: 255}}
	}
	for y := 0; y < height; y++ {
		fy := float64(y) / float64(maxImagenInt(height-1, 1))
		for x := 0; x < width; x++ {
			fx := float64(x) / float64(maxImagenInt(width-1, 1))
			wave := 0.5 + 0.5*math.Sin((fx*float64(3+rng.Intn(5))+fy*float64(2+rng.Intn(4)))*math.Pi+float64(seed%997)/97)
			base := blendImagenColor(colors[0], colors[1], fx)
			accent := blendImagenColor(base, colors[2%len(colors)], wave*0.55)
			if transparent && x < width/24 || transparent && y < height/24 || transparent && x > width-width/24 || transparent && y > height-height/24 {
				accent.A = 0
			}
			img.SetNRGBA(x, y, accent)
		}
	}
	shapeCount := 18 + len(deepResearchTokens(prompt+" "+style))%18
	for i := 0; i < shapeCount; i++ {
		cx := rng.Intn(width)
		cy := rng.Intn(height)
		radius := minImagenInt(width, height)/(10+rng.Intn(12)) + rng.Intn(maxImagenInt(2, minImagenInt(width, height)/12))
		col := colors[rng.Intn(len(colors))]
		col.A = uint8(80 + rng.Intn(120))
		drawImagenCircle(img, cx, cy, radius, col)
	}
	drawImagenPromptMarks(img, prompt, seed, colors)
	return img
}

func drawImagenCircle(img *image.NRGBA, cx, cy, radius int, col color.NRGBA) {
	if radius <= 0 {
		return
	}
	bounds := img.Bounds()
	r2 := radius * radius
	for y := maxImagenInt(bounds.Min.Y, cy-radius); y < minImagenInt(bounds.Max.Y, cy+radius); y++ {
		for x := maxImagenInt(bounds.Min.X, cx-radius); x < minImagenInt(bounds.Max.X, cx+radius); x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > r2 {
				continue
			}
			old := img.NRGBAAt(x, y)
			img.SetNRGBA(x, y, overImagenColor(old, col))
		}
	}
}

func drawImagenPromptMarks(img *image.NRGBA, prompt string, seed int64, colors []color.NRGBA) {
	tokens := deepResearchTokens(prompt)
	if len(tokens) == 0 {
		return
	}
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	barH := maxImagenInt(3, h/96)
	margin := maxImagenInt(8, w/48)
	for index, token := range tokens {
		if index >= 16 {
			break
		}
		hash := imagenSeed(token, strconv.FormatInt(seed, 10), "")
		x := margin + int(hash%int64(maxImagenInt(1, w-margin*2)))
		y := h - margin - (index+1)*(barH+maxImagenInt(2, h/240))
		length := maxImagenInt(w/12, int((hash>>8)%int64(maxImagenInt(1, w/3))))
		col := colors[index%len(colors)]
		col.A = 210
		for yy := maxImagenInt(0, y); yy < minImagenInt(h, y+barH); yy++ {
			for xx := maxImagenInt(0, x); xx < minImagenInt(w, x+length); xx++ {
				img.SetNRGBA(xx, yy, overImagenColor(img.NRGBAAt(xx, yy), col))
			}
		}
	}
}

func imagenPalette(prompt, style string, configured []string) []string {
	out := []string{}
	for _, item := range configured {
		if parsed, ok := parseHexColor(item); ok {
			out = append(out, formatHexColor(parsed))
		}
	}
	if len(out) >= 3 {
		return out
	}
	seed := imagenSeed(prompt, style, "palette")
	rng := rand.New(rand.NewSource(seed))
	for len(out) < 5 {
		hue := rng.Float64()
		sat := 0.45 + rng.Float64()*0.35
		light := 0.38 + rng.Float64()*0.32
		out = append(out, formatHexColor(hslToNRGBA(hue, sat, light)))
	}
	return out
}

func parseImagenPalette(values []string) []color.NRGBA {
	out := []color.NRGBA{}
	for _, value := range values {
		if parsed, ok := parseHexColor(value); ok {
			out = append(out, parsed)
		}
	}
	return out
}

func parseHexColor(value string) (color.NRGBA, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return color.NRGBA{}, false
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(parsed >> 16), G: uint8(parsed >> 8), B: uint8(parsed), A: 255}, true
}

func hslToNRGBA(h, s, l float64) color.NRGBA {
	var r, g, b float64
	if s == 0 {
		r, g, b = l, l, l
	} else {
		q := l * (1 + s)
		if l >= 0.5 {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hueToRGB(p, q, h+1.0/3.0)
		g = hueToRGB(p, q, h)
		b = hueToRGB(p, q, h-1.0/3.0)
	}
	return color.NRGBA{R: uint8(clampFloat(r)*255 + 0.5), G: uint8(clampFloat(g)*255 + 0.5), B: uint8(clampFloat(b)*255 + 0.5), A: 255}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}

func blendImagenColor(left, right color.NRGBA, amount float64) color.NRGBA {
	amount = clampFloat(amount)
	inv := 1 - amount
	return color.NRGBA{
		R: uint8(float64(left.R)*inv + float64(right.R)*amount),
		G: uint8(float64(left.G)*inv + float64(right.G)*amount),
		B: uint8(float64(left.B)*inv + float64(right.B)*amount),
		A: uint8(float64(left.A)*inv + float64(right.A)*amount),
	}
}

func overImagenColor(base, over color.NRGBA) color.NRGBA {
	a := float64(over.A) / 255
	inv := 1 - a
	return color.NRGBA{R: uint8(float64(over.R)*a + float64(base.R)*inv), G: uint8(float64(over.G)*a + float64(base.G)*inv), B: uint8(float64(over.B)*a + float64(base.B)*inv), A: maxAlpha(base.A, over.A)}
}

func maxAlpha(left, right uint8) uint8 {
	if left > right {
		return left
	}
	return right
}

func clampFloat(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func formatHexColor(col color.NRGBA) string {
	return "#" + hex.EncodeToString([]byte{col.R, col.G, col.B})
}

func imagenSeed(parts ...string) int64 {
	hash := fnv.New64a()
	for _, part := range parts {
		_, _ = hash.Write([]byte(strings.TrimSpace(part)))
		_, _ = hash.Write([]byte{0})
	}
	value := int64(hash.Sum64() & 0x7fffffffffffffff)
	if value == 0 {
		return time.Now().UnixNano()
	}
	return value
}

func imagenFilename(prompt string, seed int64, index int) string {
	base := sanitizeImagenFilename(prompt)
	if base == "" {
		base = "image"
	}
	return base + "-" + strconv.FormatInt(seed, 36) + "-" + strconv.Itoa(index+1) + ".png"
}

func sanitizeImagenFilename(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		ok := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if ok {
			builder.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
		if builder.Len() >= 48 {
			break
		}
	}
	return strings.Trim(builder.String(), "-")
}

func imagenDescription(prompt, style, action string, index int) string {
	parts := []string{"Procedural PNG", "prompt=" + strconv.Quote(prompt)}
	if strings.TrimSpace(style) != "" {
		parts = append(parts, "style="+strconv.Quote(style))
	}
	parts = append(parts, "action="+action, "variant="+strconv.Itoa(index+1))
	return strings.Join(parts, "; ")
}

func firstImagenPromptArg(args []string) string {
	values := []string{}
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
		if imagenArgTakesValue(arg) {
			skipNext = !strings.Contains(arg, "=")
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		values = append(values, arg)
	}
	return strings.Join(values, " ")
}

func imagenArgTakesValue(arg string) bool {
	if webSearchArgTakesValue(arg) {
		return true
	}
	switch arg {
	case "--prompt", "__prompt", "--text", "__text", "--action", "__action", "--mode", "__mode", "--style", "__style", "--negative-prompt", "__negative_prompt", "--negative_prompt", "--output-dir", "__output_dir", "--output_dir", "--output", "__output", "--width", "__width", "--height", "__height", "--count", "__count", "--seed", "__seed", "--format", "__format":
		return true
	default:
		return false
	}
}

func clampImagenSize(value int) int {
	if value <= 0 {
		return defaultImagenSize
	}
	if value < minImagenSize {
		return minImagenSize
	}
	if value > maxImagenSize {
		return maxImagenSize
	}
	return value
}

func clampImagenCount(value int) int {
	if value <= 0 {
		return defaultImagenCount
	}
	if value > maxImagenCount {
		return maxImagenCount
	}
	return value
}

func minImagenInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxImagenInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
