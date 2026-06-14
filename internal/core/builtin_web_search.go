package core

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultWebSearchMaxBytes int64 = 2 * 1024 * 1024
const defaultWebSearchMaxResults = 10
const maxWebSearchResults = 50
const defaultWebSearchUserAgent = "agtx-web-search/1.0"
const defaultWebSearchProvider = "duckduckgo_html"
const defaultWebSearchBaseURL = "https://duckduckgo.com/html/"

var webSearchSnippetPattern = regexp.MustCompile(`(?is)<(?:a|div|span|p)\b[^>]*class\s*=\s*("[^"]*(?:result__snippet|result-snippet|snippet)[^"]*"|'[^']*(?:result__snippet|result-snippet|snippet)[^']*')[^>]*>(.*?)</(?:a|div|span|p)>`)

type builtinWebSearchInput struct {
	Query      string `json:"query,omitempty"`
	Q          string `json:"q,omitempty"`
	Provider   string `json:"provider,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
	TimeoutMS  int    `json:"timeout_ms,omitempty"`
	MaxBytes   int64  `json:"max_bytes,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	Region     string `json:"region,omitempty"`
	Language   string `json:"language,omitempty"`
	SafeSearch string `json:"safe_search,omitempty"`
}

type builtinWebSearchOutput struct {
	Query       string                   `json:"query"`
	Provider    string                   `json:"provider"`
	URL         string                   `json:"url"`
	FinalURL    string                   `json:"final_url"`
	StatusCode  int                      `json:"status_code"`
	ContentType string                   `json:"content_type,omitempty"`
	Results     []builtinWebSearchResult `json:"results"`
	Count       int                      `json:"count"`
	Bytes       int                      `json:"bytes"`
	Truncated   bool                     `json:"truncated,omitempty"`
	Warnings    []string                 `json:"warnings,omitempty"`
}

type builtinWebSearchResult struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Source  string `json:"source,omitempty"`
}

func (s *Service) runBuiltinWebSearch(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	input, err := parseBuiltinWebSearchInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	query := strings.TrimSpace(firstNonEmpty(input.Query, input.Q))
	if query == "" {
		return RunResult{ExitCode: -1}, NewError(CodeInvalidArgument, "web_search requires a query", map[string]any{"expected": "query argument, --query, --q, or JSON input"})
	}
	requestURL, provider, err := buildWebSearchURL(input, query)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultWebSearchMaxBytes
	}
	maxResults := clampWebSearchResults(input.MaxResults)
	timeout := options.Timeout
	if input.TimeoutMS > 0 {
		timeout = time.Duration(input.TimeoutMS) * time.Millisecond
	}
	requestCtx, cancel := contextWithDefaultTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	userAgent := strings.TrimSpace(input.UserAgent)
	if userAgent == "" {
		userAgent = defaultWebSearchUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json,text/html,application/xhtml+xml;q=0.9,*/*;q=0.2")

	res, err := outboundHTTPClient.Do(req)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "web search timed out", map[string]any{"url": safeURLForDetails(requestURL), "timeout_ms": timeout.Milliseconds()})
		}
		return RunResult{ExitCode: -1}, err
	}
	defer res.Body.Close()

	body, truncated, err := readWebFetchBody(res.Body, maxBytes)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	output := extractWebSearchOutput(query, provider, requestURL, res, body, truncated, maxResults)
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return RunResult{ExitCode: -1, Stdout: string(append(data, '\n'))}, NewError(CodeInvalidArgument, "web search returned non-success status", map[string]any{"status_code": res.StatusCode, "url": safeURLForDetails(requestURL)})
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinWebSearchInput(options RunOptions) (builtinWebSearchInput, error) {
	input := builtinWebSearchInput{
		Query:      webFetchOptionValue(options.Args, "query", ""),
		Q:          webFetchOptionValue(options.Args, "q", ""),
		Provider:   webFetchOptionValue(options.Args, "provider", ""),
		BaseURL:    webFetchOptionValue(options.Args, "base-url", ""),
		MaxResults: webFetchOptionInt(options.Args, "max-results", 0),
		TimeoutMS:  webFetchOptionInt(options.Args, "timeout-ms", 0),
		MaxBytes:   int64(webFetchOptionInt(options.Args, "max-bytes", 0)),
		UserAgent:  webFetchOptionValue(options.Args, "user-agent", ""),
		Region:     webFetchOptionValue(options.Args, "region", ""),
		Language:   webFetchOptionValue(options.Args, "language", ""),
		SafeSearch: webFetchOptionValue(options.Args, "safe-search", ""),
	}
	if input.BaseURL == "" {
		input.BaseURL = webFetchOptionValue(options.Args, "base_url", "")
	}
	if input.MaxResults <= 0 {
		input.MaxResults = webFetchOptionInt(options.Args, "max_results", 0)
	}
	if input.TimeoutMS <= 0 {
		input.TimeoutMS = webFetchOptionInt(options.Args, "timeout_ms", 0)
	}
	if input.MaxBytes <= 0 {
		input.MaxBytes = int64(webFetchOptionInt(options.Args, "max_bytes", 0))
	}
	if len(options.Input) > 0 {
		var payload builtinWebSearchInput
		if err := json.Unmarshal(options.Input, &payload); err == nil && webSearchPayloadHasValues(payload) {
			mergeWebSearchInput(&input, payload)
		} else if strings.TrimSpace(firstNonEmpty(input.Query, input.Q)) == "" {
			input.Query = strings.TrimSpace(string(options.Input))
		}
	}
	if strings.TrimSpace(firstNonEmpty(input.Query, input.Q)) == "" {
		input.Query = firstWebSearchQueryArg(options.Args)
	}
	return input, nil
}

func webSearchPayloadHasValues(input builtinWebSearchInput) bool {
	return strings.TrimSpace(firstNonEmpty(input.Query, input.Q, input.Provider, input.BaseURL, input.UserAgent, input.Region, input.Language, input.SafeSearch)) != "" || input.MaxResults > 0 || input.TimeoutMS > 0 || input.MaxBytes > 0
}

func mergeWebSearchInput(input *builtinWebSearchInput, payload builtinWebSearchInput) {
	if strings.TrimSpace(firstNonEmpty(input.Query, input.Q)) == "" {
		input.Query = firstNonEmpty(payload.Query, payload.Q)
	}
	if strings.TrimSpace(input.Provider) == "" {
		input.Provider = payload.Provider
	}
	if strings.TrimSpace(input.BaseURL) == "" {
		input.BaseURL = payload.BaseURL
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
	if strings.TrimSpace(input.UserAgent) == "" {
		input.UserAgent = payload.UserAgent
	}
	if strings.TrimSpace(input.Region) == "" {
		input.Region = payload.Region
	}
	if strings.TrimSpace(input.Language) == "" {
		input.Language = payload.Language
	}
	if strings.TrimSpace(input.SafeSearch) == "" {
		input.SafeSearch = payload.SafeSearch
	}
}

func buildWebSearchURL(input builtinWebSearchInput, query string) (string, string, error) {
	provider := strings.TrimSpace(input.Provider)
	baseURL := strings.TrimSpace(input.BaseURL)
	if provider == "" {
		if baseURL == "" {
			provider = defaultWebSearchProvider
		} else {
			provider = "custom_http"
		}
	}
	if baseURL == "" {
		baseURL = defaultWebSearchBaseURL
	}
	parsed, err := validateWebSearchURL(baseURL)
	if err != nil {
		return "", "", err
	}
	values := parsed.Query()
	values.Set("q", query)
	if maxResults := clampWebSearchResults(input.MaxResults); maxResults > 0 {
		values.Set("count", strconv.Itoa(maxResults))
	}
	if region := strings.TrimSpace(input.Region); region != "" {
		values.Set("kl", region)
	}
	if language := strings.TrimSpace(input.Language); language != "" {
		values.Set("hl", language)
	}
	if safe := strings.TrimSpace(input.SafeSearch); safe != "" {
		values.Set("safe", safe)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), provider, nil
}

func validateWebSearchURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != raw || strings.ContainsRune(raw, 0) {
		return nil, NewError(CodeInvalidArgument, "web search base_url is invalid", map[string]any{"url": raw})
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, NewError(CodeInvalidArgument, "web search base_url must be absolute", map[string]any{"url": raw})
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return nil, NewError(CodeInvalidArgument, "remote web search requires https", map[string]any{"url": safeURLForDetails(raw)})
		}
	default:
		return nil, NewError(CodeInvalidArgument, "web search supports only http and https", map[string]any{"scheme": parsed.Scheme})
	}
	parsed.User = nil
	return parsed, nil
}

func extractWebSearchOutput(query, provider, requestURL string, res *http.Response, body []byte, truncated bool, maxResults int) builtinWebSearchOutput {
	contentType := res.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	warnings := []string{}
	if truncated {
		warnings = append(warnings, "response body was truncated before search result extraction")
	}
	results, parsedJSON := extractWebSearchResults(body, mediaType, res.Request.URL, maxResults)
	if !parsedJSON && len(results) == 0 {
		warnings = append(warnings, "no search results were extracted from the response")
	}
	return builtinWebSearchOutput{
		Query:       query,
		Provider:    provider,
		URL:         requestURL,
		FinalURL:    res.Request.URL.String(),
		StatusCode:  res.StatusCode,
		ContentType: contentType,
		Results:     results,
		Count:       len(results),
		Bytes:       len(body),
		Truncated:   truncated,
		Warnings:    warnings,
	}
}

func extractWebSearchResults(body []byte, mediaType string, base *url.URL, maxResults int) ([]builtinWebSearchResult, bool) {
	trimmed := bytes.TrimSpace(body)
	if strings.Contains(mediaType, "json") || len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if results, ok := parseWebSearchJSONResults(trimmed, maxResults); ok {
			return results, true
		}
	}
	return parseWebSearchHTMLResults(string(body), base, maxResults), false
}

func parseWebSearchJSONResults(data []byte, maxResults int) ([]builtinWebSearchResult, bool) {
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	items, ok := webSearchJSONItems(payload)
	if !ok {
		return nil, false
	}
	return webSearchResultsFromItems(items, nil, maxResults), true
}

func webSearchJSONItems(payload any) ([]any, bool) {
	switch value := payload.(type) {
	case []any:
		return value, true
	case map[string]any:
		for _, key := range []string{"results", "items", "organic_results"} {
			if items, ok := value[key].([]any); ok {
				return items, true
			}
		}
		if webPages, ok := value["webPages"].(map[string]any); ok {
			if items, ok := webPages["value"].([]any); ok {
				return items, true
			}
		}
		if data, ok := value["data"]; ok {
			return webSearchJSONItems(data)
		}
	}
	return nil, false
}

func webSearchResultsFromItems(items []any, base *url.URL, maxResults int) []builtinWebSearchResult {
	seen := map[string]bool{}
	results := []builtinWebSearchResult{}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title := webSearchStringField(object, "title", "name")
		rawURL := webSearchStringField(object, "url", "link", "href")
		resultURL := normalizeWebSearchResultURL(rawURL, base)
		if resultURL == "" || seen[resultURL] {
			continue
		}
		if title == "" {
			title = resultURL
		}
		seen[resultURL] = true
		results = append(results, builtinWebSearchResult{
			Rank:    len(results) + 1,
			Title:   title,
			URL:     resultURL,
			Snippet: webSearchStringField(object, "snippet", "description", "content", "summary"),
			Source:  webSearchSource(resultURL),
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results
}

func parseWebSearchHTMLResults(raw string, base *url.URL, maxResults int) []builtinWebSearchResult {
	results := parseWebSearchAnchors(raw, base, maxResults, true)
	if len(results) > 0 {
		return results
	}
	return parseWebSearchAnchors(raw, base, maxResults, false)
}

func parseWebSearchAnchors(raw string, base *url.URL, maxResults int, resultOnly bool) []builtinWebSearchResult {
	matches := webFetchLinkPattern.FindAllStringSubmatchIndex(raw, -1)
	seen := map[string]bool{}
	results := []builtinWebSearchResult{}
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		attrs := parseWebFetchAttrs(raw[match[2]:match[3]])
		if resultOnly && !isWebSearchResultAnchor(attrs) {
			continue
		}
		resultURL := normalizeWebSearchResultURL(attrs["href"], base)
		if resultURL == "" || seen[resultURL] {
			continue
		}
		title := normalizeWebFetchText(webFetchTagPattern.ReplaceAllString(raw[match[4]:match[5]], " "))
		if title == "" || strings.EqualFold(title, resultURL) && resultOnly {
			continue
		}
		seen[resultURL] = true
		results = append(results, builtinWebSearchResult{
			Rank:    len(results) + 1,
			Title:   title,
			URL:     resultURL,
			Snippet: webSearchSnippetAfter(raw, match[1]),
			Source:  webSearchSource(resultURL),
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results
}

func isWebSearchResultAnchor(attrs map[string]string) bool {
	className := strings.ToLower(attrs["class"])
	if strings.Contains(className, "result__a") || strings.Contains(className, "result-title") || strings.Contains(className, "result_title") || strings.Contains(className, "search-result") {
		return true
	}
	dataTestID := strings.ToLower(firstNonEmpty(attrs["data-testid"], attrs["data-test-id"]))
	return strings.Contains(dataTestID, "result")
}

func webSearchSnippetAfter(raw string, offset int) string {
	if offset < 0 || offset >= len(raw) {
		return ""
	}
	end := minWebSearchInt(len(raw), offset+2000)
	match := webSearchSnippetPattern.FindStringSubmatch(raw[offset:end])
	if len(match) < 3 {
		return ""
	}
	return normalizeWebFetchText(webFetchTagPattern.ReplaceAllString(match[2], " "))
}

func normalizeWebSearchResultURL(raw string, base *url.URL) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") || strings.HasPrefix(strings.ToLower(raw), "mailto:") || strings.HasPrefix(raw, "#") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if redirect := webSearchRedirectTarget(parsed); redirect != "" {
		parsed, err = url.Parse(redirect)
		if err != nil {
			return ""
		}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.Fragment = ""
	return parsed.String()
}

func webSearchRedirectTarget(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	values := parsed.Query()
	for _, key := range []string{"uddg", "q", "url", "u"} {
		value := strings.TrimSpace(values.Get(key))
		if value == "" {
			continue
		}
		candidate, err := url.Parse(value)
		if err == nil && (candidate.Scheme == "http" || candidate.Scheme == "https") && candidate.Host != "" {
			if strings.Contains(host, "duckduckgo.") || strings.Contains(host, "google.") || strings.Contains(host, "bing.") || strings.Contains(host, "search.") {
				return candidate.String()
			}
		}
	}
	return ""
}

func webSearchStringField(object map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := object[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if text := strings.TrimSpace(html.UnescapeString(typed)); text != "" {
				return text
			}
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func webSearchSource(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}

func firstWebSearchQueryArg(args []string) string {
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
		if webSearchArgTakesValue(arg) {
			skipNext = !strings.Contains(arg, "=")
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		values = append(values, arg)
	}
	return strings.Join(values, " ")
}

func webSearchArgTakesValue(arg string) bool {
	if webFetchArgTakesValue(arg) {
		return true
	}
	switch arg {
	case "--query", "__query", "--q", "__q", "--provider", "__provider", "--base-url", "__base_url", "--base_url", "--max-results", "__max_results", "--max_results", "--region", "__region", "--language", "__language", "--safe-search", "__safe_search", "--safe_search":
		return true
	default:
		return false
	}
}

func clampWebSearchResults(value int) int {
	if value <= 0 {
		return defaultWebSearchMaxResults
	}
	if value > maxWebSearchResults {
		return maxWebSearchResults
	}
	return value
}

func minWebSearchInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
