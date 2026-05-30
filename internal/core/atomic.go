package core

import (
	"os"
	"path/filepath"
	"runtime"
)

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
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

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := renameReplacing(tempName, path); err != nil {
		return err
	}
	cleanup = false
	if err := syncDirectory(dir); err != nil {
		return err
	}
	return nil
}

func renameReplacing(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		if removeErr := os.Remove(newPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		return os.Rename(oldPath, newPath)
	}
	return nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
