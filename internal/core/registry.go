package core

import (
	"sort"
	"strings"
)

func DefaultRegistry() Registry {
	return Registry{
		SchemaVersion: 1,
		Skills: []SkillManifest{
			defaultSkill("web_search", "0.1.0", "Web search", "Discover pages and return ranked candidate results for agents and human workflows.", []string{"web", "search", "internet"}, []string{"search", "web", "web_query", "internet", "browser", "discover", "query", "搜索", "网页", "互联网", "查找"}, []string{"call"}),
			defaultSkill("web_fetch", "0.1.0", "Web fetch", "Fetch web pages, extract readable text, and return metadata when a page is accessible.", []string{"web", "fetch", "reader"}, []string{"fetch", "read", "web_query", "article", "url", "html", "page", "webpage", "读取", "网页", "正文", "链接", "抓取"}, []string{"page", "call"}),
			defaultSkill(deepResearchSkillName, "0.1.0", "Deep research workflow", "Collect evidence, synthesize findings, and produce structured research notes.", []string{"research", "analysis", "report"}, []string{"deep_research", "research", "report", "analysis", "evidence", "compare", "调研", "研究", "报告", "分析", "证据"}, []string{"task"}),
			defaultSkill("security_audit", "0.1.0", "Security audit", "Audit capability packs, skill manifests, package archives, permissions, dependencies, and download sources before install, upgrade, or store publication.", []string{"security", "audit", "scan", "supply-chain"}, []string{"security", "audit", "scan", "risk", "manifest", "permission", "permissions", "dependency", "dependencies", "supply_chain", "skill_store", "安全", "审计", "扫描", "风险", "权限", "依赖", "技能商店"}, []string{"scan"}),
			defaultOCRSkill(),
			defaultSkill("audio", "0.1.0", "Audio ASR/TTS", "Handle speech recognition, speech synthesis, and batch audio processing tasks.", []string{"audio", "asr", "tts"}, []string{"audio", "speech", "transcribe", "voice", "meeting", "notes", "录音", "语音", "转写", "会议", "纪要"}, []string{"minute"}),
			defaultSkill("imagen", "0.1.0", "Media generation", "Expose image and media generation workflows through a lightweight skill entry.", []string{"image", "media", "generation"}, []string{"image", "generate", "media", "picture", "video", "creator", "图片", "生成", "绘图", "视频", "创作"}, []string{"task", "credit"}),
			defaultSkill("docx", "0.2.0", "Word document", "Read, summarize, and extract structured content from Word documents.", []string{"document", "docx", "word"}, []string{"docx", "word", "document", "summary", "summarize", "contract", "文档", "Word", "摘要", "总结", "合同"}, []string{"task"}),
			defaultSkill("xlsx", "0.2.0", "Excel spreadsheet", "Read sheets, ranges, and cells from Excel workbooks for structured extraction.", []string{"document", "xlsx", "spreadsheet"}, []string{"xlsx", "excel", "sheet", "spreadsheet", "table", "invoice", "表格", "Excel", "发票", "账单", "数据"}, []string{"task"}),
			defaultSkill("pptx", "0.2.0", "PowerPoint deck", "Extract slide text, notes, placeholders, and structure from presentation decks.", []string{"document", "pptx", "powerpoint", "slides"}, []string{"pptx", "powerpoint", "slides", "deck", "presentation", "演示", "幻灯片", "课件", "备注"}, []string{"task"}),
			defaultSkill("pdf", "0.2.0", "PDF", "Extract text, split pages, prepare OCR fallback, and index PDF documents.", []string{"document", "pdf"}, []string{"pdf", "paper", "ebook", "bill", "invoice", "summary", "summarize", "PDF", "论文", "账单", "发票", "摘要"}, []string{"page"}),
		},
	}
}

func defaultOCRSkill() SkillManifest {
	skill := defaultSkill(
		"ocr",
		"0.6.0",
		"RapidOCR OCR",
		"Extract text, layout, coordinates, and confidence from screenshots, scans, images, and rendered PDF page images with RapidOCR-compatible PP-OCRv6 profiles.",
		[]string{"vision", "ocr", "image", "rapidocr", "ppocrv6", "documents"},
		[]string{"ocr", "rapidocr", "rapid_ocr", "ppocr", "ppocrv6", "pp-ocrv6", "paddleocr", "paddle_ocr", "image", "screenshot", "scan", "text", "vision", "layout", "coordinates", "confidence", "图片", "截图", "扫描", "文字", "识别"},
		[]string{"page"},
	)
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "RapidOCR-compatible OCR input for the built-in native OCR runtime.",
		"additionalProperties": true,
		"properties": map[string]any{
			"input": map[string]any{
				"type":        "string",
				"description": "Image path or rendered page image accepted by the built-in native OCR runtime.",
			},
			"model_profile": map[string]any{
				"type":        "string",
				"description": "Preferred OCR model family. Native packages should prefer ppocrv6 when available and fall back explicitly when not.",
				"enum":        []string{"auto", "ppocrv6", "ppocrv5", "ppocrv4"},
				"default":     "ppocrv6",
			},
			"model_size": map[string]any{
				"type":        "string",
				"description": "PP-OCRv6 ONNX asset size used by the built-in model downloader.",
				"enum":        []string{"auto", "tiny", "small", "medium"},
				"default":     "tiny",
			},
			"backend": map[string]any{
				"type":        "string",
				"description": "Native built-in OCR inference backend. Python and NPM engines are not used.",
				"enum":        []string{"auto", "onnxruntime", "ncnn"},
				"default":     "auto",
			},
			"model_dir": map[string]any{
				"type":        "string",
				"description": "Local OCR model directory used by the built-in native runtime.",
			},
			"runtime_dir": map[string]any{
				"type":        "string",
				"description": "Local native inference runtime directory used for ONNX Runtime or ncnn shared libraries.",
			},
			"download_runtime": map[string]any{
				"type":        "boolean",
				"description": "Download the native ONNX Runtime CPU shared library for the current platform.",
			},
			"download_models": map[string]any{
				"type":        "boolean",
				"description": "Download PP-OCRv6 ONNX detector and recognizer assets for the selected model size.",
			},
			"keep_archive": map[string]any{
				"type":        "boolean",
				"description": "Keep the downloaded ONNX Runtime archive after extracting the shared library.",
			},
			"dry_run": map[string]any{
				"type":        "boolean",
				"description": "Plan OCR runtime or model downloads without writing files.",
			},
			"det_model": map[string]any{
				"type":        "string",
				"description": "Optional local detector model override, absolute or relative to the OCR model directory.",
			},
			"rec_model": map[string]any{
				"type":        "string",
				"description": "Optional local recognizer model override, absolute or relative to the OCR model directory.",
			},
			"keys": map[string]any{
				"type":        "string",
				"description": "Optional recognition keys dictionary override, absolute or relative to the OCR model directory.",
			},
			"det_limit_side_len": map[string]any{
				"type":        "integer",
				"description": "Detector resize side limit used by the native OCR path.",
				"default":     736,
			},
			"det_threshold": map[string]any{
				"type":        "number",
				"description": "Detector binary map threshold.",
				"default":     0.3,
			},
			"box_threshold": map[string]any{
				"type":        "number",
				"description": "Minimum average score for a detected text box.",
				"default":     0.5,
			},
			"unclip_ratio": map[string]any{
				"type":        "number",
				"description": "Expansion ratio for detector boxes before recognition crops.",
				"default":     1.6,
			},
			"max_candidates": map[string]any{
				"type":        "integer",
				"description": "Maximum detector boxes to send to recognition.",
				"default":     1000,
			},
			"text_score": map[string]any{
				"type":        "number",
				"description": "Minimum recognizer confidence for returned text lines.",
				"default":     0.5,
			},
			"rec_width": map[string]any{
				"type":        "integer",
				"description": "Optional fixed recognizer crop width. Overrides dynamic-width recognizer sizing.",
			},
			"rec_height": map[string]any{
				"type":        "integer",
				"description": "Optional recognizer crop height override.",
			},
			"rec_max_width": map[string]any{
				"type":        "integer",
				"description": "Maximum crop width for dynamic-width recognizer models.",
				"default":     1600,
			},
			"language_hints": map[string]any{
				"type":        "array",
				"description": "Optional language hints such as ch, en, latin, korean, arabic, or cyrillic.",
				"items":       map[string]any{"type": "string"},
			},
			"return_layout": map[string]any{
				"type":        "boolean",
				"description": "Return detected text boxes, confidence, and reading order when supported.",
			},
			"return_markdown": map[string]any{
				"type":        "boolean",
				"description": "Return markdown-friendly text when supported.",
			},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "RapidOCR-compatible OCR result.",
		"properties": map[string]any{
			"text": map[string]any{
				"type":        "string",
				"description": "Plain recognized text.",
			},
			"markdown": map[string]any{
				"type":        "string",
				"description": "Markdown-oriented text when requested and supported.",
			},
			"model_profile": map[string]any{
				"type":        "string",
				"description": "OCR model profile actually used, such as ppocrv6 or a documented fallback.",
			},
			"engine": map[string]any{
				"type":        "string",
				"description": "Inference engine actually used.",
			},
			"detected_boxes": map[string]any{
				"type":        "integer",
				"description": "Number of text boxes detected before recognition filtering.",
			},
			"processed_boxes": map[string]any{
				"type":        "integer",
				"description": "Number of detected boxes sent to the recognizer.",
			},
			"lines": map[string]any{
				"type":        "array",
				"description": "Recognized text lines with optional layout metadata.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":                 map[string]any{"type": "string"},
						"confidence":           map[string]any{"type": "number"},
						"detection_confidence": map[string]any{"type": "number"},
						"bbox": map[string]any{
							"type":        "array",
							"description": "Bounding polygon or rectangle coordinates.",
							"items":       map[string]any{"type": "number"},
						},
						"page": map[string]any{"type": "integer"},
					},
				},
			},
			"pages": map[string]any{
				"type":        "array",
				"description": "Per-page text and layout summaries for multi-page inputs.",
				"items":       map[string]any{"type": "object"},
			},
			"warnings": map[string]any{
				"type":        "array",
				"description": "Fallbacks, unsupported options, or quality warnings.",
				"items":       map[string]any{"type": "string"},
			},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-native-ocr-v1",
		Backends:      []string{"onnxruntime", "ncnn"},
		ModelProfiles: []string{"ppocrv6", "ppocrv5", "ppocrv4"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInWebSearchSkill(skill SkillManifest) SkillManifest {
	skill.Version = "0.2.0"
	skill.Summary = "Web search"
	skill.Description = "Search the web through a lightweight HTTP search endpoint and return ranked result candidates for agents before known-URL fetching."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "search")
	skill.Permissions = []Permission{{Name: "network", Description: "Sends HTTPS requests to a search endpoint, or localhost HTTP for local fixtures and private proxies."}}
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "Web search query input.",
		"additionalProperties": true,
		"required":             []string{"query"},
		"properties": map[string]any{
			"query":       map[string]any{"type": "string", "description": "Search query text."},
			"q":           map[string]any{"type": "string", "description": "Alias for query."},
			"provider":    map[string]any{"type": "string", "description": "Provider label for the result source."},
			"base_url":    map[string]any{"type": "string", "description": "Optional HTTPS search endpoint or localhost HTTP endpoint. The runtime appends q and count query parameters."},
			"max_results": map[string]any{"type": "integer", "description": "Maximum ranked results to return.", "default": 10},
			"timeout_ms":  map[string]any{"type": "integer", "description": "Optional per-request timeout in milliseconds."},
			"max_bytes":   map[string]any{"type": "integer", "description": "Maximum response body bytes to read before parsing."},
			"user_agent":  map[string]any{"type": "string", "description": "Optional user agent override."},
			"region":      map[string]any{"type": "string", "description": "Optional region hint for compatible providers."},
			"language":    map[string]any{"type": "string", "description": "Optional language hint for compatible providers."},
			"safe_search": map[string]any{"type": "string", "description": "Optional safe-search hint for compatible providers."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Ranked search result candidates.",
		"properties": map[string]any{
			"query":        map[string]any{"type": "string"},
			"provider":     map[string]any{"type": "string"},
			"url":          map[string]any{"type": "string"},
			"final_url":    map[string]any{"type": "string"},
			"status_code":  map[string]any{"type": "integer"},
			"content_type": map[string]any{"type": "string"},
			"results":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"count":        map[string]any{"type": "integer"},
			"bytes":        map[string]any{"type": "integer"},
			"warnings":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-web-search-v1",
		Backends:      []string{"search_http"},
		ModelProfiles: []string{"search_results_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInDeepResearchSkill(skill SkillManifest) SkillManifest {
	skill.Version = "0.2.0"
	skill.Summary = "Deep research workflow"
	skill.Description = "Plan searches, gather candidate sources, fetch readable pages, and produce extractive evidence notes and a structured research brief without external runtimes."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "research_workflow")
	skill.Permissions = []Permission{{Name: "network", Description: "Optionally sends HTTPS requests to search and source URLs, or localhost HTTP for local fixtures and private proxies."}}
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "Research task input.",
		"additionalProperties": true,
		"required":             []string{"question"},
		"properties": map[string]any{
			"question":        map[string]any{"type": "string", "description": "Research question or task."},
			"query":           map[string]any{"type": "string", "description": "Alias for question."},
			"topic":           map[string]any{"type": "string", "description": "Alias for question."},
			"scope":           map[string]any{"type": "string", "description": "Optional scope or constraints."},
			"depth":           map[string]any{"type": "string", "description": "Optional depth hint such as quick, standard, or deep."},
			"search_queries":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"urls":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"sources":         map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"max_sources":     map[string]any{"type": "integer", "description": "Maximum sources to retain.", "default": 5},
			"max_results":     map[string]any{"type": "integer", "description": "Maximum search results per query."},
			"search_base_url": map[string]any{"type": "string", "description": "Optional HTTPS search endpoint or localhost HTTP search proxy."},
			"skip_search":     map[string]any{"type": "boolean", "description": "Use only supplied sources and URLs."},
			"skip_fetch":      map[string]any{"type": "boolean", "description": "Do not fetch URLs; rely on supplied text and snippets."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Structured research brief with sources and extractive findings.",
		"properties": map[string]any{
			"kind":         map[string]any{"type": "string"},
			"question":     map[string]any{"type": "string"},
			"scope":        map[string]any{"type": "string"},
			"depth":        map[string]any{"type": "string"},
			"queries":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"sources":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"findings":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"caveats":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"next_actions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"report":       map[string]any{"type": "string"},
			"warnings":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-deep-research-v1",
		Backends:      []string{"research_workflow", "search_http", "net_http"},
		ModelProfiles: []string{"extractive_research_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInAudioSkill(skill SkillManifest) SkillManifest {
	skill.Version = "0.2.0"
	skill.Summary = "Audio analysis and meeting notes"
	skill.Description = "Inspect WAV audio, normalize supplied transcripts or segments, and produce lightweight meeting notes with the Go standard library. Native ASR/TTS model backends can be added later without changing the pack contract."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "wav", "meeting_notes")
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "Audio file or transcript input.",
		"additionalProperties": true,
		"properties": map[string]any{
			"path":           map[string]any{"type": "string", "description": "Local WAV audio file path."},
			"input":          map[string]any{"type": "string", "description": "Alias for path when JSON input is used."},
			"file":           map[string]any{"type": "string", "description": "Alias for path when JSON input is used."},
			"action":         map[string]any{"type": "string", "description": "inspect, analyze, transcribe, summarize, meeting_notes, or tts."},
			"transcript":     map[string]any{"type": "string", "description": "Existing transcript text for notes and segmentation."},
			"text":           map[string]any{"type": "string", "description": "Alias for transcript or text to hand to external TTS backends."},
			"segments":       map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"language_hints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"speaker_hints":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"max_bytes":      map[string]any{"type": "integer", "description": "Maximum audio bytes to inspect."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Audio metadata, transcript, and meeting notes.",
		"properties": map[string]any{
			"kind":       map[string]any{"type": "string"},
			"source":     map[string]any{"type": "string"},
			"action":     map[string]any{"type": "string"},
			"audio":      map[string]any{"type": "object"},
			"transcript": map[string]any{"type": "string"},
			"segments":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"notes":      map[string]any{"type": "object"},
			"warnings":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"metadata":   map[string]any{"type": "object"},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-audio-v1",
		Backends:      []string{"wav_audio", "transcript_notes"},
		ModelProfiles: []string{"wav_inspect_v1", "meeting_notes_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInImagenSkill(skill SkillManifest) SkillManifest {
	skill.Version = "0.2.0"
	skill.Summary = "Local media generation"
	skill.Description = "Generate deterministic local PNG visual assets and media-generation manifests from prompts with the Go standard library. Model-backed image and video providers can extend the same pack contract later."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "procedural_png", "media_manifest")
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "Media generation prompt input.",
		"additionalProperties": true,
		"required":             []string{"prompt"},
		"properties": map[string]any{
			"prompt":          map[string]any{"type": "string", "description": "Text prompt for the generated asset."},
			"text":            map[string]any{"type": "string", "description": "Alias for prompt."},
			"action":          map[string]any{"type": "string", "description": "text_to_image, image_to_video, storyboard, or media_plan."},
			"mode":            map[string]any{"type": "string", "description": "Alias for action."},
			"style":           map[string]any{"type": "string", "description": "Optional style hint."},
			"negative_prompt": map[string]any{"type": "string", "description": "Optional negative prompt recorded in the manifest."},
			"output_dir":      map[string]any{"type": "string", "description": "Directory for generated PNG assets and manifest."},
			"output":          map[string]any{"type": "string", "description": "Single output PNG path when count is 1."},
			"width":           map[string]any{"type": "integer", "description": "Output width in pixels.", "default": 1024},
			"height":          map[string]any{"type": "integer", "description": "Output height in pixels.", "default": 1024},
			"count":           map[string]any{"type": "integer", "description": "Number of PNG variants to generate.", "default": 1},
			"seed":            map[string]any{"type": "integer", "description": "Deterministic generation seed."},
			"format":          map[string]any{"type": "string", "description": "Output format. Built-in runtime supports png."},
			"palette":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Generated local media assets and manifest metadata.",
		"properties": map[string]any{
			"kind":     map[string]any{"type": "string"},
			"action":   map[string]any{"type": "string"},
			"prompt":   map[string]any{"type": "string"},
			"style":    map[string]any{"type": "string"},
			"seed":     map[string]any{"type": "integer"},
			"assets":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"count":    map[string]any{"type": "integer"},
			"warnings": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"metadata": map[string]any{"type": "object"},
			"request":  map[string]any{"type": "object"},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-imagen-v1",
		Backends:      []string{"procedural_png", "prompt_manifest"},
		ModelProfiles: []string{"procedural_image_v1", "media_plan_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInWebFetchSkill(skill SkillManifest) SkillManifest {
	skill.Version = "0.2.0"
	skill.Summary = "Web fetch"
	skill.Description = "Fetch HTTP(S) pages, extract readable text, links, title, and metadata for agents that already know the URL."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin")
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "Known-URL web page fetch input.",
		"additionalProperties": true,
		"required":             []string{"url"},
		"properties": map[string]any{
			"url":        map[string]any{"type": "string", "description": "HTTP or HTTPS URL to fetch."},
			"timeout_ms": map[string]any{"type": "integer", "description": "Optional per-request timeout in milliseconds."},
			"max_bytes":  map[string]any{"type": "integer", "description": "Maximum response body bytes to read before extraction."},
			"user_agent": map[string]any{"type": "string", "description": "Optional user agent override."},
			"text_only":  map[string]any{"type": "boolean", "description": "Prefer readable text extraction for compatible clients."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Fetched page metadata and readable text.",
		"properties": map[string]any{
			"url":          map[string]any{"type": "string"},
			"final_url":    map[string]any{"type": "string"},
			"status_code":  map[string]any{"type": "integer"},
			"content_type": map[string]any{"type": "string"},
			"title":        map[string]any{"type": "string"},
			"text":         map[string]any{"type": "string"},
			"metadata":     map[string]any{"type": "object"},
			"links":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"bytes":        map[string]any{"type": "integer"},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-web-fetch-v1",
		Backends:      []string{"net_http"},
		ModelProfiles: []string{"readability_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInOfficeSkill(skill SkillManifest) SkillManifest {
	kind := canonicalSkillName(skill.Name)
	skill.Summary = strings.TrimSpace(skill.Summary)
	skill.Description = "Read OpenXML " + strings.ToUpper(kind) + " files, extract text, metadata, and lightweight structure with the Go standard library."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "openxml")
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "OpenXML document extraction input.",
		"additionalProperties": true,
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "Local DOCX/XLSX/PPTX file path."},
			"input":    map[string]any{"type": "string", "description": "Alias for path when JSON input is used."},
			"file":     map[string]any{"type": "string", "description": "Alias for path when JSON input is used."},
			"action":   map[string]any{"type": "string", "description": "Read action. The built-in runtime currently supports extract/read."},
			"max_rows": map[string]any{"type": "integer", "description": "Maximum rows to return per XLSX sheet."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Extracted OpenXML text and structure.",
		"properties": map[string]any{
			"kind":       map[string]any{"type": "string"},
			"source":     map[string]any{"type": "string"},
			"action":     map[string]any{"type": "string"},
			"text":       map[string]any{"type": "string"},
			"properties": map[string]any{"type": "object"},
			"parts":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"sheets":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"slides":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"warnings":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-openxml-read-v1",
		Backends:      []string{"openxml"},
		ModelProfiles: []string{kind + "_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func builtInPDFSkill(skill SkillManifest) SkillManifest {
	skill.Description = "Read text-oriented PDF files, extract text streams, estimate page count, and return OCR hints for scanned documents with the Go standard library."
	skill.Tags = appendUniqueRegistryStrings(skill.Tags, "builtin", "pdf_text")
	skill.InputSchema = map[string]any{
		"type":                 "object",
		"description":          "PDF text extraction input.",
		"additionalProperties": true,
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Local PDF file path."},
			"input":       map[string]any{"type": "string", "description": "Alias for path when JSON input is used."},
			"file":        map[string]any{"type": "string", "description": "Alias for path when JSON input is used."},
			"action":      map[string]any{"type": "string", "description": "Read action. The built-in runtime currently supports extract/read."},
			"max_streams": map[string]any{"type": "integer", "description": "Maximum number of PDF streams to inspect."},
		},
	}
	skill.OutputSchema = map[string]any{
		"type":        "object",
		"description": "Extracted PDF text and diagnostics.",
		"properties": map[string]any{
			"kind":       map[string]any{"type": "string"},
			"source":     map[string]any{"type": "string"},
			"action":     map[string]any{"type": "string"},
			"text":       map[string]any{"type": "string"},
			"page_count": map[string]any{"type": "integer"},
			"streams":    map[string]any{"type": "integer"},
			"bytes":      map[string]any{"type": "integer"},
			"metadata":   map[string]any{"type": "object"},
			"warnings":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	skill.Builtin = &BuiltinInfo{
		Runtime:       "agtx-pdf-text-v1",
		Backends:      []string{"pdf_text"},
		ModelProfiles: []string{"pdf_text_v1"},
		NoPython:      true,
	}
	skill.Stub = false
	return skill
}

func defaultSkill(name, version, summary, description string, tags, keywords, meters []string) SkillManifest {
	capabilityClass := "tool"
	if canonicalSkillName(name) == deepResearchSkillName {
		capabilityClass = "workflow"
	}
	skill := SkillManifest{
		SchemaVersion: 1,
		Name:          name,
		Version:       version,
		VendorID:      "agentex",
		Capability:    &CapabilityInfo{Class: capabilityClass, UseWhen: description},
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
	if canonicalSkillName(name) == "imagen" {
		return builtInImagenSkill(skill)
	}
	if canonicalSkillName(name) == "audio" {
		return builtInAudioSkill(skill)
	}
	if canonicalSkillName(name) == deepResearchSkillName {
		return builtInDeepResearchSkill(skill)
	}
	if canonicalSkillName(name) == "web_search" {
		return builtInWebSearchSkill(skill)
	}
	if canonicalSkillName(name) == "web_fetch" {
		return builtInWebFetchSkill(skill)
	}
	if canonicalSkillName(name) == "security_audit" {
		return builtInSecurityAuditSkill(skill)
	}
	if canonicalSkillName(name) == "docx" || canonicalSkillName(name) == "xlsx" || canonicalSkillName(name) == "pptx" {
		return builtInOfficeSkill(skill)
	}
	if canonicalSkillName(name) == "pdf" {
		return builtInPDFSkill(skill)
	}
	return skill
}

func appendUniqueRegistryStrings(values []string, more ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values)+len(more))
	for _, value := range append(append([]string{}, values...), more...) {
		if strings.TrimSpace(value) == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
	canonicalNeedle := canonicalSkillName(name)
	for _, skill := range r.Skills {
		if canonicalSkillName(skill.Name) == canonicalNeedle {
			return skill, true
		}
		for _, alias := range skillAliases(skill) {
			if normalizeName(alias) == needle {
				return skill, true
			}
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
		strings.Join(skillAliases(skill), " "),
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
		case normalizeName(term) == normalizeName(skill.Name) || canonicalSkillName(term) == canonicalSkillName(skill.Name):
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

const deepResearchSkillName = "deep_research"

func canonicalSkillName(value string) string {
	normalized := normalizeName(value)
	switch normalized {
	case "research":
		return deepResearchSkillName
	case "media", "imagegen", "image_generation", "image_generate", "t2i", "text_to_image", "t2v", "text_to_video":
		return "imagen"
	case "rapidocr", "rapid_ocr", "rapidocr_v6", "rapid_ocr_v6", "paddleocr", "paddle_ocr", "ppocr", "pp_ocr", "ppocrv6", "pp_ocrv6", "pp_ocr_v6", "ocr_v6":
		return "ocr"
	case "security", "audit", "security_scan", "security-audit", "security-scan", "skill_audit", "skill_scan", "store_audit", "supply_chain", "supply-chain":
		return "security_audit"
	default:
		return normalized
	}
}

func skillAliases(skill SkillManifest) []string {
	switch normalizeName(skill.Name) {
	case deepResearchSkillName:
		return []string{"research"}
	case "imagen":
		return []string{"media", "imagegen", "image_generation", "image_generate", "t2i", "text_to_image", "t2v", "text_to_video"}
	case "ocr":
		return []string{"rapidocr", "rapid_ocr", "rapidocr_v6", "paddleocr", "paddle_ocr", "ppocr", "pp_ocr", "ppocrv6", "pp_ocrv6", "pp_ocr_v6", "ocr_v6"}
	case "security_audit":
		return []string{"security", "audit", "security_scan", "security-audit", "security-scan", "skill_audit", "skill_scan", "store_audit", "supply_chain", "supply-chain"}
	default:
		return nil
	}
}

func normalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}
