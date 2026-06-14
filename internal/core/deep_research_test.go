package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunDeepResearchWithoutInstallUsesBuiltinWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/search":
			if request.URL.Query().Get("q") == "" {
				t.Fatalf("expected search query: %s", request.URL.String())
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"items":[{"title":"Agentex Capability Notes","link":"` + "http://" + request.Host + `/source-a","snippet":"Agentex capability packs can be reused by agent frameworks."},{"title":"Research Workflow Evidence","link":"` + "http://" + request.Host + `/source-b","snippet":"Research workflows should keep evidence and caveats."}]}`))
		case "/source-a":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write([]byte(`<!doctype html><html><head><title>Agentex Capability Notes</title></head><body><article><p>Agentex capability packs can be reused by agent frameworks when they expose structured sources and findings.</p><p>The implementation should preserve evidence records for normal and pro account telemetry.</p></article></body></html>`))
		case "/source-b":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = writer.Write([]byte(`<!doctype html><html><head><title>Research Workflow Evidence</title></head><body><article><p>Research workflows should keep evidence, caveats, and next actions so agents can verify conclusions.</p></article></body></html>`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"question":"How should agentex capability packs support agent frameworks?","scope":"evidence records and telemetry","search_base_url":"` + server.URL + `/search","search_provider":"fixture","max_sources":2,"max_results":2}`)
	result, err := service.RunSkill(context.Background(), "deep_research", nil, input)
	if err != nil {
		t.Fatalf("run deep_research: %v result=%#v", err, result)
	}
	if result.Name != "deep_research" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Meter != "task" {
		t.Fatalf("expected task usage event: %#v", result.UsageEvents)
	}
	var output struct {
		Kind     string `json:"kind"`
		Question string `json:"question"`
		Sources  []struct {
			Rank    int    `json:"rank"`
			Title   string `json:"title"`
			URL     string `json:"url"`
			Fetched bool   `json:"fetched"`
		} `json:"sources"`
		Findings []struct {
			Statement  string `json:"statement"`
			SourceURL  string `json:"source_url"`
			SourceRank int    `json:"source_rank"`
		} `json:"findings"`
		Report   string   `json:"report"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode deep research output: %v stdout=%s", err, result.Stdout)
	}
	if output.Kind != "deep_research" || output.Question == "" || len(output.Sources) != 2 || len(output.Findings) == 0 {
		t.Fatalf("unexpected deep research output: %#v", output)
	}
	if !output.Sources[0].Fetched || !strings.Contains(output.Sources[0].URL, "/source-a") {
		t.Fatalf("expected fetched source-a first: %#v", output.Sources)
	}
	if !strings.Contains(output.Findings[0].Statement, "Agentex capability packs") && !strings.Contains(output.Report, "evidence") {
		t.Fatalf("expected evidence in findings/report: %#v", output)
	}
	if len(output.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", output.Warnings)
	}
}

func TestRunDeepResearchSynthesizesSuppliedSourcesOffline(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"question":"What should a research pack return?","skip_search":true,"skip_fetch":true,"sources":[{"title":"Local note","url":"https://example.com/note","text":"A research pack should return structured findings, cited sources, caveats, and next actions for agent frameworks."}]}`)
	result, err := service.RunSkill(context.Background(), "research", nil, input)
	if err != nil {
		t.Fatalf("run research alias: %v result=%#v", err, result)
	}
	if result.Name != "deep_research" || result.Version != "0.2.0" || result.Stub {
		t.Fatalf("unexpected alias run result: %#v", result)
	}
	if !strings.Contains(result.Stdout, "structured findings") || !strings.Contains(result.Stdout, "Search was skipped") {
		t.Fatalf("expected offline research synthesis output: %s", result.Stdout)
	}
}
