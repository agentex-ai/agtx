package core

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultWebFetchMaxBytes int64 = 4 * 1024 * 1024
const defaultWebFetchUserAgent = "agtx-web-fetch/1.0"

var (
	webFetchDropPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`),
		regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`),
		regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</noscript>`),
		regexp.MustCompile(`(?is)<svg\b[^>]*>.*?</svg>`),
		regexp.MustCompile(`(?is)<canvas\b[^>]*>.*?</canvas>`),
	}
	webFetchTitlePattern     = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	webFetchMetaPattern      = regexp.MustCompile(`(?is)<meta\b([^>]*)>`)
	webFetchLinkPattern      = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	webFetchTagPattern       = regexp.MustCompile(`(?s)<[^>]+>`)
	webFetchSpacePattern     = regexp.MustCompile(`[ \t\r\f\v]+`)
	webFetchBlankLinePattern = regexp.MustCompile(`\n{3,}`)
	webFetchAttrPattern      = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*("([^"]*)"|'([^']*)'|([^\s"'>/]+))`)
)

type builtinWebFetchInput struct {
	URL       string `json:"url"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	MaxBytes  int64  `json:"max_bytes,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	TextOnly  bool   `json:"text_only,omitempty"`
}

type builtinWebFetchOutput struct {
	URL         string                 `json:"url"`
	FinalURL    string                 `json:"final_url"`
	StatusCode  int                    `json:"status_code"`
	ContentType string                 `json:"content_type,omitempty"`
	Title       string                 `json:"title,omitempty"`
	Text        string                 `json:"text,omitempty"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
	Links       []builtinWebFetchLink  `json:"links,omitempty"`
	Bytes       int                    `json:"bytes"`
	Truncated   bool                   `json:"truncated,omitempty"`
	Warnings    []string               `json:"warnings,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
}

type builtinWebFetchLink struct {
	Href string `json:"href"`
	Text string `json:"text,omitempty"`
}

func (s *Service) runBuiltinWebFetch(ctx context.Context, manifest SkillManifest, options RunOptions) (RunResult, error) {
	input, err := parseBuiltinWebFetchInput(options)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	requestURL, err := validateWebFetchURL(input.URL)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultWebFetchMaxBytes
	}
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
		userAgent = defaultWebFetchUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.2")

	res, err := outboundHTTPClient.Do(req)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return RunResult{ExitCode: -1, TimedOut: true}, NewError(CodeTimeout, "web fetch timed out", map[string]any{"url": safeURLForDetails(requestURL), "timeout_ms": timeout.Milliseconds()})
		}
		return RunResult{ExitCode: -1}, err
	}
	defer res.Body.Close()

	body, truncated, err := readWebFetchBody(res.Body, maxBytes)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	output := extractWebFetchOutput(requestURL, res, body, truncated, input.TextOnly)
	data, err := json.Marshal(output)
	if err != nil {
		return RunResult{ExitCode: -1}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 400 {
		return RunResult{ExitCode: -1, Stdout: string(append(data, '\n'))}, NewError(CodeInvalidArgument, "web fetch returned non-success status", map[string]any{"status_code": res.StatusCode, "url": safeURLForDetails(requestURL)})
	}
	return RunResult{ExitCode: 0, Stdout: string(append(data, '\n'))}, nil
}

func parseBuiltinWebFetchInput(options RunOptions) (builtinWebFetchInput, error) {
	input := builtinWebFetchInput{
		URL:       webFetchOptionValue(options.Args, "url", ""),
		UserAgent: webFetchOptionValue(options.Args, "user-agent", ""),
		MaxBytes:  int64(webFetchOptionInt(options.Args, "max-bytes", 0)),
		TimeoutMS: webFetchOptionInt(options.Args, "timeout-ms", 0),
		TextOnly:  hasWebFetchBoolArg(options.Args, "text-only"),
	}
	if len(options.Input) > 0 {
		var payload builtinWebFetchInput
		if err := json.Unmarshal(options.Input, &payload); err == nil {
			if strings.TrimSpace(input.URL) == "" {
				input.URL = payload.URL
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
			input.TextOnly = input.TextOnly || payload.TextOnly
		} else if strings.TrimSpace(input.URL) == "" {
			input.URL = strings.TrimSpace(string(options.Input))
		}
	}
	if strings.TrimSpace(input.URL) == "" {
		input.URL = firstWebFetchURLArg(options.Args)
	}
	if strings.TrimSpace(input.URL) == "" {
		return input, NewError(CodeInvalidArgument, "web_fetch requires a URL", map[string]any{"expected": "url argument, --url, or JSON input"})
	}
	return input, nil
}

func validateWebFetchURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed != raw || strings.ContainsRune(raw, 0) {
		return "", NewError(CodeInvalidArgument, "web fetch url is invalid", map[string]any{"url": raw})
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", NewError(CodeInvalidArgument, "web fetch url must be absolute", map[string]any{"url": raw})
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return "", NewError(CodeInvalidArgument, "remote web fetch requires https", map[string]any{"url": safeURLForDetails(raw)})
		}
	default:
		return "", NewError(CodeInvalidArgument, "web fetch supports only http and https", map[string]any{"scheme": parsed.Scheme})
	}
	parsed.User = nil
	return parsed.String(), nil
}

func readWebFetchBody(reader io.Reader, limit int64) ([]byte, bool, error) {
	if limit <= 0 {
		limit = defaultWebFetchMaxBytes
	}
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func extractWebFetchOutput(requestURL string, res *http.Response, body []byte, truncated bool, textOnly bool) builtinWebFetchOutput {
	contentType := res.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	raw := string(body)
	output := builtinWebFetchOutput{
		URL:         requestURL,
		FinalURL:    res.Request.URL.String(),
		StatusCode:  res.StatusCode,
		ContentType: contentType,
		Bytes:       len(body),
		Truncated:   truncated,
		Headers:     selectedWebFetchHeaders(res.Header),
	}
	if truncated {
		output.Warnings = append(output.Warnings, "response body was truncated before extraction")
	}
	if strings.Contains(mediaType, "html") || strings.Contains(strings.ToLower(raw[:minInt(len(raw), 512)]), "<html") {
		cleaned := dropWebFetchIgnoredElements(raw)
		output.Title = extractWebFetchTitle(cleaned)
		output.Metadata = extractWebFetchMeta(cleaned)
		if !textOnly {
			output.Links = extractWebFetchLinks(cleaned, res.Request.URL)
		}
		output.Text = htmlToReadableText(cleaned)
		return output
	}
	output.Text = normalizeWebFetchText(raw)
	return output
}

func dropWebFetchIgnoredElements(raw string) string {
	for _, pattern := range webFetchDropPatterns {
		raw = pattern.ReplaceAllString(raw, " ")
	}
	return raw
}

func extractWebFetchTitle(raw string) string {
	match := webFetchTitlePattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return normalizeWebFetchText(html.UnescapeString(match[1]))
}

func extractWebFetchMeta(raw string) map[string]string {
	out := map[string]string{}
	for _, match := range webFetchMetaPattern.FindAllStringSubmatch(raw, 50) {
		if len(match) < 2 {
			continue
		}
		attrs := parseWebFetchAttrs(match[1])
		key := firstNonEmpty(attrs["name"], attrs["property"], attrs["http-equiv"])
		value := strings.TrimSpace(attrs["content"])
		if key == "" || value == "" {
			continue
		}
		out[strings.ToLower(key)] = html.UnescapeString(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractWebFetchLinks(raw string, base *url.URL) []builtinWebFetchLink {
	links := []builtinWebFetchLink{}
	seen := map[string]bool{}
	for _, match := range webFetchLinkPattern.FindAllStringSubmatch(raw, 100) {
		if len(match) < 3 {
			continue
		}
		attrs := parseWebFetchAttrs(match[1])
		href := strings.TrimSpace(attrs["href"])
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") || strings.HasPrefix(strings.ToLower(href), "mailto:") {
			continue
		}
		if parsed, err := url.Parse(href); err == nil && base != nil {
			href = base.ResolveReference(parsed).String()
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		text := normalizeWebFetchText(webFetchTagPattern.ReplaceAllString(match[2], " "))
		links = append(links, builtinWebFetchLink{Href: href, Text: text})
	}
	return links
}

func htmlToReadableText(raw string) string {
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n", "</div>", "\n", "</section>", "\n", "</article>", "\n",
		"</h1>", "\n", "</h2>", "\n", "</h3>", "\n", "</li>", "\n",
	)
	raw = replacer.Replace(raw)
	raw = webFetchTagPattern.ReplaceAllString(raw, " ")
	return normalizeWebFetchText(raw)
}

func normalizeWebFetchText(raw string) string {
	text := html.UnescapeString(raw)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(webFetchSpacePattern.ReplaceAllString(line, " "))
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(webFetchBlankLinePattern.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

func parseWebFetchAttrs(raw string) map[string]string {
	out := map[string]string{}
	for _, match := range webFetchAttrPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) < 6 {
			continue
		}
		value := firstNonEmpty(match[3], match[4], match[5])
		out[strings.ToLower(match[1])] = html.UnescapeString(value)
	}
	return out
}

func selectedWebFetchHeaders(headers http.Header) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"Content-Length", "Last-Modified", "ETag"} {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstWebFetchURLArg(args []string) string {
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
		if webFetchArgTakesValue(arg) {
			skipNext = true
			continue
		}
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			return arg
		}
	}
	return ""
}

func webFetchOptionValue(args []string, name, fallback string) string {
	dash := "--" + name
	underscore := strings.ReplaceAll(dash, "-", "_")
	key := strings.ReplaceAll(name, "-", "_") + "="
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, dash+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, dash+"="))
		}
		if strings.HasPrefix(arg, underscore+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, underscore+"="))
		}
		if strings.HasPrefix(arg, key) {
			return strings.TrimSpace(strings.TrimPrefix(arg, key))
		}
		if (arg == dash || arg == underscore) && index+1 < len(args) {
			return strings.TrimSpace(args[index+1])
		}
	}
	return fallback
}

func webFetchOptionInt(args []string, name string, fallback int) int {
	value := webFetchOptionValue(args, name, "")
	if value == "" {
		value = webFetchOptionValue(args, strings.ReplaceAll(name, "-", "_"), "")
	}
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func hasWebFetchBoolArg(args []string, name string) bool {
	dash := "--" + name
	underscore := strings.ReplaceAll(dash, "-", "_")
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == dash || arg == underscore || arg == strings.ReplaceAll(name, "-", "_") {
			return true
		}
	}
	return false
}

func webFetchArgTakesValue(arg string) bool {
	switch arg {
	case "--url", "__url", "--timeout-ms", "__timeout_ms", "--max-bytes", "__max_bytes", "--user-agent", "__user_agent":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
