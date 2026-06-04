package core

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultRecordMaxBytes = 4 * 1024 * 1024
	installRecordsFile    = "install-records.jsonl"
	billingRecordsFile    = "billing-records.jsonl"
)

func DefaultCapabilityPacks() []CapabilityPack {
	return []CapabilityPack{
		{
			SchemaVersion: 1,
			ID:            "standard",
			Name:          "Agentex Standard Capability Pack",
			Tier:          "standard",
			Summary:       "Core document, web, OCR, and research capabilities for everyday agent workflows.",
			Description:   "Installs the default document, web, OCR, and research skills used by ordinary productivity agents.",
			SkillNames:    []string{"web_search", "web_fetch", "research", "ocr", "docx", "xlsx", "pdf"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "seat", UnitPrice: 990, Currency: "USD", HardLimitSupported: true, RefundPolicy: "Seat charges are reversed when provisioning fails."},
					{Meter: "credit", Currency: "AGTX_CREDIT", FreeQuota: 1000, HardLimitSupported: true, RefundPolicy: "Failed skill invocations are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion: 1,
			ID:            "advanced",
			Name:          "Agentex Advanced Capability Pack",
			Tier:          "advanced",
			Summary:       "Full productivity and media capability bundle for higher-volume agent workflows.",
			Description:   "Installs every built-in first-wave skill, including media generation, audio, and presentation handling.",
			SkillNames:    []string{"web_search", "web_fetch", "research", "ocr", "audio", "imagen", "docx", "xlsx", "pptx", "pdf"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "seat", UnitPrice: 2990, Currency: "USD", HardLimitSupported: true, RefundPolicy: "Seat charges are reversed when provisioning fails."},
					{Meter: "credit", Currency: "AGTX_CREDIT", FreeQuota: 5000, HardLimitSupported: true, RefundPolicy: "Failed skill invocations are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
	}
}

func firstPartySupport() *SupportInfo {
	return &SupportInfo{
		URL:           "https://agentex.cc/support",
		PrivacyURL:    "https://agentex.cc/privacy",
		IncidentEmail: "security@agentex.cc",
	}
}

func (s *Service) ListCapabilityPacks() ([]CapabilityPackView, error) {
	installed, err := s.installedSkillsByName()
	if err != nil {
		return nil, err
	}
	packRecords, err := s.latestPackInstallRecords()
	if err != nil {
		return nil, err
	}
	packs := DefaultCapabilityPacks()
	views := make([]CapabilityPackView, 0, len(packs))
	for _, pack := range packs {
		views = append(views, s.capabilityPackView(pack, installed, packRecords[normalizeName(pack.ID)]))
	}
	sort.Slice(views, func(i, j int) bool {
		return packTierRank(views[i].Pack.Tier) < packTierRank(views[j].Pack.Tier)
	})
	return views, nil
}

func (s *Service) GetCapabilityPack(id string) (CapabilityPackView, error) {
	pack, ok := findCapabilityPack(id)
	if !ok {
		return CapabilityPackView{}, NewError(CodeNotFound, "capability pack not found", map[string]any{"pack": id, "supported_packs": capabilityPackIDs()})
	}
	installed, err := s.installedSkillsByName()
	if err != nil {
		return CapabilityPackView{}, err
	}
	packRecords, err := s.latestPackInstallRecords()
	if err != nil {
		return CapabilityPackView{}, err
	}
	return s.capabilityPackView(pack, installed, packRecords[normalizeName(pack.ID)]), nil
}

func (s *Service) PlanCapabilityPackInstall(id string) (CapabilityPackInstallPlan, error) {
	pack, ok := findCapabilityPack(id)
	if !ok {
		return CapabilityPackInstallPlan{}, NewError(CodeNotFound, "capability pack not found", map[string]any{"pack": id, "supported_packs": capabilityPackIDs()})
	}
	view, err := s.GetCapabilityPack(pack.ID)
	if err != nil {
		return CapabilityPackInstallPlan{}, err
	}
	skillPlan, err := s.PlanInstall(pack.SkillNames)
	if err != nil {
		return CapabilityPackInstallPlan{}, err
	}
	billingPreview := billingPreviewForPackInstall(pack, view, s.Auth.DeviceID)
	return CapabilityPackInstallPlan{
		Action:         "install_pack",
		Pack:           view,
		Changes:        skillPlan.Changes,
		BillingPreview: billingPreview,
		Totals:         billingTotals(billingPreview),
		Requires:       []string{"confirmation"},
		Warnings:       capabilityPackPlanWarnings(view, billingPreview),
	}, nil
}

func (s *Service) InstallCapabilityPack(ctx context.Context, id string) (CapabilityPackInstallResult, error) {
	pack, ok := findCapabilityPack(id)
	if !ok {
		return CapabilityPackInstallResult{}, NewError(CodeNotFound, "capability pack not found", map[string]any{"pack": id, "supported_packs": capabilityPackIDs()})
	}
	results := make([]InstallResult, 0, len(pack.SkillNames))
	var record InstallRecord
	var billingRecords []BillingRecord
	err := s.withMutationLock(func() error {
		for _, name := range pack.SkillNames {
			result, err := s.installSkill(ctx, name)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
		record = installRecordForPack(pack, results, s.Auth.DeviceID)
		if err := s.appendInstallRecord(record); err != nil {
			return err
		}
		billingRecords = billingRecordsForPackInstall(pack, record, s.Auth.DeviceID)
		return s.appendBillingRecords(billingRecords)
	})
	if err != nil {
		return CapabilityPackInstallResult{}, err
	}
	view, err := s.GetCapabilityPack(pack.ID)
	if err != nil {
		return CapabilityPackInstallResult{}, err
	}
	return CapabilityPackInstallResult{Pack: view, Results: results, InstallRecord: &record, BillingRecords: billingRecords}, nil
}

func (s *Service) ListInstallRecords(options RecordQueryOptions) ([]InstallRecord, error) {
	if err := ValidateRecordQueryOptions(options); err != nil {
		return nil, err
	}
	records, err := s.readInstallRecords()
	if err != nil {
		return nil, err
	}
	filtered := make([]InstallRecord, 0, len(records))
	for _, record := range records {
		if options.PackID != "" && normalizeName(record.PackID) != normalizeName(options.PackID) {
			continue
		}
		if options.Skill != "" && !installRecordMatchesSkill(record, options.Skill) {
			continue
		}
		if options.Status != "" && normalizeName(record.Status) != normalizeName(options.Status) {
			continue
		}
		if !recordTimeInRange(record.OccurredAt, options) {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].OccurredAt > filtered[j].OccurredAt
	})
	if options.Limit > 0 && len(filtered) > options.Limit {
		filtered = filtered[:options.Limit]
	}
	return filtered, nil
}

func (s *Service) ListBillingRecords(options RecordQueryOptions) (BillingRecordListResult, error) {
	if err := ValidateRecordQueryOptions(options); err != nil {
		return BillingRecordListResult{}, err
	}
	records, err := s.readBillingRecords()
	if err != nil {
		return BillingRecordListResult{}, err
	}
	filtered := make([]BillingRecord, 0, len(records))
	for _, record := range records {
		if options.PackID != "" && normalizeName(record.PackID) != normalizeName(options.PackID) {
			continue
		}
		if options.Skill != "" && normalizeName(record.SkillName) != normalizeName(options.Skill) {
			continue
		}
		if options.Status != "" && normalizeName(record.Status) != normalizeName(options.Status) {
			continue
		}
		if options.Type != "" && normalizeName(record.Type) != normalizeName(options.Type) {
			continue
		}
		if options.Currency != "" && normalizeName(record.Currency) != normalizeName(options.Currency) {
			continue
		}
		if !recordTimeInRange(record.OccurredAt, options) {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].OccurredAt > filtered[j].OccurredAt
	})
	if options.Limit > 0 && len(filtered) > options.Limit {
		filtered = filtered[:options.Limit]
	}
	return BillingRecordListResult{Records: filtered, Totals: billingTotals(filtered)}, nil
}

func ValidateRecordQueryOptions(options RecordQueryOptions) error {
	from := strings.TrimSpace(options.From)
	to := strings.TrimSpace(options.To)
	if from != "" {
		if _, err := time.Parse(time.RFC3339, from); err != nil {
			return NewError(CodeInvalidArgument, "from must be an RFC3339 timestamp", map[string]any{"field": "from", "value": options.From})
		}
	}
	if to != "" {
		if _, err := time.Parse(time.RFC3339, to); err != nil {
			return NewError(CodeInvalidArgument, "to must be an RFC3339 timestamp", map[string]any{"field": "to", "value": options.To})
		}
	}
	if from != "" && to != "" {
		fromTime, _ := time.Parse(time.RFC3339, from)
		toTime, _ := time.Parse(time.RFC3339, to)
		if fromTime.After(toTime) {
			return NewError(CodeInvalidArgument, "from must be before or equal to to", map[string]any{"from": options.From, "to": options.To})
		}
	}
	return nil
}

func (s *Service) CommerceSnapshot(options RecordQueryOptions) (CapabilityCommerceSnapshot, error) {
	packs, err := s.ListCapabilityPacks()
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	installs, err := s.ListInstallRecords(options)
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	billing, err := s.ListBillingRecords(options)
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	return CapabilityCommerceSnapshot{
		SchemaVersion:  1,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Packs:          packs,
		InstallRecords: installs,
		Billing:        billing,
	}, nil
}

func (s *Service) ExportCommerceSnapshot(path string, options RecordQueryOptions) (CommerceSnapshotExportResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CommerceSnapshotExportResult{}, NewError(CodeInvalidArgument, "export path is required", map[string]any{"field": "path"})
	}
	snapshot, err := s.CommerceSnapshot(options)
	if err != nil {
		return CommerceSnapshotExportResult{}, err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return CommerceSnapshotExportResult{}, err
	}
	if err := writeFileAtomic(path, append(data, '\n'), 0o644); err != nil {
		return CommerceSnapshotExportResult{}, err
	}
	return CommerceSnapshotExportResult{Path: path, Snapshot: snapshot}, nil
}

func (s *Service) capabilityPackView(pack CapabilityPack, installed map[string]InstalledSkill, latest InstallRecord) CapabilityPackView {
	view := CapabilityPackView{Pack: pack, Installed: true, Skills: make([]CapabilityPackSkill, 0, len(pack.SkillNames))}
	for _, name := range pack.SkillNames {
		item := CapabilityPackSkill{Name: name}
		if skill, ok := s.Registry.Find(name); ok {
			item.AvailableVersion = skill.Version
		}
		if current, ok := installed[normalizeName(name)]; ok {
			item.Installed = true
			item.InstalledVersion = current.Version
			item.Stub = current.Manifest.Stub
			item.Path = current.Path
			manifest := current.Manifest
			item.Manifest = &manifest
		} else {
			view.Installed = false
		}
		view.Skills = append(view.Skills, item)
	}
	if latest.RecordID != "" {
		view.InstalledAt = latest.OccurredAt
		view.UpdatedAt = latest.OccurredAt
	}
	return view
}

func (s *Service) installedSkillsByName() (map[string]InstalledSkill, error) {
	installed, err := s.ListInstalled()
	if err != nil {
		return nil, err
	}
	byName := make(map[string]InstalledSkill, len(installed))
	for _, skill := range installed {
		byName[normalizeName(skill.Name)] = skill
	}
	return byName, nil
}

func findCapabilityPack(id string) (CapabilityPack, bool) {
	needle := normalizeName(id)
	for _, pack := range DefaultCapabilityPacks() {
		if normalizeName(pack.ID) == needle || normalizeName(pack.Tier) == needle || normalizeName(pack.Name) == needle {
			return pack, true
		}
		for _, alias := range capabilityPackAliases(pack) {
			if normalizeName(alias) == needle {
				return pack, true
			}
		}
	}
	return CapabilityPack{}, false
}

func capabilityPackAliases(pack CapabilityPack) []string {
	switch normalizeName(pack.ID) {
	case "standard":
		return []string{"ordinary", "normal", "basic", "free", "common", "putong", "biaozhun", "jichu", "\u666e\u901a", "\u6807\u51c6", "\u57fa\u7840"}
	case "advanced":
		return []string{"premium", "pro", "plus", "gaoji", "jinjie", "zhuanye", "\u9ad8\u7ea7", "\u8fdb\u9636", "\u4e13\u4e1a"}
	default:
		return nil
	}
}

func capabilityPackIDs() []string {
	packs := DefaultCapabilityPacks()
	ids := make([]string, 0, len(packs))
	for _, pack := range packs {
		ids = append(ids, pack.ID)
	}
	sort.Strings(ids)
	return ids
}

func packTierRank(tier string) int {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "standard":
		return 10
	case "advanced":
		return 20
	default:
		return 100
	}
}

func installRecordForPack(pack CapabilityPack, results []InstallResult, deviceID string) InstallRecord {
	record := InstallRecord{
		RecordID:   "install-" + NewTraceID(),
		Action:     "install_pack",
		PackID:     pack.ID,
		PackTier:   pack.Tier,
		Status:     "installed",
		DeviceID:   strings.TrimSpace(deviceID),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Skills:     make([]InstallRecordSkill, 0, len(results)),
	}
	installedAny := false
	alreadyAny := false
	for _, result := range results {
		record.Skills = append(record.Skills, installRecordSkill(result))
		switch result.Status {
		case "installed":
			installedAny = true
		case "already_installed", "already_current":
			alreadyAny = true
		default:
			record.Status = result.Status
		}
	}
	if !installedAny && alreadyAny && record.Status == "installed" {
		record.Status = "already_installed"
	}
	return record
}

func installRecordForSkill(result InstallResult, deviceID string) InstallRecord {
	return InstallRecord{
		RecordID:   "install-" + NewTraceID(),
		Action:     "install_skill",
		SkillName:  result.Name,
		Status:     result.Status,
		DeviceID:   strings.TrimSpace(deviceID),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Skills:     []InstallRecordSkill{installRecordSkill(result)},
	}
}

func installRecordSkill(result InstallResult) InstallRecordSkill {
	return InstallRecordSkill{
		Name:            result.Name,
		Version:         result.Version,
		PreviousVersion: result.PreviousVersion,
		Status:          result.Status,
		Path:            result.Path,
		Stub:            result.Stub,
	}
}

func billingRecordsForPackInstall(pack CapabilityPack, record InstallRecord, deviceID string) []BillingRecord {
	if pack.Billing == nil || record.Status == "already_installed" {
		return nil
	}
	records := make([]BillingRecord, 0, len(pack.Billing.Meters))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, meter := range pack.Billing.Meters {
		name := strings.TrimSpace(meter.Meter)
		if name == "" {
			continue
		}
		quantity := 1.0
		unitPriceMinor := billingUnitPriceMinor(meter)
		records = append(records, BillingRecord{
			RecordID:         "bill-" + sanitizeUsageID(strings.Join([]string{record.RecordID, pack.ID, name}, "-")),
			Type:             "pack_install",
			PackID:           pack.ID,
			PackTier:         pack.Tier,
			Meter:            name,
			Quantity:         quantity,
			Currency:         strings.TrimSpace(meter.Currency),
			UnitPriceMinor:   unitPriceMinor,
			GrossAmountMinor: grossAmountMinor(quantity, unitPriceMinor),
			Status:           usageStatusLocalOnly,
			OccurredAt:       now,
			Error:            localBillingError(deviceID),
		})
	}
	return records
}

func billingPreviewForPackInstall(pack CapabilityPack, view CapabilityPackView, deviceID string) []BillingRecord {
	if pack.Billing == nil || view.Installed {
		return nil
	}
	record := InstallRecord{RecordID: "preview-" + pack.ID, Status: "planned"}
	records := billingRecordsForPackInstall(pack, record, deviceID)
	for i := range records {
		records[i].RecordID = "preview-" + pack.ID + "-" + sanitizeUsageID(records[i].Meter)
		records[i].Status = "preview"
		records[i].OccurredAt = ""
	}
	return records
}

func capabilityPackPlanWarnings(view CapabilityPackView, billingPreview []BillingRecord) []string {
	warnings := []string{}
	if view.Installed {
		warnings = append(warnings, "pack is already installed; install will not create pack-install billing records")
	}
	if len(billingPreview) > 0 {
		for _, record := range billingPreview {
			if strings.TrimSpace(record.Error) != "" {
				warnings = append(warnings, record.Error)
				break
			}
		}
	}
	return warnings
}

func billingRecordsForUsage(manifest SkillManifest, result RunResult, events []UsageEventResult) []BillingRecord {
	if len(events) == 0 {
		return nil
	}
	pack := packForSkill(manifest.Name)
	records := make([]BillingRecord, 0, len(events))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, event := range events {
		records = append(records, BillingRecord{
			RecordID:         "bill-" + sanitizeUsageID(event.EventID),
			Type:             "skill_usage",
			PackID:           pack.ID,
			PackTier:         pack.Tier,
			SkillName:        manifest.Name,
			VersionID:        manifest.Version,
			VendorID:         event.VendorID,
			Meter:            event.Meter,
			Quantity:         event.Quantity,
			Currency:         event.Currency,
			UnitPriceMinor:   event.UnitPriceMinor,
			GrossAmountMinor: event.GrossAmountMinor,
			Status:           event.Status,
			InvocationID:     result.InvocationID,
			UsageEventID:     event.EventID,
			Error:            event.Error,
			OccurredAt:       now,
		})
	}
	return records
}

func packForSkill(name string) CapabilityPack {
	var fallback CapabilityPack
	needle := normalizeName(name)
	for _, pack := range DefaultCapabilityPacks() {
		for _, skill := range pack.SkillNames {
			if normalizeName(skill) == needle {
				if fallback.ID == "" || packTierRank(pack.Tier) < packTierRank(fallback.Tier) {
					fallback = pack
				}
			}
		}
	}
	return fallback
}

func localBillingError(deviceID string) string {
	if strings.TrimSpace(deviceID) == "" {
		return "pro_api_url is not configured"
	}
	return ""
}

func (s *Service) latestPackInstallRecords() (map[string]InstallRecord, error) {
	records, err := s.readInstallRecords()
	if err != nil {
		return nil, err
	}
	latest := map[string]InstallRecord{}
	for _, record := range records {
		if strings.TrimSpace(record.PackID) == "" {
			continue
		}
		key := normalizeName(record.PackID)
		if latest[key].OccurredAt < record.OccurredAt {
			latest[key] = record
		}
	}
	return latest, nil
}

func installRecordMatchesSkill(record InstallRecord, skill string) bool {
	needle := normalizeName(skill)
	if normalizeName(record.SkillName) == needle {
		return true
	}
	for _, item := range record.Skills {
		if normalizeName(item.Name) == needle {
			return true
		}
	}
	return false
}

func recordTimeInRange(value string, options RecordQueryOptions) bool {
	if strings.TrimSpace(options.From) == "" && strings.TrimSpace(options.To) == "" {
		return true
	}
	occurred, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return false
	}
	if strings.TrimSpace(options.From) != "" {
		from, err := time.Parse(time.RFC3339, strings.TrimSpace(options.From))
		if err != nil || occurred.Before(from) {
			return false
		}
	}
	if strings.TrimSpace(options.To) != "" {
		to, err := time.Parse(time.RFC3339, strings.TrimSpace(options.To))
		if err != nil || occurred.After(to) {
			return false
		}
	}
	return true
}

func billingTotals(records []BillingRecord) []BillingTotal {
	byCurrency := map[string]BillingTotal{}
	for _, record := range records {
		currency := strings.TrimSpace(record.Currency)
		if currency == "" {
			currency = "UNSPECIFIED"
		}
		total := byCurrency[currency]
		total.Currency = currency
		total.Records++
		total.GrossAmountMinor += record.GrossAmountMinor
		byCurrency[currency] = total
	}
	totals := make([]BillingTotal, 0, len(byCurrency))
	for _, total := range byCurrency {
		totals = append(totals, total)
	}
	sort.Slice(totals, func(i, j int) bool {
		return totals[i].Currency < totals[j].Currency
	})
	return totals
}

func (s *Service) appendInstallRecord(record InstallRecord) error {
	return appendJSONLine(s.installRecordsPath(), record)
}

func (s *Service) appendBillingRecords(records []BillingRecord) error {
	for _, record := range records {
		if err := appendJSONLine(s.billingRecordsPath(), record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) readInstallRecords() ([]InstallRecord, error) {
	var records []InstallRecord
	if err := readJSONLines(s.installRecordsPath(), &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Service) readBillingRecords() ([]BillingRecord, error) {
	var records []BillingRecord
	if err := readJSONLines(s.billingRecordsPath(), &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Service) installRecordsPath() string {
	return filepath.Join(s.Paths.ConfigDir, installRecordsFile)
}

func (s *Service) billingRecordsPath() string {
	return filepath.Join(s.Paths.ConfigDir, billingRecordsFile)
}

func appendJSONLine(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readJSONLines[T any](path string, out *[]T) error {
	data, err := readFileLimited(path, defaultRecordMaxBytes, "records")
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for lineNumber, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var item T
		if err := decodeJSONStrict(line, &item); err != nil {
			return NewError(CodeInvalidArgument, "invalid record file", map[string]any{"path": path, "line": lineNumber + 1, "error": err.Error()})
		}
		*out = append(*out, item)
	}
	return nil
}
