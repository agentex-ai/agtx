package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func runExecutable(ctx context.Context, versionDir, entrypoint string, options RunOptions) (RunResult, error) {
	executable, err := validateEntrypoint(versionDir, entrypoint)
	if err != nil {
		return RunResult{}, err
	}

	runCtx := ctx
	cancel := func() {}
	if options.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, options.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, executable, options.Args...)
	cmd.Dir = versionDir
	if options.Input != nil {
		cmd.Stdin = bytes.NewReader(options.Input)
	}
	stdout := &limitedBuffer{limit: options.OutputLimitBytes}
	stderr := &limitedBuffer{limit: options.OutputLimitBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	result := RunResult{
		Stdout:           stdout.String(),
		Stderr:           stderr.String(),
		StdoutTruncated:  stdout.Truncated(),
		StderrTruncated:  stderr.Truncated(),
		OutputLimitBytes: options.OutputLimitBytes,
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		return result, NewError(CodeTimeout, "skill timed out", map[string]any{"timeout_ms": options.Timeout.Milliseconds()})
	}
	if stdout.Truncated() || stderr.Truncated() {
		result.ExitCode = -1
		return result, NewError(CodeOutputLimitExceeded, "skill output exceeded limit", map[string]any{"limit_bytes": options.OutputLimitBytes})
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, NewError(CodeRunFailed, "skill exited with a non-zero status", map[string]any{"exit_code": result.ExitCode})
		}
		result.ExitCode = -1
		return result, err
	}
	result.ExitCode = 0
	return result, nil
}

func validateEntrypoint(versionDir, entrypoint string) (string, error) {
	if entrypoint == "" {
		return "", NewError(CodeInvalidArgument, "entrypoint is required", nil)
	}
	clean, err := cleanArchiveRelativePath(entrypoint, "entrypoint")
	if err != nil {
		return "", err
	}
	executable := filepath.Join(append([]string{versionDir}, strings.Split(clean, "/")...)...)
	rel, err := filepath.Rel(versionDir, executable)
	if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", NewError(CodeInvalidArgument, "entrypoint escapes skill directory", map[string]any{"entrypoint": entrypoint})
	}
	info, err := os.Stat(executable)
	if err != nil {
		if os.IsNotExist(err) {
			return "", NewError(CodeInvalidArgument, "entrypoint is missing", map[string]any{"entrypoint": entrypoint, "path": executable})
		}
		return "", NewError(CodeInvalidArgument, "entrypoint cannot be inspected", map[string]any{"entrypoint": entrypoint, "path": executable, "error": err.Error()})
	}
	if info.IsDir() {
		return "", NewError(CodeInvalidArgument, "entrypoint is a directory", map[string]any{"entrypoint": entrypoint, "path": executable})
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", NewError(CodeInvalidArgument, "entrypoint is not executable", map[string]any{"entrypoint": entrypoint, "path": executable, "mode": info.Mode().String()})
	}
	return executable, nil
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int64
	written   int64
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.limit <= 0 {
		return b.buffer.Write(data)
	}
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.truncated = true
		b.written += int64(len(data))
		return len(data), nil
	}
	if int64(len(data)) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		b.written += int64(len(data))
		return len(data), nil
	}
	n, err := b.buffer.Write(data)
	b.written += int64(n)
	if err == io.ErrShortWrite {
		b.truncated = true
	}
	return len(data), err
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}
