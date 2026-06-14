package core

import (
	"context"
	"sort"
	"strings"
)

func DefaultCapabilityScenarios() []CapabilityScenario {
	return []CapabilityScenario{
		{
			SchemaVersion:     1,
			ID:                "invoice_processing",
			Name:              "Invoice Processing",
			Summary:           "Extract invoice fields, reconcile spreadsheet rows, and prepare an accounting handoff.",
			Description:       "Use this when an agent receives vendor invoices as PDFs, scans, Word files, or Excel exports and needs a repeatable extraction and review workflow.",
			Industry:          "finance_operations",
			RecommendedPackID: "standard",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "extract_and_reconcile_invoices",
				Domains:           []string{"finance", "documents", "operations"},
				Needs:             []string{"extract_invoice_text", "read_tables", "ocr_scans", "validate_totals", "prepare_handoff"},
				RiskLevel:         "medium",
				RequiresUserInput: false,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "vendor_invoices", Label: "Vendor invoices", Description: "PDF, scanned image, or document invoice attachments.", Formats: []string{"pdf", "png", "jpg", "docx"}, Required: true},
				{ID: "accounting_reference_data", Label: "Accounting reference data", Description: "Purchase orders, GL codes, vendor master rows, or AP exports.", Formats: []string{"xlsx", "csv"}, Required: false},
				{ID: "review_rules", Label: "Review rules", Description: "Internal policy, tolerance, or exception rules for invoice review.", Formats: []string{"txt", "docx"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "invoice_extract", Label: "Structured invoice extract", Description: "Normalized vendor, invoice, tax, currency, line-item, and total fields.", Formats: []string{"json", "xlsx"}, Required: true},
				{ID: "exception_report", Label: "Exception report", Description: "Review list for duplicate, mismatch, missing field, or confidence issues.", Formats: []string{"xlsx", "docx"}, Required: true},
				{ID: "accounting_handoff", Label: "Accounting handoff package", Description: "Final export ready for AP or accounting review.", Formats: []string{"xlsx", "csv"}, Required: false},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "intake_documents", Title: "Intake invoices", Stage: "task_profile", Description: "Classify invoice files and preserve original source references.", Skills: []string{"pdf", "ocr", "docx"}},
				{ID: "extract_fields", Title: "Extract fields", Stage: "editing", Description: "Extract headers, taxes, totals, and line items from documents and scans.", Skills: []string{"pdf", "ocr", "xlsx"}},
				{ID: "reconcile_rows", Title: "Reconcile against references", Stage: "verification", Description: "Compare invoice values with spreadsheet rows and flag mismatches.", Skills: []string{"xlsx", deepResearchSkillName}},
				{ID: "prepare_handoff", Title: "Prepare AP handoff", Stage: "handoff", Description: "Package clean rows and exceptions for accounting review.", Skills: []string{"xlsx", "docx", deepResearchSkillName}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "pdf", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Extract text, pages, and attachments from PDF invoices."},
				{Name: "ocr", Role: "fallback", Priority: "required", Stage: "editing", Reason: "Recover text from scanned or photographed invoices."},
				{Name: "xlsx", Role: "validation", Priority: "required", Stage: "verification", Reason: "Read purchase-order, line-item, and accounting spreadsheet exports."},
				{Name: "docx", Role: "implementation", Priority: "conditional", Stage: "editing", Reason: "Handle invoices or remittance notes delivered as Word documents.", Condition: "Use when the input includes DOCX attachments."},
				{Name: deepResearchSkillName, Role: "validation", Priority: "recommended", Stage: "verification", Reason: "Summarize exceptions, confidence, and human review items."},
			},
			AcceptanceCriteria: []string{
				"Every payable line keeps source file, page, vendor, invoice number, currency, and total.",
				"Tax, discount, payment term, and duplicate-invoice conflicts are surfaced before handoff.",
				"No payment or accounting mutation is executed without explicit confirmation.",
			},
			ExecutionNotes: []string{
				"Keep original invoice references in the handoff output.",
				"Flag totals, tax, vendor identity, and currency mismatches for human review.",
				"Do not post accounting mutations without an explicit confirmation step.",
			},
		},
		{
			SchemaVersion:     1,
			ID:                "due_diligence_research",
			Name:              "Due Diligence Research",
			Summary:           "Discover sources, fetch evidence, and synthesize a vendor or market diligence brief.",
			Description:       "Use this when a website or agent needs a reliable research workflow with source discovery, document reading, and structured evidence notes.",
			Industry:          "research",
			RecommendedPackID: "standard",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "produce_evidence_backed_research_brief",
				Domains:           []string{"web", "research", "documents"},
				Needs:             []string{"discover_sources", "fetch_sources", "read_pdfs", "synthesize_findings"},
				RiskLevel:         "medium",
				RequiresUserInput: false,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "research_subject", Label: "Research subject", Description: "Vendor, market, company, product, or question to investigate.", Formats: []string{"txt"}, Required: true},
				{ID: "seed_sources", Label: "Seed sources", Description: "Known URLs, reports, filings, or reference documents.", Formats: []string{"url", "pdf", "docx"}, Required: false},
				{ID: "diligence_questions", Label: "Diligence questions", Description: "Questions, risks, or claims that the brief must answer.", Formats: []string{"txt", "docx"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "source_matrix", Label: "Source matrix", Description: "Selected sources, authority notes, dates, and relevance decisions.", Formats: []string{"json", "xlsx"}, Required: true},
				{ID: "diligence_brief", Label: "Diligence brief", Description: "Evidence-backed summary with findings, caveats, and open questions.", Formats: []string{"docx", "markdown"}, Required: true},
				{ID: "risk_register", Label: "Risk register", Description: "Structured list of unresolved risks and recommended next actions.", Formats: []string{"xlsx", "json"}, Required: false},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "discover_sources", Title: "Discover sources", Stage: "task_profile", Description: "Find candidate primary, recent, and relevant sources.", Skills: []string{"web_search"}},
				{ID: "fetch_evidence", Title: "Fetch evidence", Stage: "editing", Description: "Read selected web pages and source documents.", Skills: []string{"web_fetch", "pdf", "docx"}},
				{ID: "synthesize_findings", Title: "Synthesize findings", Stage: "verification", Description: "Turn evidence into findings, caveats, and confidence notes.", Skills: []string{deepResearchSkillName}},
				{ID: "package_brief", Title: "Package brief", Stage: "handoff", Description: "Prepare a concise decision-ready diligence handoff.", Skills: []string{deepResearchSkillName, "docx"}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "web_search", Role: "discovery", Priority: "required", Stage: "task_profile", Reason: "Find candidate public sources and references."},
				{Name: "web_fetch", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Read selected pages and extract source text."},
				{Name: deepResearchSkillName, Role: "validation", Priority: "required", Stage: "verification", Reason: "Synthesize findings, caveats, and evidence trails."},
				{Name: "pdf", Role: "implementation", Priority: "recommended", Stage: "editing", Reason: "Read annual reports, filings, whitepapers, or attached source PDFs."},
				{Name: "docx", Role: "handoff", Priority: "conditional", Stage: "handoff", Reason: "Prepare or inspect a Word-format diligence memo.", Condition: "Use when the deliverable or input is a DOCX file."},
			},
			AcceptanceCriteria: []string{
				"Important claims are tied to selected sources with dates or source authority notes.",
				"Conflicting evidence and unresolved questions are preserved instead of collapsed.",
				"Source selection excludes irrelevant or low-confidence pages from the final brief.",
			},
			ExecutionNotes: []string{
				"Prefer primary and recent sources when freshness or authority matters.",
				"Fetch only sources that are relevant enough to support the brief.",
				"Expose unresolved conflicts instead of flattening them into a false conclusion.",
			},
		},
		{
			SchemaVersion:     1,
			ID:                "skill_store_security_audit",
			Name:              "Skill Store Security Audit",
			Summary:           "Scan capability packs, manifests, dependency changes, and package artifacts into a reviewable risk report.",
			Description:       "Use this when a skill store submission, third-party capability pack, or package upgrade needs security review before install, approval, or publication.",
			Industry:          "security_operations",
			RecommendedPackID: "security_audit",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "audit_skill_store_submission",
				Domains:           []string{"security", "skills", "supply_chain", "store_review"},
				Needs:             []string{"read_manifest", "scan_permissions", "check_dependencies", "verify_hashes", "prepare_human_review"},
				RiskLevel:         "high",
				RequiresUserInput: true,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "skill_manifest", Label: "Skill manifest", Description: "Capability registry entry, manifest JSON, or plugin metadata.", Formats: []string{"json"}, Required: true},
				{ID: "package_artifact", Label: "Package artifact", Description: "Local zip, tar.gz, directory, or immutable download URL.", Formats: []string{"zip", "tar.gz", "url"}, Required: false},
				{ID: "review_policy", Label: "Review policy", Description: "Allowed permissions, store policy profile, or release checklist.", Formats: []string{"json", "txt"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "risk_report", Label: "Risk report", Description: "Findings grouped by severity with evidence and recommended handling.", Formats: []string{"json", "markdown"}, Required: true},
				{ID: "permission_summary", Label: "Permission summary", Description: "Declared and inferred permission surface for human review.", Formats: []string{"json"}, Required: true},
				{ID: "approval_items", Label: "Approval items", Description: "Blocking or review-needed items before install or publication.", Formats: []string{"json", "markdown"}, Required: true},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "read_submission", Title: "Read submission", Stage: "task_profile", Description: "Load manifest, registry entry, release notes, and package metadata.", Skills: []string{"security_audit"}},
				{ID: "scan_package", Title: "Scan package", Stage: "verification", Description: "Inspect permissions, dependency files, archive paths, scripts, URLs, and hashes without executing package content.", Skills: []string{"security_audit"}},
				{ID: "prepare_review", Title: "Prepare review", Stage: "handoff", Description: "Return risk levels, evidence, and human confirmation items for store approval or install decisions.", Skills: []string{"security_audit"}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "security_audit", Role: "validation", Priority: "required", Stage: "verification", Reason: "Runs static manifest, permission, dependency, archive, URL, and hash checks before trust decisions."},
			},
			AcceptanceCriteria: []string{
				"High-risk permissions, unknown binaries, insecure URLs, missing hashes, and install-time scripts are surfaced with evidence.",
				"The scanner does not execute submitted package content during review.",
				"Blocking findings require explicit human approval before install or store publication.",
			},
			ExecutionNotes: []string{
				"Prefer immutable HTTPS package URLs and declared SHA-256 values.",
				"Treat local_process, filesystem_write, secrets, browser sessions, and credential permissions as high-review areas.",
				"Use --policy strict for store publication checks.",
			},
		},
		{
			SchemaVersion:     1,
			ID:                "contract_review",
			Name:              "Contract Review",
			Summary:           "Read contracts, extract clauses, compare evidence, and prepare review notes.",
			Description:       "Use this for contract intake workflows that combine Word/PDF parsing, OCR fallback, and research-backed risk notes.",
			Industry:          "legal_operations",
			RecommendedPackID: "standard",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "review_contract_documents",
				Domains:           []string{"legal", "documents", "research"},
				Needs:             []string{"read_contracts", "extract_clauses", "ocr_scans", "research_context", "summarize_risks"},
				RiskLevel:         "high",
				RequiresUserInput: true,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "contract_documents", Label: "Contract documents", Description: "Drafts, redlines, executed agreements, or signature packets.", Formats: []string{"docx", "pdf"}, Required: true},
				{ID: "review_playbook", Label: "Review playbook", Description: "Preferred clauses, negotiation positions, or risk checklist.", Formats: []string{"docx", "xlsx", "txt"}, Required: false},
				{ID: "external_terms", Label: "External terms", Description: "Linked policies, order forms, public terms, or referenced URLs.", Formats: []string{"url", "pdf"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "clause_table", Label: "Clause table", Description: "Extracted obligations, key dates, parties, and clause references.", Formats: []string{"xlsx", "json"}, Required: true},
				{ID: "review_memo", Label: "Review memo", Description: "Risk notes, open questions, and human-review recommendations.", Formats: []string{"docx", "markdown"}, Required: true},
				{ID: "redline_questions", Label: "Redline question list", Description: "Issues that need counsel or business owner approval.", Formats: []string{"docx", "xlsx"}, Required: false},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "intake_contracts", Title: "Intake contracts", Stage: "task_profile", Description: "Read document set, identify versions, and preserve references.", Skills: []string{"docx", "pdf", "ocr"}},
				{ID: "extract_clauses", Title: "Extract clauses", Stage: "editing", Description: "Extract clauses, obligations, dates, and referenced materials.", Skills: []string{"docx", "pdf"}},
				{ID: "compare_context", Title: "Compare context", Stage: "verification", Description: "Compare extracted terms with playbooks or public references.", Skills: []string{"web_fetch", deepResearchSkillName}},
				{ID: "prepare_review", Title: "Prepare review", Stage: "handoff", Description: "Create a human-review memo with caveats and questions.", Skills: []string{deepResearchSkillName, "docx"}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "docx", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Read contract drafts, redlines, and clause libraries."},
				{Name: "pdf", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Read executed contracts and PDF attachments."},
				{Name: "ocr", Role: "fallback", Priority: "recommended", Stage: "editing", Reason: "Extract text from scanned signature packets."},
				{Name: "web_fetch", Role: "discovery", Priority: "conditional", Stage: "task_profile", Reason: "Fetch public policies, referenced terms, or vendor pages.", Condition: "Use when the contract references external public URLs."},
				{Name: deepResearchSkillName, Role: "validation", Priority: "required", Stage: "verification", Reason: "Summarize key obligations, open questions, and human-review risks."},
			},
			AcceptanceCriteria: []string{
				"Clause findings include document, section, and page references when available.",
				"Output labels legal-risk support clearly and does not present itself as legal advice.",
				"Acceptance, rejection, or negotiation of language remains a human approval step.",
			},
			ExecutionNotes: []string{
				"Treat the output as review support, not legal advice.",
				"Preserve clause references and page numbers whenever available.",
				"Require human approval before accepting or rejecting contractual language.",
			},
		},
		{
			SchemaVersion:     1,
			ID:                "meeting_to_presentation",
			Name:              "Meeting To Presentation",
			Summary:           "Turn meeting audio and notes into a slide-ready briefing deck.",
			Description:       "Use this when an agent needs to transcribe audio, summarize decisions, and create or update presentation materials.",
			Industry:          "sales_enablement",
			RecommendedPackID: "advanced",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "convert_meeting_materials_to_deck",
				Domains:           []string{"audio", "documents", "presentation", "media"},
				Needs:             []string{"transcribe_audio", "summarize_decisions", "read_slide_decks", "generate_visual_assets", "prepare_handout"},
				RiskLevel:         "medium",
				RequiresUserInput: false,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "meeting_recording", Label: "Meeting recording", Description: "Audio file containing the conversation or customer call.", Formats: []string{"mp3", "wav", "m4a"}, Required: true},
				{ID: "meeting_notes", Label: "Meeting notes", Description: "Agenda, notes, transcript excerpts, or action-item documents.", Formats: []string{"txt", "docx"}, Required: false},
				{ID: "deck_template", Label: "Deck template", Description: "Existing presentation or brand template to update.", Formats: []string{"pptx"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "meeting_summary", Label: "Meeting summary", Description: "Decisions, owners, dates, risks, and follow-up list.", Formats: []string{"docx", "markdown"}, Required: true},
				{ID: "briefing_deck", Label: "Briefing deck", Description: "Slide-ready presentation with speaker notes.", Formats: []string{"pptx"}, Required: true},
				{ID: "supporting_assets", Label: "Supporting assets", Description: "Optional diagrams or visuals for the slide narrative.", Formats: []string{"png", "jpg"}, Required: false},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "transcribe_audio", Title: "Transcribe audio", Stage: "editing", Description: "Convert recording to structured meeting notes.", Skills: []string{"audio"}},
				{ID: "summarize_decisions", Title: "Summarize decisions", Stage: "verification", Description: "Separate decisions, owners, dates, and speculative notes.", Skills: []string{deepResearchSkillName}},
				{ID: "assemble_deck", Title: "Assemble deck", Stage: "editing", Description: "Create or update slide structure and speaker notes.", Skills: []string{"pptx"}},
				{ID: "create_assets", Title: "Create supporting visuals", Stage: "editing", Description: "Generate optional diagrams or images when they clarify the narrative.", Skills: []string{"imagen"}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "audio", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Transcribe meeting recordings and extract speaker-level notes."},
				{Name: deepResearchSkillName, Role: "validation", Priority: "required", Stage: "verification", Reason: "Organize decisions, risks, and next steps into a briefing outline."},
				{Name: "pptx", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Read or update slide decks and speaker notes."},
				{Name: "imagen", Role: "asset_creation", Priority: "recommended", Stage: "editing", Reason: "Create supporting diagrams or visual assets for slides."},
				{Name: "docx", Role: "handoff", Priority: "conditional", Stage: "handoff", Reason: "Generate a Word-format meeting brief or handout.", Condition: "Use when stakeholders need a document handoff alongside slides."},
			},
			AcceptanceCriteria: []string{
				"Deck content separates confirmed decisions from hypotheses or unresolved discussion.",
				"Action items include owners and due dates when stated in the meeting materials.",
				"Generated visuals are tied to slide purpose and can be removed without losing factual content.",
			},
			ExecutionNotes: []string{
				"Keep decisions, owners, and dates separate from speculative notes.",
				"Ask for confirmation before publishing or sending generated decks.",
				"Use generated visuals only when they clarify the slide narrative.",
			},
		},
		{
			SchemaVersion:     1,
			ID:                "marketing_asset_generation",
			Name:              "Marketing Asset Generation",
			Summary:           "Research a campaign, generate media, and package assets into documents or decks.",
			Description:       "Use this for website-driven creative workflows that combine market context, generated images, and campaign deliverables.",
			Industry:          "marketing",
			RecommendedPackID: "advanced",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "produce_campaign_assets",
				Domains:           []string{"web", "research", "media", "presentation"},
				Needs:             []string{"research_context", "generate_visuals", "assemble_deck", "prepare_copy_document"},
				RiskLevel:         "medium",
				RequiresUserInput: true,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "campaign_brief", Label: "Campaign brief", Description: "Audience, goal, positioning, constraints, and channel list.", Formats: []string{"txt", "docx"}, Required: true},
				{ID: "brand_assets", Label: "Brand assets", Description: "Logo, brand guidance, imagery, or existing campaign materials.", Formats: []string{"png", "jpg", "pdf", "pptx"}, Required: false},
				{ID: "reference_sources", Label: "Reference sources", Description: "Competitor pages, product docs, or market references.", Formats: []string{"url", "pdf"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "campaign_research", Label: "Campaign research", Description: "Evidence-backed positioning notes and claim caveats.", Formats: []string{"docx", "markdown"}, Required: true},
				{ID: "creative_assets", Label: "Creative assets", Description: "Generated or adapted visual concepts for campaign review.", Formats: []string{"png", "jpg"}, Required: true},
				{ID: "campaign_deck", Label: "Campaign deck", Description: "Stakeholder presentation with concept options and copy notes.", Formats: []string{"pptx"}, Required: false},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "research_context", Title: "Research context", Stage: "task_profile", Description: "Find market, competitor, and product context for the campaign.", Skills: []string{"web_search", "web_fetch", deepResearchSkillName}},
				{ID: "draft_positioning", Title: "Draft positioning", Stage: "verification", Description: "Prepare claims, caveats, and audience-specific messaging.", Skills: []string{deepResearchSkillName, "docx"}},
				{ID: "generate_visuals", Title: "Generate visuals", Stage: "editing", Description: "Create visual concepts using approved constraints.", Skills: []string{"imagen"}},
				{ID: "package_campaign", Title: "Package campaign", Stage: "handoff", Description: "Assemble deck, copy, and review notes for stakeholders.", Skills: []string{"pptx", "docx"}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "web_search", Role: "discovery", Priority: "required", Stage: "task_profile", Reason: "Discover market references, competitors, and campaign context."},
				{Name: deepResearchSkillName, Role: "validation", Priority: "required", Stage: "verification", Reason: "Turn sources into positioning notes and claims that can be checked."},
				{Name: "imagen", Role: "asset_creation", Priority: "required", Stage: "editing", Reason: "Generate or adapt visual assets for campaign concepts."},
				{Name: "pptx", Role: "handoff", Priority: "recommended", Stage: "handoff", Reason: "Package concepts into a stakeholder presentation."},
				{Name: "docx", Role: "handoff", Priority: "recommended", Stage: "handoff", Reason: "Prepare copy, captions, and review notes in document form."},
			},
			AcceptanceCriteria: []string{
				"Factual claims in copy or deck material are traceable to source context.",
				"Brand, rights, and audience constraints are recorded before final asset generation.",
				"Draft concepts are clearly separated from approved production assets.",
			},
			ExecutionNotes: []string{
				"Keep factual claims tied to source material.",
				"Request brand, rights, and audience constraints before final asset generation.",
				"Separate draft concepts from approved production assets.",
			},
		},
		{
			SchemaVersion:     1,
			ID:                "support_knowledge_base",
			Name:              "Support Knowledge Base",
			Summary:           "Convert manuals, tickets, spreadsheets, and web pages into searchable support articles.",
			Description:       "Use this for customer-support teams that need to ingest mixed document sources and publish structured help content.",
			Industry:          "customer_support",
			RecommendedPackID: "standard",
			TaskProfile: CapabilityTaskProfile{
				Intent:            "build_support_knowledge_base",
				Domains:           []string{"support", "documents", "web"},
				Needs:             []string{"read_manuals", "fetch_public_docs", "extract_tables", "summarize_articles", "ocr_images"},
				RiskLevel:         "low",
				RequiresUserInput: false,
			},
			Inputs: []CapabilityScenarioIO{
				{ID: "source_documents", Label: "Source documents", Description: "Manuals, internal drafts, release notes, or policy documents.", Formats: []string{"pdf", "docx"}, Required: true},
				{ID: "support_exports", Label: "Support exports", Description: "Ticket exports, FAQ spreadsheets, or product matrices.", Formats: []string{"xlsx", "csv"}, Required: false},
				{ID: "public_docs", Label: "Public docs", Description: "Existing help pages, release notes, or support URLs.", Formats: []string{"url"}, Required: false},
			},
			Deliverables: []CapabilityScenarioIO{
				{ID: "article_drafts", Label: "Article drafts", Description: "Searchable support article drafts with source references.", Formats: []string{"docx", "markdown"}, Required: true},
				{ID: "coverage_matrix", Label: "Coverage matrix", Description: "Source-to-article coverage and unresolved contradiction list.", Formats: []string{"xlsx", "json"}, Required: true},
				{ID: "publish_queue", Label: "Publish queue", Description: "Ready, review-needed, and blocked article status list.", Formats: []string{"xlsx", "csv"}, Required: false},
			},
			Workflow: []CapabilityScenarioStep{
				{ID: "ingest_sources", Title: "Ingest sources", Stage: "task_profile", Description: "Collect manuals, docs, spreadsheets, screenshots, and support pages.", Skills: []string{"pdf", "docx", "xlsx", "web_fetch"}},
				{ID: "extract_topics", Title: "Extract topics", Stage: "editing", Description: "Identify procedures, product versions, and repeated support questions.", Skills: []string{"pdf", "docx", "xlsx", "ocr"}},
				{ID: "draft_articles", Title: "Draft articles", Stage: "editing", Description: "Create structured user-facing article drafts.", Skills: []string{deepResearchSkillName, "docx"}},
				{ID: "validate_coverage", Title: "Validate coverage", Stage: "verification", Description: "Flag contradictions, missing versions, and internal-only notes.", Skills: []string{deepResearchSkillName, "xlsx"}},
			},
			Skills: []CapabilityScenarioSkill{
				{Name: "web_fetch", Role: "discovery", Priority: "required", Stage: "task_profile", Reason: "Read existing public docs, release notes, or support pages."},
				{Name: "pdf", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Extract content from PDF manuals and policy documents."},
				{Name: "docx", Role: "implementation", Priority: "required", Stage: "editing", Reason: "Read internal support drafts and article templates."},
				{Name: "xlsx", Role: "implementation", Priority: "recommended", Stage: "editing", Reason: "Read ticket exports, product matrices, and FAQ spreadsheets."},
				{Name: "ocr", Role: "fallback", Priority: "conditional", Stage: "editing", Reason: "Recover text from screenshots embedded in support materials.", Condition: "Use when screenshots or scanned pages contain important instructions."},
				{Name: deepResearchSkillName, Role: "validation", Priority: "recommended", Stage: "verification", Reason: "Create concise article drafts with caveats and source references."},
			},
			AcceptanceCriteria: []string{
				"Article drafts preserve product version, policy date, and source provenance where available.",
				"Internal-only notes are not mixed into user-facing instructions.",
				"Contradictions between source documents are flagged for support-owner review.",
			},
			ExecutionNotes: []string{
				"Preserve product versions and support policy dates.",
				"Separate user-facing instructions from internal-only notes.",
				"Flag contradictions across manuals, tickets, and public docs.",
			},
		},
	}
}

func (s *Service) ListCapabilityScenarios() ([]CapabilityScenarioView, error) {
	scenarios := DefaultCapabilityScenarios()
	views := make([]CapabilityScenarioView, 0, len(scenarios))
	for _, scenario := range scenarios {
		view, err := s.capabilityScenarioView(scenario)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool {
		left := packTierRank(views[i].RecommendedPack.Pack.Tier)
		right := packTierRank(views[j].RecommendedPack.Pack.Tier)
		if left == right {
			return views[i].Scenario.ID < views[j].Scenario.ID
		}
		return left < right
	})
	return views, nil
}

func (s *Service) GetCapabilityScenario(id string) (CapabilityScenarioView, error) {
	scenario, ok := findCapabilityScenario(id)
	if !ok {
		return CapabilityScenarioView{}, NewError(CodeNotFound, "capability scenario not found", map[string]any{"scenario": id, "supported_scenarios": capabilityScenarioIDs()})
	}
	return s.capabilityScenarioView(scenario)
}

func (s *Service) PlanCapabilityScenarioInstall(id string) (CapabilityScenarioInstallPlan, error) {
	view, err := s.GetCapabilityScenario(id)
	if err != nil {
		return CapabilityScenarioInstallPlan{}, err
	}
	packPlan := capabilityPackPlanForScenario(view.InstallPlan, view.Scenario.ID)
	return CapabilityScenarioInstallPlan{
		Action:   "install_scenario",
		Scenario: view,
		PackPlan: packPlan,
		Requires: []string{"confirmation"},
		Warnings: view.Warnings,
	}, nil
}

func (s *Service) InstallCapabilityScenario(ctx context.Context, id string) (CapabilityScenarioInstallResult, error) {
	scenario, ok := findCapabilityScenario(id)
	if !ok {
		return CapabilityScenarioInstallResult{}, NewError(CodeNotFound, "capability scenario not found", map[string]any{"scenario": id, "supported_scenarios": capabilityScenarioIDs()})
	}
	packInstall, err := s.installCapabilityPack(ctx, scenario.RecommendedPackID, scenario.ID)
	if err != nil {
		return CapabilityScenarioInstallResult{}, err
	}
	view, err := s.GetCapabilityScenario(scenario.ID)
	if err != nil {
		return CapabilityScenarioInstallResult{}, err
	}
	return CapabilityScenarioInstallResult{Scenario: view, PackInstall: packInstall}, nil
}

func (s *Service) capabilityScenarioView(scenario CapabilityScenario) (CapabilityScenarioView, error) {
	pack, err := s.GetCapabilityPack(scenario.RecommendedPackID)
	if err != nil {
		return CapabilityScenarioView{}, err
	}
	plan, err := s.PlanCapabilityPackInstall(scenario.RecommendedPackID)
	if err != nil {
		return CapabilityScenarioView{}, err
	}
	plan = capabilityPackPlanForScenario(plan, scenario.ID)
	skills := map[string]CapabilityPackSkill{}
	for _, skill := range pack.Skills {
		skills[normalizeName(skill.Name)] = skill
	}
	required := []CapabilityScenarioSkill{}
	missing := []CapabilityPackSkill{}
	installed := []CapabilityPackSkill{}
	warnings := append([]string{}, plan.Warnings...)
	ready := true
	for _, scenarioSkill := range scenario.Skills {
		if normalizeName(scenarioSkill.Priority) == "required" {
			required = append(required, scenarioSkill)
		}
		skill, ok := skills[normalizeName(scenarioSkill.Name)]
		if !ok {
			ready = false
			warnings = append(warnings, "scenario skill "+scenarioSkill.Name+" is not included in recommended pack "+scenario.RecommendedPackID)
			continue
		}
		if skill.Installed {
			installed = append(installed, skill)
			continue
		}
		missing = append(missing, skill)
		if normalizeName(scenarioSkill.Priority) == "required" {
			ready = false
		}
	}
	return CapabilityScenarioView{
		Scenario:             scenario,
		RecommendedPack:      pack,
		InstallPlan:          plan,
		RequiredSkills:       required,
		MissingSkills:        missing,
		InstalledSkills:      installed,
		Ready:                ready,
		BillingPreviewTotals: plan.Totals,
		Warnings:             dedupeStrings(warnings),
	}, nil
}

func capabilityPackPlanForScenario(plan CapabilityPackInstallPlan, scenarioID string) CapabilityPackInstallPlan {
	scenarioID = strings.TrimSpace(scenarioID)
	if scenarioID == "" {
		return plan
	}
	for i := range plan.BillingPreview {
		plan.BillingPreview[i].ScenarioID = scenarioID
	}
	return plan
}

func findCapabilityScenario(id string) (CapabilityScenario, bool) {
	needle := normalizeName(id)
	for _, scenario := range DefaultCapabilityScenarios() {
		if normalizeName(scenario.ID) == needle || normalizeName(scenario.Name) == needle {
			return scenario, true
		}
		for _, alias := range capabilityScenarioAliases(scenario.ID) {
			if normalizeName(alias) == needle {
				return scenario, true
			}
		}
	}
	return CapabilityScenario{}, false
}

func filterCapabilityScenarioViewsByScenario(scenarios []CapabilityScenarioView, scenarioID string) []CapabilityScenarioView {
	if scenario, ok := findCapabilityScenario(scenarioID); ok {
		scenarioID = scenario.ID
	}
	filtered := scenarios[:0]
	for _, scenario := range scenarios {
		if normalizeName(scenario.Scenario.ID) == normalizeName(scenarioID) {
			filtered = append(filtered, scenario)
		}
	}
	return filtered
}

func filterCapabilityScenarioViewsByPack(scenarios []CapabilityScenarioView, packID string) []CapabilityScenarioView {
	pack, ok := findCapabilityPack(packID)
	if !ok {
		return scenarios[:0]
	}
	filtered := scenarios[:0]
	for _, scenario := range scenarios {
		if capabilityScenarioViewMatchesPack(scenario, pack) {
			filtered = append(filtered, scenario)
		}
	}
	return filtered
}

func capabilityScenarioViewMatchesPack(scenario CapabilityScenarioView, pack CapabilityPack) bool {
	if normalizeName(scenario.RecommendedPack.Pack.ID) == normalizeName(pack.ID) || normalizeName(scenario.Scenario.RecommendedPackID) == normalizeName(pack.ID) {
		return true
	}
	switch normalizeName(pack.Tier) {
	case "standard", "advanced":
		return false
	}
	for _, packSkill := range pack.SkillNames {
		for _, recommendedSkill := range scenario.RecommendedPack.Pack.SkillNames {
			if normalizeName(packSkill) == normalizeName(recommendedSkill) {
				return true
			}
		}
		for _, scenarioSkill := range scenario.Scenario.Skills {
			if normalizeName(packSkill) == normalizeName(scenarioSkill.Name) {
				return true
			}
		}
	}
	return false
}

func capabilityScenarioAliases(id string) []string {
	switch normalizeName(id) {
	case "invoice_processing":
		return []string{"invoice", "invoices", "fapiao", "baoxiao", "\u53d1\u7968", "\u62a5\u9500"}
	case "due_diligence_research":
		return []string{"research", "diligence", "vendor_research", "diaoyan", "\u8c03\u7814", "\u5c3d\u8c03"}
	case "skill_store_security_audit":
		return []string{"security", "security_audit", "audit", "skill_store", "store_review", "supply_chain", "anquan", "shenji", "saomiao", "\u5b89\u5168", "\u5ba1\u8ba1", "\u626b\u63cf", "\u6280\u80fd\u5546\u5e97"}
	case "contract_review":
		return []string{"contract", "legal_review", "hetong", "\u5408\u540c", "\u5408\u540c\u5ba1\u67e5"}
	case "meeting_to_presentation":
		return []string{"meeting_deck", "presentation", "huiyi", "yanjiang", "\u4f1a\u8bae", "\u6f14\u793a"}
	case "marketing_asset_generation":
		return []string{"marketing", "assets", "campaign", "yingxiao", "\u8425\u9500", "\u7d20\u6750"}
	case "support_knowledge_base":
		return []string{"support", "knowledge_base", "kb", "kefu", "\u5ba2\u670d", "\u77e5\u8bc6\u5e93"}
	default:
		return nil
	}
}

func capabilityScenarioIDs() []string {
	scenarios := DefaultCapabilityScenarios()
	ids := make([]string, 0, len(scenarios))
	for _, scenario := range scenarios {
		ids = append(ids, scenario.ID)
	}
	sort.Strings(ids)
	return ids
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := normalizeName(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
