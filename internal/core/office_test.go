package core

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDOCXWithoutInstallUsesBuiltinOpenXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.docx")
	writeOfficeFixture(t, path, map[string]string{
		"docProps/core.xml": `<?xml version="1.0" encoding="UTF-8"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title>Quarterly Memo</dc:title>
  <dc:creator>Agentex</dc:creator>
</cp:coreProperties>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Quarterly memo</w:t></w:r></w:p>
    <w:p><w:r><w:t>Revenue increased</w:t></w:r><w:r><w:t> across regions.</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	})

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "docx", []string{path}, nil)
	if err != nil {
		t.Fatalf("run docx: %v result=%#v", err, result)
	}
	if result.Name != "docx" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Meter != "task" {
		t.Fatalf("expected task usage event: %#v", result.UsageEvents)
	}
	var output struct {
		Kind       string            `json:"kind"`
		Text       string            `json:"text"`
		Properties map[string]string `json:"properties"`
		Parts      []struct {
			Name string `json:"name"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode docx output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "docx" || output.Properties["title"] != "Quarterly Memo" {
		t.Fatalf("unexpected docx metadata: %#v", output)
	}
	if !strings.Contains(output.Text, "Quarterly memo") || !strings.Contains(output.Text, "Revenue increased across regions.") || len(output.Parts) == 0 {
		t.Fatalf("unexpected docx text: %#v", output)
	}
}

func TestRunXLSXExtractsSheetsRowsAndSharedStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.xlsx")
	writeOfficeFixture(t, path, map[string]string{
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheets><sheet name="Invoices" sheetId="1"/></sheets>
</workbook>`,
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Vendor</t></si><si><t>Total</t></si><si><t>ACME</t></si>
</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
    <row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>42</v></c></row>
  </sheetData>
</worksheet>`,
	})

	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"path":"` + filepath.ToSlash(path) + `","max_rows":10}`)
	result, err := service.RunSkill(context.Background(), "xlsx", nil, input)
	if err != nil {
		t.Fatalf("run xlsx: %v result=%#v", err, result)
	}
	var output struct {
		Kind   string `json:"kind"`
		Text   string `json:"text"`
		Sheets []struct {
			Name string `json:"name"`
			Rows []struct {
				Index  int      `json:"index"`
				Values []string `json:"values"`
			} `json:"rows"`
		} `json:"sheets"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode xlsx output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "xlsx" || len(output.Sheets) != 1 || output.Sheets[0].Name != "Invoices" {
		t.Fatalf("unexpected xlsx output: %#v", output)
	}
	if len(output.Sheets[0].Rows) != 2 || strings.Join(output.Sheets[0].Rows[0].Values, ",") != "Vendor,Total" || strings.Join(output.Sheets[0].Rows[1].Values, ",") != "ACME,42" {
		t.Fatalf("unexpected xlsx rows: %#v", output.Sheets[0].Rows)
	}
	if !strings.Contains(output.Text, "Invoices") || !strings.Contains(output.Text, "ACME") {
		t.Fatalf("unexpected xlsx text: %q", output.Text)
	}
}

func TestRunPPTXExtractsSlideTextAndNotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pptx")
	writeOfficeFixture(t, path, map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp><p:txBody><a:p><a:r><a:t>Launch Plan</a:t></a:r></a:p><a:p><a:r><a:t>Milestones</a:t></a:r></a:p></p:txBody></p:sp>
  </p:spTree></p:cSld>
</p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:notes xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Speaker notes</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld>
</p:notes>`,
	})

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "pptx", []string{"--path", path}, nil)
	if err != nil {
		t.Fatalf("run pptx: %v result=%#v", err, result)
	}
	var output struct {
		Kind   string `json:"kind"`
		Text   string `json:"text"`
		Slides []struct {
			Index int    `json:"index"`
			Text  string `json:"text"`
			Notes string `json:"notes"`
		} `json:"slides"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode pptx output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "pptx" || len(output.Slides) != 1 || output.Slides[0].Index != 1 {
		t.Fatalf("unexpected pptx output: %#v", output)
	}
	if !strings.Contains(output.Slides[0].Text, "Launch Plan") || !strings.Contains(output.Slides[0].Text, "Milestones") || !strings.Contains(output.Slides[0].Notes, "Speaker notes") {
		t.Fatalf("unexpected pptx slide extraction: %#v", output.Slides[0])
	}
}

func writeOfficeFixture(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create office fixture: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	for name, content := range entries {
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("create office fixture entry %s: %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("write office fixture entry %s: %v", name, err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close office fixture zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close office fixture: %v", err)
	}
}
