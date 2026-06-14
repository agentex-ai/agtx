package core

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const defaultDeepResearchMaxSources = 5
const maxDeepResearchSources = 20
const defaultDeepResearchMaxQueries = 3
const defaultDeepResearchFetchMaxBytes int64 = 512 * 1024

type builtinDeepResearchInput struct {
	Question       string                           `json:"question,omitempty"`
	Query          string                           `json:"query,omitempty"`
	Topic          string                           `json:"topic,omitempty"`
	Scope          string                           `json:"scope,omitempty"`
	Depth          string                           `json:"depth,omitempty"`
	OutputFormat   string                           `json:"output_format,omitempty"`
	SearchQueries  []string                         `json:"search_queries,omitempty"`
	URLs           []string                         `json:"urls,omitempty"`
	Sources        []builtinDeepResearchSourceInput `json:"sources,omitempty"`
	MaxSources     int                              `json:"max_sources,omitempty"`
	MaxResults     int                              `json:"max_results,omitempty"`
	TimeoutMS      int                              `json:"timeout_ms,omitempty"`
	MaxBytes       int64                            `json:"max_bytes,omitempty"`
	SearchBaseURL  string                           `json:"search_base_url,omitempty"`
	SearchProvider string                           `json:"search_provider,omitempty"`
	UserAgent      string                           `json:"user_agent,omitempty"`
	SkipSearch     bool                             `json:"skip_search,omitempty"`
	SkipFetch      bool                             `json:"skip_fetch,omitempty"`
}

type builtinDeepResearchSourceInput struct {
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Text    string `json:"text,omitempty"`
	Content string `json:"content,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

type builtinDeepResearchOutput struct {
	Kind        string                       `json:"kind"`
	Question    string                       `json:"question"`
	Scope       string                       `json:"scope,omitempty"`
	Depth       string                       `json:"depth,omitempty"`
	Queries     []string                     `json:"queries"`
	Sources     []builtinDeepResearchSource  `json:"sources"`
	Findings    []builtinDeepResearchFinding `json:"findings"`
	Caveats     []string                     `json:"caveats,omitempty"`
	NextActions []string                     `json:"next_actions,omitempty"`
	Report      string                       `json:"report"`
	Warnings    []string                     `json:"warnings,omitempty"`
	Metadata    map[string]any               `json:"metadata,omitempty"`
}

type builtinDeepResearchSource struct {
	Rank       int    `json:"rank"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url"`
	Snippet    string `json:"snippet,omitempty"`
	Source     string `json:"source,omitempty"`
	Excerpt    string `json:"excerpt,omitempty"`
	Fetched    bool   `json:"fetched,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	Text       string `json:"-"`
}

type builtinDeepResearchFinding struct {
	Statement   string `json:"statement"`
	SourceRank  int    `json:"source_rank"`
	SourceTitle string `json:"source_title,omitempty"`
	SourceURL   string `json:"source_url"`
	Score       int    `json:"score,omitempty"`
}

type deepResearchCandidateFinding struct {
	Finding builtinDeepResearchFinding
	Index   int
}

func (s *Service) runBuiltinDeepResearch(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	input, err := parseBuiltinDeepResearchInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	question := strings.TrimSpace(firstNonEmpty(input.Question, input.Query, input.Topic))
	if question == "" {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "deep_research requires a question", map[string]any{"expected": "question argument, --question, --query, --topic, or JSON input"})
	}
	maxSources := clampDeepResearchSources(input.MaxSources)
	maxResults := input.MaxResults
	if maxResults <= 0 {
		maxResults = maxSources
	}
	queries := buildDeepResearchQueries(question, input.Scope, input.SearchQueries)
	sources, warnings := seedDeepResearchSources(input)

	if !input.SkipSearch {
		searched, searchWarnings := s.searchDeepResearchSources(ctx, input, options, queries, maxResults)
		sources = append(sources, searched...)
		warnings = append(warnings, searchWarnings...)
	}
	sources = dedupeDeepResearchSources(sources, maxSources)
	if !input.SkipFetch {
		fetchWarnings := s.fetchDeepResearchSources(ctx, input, options, sources)
		warnings = append(warnings, fetchWarnings...)
	}
	findings := buildDeepResearchFindings(question, input.Scope, sources, maxSources*2)
	caveats := buildDeepResearchCaveats(input, sources, findings)
	nextActions := buildDeepResearchNextActions(input, sources, findings)
	report := buildDeepResearchReport(question, input, queries, sources, findings, caveats, nextActions)
	output := builtinDeepResearchOutput{
		Kind:        "deep_research",
		Question:    question,
		Scope:       strings.TrimSpace(input.Scope),
		Depth:       strings.TrimSpace(input.Depth),
		Queries:     queries,
		Sources:     sources,
		Findings:    findings,
		Caveats:     caveats,
		NextActions: nextActions,
		Report:      report,
		Warnings:    warnings,
		Metadata: map[string]any{
			"method":      "search_fetch_extractive",
			"no_python":   true,
			"max_sources": maxSources,
		},
	}
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinDeepResearchInput(options RunOptions) (builtinDeepResearchInput, error) {
	input := builtinDeepResearchInput{
		Question:       webFetchOptionValue(options.Args, "question", ""),
		Query:          webFetchOptionValue(options.Args, "query", ""),
		Topic:          webFetchOptionValue(options.Args, "topic", ""),
		Scope:          webFetchOptionValue(options.Args, "scope", ""),
		Depth:          webFetchOptionValue(options.Args, "depth", ""),
		OutputFormat:   webFetchOptionValue(options.Args, "output-format", ""),
		SearchBaseURL:  webFetchOptionValue(options.Args, "search-base-url", ""),
		SearchProvider: webFetchOptionValue(options.Args, "search-provider", ""),
		UserAgent:      webFetchOptionValue(options.Args, "user-agent", ""),
		MaxSources:     webFetchOptionInt(options.Args, "max-sources", 0),
		MaxResults:     webFetchOptionInt(options.Args, "max-results", 0),
		TimeoutMS:      webFetchOptionInt(options.Args, "timeout-ms", 0),
		MaxBytes:       int64(webFetchOptionInt(options.Args, "max-bytes", 0)),
		SkipSearch:     hasWebFetchBoolArg(options.Args, "skip-search"),
		SkipFetch:      hasWebFetchBoolArg(options.Args, "skip-fetch"),
		URLs:           deepResearchOptionValues(options.Args, "url"),
	}
	if input.SearchBaseURL == "" {
		input.SearchBaseURL = webFetchOptionValue(options.Args, "search_base_url", "")
	}
	if input.SearchProvider == "" {
		input.SearchProvider = webFetchOptionValue(options.Args, "search_provider", "")
	}
	if input.MaxSources <= 0 {
		input.MaxSources = webFetchOptionInt(options.Args, "max_sources", 0)
	}
	if input.MaxResults <= 0 {
		input.MaxResults = webFetchOptionInt(options.Args, "max_results", 0)
	}
	input.SearchQueries = deepResearchOptionValues(options.Args, "search-query")
	if len(input.SearchQueries) == 0 {
		input.SearchQueries = deepResearchOptionValues(options.Args, "search_query")
	}
	if len(options.Input) > 0 {
		var payload builtinDeepResearchInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && deepResearchPayloadHasValues(payload) {
			mergeDeepResearchInput(&input, payload)
		} else if strings.TrimSpace(firstNonEmpty(input.Question, input.Query, input.Topic)) == "" {
			input.Question = strings.TrimSpace(string(options.Input))
		}
	}
	if strings.TrimSpace(firstNonEmpty(input.Question, input.Query, input.Topic)) == "" {
		input.Question = firstDeepResearchQuestionArg(options.Args)
	}
	for _, value := range deepResearchPositionalURLs(options.Args) {
		input.URLs = append(input.URLs, value)
	}
	return input, nil
}

func deepResearchPayloadHasValues(input builtinDeepResearchInput) bool {
	return strings.TrimSpace(firstNonEmpty(input.Question, input.Query, input.Topic, input.Scope, input.Depth, input.OutputFormat, input.SearchBaseURL, input.SearchProvider, input.UserAgent)) != "" || len(input.SearchQueries) > 0 || len(input.URLs) > 0 || len(input.Sources) > 0 || input.MaxSources > 0 || input.MaxResults > 0 || input.TimeoutMS > 0 || input.MaxBytes > 0 || input.SkipSearch || input.SkipFetch
}

func mergeDeepResearchInput(input *builtinDeepResearchInput, payload builtinDeepResearchInput) {
	if strings.TrimSpace(firstNonEmpty(input.Question, input.Query, input.Topic)) == "" {
		input.Question = firstNonEmpty(payload.Question, payload.Query, payload.Topic)
	}
	if strings.TrimSpace(input.Scope) == "" {
		input.Scope = payload.Scope
	}
	if strings.TrimSpace(input.Depth) == "" {
		input.Depth = payload.Depth
	}
	if strings.TrimSpace(input.OutputFormat) == "" {
		input.OutputFormat = payload.OutputFormat
	}
	if len(input.SearchQueries) == 0 {
		input.SearchQueries = append(input.SearchQueries, payload.SearchQueries...)
	}
	input.URLs = append(input.URLs, payload.URLs...)
	input.Sources = append(input.Sources, payload.Sources...)
	if input.MaxSources <= 0 {
		input.MaxSources = payload.MaxSources
	}
	if input.MaxResults <= 0 {
		input.MaxResults = payload.MaxResults
	}
	if input.TimeoutMS <= 0 {
		input.TimeoutMS = payload.TimeoutMS
	}
	if input.MaxBytes <= 0 {
		input.MaxBytes = payload.MaxBytes
	}
	if strings.TrimSpace(input.SearchBaseURL) == "" {
		input.SearchBaseURL = payload.SearchBaseURL
	}
	if strings.TrimSpace(input.SearchProvider) == "" {
		input.SearchProvider = payload.SearchProvider
	}
	if strings.TrimSpace(input.UserAgent) == "" {
		input.UserAgent = payload.UserAgent
	}
	input.SkipSearch = input.SkipSearch || payload.SkipSearch
	input.SkipFetch = input.SkipFetch || payload.SkipFetch
}

func buildDeepResearchQueries(question, scope string, configured []string) []string {
	seen := map[string]bool{}
	appendQuery := func(value string, out *[]string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			return
		}
		seen[strings.ToLower(value)] = true
		*out = append(*out, value)
	}
	out := []string{}
	for _, query := range configured {
		appendQuery(query, &out)
	}
	if len(out) == 0 {
		appendQuery(question, &out)
		if strings.TrimSpace(scope) != "" {
			appendQuery(question+" "+scope, &out)
		}
		appendQuery(question+" evidence", &out)
	}
	if len(out) > defaultDeepResearchMaxQueries {
		return out[:defaultDeepResearchMaxQueries]
	}
	return out
}

func seedDeepResearchSources(input builtinDeepResearchInput) ([]builtinDeepResearchSource, []string) {
	warnings := []string{}
	sources := []builtinDeepResearchSource{}
	for _, source := range input.Sources {
		text := strings.TrimSpace(firstNonEmpty(source.Text, source.Content, source.Excerpt))
		item := builtinDeepResearchSource{
			Rank:    len(sources) + 1,
			Title:   strings.TrimSpace(source.Title),
			URL:     strings.TrimSpace(source.URL),
			Snippet: strings.TrimSpace(source.Snippet),
			Text:    text,
		}
		if item.Snippet == "" {
			item.Snippet = firstDeepResearchExcerpt(text)
		}
		if item.URL != "" {
			item.Source = webSearchSource(item.URL)
		}
		item.Excerpt = firstNonEmpty(item.Snippet, firstDeepResearchExcerpt(text))
		if item.URL == "" && item.Title == "" && item.Text == "" {
			continue
		}
		sources = append(sources, item)
	}
	for _, rawURL := range input.URLs {
		trimmed := strings.TrimSpace(rawURL)
		if trimmed == "" {
			continue
		}
		if _, err := url.Parse(trimmed); err != nil {
			warnings = append(warnings, "skipped invalid source url: "+trimmed)
			continue
		}
		sources = append(sources, builtinDeepResearchSource{Rank: len(sources) + 1, URL: trimmed, Source: webSearchSource(trimmed)})
	}
	return sources, warnings
}

func (s *Service) searchDeepResearchSources(ctx context.Context, input builtinDeepResearchInput, options RunOptions, queries []string, maxResults int) ([]builtinDeepResearchSource, []string) {
	sources := []builtinDeepResearchSource{}
	warnings := []string{}
	for _, query := range queries {
		searchInput := builtinWebSearchInput{
			Query:      query,
			Provider:   input.SearchProvider,
			BaseURL:    input.SearchBaseURL,
			MaxResults: maxResults,
			TimeoutMS:  input.TimeoutMS,
			MaxBytes:   input.MaxBytes,
			UserAgent:  input.UserAgent,
		}
		payload, _ := json.Marshal(searchInput)
		result, err := s.runBuiltinWebSearch(ctx, SkillManifest{Name: "web_search"}, RunOptions{Input: payload, Timeout: options.Timeout})
		if err != nil {
			warnings = append(warnings, "search failed for query "+strconv.Quote(query)+": "+ErrorFrom(err).Message)
			continue
		}
		var output builtinWebSearchOutput
		if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
			warnings = append(warnings, "search output could not be decoded for query "+strconv.Quote(query))
			continue
		}
		for _, item := range output.Results {
			sources = append(sources, builtinDeepResearchSource{
				Rank:    len(sources) + 1,
				Title:   item.Title,
				URL:     item.URL,
				Snippet: item.Snippet,
				Source:  item.Source,
				Excerpt: item.Snippet,
			})
		}
	}
	return sources, warnings
}

func (s *Service) fetchDeepResearchSources(ctx context.Context, input builtinDeepResearchInput, options RunOptions, sources []builtinDeepResearchSource) []string {
	warnings := []string{}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultDeepResearchFetchMaxBytes
	}
	for index := range sources {
		if strings.TrimSpace(sources[index].URL) == "" || strings.TrimSpace(sources[index].Text) != "" {
			continue
		}
		fetchInput := builtinWebFetchInput{URL: sources[index].URL, TimeoutMS: input.TimeoutMS, MaxBytes: maxBytes, UserAgent: input.UserAgent, TextOnly: true}
		payload, _ := json.Marshal(fetchInput)
		result, err := s.runBuiltinWebFetch(ctx, SkillManifest{Name: "web_fetch"}, RunOptions{Input: payload, Timeout: options.Timeout})
		if err != nil {
			sources[index].Error = ErrorFrom(err).Message
			warnings = append(warnings, "fetch failed for "+sources[index].URL+": "+sources[index].Error)
			continue
		}
		var output builtinWebFetchOutput
		if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
			sources[index].Error = "fetch output could not be decoded"
			warnings = append(warnings, "fetch output could not be decoded for "+sources[index].URL)
			continue
		}
		if strings.TrimSpace(sources[index].Title) == "" {
			sources[index].Title = output.Title
		}
		sources[index].Fetched = true
		sources[index].StatusCode = output.StatusCode
		sources[index].Text = output.Text
		if strings.TrimSpace(sources[index].Snippet) == "" {
			sources[index].Snippet = firstDeepResearchExcerpt(output.Text)
		}
		sources[index].Excerpt = firstNonEmpty(sources[index].Snippet, firstDeepResearchExcerpt(output.Text))
	}
	return warnings
}

func dedupeDeepResearchSources(values []builtinDeepResearchSource, limit int) []builtinDeepResearchSource {
	seen := map[string]bool{}
	out := []builtinDeepResearchSource{}
	for _, source := range values {
		key := strings.ToLower(strings.TrimSpace(source.URL))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(source.Title + " " + source.Snippet))
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		source.Rank = len(out) + 1
		if source.Source == "" && source.URL != "" {
			source.Source = webSearchSource(source.URL)
		}
		if source.Excerpt == "" {
			source.Excerpt = firstNonEmpty(source.Snippet, firstDeepResearchExcerpt(source.Text))
		}
		out = append(out, source)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildDeepResearchFindings(question, scope string, sources []builtinDeepResearchSource, limit int) []builtinDeepResearchFinding {
	tokens := deepResearchTokens(question + " " + scope)
	candidates := []deepResearchCandidateFinding{}
	for _, source := range sources {
		segments := deepResearchSegments(firstNonEmpty(source.Text, source.Snippet, source.Excerpt, source.Title))
		for index, segment := range segments {
			score := scoreDeepResearchSegment(segment, tokens)
			if score == 0 && index > 2 {
				continue
			}
			if score == 0 {
				score = 1
			}
			candidates = append(candidates, deepResearchCandidateFinding{
				Finding: builtinDeepResearchFinding{
					Statement:   segment,
					SourceRank:  source.Rank,
					SourceTitle: source.Title,
					SourceURL:   source.URL,
					Score:       score,
				},
				Index: index,
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Finding.Score != candidates[j].Finding.Score {
			return candidates[i].Finding.Score > candidates[j].Finding.Score
		}
		if candidates[i].Finding.SourceRank != candidates[j].Finding.SourceRank {
			return candidates[i].Finding.SourceRank < candidates[j].Finding.SourceRank
		}
		return candidates[i].Index < candidates[j].Index
	})
	seen := map[string]bool{}
	findings := []builtinDeepResearchFinding{}
	for _, candidate := range candidates {
		key := strings.ToLower(candidate.Finding.Statement)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		findings = append(findings, candidate.Finding)
		if len(findings) >= limit {
			break
		}
	}
	return findings
}

func buildDeepResearchCaveats(input builtinDeepResearchInput, sources []builtinDeepResearchSource, findings []builtinDeepResearchFinding) []string {
	caveats := []string{"This built-in workflow is extractive: it summarizes fetched text and snippets without external model reasoning."}
	if input.SkipSearch {
		caveats = append(caveats, "Search was skipped by request; coverage is limited to supplied sources and URLs.")
	}
	if input.SkipFetch {
		caveats = append(caveats, "Fetch was skipped by request; findings rely on supplied text and search snippets.")
	}
	if len(sources) == 0 {
		caveats = append(caveats, "No sources were collected.")
	}
	fetched := 0
	for _, source := range sources {
		if source.Fetched || source.Text != "" {
			fetched++
		}
	}
	if len(sources) > 0 && fetched == 0 {
		caveats = append(caveats, "No source pages were fetched; use search_base_url, urls, or supplied source text for stronger evidence.")
	}
	if len(findings) == 0 {
		caveats = append(caveats, "No relevant findings were extracted from the available evidence.")
	}
	return caveats
}

func buildDeepResearchNextActions(input builtinDeepResearchInput, sources []builtinDeepResearchSource, findings []builtinDeepResearchFinding) []string {
	actions := []string{}
	if len(sources) < clampDeepResearchSources(input.MaxSources) {
		actions = append(actions, "Add more primary or authoritative sources for broader coverage.")
	}
	if len(findings) < 3 {
		actions = append(actions, "Fetch additional source pages or provide source text to improve extracted findings.")
	}
	actions = append(actions, "Review the cited source URLs before using the report in high-stakes decisions.")
	return actions
}

func buildDeepResearchReport(question string, input builtinDeepResearchInput, queries []string, sources []builtinDeepResearchSource, findings []builtinDeepResearchFinding, caveats, nextActions []string) string {
	var builder strings.Builder
	builder.WriteString("# Research Brief\n\n")
	builder.WriteString("Question: ")
	builder.WriteString(question)
	builder.WriteString("\n")
	if strings.TrimSpace(input.Scope) != "" {
		builder.WriteString("Scope: ")
		builder.WriteString(strings.TrimSpace(input.Scope))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(input.Depth) != "" {
		builder.WriteString("Depth: ")
		builder.WriteString(strings.TrimSpace(input.Depth))
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Queries\n")
	for _, query := range queries {
		builder.WriteString("- ")
		builder.WriteString(query)
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Findings\n")
	if len(findings) == 0 {
		builder.WriteString("- No findings extracted.\n")
	} else {
		for _, finding := range findings {
			builder.WriteString("- ")
			builder.WriteString(finding.Statement)
			builder.WriteString(" [source ")
			builder.WriteString(strconv.Itoa(finding.SourceRank))
			builder.WriteString("]\n")
		}
	}
	builder.WriteString("\n## Sources\n")
	if len(sources) == 0 {
		builder.WriteString("- No sources collected.\n")
	} else {
		for _, source := range sources {
			builder.WriteString("- [")
			builder.WriteString(strconv.Itoa(source.Rank))
			builder.WriteString("] ")
			builder.WriteString(firstNonEmpty(source.Title, source.Source, source.URL))
			if source.URL != "" {
				builder.WriteString(" - ")
				builder.WriteString(source.URL)
			}
			builder.WriteString("\n")
		}
	}
	if len(caveats) > 0 {
		builder.WriteString("\n## Caveats\n")
		for _, caveat := range caveats {
			builder.WriteString("- ")
			builder.WriteString(caveat)
			builder.WriteString("\n")
		}
	}
	if len(nextActions) > 0 {
		builder.WriteString("\n## Next Actions\n")
		for _, action := range nextActions {
			builder.WriteString("- ")
			builder.WriteString(action)
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

func deepResearchTokens(value string) []string {
	seen := map[string]bool{}
	tokens := []string{}
	for _, token := range tokenize(value) {
		token = strings.ToLower(strings.TrimSpace(token))
		if len(token) < 3 || ignoredSearchTerm(token) || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	return tokens
}

func deepResearchSegments(text string) []string {
	replacer := strings.NewReplacer("\r\n", "\n", "\r", "\n", ". ", ".\n", "? ", "?\n", "! ", "!\n")
	text = replacer.Replace(text)
	parts := strings.Split(text, "\n")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(webFetchSpacePattern.ReplaceAllString(part, " "))
		part = strings.Trim(part, " -\\t")
		if len(part) < 20 {
			continue
		}
		if len(part) > 420 {
			part = strings.TrimSpace(part[:420])
		}
		out = append(out, part)
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func scoreDeepResearchSegment(segment string, tokens []string) int {
	lower := strings.ToLower(segment)
	score := 0
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			score += 2
		}
	}
	if strings.Contains(lower, "according to") || strings.Contains(lower, "reported") || strings.Contains(lower, "study") || strings.Contains(lower, "evidence") {
		score++
	}
	return score
}

func firstDeepResearchExcerpt(text string) string {
	segments := deepResearchSegments(text)
	if len(segments) == 0 {
		return ""
	}
	return segments[0]
}

func deepResearchOptionValues(args []string, name string) []string {
	dash := "--" + name
	underscore := strings.ReplaceAll(dash, "-", "_")
	key := strings.ReplaceAll(name, "-", "_") + "="
	values := []string{}
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, dash+"=") {
			values = append(values, strings.TrimSpace(strings.TrimPrefix(arg, dash+"=")))
			continue
		}
		if strings.HasPrefix(arg, underscore+"=") {
			values = append(values, strings.TrimSpace(strings.TrimPrefix(arg, underscore+"=")))
			continue
		}
		if strings.HasPrefix(arg, key) {
			values = append(values, strings.TrimSpace(strings.TrimPrefix(arg, key)))
			continue
		}
		if (arg == dash || arg == underscore) && index+1 < len(args) {
			values = append(values, strings.TrimSpace(args[index+1]))
		}
	}
	return values
}

func deepResearchPositionalURLs(args []string) []string {
	values := []string{}
	skipNext := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if deepResearchArgTakesValue(arg) {
			skipNext = !strings.Contains(arg, "=")
			continue
		}
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			values = append(values, arg)
		}
	}
	return values
}

func firstDeepResearchQuestionArg(args []string) string {
	values := []string{}
	skipNext := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if deepResearchArgTakesValue(arg) {
			skipNext = !strings.Contains(arg, "=")
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			continue
		}
		values = append(values, arg)
	}
	return strings.Join(values, " ")
}

func deepResearchArgTakesValue(arg string) bool {
	if webSearchArgTakesValue(arg) {
		return true
	}
	switch arg {
	case "--question", "__question", "--topic", "__topic", "--scope", "__scope", "--depth", "__depth", "--output-format", "__output_format", "--output_format", "--search-query", "__search_query", "--search_query", "--search-base-url", "__search_base_url", "--search_base_url", "--search-provider", "__search_provider", "--search_provider", "--max-sources", "__max_sources", "--max_sources", "--url", "__url":
		return true
	default:
		return false
	}
}

func clampDeepResearchSources(value int) int {
	if value <= 0 {
		return defaultDeepResearchMaxSources
	}
	if value > maxDeepResearchSources {
		return maxDeepResearchSources
	}
	return value
}
