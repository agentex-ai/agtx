package core

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const semanticManifestName = ".agentex-manifest.json"

type GoalOptions struct {
	Args []string
}

type SemanticGoalResult struct {
	Input                string                  `json:"input"`
	ParsedGoal           SemanticParsedGoal      `json:"parsed_goal"`
	IR                   SemanticIR              `json:"ir"`
	Plan                 []SemanticOperation     `json:"plan"`
	Result               SemanticExecutionResult `json:"result"`
	ConfirmationRequired bool                    `json:"confirmation_required"`
	ManifestPath         string                  `json:"manifest_path,omitempty"`
}

type SemanticParsedGoal struct {
	Intent         string `json:"intent"`
	Object         string `json:"object"`
	State          string `json:"state"`
	TimeRef        string `json:"time_ref"`
	DestinationRef string `json:"destination_ref"`
	Mode           string `json:"mode"`
}

type SemanticIR struct {
	Version     int                    `json:"version"`
	Goal        SemanticIRGoal         `json:"goal"`
	Mode        string                 `json:"mode"`
	Objects     []SemanticObjectHandle `json:"objects"`
	Constraints SemanticConstraints    `json:"constraints"`
	Policy      SemanticPolicy         `json:"policy"`
}

type SemanticIRGoal struct {
	Intent     string `json:"intent"`
	State      string `json:"state"`
	ObjectType string `json:"object_type"`
}

type SemanticObjectHandle struct {
	Handle   string                 `json:"handle"`
	Type     string                 `json:"type"`
	Provider string                 `json:"provider"`
	Metadata SemanticObjectMetadata `json:"metadata"`
}

type SemanticObjectMetadata struct {
	SourcePath      string `json:"source_path,omitempty"`
	Count           int    `json:"count,omitempty"`
	SizeBytes       int64  `json:"size_bytes,omitempty"`
	ModifiedStart   string `json:"modified_start,omitempty"`
	ModifiedEnd     string `json:"modified_end,omitempty"`
	PermissionScope string `json:"permission_scope"`
}

type SemanticConstraints struct {
	RequiredReplicas    int                `json:"required_replicas"`
	DestinationReplicas int                `json:"destination_replicas"`
	DestinationRef      string             `json:"destination_ref"`
	TimeWindow          SemanticTimeWindow `json:"time_window"`
}

type SemanticTimeWindow struct {
	Ref   string `json:"ref"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type SemanticPolicy struct {
	DryRun           bool   `json:"dry_run"`
	Overwrite        bool   `json:"overwrite"`
	ConflictBehavior string `json:"conflict_behavior"`
	Approval         string `json:"approval"`
}

type SemanticOperation struct {
	Op          string `json:"op"`
	Status      string `json:"status"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Reason      string `json:"reason,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

type SemanticExecutionResult struct {
	Applied   bool                 `json:"applied"`
	Scanned   int                  `json:"scanned"`
	Planned   int                  `json:"planned"`
	Copied    int                  `json:"copied"`
	Skipped   int                  `json:"skipped"`
	Conflicts int                  `json:"conflicts"`
	Failed    int                  `json:"failed"`
	Files     []SemanticFileResult `json:"files,omitempty"`
}

type SemanticFileResult struct {
	Path        string `json:"path"`
	Destination string `json:"destination"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

type semanticGoalRunOptions struct {
	photoRoot string
	nasRoot   string
	manifest  string
	today     string
	apply     bool
	overwrite bool
}

type semanticFile struct {
	path    string
	rel     string
	size    int64
	modTime time.Time
}

type semanticManifest struct {
	SchemaVersion int                     `json:"schema_version"`
	UpdatedAt     string                  `json:"updated_at"`
	Goal          SemanticIRGoal          `json:"goal"`
	Mode          string                  `json:"mode"`
	Objects       []SemanticObjectHandle  `json:"objects"`
	Constraints   SemanticConstraints     `json:"constraints"`
	Policy        SemanticPolicy          `json:"policy"`
	Result        SemanticExecutionResult `json:"result"`
}

func (s *Service) RunGoal(_ context.Context, text string, options GoalOptions) (SemanticGoalResult, error) {
	parsed, err := parseSemanticGoalText(text)
	if err != nil {
		return SemanticGoalResult{}, err
	}
	runOptions, err := parseSemanticGoalArgs(options.Args)
	if err != nil {
		return SemanticGoalResult{}, err
	}
	if err := requireDir(runOptions.photoRoot, "--photo-root"); err != nil {
		return SemanticGoalResult{}, err
	}
	if err := requireDir(runOptions.nasRoot, "--nas-root"); err != nil {
		return SemanticGoalResult{}, err
	}
	if runOptions.manifest == "" {
		runOptions.manifest = filepath.Join(runOptions.nasRoot, semanticManifestName)
	}

	window, start, end, err := semanticTimeWindow(parsed.TimeRef, runOptions.today)
	if err != nil {
		return SemanticGoalResult{}, err
	}
	files, err := scanSemanticPhotos(runOptions.photoRoot, start, end)
	if err != nil {
		return SemanticGoalResult{}, err
	}
	objects := semanticObjects(parsed, runOptions, files)
	ir := SemanticIR{
		Version: 1,
		Goal: SemanticIRGoal{
			Intent:     parsed.Intent,
			State:      parsed.State,
			ObjectType: parsed.Object,
		},
		Mode:    parsed.Mode,
		Objects: objects,
		Constraints: SemanticConstraints{
			RequiredReplicas:    2,
			DestinationReplicas: 1,
			DestinationRef:      parsed.DestinationRef,
			TimeWindow:          window,
		},
		Policy: SemanticPolicy{
			DryRun:           !runOptions.apply,
			Overwrite:        runOptions.overwrite,
			ConflictBehavior: "skip",
			Approval:         approvalMode(runOptions.apply),
		},
	}
	result := SemanticGoalResult{Input: strings.TrimSpace(text), ParsedGoal: parsed, IR: ir, ManifestPath: runOptions.manifest}
	result.Plan, result.Result = executeSemanticPlan(files, runOptions)
	result.ConfirmationRequired = !runOptions.apply && result.Result.Planned > 0
	if runOptions.apply {
		if err := writeSemanticManifest(runOptions.manifest, result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func parseSemanticGoalText(text string) (SemanticParsedGoal, error) {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if trimmed == "" {
		return SemanticParsedGoal{}, NewError(CodeInvalidArgument, "goal is required", nil)
	}
	// ponytail: keyword classifier for one MVP goal family; replace with a real parser when more goal families ship.
	if !containsAny(lower, "照片", "相片", "图片", "photo", "photos", "image", "images") ||
		!containsAny(lower, "备份", "同步", "存起来", "保存", "副本", "backup", "copy", "sync", "save", "replica") {
		return SemanticParsedGoal{}, NewError(CodeNotImplemented, "unsupported semantic goal", map[string]any{
			"supported_objects": []string{"photo_collection"},
			"supported_states":  []string{"replicated"},
		})
	}
	mode := "execute_once"
	if containsAny(lower, "始终", "一直", "总是", "应该有", "ensure", "always", "reconcile") {
		mode = "reconcile_state"
	}
	timeRef := "all"
	if strings.Contains(lower, "昨天") || strings.Contains(lower, "yesterday") {
		timeRef = "yesterday"
	}
	destinationRef := "configured_destination"
	if strings.Contains(lower, "nas") {
		destinationRef = "nas"
	}
	return SemanticParsedGoal{
		Intent:         "ensure",
		Object:         "photo_collection",
		State:          "replicated",
		TimeRef:        timeRef,
		DestinationRef: destinationRef,
		Mode:           mode,
	}, nil
}

func parseSemanticGoalArgs(args []string) (semanticGoalRunOptions, error) {
	var options semanticGoalRunOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--apply":
			options.apply = true
		case arg == "--overwrite":
			options.overwrite = true
		case arg == "--photo-root" || arg == "--nas-root" || arg == "--manifest" || arg == "--today":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return options, NewError(CodeInvalidArgument, arg+" requires a value", map[string]any{"flag": arg, "supported_flags": semanticGoalFlags()})
			}
			setSemanticGoalArg(&options, arg, args[i+1])
			i++
		case strings.HasPrefix(arg, "--photo-root="):
			options.photoRoot = strings.TrimPrefix(arg, "--photo-root=")
		case strings.HasPrefix(arg, "--nas-root="):
			options.nasRoot = strings.TrimPrefix(arg, "--nas-root=")
		case strings.HasPrefix(arg, "--manifest="):
			options.manifest = strings.TrimPrefix(arg, "--manifest=")
		case strings.HasPrefix(arg, "--today="):
			options.today = strings.TrimPrefix(arg, "--today=")
		default:
			return options, NewError(CodeInvalidArgument, "unexpected semantic goal argument", map[string]any{"arg": arg, "supported_flags": semanticGoalFlags()})
		}
	}
	if strings.TrimSpace(options.photoRoot) == "" || strings.TrimSpace(options.nasRoot) == "" {
		return options, NewError(CodeInvalidArgument, "semantic goal requires --photo-root and --nas-root", map[string]any{"supported_flags": semanticGoalFlags()})
	}
	var err error
	options.photoRoot, err = filepath.Abs(options.photoRoot)
	if err != nil {
		return options, err
	}
	options.nasRoot, err = filepath.Abs(options.nasRoot)
	if err != nil {
		return options, err
	}
	if options.manifest != "" {
		options.manifest, err = filepath.Abs(options.manifest)
		if err != nil {
			return options, err
		}
	}
	return options, nil
}

func semanticGoalFlags() []string {
	return []string{"--photo-root", "--nas-root", "--manifest", "--today", "--apply", "--overwrite"}
}

func setSemanticGoalArg(options *semanticGoalRunOptions, flag, value string) {
	switch flag {
	case "--photo-root":
		options.photoRoot = value
	case "--nas-root":
		options.nasRoot = value
	case "--manifest":
		options.manifest = value
	case "--today":
		options.today = value
	}
}

func semanticTimeWindow(ref, today string) (SemanticTimeWindow, time.Time, time.Time, error) {
	if ref != "yesterday" {
		return SemanticTimeWindow{Ref: ref}, time.Time{}, time.Time{}, nil
	}
	var todayStart time.Time
	if strings.TrimSpace(today) == "" {
		now := time.Now()
		todayStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	} else {
		parsed, err := time.ParseInLocation("2006-01-02", today, time.Local)
		if err != nil {
			return SemanticTimeWindow{}, time.Time{}, time.Time{}, NewError(CodeInvalidArgument, "--today must be YYYY-MM-DD", map[string]any{"flag": "--today"})
		}
		todayStart = parsed
	}
	start := todayStart.AddDate(0, 0, -1)
	end := todayStart
	return SemanticTimeWindow{Ref: ref, Start: formatSemanticTime(start), End: formatSemanticTime(end)}, start, end, nil
}

func scanSemanticPhotos(root string, start, end time.Time) ([]semanticFile, error) {
	var files []semanticFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !isSemanticPhoto(path) {
			return nil
		}
		if !start.IsZero() && (info.ModTime().Before(start) || !info.ModTime().Before(end)) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, semanticFile{path: path, rel: filepath.ToSlash(rel), size: info.Size(), modTime: info.ModTime()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, err
}

func semanticObjects(parsed SemanticParsedGoal, options semanticGoalRunOptions, files []semanticFile) []SemanticObjectHandle {
	var size int64
	var minMod, maxMod time.Time
	for index, file := range files {
		size += file.size
		if index == 0 || file.modTime.Before(minMod) {
			minMod = file.modTime
		}
		if index == 0 || file.modTime.After(maxMod) {
			maxMod = file.modTime
		}
	}
	metadata := SemanticObjectMetadata{SourcePath: options.photoRoot, Count: len(files), SizeBytes: size, PermissionScope: "read"}
	if len(files) > 0 {
		metadata.ModifiedStart = formatSemanticTime(minMod)
		metadata.ModifiedEnd = formatSemanticTime(maxMod)
	}
	return []SemanticObjectHandle{
		{
			Handle:   "photo_collection://" + parsed.TimeRef,
			Type:     parsed.Object,
			Provider: "local_fs",
			Metadata: metadata,
		},
		{
			Handle:   "directory://" + parsed.DestinationRef,
			Type:     "directory",
			Provider: "nas_fs",
			Metadata: SemanticObjectMetadata{SourcePath: options.nasRoot, PermissionScope: "write"},
		},
	}
}

func executeSemanticPlan(files []semanticFile, options semanticGoalRunOptions) ([]SemanticOperation, SemanticExecutionResult) {
	plan := make([]SemanticOperation, 0, len(files))
	result := SemanticExecutionResult{Applied: options.apply, Scanned: len(files)}
	for _, file := range files {
		destination := filepath.Join(options.nasRoot, filepath.FromSlash(file.rel))
		op := SemanticOperation{Op: "copy", Status: "planned", Source: file.path, Destination: destination, SizeBytes: file.size}
		fileResult := SemanticFileResult{Path: file.rel, Destination: destination, Status: "planned"}
		info, statErr := os.Stat(destination)
		switch {
		case statErr == nil && sameSemanticSignature(file, info):
			op.Op = "skip"
			op.Status = "skipped"
			op.Reason = "already_replicated"
			fileResult.Status = op.Status
			fileResult.Reason = op.Reason
			result.Skipped++
		case statErr == nil && !options.overwrite:
			op.Op = "skip"
			op.Status = "conflict"
			op.Reason = "destination_exists"
			fileResult.Status = op.Status
			fileResult.Reason = op.Reason
			result.Conflicts++
		case statErr != nil && !os.IsNotExist(statErr):
			op.Op = "skip"
			op.Status = "failed"
			op.Reason = statErr.Error()
			fileResult.Status = op.Status
			fileResult.Reason = op.Reason
			result.Failed++
		case !options.apply:
			result.Planned++
		default:
			if err := copySemanticFile(file, destination, options.overwrite); err != nil {
				op.Op = "copy"
				op.Status = "failed"
				op.Reason = err.Error()
				fileResult.Status = op.Status
				fileResult.Reason = op.Reason
				result.Failed++
			} else {
				op.Status = "copied"
				fileResult.Status = op.Status
				result.Copied++
			}
		}
		plan = append(plan, op)
		result.Files = append(result.Files, fileResult)
	}
	return plan, result
}

func copySemanticFile(file semanticFile, destination string, overwrite bool) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if overwrite {
		return copySemanticFileReplacing(file, destination)
	}
	src, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(destination, file.modTime, file.modTime)
}

func copySemanticFileReplacing(file semanticFile, destination string) error {
	src, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer src.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".tmp-*")
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
	if _, err := io.Copy(temp, src); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0o644); err != nil {
		return err
	}
	if err := os.Chtimes(tempName, file.modTime, file.modTime); err != nil {
		return err
	}
	if err := renameReplacing(tempName, destination); err != nil {
		return err
	}
	cleanup = false
	return syncDirectory(filepath.Dir(destination))
}

func writeSemanticManifest(path string, result SemanticGoalResult) error {
	manifest := semanticManifest{
		SchemaVersion: 1,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Goal:          result.IR.Goal,
		Mode:          result.IR.Mode,
		Objects:       result.IR.Objects,
		Constraints:   result.IR.Constraints,
		Policy:        result.IR.Policy,
		Result:        result.Result,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

func isSemanticPhoto(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".heic", ".heif", ".webp", ".gif", ".tif", ".tiff", ".bmp":
		return true
	default:
		return false
	}
}

func sameSemanticSignature(file semanticFile, info os.FileInfo) bool {
	if info.IsDir() || info.Size() != file.size {
		return false
	}
	// ponytail: size+mtime is the v0 file signature; switch to hashes when collision risk matters.
	diff := info.ModTime().Sub(file.modTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= time.Second
}

func requireDir(path, flag string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewError(CodeNotFound, flag+" does not exist", map[string]any{"path": path})
		}
		return err
	}
	if !info.IsDir() {
		return NewError(CodeInvalidArgument, flag+" must be a directory", map[string]any{"path": path})
	}
	return nil
}

func approvalMode(apply bool) string {
	if apply {
		return "approved"
	}
	return "dry_run_until_apply"
}

func formatSemanticTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
