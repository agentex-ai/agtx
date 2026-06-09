package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type officeCoreProperties struct {
	XMLName        xml.Name `xml:"http://schemas.openxmlformats.org/package/2006/metadata/core-properties coreProperties"`
	Title          string   `xml:"http://purl.org/dc/elements/1.1/ title,omitempty"`
	Subject        string   `xml:"http://purl.org/dc/elements/1.1/ subject,omitempty"`
	Creator        string   `xml:"http://purl.org/dc/elements/1.1/ creator,omitempty"`
	Keywords       string   `xml:"keywords,omitempty"`
	Description    string   `xml:"http://purl.org/dc/elements/1.1/ description,omitempty"`
	LastModifiedBy string   `xml:"lastModifiedBy,omitempty"`
	Revision       string   `xml:"revision,omitempty"`
	Category       string   `xml:"category,omitempty"`
}

type officeCorePropertiesOutput struct {
	XMLName        xml.Name `xml:"cp:coreProperties"`
	CP             string   `xml:"xmlns:cp,attr"`
	DC             string   `xml:"xmlns:dc,attr"`
	DCTerms        string   `xml:"xmlns:dcterms,attr"`
	Dcmitype       string   `xml:"xmlns:dcmitype,attr"`
	XSI            string   `xml:"xmlns:xsi,attr"`
	Title          string   `xml:"dc:title,omitempty"`
	Subject        string   `xml:"dc:subject,omitempty"`
	Creator        string   `xml:"dc:creator,omitempty"`
	Keywords       string   `xml:"cp:keywords,omitempty"`
	Description    string   `xml:"dc:description,omitempty"`
	LastModifiedBy string   `xml:"cp:lastModifiedBy,omitempty"`
	Revision       string   `xml:"cp:revision,omitempty"`
	Category       string   `xml:"cp:category,omitempty"`
}

func applyOfficeAttributionForRun(versionDir string, options RunOptions, result RunResult) {
	agentName := strings.TrimSpace(options.AgentName)
	if agentName == "" {
		return
	}
	byline := "by " + agentName
	for _, path := range officeAttributionCandidatePaths(versionDir, options, result) {
		_ = updateOfficeCoreProperties(path, agentName, byline)
	}
}

func officeAttributionCandidatePaths(versionDir string, options RunOptions, result RunResult) []string {
	seen := map[string]bool{}
	var candidates []string
	add := func(raw string) {
		raw = strings.Trim(strings.TrimSpace(raw), `"'`)
		if raw == "" || !isOfficeDocumentPath(raw) {
			return
		}
		for _, path := range resolveOfficeArtifactPath(versionDir, raw) {
			if seen[path] {
				continue
			}
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				seen[path] = true
				candidates = append(candidates, path)
			}
		}
	}
	collectOfficePathsFromArgs(options.Args, add)
	collectOfficePathsFromJSON(options.Input, add)
	collectOfficePathsFromJSON([]byte(result.Stdout), add)
	return candidates
}

func resolveOfficeArtifactPath(versionDir, raw string) []string {
	if filepath.IsAbs(raw) {
		return []string{filepath.Clean(raw)}
	}
	var paths []string
	if versionDir != "" {
		paths = append(paths, filepath.Join(versionDir, raw))
	}
	if abs, err := filepath.Abs(raw); err == nil {
		paths = append(paths, abs)
	}
	return paths
}

func collectOfficePathsFromArgs(args []string, add func(string)) {
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if key, value, ok := strings.Cut(arg, "="); ok {
			values[normalizeAttributionKey(key)] = value
			continue
		}
		if strings.HasPrefix(arg, "--") {
			key := normalizeAttributionKey(arg)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				values[key] = args[i+1]
				i++
			}
		}
	}
	addOfficeOutputValues(values, add)
}

func addOfficeOutputValues(values map[string]string, add func(string)) {
	for _, key := range []string{
		"output", "output_path", "output_file",
		"out", "out_path", "out_file",
		"destination", "destination_path", "destination_file",
		"target", "target_path", "target_file",
		"result", "result_path", "result_file",
		"artifact", "artifact_path", "artifact_file",
		"generated", "generated_path", "generated_file",
		"document_output", "workbook_output", "presentation_output",
	} {
		add(values[key])
	}
	if isOfficeMutationAction(values["action"]) {
		for _, key := range []string{
			"path", "file", "file_path",
			"document", "document_path",
			"workbook", "workbook_path",
			"presentation", "presentation_path",
		} {
			add(values[key])
		}
	}
}

func collectOfficePathsFromJSON(data []byte, add func(string)) {
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return
	}
	collectOfficePathsFromValue(value, add)
}

func collectOfficePathsFromValue(value any, add func(string)) {
	switch typed := value.(type) {
	case map[string]any:
		values := map[string]string{}
		for childKey, childValue := range typed {
			key := normalizeAttributionKey(childKey)
			if stringValue, ok := childValue.(string); ok {
				values[key] = stringValue
			}
			collectOfficePathsFromValue(childValue, add)
		}
		addOfficeOutputValues(values, add)
	case []any:
		for _, item := range typed {
			collectOfficePathsFromValue(item, add)
		}
	}
}

func normalizeAttributionKey(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "-")
	key = strings.ReplaceAll(key, "-", "_")
	return strings.ToLower(key)
}

func isOfficeDocumentPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx", ".xlsx", ".pptx":
		return true
	default:
		return false
	}
}

func isOfficeMutationAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "create", "edit", "write", "export", "generate", "save", "render", "build":
		return true
	default:
		return false
	}
}

func updateOfficeCoreProperties(path, agentName, byline string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	readerClosed := false
	closeReader := func() {
		if !readerClosed {
			_ = reader.Close()
			readerClosed = true
		}
	}
	defer closeReader()

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".agtx-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempName)
		}
	}()

	writer := zip.NewWriter(temp)
	foundCore := false
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		data, err := readOfficeZipFile(file)
		if err != nil {
			_ = writer.Close()
			_ = temp.Close()
			return err
		}
		if file.Name == "docProps/core.xml" {
			foundCore = true
			data = officeCorePropertiesXML(data, agentName, byline)
		}
		if err := writeOfficeZipFile(writer, file.FileHeader, data); err != nil {
			_ = writer.Close()
			_ = temp.Close()
			return err
		}
	}
	if !foundCore {
		header := &zip.FileHeader{Name: "docProps/core.xml", Method: zip.Deflate}
		header.SetMode(0o644)
		if err := writeOfficeZipFile(writer, *header, officeCorePropertiesXML(nil, agentName, byline)); err != nil {
			_ = writer.Close()
			_ = temp.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := reader.Close(); err != nil {
		return err
	}
	readerClosed = true
	if err := renameReplacing(tempName, path); err != nil {
		return fmt.Errorf("replace office document: %w", err)
	}
	cleanup = false
	return nil
}

func officeCorePropertiesXML(data []byte, agentName, byline string) []byte {
	props := officeCoreProperties{}
	if len(bytes.TrimSpace(data)) > 0 {
		_ = xml.Unmarshal(data, &props)
	}
	props.Creator = agentName
	props.LastModifiedBy = agentName
	props.Description = officeDescriptionWithByline(props.Description, byline)
	out, err := xml.MarshalIndent(officeCorePropertiesOutput{
		CP:             "http://schemas.openxmlformats.org/package/2006/metadata/core-properties",
		DC:             "http://purl.org/dc/elements/1.1/",
		DCTerms:        "http://purl.org/dc/terms/",
		Dcmitype:       "http://purl.org/dc/dcmitype/",
		XSI:            "http://www.w3.org/2001/XMLSchema-instance",
		Title:          props.Title,
		Subject:        props.Subject,
		Creator:        props.Creator,
		Keywords:       props.Keywords,
		Description:    props.Description,
		LastModifiedBy: props.LastModifiedBy,
		Revision:       props.Revision,
		Category:       props.Category,
	}, "", "  ")
	if err != nil {
		return data
	}
	return append([]byte(xml.Header), out...)
}

func officeDescriptionWithByline(description, byline string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return byline
	}
	if strings.Contains(description, byline) {
		return description
	}
	return description + "\n" + byline
}

func readOfficeZipFile(file *zip.File) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}

func writeOfficeZipFile(writer *zip.Writer, source zip.FileHeader, data []byte) error {
	header := source
	header.Method = zip.Deflate
	target, err := writer.CreateHeader(&header)
	if err != nil {
		return err
	}
	_, err = target.Write(data)
	return err
}
