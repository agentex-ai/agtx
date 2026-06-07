package core

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

func TestDoctorReportsCommerceLedgerIntegrity(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "advanced"); err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	result := service.Doctor()
	if !result.OK {
		t.Fatalf("expected doctor to pass after signed commerce install: %#v", result)
	}
	installCheck := findDoctorCheck(result.Checks, "commerce_install_ledger")
	billingCheck := findDoctorCheck(result.Checks, "commerce_billing_ledger")
	if installCheck == nil || billingCheck == nil {
		t.Fatalf("expected commerce ledger checks: %#v", result.Checks)
	}
	assertLedgerDoctorSummary(t, installCheck, integrityStatusVerified)
	assertLedgerDoctorSummary(t, billingCheck, integrityStatusVerified)
}

func TestDoctorReportsDeletedBillingLedgerIntegrityFailure(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "advanced"); err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	if err := os.Remove(service.billingRecordsPath()); err != nil {
		t.Fatalf("delete billing ledger: %v", err)
	}
	if err := os.Remove(service.ledgerHeadPath(billingRecordsFile)); err != nil {
		t.Fatalf("delete billing head: %v", err)
	}

	result := service.Doctor()
	check := findDoctorCheck(result.Checks, "commerce_billing_ledger")
	if check == nil {
		t.Fatalf("expected billing ledger check: %#v", result.Checks)
	}
	if check.OK || check.Severity != "error" || result.OK {
		t.Fatalf("expected doctor billing ledger error, got result=%#v check=%#v", result, check)
	}
	summary, ok := check.Details.(LedgerIntegritySummary)
	if !ok || summary.Status != integrityStatusFailed || summary.AnchorMatched {
		t.Fatalf("expected failed anchored summary, got %#v", check.Details)
	}
}

func TestDoctorWarnsForLegacyUnsignedCommerceLedger(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	record := InstallRecord{
		RecordID:   "legacy-install",
		Action:     "install_pack",
		PackID:     "pdf",
		Status:     "installed",
		OccurredAt: "2026-01-01T00:00:00Z",
	}
	if err := appendJSONLine(service.installRecordsPath(), record); err != nil {
		t.Fatalf("write legacy install record: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(service.Paths.ConfigDir, ledgerPrivateDirMode); err != nil {
			t.Fatalf("chmod config dir: %v", err)
		}
	}

	result := service.Doctor()
	check := findDoctorCheck(result.Checks, "commerce_install_ledger")
	if check == nil {
		t.Fatalf("expected install ledger check: %#v", result.Checks)
	}
	if !check.OK || check.Severity != "warning" || !result.OK {
		t.Fatalf("expected legacy unsigned warning without doctor failure, got result=%#v check=%#v", result, check)
	}
	assertLedgerDoctorSummary(t, check, integrityStatusLegacyUnsigned)
}

func TestDoctorReportsPermissiveCommerceLedgerFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission checks are skipped on Windows")
	}
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "advanced"); err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	if err := os.Chmod(service.billingRecordsPath(), 0o644); err != nil {
		t.Fatalf("chmod billing ledger: %v", err)
	}

	result := service.Doctor()
	check := findDoctorCheckByPath(result.Checks, "commerce_ledger_file", service.billingRecordsPath())
	if check == nil {
		t.Fatalf("expected billing ledger file permission check: %#v", result.Checks)
	}
	if check.OK || check.Severity != "error" || result.OK {
		t.Fatalf("expected permissive ledger file to fail doctor, got result=%#v check=%#v", result, check)
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

func findDoctorCheckByPath(checks []DoctorCheck, name, path string) *DoctorCheck {
	for index := range checks {
		if checks[index].Name == name && checks[index].Path == path {
			return &checks[index]
		}
	}
	return nil
}

func assertLedgerDoctorSummary(t *testing.T, check *DoctorCheck, status string) {
	t.Helper()
	summary, ok := check.Details.(LedgerIntegritySummary)
	if !ok {
		t.Fatalf("expected ledger summary details, got %#v", check.Details)
	}
	if summary.Status != status {
		t.Fatalf("expected ledger status %s, got %#v", status, summary)
	}
}
