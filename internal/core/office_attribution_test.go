package core

import (
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
