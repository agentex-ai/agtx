package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSemanticGoalPhrasingsNormalizeToSameIRGoal(t *testing.T) {
	phrases := []string{
		"把昨天的照片备份到 NAS",
		"昨天照片同步NAS",
		"昨天的图片帮我存起来",
	}
	var first SemanticParsedGoal
	for index, phrase := range phrases {
		parsed, err := parseSemanticGoalText(phrase)
		if err != nil {
			t.Fatalf("parse %q: %v", phrase, err)
		}
		if index == 0 {
			first = parsed
			continue
		}
		if parsed.Intent != first.Intent || parsed.Object != first.Object || parsed.State != first.State || parsed.TimeRef != first.TimeRef || parsed.Mode != first.Mode {
			t.Fatalf("expected same parsed goal:\nfirst=%#v\nparsed=%#v", first, parsed)
		}
	}
}

func TestSemanticGoalExecutesDryRunApplySkipAndReconcile(t *testing.T) {
	root := t.TempDir()
	photoRoot := filepath.Join(root, "photos")
	nasRoot := filepath.Join(root, "nas")
	if err := os.MkdirAll(filepath.Join(photoRoot, "trip"), 0o755); err != nil {
		t.Fatalf("mkdir photos: %v", err)
	}
	if err := os.MkdirAll(nasRoot, 0o755); err != nil {
		t.Fatalf("mkdir nas: %v", err)
	}
	yesterday := mustLocalDate(t, "2026-06-21T10:00:00")
	today := mustLocalDate(t, "2026-06-22T10:00:00")
	writeSemanticGoalFixture(t, filepath.Join(photoRoot, "a.jpg"), []byte("a"), yesterday)
	writeSemanticGoalFixture(t, filepath.Join(photoRoot, "trip", "b.png"), []byte("bb"), yesterday)
	writeSemanticGoalFixture(t, filepath.Join(photoRoot, "old.jpg"), []byte("old"), yesterday.AddDate(0, 0, -2))
	writeSemanticGoalFixture(t, filepath.Join(photoRoot, "note.txt"), []byte("nope"), yesterday)

	service := NewService(PathsForRoot(filepath.Join(root, "home")))
	args := []string{"--photo-root", photoRoot, "--nas-root", nasRoot, "--today", "2026-06-22"}
	dryRun, err := service.RunGoal(context.Background(), "把昨天的照片备份到 NAS", GoalOptions{Args: args})
	if err != nil {
		t.Fatalf("dry-run goal: %v", err)
	}
	if dryRun.IR.Goal.Intent != "ensure" || dryRun.IR.Goal.State != "replicated" || dryRun.IR.Goal.ObjectType != "photo_collection" {
		t.Fatalf("unexpected IR goal: %#v", dryRun.IR.Goal)
	}
	if dryRun.IR.Policy.Overwrite || !dryRun.IR.Policy.DryRun {
		t.Fatalf("expected dry-run with overwrite=false: %#v", dryRun.IR.Policy)
	}
	if dryRun.IR.Constraints.TimeWindow.Start == "" || dryRun.IR.Constraints.TimeWindow.End == "" {
		t.Fatalf("expected yesterday window: %#v", dryRun.IR.Constraints.TimeWindow)
	}
	if dryRun.Result.Scanned != 2 || dryRun.Result.Planned != 2 || !dryRun.ConfirmationRequired {
		t.Fatalf("unexpected dry-run result: %#v", dryRun.Result)
	}
	if _, err := os.Stat(filepath.Join(nasRoot, "a.jpg")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not copy, stat err=%v", err)
	}
	firstIR, err := json.Marshal(dryRun.IR)
	if err != nil {
		t.Fatalf("marshal IR: %v", err)
	}
	secondIR, err := json.Marshal(dryRun.IR)
	if err != nil {
		t.Fatalf("marshal IR again: %v", err)
	}
	if string(firstIR) != string(secondIR) {
		t.Fatalf("IR JSON should be stable")
	}

	applyArgs := append(append([]string{}, args...), "--apply")
	applied, err := service.RunGoal(context.Background(), "把昨天的照片备份到 NAS", GoalOptions{Args: applyArgs})
	if err != nil {
		t.Fatalf("apply goal: %v", err)
	}
	if !applied.Result.Applied || applied.Result.Copied != 2 || applied.Result.Failed != 0 {
		t.Fatalf("unexpected apply result: %#v", applied.Result)
	}
	if _, err := os.Stat(filepath.Join(nasRoot, semanticManifestName)); err != nil {
		t.Fatalf("expected manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nasRoot, "trip", "b.png")); err != nil {
		t.Fatalf("expected nested copy: %v", err)
	}

	rerun, err := service.RunGoal(context.Background(), "昨天照片同步NAS", GoalOptions{Args: applyArgs})
	if err != nil {
		t.Fatalf("rerun goal: %v", err)
	}
	if rerun.Result.Copied != 0 || rerun.Result.Skipped != 2 {
		t.Fatalf("expected duplicate files skipped: %#v", rerun.Result)
	}

	if err := os.Remove(filepath.Join(nasRoot, "a.jpg")); err != nil {
		t.Fatalf("remove backup: %v", err)
	}
	reconciled, err := service.RunGoal(context.Background(), "昨天照片始终应该有两份备份", GoalOptions{Args: applyArgs})
	if err != nil {
		t.Fatalf("reconcile goal: %v", err)
	}
	if reconciled.IR.Mode != "reconcile_state" || reconciled.Result.Copied != 1 || reconciled.Result.Skipped != 1 {
		t.Fatalf("unexpected reconcile result: mode=%s result=%#v", reconciled.IR.Mode, reconciled.Result)
	}
	if _, err := os.Stat(filepath.Join(nasRoot, "a.jpg")); err != nil {
		t.Fatalf("expected restored backup: %v", err)
	}

	if _, err := service.RunGoal(context.Background(), "把昨天的照片备份到 NAS", GoalOptions{Args: []string{"--photo-root", photoRoot, "--nas-root", filepath.Join(root, "missing"), "--today", "2026-06-22"}}); !IsErrorCode(err, CodeNotFound) {
		t.Fatalf("expected missing NAS to fail safely, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(nasRoot, "a.jpg"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale backup: %v", err)
	}
	conflict, err := service.RunGoal(context.Background(), "把昨天的照片备份到 NAS", GoalOptions{Args: applyArgs})
	if err != nil {
		t.Fatalf("conflict goal: %v", err)
	}
	if conflict.Result.Conflicts != 1 {
		t.Fatalf("expected overwrite=false conflict: %#v", conflict.Result)
	}
	overwriteArgs := append(append([]string{}, applyArgs...), "--overwrite")
	overwritten, err := service.RunGoal(context.Background(), "把昨天的照片备份到 NAS", GoalOptions{Args: overwriteArgs})
	if err != nil {
		t.Fatalf("overwrite goal: %v", err)
	}
	if overwritten.Result.Copied != 1 {
		t.Fatalf("expected overwrite copy: %#v", overwritten.Result)
	}
	data, err := os.ReadFile(filepath.Join(nasRoot, "a.jpg"))
	if err != nil {
		t.Fatalf("read overwritten backup: %v", err)
	}
	if string(data) != "a" {
		t.Fatalf("expected overwritten content, got %q", string(data))
	}
	if !today.After(yesterday) {
		t.Fatalf("test fixture dates are wrong")
	}
}

func writeSemanticGoalFixture(t *testing.T, path string, data []byte, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture parent: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes fixture: %v", err)
	}
}

func mustLocalDate(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.Local)
	if err != nil {
		t.Fatalf("parse time %s: %v", value, err)
	}
	return parsed
}
