package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWebSearchWithoutInstallUsesBuiltinRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/search" || request.URL.Query().Get("q") != "agentex pdf" || request.URL.Query().Get("count") != "2" {
			t.Fatalf("unexpected search request: %s", request.URL.String())
		}
		if !strings.Contains(request.Header.Get("User-Agent"), "agtx-web-search") {
			t.Fatalf("expected default user agent, got %q", request.Header.Get("User-Agent"))
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(`<!doctype html>
<html><body>
  <div class="result">
    <a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Falpha%3Fref%3D1">Alpha Result</a>
    <div class="result__snippet">First &amp; useful snippet.</div>
  </div>
  <div class="result">
    <a class="result__a" href="https://docs.example.org/bravo">Bravo Docs</a>
    <span class="result__snippet">Second snippet.</span>
  </div>
</body></html>`))
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "web_search", []string{"--base-url", server.URL + "/search", "--query", "agentex pdf", "--max-results", "2"}, nil)
	if err != nil {
		t.Fatalf("run web_search: %v result=%#v", err, result)
	}
	if result.Name != "web_search" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Meter != "call" {
		t.Fatalf("expected call usage event: %#v", result.UsageEvents)
	}
	var output struct {
		Query    string `json:"query"`
		Provider string `json:"provider"`
		Count    int    `json:"count"`
		Results  []struct {
			Rank    int    `json:"rank"`
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Source  string `json:"source"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode web search output: %v stdout=%s", err, result.Stdout)
	}
	if output.Query != "agentex pdf" || output.Provider != "custom_http" || output.Count != 2 || len(output.Results) != 2 {
		t.Fatalf("unexpected search output: %#v", output)
	}
	if output.Results[0].Rank != 1 || output.Results[0].Title != "Alpha Result" || output.Results[0].URL != "https://example.com/alpha?ref=1" || output.Results[0].Source != "example.com" || output.Results[0].Snippet != "First & useful snippet." {
		t.Fatalf("unexpected first result: %#v", output.Results[0])
	}
	if output.Results[1].Title != "Bravo Docs" || output.Results[1].URL != "https://docs.example.org/bravo" {
		t.Fatalf("unexpected second result: %#v", output.Results[1])
	}
}

func TestRunWebSearchAcceptsJSONSearchProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("q") != "json query" {
			t.Fatalf("unexpected query: %s", request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[{"title":"JSON One","link":"https://json.example/one","snippet":"One snippet"},{"name":"JSON Two","url":"https://json.example/two","description":"Two snippet"}]}`))
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"query":"json query","base_url":"` + server.URL + `/api","provider":"fixture_json","max_results":1}`)
	result, err := service.RunSkill(context.Background(), "web_search", nil, input)
	if err != nil {
		t.Fatalf("run web_search with json input: %v result=%#v", err, result)
	}
	if !strings.Contains(result.Stdout, `"provider":"fixture_json"`) || !strings.Contains(result.Stdout, `"JSON One"`) || strings.Contains(result.Stdout, `"JSON Two"`) {
		t.Fatalf("unexpected json search output: %s", result.Stdout)
	}
}

func TestRunWebSearchRejectsRemoteHTTPBaseURL(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "web_search", []string{"--query", "agentex", "--base-url", "http://example.com/search"}, nil)
	if !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for remote http, got %v result=%#v", err, result)
	}
}
