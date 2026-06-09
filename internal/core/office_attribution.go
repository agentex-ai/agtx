package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	officeContentTypesNS       = "http://schemas.openxmlformats.org/package/2006/content-types"
	officeRelationshipsNS      = "http://schemas.openxmlformats.org/package/2006/relationships"
	officeCorePropertiesRel    = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	officeCorePropertiesCT     = "application/vnd.openxmlformats-package.core-properties+xml"
	officeCorePropertiesTarget = "docProps/core.xml"
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

type officeContentTypes struct {
	XMLName   xml.Name                    `xml:"Types"`
	XMLNS     string                      `xml:"xmlns,attr"`
	Defaults  []officeContentTypeDefault  `xml:"Default,omitempty"`
	Overrides []officeContentTypeOverride `xml:"Override,omitempty"`
}

type officeContentTypeDefault struct {
	Extension   string `xml:"Extension,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type officeContentTypeOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type officeRelationships struct {
	XMLName       xml.Name             `xml:"Relationships"`
	XMLNS         string               `xml:"xmlns,attr"`
	Relationships []officeRelationship `xml:"Relationship,omitempty"`
}

type officeRelationship struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr,omitempty"`
}

func applyOfficeAttributionForRun(versionDir string, options RunOptions, result RunResult) []string {
	agentName := strings.TrimSpace(options.AgentName)
	if agentName == "" {
		return nil
	}
	byline := "by " + agentName
	attributed := []string{}
	for _, path := range officeAttributionCandidatePaths(versionDir, options, result) {
		if err := updateOfficeCoreProperties(path, agentName, byline); err == nil {
			attributed = append(attributed, path)
		}
	}
	return attributed
}

func officeAttributionCandidatePaths(versionDir string, options RunOptions, result RunResult) []string {
	seen := map[string]bool{}
	var candidates []string
	add := func(raw string) {
		raw = normalizeOfficeArtifactPath(raw)
		if raw == "" || !isOfficeDocumentPath(raw) {
			return
		}
		path, ok := resolveExistingOfficeArtifactPath(versionDir, raw)
		if !ok || seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}
	collectOfficePathsFromArgs(options.Args, add)
	collectOfficePathsFromJSON(options.Input, add)
	collectOfficePathsFromJSON([]byte(result.Stdout), add)
	collectOfficePathsFromJSON([]byte(result.Stderr), add)
	collectOfficePathsFromText([]byte(result.Stdout), add)
	collectOfficePathsFromText([]byte(result.Stderr), add)
	return candidates
}

func normalizeOfficeArtifactPath(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), `"'`)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return raw
	}
	if isWindowsDrivePath(raw, parsed.Scheme) {
		return raw
	}
	if !strings.EqualFold(parsed.Scheme, "file") {
		return ""
	}
	if parsed.Host != "" {
		if isLocalFileURIHost(parsed.Host) {
			return officeFileURIPath(parsed.Path, parsed.Opaque)
		}
		if runtime.GOOS == "windows" {
			return `\\` + parsed.Host + filepath.FromSlash(parsed.Path)
		}
		return ""
	}
	return officeFileURIPath(parsed.Path, parsed.Opaque)
}

func officeFileURIPath(path, opaque string) string {
	if path == "" {
		path = opaque
	}
	if path == "" {
		return ""
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

func isLocalFileURIHost(host string) bool {
	return host == "" || strings.EqualFold(host, "localhost")
}

func isWindowsDrivePath(path, scheme string) bool {
	if len(scheme) != 1 || len(path) < 3 {
		return false
	}
	letter := path[0]
	if !((letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')) {
		return false
	}
	return path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func resolveExistingOfficeArtifactPath(versionDir, raw string) (string, bool) {
	if filepath.IsAbs(raw) {
		return existingOfficeFile(filepath.Clean(raw))
	}
	if versionDir != "" {
		if path, ok := existingOfficeFile(filepath.Join(versionDir, raw)); ok {
			return path, true
		}
	}
	if abs, err := filepath.Abs(raw); err == nil {
		return existingOfficeFile(abs)
	}
	return "", false
}

func existingOfficeFile(path string) (string, bool) {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func collectOfficePathsFromArgs(args []string, add func(string)) {
	values := map[string][]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if key, value, ok := strings.Cut(arg, "="); ok {
			addOfficeOutputValue(values, normalizeAttributionKey(key), value)
			continue
		}
		if strings.HasPrefix(arg, "--") {
			key := normalizeAttributionKey(arg)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				addOfficeOutputValue(values, key, args[i+1])
				i++
			}
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			key := normalizeAttributionKey(arg)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				addOfficeOutputValue(values, key, args[i+1])
				i++
			}
		}
	}
	addOfficeOutputValues(values, add)
}

func addOfficeOutputValue(values map[string][]string, key, value string) {
	values[key] = append(values[key], value)
}

func addOfficeOutputValues(values map[string][]string, add func(string)) {
	for _, key := range []string{
		"o",
		"output", "output_path", "output_file",
		"out", "out_path", "out_file",
		"save_as", "save_to", "save_path", "save_file",
		"export", "export_path", "export_file",
		"created", "created_path", "created_file",
		"destination", "destination_path", "destination_file",
		"target", "target_path", "target_file",
		"result", "result_path", "result_file",
		"artifact", "artifact_path", "artifact_file",
		"generated", "generated_path", "generated_file",
		"document_output", "workbook_output", "presentation_output",
	} {
		for _, value := range values[key] {
			add(value)
		}
	}
	if isOfficeMutationAction(firstOfficeOutputValue(values, "action")) {
		for _, key := range []string{
			"path", "file", "file_path",
			"document", "document_path",
			"workbook", "workbook_path",
			"presentation", "presentation_path",
		} {
			for _, value := range values[key] {
				add(value)
			}
		}
	}
}

func firstOfficeOutputValue(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func collectOfficePathsFromJSON(data []byte, add func(string)) {
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	var value any
	if err := json.Unmarshal(data, &value); err == nil {
		collectOfficePathsFromValue(value, "", add)
		return
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var lineValue any
		if err := json.Unmarshal(line, &lineValue); err == nil {
			collectOfficePathsFromValue(lineValue, "", add)
		}
	}
}

func collectOfficePathsFromText(data []byte, add func(string)) {
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line := strings.TrimSpace(string(line))
		if line == "" {
			continue
		}
		if value, ok := officeTextOutputValue(line); ok {
			add(value)
		}
	}
}

func officeTextOutputValue(line string) (string, bool) {
	if key, value, ok := strings.Cut(line, "="); ok && isOfficeTextOutputLabel(key) {
		return extractOfficeTextPathValue(value), true
	}
	if key, value, ok := strings.Cut(line, ":"); ok && isOfficeTextOutputLabel(key) {
		return extractOfficeTextPathValue(value), true
	}
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, label := range []string{
		"saved to", "saved as",
		"save to", "save as",
		"exported to", "exported file",
		"created file", "generated file",
		"output file", "artifact file",
		"written to", "wrote",
	} {
		if lower == label {
			return "", false
		}
		if strings.HasPrefix(lower, label+" ") || strings.HasPrefix(lower, label+"\t") {
			return extractOfficeTextPathValue(strings.TrimSpace(line[len(label):])), true
		}
	}
	return "", false
}

func extractOfficeTextPathValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value[0] == '<' {
		for index := 1; index < len(value); index++ {
			if value[index] == '>' {
				return value[1:index]
			}
		}
		return strings.Trim(value[1:], " \t\r\n.,;")
	}
	if value[0] == '"' || value[0] == '\'' || value[0] == '`' {
		quote := value[0]
		for index := 1; index < len(value); index++ {
			if value[index] == quote {
				return value[1:index]
			}
		}
		return strings.Trim(value[1:], " \t\r\n.,;")
	}
	lower := strings.ToLower(value)
	for _, ext := range []string{".docx", ".xlsx", ".pptx"} {
		if index := strings.LastIndex(lower, ext); index >= 0 {
			end := index + len(ext)
			if end == len(value) || isOfficeTextPathBoundary(value[end]) {
				return strings.Trim(value[:end], " \t\r\n\"'`.,;")
			}
		}
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], " \t\r\n\"'`.,;")
}

func isOfficeTextPathBoundary(char byte) bool {
	switch char {
	case ' ', '\t', '\r', '\n', '"', '\'', '`', ',', ';', '.', ')', ']', '}':
		return true
	default:
		return false
	}
}

func collectOfficePathsFromValue(value any, key string, add func(string)) {
	if isOfficeOutputContainerKey(key) {
		collectOfficeOutputContainerPaths(value, add)
		return
	}
	if isOfficeOutputObjectKey(key) {
		collectOfficeOutputObjectPaths(value, add)
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		values := map[string][]string{}
		for childKey, childValue := range typed {
			key := normalizeAttributionKey(childKey)
			if stringValue, ok := childValue.(string); ok {
				addOfficeOutputValue(values, key, stringValue)
			}
			collectOfficePathsFromValue(childValue, key, add)
		}
		addOfficeOutputValues(values, add)
	case []any:
		for _, item := range typed {
			collectOfficePathsFromValue(item, key, add)
		}
	}
}

func collectOfficeOutputObjectPaths(value any, add func(string)) {
	switch typed := value.(type) {
	case string:
		add(typed)
	case map[string]any:
		values := map[string][]string{}
		for childKey, childValue := range typed {
			key := normalizeAttributionKey(childKey)
			if stringValue, ok := childValue.(string); ok {
				addOfficeOutputValue(values, key, stringValue)
				continue
			}
			if isOfficeOutputContainerKey(key) {
				collectOfficeOutputContainerPaths(childValue, add)
				continue
			}
			if isOfficeOutputObjectPathContainerKey(key) {
				collectOfficeOutputObjectPaths(childValue, add)
				continue
			}
			if isOfficeOutputObjectKey(key) {
				collectOfficeOutputObjectPaths(childValue, add)
			}
		}
		addOfficeOutputObjectPathValues(values, add)
	case []any:
		for _, item := range typed {
			collectOfficeOutputObjectPaths(item, add)
		}
	}
}

func collectOfficeOutputContainerPaths(value any, add func(string)) {
	switch typed := value.(type) {
	case string:
		add(typed)
	case map[string]any:
		if hasOfficeOutputObjectPathFields(typed) {
			collectOfficeOutputObjectPaths(typed, add)
			return
		}
		for childKey, childValue := range typed {
			if isOfficeSourceLikeKey(childKey) {
				continue
			}
			collectOfficeOutputContainerPaths(childValue, add)
		}
	case []any:
		for _, item := range typed {
			collectOfficeOutputContainerPaths(item, add)
		}
	}
}

func addOfficeOutputObjectPathValues(values map[string][]string, add func(string)) {
	addOfficeOutputValues(values, add)
	for key, rawValues := range values {
		if !isOfficeOutputObjectPathValueKey(key) {
			continue
		}
		for _, value := range rawValues {
			add(value)
		}
	}
}

func hasOfficeOutputObjectPathFields(value map[string]any) bool {
	for childKey := range value {
		key := normalizeAttributionKey(childKey)
		if isOfficeOutputObjectPathValueKey(key) ||
			isOfficeOutputContainerKey(key) ||
			isOfficeOutputObjectKey(key) ||
			isOfficeOutputObjectPathContainerKey(key) {
			return true
		}
	}
	return false
}

func normalizeAttributionKey(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "-")
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, " ", "_")
	var builder strings.Builder
	for index, char := range key {
		if char >= 'A' && char <= 'Z' {
			if index > 0 {
				builder.WriteByte('_')
			}
			builder.WriteRune(char + ('a' - 'A'))
			continue
		}
		builder.WriteRune(char)
	}
	return strings.ToLower(builder.String())
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

func isOfficeTextOutputLabel(label string) bool {
	switch normalizeAttributionKey(label) {
	case "o",
		"output", "output_file", "output_path",
		"out", "out_file", "out_path",
		"saved_to", "saved_as", "save_to", "save_as", "save_file", "save_path",
		"exported_to", "exported_file", "export", "export_file", "export_path",
		"created", "created_file", "created_path",
		"generated", "generated_file", "generated_path",
		"artifact", "artifact_file", "artifact_path",
		"destination", "destination_file", "destination_path",
		"target", "target_file", "target_path",
		"result", "result_file", "result_path":
		return true
	default:
		return false
	}
}

func isOfficeOutputObjectKey(key string) bool {
	switch normalizeAttributionKey(key) {
	case "o",
		"output", "output_file", "output_path",
		"out", "out_file", "out_path",
		"save_as", "save_to", "save_file", "save_path",
		"export", "export_file", "export_path",
		"created", "created_file", "created_path",
		"destination", "destination_file", "destination_path",
		"target", "target_file", "target_path",
		"result", "result_file", "result_path",
		"artifact", "artifact_file", "artifact_path",
		"generated", "generated_file", "generated_path",
		"document_output", "workbook_output", "presentation_output":
		return true
	default:
		return false
	}
}

func isOfficeOutputObjectPathValueKey(key string) bool {
	switch normalizeAttributionKey(key) {
	case "path", "file", "file_path",
		"url", "uri", "href",
		"save_as", "save_to", "save_file", "save_path",
		"export", "export_file", "export_path",
		"created", "created_file", "created_path",
		"document", "document_path",
		"workbook", "workbook_path",
		"presentation", "presentation_path":
		return true
	default:
		return false
	}
}

func isOfficeOutputObjectPathContainerKey(key string) bool {
	switch normalizeAttributionKey(key) {
	case "files", "paths", "urls", "uris",
		"documents", "document_paths",
		"workbooks", "workbook_paths",
		"presentations", "presentation_paths":
		return true
	default:
		return false
	}
}

func isOfficeSourceLikeKey(key string) bool {
	switch normalizeAttributionKey(key) {
	case "input", "inputs",
		"source", "sources",
		"source_file", "source_files", "source_path", "source_paths",
		"template", "templates", "template_file", "template_files", "template_path", "template_paths":
		return true
	default:
		return false
	}
}

func isOfficeOutputContainerKey(key string) bool {
	switch normalizeAttributionKey(key) {
	case "outputs", "output_files", "output_paths",
		"created_files", "created_paths",
		"exported_files", "exported_paths",
		"saved_files", "saved_paths",
		"artifacts", "artifact_files", "artifact_paths",
		"results", "result_files", "result_paths",
		"generated_files", "generated_paths":
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
	if !officeZipHasPackageMarkers(reader.File) {
		return fmt.Errorf("not an office document package")
	}

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
	foundContentTypes := false
	foundRelationships := false
	writtenOfficePackagePart := map[string]bool{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		partName := normalizedOfficePackagePartName(file.Name)
		if partName != "" {
			if writtenOfficePackagePart[partName] {
				continue
			}
			writtenOfficePackagePart[partName] = true
		}
		data, err := readOfficeZipFile(file)
		if err != nil {
			_ = writer.Close()
			_ = temp.Close()
			return err
		}
		if partName == officeCorePropertiesTarget {
			foundCore = true
			data = officeCorePropertiesXML(data, agentName, byline)
		}
		if partName == "[Content_Types].xml" {
			foundContentTypes = true
			data = officeContentTypesXML(data)
		}
		if partName == "_rels/.rels" {
			foundRelationships = true
			data = officeRelationshipsXML(data)
		}
		header := file.FileHeader
		if partName != "" {
			header.Name = partName
		}
		if err := writeOfficeZipFile(writer, header, data); err != nil {
			_ = writer.Close()
			_ = temp.Close()
			return err
		}
	}
	if !foundContentTypes {
		header := &zip.FileHeader{Name: "[Content_Types].xml", Method: zip.Deflate}
		header.SetMode(0o644)
		if err := writeOfficeZipFile(writer, *header, officeContentTypesXML(nil)); err != nil {
			_ = writer.Close()
			_ = temp.Close()
			return err
		}
	}
	if !foundRelationships {
		header := &zip.FileHeader{Name: "_rels/.rels", Method: zip.Deflate}
		header.SetMode(0o644)
		if err := writeOfficeZipFile(writer, *header, officeRelationshipsXML(nil)); err != nil {
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

func officeContentTypesXML(data []byte) []byte {
	contentTypes := officeContentTypes{XMLNS: officeContentTypesNS}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := xml.Unmarshal(data, &contentTypes); err != nil {
			return data
		}
	}
	if contentTypes.XMLNS == "" {
		contentTypes.XMLNS = officeContentTypesNS
	}
	hasCore := false
	overrides := make([]officeContentTypeOverride, 0, len(contentTypes.Overrides)+1)
	for _, override := range contentTypes.Overrides {
		if sameOfficePartName(override.PartName, officeCorePropertiesTarget) {
			if hasCore {
				continue
			}
			override.PartName = "/" + officeCorePropertiesTarget
			override.ContentType = officeCorePropertiesCT
			hasCore = true
		}
		overrides = append(overrides, override)
	}
	if !hasCore {
		overrides = append(overrides, officeContentTypeOverride{
			PartName:    "/" + officeCorePropertiesTarget,
			ContentType: officeCorePropertiesCT,
		})
	}
	contentTypes.Overrides = overrides
	out, err := xml.MarshalIndent(contentTypes, "", "  ")
	if err != nil {
		return data
	}
	return append([]byte(xml.Header), out...)
}

func officeRelationshipsXML(data []byte) []byte {
	relationships := officeRelationships{XMLNS: officeRelationshipsNS}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := xml.Unmarshal(data, &relationships); err != nil {
			return data
		}
	}
	if relationships.XMLNS == "" {
		relationships.XMLNS = officeRelationshipsNS
	}
	hasCore := false
	ids := map[string]bool{}
	normalized := make([]officeRelationship, 0, len(relationships.Relationships)+1)
	for _, relationship := range relationships.Relationships {
		ids[relationship.ID] = true
		if relationship.Type == officeCorePropertiesRel || sameOfficePartName(relationship.Target, officeCorePropertiesTarget) {
			if hasCore {
				continue
			}
			relationship.Type = officeCorePropertiesRel
			relationship.Target = officeCorePropertiesTarget
			relationship.TargetMode = ""
			hasCore = true
		}
		normalized = append(normalized, relationship)
	}
	if !hasCore {
		normalized = append(normalized, officeRelationship{
			ID:     nextOfficeRelationshipID(ids),
			Type:   officeCorePropertiesRel,
			Target: officeCorePropertiesTarget,
		})
	}
	relationships.Relationships = normalized
	out, err := xml.MarshalIndent(relationships, "", "  ")
	if err != nil {
		return data
	}
	return append([]byte(xml.Header), out...)
}

func sameOfficePartName(value, target string) bool {
	return strings.TrimLeft(strings.TrimSpace(value), "/") == strings.TrimLeft(strings.TrimSpace(target), "/")
}

func normalizedOfficePackagePartName(name string) string {
	name = strings.TrimLeft(filepath.ToSlash(name), "/")
	switch name {
	case "[Content_Types].xml", "_rels/.rels", officeCorePropertiesTarget:
		return name
	default:
		return ""
	}
}

func officeZipHasPackageMarkers(files []*zip.File) bool {
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimLeft(filepath.ToSlash(file.Name), "/")
		if name == "[Content_Types].xml" || name == "_rels/.rels" || name == officeCorePropertiesTarget {
			return true
		}
		if strings.HasPrefix(name, "word/") || strings.HasPrefix(name, "xl/") || strings.HasPrefix(name, "ppt/") || strings.HasPrefix(name, "docProps/") {
			return true
		}
	}
	return false
}

func nextOfficeRelationshipID(ids map[string]bool) string {
	for index := 1; ; index++ {
		id := fmt.Sprintf("rId%d", index)
		if !ids[id] {
			return id
		}
	}
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
