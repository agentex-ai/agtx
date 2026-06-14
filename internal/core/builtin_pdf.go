package core

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const defaultPDFInputMaxBytes int64 = 128 * 1024 * 1024
const defaultPDFStreamMaxBytes int64 = 32 * 1024 * 1024
const defaultPDFMaxStreams = 4096

var pdfPageTypePattern = regexp.MustCompile(`/Type\s*/Page\b`)

type builtinPDFInput struct {
	Path       string `json:"path,omitempty"`
	Input      string `json:"input,omitempty"`
	File       string `json:"file,omitempty"`
	Action     string `json:"action,omitempty"`
	MaxStreams int    `json:"max_streams,omitempty"`
}

type builtinPDFOutput struct {
	Kind      string            `json:"kind"`
	Source    string            `json:"source,omitempty"`
	Action    string            `json:"action"`
	Text      string            `json:"text,omitempty"`
	PageCount int               `json:"page_count,omitempty"`
	Streams   int               `json:"streams"`
	Bytes     int               `json:"bytes"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Warnings  []string          `json:"warnings,omitempty"`
}

type pdfContentStream struct {
	Data     []byte
	Filtered bool
	Filter   string
}

type pdfTextToken struct {
	Kind  string
	Value string
}

func (s *Service) runBuiltinPDF(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	default:
	}
	input, err := parseBuiltinPDFInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	data, source, err := builtinPDFBytes(input, options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		action = "extract"
	}
	output, err := extractBuiltinPDF(data, source, action, input.MaxStreams)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(encoded, '\n'))}, nil
}

func parseBuiltinPDFInput(options RunOptions) (builtinPDFInput, error) {
	input := builtinPDFInput{
		Path:       officeOptionValue(options.Args, "path", ""),
		Action:     officeOptionValue(options.Args, "action", ""),
		MaxStreams: officeOptionInt(options.Args, "max-streams", 0),
	}
	if input.Path == "" {
		input.Path = officeOptionValue(options.Args, "input", "")
	}
	if input.Path == "" {
		input.Path = officeOptionValue(options.Args, "file", "")
	}
	if len(options.Input) > 0 {
		var payload builtinPDFInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && (payload.Path != "" || payload.Input != "" || payload.File != "" || payload.Action != "" || payload.MaxStreams != 0) {
			if strings.TrimSpace(input.Path) == "" {
				input.Path = firstNonEmpty(payload.Path, payload.Input, payload.File)
			}
			if strings.TrimSpace(input.Action) == "" {
				input.Action = payload.Action
			}
			if input.MaxStreams <= 0 {
				input.MaxStreams = payload.MaxStreams
			}
		}
	}
	if strings.TrimSpace(input.Path) == "" {
		input.Path = firstPDFPathArg(options.Args)
	}
	return input, nil
}

func builtinPDFBytes(input builtinPDFInput, options RunOptions) ([]byte, string, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		if len(options.Input) == 0 {
			return nil, "", NewError(CodeInvalidArgument, "pdf skill requires a file path or --input bytes", map[string]any{"expected": "path argument, --path, JSON path, or CLI --input"})
		}
		return options.Input, "stdin", nil
	}
	if strings.TrimSpace(path) != path || strings.ContainsRune(path, 0) {
		return nil, "", NewError(CodeInvalidArgument, "pdf input path is invalid", map[string]any{"path": path})
	}
	data, err := readFileLimited(path, defaultPDFInputMaxBytes, "pdf document")
	if err != nil {
		return nil, path, err
	}
	return data, path, nil
}

func extractBuiltinPDF(data []byte, source, action string, maxStreams int) (builtinPDFOutput, error) {
	if int64(len(data)) > defaultPDFInputMaxBytes {
		return builtinPDFOutput{}, NewError(CodeSizeLimitExceeded, "pdf document exceeds configured size limit", map[string]any{"size": len(data), "limit": defaultPDFInputMaxBytes})
	}
	if !bytes.Contains(data[:minPDFInt(len(data), 1024)], []byte("%PDF-")) {
		return builtinPDFOutput{}, NewError(CodeInvalidArgument, "input is not a PDF document", map[string]any{"source": source})
	}
	if maxStreams <= 0 {
		maxStreams = defaultPDFMaxStreams
	}
	streams, warnings, err := extractPDFContentStreams(data, maxStreams)
	if err != nil {
		return builtinPDFOutput{}, err
	}
	texts := make([]string, 0, len(streams))
	for _, stream := range streams {
		text := extractPDFTextFromContent(stream.Data)
		if strings.TrimSpace(text) != "" {
			texts = append(texts, text)
		}
	}
	text := joinOfficeTexts(texts)
	if strings.TrimSpace(text) == "" {
		warnings = append(warnings, "no text was extracted; scanned or image-only PDFs require OCR")
	}
	return builtinPDFOutput{
		Kind:      "pdf",
		Source:    source,
		Action:    action,
		Text:      text,
		PageCount: estimatePDFPageCount(data),
		Streams:   len(streams),
		Bytes:     len(data),
		Warnings:  warnings,
	}, nil
}

func extractPDFContentStreams(data []byte, maxStreams int) ([]pdfContentStream, []string, error) {
	streams := make([]pdfContentStream, 0)
	warnings := []string{}
	offset := 0
	for offset < len(data) {
		if len(streams) >= maxStreams {
			warnings = append(warnings, "stream limit reached; output may be truncated")
			break
		}
		streamIndex := bytes.Index(data[offset:], []byte("stream"))
		if streamIndex < 0 {
			break
		}
		streamIndex += offset
		streamStart := streamIndex + len("stream")
		if streamStart < len(data) && data[streamStart] == '\r' {
			streamStart++
			if streamStart < len(data) && data[streamStart] == '\n' {
				streamStart++
			}
		} else if streamStart < len(data) && data[streamStart] == '\n' {
			streamStart++
		}
		endIndex := bytes.Index(data[streamStart:], []byte("endstream"))
		if endIndex < 0 {
			break
		}
		endIndex += streamStart
		raw := bytes.TrimRight(data[streamStart:endIndex], "\r\n")
		if int64(len(raw)) > defaultPDFStreamMaxBytes {
			warnings = append(warnings, "skipped oversized PDF stream")
			offset = endIndex + len("endstream")
			continue
		}
		dict := pdfDictionaryBeforeStream(data, streamIndex)
		decoded, filtered, filterName, err := decodePDFStream(raw, dict)
		if err != nil {
			warnings = append(warnings, err.Error())
			offset = endIndex + len("endstream")
			continue
		}
		if decoded != nil {
			streams = append(streams, pdfContentStream{Data: decoded, Filtered: filtered, Filter: filterName})
		}
		offset = endIndex + len("endstream")
	}
	return streams, warnings, nil
}

func pdfDictionaryBeforeStream(data []byte, streamIndex int) []byte {
	startSearch := maxPDFInt(0, streamIndex-8192)
	window := data[startSearch:streamIndex]
	start := bytes.LastIndex(window, []byte("<<"))
	if start < 0 {
		return nil
	}
	return window[start:]
}

func decodePDFStream(raw, dict []byte) ([]byte, bool, string, error) {
	filterName := pdfFilterName(dict)
	switch filterName {
	case "", "none":
		return raw, false, "", nil
	case "FlateDecode", "Fl":
		reader, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, true, filterName, NewError(CodeInvalidArgument, "failed to decode FlateDecode PDF stream", err.Error())
		}
		defer reader.Close()
		decoded, err := readAllLimited(reader, defaultPDFStreamMaxBytes, "pdf stream")
		if err != nil {
			return nil, true, filterName, err
		}
		return decoded, true, filterName, nil
	default:
		return nil, true, filterName, NewError(CodeInvalidArgument, "skipped unsupported PDF stream filter", map[string]any{"filter": filterName})
	}
}

func pdfFilterName(dict []byte) string {
	text := string(dict)
	if !strings.Contains(text, "/Filter") {
		return ""
	}
	for _, name := range []string{"FlateDecode", "Fl", "LZWDecode", "ASCII85Decode", "DCTDecode", "JPXDecode", "CCITTFaxDecode"} {
		if strings.Contains(text, "/"+name) {
			return name
		}
	}
	return "unknown"
}

func extractPDFTextFromContent(data []byte) string {
	var builder strings.Builder
	operands := []pdfTextToken{}
	inText := false
	for index := 0; index < len(data); {
		token, next, ok := nextPDFContentToken(data, index)
		if !ok {
			break
		}
		index = next
		if token.Kind == "word" && !isPDFNumber(token.Value) {
			switch token.Value {
			case "BT":
				inText = true
			case "ET":
				appendPDFNewline(&builder)
				inText = false
			case "Tj":
				if inText {
					appendPDFText(&builder, lastPDFStringOperand(operands))
				}
			case "TJ":
				if inText {
					for _, value := range pdfStringOperands(operands) {
						appendPDFText(&builder, value)
					}
				}
			case "'":
				if inText {
					appendPDFNewline(&builder)
					appendPDFText(&builder, lastPDFStringOperand(operands))
				}
			case "\"":
				if inText {
					appendPDFNewline(&builder)
					appendPDFText(&builder, lastPDFStringOperand(operands))
				}
			case "T*", "Td", "TD":
				if inText {
					appendPDFNewline(&builder)
				}
			}
			operands = operands[:0]
			continue
		}
		operands = append(operands, token)
	}
	return normalizeOfficeText(builder.String())
}

func nextPDFContentToken(data []byte, index int) (pdfTextToken, int, bool) {
	index = skipPDFSpace(data, index)
	if index >= len(data) {
		return pdfTextToken{}, index, false
	}
	switch data[index] {
	case '(':
		value, next := parsePDFLiteralString(data, index)
		return pdfTextToken{Kind: "string", Value: value}, next, true
	case '<':
		if index+1 < len(data) && data[index+1] == '<' {
			return pdfTextToken{Kind: "delimiter", Value: "<<"}, index + 2, true
		}
		value, next := parsePDFHexString(data, index)
		return pdfTextToken{Kind: "string", Value: value}, next, true
	case '[':
		return pdfTextToken{Kind: "delimiter", Value: "["}, index + 1, true
	case ']':
		return pdfTextToken{Kind: "delimiter", Value: "]"}, index + 1, true
	case '/':
		next := index + 1
		for next < len(data) && !isPDFSpace(data[next]) && !isPDFDelimiter(data[next]) {
			next++
		}
		return pdfTextToken{Kind: "name", Value: string(data[index:next])}, next, true
	default:
		next := index
		for next < len(data) && !isPDFSpace(data[next]) && !isPDFDelimiter(data[next]) {
			next++
		}
		return pdfTextToken{Kind: "word", Value: string(data[index:next])}, next, true
	}
}

func skipPDFSpace(data []byte, index int) int {
	for index < len(data) {
		if data[index] == '%' {
			for index < len(data) && data[index] != '\n' && data[index] != '\r' {
				index++
			}
			continue
		}
		if !isPDFSpace(data[index]) {
			break
		}
		index++
	}
	return index
}

func parsePDFLiteralString(data []byte, index int) (string, int) {
	index++
	depth := 1
	buf := []byte{}
	for index < len(data) && depth > 0 {
		ch := data[index]
		index++
		switch ch {
		case '(':
			depth++
			buf = append(buf, ch)
		case ')':
			depth--
			if depth > 0 {
				buf = append(buf, ch)
			}
		case '\\':
			if index >= len(data) {
				break
			}
			escaped := data[index]
			index++
			switch escaped {
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case '(', ')', '\\':
				buf = append(buf, escaped)
			case '\r':
				if index < len(data) && data[index] == '\n' {
					index++
				}
			case '\n':
				// line continuation
			default:
				if escaped >= '0' && escaped <= '7' {
					octal := []byte{escaped}
					for len(octal) < 3 && index < len(data) && data[index] >= '0' && data[index] <= '7' {
						octal = append(octal, data[index])
						index++
					}
					value, err := strconv.ParseInt(string(octal), 8, 16)
					if err == nil {
						buf = append(buf, byte(value))
					}
				} else {
					buf = append(buf, escaped)
				}
			}
		default:
			buf = append(buf, ch)
		}
	}
	return decodePDFTextBytes(buf), index
}

func parsePDFHexString(data []byte, index int) (string, int) {
	index++
	start := index
	for index < len(data) && data[index] != '>' {
		index++
	}
	raw := make([]byte, 0, index-start)
	for _, ch := range data[start:index] {
		if !isPDFSpace(ch) {
			raw = append(raw, ch)
		}
	}
	if len(raw)%2 == 1 {
		raw = append(raw, '0')
	}
	decoded, err := hex.DecodeString(string(raw))
	if err != nil {
		decoded = nil
	}
	if index < len(data) && data[index] == '>' {
		index++
	}
	return decodePDFTextBytes(decoded), index
}

func decodePDFTextBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
		return decodeUTF16(data[2:], true)
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		return decodeUTF16(data[2:], false)
	}
	if len(data)%2 == 0 && len(data) >= 4 && data[0] == 0 && data[2] == 0 {
		return decodeUTF16(data, true)
	}
	if utf8.Valid(data) {
		return string(data)
	}
	runes := make([]rune, 0, len(data))
	for _, b := range data {
		if b == 0 {
			continue
		}
		runes = append(runes, pdfWinAnsiRune(b))
	}
	return string(runes)
}

func decodeUTF16(data []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for index := 0; index+1 < len(data); index += 2 {
		if bigEndian {
			units = append(units, uint16(data[index])<<8|uint16(data[index+1]))
		} else {
			units = append(units, uint16(data[index+1])<<8|uint16(data[index]))
		}
	}
	return string(utf16.Decode(units))
}

func pdfWinAnsiRune(b byte) rune {
	return rune(b)
}

func appendPDFText(builder *strings.Builder, value string) {
	if value == "" {
		return
	}
	builder.WriteString(value)
}

func appendPDFNewline(builder *strings.Builder) {
	text := builder.String()
	if text != "" && !strings.HasSuffix(text, "\n") {
		builder.WriteByte('\n')
	}
}

func lastPDFStringOperand(operands []pdfTextToken) string {
	for index := len(operands) - 1; index >= 0; index-- {
		if operands[index].Kind == "string" {
			return operands[index].Value
		}
	}
	return ""
}

func pdfStringOperands(operands []pdfTextToken) []string {
	values := []string{}
	for _, operand := range operands {
		if operand.Kind == "string" {
			values = append(values, operand.Value)
		}
	}
	return values
}

func isPDFNumber(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

func isPDFSpace(ch byte) bool {
	switch ch {
	case 0, '\t', '\n', '\f', '\r', ' ':
		return true
	default:
		return false
	}
}

func isPDFDelimiter(ch byte) bool {
	switch ch {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}

func estimatePDFPageCount(data []byte) int {
	return len(pdfPageTypePattern.FindAll(data, -1))
}

func firstPDFPathArg(args []string) string {
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
		if pdfArgTakesValue(arg) {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		return arg
	}
	return ""
}

func pdfArgTakesValue(arg string) bool {
	if officeArgTakesValue(arg) {
		return true
	}
	switch arg {
	case "--max-streams", "--max_streams", "__max_streams":
		return true
	default:
		return false
	}
}

func maxPDFInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minPDFInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
