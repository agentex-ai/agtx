package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	defaultAuthMaxBytes = 128 * 1024
	proRedirectURI      = "agtx://pro/callback"
)

type AuthState struct {
	SchemaVersion int          `json:"schema_version"`
	DeviceID      string       `json:"device_id"`
	DeviceName    string       `json:"device_name"`
	AccessToken   string       `json:"access_token,omitempty"`
	RefreshToken  string       `json:"refresh_token,omitempty"`
	ExpiresAt     string       `json:"expires_at,omitempty"`
	Pending       *PendingAuth `json:"pending,omitempty"`
}

type PendingAuth struct {
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	StartedAt    string `json:"started_at"`
	ProAPIURL    string `json:"pro_api_url"`
}

type proTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	ExpiresAt    string `json:"expires_at"`
	RegistryURL  string `json:"registry_url"`
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	DeviceLimit  int    `json:"device_limit"`
	Subscription string `json:"subscription"`
}

type requestAuth struct {
	AccessToken string
	DeviceID    string
}

func LoadAuth(path string) (AuthState, error) {
	data, err := readFileLimited(path, defaultAuthMaxBytes, "auth")
	if err != nil {
		if os.IsNotExist(err) {
			return AuthState{SchemaVersion: 1}, nil
		}
		return AuthState{}, err
	}
	auth, err := decodeAuth(data)
	if err != nil {
		return AuthState{}, NewError(CodeInvalidArgument, "invalid agtx auth", map[string]any{"path": path, "error": err.Error()})
	}
	return auth, nil
}

func SaveAuth(path string, auth AuthState) error {
	auth = normalizeAuth(auth)
	if err := validateAuth(auth); err != nil {
		return err
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func (s *Service) ProLoginStart(ctx context.Context) (ProLoginStartResult, error) {
	_ = ctx
	apiURL, err := s.proAPIURL()
	if err != nil {
		return ProLoginStartResult{}, err
	}
	auth := normalizeAuth(s.Auth)
	if auth.DeviceID == "" {
		auth.DeviceID, err = randomToken(32)
		if err != nil {
			return ProLoginStartResult{}, err
		}
	}
	if auth.DeviceName == "" {
		auth.DeviceName = defaultDeviceName()
	}
	state, err := randomToken(32)
	if err != nil {
		return ProLoginStartResult{}, err
	}
	verifier, err := randomToken(48)
	if err != nil {
		return ProLoginStartResult{}, err
	}
	auth.Pending = &PendingAuth{State: state, CodeVerifier: verifier, StartedAt: time.Now().UTC().Format(time.RFC3339), ProAPIURL: apiURL}
	if err := SaveAuth(s.Paths.AuthFile, auth); err != nil {
		return ProLoginStartResult{}, err
	}
	s.Auth = auth
	loginURL, err := buildLoginURL(apiURL, auth, state, verifier)
	if err != nil {
		return ProLoginStartResult{}, err
	}
	return ProLoginStartResult{LoginURL: loginURL, State: state, DeviceID: auth.DeviceID, RedirectURI: proRedirectURI, AuthPath: s.Paths.AuthFile}, nil
}

func (s *Service) ProCallback(ctx context.Context, rawCallback string) (ProCallbackResult, error) {
	callback, err := url.Parse(rawCallback)
	if err != nil {
		return ProCallbackResult{}, NewError(CodeInvalidArgument, "callback uri is invalid", map[string]any{"error": err.Error()})
	}
	if callback.Scheme != "agtx" || callback.Host != "pro" || callback.Path != "/callback" {
		return ProCallbackResult{}, NewError(CodeInvalidArgument, "callback uri must use agtx://pro/callback", nil)
	}
	code := strings.TrimSpace(callback.Query().Get("code"))
	state := strings.TrimSpace(callback.Query().Get("state"))
	if code == "" || state == "" {
		return ProCallbackResult{}, NewError(CodeInvalidArgument, "callback uri requires code and state", nil)
	}
	auth := normalizeAuth(s.Auth)
	if auth.Pending == nil {
		return ProCallbackResult{}, NewError(CodeInvalidArgument, "no pending pro login", map[string]any{"retry_with": "agtx pro login"})
	}
	if state != auth.Pending.State {
		return ProCallbackResult{}, NewError(CodeInvalidArgument, "callback state does not match pending login", nil)
	}
	apiURL := auth.Pending.ProAPIURL
	token, err := s.exchangeCode(ctx, apiURL, code, auth.Pending.CodeVerifier, auth)
	if err != nil {
		return ProCallbackResult{}, err
	}
	auth.AccessToken = token.AccessToken
	auth.RefreshToken = token.RefreshToken
	auth.ExpiresAt = tokenExpiry(token)
	auth.Pending = nil
	if token.DeviceID != "" {
		auth.DeviceID = token.DeviceID
	}
	if token.DeviceName != "" {
		auth.DeviceName = token.DeviceName
	}
	if err := SaveAuth(s.Paths.AuthFile, auth); err != nil {
		return ProCallbackResult{}, err
	}
	s.Auth = auth
	if token.RegistryURL != "" && token.RegistryURL != s.Config.RegistryURL {
		config, err := SetConfigValue(s.Config, "registry_url", token.RegistryURL)
		if err != nil {
			return ProCallbackResult{}, err
		}
		if err := SaveConfig(s.Paths.ConfigFile, config); err != nil {
			return ProCallbackResult{}, err
		}
		s.Config = config
		s.Registry, s.RegistrySources = LoadRegistry(s.Paths, s.Config)
	}
	return ProCallbackResult{Authenticated: true, DeviceID: auth.DeviceID, DeviceName: auth.DeviceName, ExpiresAt: auth.ExpiresAt, RegistryURL: token.RegistryURL, ProAPIURL: apiURL, AuthPath: s.Paths.AuthFile, DeviceLimit: token.DeviceLimit, Subscription: token.Subscription}, nil
}

func (s *Service) ProStatus(ctx context.Context) (ProStatusResult, error) {
	authState, authErr := LoadAuth(s.Paths.AuthFile)
	if authErr != nil {
		if IsErrorCode(authErr, CodeInvalidArgument) {
			authState := AuthState{SchemaVersion: 1}
			setup := buildProSetupResult(s.Paths, s.Config, authState, runtime.GOOS, runtime.GOARCH)
			setup.CurrentStatus = appendUniqueStrings(setup.CurrentStatus, "auth_invalid")
			setup.RecommendedActions = buildProSetupActions(setup)
			return proStatusFromSetup(authState, setup), nil
		}
		return ProStatusResult{}, authErr
	}
	authState = normalizeAuth(authState)
	if authState.Pending != nil {
		setup := buildProSetupResult(s.Paths, s.Config, authState, runtime.GOOS, runtime.GOARCH)
		return proStatusFromSetup(authState, setup), nil
	}

	auth, err := s.currentAuth(ctx)
	if err != nil {
		return ProStatusResult{}, err
	}
	setup := buildProSetupResult(s.Paths, s.Config, auth, runtime.GOOS, runtime.GOARCH)
	status := proStatusFromSetup(auth, setup)
	if auth.AccessToken == "" {
		return status, nil
	}
	var remote ProStatusResult
	if err := s.proGET(ctx, "/v1/pro/status", &remote); err != nil {
		return status, err
	}
	remote.Authenticated = true
	if remote.DeviceID == "" {
		remote.DeviceID = auth.DeviceID
	}
	if remote.DeviceName == "" {
		remote.DeviceName = auth.DeviceName
	}
	if remote.ExpiresAt == "" {
		remote.ExpiresAt = auth.ExpiresAt
	}
	if remote.AuthPath == "" {
		remote.AuthPath = s.Paths.AuthFile
	}
	if len(remote.RecommendedActions) == 0 {
		remote.RecommendedActions = status.RecommendedActions
	}
	if len(remote.CurrentStatus) == 0 {
		remote.CurrentStatus = slices.Clone(status.CurrentStatus)
	}
	return remote, nil
}

func proStatusFromSetup(auth AuthState, setup ProSetupResult) ProStatusResult {
	status := ProStatusResult{
		Authenticated:      setup.Authenticated,
		DeviceID:           auth.DeviceID,
		DeviceName:         auth.DeviceName,
		ExpiresAt:          auth.ExpiresAt,
		AuthPath:           setup.AuthPath,
		RecommendedActions: filterProStatusActions(setup.RecommendedActions),
		CurrentStatus:      slices.Clone(setup.CurrentStatus),
	}
	if len(status.CurrentStatus) == 0 {
		if status.Authenticated {
			status.CurrentStatus = []string{"authenticated"}
		} else {
			status.CurrentStatus = []string{"not_authenticated"}
		}
	}
	return status
}

func filterProStatusActions(actions []ProSetupAction) []ProSetupAction {
	if len(actions) == 0 {
		return nil
	}
	filtered := make([]ProSetupAction, 0, len(actions))
	for _, action := range actions {
		if action.ID == "check_status" {
			continue
		}
		filtered = append(filtered, action)
	}
	return uniqueProActions(filtered)
}

func (s *Service) ProDevices(ctx context.Context) ([]ProDevice, error) {
	var devices []ProDevice
	if err := s.proGET(ctx, "/v1/devices", &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func (s *Service) ProRevokeDevice(ctx context.Context, id string) (ProDevice, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ProDevice{}, NewError(CodeInvalidArgument, "device id is required", nil)
	}
	if err := validateDeviceID(id); err != nil {
		return ProDevice{}, err
	}
	var device ProDevice
	err := s.proJSON(ctx, http.MethodPost, "/v1/devices/"+url.PathEscape(id)+"/revoke", nil, &device)
	return device, err
}

func (s *Service) ProLogout() (ProLogoutResult, error) {
	if err := os.Remove(s.Paths.AuthFile); err != nil && !os.IsNotExist(err) {
		return ProLogoutResult{}, err
	}
	s.Auth = AuthState{SchemaVersion: 1}
	return ProLogoutResult{AuthPath: s.Paths.AuthFile, LoggedOut: true}, nil
}

func (s *Service) ProSetup(ctx context.Context) (ProSetupResult, error) {
	result, err := loadProSetupPreview(s.Paths, s.Config)
	if err != nil {
		return ProSetupResult{}, err
	}
	_ = ctx
	return result, nil
}

func (s *Service) ProRegisterScheme() (ProSchemeResult, error) {
	if proRegisterSchemeHook != nil {
		return proRegisterSchemeHook()
	}
	return registerProScheme()
}

func (s *Service) proAPIURL() (string, error) {
	return proAPIURLFromConfig(s.Config)
}

func (s *Service) exchangeCode(ctx context.Context, apiURL, code, verifier string, auth AuthState) (proTokenResponse, error) {
	request := map[string]any{"grant_type": "authorization_code", "code": code, "code_verifier": verifier, "redirect_uri": proRedirectURI, "device_id": auth.DeviceID, "device_name": auth.DeviceName}
	var response proTokenResponse
	err := requestJSON(ctx, http.MethodPost, apiURL+"/v1/cli/token", requestAuth{}, request, &response)
	return response, err
}

func (s *Service) proGET(ctx context.Context, path string, out any) error {
	return s.proJSON(ctx, http.MethodGet, path, nil, out)
}

func (s *Service) proJSON(ctx context.Context, method, path string, in, out any) error {
	apiURL, err := s.proAPIURL()
	if err != nil {
		return err
	}
	auth, err := s.currentAuth(ctx)
	if err != nil {
		return withProRecoveryDetails(err, s.Paths, s.Config)
	}
	if strings.TrimSpace(auth.AccessToken) == "" {
		return withProRecoveryDetails(NewError(CodeUnauthorized, "pro login required", map[string]any{"retry_with": "agtx pro login"}), s.Paths, s.Config)
	}
	if err := requestJSON(ctx, method, apiURL+path, requestAuth{AccessToken: auth.AccessToken, DeviceID: auth.DeviceID}, in, out); err != nil {
		return withProRecoveryDetails(err, s.Paths, s.Config)
	}
	return nil
}

func buildLoginURL(apiURL string, auth AuthState, state, verifier string) (string, error) {
	parsed, err := url.Parse(apiURL + "/v1/cli/login/start")
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("state", state)
	query.Set("device_id", auth.DeviceID)
	query.Set("device_name", auth.DeviceName)
	query.Set("redirect_uri", proRedirectURI)
	query.Set("code_challenge", pkceChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func requestJSON(ctx context.Context, method, rawURL string, auth requestAuth, in, out any) error {
	requestCtx, cancel := contextWithDefaultTimeout(ctx, defaultProRequestTimeout)
	defer cancel()
	var body *bytes.Reader
	if in == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(requestCtx, method, rawURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	}
	if auth.DeviceID != "" {
		req.Header.Set("X-AGTX-Device-ID", auth.DeviceID)
	}
	res, err := outboundHTTPClient.Do(req)
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return NewError(CodeTimeout, "pro request timed out", map[string]any{"url": safeURLForDetails(rawURL), "timeout_ms": defaultProRequestTimeout.Milliseconds()})
		}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, _ := readAllLimited(res.Body, defaultAuthMaxBytes, "pro error")
		return remoteHTTPError("pro request failed", rawURL, res.StatusCode, res.Status, data)
	}
	if out == nil {
		return nil
	}
	data, err := readAllLimited(res.Body, defaultAuthMaxBytes, "pro response")
	if err != nil {
		if requestCtx.Err() == context.DeadlineExceeded {
			return NewError(CodeTimeout, "pro request timed out", map[string]any{"url": safeURLForDetails(rawURL), "timeout_ms": defaultProRequestTimeout.Milliseconds()})
		}
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := decodeJSONStrict(data, out); err != nil {
		return NewError(CodeInvalidArgument, "invalid pro response", err.Error())
	}
	return nil
}

func decodeAuth(data []byte) (AuthState, error) {
	var auth AuthState
	if err := decodeJSONStrict(data, &auth); err != nil {
		return AuthState{}, err
	}
	auth = normalizeAuth(auth)
	return auth, validateAuth(auth)
}

func normalizeAuth(auth AuthState) AuthState {
	if auth.SchemaVersion == 0 {
		auth.SchemaVersion = 1
	}
	return auth
}

func validateAuth(auth AuthState) error {
	if auth.SchemaVersion != 1 {
		return NewError(CodeInvalidArgument, "unsupported auth schema_version", map[string]any{"schema_version": auth.SchemaVersion})
	}
	if auth.DeviceID != "" && strings.TrimSpace(auth.DeviceID) == "" {
		return NewError(CodeInvalidArgument, "device_id cannot be blank", nil)
	}
	if strings.ContainsRune(auth.DeviceID, 0) || strings.ContainsRune(auth.AccessToken, 0) || strings.ContainsRune(auth.RefreshToken, 0) {
		return NewError(CodeInvalidArgument, "auth values must not contain NUL bytes", nil)
	}
	return nil
}

func loadRequestAuth(ctx context.Context, paths Paths, config Config) requestAuth {
	state, err := authForHTTPRequest(ctx, paths, config)
	if err != nil {
		return requestAuth{}
	}
	return state
}

func attachAuthHeader(req *http.Request, config Config, auth requestAuth) {
	if req == nil || strings.TrimSpace(auth.AccessToken) == "" || !shouldAuthorizeURL(req.URL, config) {
		return
	}
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	if auth.DeviceID != "" {
		req.Header.Set("X-AGTX-Device-ID", auth.DeviceID)
	}
}

func authForHTTPRequest(ctx context.Context, paths Paths, config Config) (requestAuth, error) {
	auth, err := LoadAuth(paths.AuthFile)
	if err != nil {
		return requestAuth{}, err
	}
	auth, err = refreshAuthIfNeeded(ctx, paths.AuthFile, config, auth)
	if err != nil {
		return requestAuth{}, err
	}
	return requestAuth{AccessToken: strings.TrimSpace(auth.AccessToken), DeviceID: strings.TrimSpace(auth.DeviceID)}, nil
}

func (s *Service) currentAuth(ctx context.Context) (AuthState, error) {
	auth, err := LoadAuth(s.Paths.AuthFile)
	if err != nil {
		return AuthState{}, err
	}
	auth, err = refreshAuthIfNeeded(ctx, s.Paths.AuthFile, s.Config, auth)
	if err != nil {
		return AuthState{}, err
	}
	s.Auth = auth
	return auth, nil
}

func refreshAuthIfNeeded(ctx context.Context, authPath string, config Config, auth AuthState) (AuthState, error) {
	auth = normalizeAuth(auth)
	if strings.TrimSpace(auth.RefreshToken) == "" || !accessTokenExpiredSoon(auth.ExpiresAt) {
		return auth, nil
	}
	apiURL, err := proAPIURLFromConfig(config)
	if err != nil {
		return auth, nil
	}
	token, err := refreshAccessToken(ctx, apiURL, auth)
	if err != nil {
		return auth, err
	}
	if token.AccessToken != "" {
		auth.AccessToken = token.AccessToken
	}
	if token.RefreshToken != "" {
		auth.RefreshToken = token.RefreshToken
	}
	auth.ExpiresAt = tokenExpiry(token)
	if token.DeviceID != "" {
		auth.DeviceID = token.DeviceID
	}
	if token.DeviceName != "" {
		auth.DeviceName = token.DeviceName
	}
	if err := SaveAuth(authPath, auth); err != nil {
		return auth, err
	}
	return auth, nil
}

func refreshAccessToken(ctx context.Context, apiURL string, auth AuthState) (proTokenResponse, error) {
	request := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": auth.RefreshToken,
		"device_id":     auth.DeviceID,
		"device_name":   auth.DeviceName,
	}
	var response proTokenResponse
	err := requestJSON(ctx, http.MethodPost, strings.TrimRight(apiURL, "/")+"/v1/cli/token", requestAuth{}, request, &response)
	return response, err
}

func proAPIURLFromConfig(config Config) (string, error) {
	if strings.TrimSpace(config.ProAPIURL) != "" {
		if err := validateServiceURL("pro_api_url", config.ProAPIURL); err != nil {
			return "", err
		}
		parsed, err := url.Parse(config.ProAPIURL)
		if err != nil {
			return "", err
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		parsed.RawPath = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String(), nil
	}
	if strings.TrimSpace(config.RegistryURL) != "" {
		if err := validateRegistryURL(config.RegistryURL); err != nil {
			return "", err
		}
		parsed, err := url.Parse(config.RegistryURL)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host, nil
		}
	}
	return "", NewError(CodeInvalidArgument, "pro_api_url is not configured", map[string]any{"retry_with": "agtx config set pro_api_url https://example.com"})
}

func accessTokenExpiredSoon(expiresAt string) bool {
	if strings.TrimSpace(expiresAt) == "" {
		return false
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return true
	}
	return time.Until(expires) <= time.Minute
}

func shouldAuthorizeURL(target *url.URL, config Config) bool {
	if target == nil {
		return false
	}
	for _, raw := range []string{config.ProAPIURL, config.RegistryURL} {
		if sameOrigin(target, raw) {
			return true
		}
	}
	return false
}

func sameOrigin(target *url.URL, raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(target.Scheme, parsed.Scheme) && strings.EqualFold(target.Host, parsed.Host)
}

func tokenExpiry(token proTokenResponse) string {
	if strings.TrimSpace(token.ExpiresAt) != "" {
		return token.ExpiresAt
	}
	if token.ExpiresIn > 0 {
		return time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return ""
}

func buildProSetupActions(result ProSetupResult) []ProSetupAction {
	actions := []ProSetupAction{}
	if hasSetupStatus(result.CurrentStatus, "auth_invalid") {
		actions = append(actions, ProSetupAction{
			ID:       "reset_local_auth",
			Title:    "Reset local auth",
			Summary:  "Remove the corrupt local auth file before starting a fresh Pro login flow.",
			Blocking: true,
			Command:  "agtx pro logout",
			MCPTool:  "logout_pro",
			AppliesWhen: []string{
				"auth_invalid",
			},
		})
	}
	if result.ProAPIURL == "" {
		actions = append(actions, ProSetupAction{
			ID:       "configure_pro_api",
			Title:    "Configure Pro API",
			Summary:  "Set pro_api_url before starting the Pro login flow.",
			Blocking: true,
			Command:  "agtx config set pro_api_url https://agtx-pro.example.com",
			AppliesWhen: []string{
				"pro_api_not_configured",
			},
		})
	}
	if !result.Authenticated && result.CanRegisterScheme {
		actions = append(actions, ProSetupAction{
			ID:       "register_callback_scheme",
			Title:    "Register callback scheme",
			Summary:  "Register agtx:// so browser login callbacks can return to agtx automatically.",
			Blocking: false,
			Command:  "agtx pro register-scheme",
			MCPTool:  "register_pro_scheme",
			AppliesWhen: []string{
				"not_authenticated",
			},
		})
	}
	if !result.Authenticated && !result.HasPendingLogin && result.ProAPIURL != "" {
		actions = append(actions, ProSetupAction{
			ID:       "start_login",
			Title:    "Start Pro login",
			Summary:  "Create a login URL and pending PKCE state.",
			Blocking: true,
			Command:  "agtx pro login --open",
			MCPTool:  "start_pro_login",
			AppliesWhen: []string{
				"pro_api_configured",
				"not_authenticated",
			},
		})
	}
	if !result.Authenticated && result.HasPendingLogin {
		actions = append(actions, ProSetupAction{
			ID:       "complete_login",
			Title:    "Complete Pro login",
			Summary:  "Finish login with the agtx://pro/callback URI returned by the browser flow.",
			Blocking: true,
			Command:  "agtx pro callback \"agtx://pro/callback?code=...&state=...\"",
			MCPTool:  "complete_pro_login",
			Arguments: map[string]any{
				"callback_uri": "agtx://pro/callback?code=...&state=...",
			},
			AppliesWhen: []string{
				"pending_login",
			},
		})
	}
	if result.Authenticated {
		actions = append(actions, ProSetupAction{
			ID:       "check_status",
			Title:    "Check Pro status",
			Summary:  "Inspect subscription state and device limits before Pro-only installs.",
			Blocking: false,
			Command:  "agtx pro status --json",
			MCPTool:  "get_pro_status",
			AppliesWhen: []string{
				"authenticated",
			},
		})
	}
	return actions
}

func hasSetupStatus(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func defaultDeviceName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = runtime.GOOS + "-" + runtime.GOARCH
	}
	return host
}

func validateDeviceID(id string) error {
	if strings.TrimSpace(id) == "" {
		return NewError(CodeInvalidArgument, "device id is required", nil)
	}
	if strings.TrimSpace(id) != id {
		return NewError(CodeInvalidArgument, "device id must not contain leading or trailing whitespace", map[string]any{"value": id})
	}
	if id == "." || id == ".." || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.ContainsRune(id, 0) {
		return NewError(CodeInvalidArgument, "device id must be a safe path segment", map[string]any{"value": id})
	}
	if !deviceIDPattern.MatchString(id) {
		return NewError(CodeInvalidArgument, "device id contains unsupported characters", map[string]any{"value": id})
	}
	return nil
}

func randomToken(bytesLen int) (string, error) {
	token, err := NewSecretToken(bytesLen)
	if err != nil {
		return "", NewError(CodeInternal, "secure random token generation failed", map[string]any{"error": err.Error()})
	}
	return token, nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
