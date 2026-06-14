package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAudioWithoutInstallInspectsWAVAndBuildsNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting.wav")
	writeWAVFixture(t, path, 16000, 1, 800)

	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"path":"` + filepath.ToSlash(path) + `","transcript":"Alice: We decided to ship the audio pack today.\nBob: Action item: Bob will review the telemetry records.\nAlice: Should we add ASR later?","action":"meeting_notes"}`)
	result, err := service.RunSkill(context.Background(), "audio", nil, input)
	if err != nil {
		t.Fatalf("run audio: %v result=%#v", err, result)
	}
	if result.Name != "audio" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Meter != "minute" {
		t.Fatalf("expected minute usage event: %#v", result.UsageEvents)
	}
	var output struct {
		Kind  string `json:"kind"`
		Audio struct {
			Format        string  `json:"format"`
			SampleRate    int     `json:"sample_rate"`
			Channels      int     `json:"channels"`
			BitsPerSample int     `json:"bits_per_sample"`
			DurationMS    int64   `json:"duration_ms"`
			Peak          float64 `json:"peak"`
			RMS           float64 `json:"rms"`
		} `json:"audio"`
		Segments []struct {
			Speaker string `json:"speaker"`
			Text    string `json:"text"`
		} `json:"segments"`
		Notes struct {
			Summary     string   `json:"summary"`
			Decisions   []string `json:"decisions"`
			ActionItems []string `json:"action_items"`
			Questions   []string `json:"questions"`
			Keywords    []string `json:"keywords"`
		} `json:"notes"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode audio output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "audio" || output.Audio.Format != "wav" || output.Audio.SampleRate != 16000 || output.Audio.Channels != 1 || output.Audio.BitsPerSample != 16 || output.Audio.DurationMS != 800 {
		t.Fatalf("unexpected audio metadata: %#v", output.Audio)
	}
	if output.Audio.Peak <= 0 || output.Audio.RMS <= 0 {
		t.Fatalf("expected signal metrics: %#v", output.Audio)
	}
	if len(output.Segments) != 3 || output.Segments[0].Speaker != "Alice" {
		t.Fatalf("unexpected segments: %#v", output.Segments)
	}
	if len(output.Notes.Decisions) == 0 || len(output.Notes.ActionItems) == 0 || len(output.Notes.Questions) == 0 || !strings.Contains(output.Notes.Summary, "ship the audio pack") {
		t.Fatalf("unexpected notes: %#v", output.Notes)
	}
}

func TestRunAudioSummarizesTranscriptOnly(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"text":"Lead: The team approved the launch plan. Ops: Action item: prepare the release notes. QA: Who will verify macOS?","action":"summarize"}`)
	result, err := service.RunSkill(context.Background(), "audio", nil, input)
	if err != nil {
		t.Fatalf("run transcript audio: %v result=%#v", err, result)
	}
	if !strings.Contains(result.Stdout, "release notes") || !strings.Contains(result.Stdout, "approved") {
		t.Fatalf("expected transcript notes output: %s", result.Stdout)
	}
}

func writeWAVFixture(t *testing.T, path string, sampleRate, channels int, durationMS int) {
	t.Helper()
	frames := sampleRate * durationMS / 1000
	data := bytes.Buffer{}
	for frame := 0; frame < frames; frame++ {
		value := int16(math.Sin(float64(frame)*2*math.Pi*440/float64(sampleRate)) * 12000)
		for channel := 0; channel < channels; channel++ {
			if err := binary.Write(&data, binary.LittleEndian, value); err != nil {
				t.Fatalf("write sample: %v", err)
			}
		}
	}
	byteRate := sampleRate * channels * 2
	blockAlign := channels * 2
	out := bytes.Buffer{}
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+data.Len()))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&out, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&out, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&out, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(data.Len()))
	out.Write(data.Bytes())
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatalf("write wav fixture: %v", err)
	}
}
