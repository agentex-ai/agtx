package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLoadAuthRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"access_token":"x","extra":true}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if _, err := LoadAuth(path); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid auth, got %v", err)
	}
}

func TestRefreshRegistryAddsAuthOnlyForSameOrigin(t *testing.T) {
	root := t.TempDir()
	paths := PathsForRoot(root)
	var gotAuth string
	var gotDevice string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		_, _ = writer.Write([]byte(`{"schema_version":1,"skills":[]}`))
	}))
	defer server.Close()

	if err := SaveAuth(paths.AuthFile, AuthState{SchemaVersion: 1, AccessToken: "secret", DeviceID: "device-1"}); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	if _, err := RefreshRegistry(context.Background(), paths, Config{SchemaVersion: 1, RegistryURL: server.URL + "/v1/registry", ProAPIURL: server.URL, Channel: "stable", Telemetry: "off", RegistryMaxBytes: defaultRegistryMaxBytes, RegistryDownloadTimeoutMS: defaultRegistryDownloadTimeoutMS, PackageMaxBytes: defaultPackageMaxBytes, PackageDownloadTimeoutMS: defaultPackageDownloadTimeoutMS, ExtractedMaxBytes: defaultExtractedMaxBytes, ExtractedMaxFiles: defaultExtractedMaxFiles}); err != nil {
		t.Fatalf("refresh registry: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("expected auth header, got %q", gotAuth)
	}
	if gotDevice != "device-1" {
		t.Fatalf("expected device header, got %q", gotDevice)
	}
}

func TestRefreshRegistryDoesNotAddAuthForDifferentOrigin(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://packages.example.com/tool.zip", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	config := DefaultConfig()
	config.RegistryURL = "https://registry.example.com/v1/registry"
	config.ProAPIURL = "https://pro.example.com"
	attachAuthHeader(req, config, requestAuth{AccessToken: "secret"})
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no auth header, got %q", got)
	}
}

func TestProLoginAndCallbackFlow(t *testing.T) {
	var tokenRequest string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli/token":
			tokenRequest = request.Header.Get("Authorization")
			_, _ = writer.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"registry_url":"` + "http://" + request.Host + `/v1/registry","device_limit":3,"subscription":"active"}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = server.URL
	login, err := service.ProLoginStart(context.Background())
	if err != nil {
		t.Fatalf("login start: %v", err)
	}
	if !strings.Contains(login.LoginURL, "/v1/cli/login/start") || login.State == "" {
		t.Fatalf("unexpected login result: %#v", login)
	}
	callback, err := service.ProCallback(context.Background(), "agtx://pro/callback?code=abc&state="+login.State)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if callback.DeviceLimit != 3 || service.Auth.AccessToken != "access" || service.Config.RegistryURL == "" {
		t.Fatalf("unexpected callback state: callback=%#v auth=%#v config=%#v", callback, service.Auth, service.Config)
	}
	if tokenRequest != "" {
		t.Fatalf("token exchange should not send bearer auth, got %q", tokenRequest)
	}
}

func TestProStatusMapsDeviceLimitExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"error":{"code":"device_limit_exceeded","message":"too many devices"}}`))
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = server.URL
	service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device"}
	if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	if _, err := service.ProStatus(context.Background()); !IsErrorCode(err, CodeDeviceLimitExceeded) {
		t.Fatalf("expected device limit error, got %v", err)
	} else {
		coreErr := ErrorFrom(err)
		details, ok := coreErr.Details.(map[string]any)
		if !ok {
			t.Fatalf("expected structured details: %#v", coreErr.Details)
		}
		if _, ok := details["pro_setup"].(ProSetupResult); !ok {
			t.Fatalf("expected pro_setup recovery details: %#v", details)
		}
		actions, ok := details["next_actions"].([]ProSetupAction)
		if !ok || !containsSetupAction(actions, "list_devices") {
			t.Fatalf("expected list_devices recovery action: %#v", details["next_actions"])
		}
	}
}

func TestProStatusRefreshesExpiredToken(t *testing.T) {
	var gotRefresh bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/cli/token":
			gotRefresh = true
			_, _ = writer.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"device_id":"device","device_name":"Dev"}`))
		case "/v1/pro/status":
			if request.Header.Get("Authorization") != "Bearer new-access" {
				t.Fatalf("expected refreshed auth header, got %q", request.Header.Get("Authorization"))
			}
			if request.Header.Get("X-AGTX-Device-ID") != "device" {
				t.Fatalf("expected device header, got %q", request.Header.Get("X-AGTX-Device-ID"))
			}
			_, _ = writer.Write([]byte(`{"subscription":"active","plan":"pro"}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = server.URL
	service.Auth = AuthState{SchemaVersion: 1, AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: "2000-01-01T00:00:00Z", DeviceID: "device", DeviceName: "Dev"}
	if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	status, err := service.ProStatus(context.Background())
	if err != nil {
		t.Fatalf("pro status: %v", err)
	}
	if !gotRefresh || status.Subscription != "active" || service.Auth.AccessToken != "new-access" {
		t.Fatalf("expected refreshed status, got status=%#v auth=%#v refreshed=%t", status, service.Auth, gotRefresh)
	}
}

func TestProSetupUnauthenticated(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))

	result, err := service.ProSetup(context.Background())
	if err != nil {
		t.Fatalf("pro setup: %v", err)
	}
	if result.Authenticated || result.HasPendingLogin {
		t.Fatalf("expected unauthenticated setup: %#v", result)
	}
	if result.CallbackScheme != "agtx" || result.AuthPath == "" || result.ConfigPath == "" || result.Platform == "" {
		t.Fatalf("expected core setup metadata: %#v", result)
	}
	if !slices.Contains(result.CurrentStatus, "pro_api_not_configured") || !slices.Contains(result.CurrentStatus, "not_authenticated") {
		t.Fatalf("expected setup statuses: %#v", result.CurrentStatus)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if !result.CanRegisterScheme || result.SchemeCommandHint != "agtx pro register-scheme" {
			t.Fatalf("expected automatic scheme registration hint on %s: %#v", runtime.GOOS, result)
		}
	} else {
		if result.CanRegisterScheme || !slices.Contains(result.CurrentStatus, "scheme_registration_manual") {
			t.Fatalf("expected manual registration status on %s: %#v", runtime.GOOS, result)
		}
	}
	if !containsSetupAction(result.RecommendedActions, "configure_pro_api") {
		t.Fatalf("expected configure_pro_api action: %#v", result.RecommendedActions)
	}
	if containsSetupAction(result.RecommendedActions, "start_login") || containsSetupAction(result.RecommendedActions, "complete_login") {
		t.Fatalf("did not expect login actions before pro_api_url is configured: %#v", result.RecommendedActions)
	}
}

func TestProSetupPendingLogin(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"
	login, err := service.ProLoginStart(context.Background())
	if err != nil {
		t.Fatalf("pro login start: %v", err)
	}

	result, err := service.ProSetup(context.Background())
	if err != nil {
		t.Fatalf("pro setup: %v", err)
	}
	if result.Authenticated || !result.HasPendingLogin {
		t.Fatalf("expected pending login setup: %#v", result)
	}
	if result.ProAPIURL != "https://pro.example.com" {
		t.Fatalf("expected configured pro api url, got %#v", result)
	}
	if !slices.Contains(result.CurrentStatus, "pro_api_configured") || !slices.Contains(result.CurrentStatus, "pending_login") {
		t.Fatalf("expected pending statuses: %#v", result.CurrentStatus)
	}
	if !containsSetupAction(result.RecommendedActions, "complete_login") {
		t.Fatalf("expected complete_login action: %#v", result.RecommendedActions)
	}
	if containsSetupAction(result.RecommendedActions, "start_login") {
		t.Fatalf("did not expect start_login during pending login: %#v", result.RecommendedActions)
	}
	if !strings.Contains(login.LoginURL, "/v1/cli/login/start") {
		t.Fatalf("expected login fixture url: %#v", login)
	}
}

func TestProStatusPendingLoginPreview(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"
	login, err := service.ProLoginStart(context.Background())
	if err != nil {
		t.Fatalf("pro login start: %v", err)
	}
	status, err := service.ProStatus(context.Background())
	if err != nil {
		t.Fatalf("pro status: %v", err)
	}
	if status.Authenticated {
		t.Fatalf("expected unauthenticated pending login status: %#v", status)
	}
	if status.AuthPath == "" || status.DeviceID == "" || status.DeviceID != login.DeviceID {
		t.Fatalf("expected pending login device/auth details: %#v", status)
	}
	if !slices.Contains(status.CurrentStatus, "pending_login") {
		t.Fatalf("expected pending_login status marker: %#v", status.CurrentStatus)
	}
	if !containsSetupAction(status.RecommendedActions, "complete_login") {
		t.Fatalf("expected complete_login action: %#v", status.RecommendedActions)
	}
}

func TestProStatusConfiguredStartLoginPreview(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"
	status, err := service.ProStatus(context.Background())
	if err != nil {
		t.Fatalf("pro status: %v", err)
	}
	if status.Authenticated || !slices.Contains(status.CurrentStatus, "not_authenticated") {
		t.Fatalf("expected unauthenticated status preview: %#v", status)
	}
	if !containsSetupAction(status.RecommendedActions, "start_login") {
		t.Fatalf("expected start_login action: %#v", status.RecommendedActions)
	}
}

func TestProSetupAuthenticated(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.RegistryURL = "https://registry.example.com/v1/registry"
	auth := AuthState{
		SchemaVersion: 1,
		DeviceID:      "device-1",
		DeviceName:    "Demo Mac",
		AccessToken:   "access-token",
		RefreshToken:  "refresh-token",
		ExpiresAt:     "2099-01-01T00:00:00Z",
	}
	if err := SaveAuth(service.Paths.AuthFile, auth); err != nil {
		t.Fatalf("save auth: %v", err)
	}

	result, err := service.ProSetup(context.Background())
	if err != nil {
		t.Fatalf("pro setup: %v", err)
	}
	if !result.Authenticated || result.HasPendingLogin {
		t.Fatalf("expected authenticated setup: %#v", result)
	}
	if result.RegistryURL != "https://registry.example.com/v1/registry" || result.ProAPIURL != "https://registry.example.com" {
		t.Fatalf("expected registry-derived pro api url: %#v", result)
	}
	if !slices.Contains(result.CurrentStatus, "authenticated") || !slices.Contains(result.CurrentStatus, "registry_configured") {
		t.Fatalf("expected authenticated statuses: %#v", result.CurrentStatus)
	}
	if !containsSetupAction(result.RecommendedActions, "check_status") {
		t.Fatalf("expected check_status action: %#v", result.RecommendedActions)
	}
	if containsSetupAction(result.RecommendedActions, "start_login") || containsSetupAction(result.RecommendedActions, "complete_login") || containsSetupAction(result.RecommendedActions, "configure_pro_api") {
		t.Fatalf("did not expect unauthenticated actions: %#v", result.RecommendedActions)
	}
}

func TestProStatusInvalidAuthPreview(t *testing.T) {
	root := t.TempDir()
	paths := PathsForRoot(root)
	if err := os.MkdirAll(filepath.Dir(paths.AuthFile), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(paths.AuthFile, []byte(`{"schema_version":1,"access_token":"secret","extra":true}`), 0o600); err != nil {
		t.Fatalf("write invalid auth: %v", err)
	}
	service := NewService(paths)
	status, err := service.ProStatus(context.Background())
	if err != nil {
		t.Fatalf("pro status: %v", err)
	}
	if status.Authenticated || !slices.Contains(status.CurrentStatus, "auth_invalid") {
		t.Fatalf("expected auth_invalid status preview: %#v", status)
	}
	if !containsSetupAction(status.RecommendedActions, "reset_local_auth") {
		t.Fatalf("expected reset_local_auth action: %#v", status.RecommendedActions)
	}
}

func TestProSetupInvalidAuthPreview(t *testing.T) {
	root := t.TempDir()
	paths := PathsForRoot(root)
	if err := os.MkdirAll(filepath.Dir(paths.AuthFile), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(paths.AuthFile, []byte(`{"schema_version":1,"access_token":"secret","extra":true}`), 0o600); err != nil {
		t.Fatalf("write invalid auth: %v", err)
	}

	service := NewService(paths)
	service.Config.ProAPIURL = "https://pro.example.com"
	result, err := service.ProSetup(context.Background())
	if err != nil {
		t.Fatalf("pro setup: %v", err)
	}
	if result.Authenticated || result.HasPendingLogin {
		t.Fatalf("expected invalid auth preview to remain unauthenticated: %#v", result)
	}
	if !slices.Contains(result.CurrentStatus, "auth_invalid") {
		t.Fatalf("expected auth_invalid status: %#v", result.CurrentStatus)
	}
	if !containsSetupAction(result.RecommendedActions, "reset_local_auth") {
		t.Fatalf("expected reset_local_auth action: %#v", result.RecommendedActions)
	}
	if !containsSetupAction(result.RecommendedActions, "start_login") {
		t.Fatalf("expected start_login action after preview recovery: %#v", result.RecommendedActions)
	}
}

func TestProDevicesUnauthorizedIncludesRecoveryHints(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	service.Config.ProAPIURL = "https://pro.example.com"

	_, err := service.ProDevices(context.Background())
	if !IsErrorCode(err, CodeUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	coreErr := ErrorFrom(err)
	details, ok := coreErr.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected detail map, got %#v", coreErr.Details)
	}
	setup, ok := details["pro_setup"].(ProSetupResult)
	if !ok || setup.ProAPIURL != "https://pro.example.com" || setup.Authenticated {
		t.Fatalf("expected pro setup preview in details, got %#v", details["pro_setup"])
	}
	actions, ok := details["next_actions"].([]ProSetupAction)
	if !ok || !containsSetupAction(actions, "restart_login") {
		t.Fatalf("expected restart_login recovery action, got %#v", details["next_actions"])
	}
}

func TestProRevokeRejectsUnsafeDeviceID(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.ProRevokeDevice(context.Background(), "../device"); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid device id, got %v", err)
	}
	if _, err := service.ProRevokeDevice(context.Background(), "device/one"); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid device id with slash, got %v", err)
	}
	if err := validateDeviceID("_base64url-device"); err != nil {
		t.Fatalf("expected base64url-style device id to be allowed: %v", err)
	}
}

func TestProAPIURLFromConfigValidatesRuntimeValues(t *testing.T) {
	if _, err := proAPIURLFromConfig(Config{SchemaVersion: 1, ProAPIURL: "http://pro.example.com"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected remote http pro api url rejected, got %v", err)
	}
	got, err := proAPIURLFromConfig(Config{SchemaVersion: 1, ProAPIURL: "https://pro.example.com/base/"})
	if err != nil {
		t.Fatalf("expected valid pro api url: %v", err)
	}
	if got != "https://pro.example.com/base" {
		t.Fatalf("unexpected normalized pro api url: %s", got)
	}
}

func containsSetupAction(actions []ProSetupAction, want string) bool {
	for _, action := range actions {
		if action.ID == want {
			return true
		}
	}
	return false
}
