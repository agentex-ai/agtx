package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

const defaultAudioInputMaxBytes int64 = 256 * 1024 * 1024

type builtinAudioInput struct {
	Path          string                     `json:"path,omitempty"`
	Input         string                     `json:"input,omitempty"`
	File          string                     `json:"file,omitempty"`
	Action        string                     `json:"action,omitempty"`
	Transcript    string                     `json:"transcript,omitempty"`
	Text          string                     `json:"text,omitempty"`
	Segments      []builtinAudioSegmentInput `json:"segments,omitempty"`
	LanguageHints []string                   `json:"language_hints,omitempty"`
	SpeakerHints  []string                   `json:"speaker_hints,omitempty"`
	MaxBytes      int64                      `json:"max_bytes,omitempty"`
}

type builtinAudioSegmentInput struct {
	StartMS int64  `json:"start_ms,omitempty"`
	EndMS   int64  `json:"end_ms,omitempty"`
	Speaker string `json:"speaker,omitempty"`
	Text    string `json:"text,omitempty"`
}

type builtinAudioOutput struct {
	Kind       string                `json:"kind"`
	Source     string                `json:"source,omitempty"`
	Action     string                `json:"action"`
	Audio      *builtinAudioInfo     `json:"audio,omitempty"`
	Transcript string                `json:"transcript,omitempty"`
	Segments   []builtinAudioSegment `json:"segments,omitempty"`
	Notes      *builtinAudioNotes    `json:"notes,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
	Metadata   map[string]any        `json:"metadata,omitempty"`
}

type builtinAudioInfo struct {
	Format        string  `json:"format"`
	Encoding      string  `json:"encoding,omitempty"`
	SampleRate    int     `json:"sample_rate,omitempty"`
	Channels      int     `json:"channels,omitempty"`
	BitsPerSample int     `json:"bits_per_sample,omitempty"`
	DurationMS    int64   `json:"duration_ms,omitempty"`
	Frames        int64   `json:"frames,omitempty"`
	DataBytes     int64   `json:"data_bytes,omitempty"`
	Peak          float64 `json:"peak,omitempty"`
	RMS           float64 `json:"rms,omitempty"`
	SilenceRatio  float64 `json:"silence_ratio,omitempty"`
}

type builtinAudioSegment struct {
	Index   int    `json:"index"`
	StartMS int64  `json:"start_ms,omitempty"`
	EndMS   int64  `json:"end_ms,omitempty"`
	Speaker string `json:"speaker,omitempty"`
	Text    string `json:"text"`
}

type builtinAudioNotes struct {
	Summary     string   `json:"summary,omitempty"`
	Decisions   []string `json:"decisions,omitempty"`
	ActionItems []string `json:"action_items,omitempty"`
	Questions   []string `json:"questions,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

type wavFormat struct {
	AudioFormat   uint16
	Channels      uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

func (s *Service) runBuiltinAudio(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	default:
	}
	input, rawInput, err := parseBuiltinAudioInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	output, err := s.buildBuiltinAudioOutput(input, rawInput, options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinAudioInput(options RunOptions) (builtinAudioInput, []byte, error) {
	input := builtinAudioInput{
		Path:       webFetchOptionValue(options.Args, "path", ""),
		Action:     webFetchOptionValue(options.Args, "action", ""),
		Transcript: webFetchOptionValue(options.Args, "transcript", ""),
		Text:       webFetchOptionValue(options.Args, "text", ""),
		MaxBytes:   int64(webFetchOptionInt(options.Args, "max-bytes", 0)),
	}
	if input.Path == "" {
		input.Path = firstNonEmpty(webFetchOptionValue(options.Args, "input", ""), webFetchOptionValue(options.Args, "file", ""))
	}
	if input.MaxBytes <= 0 {
		input.MaxBytes = int64(webFetchOptionInt(options.Args, "max_bytes", 0))
	}
	if len(options.Input) > 0 {
		var payload builtinAudioInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && audioPayloadHasValues(payload) {
			mergeAudioInput(&input, payload)
		} else if strings.TrimSpace(firstNonEmpty(input.Path, input.Transcript, input.Text)) == "" {
			if isLikelyWAV(options.Input) {
				return input, options.Input, nil
			}
			input.Transcript = strings.TrimSpace(string(options.Input))
		}
	}
	if strings.TrimSpace(input.Path) == "" {
		input.Path = firstAudioPathArg(options.Args)
	}
	if strings.TrimSpace(firstNonEmpty(input.Path, input.Transcript, input.Text)) == "" && len(input.Segments) == 0 && len(options.Input) == 0 {
		return input, nil, NewError(CodeInvalidArgument, "audio requires an audio file path, WAV bytes, transcript, or text", map[string]any{"expected": "path argument, --path, JSON path/transcript, or stdin"})
	}
	return input, options.Input, nil
}

func audioPayloadHasValues(input builtinAudioInput) bool {
	return strings.TrimSpace(firstNonEmpty(input.Path, input.Input, input.File, input.Action, input.Transcript, input.Text)) != "" || len(input.Segments) > 0 || len(input.LanguageHints) > 0 || len(input.SpeakerHints) > 0 || input.MaxBytes > 0
}

func mergeAudioInput(input *builtinAudioInput, payload builtinAudioInput) {
	if strings.TrimSpace(input.Path) == "" {
		input.Path = firstNonEmpty(payload.Path, payload.Input, payload.File)
	}
	if strings.TrimSpace(input.Action) == "" {
		input.Action = payload.Action
	}
	if strings.TrimSpace(input.Transcript) == "" {
		input.Transcript = payload.Transcript
	}
	if strings.TrimSpace(input.Text) == "" {
		input.Text = payload.Text
	}
	if len(input.Segments) == 0 {
		input.Segments = append(input.Segments, payload.Segments...)
	}
	if len(input.LanguageHints) == 0 {
		input.LanguageHints = append(input.LanguageHints, payload.LanguageHints...)
	}
	if len(input.SpeakerHints) == 0 {
		input.SpeakerHints = append(input.SpeakerHints, payload.SpeakerHints...)
	}
	if input.MaxBytes <= 0 {
		input.MaxBytes = payload.MaxBytes
	}
}

func (s *Service) buildBuiltinAudioOutput(input builtinAudioInput, rawInput []byte, options RunOptions) (builtinAudioOutput, error) {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		action = "analyze"
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultAudioInputMaxBytes
	}
	warnings := []string{}
	var audioInfo *builtinAudioInfo
	source := strings.TrimSpace(input.Path)
	var audioData []byte
	if source != "" {
		if strings.TrimSpace(source) != source || strings.ContainsRune(source, 0) {
			return builtinAudioOutput{}, NewError(CodeInvalidArgument, "audio input path is invalid", map[string]any{"path": source})
		}
		data, err := readFileLimited(source, maxBytes, "audio file")
		if err != nil {
			return builtinAudioOutput{}, err
		}
		audioData = data
	} else if isLikelyWAV(rawInput) {
		source = "stdin"
		audioData = rawInput
	}
	if len(audioData) > 0 {
		info, parseWarnings, err := parseBuiltinWAV(audioData)
		if err != nil {
			return builtinAudioOutput{}, err
		}
		audioInfo = &info
		warnings = append(warnings, parseWarnings...)
	}
	segments := normalizeAudioSegments(input.Segments)
	transcript := strings.TrimSpace(firstNonEmpty(input.Transcript, input.Text, transcriptFromAudioSegments(segments)))
	if action == "transcribe" && transcript == "" {
		warnings = append(warnings, "native ASR model is not bundled; provide transcript or segments, or install a future ASR backend")
	}
	if (action == "tts" || action == "synthesize") && strings.TrimSpace(input.Text) != "" {
		warnings = append(warnings, "native voice synthesis is not bundled; text was returned for an external TTS backend")
	}
	if transcript != "" && len(segments) == 0 {
		segments = segmentsFromTranscript(transcript)
	}
	var notes *builtinAudioNotes
	if transcript != "" && audioActionWantsNotes(action) {
		built := buildAudioNotes(transcript, segments)
		notes = &built
	}
	metadata := map[string]any{
		"no_python": true,
		"method":    "wav_inspection_and_transcript_notes",
	}
	if len(input.LanguageHints) > 0 {
		metadata["language_hints"] = input.LanguageHints
	}
	if len(input.SpeakerHints) > 0 {
		metadata["speaker_hints"] = input.SpeakerHints
	}
	return builtinAudioOutput{
		Kind:       "audio",
		Source:     source,
		Action:     action,
		Audio:      audioInfo,
		Transcript: transcript,
		Segments:   segments,
		Notes:      notes,
		Warnings:   warnings,
		Metadata:   metadata,
	}, nil
}

func parseBuiltinWAV(data []byte) (builtinAudioInfo, []string, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return builtinAudioInfo{}, nil, NewError(CodeInvalidArgument, "audio built-in currently supports RIFF/WAVE files", nil)
	}
	warnings := []string{}
	var format wavFormat
	var dataChunk []byte
	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkSize < 0 || chunkEnd > len(data) {
			break
		}
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return builtinAudioInfo{}, warnings, NewError(CodeInvalidArgument, "WAV fmt chunk is too small", nil)
			}
			chunk := data[chunkStart:chunkEnd]
			format = wavFormat{
				AudioFormat:   binary.LittleEndian.Uint16(chunk[0:2]),
				Channels:      binary.LittleEndian.Uint16(chunk[2:4]),
				SampleRate:    binary.LittleEndian.Uint32(chunk[4:8]),
				ByteRate:      binary.LittleEndian.Uint32(chunk[8:12]),
				BlockAlign:    binary.LittleEndian.Uint16(chunk[12:14]),
				BitsPerSample: binary.LittleEndian.Uint16(chunk[14:16]),
			}
		case "data":
			dataChunk = data[chunkStart:chunkEnd]
		}
		offset = chunkEnd
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if format.SampleRate == 0 || format.Channels == 0 || format.BitsPerSample == 0 {
		return builtinAudioInfo{}, warnings, NewError(CodeInvalidArgument, "WAV fmt chunk was not found", nil)
	}
	if dataChunk == nil {
		return builtinAudioInfo{}, warnings, NewError(CodeInvalidArgument, "WAV data chunk was not found", nil)
	}
	encoding := wavEncodingName(format.AudioFormat)
	info := builtinAudioInfo{
		Format:        "wav",
		Encoding:      encoding,
		SampleRate:    int(format.SampleRate),
		Channels:      int(format.Channels),
		BitsPerSample: int(format.BitsPerSample),
		DataBytes:     int64(len(dataChunk)),
	}
	if format.BlockAlign > 0 {
		info.Frames = int64(len(dataChunk) / int(format.BlockAlign))
	}
	if format.SampleRate > 0 && info.Frames > 0 {
		info.DurationMS = int64(math.Round(float64(info.Frames) * 1000 / float64(format.SampleRate)))
	} else if format.ByteRate > 0 {
		info.DurationMS = int64(math.Round(float64(len(dataChunk)) * 1000 / float64(format.ByteRate)))
	}
	peak, rms, silence, metricWarnings := wavSignalMetrics(dataChunk, format)
	info.Peak = roundAudioMetric(peak)
	info.RMS = roundAudioMetric(rms)
	info.SilenceRatio = roundAudioMetric(silence)
	warnings = append(warnings, metricWarnings...)
	return info, warnings, nil
}

func wavEncodingName(format uint16) string {
	switch format {
	case 1:
		return "pcm"
	case 3:
		return "ieee_float"
	default:
		return "unknown_" + strconv.Itoa(int(format))
	}
}

func wavSignalMetrics(data []byte, format wavFormat) (float64, float64, float64, []string) {
	warnings := []string{}
	bytesPerSample := int(format.BitsPerSample / 8)
	if bytesPerSample <= 0 || len(data) < bytesPerSample {
		return 0, 0, 0, warnings
	}
	if format.AudioFormat != 1 && !(format.AudioFormat == 3 && format.BitsPerSample == 32) {
		return 0, 0, 0, append(warnings, "signal metrics support PCM and 32-bit IEEE float WAV data")
	}
	count := 0
	silence := 0
	peak := 0.0
	sumSquares := 0.0
	for offset := 0; offset+bytesPerSample <= len(data); offset += bytesPerSample {
		value, ok := wavSampleValue(data[offset:offset+bytesPerSample], format)
		if !ok {
			continue
		}
		abs := math.Abs(value)
		if abs > peak {
			peak = abs
		}
		if abs < 0.02 {
			silence++
		}
		sumSquares += value * value
		count++
	}
	if count == 0 {
		return 0, 0, 0, warnings
	}
	rms := math.Sqrt(sumSquares / float64(count))
	return peak, rms, float64(silence) / float64(count), warnings
}

func wavSampleValue(sample []byte, format wavFormat) (float64, bool) {
	switch format.AudioFormat {
	case 1:
		switch format.BitsPerSample {
		case 8:
			return (float64(sample[0]) - 128) / 128, true
		case 16:
			return float64(int16(binary.LittleEndian.Uint16(sample))) / 32768, true
		case 24:
			value := int32(sample[0]) | int32(sample[1])<<8 | int32(sample[2])<<16
			if value&0x800000 != 0 {
				value |= ^0xffffff
			}
			return float64(value) / 8388608, true
		case 32:
			return float64(int32(binary.LittleEndian.Uint32(sample))) / 2147483648, true
		}
	case 3:
		if format.BitsPerSample == 32 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(sample))), true
		}
	}
	return 0, false
}

func normalizeAudioSegments(inputs []builtinAudioSegmentInput) []builtinAudioSegment {
	segments := []builtinAudioSegment{}
	for _, input := range inputs {
		text := normalizeWebFetchText(input.Text)
		if text == "" {
			continue
		}
		segments = append(segments, builtinAudioSegment{Index: len(segments) + 1, StartMS: input.StartMS, EndMS: input.EndMS, Speaker: strings.TrimSpace(input.Speaker), Text: text})
	}
	return segments
}

func transcriptFromAudioSegments(segments []builtinAudioSegment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.Speaker != "" {
			parts = append(parts, segment.Speaker+": "+segment.Text)
		} else {
			parts = append(parts, segment.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func segmentsFromTranscript(transcript string) []builtinAudioSegment {
	lines := strings.Split(transcript, "\n")
	segments := []builtinAudioSegment{}
	for _, line := range lines {
		line = normalizeWebFetchText(line)
		if line == "" {
			continue
		}
		speaker := ""
		text := line
		if index := strings.Index(line, ":"); index > 0 && index <= 40 {
			speaker = strings.TrimSpace(line[:index])
			text = strings.TrimSpace(line[index+1:])
		}
		if text == "" {
			continue
		}
		segments = append(segments, builtinAudioSegment{Index: len(segments) + 1, Speaker: speaker, Text: text})
	}
	return segments
}

func buildAudioNotes(transcript string, segments []builtinAudioSegment) builtinAudioNotes {
	sentences := audioSentences(transcript)
	notes := builtinAudioNotes{Summary: audioSummary(sentences), Keywords: audioKeywords(transcript, 8)}
	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		if containsAnyAudioPhrase(lower, []string{"decided", "decision", "resolved", "approved", "agreed", "we will", "we'll"}) {
			notes.Decisions = appendUniqueAudioText(notes.Decisions, sentence)
		}
		if containsAnyAudioPhrase(lower, []string{"action", "todo", "to do", "follow up", "follow-up", "owner", "assigned", "needs to", "will prepare", "will send", "will review"}) {
			notes.ActionItems = appendUniqueAudioText(notes.ActionItems, sentence)
		}
		if strings.HasSuffix(strings.TrimSpace(sentence), "?") || containsAnyAudioPhrase(lower, []string{"can we", "should we", "how do", "what is", "when will", "who will"}) {
			notes.Questions = appendUniqueAudioText(notes.Questions, sentence)
		}
	}
	if len(notes.ActionItems) == 0 {
		for _, segment := range segments {
			lower := strings.ToLower(segment.Text)
			if strings.Contains(lower, "will ") || strings.Contains(lower, "needs to") {
				notes.ActionItems = appendUniqueAudioText(notes.ActionItems, segment.Text)
			}
		}
	}
	return notes
}

func audioSummary(sentences []string) string {
	if len(sentences) == 0 {
		return ""
	}
	limit := 3
	if len(sentences) < limit {
		limit = len(sentences)
	}
	return strings.Join(sentences[:limit], " ")
}

func audioSentences(text string) []string {
	replacer := strings.NewReplacer("\r\n", "\n", "\r", "\n", ". ", ".\n", "? ", "?\n", "! ", "!\n")
	parts := strings.Split(replacer.Replace(text), "\n")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(webFetchSpacePattern.ReplaceAllString(part, " "))
		if len(part) < 4 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func audioKeywords(text string, limit int) []string {
	counts := map[string]int{}
	for _, token := range tokenize(text) {
		token = strings.ToLower(strings.Trim(token, " .,:;!?()[]{}\"'"))
		if len(token) < 4 || ignoredSearchTerm(token) || audioStopWord(token) {
			continue
		}
		counts[token]++
	}
	type pair struct {
		Text  string
		Count int
	}
	pairs := []pair{}
	for text, count := range counts {
		pairs = append(pairs, pair{Text: text, Count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		return pairs[i].Text < pairs[j].Text
	})
	out := []string{}
	for _, pair := range pairs {
		out = append(out, pair.Text)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func audioStopWord(value string) bool {
	switch value {
	case "that", "this", "there", "their", "about", "meeting", "audio", "speaker", "transcript", "will":
		return true
	default:
		return false
	}
}

func containsAnyAudioPhrase(value string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}

func appendUniqueAudioText(values []string, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return values
	}
	for _, value := range values {
		if strings.EqualFold(value, text) {
			return values
		}
	}
	return append(values, text)
}

func audioActionWantsNotes(action string) bool {
	switch action {
	case "analyze", "summarize", "summary", "meeting_notes", "notes", "transcribe":
		return true
	default:
		return false
	}
}

func firstAudioPathArg(args []string) string {
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
		if audioArgTakesValue(arg) {
			skipNext = !strings.Contains(arg, "=")
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		return arg
	}
	return ""
}

func audioArgTakesValue(arg string) bool {
	if webFetchArgTakesValue(arg) {
		return true
	}
	switch arg {
	case "--path", "__path", "--input", "__input", "--file", "__file", "--action", "__action", "--transcript", "__transcript", "--text", "__text", "--max-bytes", "__max_bytes", "--max_bytes":
		return true
	default:
		return false
	}
}

func isLikelyWAV(data []byte) bool {
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

func roundAudioMetric(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*10000) / 10000
}
