package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultRegistrySkillsDeclareBillingMeters(t *testing.T) {
	registry := DefaultRegistry()
	expected := map[string][]string{
		"web_search": {"call"},
		"web_fetch":  {"page", "call"},
		"research":   {"task"},
		"ocr":        {"page"},
		"audio":      {"minute"},
		"imagen":     {"task", "credit"},
		"docx":       {"task"},
		"xlsx":       {"task"},
		"pptx":       {"task"},
		"pdf":        {"page"},
	}

	for name, meters := range expected {
		t.Run(name, func(t *testing.T) {
			skill, ok := registry.Find(name)
			if !ok {
				t.Fatalf("missing default skill %s", name)
			}
			if skill.VendorID != "agentex" {
				t.Fatalf("expected agentex vendor, got %q", skill.VendorID)
			}
			expectedClass := "tool"
			if name == "research" {
				expectedClass = "workflow"
			}
			if skill.Capability == nil || skill.Capability.Class != expectedClass {
				t.Fatalf("expected %s capability, got %#v", expectedClass, skill.Capability)
			}
			if skill.Billing == nil {
				t.Fatal("expected billing metadata")
			}
			for _, meter := range meters {
				if !hasBillingMeter(skill.Billing.Meters, meter) {
					t.Fatalf("expected meter %s in %#v", meter, skill.Billing.Meters)
				}
			}
			if skill.Billing.RevenueShare == nil || skill.Billing.RevenueShare.ISV != 70 || skill.Billing.RevenueShare.Platform != 30 {
				t.Fatalf("unexpected revenue share: %#v", skill.Billing.RevenueShare)
			}
		})
	}
}

func TestPlanInstallIncludesCommerceSummary(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	plan, err := service.PlanInstall([]string{"web_fetch"})
	if err != nil {
		t.Fatalf("plan install: %v", err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("expected one planned change, got %#v", plan)
	}
	commerce := plan.Changes[0].Commerce
	if commerce == nil {
		t.Fatal("expected commerce summary")
	}
	if commerce.VendorID != "agentex" || commerce.CapabilityClass != "tool" {
		t.Fatalf("unexpected commerce identity: %#v", commerce)
	}
	if len(commerce.BillingMeters) != 2 || commerce.BillingMeters[0] != "call" || commerce.BillingMeters[1] != "page" {
		t.Fatalf("unexpected billing meters: %#v", commerce.BillingMeters)
	}
}

func TestDefaultCapabilityPacksExposeWebsiteFirstWaveAndBundles(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	packs, err := service.ListCapabilityPacks()
	if err != nil {
		t.Fatalf("list capability packs: %v", err)
	}
	expected := []string{"web_search", "web_fetch", "research", "ocr", "audio", "imagen", "docx", "xlsx", "pptx", "pdf", "documents", "standard", "advanced"}
	if len(packs) != len(expected) {
		t.Fatalf("expected website first-wave packs and bundles, got %#v", packs)
	}
	for index, id := range expected {
		if packs[index].Pack.ID != id {
			t.Fatalf("expected pack %s at index %d, got %#v", id, index, packs[index].Pack)
		}
		if packs[index].Pack.UseWhen == "" || len(packs[index].Pack.Inputs) == 0 || len(packs[index].Pack.Outputs) == 0 {
			t.Fatalf("expected website contract metadata for %s: %#v", id, packs[index].Pack)
		}
	}
	if packs[0].Installed || packs[len(packs)-1].Installed {
		t.Fatalf("fresh service should not have packs installed: %#v", packs)
	}
	pdf, err := service.GetCapabilityPack("pdf")
	if err != nil {
		t.Fatalf("get pdf pack: %v", err)
	}
	if pdf.Pack.CapabilityClass != "tool" || len(pdf.Pack.SkillNames) != 1 || pdf.Pack.SkillNames[0] != "pdf" || !hasBillingMeter(pdf.Pack.Billing.Meters, "page") {
		t.Fatalf("expected single pdf capability pack metadata: %#v", pdf.Pack)
	}
	media, err := service.GetCapabilityPack("mediagen")
	if err != nil {
		t.Fatalf("get mediagen alias: %v", err)
	}
	if media.Pack.ID != "imagen" {
		t.Fatalf("expected mediagen alias to resolve to imagen, got %#v", media.Pack)
	}
	standard, err := service.GetCapabilityPack("standard")
	if err != nil {
		t.Fatalf("get standard pack: %v", err)
	}
	if standard.Pack.Billing == nil || !hasBillingMeter(standard.Pack.Billing.Meters, "seat") || !hasBillingMeter(standard.Pack.Billing.Meters, "credit") {
		t.Fatalf("expected standard pack billing metadata: %#v", standard.Pack.Billing)
	}
}

func TestCapabilityScenariosMapRealTasksToPacksAndPlans(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	scenarios, err := service.ListCapabilityScenarios()
	if err != nil {
		t.Fatalf("list capability scenarios: %v", err)
	}
	if len(scenarios) < 6 {
		t.Fatalf("expected built-in real task scenarios, got %#v", scenarios)
	}
	invoice, err := service.GetCapabilityScenario("invoice")
	if err != nil {
		t.Fatalf("get invoice scenario: %v", err)
	}
	if invoice.Scenario.ID != "invoice_processing" || invoice.RecommendedPack.Pack.ID != "standard" {
		t.Fatalf("unexpected invoice scenario mapping: %#v", invoice)
	}
	if invoice.Ready || len(invoice.MissingSkills) == 0 {
		t.Fatalf("fresh invoice scenario should need install: %#v", invoice)
	}
	if invoice.InstallPlan.Action != "install_pack" || len(invoice.InstallPlan.Changes) == 0 || len(invoice.BillingPreviewTotals) != 2 {
		t.Fatalf("expected install plan and billing preview: %#v", invoice)
	}
	if len(invoice.Scenario.Inputs) < 2 || len(invoice.Scenario.Deliverables) < 2 || len(invoice.Scenario.Workflow) < 3 || len(invoice.Scenario.AcceptanceCriteria) < 2 {
		t.Fatalf("expected website-ready scenario workflow metadata: %#v", invoice.Scenario)
	}
	if invoice.Scenario.Inputs[0].ID != "vendor_invoices" || !invoice.Scenario.Inputs[0].Required || invoice.Scenario.Deliverables[0].ID != "invoice_extract" {
		t.Fatalf("unexpected invoice scenario IO metadata: %#v", invoice.Scenario)
	}
	if invoice.Scenario.Workflow[0].ID != "intake_documents" || len(invoice.Scenario.Workflow[0].Skills) == 0 {
		t.Fatalf("unexpected invoice workflow metadata: %#v", invoice.Scenario.Workflow)
	}
	for _, record := range invoice.InstallPlan.BillingPreview {
		if record.ScenarioID != "invoice_processing" {
			t.Fatalf("scenario billing preview should carry scenario_id: %#v", invoice.InstallPlan.BillingPreview)
		}
	}
	if !scenarioHasRequiredSkill(invoice, "pdf") || !scenarioHasRequiredSkill(invoice, "ocr") || !scenarioHasRequiredSkill(invoice, "xlsx") {
		t.Fatalf("invoice scenario should require document extraction skills: %#v", invoice.RequiredSkills)
	}
	meeting, err := service.GetCapabilityScenario("meeting_deck")
	if err != nil {
		t.Fatalf("get meeting scenario alias: %v", err)
	}
	if meeting.Scenario.ID != "meeting_to_presentation" || meeting.RecommendedPack.Pack.ID != "advanced" || !scenarioHasRequiredSkill(meeting, "audio") || !scenarioHasRequiredSkill(meeting, "pptx") {
		t.Fatalf("unexpected meeting scenario mapping: %#v", meeting)
	}
}

func TestCapabilityScenarioReadinessFollowsInstalledPack(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	invoice, err := service.GetCapabilityScenario("invoice_processing")
	if err != nil {
		t.Fatalf("get invoice scenario: %v", err)
	}
	if !invoice.Ready || !invoice.RecommendedPack.Installed || len(invoice.MissingSkills) != 0 {
		t.Fatalf("expected invoice scenario ready after standard install: %#v", invoice)
	}
	if len(invoice.BillingPreviewTotals) != 0 || len(invoice.InstallPlan.BillingPreview) != 0 {
		t.Fatalf("installed scenario should not preview new pack billing: %#v", invoice)
	}
}

func TestPlanCapabilityPackInstallShowsChangesAndBillingPreview(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	plan, err := service.PlanCapabilityPackInstall("standard")
	if err != nil {
		t.Fatalf("plan standard pack install: %v", err)
	}
	if plan.Action != "install_pack" || plan.Pack.Pack.ID != "standard" {
		t.Fatalf("unexpected pack plan identity: %#v", plan)
	}
	if len(plan.Changes) != len(plan.Pack.Pack.SkillNames) {
		t.Fatalf("expected one change per skill: %#v", plan.Changes)
	}
	if len(plan.BillingPreview) != 2 || len(plan.Totals) != 2 {
		t.Fatalf("expected seat and credit billing preview: %#v", plan)
	}
	if len(plan.Requires) != 1 || plan.Requires[0] != "confirmation" {
		t.Fatalf("expected confirmation requirement: %#v", plan.Requires)
	}
	records, err := service.ListInstallRecords(RecordQueryOptions{PackID: "standard"})
	if err != nil {
		t.Fatalf("list install records after plan: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("plan should not write install records: %#v", records)
	}
}

func TestPlanCapabilityPackInstallOmitsBillingPreviewWhenAlreadyInstalled(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	plan, err := service.PlanCapabilityPackInstall("standard")
	if err != nil {
		t.Fatalf("plan installed standard pack: %v", err)
	}
	if !plan.Pack.Installed || len(plan.BillingPreview) != 0 {
		t.Fatalf("installed pack should not have billing preview: %#v", plan)
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "already installed") {
		t.Fatalf("expected already installed warning: %#v", plan.Warnings)
	}
}

func TestInstallCapabilityScenarioRecordsScenarioInstallAndBilling(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	plan, err := service.PlanCapabilityScenarioInstall("invoice")
	if err != nil {
		t.Fatalf("plan scenario install: %v", err)
	}
	if plan.Action != "install_scenario" || plan.Scenario.Scenario.ID != "invoice_processing" || plan.PackPlan.Pack.Pack.ID != "standard" {
		t.Fatalf("unexpected scenario install plan: %#v", plan)
	}
	if len(plan.PackPlan.BillingPreview) != 2 || plan.PackPlan.BillingPreview[0].ScenarioID != "invoice_processing" {
		t.Fatalf("expected scenario-tagged billing preview: %#v", plan.PackPlan.BillingPreview)
	}

	result, err := service.InstallCapabilityScenario(context.Background(), "invoice")
	if err != nil {
		t.Fatalf("install scenario: %v", err)
	}
	if result.Scenario.Scenario.ID != "invoice_processing" || result.PackInstall.Pack.Pack.ID != "standard" || !result.Scenario.Ready {
		t.Fatalf("unexpected scenario install result: %#v", result)
	}
	record := result.PackInstall.InstallRecord
	if record == nil || record.Action != "install_scenario" || record.PackID != "standard" || record.ScenarioID != "invoice_processing" {
		t.Fatalf("expected scenario install record: %#v", record)
	}
	if len(result.PackInstall.BillingRecords) != 2 {
		t.Fatalf("expected scenario install billing records: %#v", result.PackInstall.BillingRecords)
	}
	for _, billing := range result.PackInstall.BillingRecords {
		if billing.ScenarioID != "invoice_processing" || billing.PackID != "standard" || billing.Type != "pack_install" {
			t.Fatalf("expected scenario-tagged billing record: %#v", billing)
		}
	}

	installs, err := service.ListInstallRecords(RecordQueryOptions{ScenarioID: "invoice_processing"})
	if err != nil {
		t.Fatalf("list scenario install records: %v", err)
	}
	if len(installs) != 1 || installs[0].RecordID != record.RecordID || installs[0].Action != "install_scenario" {
		t.Fatalf("unexpected scenario install records: %#v", installs)
	}
	aliasInstalls, err := service.ListInstallRecords(RecordQueryOptions{ScenarioID: "invoice"})
	if err != nil {
		t.Fatalf("list scenario alias install records: %v", err)
	}
	if len(aliasInstalls) != 1 || aliasInstalls[0].ScenarioID != "invoice_processing" {
		t.Fatalf("expected alias scenario install records: %#v", aliasInstalls)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{ScenarioID: "invoice_processing"})
	if err != nil {
		t.Fatalf("list scenario billing records: %v", err)
	}
	if len(billing.Records) != 2 || len(billing.Totals) != 2 || billing.Records[0].ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario billing records: %#v", billing)
	}
	aliasBilling, err := service.ListBillingRecords(RecordQueryOptions{ScenarioID: "invoice"})
	if err != nil {
		t.Fatalf("list scenario alias billing records: %v", err)
	}
	if len(aliasBilling.Records) != 2 || aliasBilling.Records[0].ScenarioID != "invoice_processing" {
		t.Fatalf("expected alias scenario billing records: %#v", aliasBilling)
	}
	emptyBilling, err := service.ListBillingRecords(RecordQueryOptions{ScenarioID: "meeting_to_presentation"})
	if err != nil {
		t.Fatalf("list unrelated scenario billing records: %v", err)
	}
	if len(emptyBilling.Records) != 0 || len(emptyBilling.Totals) != 0 {
		t.Fatalf("unrelated scenario filter should be empty: %#v", emptyBilling)
	}
	snapshot, err := service.CommerceSnapshot(RecordQueryOptions{ScenarioID: "invoice_processing"})
	if err != nil {
		t.Fatalf("scenario snapshot: %v", err)
	}
	if len(snapshot.InstallRecords.Records) != 1 || len(snapshot.Billing.Records) != 2 {
		t.Fatalf("expected scenario snapshot ledgers: %#v", snapshot)
	}
	if len(snapshot.Scenarios) != 1 || snapshot.Scenarios[0].Scenario.ID != "invoice_processing" {
		t.Fatalf("expected scenario snapshot view filter: %#v", snapshot.Scenarios)
	}
	ledger, err := service.CapabilityScenarioLedger("invoice", RecordQueryOptions{})
	if err != nil {
		t.Fatalf("scenario ledger: %v", err)
	}
	if ledger.Scenario.Scenario.ID != "invoice_processing" || ledger.LatestInstall == nil || ledger.LatestInstall.RecordID != record.RecordID {
		t.Fatalf("unexpected scenario ledger identity/latest install: %#v", ledger)
	}
	if len(ledger.InstallRecords) != 1 || len(ledger.Billing.Records) != 2 || len(ledger.Billing.Totals) != 2 || len(ledger.PackInstallRecords) != 2 || len(ledger.UsageRecords) != 0 {
		t.Fatalf("unexpected scenario ledger records: %#v", ledger)
	}
	usageOnly, err := service.CapabilityScenarioLedger("invoice", RecordQueryOptions{Type: "skill_usage"})
	if err != nil {
		t.Fatalf("scenario usage-only ledger: %v", err)
	}
	if len(usageOnly.Billing.Records) != 0 || len(usageOnly.PackInstallRecords) != 0 || len(usageOnly.InstallRecords) != 1 {
		t.Fatalf("expected usage-only ledger to filter billing records but keep scenario installs: %#v", usageOnly)
	}
}

func TestInstallCapabilityPackRecordsInstallAndBilling(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.InstallCapabilityPack(context.Background(), "advanced")
	if err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	if !result.Pack.Installed || result.Pack.Pack.ID != "advanced" {
		t.Fatalf("expected installed advanced pack: %#v", result.Pack)
	}
	if len(result.Results) != len(result.Pack.Pack.SkillNames) {
		t.Fatalf("expected install result per skill: results=%d skills=%d", len(result.Results), len(result.Pack.Pack.SkillNames))
	}
	if result.InstallRecord == nil || result.InstallRecord.PackID != "advanced" || result.InstallRecord.Action != "install_pack" {
		t.Fatalf("expected pack install record: %#v", result.InstallRecord)
	}
	if len(result.BillingRecords) != 2 {
		t.Fatalf("expected seat and credit billing records: %#v", result.BillingRecords)
	}
	records, err := service.ListInstallRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list install records: %v", err)
	}
	if len(records) != 1 || records[0].PackID != "advanced" || len(records[0].Skills) != len(result.Results) {
		t.Fatalf("unexpected install ledger: %#v", records)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list billing records: %v", err)
	}
	if len(billing.Records) != 2 || len(billing.Totals) != 2 {
		t.Fatalf("unexpected billing ledger: %#v", billing)
	}
	if billing.Integrity == nil || billing.Integrity.Status != integrityStatusVerified {
		t.Fatalf("expected verified billing ledger: %#v", billing.Integrity)
	}
	for _, record := range billing.Records {
		if record.Integrity == nil || record.Integrity.Status != integrityStatusVerified {
			t.Fatalf("expected verified billing record integrity: %#v", record)
		}
	}
	installLedger, err := service.ListInstallRecordsWithIntegrity(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list install records with integrity: %v", err)
	}
	if installLedger.Integrity == nil || installLedger.Integrity.Status != integrityStatusVerified || installLedger.Records[0].Integrity == nil {
		t.Fatalf("expected verified install ledger: %#v", installLedger)
	}
	filteredInstalls, err := service.ListInstallRecords(RecordQueryOptions{PackID: "advanced", Status: "installed", From: result.InstallRecord.OccurredAt, To: result.InstallRecord.OccurredAt})
	if err != nil {
		t.Fatalf("filter install records: %v", err)
	}
	if len(filteredInstalls) != 1 || filteredInstalls[0].RecordID != result.InstallRecord.RecordID {
		t.Fatalf("unexpected filtered install records: %#v", filteredInstalls)
	}
	filteredBilling, err := service.ListBillingRecords(RecordQueryOptions{PackID: "advanced", Status: usageStatusLocalOnly, Type: "pack_install", Currency: "USD", From: billing.Records[0].OccurredAt, To: billing.Records[0].OccurredAt})
	if err != nil {
		t.Fatalf("filter billing records: %v", err)
	}
	if len(filteredBilling.Records) != 1 || filteredBilling.Records[0].Currency != "USD" || len(filteredBilling.Totals) != 1 {
		t.Fatalf("unexpected filtered billing records: %#v", filteredBilling)
	}
	if _, err := service.ListBillingRecords(RecordQueryOptions{From: "not-a-time"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid time filter error, got %v", err)
	}
}

func TestLedgerIntegrityDetectsTamperedBillingRecords(t *testing.T) {
	root := t.TempDir()
	service := NewService(PathsForRoot(root))
	if _, err := service.InstallCapabilityPack(context.Background(), "advanced"); err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list billing records: %v", err)
	}
	if billing.Integrity == nil || billing.Integrity.Status != integrityStatusVerified {
		t.Fatalf("expected verified billing ledger before tamper: %#v", billing.Integrity)
	}

	path := filepath.Join(service.Paths.ConfigDir, billingRecordsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read billing ledger: %v", err)
	}
	tampered := bytes.Replace(data, []byte(`"gross_amount_minor":2990`), []byte(`"gross_amount_minor":1`), 1)
	if bytes.Equal(data, tampered) {
		t.Fatalf("test did not modify billing ledger:\n%s", string(data))
	}
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatalf("write tampered billing ledger: %v", err)
	}

	billing, err = service.ListBillingRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list tampered billing records: %v", err)
	}
	if billing.Integrity == nil || billing.Integrity.Status != integrityStatusFailed || billing.Integrity.Failed == 0 {
		t.Fatalf("expected failed integrity after tamper: %#v", billing.Integrity)
	}
	if billing.Records[0].Integrity == nil || billing.Records[0].Integrity.Status != integrityStatusFailed {
		t.Fatalf("expected tampered record integrity failure: %#v", billing.Records[0])
	}

	if _, err := service.InstallCapabilityPack(context.Background(), "pdf"); !IsErrorCode(err, CodeIntegrityFailed) {
		t.Fatalf("expected append to fail after tampered billing ledger, got %v", err)
	}
}

func TestLedgerIntegrityDetectsDeletedBillingLedgerWithAnchors(t *testing.T) {
	root := t.TempDir()
	service := NewService(PathsForRoot(root))
	if _, err := service.InstallCapabilityPack(context.Background(), "advanced"); err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list billing records: %v", err)
	}
	if billing.Integrity == nil || billing.Integrity.Status != integrityStatusVerified || billing.Integrity.Anchors < 2 || !billing.Integrity.AnchorMatched {
		t.Fatalf("expected anchored verified billing ledger before reset: %#v", billing.Integrity)
	}
	for _, path := range service.ledgerAnchorPaths(billingRecordsFile) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected billing anchor at %s: %v", path, err)
		}
	}

	if err := os.Remove(service.billingRecordsPath()); err != nil {
		t.Fatalf("delete billing ledger: %v", err)
	}
	if err := os.Remove(service.ledgerHeadPath(billingRecordsFile)); err != nil {
		t.Fatalf("delete billing head: %v", err)
	}

	billing, err = service.ListBillingRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list reset billing records: %v", err)
	}
	if len(billing.Records) != 0 {
		t.Fatalf("expected deleted billing records to be empty: %#v", billing.Records)
	}
	if billing.Integrity == nil || billing.Integrity.Status != integrityStatusFailed || billing.Integrity.Anchors < 2 || billing.Integrity.AnchorMatched || !strings.Contains(billing.Integrity.Reason, "anchor mismatch") {
		t.Fatalf("expected anchor mismatch after deleted billing ledger: %#v", billing.Integrity)
	}
	if _, err := service.InstallCapabilityPack(context.Background(), "pdf"); !IsErrorCode(err, CodeIntegrityFailed) {
		t.Fatalf("expected append to fail after deleted anchored billing ledger, got %v", err)
	}
}

func TestCommerceProofSignsChallengeBoundLedgerIntegrity(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	challenge := "site-nonce-123"
	proof, err := service.CommerceProof(challenge)
	if err != nil {
		t.Fatalf("commerce proof: %v", err)
	}
	if proof.Challenge != challenge || proof.Payload.Challenge != challenge || proof.PayloadHash == "" || proof.Signature == "" || proof.PublicKey == "" {
		t.Fatalf("unexpected commerce proof envelope: %#v", proof)
	}
	if proof.Payload.TrustLevel != "local_signed" || proof.Payload.ReceiptStatus != "local_only" || len(proof.Payload.Ledgers) != 3 {
		t.Fatalf("unexpected commerce proof payload: %#v", proof.Payload)
	}
	verification := VerifyCommerceProof(proof, challenge)
	if !verification.OK || !verification.SignatureMatched || !verification.PayloadHashMatched || !verification.ChallengeMatched || !verification.EnvelopeMatched {
		t.Fatalf("expected valid commerce proof verification: %#v", verification)
	}
	wrongChallenge := VerifyCommerceProof(proof, "other-nonce")
	if wrongChallenge.OK || wrongChallenge.ChallengeMatched {
		t.Fatalf("expected challenge mismatch to fail verification: %#v", wrongChallenge)
	}
	tampered := proof
	tampered.Payload.Ledgers[0].Status = integrityStatusFailed
	tampered.Payload.OK = false
	tamperedVerification := VerifyCommerceProof(tampered, challenge)
	if tamperedVerification.OK || tamperedVerification.SignatureMatched || tamperedVerification.PayloadHashMatched {
		t.Fatalf("expected tampered proof payload to fail verification: %#v", tamperedVerification)
	}
	if _, err := service.CommerceProof(""); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected empty proof challenge to be invalid, got %v", err)
	}
}

func TestSubmitCommerceProofStoresServerReceipt(t *testing.T) {
	receiptPublicKey, receiptPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate receipt key: %v", err)
	}
	var gotPath string
	var gotAuth string
	var gotDevice string
	var gotRequest commerceProofSubmitRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		if gotPath != "/v1/commerce/proofs" {
			http.Error(writer, "unexpected path", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&gotRequest); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if !VerifyCommerceProof(gotRequest.Proof, "server-nonce").OK || !gotRequest.Verification.OK {
			http.Error(writer, "invalid proof", http.StatusBadRequest)
			return
		}
		receipt := signedTestCommerceReceipt(t, gotRequest.Proof, receiptPublicKey, receiptPrivateKey, 1)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(commerceProofSubmitResponse{OK: true, Receipt: receipt})
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = server.URL
	service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device-1"}
	if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}

	result, err := service.SubmitCommerceProof(context.Background(), "server-nonce")
	if err != nil {
		t.Fatalf("submit commerce proof: %v", err)
	}
	if gotPath != "/v1/commerce/proofs" || gotAuth != "Bearer access" || gotDevice != "device-1" {
		t.Fatalf("unexpected proof submit request: path=%q auth=%q device=%q", gotPath, gotAuth, gotDevice)
	}
	if !result.Verification.OK || result.Receipt.ReceiptID == "" || result.Receipt.Integrity == nil || result.Receipt.Integrity.Status != integrityStatusVerified {
		t.Fatalf("unexpected receipt submit result: %#v", result)
	}
	if result.Proof.Payload.DeviceID != "device-1" || result.Receipt.DeviceID != "device-1" {
		t.Fatalf("expected proof and receipt to carry device id: %#v", result)
	}
	if !VerifyCommerceReceipt(result.Proof, result.Receipt).OK {
		t.Fatalf("stored receipt should verify against submitted proof: %#v", result)
	}
	receipts, err := service.ListCommerceReceipts(RecordQueryOptions{Status: commerceReceiptStatusReceived})
	if err != nil {
		t.Fatalf("list commerce receipts: %v", err)
	}
	if len(receipts.Records) != 1 || receipts.Records[0].ReceiptID != result.Receipt.ReceiptID || receipts.Integrity == nil || receipts.Integrity.Status != integrityStatusVerified || receipts.Integrity.Anchors < 2 || !receipts.Integrity.AnchorMatched {
		t.Fatalf("expected verified anchored receipt ledger: %#v", receipts)
	}

	data, err := os.ReadFile(service.commerceReceiptsPath())
	if err != nil {
		t.Fatalf("read receipt ledger: %v", err)
	}
	tampered := bytes.Replace(data, []byte(commerceReceiptStatusReceived), []byte("server_changed"), 1)
	if err := os.WriteFile(service.commerceReceiptsPath(), tampered, 0o600); err != nil {
		t.Fatalf("tamper receipt ledger: %v", err)
	}
	integrity, err := service.CommerceReceiptIntegrity()
	if err != nil {
		t.Fatalf("receipt integrity after tamper: %v", err)
	}
	if integrity.Status != integrityStatusFailed || integrity.Failed == 0 {
		t.Fatalf("expected tampered receipt ledger to fail integrity: %#v", integrity)
	}
	if _, err := service.SubmitCommerceProof(context.Background(), "server-nonce-2"); !IsErrorCode(err, CodeIntegrityFailed) {
		t.Fatalf("expected tampered receipt ledger to block future receipt append, got %v", err)
	}
}

func signedTestCommerceReceipt(t *testing.T, proof CommerceProof, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, sequence int64) CommerceReceipt {
	t.Helper()
	receipt := CommerceReceipt{
		SchemaVersion:    1,
		ReceiptID:        commerceReceiptIDForProof(proof),
		Status:           commerceReceiptStatusReceived,
		ReceivedAt:       time.Now().UTC().Format(time.RFC3339),
		Issuer:           "agtx-test-pro",
		ServerLedgerID:   "test-commerce-receipts",
		ServerSequence:   sequence,
		Algorithm:        commerceReceiptAlgorithm,
		KeyID:            "test-receipt-key",
		PublicKey:        base64.StdEncoding.EncodeToString(publicKey),
		ProofPayloadHash: proof.PayloadHash,
		ProofSignature:   proof.Signature,
		ProofKeyID:       proof.KeyID,
		Challenge:        proof.Challenge,
		DeviceID:         proof.Payload.DeviceID,
	}
	payload, err := commerceReceiptPayloadBytes(receipt)
	if err != nil {
		t.Fatalf("canonical receipt payload: %v", err)
	}
	receipt.ServerSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return receipt
}

func TestInstallWebsiteCapabilityPackRecordsInstallAndBilling(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	plan, err := service.PlanCapabilityPackInstall("pdf")
	if err != nil {
		t.Fatalf("plan pdf pack install: %v", err)
	}
	if plan.Pack.Pack.ID != "pdf" || len(plan.Changes) != 1 || len(plan.BillingPreview) != 1 || plan.BillingPreview[0].PackID != "pdf" || plan.BillingPreview[0].Meter != "page" {
		t.Fatalf("unexpected pdf plan: %#v", plan)
	}
	result, err := service.InstallCapabilityPack(context.Background(), "pdf")
	if err != nil {
		t.Fatalf("install pdf pack: %v", err)
	}
	if result.Pack.Pack.ID != "pdf" || !result.Pack.Installed || len(result.Results) != 1 {
		t.Fatalf("unexpected pdf install result: %#v", result)
	}
	if result.InstallRecord == nil || result.InstallRecord.PackID != "pdf" || result.InstallRecord.Action != "install_pack" {
		t.Fatalf("expected pdf install record: %#v", result.InstallRecord)
	}
	if len(result.BillingRecords) != 1 || result.BillingRecords[0].PackID != "pdf" || result.BillingRecords[0].Meter != "page" {
		t.Fatalf("expected pdf billing record: %#v", result.BillingRecords)
	}
	installs, err := service.ListInstallRecords(RecordQueryOptions{PackID: "pdf"})
	if err != nil {
		t.Fatalf("list pdf installs: %v", err)
	}
	if len(installs) != 1 || installs[0].PackID != "pdf" || !installRecordMatchesSkill(installs[0], "pdf") {
		t.Fatalf("unexpected pdf install records: %#v", installs)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{PackID: "pdf"})
	if err != nil {
		t.Fatalf("list pdf billing: %v", err)
	}
	if len(billing.Records) != 1 || len(billing.Totals) != 1 || billing.Records[0].PackID != "pdf" {
		t.Fatalf("unexpected pdf billing records: %#v", billing)
	}
	snapshot, err := service.CommerceSnapshot(RecordQueryOptions{PackID: "pdf"})
	if err != nil {
		t.Fatalf("pdf snapshot: %v", err)
	}
	if len(snapshot.Packs) != 1 || snapshot.Packs[0].Pack.ID != "pdf" || len(snapshot.InstallRecords.Records) != 1 || len(snapshot.Billing.Records) != 1 {
		t.Fatalf("unexpected pdf snapshot: %#v", snapshot)
	}
}

func TestCommerceHTTPHandlerExposesWebsiteQueries(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{AllowedOrigin: "https://site.example"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/commerce/snapshot?pack_id=standard&limit=5", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Origin", "https://site.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected snapshot status: %s", response.Status)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "https://site.example" {
		t.Fatalf("expected CORS header, got %q", response.Header.Get("Access-Control-Allow-Origin"))
	}
	var snapshotResponse struct {
		OK   bool                       `json:"ok"`
		Data CapabilityCommerceSnapshot `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshotResponse); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if !snapshotResponse.OK || len(snapshotResponse.Data.Packs) != 1 || snapshotResponse.Data.Packs[0].Pack.ID != "standard" {
		t.Fatalf("unexpected snapshot response: %#v", snapshotResponse)
	}
	if len(snapshotResponse.Data.InstallRecords.Records) != 1 || len(snapshotResponse.Data.Billing.Records) != 2 {
		t.Fatalf("expected install and billing records in snapshot: %#v", snapshotResponse.Data)
	}
	if len(snapshotResponse.Data.Scenarios) == 0 {
		t.Fatalf("expected website snapshot scenarios: %#v", snapshotResponse.Data)
	}
	for _, scenario := range snapshotResponse.Data.Scenarios {
		if scenario.RecommendedPack.Pack.ID != "standard" {
			t.Fatalf("pack_id filter should keep only standard scenarios: %#v", scenario)
		}
	}

	dashboardRequest, err := http.NewRequest(http.MethodGet, server.URL+"/commerce", nil)
	if err != nil {
		t.Fatalf("new dashboard request: %v", err)
	}
	dashboardRequest.Header.Set("Origin", "https://site.example")
	dashboardResponse, err := http.DefaultClient.Do(dashboardRequest)
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	defer dashboardResponse.Body.Close()
	if dashboardResponse.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("dashboard must not be CORS-readable, got %q", dashboardResponse.Header.Get("Access-Control-Allow-Origin"))
	}

	response, err = http.Get(server.URL + "/v1/commerce/install-records?skill=pdf")
	if err != nil {
		t.Fatalf("get install records: %v", err)
	}
	defer response.Body.Close()
	var installsResponse struct {
		OK   bool                    `json:"ok"`
		Data InstallRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&installsResponse); err != nil {
		t.Fatalf("decode install records: %v", err)
	}
	if !installsResponse.OK || len(installsResponse.Data.Records) != 1 || !installRecordMatchesSkill(installsResponse.Data.Records[0], "pdf") || installsResponse.Data.Integrity == nil {
		t.Fatalf("unexpected install records response: %#v", installsResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/billing-records?pack_id=standard")
	if err != nil {
		t.Fatalf("get billing records: %v", err)
	}
	defer response.Body.Close()
	var billingResponse struct {
		OK   bool                    `json:"ok"`
		Data BillingRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&billingResponse); err != nil {
		t.Fatalf("decode billing records: %v", err)
	}
	if !billingResponse.OK || len(billingResponse.Data.Records) != 2 || len(billingResponse.Data.Totals) != 2 {
		t.Fatalf("unexpected billing records response: %#v", billingResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/integrity")
	if err != nil {
		t.Fatalf("get commerce integrity: %v", err)
	}
	defer response.Body.Close()
	var integrityResponse struct {
		OK   bool                    `json:"ok"`
		Data CommerceIntegrityResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&integrityResponse); err != nil {
		t.Fatalf("decode commerce integrity: %v", err)
	}
	if !integrityResponse.OK || !integrityResponse.Data.OK || len(integrityResponse.Data.Ledgers) != 3 || len(integrityResponse.Data.Checks) == 0 {
		t.Fatalf("unexpected commerce integrity response: %#v", integrityResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/proof?challenge=site-nonce")
	if err != nil {
		t.Fatalf("get commerce proof: %v", err)
	}
	defer response.Body.Close()
	var proofResponse struct {
		OK   bool          `json:"ok"`
		Data CommerceProof `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&proofResponse); err != nil {
		t.Fatalf("decode commerce proof: %v", err)
	}
	if !proofResponse.OK || !VerifyCommerceProof(proofResponse.Data, "site-nonce").OK {
		t.Fatalf("unexpected commerce proof response: %#v", proofResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/billing-records?pack_id=standard&type=pack_install&currency=USD&status=local_only")
	if err != nil {
		t.Fatalf("get filtered billing records: %v", err)
	}
	defer response.Body.Close()
	var filteredBillingResponse struct {
		OK   bool                    `json:"ok"`
		Data BillingRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&filteredBillingResponse); err != nil {
		t.Fatalf("decode filtered billing records: %v", err)
	}
	if !filteredBillingResponse.OK || len(filteredBillingResponse.Data.Records) != 1 || filteredBillingResponse.Data.Records[0].Currency != "USD" {
		t.Fatalf("unexpected filtered billing records response: %#v", filteredBillingResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/packs?pack_id=pdf")
	if err != nil {
		t.Fatalf("get pdf pack: %v", err)
	}
	defer response.Body.Close()
	var packsResponse struct {
		OK   bool                 `json:"ok"`
		Data []CapabilityPackView `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&packsResponse); err != nil {
		t.Fatalf("decode packs response: %v", err)
	}
	if !packsResponse.OK || len(packsResponse.Data) != 1 || packsResponse.Data[0].Pack.ID != "pdf" || packsResponse.Data[0].Pack.UseWhen == "" {
		t.Fatalf("unexpected pdf pack response: %#v", packsResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/packs?pack_id=mediagen")
	if err != nil {
		t.Fatalf("get mediagen pack alias: %v", err)
	}
	defer response.Body.Close()
	var mediaPacksResponse struct {
		OK   bool                 `json:"ok"`
		Data []CapabilityPackView `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&mediaPacksResponse); err != nil {
		t.Fatalf("decode media packs response: %v", err)
	}
	if !mediaPacksResponse.OK || len(mediaPacksResponse.Data) != 1 || mediaPacksResponse.Data[0].Pack.ID != "imagen" {
		t.Fatalf("unexpected mediagen pack response: %#v", mediaPacksResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/scenarios?pack_id=pdf")
	if err != nil {
		t.Fatalf("get pdf scenarios: %v", err)
	}
	defer response.Body.Close()
	var pdfScenariosResponse struct {
		OK   bool                     `json:"ok"`
		Data []CapabilityScenarioView `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&pdfScenariosResponse); err != nil {
		t.Fatalf("decode pdf scenarios: %v", err)
	}
	if !pdfScenariosResponse.OK || len(pdfScenariosResponse.Data) == 0 {
		t.Fatalf("expected pdf-related scenarios: %#v", pdfScenariosResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/scenarios?scenario_id=meeting_deck")
	if err != nil {
		t.Fatalf("get capability scenarios: %v", err)
	}
	defer response.Body.Close()
	var scenariosResponse struct {
		OK   bool                     `json:"ok"`
		Data []CapabilityScenarioView `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&scenariosResponse); err != nil {
		t.Fatalf("decode capability scenarios: %v", err)
	}
	if !scenariosResponse.OK || len(scenariosResponse.Data) != 1 || scenariosResponse.Data[0].Scenario.ID != "meeting_to_presentation" || scenariosResponse.Data[0].RecommendedPack.Pack.ID != "advanced" {
		t.Fatalf("unexpected capability scenarios response: %#v", scenariosResponse)
	}
}

func TestCommerceHTTPRejectsWildcardAllowedOrigin(t *testing.T) {
	if err := ValidateCommerceAllowedOrigin("*"); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected wildcard origin to be rejected, got %v", err)
	}
	if err := ValidateCommerceAllowedOrigin("https://site.example/path"); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected origin with path to be rejected, got %v", err)
	}
	if err := ValidateCommerceAllowedOrigin("https://site.example"); err != nil {
		t.Fatalf("expected concrete origin to be accepted, got %v", err)
	}
}

func TestCommerceHTTPHandlerSubmitsProofAndListsReceipts(t *testing.T) {
	receiptPublicKey, receiptPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate receipt key: %v", err)
	}
	var submitCalls int
	var gotPath string
	var gotAuth string
	var gotDevice string
	var gotRequest commerceProofSubmitRequest
	proServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		submitCalls++
		gotPath = request.URL.Path
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		if request.Method != http.MethodPost || gotPath != "/v1/commerce/proofs" {
			http.Error(writer, "unexpected proof submit request", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&gotRequest); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if gotRequest.SchemaVersion != 1 || gotRequest.ClientVersion == "" || gotRequest.SubmittedAt == "" {
			http.Error(writer, "invalid submit envelope", http.StatusBadRequest)
			return
		}
		if !gotRequest.Verification.OK || !VerifyCommerceProof(gotRequest.Proof, "site-submit-nonce").OK {
			http.Error(writer, "invalid commerce proof", http.StatusBadRequest)
			return
		}
		receipt := signedTestCommerceReceipt(t, gotRequest.Proof, receiptPublicKey, receiptPrivateKey, 1)
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(commerceProofSubmitResponse{OK: true, Receipt: receipt})
	}))
	defer proServer.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = proServer.URL
	service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device-1"}
	if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/proof/submit", bytes.NewBufferString(`{"challenge":"site-submit-nonce","yes":true}`))
	if err != nil {
		t.Fatalf("new proof submit request without token: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post proof submit without token: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || submitCalls != 0 {
		t.Fatalf("expected unauthorized proof submit without Pro call, status=%s calls=%d", response.Status, submitCalls)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/proof/submit", bytes.NewBufferString(`{"challenge":"site-submit-nonce","yes":false}`))
	if err != nil {
		t.Fatalf("new proof submit confirmation request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post proof submit without yes: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPreconditionRequired || submitCalls != 0 {
		t.Fatalf("expected confirmation proof submit without Pro call, status=%s calls=%d", response.Status, submitCalls)
	}
	var confirmation struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&confirmation); err != nil {
		t.Fatalf("decode confirmation response: %v", err)
	}
	if confirmation.OK || confirmation.Error.Code != CodeConfirmationRequired {
		t.Fatalf("unexpected confirmation response: %#v", confirmation)
	}

	request, err = http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/proof/submit", bytes.NewBufferString(`{"challenge":"site-submit-nonce","yes":true}`))
	if err != nil {
		t.Fatalf("new proof submit request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post proof submit: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected proof submit status: %s", response.Status)
	}
	if submitCalls != 1 || gotPath != "/v1/commerce/proofs" || gotAuth != "Bearer access" || gotDevice != "device-1" {
		t.Fatalf("unexpected proof submit request: calls=%d path=%q auth=%q device=%q", submitCalls, gotPath, gotAuth, gotDevice)
	}
	var submitResponse struct {
		OK   bool                        `json:"ok"`
		Data CommerceReceiptSubmitResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&submitResponse); err != nil {
		t.Fatalf("decode proof submit response: %v", err)
	}
	if !submitResponse.OK || !submitResponse.Data.Verification.OK || submitResponse.Data.Receipt.ReceiptID == "" || submitResponse.Data.Receipt.Status != commerceReceiptStatusReceived {
		t.Fatalf("unexpected proof submit response: %#v", submitResponse)
	}
	if submitResponse.Data.Receipt.Integrity == nil || submitResponse.Data.Receipt.Integrity.Status != integrityStatusVerified {
		t.Fatalf("expected locally signed receipt integrity: %#v", submitResponse.Data.Receipt)
	}
	if !VerifyCommerceReceipt(submitResponse.Data.Proof, submitResponse.Data.Receipt).OK {
		t.Fatalf("receipt should verify against submitted proof: %#v", submitResponse.Data)
	}

	response, err = http.Get(server.URL + "/v1/commerce/receipts?status=server_received")
	if err != nil {
		t.Fatalf("get commerce receipts: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected receipts status: %s", response.Status)
	}
	var receiptsResponse struct {
		OK   bool                      `json:"ok"`
		Data CommerceReceiptListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&receiptsResponse); err != nil {
		t.Fatalf("decode receipts response: %v", err)
	}
	if !receiptsResponse.OK || len(receiptsResponse.Data.Records) != 1 || receiptsResponse.Data.Records[0].ReceiptID != submitResponse.Data.Receipt.ReceiptID || receiptsResponse.Data.Integrity == nil || receiptsResponse.Data.Integrity.Status != integrityStatusVerified {
		t.Fatalf("unexpected receipts response: %#v", receiptsResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/snapshot?pack_id=standard")
	if err != nil {
		t.Fatalf("get commerce snapshot: %v", err)
	}
	defer response.Body.Close()
	var snapshotResponse struct {
		OK   bool                       `json:"ok"`
		Data CapabilityCommerceSnapshot `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshotResponse); err != nil {
		t.Fatalf("decode snapshot response: %v", err)
	}
	if !snapshotResponse.OK || len(snapshotResponse.Data.Receipts.Records) != 1 || snapshotResponse.Data.Receipts.Records[0].ReceiptID != submitResponse.Data.Receipt.ReceiptID {
		t.Fatalf("expected receipt in website snapshot: %#v", snapshotResponse)
	}
}

func TestCommerceHTTPHandlerPlansCapabilityPackInstall(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/install-plan?pack_id=standard")
	if err != nil {
		t.Fatalf("get install plan: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected install plan status: %s", response.Status)
	}
	var planResponse struct {
		OK   bool                      `json:"ok"`
		Data CapabilityPackInstallPlan `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&planResponse); err != nil {
		t.Fatalf("decode install plan: %v", err)
	}
	if !planResponse.OK || planResponse.Data.Pack.Pack.ID != "standard" || len(planResponse.Data.Changes) == 0 || len(planResponse.Data.BillingPreview) != 2 {
		t.Fatalf("unexpected install plan response: %#v", planResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/install-plan?pack_id=pdf")
	if err != nil {
		t.Fatalf("get pdf install plan: %v", err)
	}
	defer response.Body.Close()
	var pdfPlanResponse struct {
		OK   bool                      `json:"ok"`
		Data CapabilityPackInstallPlan `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&pdfPlanResponse); err != nil {
		t.Fatalf("decode pdf install plan: %v", err)
	}
	if !pdfPlanResponse.OK || pdfPlanResponse.Data.Pack.Pack.ID != "pdf" || len(pdfPlanResponse.Data.Changes) != 1 || len(pdfPlanResponse.Data.BillingPreview) != 1 {
		t.Fatalf("unexpected pdf install plan response: %#v", pdfPlanResponse)
	}
}

func TestCommerceHTTPHandlerInstallsCapabilityScenarioWithScenarioRecords(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/scenario-install-plan?scenario_id=invoice")
	if err != nil {
		t.Fatalf("get scenario install plan: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected scenario plan status: %s", response.Status)
	}
	var planResponse struct {
		OK   bool                          `json:"ok"`
		Data CapabilityScenarioInstallPlan `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&planResponse); err != nil {
		t.Fatalf("decode scenario plan: %v", err)
	}
	if !planResponse.OK || planResponse.Data.Action != "install_scenario" || planResponse.Data.Scenario.Scenario.ID != "invoice_processing" || len(planResponse.Data.PackPlan.BillingPreview) != 2 || planResponse.Data.PackPlan.BillingPreview[0].ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario plan response: %#v", planResponse)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/install-scenario", bytes.NewBufferString(`{"scenario_id":"invoice","yes":true}`))
	if err != nil {
		t.Fatalf("new scenario install request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post scenario install: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected scenario install status: %s", response.Status)
	}
	var installResponse struct {
		OK   bool                            `json:"ok"`
		Data CapabilityScenarioInstallResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&installResponse); err != nil {
		t.Fatalf("decode scenario install: %v", err)
	}
	if !installResponse.OK || installResponse.Data.Scenario.Scenario.ID != "invoice_processing" || installResponse.Data.PackInstall.InstallRecord == nil || installResponse.Data.PackInstall.InstallRecord.ScenarioID != "invoice_processing" || installResponse.Data.PackInstall.InstallRecord.Action != "install_scenario" {
		t.Fatalf("unexpected scenario install response: %#v", installResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/install-records?scenario_id=invoice_processing")
	if err != nil {
		t.Fatalf("get scenario install records: %v", err)
	}
	defer response.Body.Close()
	var installsResponse struct {
		OK   bool                    `json:"ok"`
		Data InstallRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&installsResponse); err != nil {
		t.Fatalf("decode scenario install records: %v", err)
	}
	if !installsResponse.OK || len(installsResponse.Data.Records) != 1 || installsResponse.Data.Records[0].ScenarioID != "invoice_processing" || installsResponse.Data.Records[0].Action != "install_scenario" || installsResponse.Data.Integrity == nil {
		t.Fatalf("unexpected scenario install records response: %#v", installsResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/billing-records?scenario_id=invoice_processing")
	if err != nil {
		t.Fatalf("get scenario billing records: %v", err)
	}
	defer response.Body.Close()
	var billingResponse struct {
		OK   bool                    `json:"ok"`
		Data BillingRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&billingResponse); err != nil {
		t.Fatalf("decode scenario billing records: %v", err)
	}
	if !billingResponse.OK || len(billingResponse.Data.Records) != 2 || billingResponse.Data.Records[0].ScenarioID != "invoice_processing" || len(billingResponse.Data.Totals) != 2 {
		t.Fatalf("unexpected scenario billing records response: %#v", billingResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/scenario-ledger?scenario_id=invoice&type=pack_install")
	if err != nil {
		t.Fatalf("get scenario ledger: %v", err)
	}
	defer response.Body.Close()
	var ledgerResponse struct {
		OK   bool                     `json:"ok"`
		Data CapabilityScenarioLedger `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&ledgerResponse); err != nil {
		t.Fatalf("decode scenario ledger: %v", err)
	}
	if !ledgerResponse.OK || ledgerResponse.Data.Scenario.Scenario.ID != "invoice_processing" || ledgerResponse.Data.LatestInstall == nil || ledgerResponse.Data.LatestInstall.ScenarioID != "invoice_processing" {
		t.Fatalf("unexpected scenario ledger response: %#v", ledgerResponse)
	}
	if len(ledgerResponse.Data.InstallRecords) != 1 || len(ledgerResponse.Data.Billing.Records) != 2 || len(ledgerResponse.Data.PackInstallRecords) != 2 || len(ledgerResponse.Data.UsageRecords) != 0 {
		t.Fatalf("unexpected scenario ledger records response: %#v", ledgerResponse.Data)
	}

	response, err = http.Get(server.URL + "/v1/commerce/snapshot?scenario_id=invoice_processing")
	if err != nil {
		t.Fatalf("get scenario snapshot: %v", err)
	}
	defer response.Body.Close()
	var snapshotResponse struct {
		OK   bool                       `json:"ok"`
		Data CapabilityCommerceSnapshot `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshotResponse); err != nil {
		t.Fatalf("decode scenario snapshot: %v", err)
	}
	if !snapshotResponse.OK || len(snapshotResponse.Data.InstallRecords.Records) != 1 || len(snapshotResponse.Data.Billing.Records) != 2 {
		t.Fatalf("unexpected scenario snapshot response: %#v", snapshotResponse)
	}
}

func TestCommerceHTTPHandlerPlanRequiresPackID(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/install-plan")
	if err != nil {
		t.Fatalf("get install plan without pack id: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", response.Status)
	}
}

func TestCommerceHTTPHandlerInstallsCapabilityPackWithConfirmation(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/install-pack", bytes.NewBufferString(`{"pack_id":"advanced","yes":true}`))
	if err != nil {
		t.Fatalf("new install request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post install pack: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected install status: %s", response.Status)
	}
	var installResponse struct {
		OK   bool                        `json:"ok"`
		Data CapabilityPackInstallResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&installResponse); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	if !installResponse.OK || installResponse.Data.Pack.Pack.ID != "advanced" || !installResponse.Data.Pack.Installed {
		t.Fatalf("unexpected install response: %#v", installResponse)
	}
	if installResponse.Data.InstallRecord == nil || installResponse.Data.InstallRecord.PackID != "advanced" || len(installResponse.Data.BillingRecords) != 2 {
		t.Fatalf("expected install and billing records in response: %#v", installResponse.Data)
	}

	response, err = http.Get(server.URL + "/v1/commerce/snapshot?pack_id=advanced")
	if err != nil {
		t.Fatalf("get snapshot after install: %v", err)
	}
	defer response.Body.Close()
	var snapshotResponse struct {
		OK   bool                       `json:"ok"`
		Data CapabilityCommerceSnapshot `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshotResponse); err != nil {
		t.Fatalf("decode snapshot after install: %v", err)
	}
	if !snapshotResponse.OK || len(snapshotResponse.Data.InstallRecords.Records) != 1 || len(snapshotResponse.Data.Billing.Records) != 2 {
		t.Fatalf("expected installed pack in website snapshot: %#v", snapshotResponse.Data)
	}
}

func TestCommerceHTTPHandlerServesDashboard(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "dashboard-token"}))
	defer server.Close()

	response, err := http.Get(server.URL + "/commerce")
	if err != nil {
		t.Fatalf("get commerce dashboard: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dashboard status: %s", response.Status)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected html content type, got %q", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	if !bytes.Contains(body, []byte("agtx Commerce")) ||
		!bytes.Contains(body, []byte("scenarioFilter")) ||
		!bytes.Contains(body, []byte("deliverables:")) ||
		!bytes.Contains(body, []byte("steps:")) ||
		!bytes.Contains(body, []byte("/v1/commerce/scenario-install-plan")) ||
		!bytes.Contains(body, []byte("/v1/commerce/install-scenario")) ||
		!bytes.Contains(body, []byte("scenario-ledger")) ||
		!bytes.Contains(body, []byte("scenario_id")) ||
		!bytes.Contains(body, []byte("/v1/commerce/install-pack")) ||
		!bytes.Contains(body, []byte("dashboard-token")) {
		t.Fatalf("dashboard missing expected content: %s", string(body))
	}
}

func TestCommerceHTTPHandlerInstallRequiresConfirmation(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/install-pack", bytes.NewBufferString(`{"pack_id":"standard"}`))
	if err != nil {
		t.Fatalf("new install request without confirmation: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post install pack without confirmation: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("expected confirmation status, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Action   string `json:"action"`
				Expected string `json:"expected"`
				Pack     string `json:"pack"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode confirmation response: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeConfirmationRequired || errorResponse.Error.Details.Action != "install-pack" || errorResponse.Error.Details.Expected != "yes=true" || errorResponse.Error.Details.Pack != "standard" {
		t.Fatalf("unexpected confirmation response: %#v", errorResponse)
	}
}

func TestCommerceHTTPHandlerInstallRequiresMutationToken(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	response, err := http.Post(server.URL+"/v1/commerce/install-pack", "application/json", bytes.NewBufferString(`{"pack_id":"standard","yes":true}`))
	if err != nil {
		t.Fatalf("post install pack without token: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Header string `json:"header"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode unauthorized response: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeUnauthorized || errorResponse.Error.Details.Header != "X-AGTX-Commerce-Token" {
		t.Fatalf("unexpected unauthorized response: %#v", errorResponse)
	}
}

func TestCommerceHTTPHandlerRejectsBadLimit(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/snapshot?limit=bad")
	if err != nil {
		t.Fatalf("get bad snapshot: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Parameter string `json:"parameter"`
				Reason    string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeInvalidArgument || errorResponse.Error.Details.Parameter != "limit" || errorResponse.Error.Details.Reason != "invalid_positive_integer" {
		t.Fatalf("unexpected error response: %#v", errorResponse)
	}
}

func TestCommerceHTTPHandlerRejectsBadTimeFilter(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/billing-records?from=bad")
	if err != nil {
		t.Fatalf("get bad time filter: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode time filter error: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeInvalidArgument || errorResponse.Error.Details.Field != "from" {
		t.Fatalf("unexpected time filter error: %#v", errorResponse)
	}
}

func TestCapabilityPackAliasesSupportChineseNames(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	standard, err := service.GetCapabilityPack("\u666e\u901a")
	if err != nil {
		t.Fatalf("lookup standard alias: %v", err)
	}
	if standard.Pack.ID != "standard" {
		t.Fatalf("expected standard alias, got %#v", standard.Pack)
	}
	advanced, err := service.GetCapabilityPack("\u9ad8\u7ea7")
	if err != nil {
		t.Fatalf("lookup advanced alias: %v", err)
	}
	if advanced.Pack.ID != "advanced" {
		t.Fatalf("expected advanced alias, got %#v", advanced.Pack)
	}
}

func scenarioHasRequiredSkill(view CapabilityScenarioView, name string) bool {
	for _, skill := range view.RequiredSkills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func TestRunSkillRecordsLocalUsageEvents(t *testing.T) {
	service := installBillableEchoSkill(t, nil)
	result, err := service.RunSkill(context.Background(), "echo_metered", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("run billable skill: %v", err)
	}
	if result.InvocationID == "" {
		t.Fatal("expected invocation id")
	}
	if len(result.UsageEvents) != 1 {
		t.Fatalf("expected one usage event, got %#v", result.UsageEvents)
	}
	if result.UsageEvents[0].Meter != "call" || result.UsageEvents[0].Quantity != 1 {
		t.Fatalf("unexpected first usage event: %#v", result.UsageEvents[0])
	}
	if result.UsageEvents[0].Status != usageStatusLocalOnly || result.UsageEvents[0].PackID != "echo_metered" || result.UsageEvents[0].VersionID != "1.0.0" {
		t.Fatalf("unexpected local usage event: %#v", result.UsageEvents[0])
	}
	if result.UsageEvents[0].UnitPriceMinor != 3 || result.UsageEvents[0].GrossAmountMinor != 3 {
		t.Fatalf("unexpected local amount: %#v", result.UsageEvents[0])
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{Skill: "echo_metered"})
	if err != nil {
		t.Fatalf("list usage billing records: %v", err)
	}
	if len(billing.Records) != 1 || billing.Records[0].Type != "skill_usage" || billing.Records[0].UsageEventID != result.UsageEvents[0].EventID {
		t.Fatalf("expected usage billing record: %#v", billing)
	}

	scenarioResult, err := service.RunSkillWithOptions(context.Background(), "echo_metered", RunOptions{
		Args:       []string{"invoice"},
		ScenarioID: "invoice",
	})
	if err != nil {
		t.Fatalf("run scenario billable skill: %v", err)
	}
	if scenarioResult.ScenarioID != "invoice_processing" || len(scenarioResult.UsageEvents) != 1 || scenarioResult.UsageEvents[0].ScenarioID != "invoice_processing" {
		t.Fatalf("expected scenario-tagged usage event: %#v", scenarioResult)
	}
	scenarioBilling, err := service.ListBillingRecords(RecordQueryOptions{ScenarioID: "invoice", Skill: "echo_metered", Type: "skill_usage"})
	if err != nil {
		t.Fatalf("list scenario usage billing records: %v", err)
	}
	if len(scenarioBilling.Records) != 1 || scenarioBilling.Records[0].ScenarioID != "invoice_processing" || scenarioBilling.Records[0].UsageEventID != scenarioResult.UsageEvents[0].EventID {
		t.Fatalf("expected scenario usage billing record: %#v", scenarioBilling)
	}
	if _, err := service.RunSkillWithOptions(context.Background(), "echo_metered", RunOptions{ScenarioID: "unknown_scenario"}); !IsErrorCode(err, CodeNotFound) {
		t.Fatalf("expected unknown scenario to fail before run, got %v", err)
	}
}

func TestBuiltInSkillUsageRecordsWebsitePackID(t *testing.T) {
	service := installBillablePDFSkill(t, nil)
	result, err := service.RunSkillWithOptions(context.Background(), "pdf", RunOptions{ScenarioID: "invoice"})
	if err != nil {
		t.Fatalf("run pdf skill: %v", err)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].PackID != "pdf" || result.UsageEvents[0].ScenarioID != "invoice_processing" {
		t.Fatalf("expected pdf usage to use website pack id: %#v", result.UsageEvents)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{PackID: "pdf", ScenarioID: "invoice", Skill: "pdf", Type: "skill_usage"})
	if err != nil {
		t.Fatalf("list pdf usage billing: %v", err)
	}
	if len(billing.Records) != 1 || billing.Records[0].PackID != "pdf" || billing.Records[0].ScenarioID != "invoice_processing" {
		t.Fatalf("expected pdf usage billing under pdf pack: %#v", billing)
	}
}

func TestRunSkillReportsUsageEventsToPro(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotDevice string
	var got commerceUsageRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			t.Fatalf("decode usage request: %v", err)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"ok":true,"event_id":"server-event","status":"recorded","gross_amount_minor":42}`))
	}))
	defer server.Close()

	service := installBillableEchoSkill(t, func(service *Service) {
		service.Config.ProAPIURL = server.URL
		service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device"}
		if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
			t.Fatalf("save auth: %v", err)
		}
	})
	result, err := service.RunSkillWithOptions(context.Background(), "echo_metered", RunOptions{ScenarioID: "invoice"})
	if err != nil {
		t.Fatalf("run billable skill: %v", err)
	}
	if gotPath != "/v1/commerce/usage" || gotAuth != "Bearer access" || gotDevice != "device" {
		t.Fatalf("unexpected usage request path/auth/device: path=%q auth=%q device=%q", gotPath, gotAuth, gotDevice)
	}
	if got.EventID == "" || got.PackID != "echo_metered" || got.VersionID != "1.0.0" || got.VendorID != "agentex" || got.Meter != "call" {
		t.Fatalf("unexpected usage payload: %#v", got)
	}
	if got.ScenarioID != "invoice_processing" || result.ScenarioID != "invoice_processing" {
		t.Fatalf("expected scenario-tagged usage payload: request=%#v result=%#v", got, result)
	}
	if got.InvocationID == "" || got.InvocationID != result.InvocationID {
		t.Fatalf("expected invocation id in payload: request=%#v result=%#v", got, result)
	}
	if got.Quantity != 1 || got.UnitPriceMinor != 3 || got.GrossAmountMinor != 3 || got.Currency != "AGTX_CREDIT" {
		t.Fatalf("unexpected usage amount payload: %#v", got)
	}
	if got.Evidence["source"] != "agtx_cli" || got.Evidence["skill"] != "echo_metered" || got.Evidence["scenario_id"] != "invoice_processing" {
		t.Fatalf("unexpected usage evidence: %#v", got.Evidence)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Status != "recorded" || result.UsageEvents[0].EventID != "server-event" || result.UsageEvents[0].ScenarioID != "invoice_processing" || result.UsageEvents[0].GrossAmountMinor != 42 {
		t.Fatalf("unexpected reported usage events: %#v", result.UsageEvents)
	}
}

func TestRunSkillUsageReportFailureDoesNotFailRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"code":"internal_error","message":"usage down"}}`))
	}))
	defer server.Close()

	service := installBillableEchoSkill(t, func(service *Service) {
		service.Config.ProAPIURL = server.URL
		service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device"}
		if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
			t.Fatalf("save auth: %v", err)
		}
	})
	result, err := service.RunSkill(context.Background(), "echo_metered", nil, nil)
	if err != nil {
		t.Fatalf("run should succeed when usage reporting fails: %v", err)
	}
	if result.ExitCode != 0 || result.Stdout == "" {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Status != usageStatusReportFailed || result.UsageEvents[0].Error == "" {
		t.Fatalf("expected usage report failure in result: %#v", result.UsageEvents)
	}
}

func TestValidateRegistryAcceptsCommerceMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version": 1,
  "skills": [
    {
      "schema_version": 1,
      "name": "invoice_leads",
      "version": "1.0.0",
      "vendor_id": "demo_isv",
      "capability": {
        "class": "commerce",
        "use_when": "Use when an agent should qualify invoice-management leads."
      },
      "summary": "Invoice lead workflow",
      "description": "Qualifies invoice-management leads and attributes downstream purchases.",
      "tags": ["commerce", "invoice"],
      "platforms": [{"os": "darwin", "arch": "arm64"}],
      "billing": {
        "meters": [
          {
            "meter": "success",
            "unit_price": 0,
            "currency": "AGTX_CREDIT",
            "free_quota": 0,
            "hard_limit_supported": true,
            "refund_policy": "Do not bill rejected leads."
          }
        ],
        "revenue_share": {
          "isv": 70,
          "platform": 30,
          "basis": "net_revenue_after_payment_processor_tax_and_refunds"
        }
      },
      "attribution": {
        "events": ["lead_created", "purchase_completed"],
        "default_window_days": {"cpa": 30, "cps": 45},
        "default_cps_rate": 15,
        "renewal_cps": "disabled_by_default"
      },
      "support": {
        "url": "https://example.com/support",
        "privacy_url": "https://example.com/privacy",
        "incident_email": "security@example.com"
      },
      "stub": true
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	result, err := ValidateRegistryFile(path)
	if err != nil {
		t.Fatalf("validate registry: %v", err)
	}
	if !result.OK || result.Skills != 1 {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestValidateRegistryRejectsUnsupportedCommerceMetadata(t *testing.T) {
	tests := map[string]string{
		"capability class":  `"capability":{"class":"unsafe"}`,
		"billing meter":     `"billing":{"meters":[{"meter":"mystery"}]}`,
		"attribution event": `"attribution":{"events":["unknown_event"]}`,
	}

	for name, fragment := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.json")
			data := `{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],` + fragment + `,"stub":true}]}`
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatalf("write registry: %v", err)
			}
			if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestValidateRegistryRequiresSupportForThirdPartyMonetizedSkills(t *testing.T) {
	tests := map[string]string{
		"missing support": `"vendor_id":"demo_isv","billing":{"meters":[{"meter":"call"}]}`,
		"missing privacy": `"vendor_id":"demo_isv","billing":{"meters":[{"meter":"call"}]},"support":{"url":"https://example.com/support","incident_email":"security@example.com"}`,
		"bad support url": `"vendor_id":"demo_isv","billing":{"meters":[{"meter":"call"}]},"support":{"url":"ftp://example.com/support","privacy_url":"https://example.com/privacy","incident_email":"security@example.com"}`,
	}

	for name, fragment := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.json")
			data := `{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0",` + fragment + `,"summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatalf("write registry: %v", err)
			}
			if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestValidateRegistryAllowsFirstPartyBillingWithoutSupport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","vendor_id":"agentex","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"billing":{"meters":[{"meter":"call"}]},"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := ValidateRegistryFile(path); err != nil {
		t.Fatalf("first-party billing should not require explicit support metadata: %v", err)
	}
}

func TestCommerceManifestExamplesValidate(t *testing.T) {
	examplesDir := filepath.Join("..", "..", "docs", "standards", "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected commerce manifest examples")
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(examplesDir, entry.Name()))
			if err != nil {
				t.Fatalf("read example: %v", err)
			}
			var manifest SkillManifest
			if err := decodeJSONStrict(data, &manifest); err != nil {
				t.Fatalf("decode example: %v", err)
			}
			if _, err := validateSkillManifest(manifest); err != nil {
				t.Fatalf("validate example: %v", err)
			}
			if manifest.VendorID == "" || manifest.Capability == nil || manifest.Billing == nil || manifest.Support == nil {
				t.Fatalf("example should include vendor, capability, billing, and support metadata: %#v", manifest)
			}
		})
	}
}

func hasBillingMeter(meters []BillingMeter, want string) bool {
	for _, meter := range meters {
		if meter.Meter == want {
			return true
		}
	}
	return false
}

func installBillableEchoSkill(t *testing.T, configure func(*Service)) *Service {
	t.Helper()
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	if configure != nil {
		configure(service)
	}
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo_metered",
		Version:       "1.0.0",
		VendorID:      "agentex",
		Capability:    &CapabilityInfo{Class: "tool"},
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
		Billing: &BillingInfo{
			Meters: []BillingMeter{
				{Meter: "call", Currency: "AGTX_CREDIT", UnitPrice: 3, RefundPolicy: "Do not bill failed invocations."},
			},
			RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
		},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo_metered"}); err != nil {
		t.Fatalf("install billable skill: %v", err)
	}
	return service
}

func installBillablePDFSkill(t *testing.T, configure func(*Service)) *Service {
	t.Helper()
	root := t.TempDir()
	entrypoint := "pdf.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "pdf.bat"
	}
	archivePath := filepath.Join(root, "pdf.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	if configure != nil {
		configure(service)
	}
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "pdf",
		Version:       "1.0.0",
		VendorID:      "agentex",
		Capability:    &CapabilityInfo{Class: "tool"},
		Summary:       "PDF",
		Description:   "PDF test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
		Billing: &BillingInfo{
			Meters: []BillingMeter{
				{Meter: "page", Currency: "AGTX_CREDIT", UnitPrice: 5, RefundPolicy: "Do not bill failed invocations."},
			},
			RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
		},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"pdf"}); err != nil {
		t.Fatalf("install billable pdf skill: %v", err)
	}
	return service
}
