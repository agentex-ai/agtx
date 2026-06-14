package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	pathpkg "path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const defaultOfficeInputMaxBytes int64 = 64 * 1024 * 1024
const defaultOfficeEntryMaxBytes int64 = 16 * 1024 * 1024
const defaultOfficeMaxEntries = 4096

var xlsxCellRefPattern = regexp.MustCompile(`^[A-Za-z]+[0-9]+$`)

type builtinOfficeInput struct {
	Path    string `json:"path,omitempty"`
	Input   string `json:"input,omitempty"`
	File    string `json:"file,omitempty"`
	Action  string `json:"action,omitempty"`
	MaxRows int    `json:"max_rows,omitempty"`
}

type builtinOfficeOutput struct {
	Kind       string              `json:"kind"`
	Source     string              `json:"source,omitempty"`
	Action     string              `json:"action"`
	Text       string              `json:"text,omitempty"`
	Properties map[string]string   `json:"properties,omitempty"`
	Parts      []builtinOfficePart `json:"parts,omitempty"`
	Sheets     []builtinXLSXSheet  `json:"sheets,omitempty"`
	Slides     []builtinPPTXSlide  `json:"slides,omitempty"`
	Warnings   []string            `json:"warnings,omitempty"`
}

type builtinOfficePart struct {
	Name string `json:"name"`
	Text string `json:"text,omitempty"`
}

type builtinXLSXSheet struct {
	Name  string            `json:"name"`
	Rows  []builtinXLSXRow  `json:"rows,omitempty"`
	Text  string            `json:"text,omitempty"`
	Cells []builtinXLSXCell `json:"cells,omitempty"`
}

type builtinXLSXRow struct {
	Index  int      `json:"index,omitempty"`
	Values []string `json:"values"`
}

type builtinXLSXCell struct {
	Ref   string `json:"ref,omitempty"`
	Value string `json:"value,omitempty"`
}

type builtinPPTXSlide struct {
	Index int      `json:"index"`
	Text  string   `json:"text,omitempty"`
	Notes string   `json:"notes,omitempty"`
	Parts []string `json:"parts,omitempty"`
}

type officeZipPackage struct {
	files map[string]*zip.File
}

func (s *Service) runBuiltinOffice(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	default:
	}
	input, err := parseBuiltinOfficeInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	data, source, err := builtinOfficeBytes(input, options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	pkg, err := openOfficeZip(data)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	kind := canonicalSkillName(manifest.Name)
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		action = "extract"
	}
	var output builtinOfficeOutput
	switch kind {
	case "docx":
		output, err = extractBuiltinDOCX(pkg, source, action)
	case "xlsx":
		output, err = extractBuiltinXLSX(pkg, source, action, input.MaxRows)
	case "pptx":
		output, err = extractBuiltinPPTX(pkg, source, action)
	default:
		return RunResult{ExitCode: -1}, NewError(CodeNotImplemented, "built-in office runtime is not implemented for skill", map[string]any{"skill": manifest.Name})
	}
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	data, err = json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinOfficeInput(options RunOptions) (builtinOfficeInput, error) {
	input := builtinOfficeInput{
		Path:    officeOptionValue(options.Args, "path", ""),
		Action:  officeOptionValue(options.Args, "action", ""),
		MaxRows: officeOptionInt(options.Args, "max-rows", 0),
	}
	if input.Path == "" {
		input.Path = officeOptionValue(options.Args, "input", "")
	}
	if input.Path == "" {
		input.Path = officeOptionValue(options.Args, "file", "")
	}
	if len(options.Input) > 0 {
		var payload builtinOfficeInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && (payload.Path != "" || payload.Input != "" || payload.File != "" || payload.Action != "" || payload.MaxRows != 0) {
			if strings.TrimSpace(input.Path) == "" {
				input.Path = firstNonEmpty(payload.Path, payload.Input, payload.File)
			}
			if strings.TrimSpace(input.Action) == "" {
				input.Action = payload.Action
			}
			if input.MaxRows <= 0 {
				input.MaxRows = payload.MaxRows
			}
		}
	}
	if strings.TrimSpace(input.Path) == "" {
		input.Path = firstOfficePathArg(options.Args)
	}
	return input, nil
}

func builtinOfficeBytes(input builtinOfficeInput, options RunOptions) ([]byte, string, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		if len(options.Input) == 0 {
			return nil, "", NewError(CodeInvalidArgument, "office skill requires a file path or --input bytes", map[string]any{"expected": "path argument, --path, JSON path, or CLI --input"})
		}
		return options.Input, "stdin", nil
	}
	if strings.TrimSpace(path) != path || strings.ContainsRune(path, 0) {
		return nil, "", NewError(CodeInvalidArgument, "office input path is invalid", map[string]any{"path": path})
	}
	data, err := readFileLimited(path, defaultOfficeInputMaxBytes, "office document")
	if err != nil {
		return nil, path, err
	}
	return data, path, nil
}

func openOfficeZip(data []byte) (officeZipPackage, error) {
	if int64(len(data)) > defaultOfficeInputMaxBytes {
		return officeZipPackage{}, NewError(CodeSizeLimitExceeded, "office document exceeds configured size limit", map[string]any{"size": len(data), "limit": defaultOfficeInputMaxBytes})
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return officeZipPackage{}, NewError(CodeInvalidArgument, "office document is not a valid OpenXML zip package", nil)
	}
	if len(reader.File) > defaultOfficeMaxEntries {
		return officeZipPackage{}, NewError(CodeSizeLimitExceeded, "office document contains too many parts", map[string]any{"files": len(reader.File), "limit": defaultOfficeMaxEntries})
	}
	files := map[string]*zip.File{}
	for _, file := range reader.File {
		name := pathpkg.Clean(strings.TrimSpace(file.Name))
		if name == "." || strings.HasPrefix(name, "../") || pathpkg.IsAbs(name) || strings.Contains(name, "\\") {
			return officeZipPackage{}, NewError(CodeInvalidArgument, "office document contains unsafe part path", map[string]any{"path": file.Name})
		}
		if file.UncompressedSize64 > uint64(defaultOfficeEntryMaxBytes) {
			return officeZipPackage{}, NewError(CodeSizeLimitExceeded, "office document part exceeds configured size limit", map[string]any{"path": file.Name, "size": file.UncompressedSize64, "limit": defaultOfficeEntryMaxBytes})
		}
		files[name] = file
	}
	return officeZipPackage{files: files}, nil
}

func (p officeZipPackage) readPart(name string) ([]byte, bool, error) {
	name = pathpkg.Clean(strings.TrimSpace(name))
	file, ok := p.files[name]
	if !ok {
		return nil, false, nil
	}
	handle, err := file.Open()
	if err != nil {
		return nil, false, err
	}
	defer handle.Close()
	data, err := readAllLimited(handle, defaultOfficeEntryMaxBytes, "office document part")
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (p officeZipPackage) partNames(prefix, suffix string) []string {
	names := make([]string, 0)
	for name := range p.files {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func extractBuiltinDOCX(pkg officeZipPackage, source, action string) (builtinOfficeOutput, error) {
	if _, ok := pkg.files["word/document.xml"]; !ok {
		return builtinOfficeOutput{}, NewError(CodeInvalidArgument, "DOCX document is missing word/document.xml", map[string]any{"source": source})
	}
	properties := extractOfficeCoreProperties(pkg)
	parts := []builtinOfficePart{}
	mainText, err := textFromOpenXMLPart(pkg, "word/document.xml")
	if err != nil {
		return builtinOfficeOutput{}, err
	}
	parts = append(parts, builtinOfficePart{Name: "word/document.xml", Text: mainText})
	for _, name := range pkg.partNames("word/header", ".xml") {
		text, err := textFromOpenXMLPart(pkg, name)
		if err != nil {
			return builtinOfficeOutput{}, err
		}
		if text != "" {
			parts = append(parts, builtinOfficePart{Name: name, Text: text})
		}
	}
	for _, name := range pkg.partNames("word/footer", ".xml") {
		text, err := textFromOpenXMLPart(pkg, name)
		if err != nil {
			return builtinOfficeOutput{}, err
		}
		if text != "" {
			parts = append(parts, builtinOfficePart{Name: name, Text: text})
		}
	}
	return builtinOfficeOutput{
		Kind:       "docx",
		Source:     source,
		Action:     action,
		Text:       joinOfficeTexts(partsToText(parts)),
		Properties: properties,
		Parts:      parts,
	}, nil
}

func extractBuiltinXLSX(pkg officeZipPackage, source, action string, maxRows int) (builtinOfficeOutput, error) {
	if len(pkg.partNames("xl/worksheets/sheet", ".xml")) == 0 {
		return builtinOfficeOutput{}, NewError(CodeInvalidArgument, "XLSX workbook is missing worksheets", map[string]any{"source": source})
	}
	if maxRows <= 0 {
		maxRows = 1000
	}
	properties := extractOfficeCoreProperties(pkg)
	sharedStrings, err := extractXLSXSharedStrings(pkg)
	if err != nil {
		return builtinOfficeOutput{}, err
	}
	sheetNames := extractXLSXWorkbookSheetNames(pkg)
	worksheetParts := pkg.partNames("xl/worksheets/sheet", ".xml")
	sheets := make([]builtinXLSXSheet, 0, len(worksheetParts))
	for index, name := range worksheetParts {
		sheetName := fmt.Sprintf("Sheet%d", index+1)
		if index < len(sheetNames) && strings.TrimSpace(sheetNames[index]) != "" {
			sheetName = sheetNames[index]
		}
		sheet, err := extractXLSXSheet(pkg, name, sheetName, sharedStrings, maxRows)
		if err != nil {
			return builtinOfficeOutput{}, err
		}
		sheets = append(sheets, sheet)
	}
	return builtinOfficeOutput{
		Kind:       "xlsx",
		Source:     source,
		Action:     action,
		Text:       joinOfficeTexts(xlsxSheetsToText(sheets)),
		Properties: properties,
		Sheets:     sheets,
	}, nil
}

func extractBuiltinPPTX(pkg officeZipPackage, source, action string) (builtinOfficeOutput, error) {
	slideParts := pkg.partNames("ppt/slides/slide", ".xml")
	if len(slideParts) == 0 {
		return builtinOfficeOutput{}, NewError(CodeInvalidArgument, "PPTX deck is missing slides", map[string]any{"source": source})
	}
	properties := extractOfficeCoreProperties(pkg)
	slides := make([]builtinPPTXSlide, 0, len(slideParts))
	for index, name := range slideParts {
		text, err := textFromOpenXMLPart(pkg, name)
		if err != nil {
			return builtinOfficeOutput{}, err
		}
		slide := builtinPPTXSlide{Index: index + 1, Text: text, Parts: []string{name}}
		notesName := fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", index+1)
		if _, ok := pkg.files[notesName]; ok {
			notes, err := textFromOpenXMLPart(pkg, notesName)
			if err != nil {
				return builtinOfficeOutput{}, err
			}
			slide.Notes = notes
			slide.Parts = append(slide.Parts, notesName)
		}
		slides = append(slides, slide)
	}
	return builtinOfficeOutput{
		Kind:       "pptx",
		Source:     source,
		Action:     action,
		Text:       joinOfficeTexts(pptxSlidesToText(slides)),
		Properties: properties,
		Slides:     slides,
	}, nil
}

func textFromOpenXMLPart(pkg officeZipPackage, name string) (string, error) {
	data, ok, err := pkg.readPart(name)
	if err != nil || !ok {
		return "", err
	}
	return extractOpenXMLText(data), nil
}

func extractOpenXMLText(data []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch typed := token.(type) {
		case xml.CharData:
			text := string(typed)
			if text != "" {
				builder.WriteString(text)
			}
		case xml.StartElement:
			switch typed.Name.Local {
			case "tab":
				builder.WriteByte('\t')
			case "br", "cr":
				builder.WriteByte('\n')
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "p", "tr", "row", "tc", "slide", "section", "li", "div":
				text := builder.String()
				if text != "" && !strings.HasSuffix(text, "\n") {
					builder.WriteByte('\n')
				}
			}
		}
	}
	return normalizeOfficeText(builder.String())
}

func extractOfficeCoreProperties(pkg officeZipPackage) map[string]string {
	data, ok, err := pkg.readPart("docProps/core.xml")
	if err != nil || !ok {
		return nil
	}
	properties := map[string]string{}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "title", "subject", "creator", "description", "keywords", "lastModifiedBy", "created", "modified", "category":
			var value string
			if err := decoder.DecodeElement(&value, &start); err == nil && strings.TrimSpace(value) != "" {
				properties[start.Name.Local] = strings.TrimSpace(value)
			}
		}
	}
	if len(properties) == 0 {
		return nil
	}
	return properties
}

func extractXLSXSharedStrings(pkg officeZipPackage) ([]string, error) {
	data, ok, err := pkg.readPart("xl/sharedStrings.xml")
	if err != nil || !ok {
		return nil, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}
		var raw struct {
			Text []string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		}
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			return nil, err
		}
		parts := append([]string{}, raw.Text...)
		for _, run := range raw.Runs {
			parts = append(parts, run.Text)
		}
		values = append(values, normalizeOfficeText(strings.Join(parts, "")))
	}
	return values, nil
}

func extractXLSXWorkbookSheetNames(pkg officeZipPackage) []string {
	data, ok, err := pkg.readPart("xl/workbook.xml")
	if err != nil || !ok {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var names []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local == "name" && strings.TrimSpace(attr.Value) != "" {
				names = append(names, strings.TrimSpace(attr.Value))
				break
			}
		}
	}
	return names
}

func extractXLSXSheet(pkg officeZipPackage, partName, sheetName string, sharedStrings []string, maxRows int) (builtinXLSXSheet, error) {
	data, ok, err := pkg.readPart(partName)
	if err != nil || !ok {
		return builtinXLSXSheet{}, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var rows []builtinXLSXRow
	var cells []builtinXLSXCell
	currentRow := builtinXLSXRow{}
	inRow := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return builtinXLSXSheet{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "row":
				inRow = true
				currentRow = builtinXLSXRow{Index: attrInt(typed.Attr, "r")}
			case "c":
				value, ref, err := decodeXLSXCell(decoder, typed, sharedStrings)
				if err != nil {
					return builtinXLSXSheet{}, err
				}
				if inRow {
					currentRow.Values = append(currentRow.Values, value)
				}
				if value != "" || ref != "" {
					cells = append(cells, builtinXLSXCell{Ref: ref, Value: value})
				}
			}
		case xml.EndElement:
			if typed.Name.Local == "row" && inRow {
				if len(currentRow.Values) > 0 {
					rows = append(rows, currentRow)
					if len(rows) >= maxRows {
						return builtinXLSXSheet{Name: sheetName, Rows: rows, Cells: cells, Text: rowsToText(rows)}, nil
					}
				}
				inRow = false
			}
		}
	}
	return builtinXLSXSheet{Name: sheetName, Rows: rows, Cells: cells, Text: rowsToText(rows)}, nil
}

func decodeXLSXCell(decoder *xml.Decoder, start xml.StartElement, sharedStrings []string) (string, string, error) {
	ref := ""
	cellType := ""
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "r":
			if xlsxCellRefPattern.MatchString(attr.Value) {
				ref = attr.Value
			}
		case "t":
			cellType = attr.Value
		}
	}
	var raw struct {
		Value      string `xml:"v"`
		InlineText struct {
			Text []string `xml:"t"`
			Runs []struct {
				Text string `xml:"t"`
			} `xml:"r"`
		} `xml:"is"`
	}
	if err := decoder.DecodeElement(&raw, &start); err != nil {
		return "", ref, err
	}
	value := strings.TrimSpace(raw.Value)
	switch cellType {
	case "s":
		index, err := strconv.Atoi(value)
		if err == nil && index >= 0 && index < len(sharedStrings) {
			value = sharedStrings[index]
		}
	case "inlineStr":
		parts := append([]string{}, raw.InlineText.Text...)
		for _, run := range raw.InlineText.Runs {
			parts = append(parts, run.Text)
		}
		value = strings.Join(parts, "")
	case "str":
		value = strings.TrimSpace(value)
	}
	return normalizeOfficeText(value), ref, nil
}

func normalizeOfficeText(raw string) string {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\t", " \t ")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			out = append(out, strings.Join(fields, " "))
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func rowsToText(rows []builtinXLSXRow) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row.Values) > 0 {
			lines = append(lines, strings.Join(row.Values, "\t"))
		}
	}
	return strings.Join(lines, "\n")
}

func partsToText(parts []builtinOfficePart) []string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			values = append(values, part.Text)
		}
	}
	return values
}

func xlsxSheetsToText(sheets []builtinXLSXSheet) []string {
	values := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		if strings.TrimSpace(sheet.Text) == "" {
			continue
		}
		values = append(values, sheet.Name+"\n"+sheet.Text)
	}
	return values
}

func pptxSlidesToText(slides []builtinPPTXSlide) []string {
	values := make([]string, 0, len(slides))
	for _, slide := range slides {
		parts := []string{}
		if strings.TrimSpace(slide.Text) != "" {
			parts = append(parts, slide.Text)
		}
		if strings.TrimSpace(slide.Notes) != "" {
			parts = append(parts, "Notes:\n"+slide.Notes)
		}
		if len(parts) > 0 {
			values = append(values, fmt.Sprintf("Slide %d\n%s", slide.Index, strings.Join(parts, "\n")))
		}
	}
	return values
}

func joinOfficeTexts(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return strings.Join(out, "\n\n")
}

func attrInt(attrs []xml.Attr, name string) int {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			value, _ := strconv.Atoi(attr.Value)
			return value
		}
	}
	return 0
}

func firstOfficePathArg(args []string) string {
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
		if officeArgTakesValue(arg) {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		return arg
	}
	return ""
}

func officeOptionValue(args []string, name, fallback string) string {
	dash := "--" + name
	underscore := "--" + strings.ReplaceAll(name, "-", "_")
	legacyUnderscore := strings.ReplaceAll(dash, "-", "_")
	key := strings.ReplaceAll(name, "-", "_") + "="
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, dash+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, dash+"="))
		}
		if strings.HasPrefix(arg, underscore+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, underscore+"="))
		}
		if strings.HasPrefix(arg, legacyUnderscore+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, legacyUnderscore+"="))
		}
		if strings.HasPrefix(arg, key) {
			return strings.TrimSpace(strings.TrimPrefix(arg, key))
		}
		if (arg == dash || arg == underscore || arg == legacyUnderscore) && index+1 < len(args) {
			return strings.TrimSpace(args[index+1])
		}
	}
	return fallback
}

func officeOptionInt(args []string, name string, fallback int) int {
	value := officeOptionValue(args, name, "")
	if value == "" {
		value = officeOptionValue(args, strings.ReplaceAll(name, "-", "_"), "")
	}
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func officeArgTakesValue(arg string) bool {
	switch arg {
	case "--path", "__path", "--input", "__input", "--file", "__file", "--action", "__action", "--max-rows", "--max_rows", "__max_rows":
		return true
	default:
		return false
	}
}
