package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorReportsMissingAuthAsInfo(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result := service.Doctor()
	check := findDoctorCheck(result.Checks, "auth_file")
	if check == nil {
		t.Fatalf("expected auth_file check: %#v", result.Checks)
	}
	if !check.OK || check.Severity != "info" {
		t.Fatalf("expected missing auth info check, got %#v", check)
	}
}

func TestDoctorReportsInvalidAuthAsError(t *testing.T) {
	root := t.TempDir()
	paths := PathsForRoot(root)
	if err := os.MkdirAll(filepath.Dir(paths.AuthFile), 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.WriteFile(paths.AuthFile, []byte(`{"schema_version":1,"extra":true}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	service := NewService(paths)
	result := service.Doctor()
	check := findDoctorCheck(result.Checks, "auth_file")
	if check == nil {
		t.Fatalf("expected auth_file check: %#v", result.Checks)
	}
	if check.OK || check.Severity != "error" {
		t.Fatalf("expected invalid auth error check, got %#v", check)
	}
	if result.OK {
		t.Fatalf("expected doctor failure for invalid auth")
	}
}

func TestDoctorWarnsForExpiredAuth(t *testing.T) {
	paths := PathsForRoot(t.TempDir())
	if err := SaveAuth(paths.AuthFile, AuthState{SchemaVersion: 1, AccessToken: "access", RefreshToken: "refresh", ExpiresAt: "2000-01-01T00:00:00Z", DeviceID: "device"}); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	service := NewService(paths)
	result := service.Doctor()
	check := findDoctorCheck(result.Checks, "auth_file")
	if check == nil {
		t.Fatalf("expected auth_file check: %#v", result.Checks)
	}
	if !check.OK || check.Severity != "warning" {
		t.Fatalf("expected expired auth warning check, got %#v", check)
	}
	if !result.OK {
		t.Fatalf("warnings should not fail doctor: %#v", result.Summary)
	}
}

func findDoctorCheck(checks []DoctorCheck, name string) *DoctorCheck {
	for index := range checks {
		if checks[index].Name == name {
			return &checks[index]
		}
	}
	return nil
}
