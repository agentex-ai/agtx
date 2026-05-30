package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicCreatesParentAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := writeFileAtomic(path, []byte(`{"ok":true}`), 0o640); err != nil {
		t.Fatalf("write atomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read atomic file: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected data: %s", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat atomic file: %v", err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Fatalf("unexpected mode: %s", info.Mode())
		}
	}
}

func TestWriteFileAtomicReplacesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "current")
	if err := writeFileAtomic(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := writeFileAtomic(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("replace old: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced file: %v", err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected replaced data: %q", data)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".current.tmp-") {
			t.Fatalf("temporary file was not cleaned up: %s", entry.Name())
		}
	}
}
