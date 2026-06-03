package core

import (
	"sort"
	"strings"
)

func DefaultRegistry() Registry {
	return Registry{
		SchemaVersion: 1,
		Skills: []SkillManifest{
			defaultSkill("web_search", "0.1.0", "Web search", "Discover pages and return ranked candidate results for agents and human workflows.", []string{"web", "search", "internet"}, []string{"search", "web", "internet", "browser", "discover", "query", "搜索", "网页", "互联网", "查找"}, []string{"call"}),
			defaultSkill("web_fetch", "0.1.0", "Web fetch", "Fetch web pages, extract readable text, and return metadata when a page is accessible.", []string{"web", "fetch", "reader"}, []string{"fetch", "read", "article", "url", "html", "page", "webpage", "读取", "网页", "正文", "链接", "抓取"}, []string{"page", "call"}),
			defaultSkill("research", "0.1.0", "Research workflow", "Collect evidence, synthesize findings, and produce structured research notes.", []string{"research", "analysis", "report"}, []string{"research", "report", "analysis", "evidence", "compare", "调研", "研究", "报告", "分析", "证据"}, []string{"task"}),
			defaultSkill("ocr", "0.1.0", "OCR", "Extract text and coordinates from screenshots, scans, images, and PDF pages.", []string{"vision", "ocr", "image"}, []string{"ocr", "image", "screenshot", "scan", "text", "vision", "图片", "截图", "扫描", "文字", "识别"}, []string{"page"}),
			defaultSkill("audio", "0.1.0", "Audio ASR/TTS", "Handle speech recognition, speech synthesis, and batch audio processing tasks.", []string{"audio", "asr", "tts"}, []string{"audio", "speech", "transcribe", "voice", "meeting", "notes", "录音", "语音", "转写", "会议", "纪要"}, []string{"minute"}),
			defaultSkill("imagen", "0.1.0", "Media generation", "Expose image and media generation workflows through a lightweight skill entry.", []string{"image", "media", "generation"}, []string{"image", "generate", "media", "picture", "video", "creator", "图片", "生成", "绘图", "视频", "创作"}, []string{"task", "credit"}),
			defaultSkill("docx", "0.1.0", "Word document", "Read, summarize, and extract structured content from Word documents.", []string{"document", "docx", "word"}, []string{"docx", "word", "document", "summary", "summarize", "contract", "文档", "Word", "摘要", "总结", "合同"}, []string{"task"}),
			defaultSkill("xlsx", "0.1.0", "Excel spreadsheet", "Read sheets, ranges, and cells from Excel workbooks for structured extraction.", []string{"document", "xlsx", "spreadsheet"}, []string{"xlsx", "excel", "sheet", "spreadsheet", "table", "invoice", "表格", "Excel", "发票", "账单", "数据"}, []string{"task"}),
			defaultSkill("pptx", "0.1.0", "PowerPoint deck", "Extract slide text, notes, placeholders, and structure from presentation decks.", []string{"document", "pptx", "powerpoint", "slides"}, []string{"pptx", "powerpoint", "slides", "deck", "presentation", "演示", "幻灯片", "课件", "备注"}, []string{"task"}),
			defaultSkill("pdf", "0.1.0", "PDF", "Extract text, split pages, prepare OCR fallback, and index PDF documents.", []string{"document", "pdf"}, []string{"pdf", "paper", "ebook", "bill", "invoice", "summary", "summarize", "PDF", "论文", "账单", "发票", "摘要"}, []string{"page"}),
		},
	}
}

func defaultSkill(name, version, summary, description string, tags, keywords, meters []string) SkillManifest {
	return SkillManifest{
		SchemaVersion: 1,
		Name:          name,
		Version:       version,
		VendorID:      "agentex",
		Capability:    &CapabilityInfo{Class: "tool", UseWhen: description},
		Summary:       summary,
		Description:   description,
		Tags:          tags,
		Keywords:      keywords,
		Permissions: []Permission{
			{Name: "local_process", Description: "Runs a local native skill executable when a real package is installed."},
		},
		Platforms: []PlatformBundle{
			{OS: "darwin", Arch: "arm64"},
			{OS: "darwin", Arch: "amd64"},
			{OS: "linux", Arch: "arm64"},
			{OS: "linux", Arch: "amd64"},
			{OS: "windows", Arch: "amd64"},
		},
		InputSchema: map[string]any{
			"type":        "object",
			"description": "Skill-specific input. v1 stubs return not_implemented until native packages are published.",
		},
		OutputSchema: map[string]any{
			"type":        "object",
			"description": "Skill-specific output.",
		},
		Billing:   defaultBilling(meters),
		Signature: &SignatureInfo{Algorithm: "reserved"},
		Stub:      true,
	}
}

func defaultBilling(meters []string) *BillingInfo {
	billingMeters := make([]BillingMeter, 0, len(meters))
	for _, meter := range meters {
		billingMeters = append(billingMeters, BillingMeter{
			Meter:              meter,
			Currency:           "AGTX_CREDIT",
			HardLimitSupported: true,
			RefundPolicy:       "Do not bill failed invocations.",
		})
	}
	return &BillingInfo{
		Meters: billingMeters,
		RevenueShare: &RevenueShare{
			ISV:      70,
			Platform: 30,
			Basis:    "net_revenue_after_payment_processor_tax_and_refunds",
		},
	}
}

func (r Registry) Find(name string) (SkillManifest, bool) {
	needle := normalizeName(name)
	for _, skill := range r.Skills {
		if normalizeName(skill.Name) == needle {
			return skill, true
		}
	}
	return SkillManifest{}, false
}

func (r Registry) Search(query string, limit int) []SearchResult {
	query = strings.TrimSpace(query)
	results := make([]SearchResult, 0, len(r.Skills))
	for _, skill := range r.Skills {
		score, matched := scoreSkill(skill, query)
		if query == "" || score > 0 {
			results = append(results, SearchResult{Skill: skill, Score: score, Matched: matched})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Skill.Name < results[j].Skill.Name
		}
		return results[i].Score > results[j].Score
	})
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func scoreSkill(skill SkillManifest, query string) (int, []string) {
	if strings.TrimSpace(query) == "" {
		return 1, nil
	}
	queryLower := strings.ToLower(strings.TrimSpace(query))

	searchText := strings.ToLower(strings.Join([]string{
		skill.Name,
		strings.ReplaceAll(skill.Name, "_", " "),
		skill.Summary,
		skill.Description,
		strings.Join(skill.Tags, " "),
		strings.Join(skill.Keywords, " "),
	}, " "))

	score := 0
	matchedSet := map[string]bool{}
	for _, term := range tokenize(query) {
		if term == "" {
			continue
		}
		if ignoredSearchTerm(term) {
			continue
		}
		termScore := 0
		name := strings.ToLower(skill.Name)
		switch {
		case normalizeName(term) == normalizeName(skill.Name):
			termScore = 120
		case strings.Contains(name, strings.ToLower(term)):
			termScore = 80
		case containsFold(skill.Tags, term):
			termScore = 45
		case containsFold(skill.Keywords, term):
			termScore = 35
		case strings.Contains(searchText, strings.ToLower(term)):
			termScore = 15
		}
		if termScore > 0 {
			score += termScore
			matchedSet[term] = true
		}
	}
	for _, value := range append(append([]string{}, skill.Tags...), skill.Keywords...) {
		valueLower := strings.ToLower(strings.TrimSpace(value))
		if valueLower != "" && strings.Contains(queryLower, valueLower) {
			score += 25
			matchedSet[value] = true
		}
	}

	matched := make([]string, 0, len(matchedSet))
	for term := range matchedSet {
		matched = append(matched, term)
	}
	sort.Strings(matched)
	return score, matched
}

func ignoredSearchTerm(term string) bool {
	switch strings.ToLower(strings.TrimSpace(term)) {
	case "a", "an", "and", "are", "as", "by", "for", "from", "in", "into", "of", "on", "or", "the", "to", "with":
		return true
	default:
		return false
	}
}

func tokenize(query string) []string {
	normalized := strings.NewReplacer(",", " ", ".", " ", ";", " ", ":", " ", "\n", " ", "\t", " ", "_", " ", "-", " ").Replace(query)
	fields := strings.Fields(normalized)
	if len(fields) == 0 && strings.TrimSpace(query) != "" {
		return []string{strings.TrimSpace(query)}
	}
	if len(fields) <= 1 {
		return append(fields, strings.TrimSpace(query))
	}
	return fields
}

func containsFold(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func normalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}
