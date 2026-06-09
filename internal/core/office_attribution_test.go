package core

import (
	"archive/zip"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestOfficeAttributionCandidatePathsUseFirstExistingRelativeOutput(t *testing.T) {
	root := t.TempDir()
	versionDir := filepath.Join(root, "skill")
	cwd := filepath.Join(root, "cwd")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatalf("create version dir: %v", err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("create cwd: %v", err)
	}
	versionOutput := filepath.Join(versionDir, "out.docx")
	cwdOutput := filepath.Join(cwd, "out.docx")
	writeMinimalOfficeDocument(t, versionOutput)
	writeMinimalOfficeDocument(t, cwdOutput)
	t.Chdir(cwd)

	candidates := officeAttributionCandidatePaths(versionDir, RunOptions{
		Args: []string{"--output", "out.docx"},
	}, RunResult{})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(versionOutput) {
		t.Fatalf("expected version dir output candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseEqualsAndCamelCaseOutputs(t *testing.T) {
	root := t.TempDir()
	firstOutput := filepath.Join(root, "first.docx")
	secondOutput := filepath.Join(root, "second.xlsx")
	thirdOutput := filepath.Join(root, "third.pptx")
	writeMinimalOfficeDocument(t, firstOutput)
	writeMinimalOfficeDocument(t, secondOutput)
	writeMinimalOfficeDocument(t, thirdOutput)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{
			"--output=" + firstOutput,
			"outputPath=" + secondOutput,
			"outputFile=" + thirdOutput,
		},
	}, RunResult{})

	expected := []string{
		filepath.Clean(firstOutput),
		filepath.Clean(secondOutput),
		filepath.Clean(thirdOutput),
	}
	if strings.Join(candidates, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("expected output candidates %#v, got %#v", expected, candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseShortAndExportOutputs(t *testing.T) {
	root := t.TempDir()
	firstOutput := filepath.Join(root, "short.docx")
	secondOutput := filepath.Join(root, "saved.xlsx")
	thirdOutput := filepath.Join(root, "exported.pptx")
	fourthOutput := filepath.Join(root, "created.docx")
	for _, path := range []string{firstOutput, secondOutput, thirdOutput, fourthOutput} {
		writeMinimalOfficeDocument(t, path)
	}

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{
			"-o", firstOutput,
			"saveAs=" + secondOutput,
			"exportPath=" + thirdOutput,
			"createdFile=" + fourthOutput,
		},
	}, RunResult{})

	expected := []string{
		filepath.Clean(firstOutput),
		filepath.Clean(secondOutput),
		filepath.Clean(thirdOutput),
		filepath.Clean(fourthOutput),
	}
	if strings.Join(candidates, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("expected short/export output candidates %#v, got %#v", expected, candidates)
	}
}

func TestOfficeAttributionCandidatePathsKeepsRepeatedOutputs(t *testing.T) {
	root := t.TempDir()
	firstOutput := filepath.Join(root, "first.docx")
	secondOutput := filepath.Join(root, "second.docx")
	writeMinimalOfficeDocument(t, firstOutput)
	writeMinimalOfficeDocument(t, secondOutput)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"--output", firstOutput, "--output", secondOutput},
	}, RunResult{})

	expected := []string{filepath.Clean(firstOutput), filepath.Clean(secondOutput)}
	if strings.Join(candidates, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("expected repeated output candidates %#v, got %#v", expected, candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseFileURIOutputs(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "with space.docx")
	writeMinimalOfficeDocument(t, outputPath)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"--output", fileURIForOfficeTest(outputPath)},
	}, RunResult{})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected file URI output candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseLocalhostFileURIOutputs(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "localhost.docx")
	writeMinimalOfficeDocument(t, outputPath)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"--output", localhostFileURIForOfficeTest(outputPath)},
	}, RunResult{})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected localhost file URI output candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseOpaqueFileURIOutputs(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "opaque.docx")
	writeMinimalOfficeDocument(t, outputPath)

	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"--output", "file:" + filepath.ToSlash(outputPath)},
	}, RunResult{})

	if len(candidates) != 1 || candidates[0] != filepath.Clean(outputPath) {
		t.Fatalf("expected opaque file URI output candidate, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsIgnoreRemoteURIOutputs(t *testing.T) {
	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"--output", "https://example.com/report.docx"},
	}, RunResult{})

	if len(candidates) != 0 {
		t.Fatalf("expected remote URI output to be ignored, got %#v", candidates)
	}
}

func TestOfficeAttributionCandidatePathsIgnoreNonLocalFileURIOutputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows file URI host maps to UNC path")
	}
	candidates := officeAttributionCandidatePaths("", RunOptions{
		Args: []string{"--output", "file://example.com/report.docx"},
	}, RunResult{})

	if len(candidates) != 0 {
		t.Fatalf("expected non-local file URI output to be ignored, got %#v", candidates)
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
	inputPath := filepath.Join(root, "template.docx")
	firstOutput := filepath.Join(root, "report.docx")
	secondOutput := filepath.Join(root, "named.docx")
	for _, path := range []string{inputPath, firstOutput, secondOutput} {
		writeMinimalOfficeDocument(t, path)
	}
	stdout, err := json.Marshal(map[string]any{
		"outputs": []map[string]any{
			{
				"path":   firstOutput,
				"source": inputPath,
			},
		},
		"output_files": map[string]string{
			"named":  secondOutput,
			"source": inputPath,
		},
	})
	if err != nil {
		t.Fatalf("marshal stdout: %v", err)
	}

	candidates := officeAttributionCandidatePaths("", RunOptions{}, RunResult{Stdout: string(stdout)})

	expected := []string{filepath.Clean(firstOutput), filepath.Clean(secondOutput)}
	if !sameOfficePathSet(candidates, expected) {
		t.Fatalf("expected JSON output candidates %#v, got %#v", expected, candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseJSONOutputObjects(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "template.docx")
	firstOutput := filepath.Join(root, "report.docx")
	secondOutput := filepath.Join(root, "workbook.xlsx")
	thirdOutput := filepath.Join(root, "deck.pptx")
	for _, path := range []string{inputPath, firstOutput, secondOutput, thirdOutput} {
		writeMinimalOfficeDocument(t, path)
	}
	stdout, err := json.Marshal(map[string]any{
		"input": map[string]any{
			"path": inputPath,
		},
		"output": map[string]any{
			"path":   firstOutput,
			"source": inputPath,
		},
		"artifact": map[string]any{
			"uri": fileURIForOfficeTest(secondOutput),
		},
		"result": map[string]any{
			"files": []map[string]any{
				{"path": thirdOutput},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal stdout: %v", err)
	}

	candidates := officeAttributionCandidatePaths("", RunOptions{}, RunResult{Stdout: string(stdout)})

	expected := []string{
		filepath.Clean(firstOutput),
		filepath.Clean(secondOutput),
		filepath.Clean(thirdOutput),
	}
	if !sameOfficePathSet(candidates, expected) {
		t.Fatalf("expected JSON output object candidates %#v, got %#v", expected, candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseNDJSONOutputs(t *testing.T) {
	root := t.TempDir()
	firstOutput := filepath.Join(root, "stream.docx")
	secondOutput := filepath.Join(root, "created.xlsx")
	for _, path := range []string{firstOutput, secondOutput} {
		writeMinimalOfficeDocument(t, path)
	}
	firstLine, err := json.Marshal(map[string]any{
		"event": "artifact",
		"output": map[string]any{
			"path": firstOutput,
		},
	})
	if err != nil {
		t.Fatalf("marshal first line: %v", err)
	}
	secondLine, err := json.Marshal(map[string]any{
		"event": "done",
		"createdFiles": map[string]string{
			"workbook": secondOutput,
		},
	})
	if err != nil {
		t.Fatalf("marshal second line: %v", err)
	}
	stdout := strings.Join([]string{
		"plain log line",
		string(firstLine),
		string(secondLine),
	}, "\n")

	candidates := officeAttributionCandidatePaths("", RunOptions{}, RunResult{Stdout: stdout})

	expected := []string{filepath.Clean(firstOutput), filepath.Clean(secondOutput)}
	if !sameOfficePathSet(candidates, expected) {
		t.Fatalf("expected NDJSON output candidates %#v, got %#v", expected, candidates)
	}
}

func TestOfficeAttributionCandidatePathsUseTextOutputHints(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "template.docx")
	firstOutput := filepath.Join(root, "report with spaces.docx")
	secondOutput := filepath.Join(root, "plain.xlsx")
	thirdOutput := filepath.Join(root, "stderr.pptx")
	nestedDir := filepath.Join(root, "archive.docx.folder")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	fourthOutput := filepath.Join(nestedDir, "final.docx")
	for _, path := range []string{inputPath, firstOutput, secondOutput, thirdOutput, fourthOutput} {
		writeMinimalOfficeDocument(t, path)
	}

	stdout := strings.Join([]string{
		"Input file: " + inputPath,
		`Saved to: "` + firstOutput + `"`,
		"Output file=" + secondOutput + " (complete)",
		"Artifact file: <" + fourthOutput + ">",
	}, "\n")
	stderr := "Exported file: " + fileURIForOfficeTest(thirdOutput)

	candidates := officeAttributionCandidatePaths("", RunOptions{}, RunResult{
		Stdout: stdout,
		Stderr: stderr,
	})

	expected := []string{
		filepath.Clean(firstOutput),
		filepath.Clean(secondOutput),
		filepath.Clean(thirdOutput),
		filepath.Clean(fourthOutput),
	}
	if !sameOfficePathSet(candidates, expected) {
		t.Fatalf("expected text output hint candidates %#v, got %#v", expected, candidates)
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

func TestUpdateOfficeCorePropertiesAddsMissingPackageMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "generated.docx")
	writeOfficeDocumentWithoutCoreProperties(t, path)

	if err := updateOfficeCoreProperties(path, "Codex", "by Codex"); err != nil {
		t.Fatalf("update office core properties: %v", err)
	}

	coreXML := readZipFileStringForTest(t, path, "docProps/core.xml")
	if !strings.Contains(coreXML, "<dc:creator>Codex</dc:creator>") || !strings.Contains(coreXML, "by Codex") {
		t.Fatalf("expected generated core metadata, got:\n%s", coreXML)
	}
	contentTypesXML := readZipFileStringForTest(t, path, "[Content_Types].xml")
	if !strings.Contains(contentTypesXML, `PartName="/docProps/core.xml"`) || !strings.Contains(contentTypesXML, officeCorePropertiesCT) {
		t.Fatalf("expected core properties content type, got:\n%s", contentTypesXML)
	}
	relationshipsXML := readZipFileStringForTest(t, path, "_rels/.rels")
	if !strings.Contains(relationshipsXML, officeCorePropertiesRel) || !strings.Contains(relationshipsXML, `Target="docProps/core.xml"`) {
		t.Fatalf("expected core properties relationship, got:\n%s", relationshipsXML)
	}
}

func TestUpdateOfficeCorePropertiesRejectsNonOfficeZip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plain.docx")
	writePlainZipForOfficeTest(t, path)

	if err := updateOfficeCoreProperties(path, "Codex", "by Codex"); err == nil {
		t.Fatal("expected non-office zip to be rejected")
	}
	if zipEntryExistsForOfficeTest(t, path, "docProps/core.xml") {
		t.Fatal("expected non-office zip to remain unattributed")
	}
}

func TestUpdateOfficeCorePropertiesCollapsesDuplicatePackageParts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "duplicates.docx")
	writeOfficeDocumentWithDuplicatePackageParts(t, path)

	if err := updateOfficeCoreProperties(path, "Codex", "by Codex"); err != nil {
		t.Fatalf("update office core properties: %v", err)
	}

	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "docProps/core.xml"} {
		if count := zipEntryCountForOfficeTest(t, path, name); count != 1 {
			t.Fatalf("expected one %s entry, got %d", name, count)
		}
	}
	for _, name := range []string{"/[Content_Types].xml", "/_rels/.rels", "/docProps/core.xml"} {
		if count := zipEntryCountForOfficeTest(t, path, name); count != 0 {
			t.Fatalf("expected no leading-slash %s entries, got %d", name, count)
		}
	}

	coreXML := readZipFileStringForTest(t, path, "docProps/core.xml")
	if !strings.Contains(coreXML, "<dc:creator>Codex</dc:creator>") || !strings.Contains(coreXML, "by Codex") {
		t.Fatalf("expected updated core metadata, got:\n%s", coreXML)
	}
	contentTypesXML := readZipFileStringForTest(t, path, "[Content_Types].xml")
	if strings.Count(contentTypesXML, `PartName="/docProps/core.xml"`) != 1 || !strings.Contains(contentTypesXML, officeCorePropertiesCT) {
		t.Fatalf("expected one normalized core content type, got:\n%s", contentTypesXML)
	}
	relationshipsXML := readZipFileStringForTest(t, path, "_rels/.rels")
	if strings.Count(relationshipsXML, officeCorePropertiesRel) != 1 || strings.Contains(relationshipsXML, "TargetMode=") {
		t.Fatalf("expected one normalized core relationship, got:\n%s", relationshipsXML)
	}
}

func TestOfficePackageMetadataNormalizesExistingCoreEntries(t *testing.T) {
	contentTypesXML := string(officeContentTypesXML([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Override PartName="docProps/core.xml" ContentType="application/xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/xml"/>
</Types>`)))
	if strings.Count(contentTypesXML, `PartName="/docProps/core.xml"`) != 1 || !strings.Contains(contentTypesXML, officeCorePropertiesCT) {
		t.Fatalf("expected normalized core content type, got:\n%s", contentTypesXML)
	}

	relationshipsXML := string(officeRelationshipsXML([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId9" Type="bad" Target="/docProps/core.xml" TargetMode="External"/>
  <Relationship Id="rId10" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`)))
	if !strings.Contains(relationshipsXML, `Type="`+officeCorePropertiesRel+`"`) || !strings.Contains(relationshipsXML, `Target="docProps/core.xml"`) {
		t.Fatalf("expected normalized core relationship, got:\n%s", relationshipsXML)
	}
	if strings.Count(relationshipsXML, officeCorePropertiesRel) != 1 {
		t.Fatalf("expected one core relationship, got:\n%s", relationshipsXML)
	}
	if strings.Contains(relationshipsXML, "TargetMode=") {
		t.Fatalf("expected core relationship to clear target mode, got:\n%s", relationshipsXML)
	}
}

func fileURIForOfficeTest(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return (&url.URL{Scheme: "file", Path: "/" + slashPath}).String()
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func localhostFileURIForOfficeTest(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return (&url.URL{Scheme: "file", Host: "localhost", Path: "/" + slashPath}).String()
	}
	return (&url.URL{Scheme: "file", Host: "localhost", Path: slashPath}).String()
}

func sameOfficePathSet(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	seen := map[string]int{}
	for _, path := range actual {
		seen[path]++
	}
	for _, path := range expected {
		if seen[path] == 0 {
			return false
		}
		seen[path]--
	}
	return true
}

func writePlainZipForOfficeTest(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create plain zip: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	writer, err := zipWriter.Create("notes.txt")
	if err != nil {
		t.Fatalf("create plain zip entry: %v", err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("write plain zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close plain zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close plain zip file: %v", err)
	}
}

func zipEntryExistsForOfficeTest(t *testing.T, path, name string) bool {
	return zipEntryCountForOfficeTest(t, path, name) > 0
}

func zipEntryCountForOfficeTest(t *testing.T, path, name string) int {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer reader.Close()
	count := 0
	for _, file := range reader.File {
		if file.Name == name {
			count++
		}
	}
	return count
}

func writeOfficeDocumentWithDuplicatePackageParts(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create office document: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	entries := []struct {
		name    string
		content string
	}{
		{
			name: "[Content_Types].xml",
			content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="docProps/core.xml" ContentType="application/xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/xml"/>
</Types>`,
		},
		{
			name: "/[Content_Types].xml",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		},
		{
			name: "_rels/.rels",
			content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="bad" Target="/docProps/core.xml" TargetMode="External"/>
  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`,
		},
		{
			name: "/_rels/.rels",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		},
		{
			name: "/docProps/core.xml",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title>Duplicate Template</dc:title>
</cp:coreProperties>`,
		},
		{
			name: "docProps/core.xml",
			content: `<?xml version="1.0" encoding="UTF-8"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:creator>Old Agent</dc:creator>
</cp:coreProperties>`,
		},
		{
			name: "word/document.xml",
			content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p></w:body>
</w:document>`,
		},
	}
	for _, entry := range entries {
		writer, err := zipWriter.Create(entry.name)
		if err != nil {
			t.Fatalf("create office zip entry: %v", err)
		}
		if _, err := writer.Write([]byte(entry.content)); err != nil {
			t.Fatalf("write office zip entry: %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close office zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close office document: %v", err)
	}
}

func writeOfficeDocumentWithoutCoreProperties(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create office document: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	entries := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p></w:body>
</w:document>`,
	}
	for name, content := range entries {
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("create office zip entry: %v", err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write office zip entry: %v", err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close office zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close office document: %v", err)
	}
}
