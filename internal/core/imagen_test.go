package core

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunImagenWithoutInstallGeneratesPNGAssets(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "assets")
	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"prompt":"calm dashboard hero for agent capability packs","style":"clean editorial","output_dir":"` + filepath.ToSlash(outputDir) + `","width":160,"height":96,"count":2,"seed":42}`)
	result, err := service.RunSkill(context.Background(), "imagen", nil, input)
	if err != nil {
		t.Fatalf("run imagen: %v result=%#v", err, result)
	}
	if result.Name != "imagen" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 2 || result.UsageEvents[0].Meter != "task" || result.UsageEvents[1].Meter != "credit" {
		t.Fatalf("expected task and credit usage events: %#v", result.UsageEvents)
	}
	var output struct {
		Kind     string   `json:"kind"`
		Prompt   string   `json:"prompt"`
		Count    int      `json:"count"`
		Warnings []string `json:"warnings"`
		Metadata struct {
			ManifestPath string `json:"manifest_path"`
		} `json:"metadata"`
		Assets []struct {
			Path   string `json:"path"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Format string `json:"format"`
			SHA256 string `json:"sha256"`
			Bytes  int    `json:"bytes"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode imagen output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "imagen" || output.Count != 2 || len(output.Assets) != 2 || !strings.Contains(output.Prompt, "dashboard") {
		t.Fatalf("unexpected imagen output: %#v", output)
	}
	if len(output.Warnings) == 0 || output.Metadata.ManifestPath == "" {
		t.Fatalf("expected warnings and manifest path: %#v", output)
	}
	for _, asset := range output.Assets {
		data, err := os.ReadFile(asset.Path)
		if err != nil {
			t.Fatalf("read generated asset: %v", err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode generated png: %v", err)
		}
		if asset.Format != "png" || asset.Width != 160 || asset.Height != 96 || img.Bounds().Dx() != 160 || img.Bounds().Dy() != 96 || asset.Bytes != len(data) || len(asset.SHA256) != 64 {
			t.Fatalf("unexpected asset metadata: %#v", asset)
		}
	}
	if _, err := os.Stat(output.Metadata.ManifestPath); err != nil {
		t.Fatalf("expected manifest file: %v", err)
	}
}

func TestRunImagenWritesSingleOutputFromFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single.png")
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "imagen", []string{"--prompt", "small launch badge", "--output", path, "--width", "80", "--height", "80"}, nil)
	if err != nil {
		t.Fatalf("run imagen flags: %v result=%#v", err, result)
	}
	if result.Name != "imagen" || result.Version != "0.2.0" || result.Stub {
		t.Fatalf("unexpected imagen run result: %#v", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected single output path: %v", err)
	}
	var output struct {
		Assets []struct {
			Path string `json:"path"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode alias output: %v stdout=%s", err, result.Stdout)
	}
	if len(output.Assets) != 1 || filepath.Clean(output.Assets[0].Path) != filepath.Clean(path) {
		t.Fatalf("expected output path in stdout: %#v", output.Assets)
	}
}
