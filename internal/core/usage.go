package core

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

const usageStatusLocalOnly = "local_only"
const usageStatusReported = "reported"
const usageStatusReportFailed = "report_failed"

type commerceUsageRequest struct {
	EventID          string         `json:"event_id"`
	PackID           string         `json:"pack_id"`
	ScenarioID       string         `json:"scenario_id,omitempty"`
	VersionID        string         `json:"version_id,omitempty"`
	VendorID         string         `json:"vendor_id,omitempty"`
	Meter            string         `json:"meter"`
	Quantity         float64        `json:"quantity"`
	Currency         string         `json:"currency,omitempty"`
	UnitPriceMinor   int64          `json:"unit_price_minor,omitempty"`
	GrossAmountMinor int64          `json:"gross_amount_minor,omitempty"`
	InvocationID     string         `json:"invocation_id,omitempty"`
	Evidence         map[string]any `json:"evidence,omitempty"`
	OccurredAt       string         `json:"occurred_at,omitempty"`
}

type commerceUsageResponse struct {
	OK                  bool   `json:"ok"`
	Duplicate           bool   `json:"duplicate,omitempty"`
	EventID             string `json:"event_id,omitempty"`
	Status              string `json:"status,omitempty"`
	GrossAmountMinor    int64  `json:"gross_amount_minor,omitempty"`
	ISVAmountMinor      int64  `json:"isv_amount_minor,omitempty"`
	PlatformAmountMinor int64  `json:"platform_amount_minor,omitempty"`
}

func (s *Service) recordRunUsage(ctx context.Context, manifest SkillManifest, result RunResult) []UsageEventResult {
	events := buildRunUsageEvents(manifest, result)
	if len(events) == 0 {
		return nil
	}
	if !usageReportingConfigured(s.Config) {
		for index := range events {
			events[index].Status = usageStatusLocalOnly
			events[index].Error = "pro_api_url is not configured"
		}
		return events
	}
	for index := range events {
		request := usageRequestForEvent(manifest, result, events[index])
		var response commerceUsageResponse
		if err := s.proJSON(ctx, http.MethodPost, "/v1/commerce/usage", request, &response); err != nil {
			events[index].Status = usageStatusReportFailed
			events[index].Error = ErrorFrom(err).Message
			continue
		}
		if strings.TrimSpace(response.EventID) != "" {
			events[index].EventID = response.EventID
		}
		if strings.TrimSpace(response.Status) != "" {
			events[index].Status = response.Status
		} else {
			events[index].Status = usageStatusReported
		}
		if response.GrossAmountMinor != 0 {
			events[index].GrossAmountMinor = response.GrossAmountMinor
		}
	}
	return events
}

func buildRunUsageEvents(manifest SkillManifest, result RunResult) []UsageEventResult {
	if manifest.Billing == nil || len(manifest.Billing.Meters) == 0 || result.ExitCode != 0 || result.Stub {
		return nil
	}
	invocationID := strings.TrimSpace(result.InvocationID)
	if invocationID == "" {
		invocationID = NewTraceID()
	}
	vendorID := strings.TrimSpace(manifest.VendorID)
	if vendorID == "" {
		vendorID = "agentex"
	}
	scenarioID := strings.TrimSpace(result.ScenarioID)
	packID := packIDForUsage(manifest.Name)
	events := make([]UsageEventResult, 0, len(manifest.Billing.Meters))
	for _, meter := range manifest.Billing.Meters {
		name := strings.TrimSpace(meter.Meter)
		if name == "" {
			continue
		}
		quantity := defaultRunUsageQuantity(name)
		unitPriceMinor := billingUnitPriceMinor(meter)
		events = append(events, UsageEventResult{
			EventID:          usageEventID(invocationID, manifest.Name, manifest.Version, name, len(events)),
			PackID:           packID,
			ScenarioID:       scenarioID,
			VersionID:        manifest.Version,
			VendorID:         vendorID,
			Meter:            name,
			Quantity:         quantity,
			Currency:         strings.TrimSpace(meter.Currency),
			UnitPriceMinor:   unitPriceMinor,
			GrossAmountMinor: grossAmountMinor(quantity, unitPriceMinor),
			Status:           usageStatusLocalOnly,
		})
	}
	return events
}

func packIDForUsage(skillName string) string {
	if pack := packForSkill(skillName); strings.TrimSpace(pack.ID) != "" {
		return pack.ID
	}
	return strings.TrimSpace(skillName)
}

func usageRequestForEvent(manifest SkillManifest, result RunResult, event UsageEventResult) commerceUsageRequest {
	evidence := map[string]any{
		"source":           "agtx_cli",
		"skill":            manifest.Name,
		"version":          manifest.Version,
		"exit_code":        result.ExitCode,
		"duration_ms":      result.DurationMS,
		"output_truncated": result.StdoutTruncated || result.StderrTruncated,
	}
	if manifest.Capability != nil && strings.TrimSpace(manifest.Capability.Class) != "" {
		evidence["capability_class"] = strings.TrimSpace(manifest.Capability.Class)
	}
	if strings.TrimSpace(event.ScenarioID) != "" {
		evidence["scenario_id"] = strings.TrimSpace(event.ScenarioID)
	}
	return commerceUsageRequest{
		EventID:          event.EventID,
		PackID:           event.PackID,
		ScenarioID:       event.ScenarioID,
		VersionID:        event.VersionID,
		VendorID:         event.VendorID,
		Meter:            event.Meter,
		Quantity:         event.Quantity,
		Currency:         event.Currency,
		UnitPriceMinor:   event.UnitPriceMinor,
		GrossAmountMinor: event.GrossAmountMinor,
		InvocationID:     result.InvocationID,
		Evidence:         evidence,
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
	}
}

func usageReportingConfigured(config Config) bool {
	if strings.TrimSpace(config.ProAPIURL) != "" {
		return true
	}
	if strings.TrimSpace(config.RegistryURL) == "" {
		return false
	}
	_, err := proAPIURLFromConfig(config)
	return err == nil
}

func defaultRunUsageQuantity(meter string) float64 {
	switch meter {
	case "call", "task", "page", "minute", "token", "credit", "seat", "storage_gb_day", "success", "scan":
		return 1
	default:
		return 1
	}
}

func billingUnitPriceMinor(meter BillingMeter) int64 {
	if meter.UnitPrice <= 0 {
		return 0
	}
	return int64(math.Round(meter.UnitPrice))
}

func grossAmountMinor(quantity float64, unitPriceMinor int64) int64 {
	if quantity <= 0 || unitPriceMinor <= 0 {
		return 0
	}
	return int64(math.Round(quantity * float64(unitPriceMinor)))
}

func usageEventID(invocationID, skill, version, meter string, index int) string {
	parts := []string{strings.TrimSpace(invocationID), strings.TrimSpace(skill), strings.TrimSpace(version), strings.TrimSpace(meter), fmt.Sprint(index + 1)}
	return sanitizeUsageID(strings.Join(parts, "-"))
}

func sanitizeUsageID(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '_' || char == '-' || char == '.':
			builder.WriteRune(char)
		default:
			builder.WriteByte('-')
		}
	}
	clean := strings.Trim(builder.String(), "-._")
	if clean == "" {
		return NewTraceID()
	}
	if len(clean) > 120 {
		return clean[:120]
	}
	return clean
}
