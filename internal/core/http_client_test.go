package core

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeURLForDetailsRedactsQueryFragmentAndUserInfo(t *testing.T) {
	got := safeURLForDetails("https://user:pass@example.com/download.zip?token=secret#frag")
	if got != "https://example.com/download.zip" {
		t.Fatalf("unexpected safe url: %s", got)
	}
}

func TestOutboundHTTPClientRejectsRemoteHTTPRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://packages.example.com/tool.zip", http.StatusFound)
	}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = outboundHTTPClient.Do(request)
	if !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid redirect error, got %v", err)
	}
	coreErr := ErrorFrom(err)
	details, ok := coreErr.Details.(map[string]any)
	if !ok || strings.Contains(details["url"].(string), "token=") {
		t.Fatalf("expected redacted redirect url details, got %#v", coreErr.Details)
	}
}

func TestOutboundHTTPClientStripsAuthOnCrossOriginRedirect(t *testing.T) {
	var gotAuth string
	var gotDevice string
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		_, _ = writer.Write([]byte("ok"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	request, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("X-AGTX-Device-ID", "device-1")
	response, err := outboundHTTPClient.Do(request)
	if err != nil {
		t.Fatalf("redirect request failed: %v", err)
	}
	defer response.Body.Close()
	if gotAuth != "" || gotDevice != "" {
		t.Fatalf("expected auth stripped on cross-origin redirect, got auth=%q device=%q", gotAuth, gotDevice)
	}
}
