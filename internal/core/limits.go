package core

import (
	"io"
	"os"
)

func readFileLimited(path string, limit int64, label string) ([]byte, error) {
	if limit <= 0 {
		limit = defaultPackageMaxBytes
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, NewError(CodeSizeLimitExceeded, label+" exceeds configured size limit", map[string]any{"path": path, "size": info.Size(), "limit": limit})
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readAllLimited(file, limit, label)
}

func readAllLimited(reader io.Reader, limit int64, label string) ([]byte, error) {
	if limit <= 0 {
		limit = defaultPackageMaxBytes
	}
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, NewError(CodeSizeLimitExceeded, label+" exceeds configured size limit", map[string]any{"size": len(data), "limit": limit})
	}
	return data, nil
}
