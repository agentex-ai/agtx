package core

import (
	"os"
	"path/filepath"
	"testing"
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
			if skill.Capability == nil || skill.Capability.Class != "tool" {
				t.Fatalf("expected tool capability, got %#v", skill.Capability)
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
