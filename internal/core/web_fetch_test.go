package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWebFetchWithoutInstallUsesBuiltinRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte(`<!doctype html>
<html>
<head>
  <title>Example Page</title>
  <meta name="description" content="A small fixture">
  <style>.hidden{display:none}</style>
  <script>window.nope = true</script>
</head>
<body>
  <article>
    <h1>Example Heading</h1>
    <p>Hello <strong>agent</strong> world.</p>
    <a href="/next">Next page</a>
  </article>
</body>
</html>`))
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "web_fetch", []string{server.URL + "/page"}, nil)
	if err != nil {
		t.Fatalf("run web_fetch: %v result=%#v", err, result)
	}
	if result.Name != "web_fetch" || result.Version != "0.2.0" || result.Stub || result.ExitCode != 0 {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 2 {
		t.Fatalf("expected page and call usage events: %#v", result.UsageEvents)
	}
	var output struct {
		URL        string            `json:"url"`
		FinalURL   string            `json:"final_url"`
		StatusCode int               `json:"status_code"`
		Title      string            `json:"title"`
		Text       string            `json:"text"`
		Metadata   map[string]string `json:"metadata"`
		Links      []struct {
			Href string `json:"href"`
			Text string `json:"text"`
		} `json:"links"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &output); err != nil {
		t.Fatalf("decode web fetch output: %v stdout=%s", err, result.Stdout)
	}
	if output.URL != server.URL+"/page" || output.StatusCode != 200 || output.Title != "Example Page" {
		t.Fatalf("unexpected output metadata: %#v", output)
	}
	if !strings.Contains(output.Text, "Example Heading") || !strings.Contains(output.Text, "Hello agent world.") || strings.Contains(output.Text, "window.nope") {
		t.Fatalf("unexpected readable text: %q", output.Text)
	}
	if output.Metadata["description"] != "A small fixture" {
		t.Fatalf("expected meta description: %#v", output.Metadata)
	}
	if len(output.Links) != 1 || output.Links[0].Href != server.URL+"/next" || output.Links[0].Text != "Next page" {
		t.Fatalf("unexpected links: %#v", output.Links)
	}
}

func TestRunWebFetchAcceptsJSONInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("plain text body"))
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	input := []byte(`{"url":"` + server.URL + `/plain","text_only":true}`)
	result, err := service.RunSkill(context.Background(), "web_fetch", nil, input)
	if err != nil {
		t.Fatalf("run web_fetch with json input: %v result=%#v", err, result)
	}
	if !strings.Contains(result.Stdout, "plain text body") {
		t.Fatalf("expected text body in output: %s", result.Stdout)
	}
}

func TestRunWebFetchRejectsRemoteHTTP(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.RunSkill(context.Background(), "web_fetch", []string{"http://example.com/page"}, nil)
	if !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid argument for remote http, got %v result=%#v", err, result)
	}
}
