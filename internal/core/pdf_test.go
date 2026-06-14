package core

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPDFWithoutInstallUsesBuiltinTextExtractor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pdf")
	writePDFFixture(t, path, "BT /F1 12 Tf 72 720 Td (Hello PDF) Tj T* [(Line) 120 ( two)] TJ ET", false)

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "pdf", []string{path}, nil)
	if err != nil {
		t.Fatalf("run pdf: %v result=%#v", err, result)
	}
	if result.Name != "pdf" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Meter != "page" {
		t.Fatalf("expected page usage event: %#v", result.UsageEvents)
	}
	var output struct {
		Kind      string   `json:"kind"`
		Text      string   `json:"text"`
		PageCount int      `json:"page_count"`
		Streams   int      `json:"streams"`
		Warnings  []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode pdf output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "pdf" || output.PageCount != 1 || output.Streams != 1 {
		t.Fatalf("unexpected pdf metadata: %#v", output)
	}
	if !strings.Contains(output.Text, "Hello PDF") || !strings.Contains(output.Text, "Line two") || len(output.Warnings) != 0 {
		t.Fatalf("unexpected pdf text output: %#v", output)
	}
}

func TestRunPDFExtractsFlateDecodeStreamFromJSONInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compressed.pdf")
	writePDFFixture(t, path, "BT /F1 10 Tf 36 700 Td (Compressed text) Tj ET", true)

	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"path":"` + filepath.ToSlash(path) + `"}`)
	result, err := service.RunSkill(context.Background(), "pdf", nil, input)
	if err != nil {
		t.Fatalf("run compressed pdf: %v result=%#v", err, result)
	}
	if !strings.Contains(result.Stdout, "Compressed text") {
		t.Fatalf("expected compressed text in output: %s", result.Stdout)
	}
}

func writePDFFixture(t *testing.T, path, content string, flate bool) {
	t.Helper()
	stream := []byte(content)
	filter := ""
	if flate {
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := writer.Write(stream); err != nil {
			t.Fatalf("compress pdf stream: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close compressor: %v", err)
		}
		stream = compressed.Bytes()
		filter = "/Filter /FlateDecode "
	}
	pdf := fmt.Sprintf(`%%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >> endobj
4 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj
5 0 obj << %s/Length %d >>
stream
%s
endstream
endobj
trailer << /Root 1 0 R >>
%%%%EOF
`, filter, len(stream), string(stream))
	if err := os.WriteFile(path, []byte(pdf), 0o644); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}
}
