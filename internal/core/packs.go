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
	commerceReceiptsFile  = "commerce-receipts.jsonl"
)

func DefaultCapabilityPacks() []CapabilityPack {
	return []CapabilityPack{
		{
			SchemaVersion:   1,
			ID:              "web_search",
			Name:            "web_search",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Discover relevant web pages, references, candidate sources, or search-result summaries.",
			Summary:         "Native web search capability pack for discovering pages, returning candidate results, and giving agents the right starting points.",
			Description:     "Use when an agent needs to discover relevant pages, official references, or candidate sources before reading or synthesizing evidence.",
			Inputs:          []string{"natural-language query", "locale", "optional freshness constraints"},
			Outputs:         []string{"ranked results", "source metadata", "short evidence snippets"},
			Tags:            []string{"web", "search", "research", "sources"},
			SkillNames:      []string{"web_search"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "call", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed search invocations are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "web_fetch",
			Name:            "web_fetch",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Read a known URL or extract article text and metadata.",
			Summary:         "Native web fetch capability pack for retrieval, article extraction, metadata parsing, and browser relay when pages are hard to read.",
			Description:     "Use when the agent already has a URL and needs readable content, canonical metadata, or an authenticated/browser-assisted fallback path.",
			Inputs:          []string{"URL", "optional session context", "optional browser relay requirement"},
			Outputs:         []string{"title", "canonical URL", "main content", "metadata", "extraction notes"},
			Tags:            []string{"web", "fetch", "article", "metadata"},
			SkillNames:      []string{"web_fetch"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "call", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed fetch invocations are not billed."},
					{Meter: "page", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Pages that fail extraction are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              deepResearchSkillName,
			Name:            "deep_research",
			Tier:            "first_wave",
			CapabilityClass: "workflow",
			UseWhen:         "Handle multi-step evidence gathering, synthesis, product analysis, UI review, or decision support.",
			Summary:         "Package collection, deep research, UI review, and analysis reports into reusable conclusions.",
			Description:     "Use when a task needs planning, source selection, evidence synthesis, caveats, comparison, or decision-ready reporting.",
			Inputs:          []string{"research question", "scope", "depth", "preferred output format"},
			Outputs:         []string{"structured report", "evidence trail", "caveats", "next actions"},
			Tags:            []string{"research", "synthesis", "analysis", "report"},
			SkillNames:      []string{deepResearchSkillName},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "task", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed research tasks are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "ocr",
			Name:            "ocr",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Extract text from screenshots, scans, PDF pages, UI images, or photos.",
			Summary:         "Native OCR for screenshots, scans, PDF pages, and UI text, built for document work and automation.",
			Description:     "Use when text must be recovered from image-like inputs with structure, coordinates, or confidence notes.",
			Inputs:          []string{"image or page file", "optional language hints"},
			Outputs:         []string{"text", "structure", "coordinates", "confidence notes"},
			Tags:            []string{"vision", "ocr", "documents", "screenshots"},
			SkillNames:      []string{"ocr"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "page", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed OCR pages are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "audio",
			Name:            "audio",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Process speech recognition, speech synthesis, meeting notes, or batch audio jobs.",
			Summary:         "ASR and TTS as native capability packs for speech recognition, synthesis, and batch audio jobs.",
			Description:     "Use when an agent needs transcripts, synthesized speech, timelines, speaker hints, or audio-derived meeting notes.",
			Inputs:          []string{"audio file or text", "optional language hints", "optional speaker hints"},
			Outputs:         []string{"transcript", "synthesized audio", "timeline", "summary notes"},
			Tags:            []string{"audio", "asr", "tts", "meetings"},
			SkillNames:      []string{"audio"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "minute", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed audio minutes are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "imagen",
			Name:            "imagen",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Generate image, video, or multimodal media assets from prompts or source media.",
			Summary:         "Text-to-image, image-to-video, and multimodal generation behind one light capability-pack entry.",
			Description:     "Use when the task is media creation rather than image review, accessibility critique, or generic image search.",
			Inputs:          []string{"text prompt", "optional source image", "generation category", "optional model or size"},
			Outputs:         []string{"generated media", "task id", "status", "provider notes"},
			Tags:            []string{"media", "image", "video", "generation"},
			SkillNames:      []string{"imagen"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "task", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed media tasks are not billed."},
					{Meter: "credit", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Unused or failed generation credits are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "docx",
			Name:            "docx",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Read, create, edit, template, summarize, convert, or validate Word documents.",
			Summary:         "Native Word document reading, summarization, structured extraction, and lightweight conversion for contracts, manuals, and long-form notes.",
			Description:     "Use when a .docx file is the source of truth or final artifact and the task needs native Word-style structure.",
			Inputs:          []string{"DOCX file", "action", "optional markdown/content", "optional template variables"},
			Outputs:         []string{"structured text", "metadata", "tables", "comments", "native DOCX artifact"},
			Tags:            []string{"documents", "docx", "word", "office"},
			SkillNames:      []string{"docx"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "task", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed DOCX tasks are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "xlsx",
			Name:            "xlsx",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Read, create, mutate, analyze, validate, or compare native spreadsheet workbooks.",
			Summary:         "Native Excel workbook, sheet range, and cell data processing for budgets, operations tables, and batch cleanup.",
			Description:     "Use when a .xlsx workbook is the source of truth or final artifact and the task needs sheet-aware mutation or analysis.",
			Inputs:          []string{"XLSX file", "action", "sheet/range/cell references", "optional rows or markdown tables"},
			Outputs:         []string{"structured rows", "metadata", "tables", "validation findings", "native XLSX artifact"},
			Tags:            []string{"documents", "xlsx", "spreadsheet", "office"},
			SkillNames:      []string{"xlsx"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "task", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed XLSX tasks are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "pptx",
			Name:            "pptx",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Read, create, edit, duplicate, validate, or update native presentation decks.",
			Summary:         "Native slide text, speaker notes, image placeholders, and page structure extraction so decks become reliable agent-readable material.",
			Description:     "Use when a .pptx deck is the source of truth or final artifact and the task needs slide-aware structure or mutation.",
			Inputs:          []string{"PPTX file", "action", "slide selectors", "optional sections or chart data"},
			Outputs:         []string{"slide structure", "speaker notes", "placeholders", "chart updates", "native PPTX artifact"},
			Tags:            []string{"documents", "pptx", "presentation", "slides"},
			SkillNames:      []string{"pptx"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "task", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed PPTX tasks are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "pdf",
			Name:            "pdf",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Read, create, fill, reformat, split, OCR, or index native PDF documents.",
			Summary:         "Native PDF text extraction, page splitting, OCR fallback, and indexing for papers, bills, scans, and ebooks.",
			Description:     "Use when the final artifact must be PDF, or when text, metadata, forms, pages, or OCR fallback are needed from PDFs.",
			Inputs:          []string{"PDF file or URL", "action", "read mode", "optional pages or form fields"},
			Outputs:         []string{"text", "layout", "pages", "metadata", "forms", "OCR notes", "native PDF artifact"},
			Tags:            []string{"documents", "pdf", "forms", "ocr"},
			SkillNames:      []string{"pdf"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "page", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed PDF pages are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "documents",
			Name:            "docx / xlsx / pptx / pdf",
			Tier:            "first_wave",
			CapabilityClass: "tool",
			UseWhen:         "Read, summarize, convert, index, or extract structured fields from native documents.",
			Summary:         "Native document family pack for Word, Excel, PowerPoint, and PDF workflows.",
			Description:     "Use when the task spans several native document formats and the website wants the registry's document-family capability.",
			Inputs:          []string{"document file", "extraction goal", "optional schema"},
			Outputs:         []string{"structured text", "metadata", "tables", "summaries", "extracted fields"},
			Tags:            []string{"documents", "docx", "xlsx", "pptx", "pdf"},
			SkillNames:      []string{"docx", "xlsx", "pptx", "pdf"},
			Billing: &BillingInfo{
				Meters: []BillingMeter{
					{Meter: "page", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed document pages are not billed."},
					{Meter: "task", Currency: "AGTX_CREDIT", HardLimitSupported: true, RefundPolicy: "Failed document tasks are not billed."},
				},
				RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
			},
			Support: firstPartySupport(),
		},
		{
			SchemaVersion:   1,
			ID:              "standard",
			Name:            "Agentex Standard Capability Pack",
			Tier:            "standard",
			CapabilityClass: "content",
			UseWhen:         "Install the ordinary first-party working set for everyday document, web, OCR, and research workflows.",
			Summary:         "Core document, web, OCR, and research capabilities for everyday agent workflows.",
			Description:     "Installs the default document, web, OCR, and research skills used by ordinary productivity agents.",
			Inputs:          []string{"ordinary productivity task", "documents", "web sources", "optional scans"},
			Outputs:         []string{"installed web, research, OCR, and document skills", "local install records", "billing records"},
			Tags:            []string{"bundle", "standard", "web", "documents", "research"},
			SkillNames:      []string{"web_search", "web_fetch", deepResearchSkillName, "ocr", "docx", "xlsx", "pdf"},
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
			SchemaVersion:   1,
			ID:              "advanced",
			Name:            "Agentex Advanced Capability Pack",
			Tier:            "advanced",
			CapabilityClass: "content",
			UseWhen:         "Install the full first-wave working set for higher-volume productivity, media, audio, and presentation workflows.",
			Summary:         "Full productivity and media capability bundle for higher-volume agent workflows.",
			Description:     "Installs every built-in first-wave skill, including media generation, audio, and presentation handling.",
			Inputs:          []string{"advanced productivity task", "media or audio sources", "documents", "presentations"},
			Outputs:         []string{"installed first-wave skills", "local install records", "billing records"},
			Tags:            []string{"bundle", "advanced", "audio", "media", "documents"},
			SkillNames:      []string{"web_search", "web_fetch", deepResearchSkillName, "ocr", "audio", "imagen", "docx", "xlsx", "pptx", "pdf"},
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
		left := packSortRank(views[i].Pack)
		right := packSortRank(views[j].Pack)
		if left == right {
			return views[i].Pack.ID < views[j].Pack.ID
		}
		return left < right
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
	return s.installCapabilityPack(ctx, id, "")
}

func (s *Service) installCapabilityPack(ctx context.Context, id, scenarioID string) (CapabilityPackInstallResult, error) {
	pack, ok := findCapabilityPack(id)
	if !ok {
		return CapabilityPackInstallResult{}, NewError(CodeNotFound, "capability pack not found", map[string]any{"pack": id, "supported_packs": capabilityPackIDs()})
	}
	scenarioID = strings.TrimSpace(scenarioID)
	results := make([]InstallResult, 0, len(pack.SkillNames))
	var record InstallRecord
	var billingRecords []BillingRecord
	err := s.withMutationLock(func() error {
		if err := s.ensureCommerceLedgersAppendable(); err != nil {
			return err
		}
		for _, name := range pack.SkillNames {
			result, err := s.installSkill(ctx, name)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
		record = installRecordForPack(pack, results, s.Auth.DeviceID, scenarioID)
		signedRecord, err := s.appendInstallRecord(record)
		if err != nil {
			return err
		}
		record = signedRecord
		signedBillingRecords, err := s.appendBillingRecords(billingRecordsForPackInstall(pack, record, s.Auth.DeviceID))
		if err != nil {
			return err
		}
		billingRecords = signedBillingRecords
		return nil
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
	result, err := s.ListInstallRecordsWithIntegrity(options)
	if err != nil {
		return nil, err
	}
	return result.Records, nil
}

func (s *Service) ListInstallRecordsWithIntegrity(options RecordQueryOptions) (InstallRecordListResult, error) {
	if err := ValidateRecordQueryOptions(options); err != nil {
		return InstallRecordListResult{}, err
	}
	options = canonicalRecordQueryOptions(options)
	records, integrity, err := s.readInstallRecordsWithIntegrity()
	if err != nil {
		return InstallRecordListResult{}, err
	}
	filtered := make([]InstallRecord, 0, len(records))
	for _, record := range records {
		if options.PackID != "" && !capabilityRecordPackMatches(record.PackID, options.PackID) {
			continue
		}
		if options.ScenarioID != "" && normalizeName(record.ScenarioID) != normalizeName(options.ScenarioID) {
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
	return InstallRecordListResult{Records: filtered, Integrity: &integrity}, nil
}

func (s *Service) ListBillingRecords(options RecordQueryOptions) (BillingRecordListResult, error) {
	if err := ValidateRecordQueryOptions(options); err != nil {
		return BillingRecordListResult{}, err
	}
	options = canonicalRecordQueryOptions(options)
	records, integrity, err := s.readBillingRecordsWithIntegrity()
	if err != nil {
		return BillingRecordListResult{}, err
	}
	filtered := make([]BillingRecord, 0, len(records))
	for _, record := range records {
		if options.PackID != "" && !capabilityRecordPackMatches(record.PackID, options.PackID) {
			continue
		}
		if options.ScenarioID != "" && normalizeName(record.ScenarioID) != normalizeName(options.ScenarioID) {
			continue
		}
		if options.Skill != "" && canonicalSkillName(record.SkillName) != canonicalSkillName(options.Skill) {
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
	return BillingRecordListResult{Records: filtered, Totals: billingTotals(filtered), Integrity: &integrity}, nil
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

func canonicalRecordQueryOptions(options RecordQueryOptions) RecordQueryOptions {
	if pack, ok := findCapabilityPack(options.PackID); ok {
		options.PackID = pack.ID
	}
	if strings.TrimSpace(options.Skill) != "" {
		options.Skill = canonicalSkillName(options.Skill)
	}
	if scenario, ok := findCapabilityScenario(options.ScenarioID); ok {
		options.ScenarioID = scenario.ID
	}
	return options
}

func (s *Service) CommerceSnapshot(options RecordQueryOptions) (CapabilityCommerceSnapshot, error) {
	packs, err := s.ListCapabilityPacks()
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	scenarios, err := s.ListCapabilityScenarios()
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	if strings.TrimSpace(options.PackID) != "" {
		packs = filterCapabilityPackViewsByPack(packs, options.PackID)
		scenarios = filterCapabilityScenarioViewsByPack(scenarios, options.PackID)
	}
	if strings.TrimSpace(options.ScenarioID) != "" {
		scenarios = filterCapabilityScenarioViewsByScenario(scenarios, options.ScenarioID)
	}
	installs, err := s.ListInstallRecordsWithIntegrity(options)
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	billing, err := s.ListBillingRecords(options)
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	receipts, err := s.ListCommerceReceipts(options)
	if err != nil {
		return CapabilityCommerceSnapshot{}, err
	}
	integrity := []LedgerIntegritySummary{}
	if installs.Integrity != nil {
		integrity = append(integrity, *installs.Integrity)
	}
	if billing.Integrity != nil {
		integrity = append(integrity, *billing.Integrity)
	}
	if receipts.Integrity != nil {
		integrity = append(integrity, *receipts.Integrity)
	}
	return CapabilityCommerceSnapshot{
		SchemaVersion:  1,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Packs:          packs,
		Scenarios:      scenarios,
		InstallRecords: installs,
		Billing:        billing,
		Receipts:       receipts,
		Integrity:      integrity,
	}, nil
}

func (s *Service) CommerceIntegrity() (CommerceIntegrityResult, error) {
	checks := s.checkCommerceLedgers()
	ledgers := make([]LedgerIntegritySummary, 0, 3)
	for _, check := range checks {
		if summary, ok := check.Details.(LedgerIntegritySummary); ok {
			ledgers = append(ledgers, summary)
		}
	}
	return CommerceIntegrityResult{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		OK:            checksOK(checks),
		Summary:       summarizeChecks(checks),
		Ledgers:       ledgers,
		Checks:        checks,
	}, nil
}

func filterCapabilityPackViewsByPack(packs []CapabilityPackView, packID string) []CapabilityPackView {
	if pack, ok := findCapabilityPack(packID); ok {
		packID = pack.ID
	}
	filtered := packs[:0]
	for _, pack := range packs {
		if normalizeName(pack.Pack.ID) == normalizeName(packID) {
			filtered = append(filtered, pack)
		}
	}
	return filtered
}

func (s *Service) CapabilityScenarioLedger(id string, options RecordQueryOptions) (CapabilityScenarioLedger, error) {
	scenario, ok := findCapabilityScenario(id)
	if !ok {
		return CapabilityScenarioLedger{}, NewError(CodeNotFound, "capability scenario not found", map[string]any{"scenario": id, "supported_scenarios": capabilityScenarioIDs()})
	}
	options.ScenarioID = scenario.ID
	view, err := s.GetCapabilityScenario(scenario.ID)
	if err != nil {
		return CapabilityScenarioLedger{}, err
	}
	installs, err := s.ListInstallRecords(options)
	if err != nil {
		return CapabilityScenarioLedger{}, err
	}
	billing, err := s.ListBillingRecords(options)
	if err != nil {
		return CapabilityScenarioLedger{}, err
	}
	var latest *InstallRecord
	if len(installs) > 0 {
		latestRecord := installs[0]
		latest = &latestRecord
	}
	usageRecords := make([]BillingRecord, 0, len(billing.Records))
	packInstallRecords := make([]BillingRecord, 0, len(billing.Records))
	for _, record := range billing.Records {
		switch normalizeName(record.Type) {
		case "skill_usage":
			usageRecords = append(usageRecords, record)
		case "pack_install":
			packInstallRecords = append(packInstallRecords, record)
		}
	}
	return CapabilityScenarioLedger{
		SchemaVersion:      1,
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		Scenario:           view,
		LatestInstall:      latest,
		InstallRecords:     installs,
		Billing:            billing,
		UsageRecords:       usageRecords,
		PackInstallRecords: packInstallRecords,
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
	case "web_search":
		return []string{"web-search", "search", "web query search", "wangye_sousuo", "sousuo", "\u641c\u7d22", "\u7f51\u9875\u641c\u7d22"}
	case "web_fetch":
		return []string{"web-fetch", "web_read", "web-query-read", "fetch", "read_url", "wangye_duqu", "\u7f51\u9875\u8bfb\u53d6", "\u6293\u53d6"}
	case deepResearchSkillName:
		return []string{"research", "analyze", "advisor", "ui_review", "diaoyan", "\u8c03\u7814", "\u7814\u7a76"}
	case "ocr":
		return []string{"vision", "screen_ocr", "image_text", "shibie", "\u8bc6\u522b", "\u6587\u5b57\u8bc6\u522b"}
	case "audio":
		return []string{"asr", "tts", "transcribe", "speech", "yuyin", "\u97f3\u9891", "\u8bed\u97f3", "\u8f6c\u5199"}
	case "imagen":
		return []string{"mediagen", "media", "image", "image_generation", "imagegen", "t2i", "t2v", "shengcheng", "\u5a92\u4f53", "\u56fe\u7247\u751f\u6210", "\u751f\u6210"}
	case "docx":
		return []string{"word", "document", "word_document", "wendang", "\u6587\u6863", "\u6587\u6863\u80fd\u529b\u5305"}
	case "xlsx":
		return []string{"excel", "spreadsheet", "sheet", "biaoge", "\u8868\u683c", "\u7535\u5b50\u8868\u683c"}
	case "pptx":
		return []string{"powerpoint", "presentation", "slides", "deck", "yanshi", "\u6f14\u793a", "\u5e7b\u706f\u7247"}
	case "pdf":
		return []string{"paper", "ebook", "pdf_pack", "pdfpack", "\u8bba\u6587", "\u7535\u5b50\u4e66"}
	case "documents":
		return []string{"document", "office", "docx_xlsx_pptx_pdf", "docx/xlsx/pptx/pdf", "wendangzu", "\u6587\u6863\u65cf", "\u6587\u6863"}
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
	case "first_wave":
		return 5
	case "standard":
		return 10
	case "advanced":
		return 20
	default:
		return 100
	}
}

func packSortRank(pack CapabilityPack) int {
	switch normalizeName(pack.ID) {
	case "web_search":
		return 10
	case "web_fetch":
		return 20
	case deepResearchSkillName:
		return 30
	case "ocr":
		return 40
	case "audio":
		return 50
	case "imagen":
		return 60
	case "docx":
		return 70
	case "xlsx":
		return 80
	case "pptx":
		return 90
	case "pdf":
		return 100
	case "documents":
		return 110
	case "standard":
		return 200
	case "advanced":
		return 210
	default:
		return 1000 + packTierRank(pack.Tier)
	}
}

func installRecordForPack(pack CapabilityPack, results []InstallResult, deviceID, scenarioID string) InstallRecord {
	action := "install_pack"
	if strings.TrimSpace(scenarioID) != "" {
		action = "install_scenario"
	}
	record := InstallRecord{
		RecordID:   "install-" + NewTraceID(),
		Action:     action,
		PackID:     pack.ID,
		PackTier:   pack.Tier,
		ScenarioID: strings.TrimSpace(scenarioID),
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
			ScenarioID:       strings.TrimSpace(record.ScenarioID),
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
		packID := strings.TrimSpace(event.PackID)
		if packID == "" {
			packID = packIDForUsage(manifest.Name)
		}
		records = append(records, BillingRecord{
			RecordID:         "bill-" + sanitizeUsageID(event.EventID),
			Type:             "skill_usage",
			PackID:           packID,
			PackTier:         pack.Tier,
			ScenarioID:       strings.TrimSpace(event.ScenarioID),
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
	needle := canonicalSkillName(name)
	for _, pack := range DefaultCapabilityPacks() {
		for _, skill := range pack.SkillNames {
			if canonicalSkillName(skill) == needle {
				if canonicalSkillName(pack.ID) == needle {
					return pack
				}
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
	records, _, err := s.readInstallRecordsWithIntegrity()
	if err != nil {
		return nil, err
	}
	latest := map[string]InstallRecord{}
	for _, record := range records {
		if strings.TrimSpace(record.PackID) == "" {
			continue
		}
		key := normalizeName(record.PackID)
		if pack, ok := findCapabilityPack(record.PackID); ok {
			key = normalizeName(pack.ID)
		}
		if latest[key].OccurredAt < record.OccurredAt {
			latest[key] = record
		}
	}
	return latest, nil
}

func installRecordMatchesSkill(record InstallRecord, skill string) bool {
	needle := canonicalSkillName(skill)
	if canonicalSkillName(record.SkillName) == needle {
		return true
	}
	for _, item := range record.Skills {
		if canonicalSkillName(item.Name) == needle {
			return true
		}
	}
	return false
}

func capabilityRecordPackMatches(recordPackID, queryPackID string) bool {
	if normalizeName(recordPackID) == normalizeName(queryPackID) {
		return true
	}
	recordPack, recordOK := findCapabilityPack(recordPackID)
	queryPack, queryOK := findCapabilityPack(queryPackID)
	return recordOK && queryOK && normalizeName(recordPack.ID) == normalizeName(queryPack.ID)
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

func (s *Service) appendInstallRecord(record InstallRecord) (InstallRecord, error) {
	return s.appendSignedInstallRecord(record)
}

func (s *Service) appendBillingRecords(records []BillingRecord) ([]BillingRecord, error) {
	return s.appendSignedBillingRecords(records)
}

func (s *Service) readInstallRecords() ([]InstallRecord, error) {
	records, _, err := s.readInstallRecordsWithIntegrity()
	return records, err
}

func (s *Service) readBillingRecords() ([]BillingRecord, error) {
	records, _, err := s.readBillingRecordsWithIntegrity()
	return records, err
}

func (s *Service) InstallRecordIntegrity() (LedgerIntegritySummary, error) {
	summary, _, err := s.verifyInstallRecordsFromDisk()
	return summary, err
}

func (s *Service) BillingRecordIntegrity() (LedgerIntegritySummary, error) {
	summary, _, err := s.verifyBillingRecordsFromDisk()
	return summary, err
}

func (s *Service) verifyInstallRecordsFromDisk() (LedgerIntegritySummary, []InstallRecord, error) {
	records, summary, err := s.readInstallRecordsWithIntegrity()
	return summary, records, err
}

func (s *Service) verifyBillingRecordsFromDisk() (LedgerIntegritySummary, []BillingRecord, error) {
	records, summary, err := s.readBillingRecordsWithIntegrity()
	return summary, records, err
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
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = file.Chmod(0o600)
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
