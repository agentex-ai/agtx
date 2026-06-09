package core

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOfficeAttributionCandidatePathsUseExplicitOutputs(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "template.docx")
	outputPath := filepath.Join(root, "out.docx")
	writeMinimalOfficeDocument(t, inputPath)
	writeMinimalOfficeDocument(t, outputPath)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{inputPath, "--output", outputPath},
	}, RunResult{})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected only explicit output candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsTreatActionPathAsOutput(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "sample.docx")
	writeMinimalOfficeDocument(t, outputPath)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"action=create", "path=" + outputPath},
	}, RunResult{})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected action path candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsIgnoreAmbiguousPath(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "template.docx")
	writeMinimalOfficeDocument(t, inputPath)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"path=" + inputPath},
	}, RunResult{})

	if len(candidates) != 0 {
		t.Fatalf("expected ambiguous path to be ignored, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseJSONOutputContainers(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "report.docx")
	writeMinimalOfficeDocument(t, outputPath)
	stdout, err := json.Marshal(map[string]any{
		"outputs": []string{outputPath},
	})
	if err != nil {
		t.Fatalf("marshal stdout: %v", err)
	}

	candidates := officeAttributionCandidatePaths("", RunOptions{}, RunResult{Stdout: string(stdout)})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected JSON output candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsIgnoreJSONInputContainers(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "source.docx")
	writeMinimalOfficeDocument(t, inputPath)
	input, err := json.Marshal(map[string]any{
		"inputs": []string{inputPath},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	candidates := officeAttributionCandidatePaths("", RunOptions{Input: input}, RunResult{})

	if len(candidates) != 0 {
		t.Fatalf("expected JSON input container to be ignored, got %#v", candidates)
	}
}
