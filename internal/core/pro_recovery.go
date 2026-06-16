package core

import (
	"net/http"
	"runtime"
	"strings"
)

func buildProSetupResult(paths Paths, config Config, auth AuthState, goos, goarch string) ProSetupResult {
	auth = normalizeAuth(auth)
	result := ProSetupResult{
		Authenticated:      strings.TrimSpace(auth.AccessToken) != "",
		HasPendingLogin:    auth.Pending != nil,
		CallbackScheme:     proSchemeName,
		CallbackURIExample: "agtx://pro/callback?code=...&state=...",
		AuthPath:           paths.AuthFile,
		ConfigPath:         paths.ConfigFile,
		ProAPIURL:          strings.TrimSpace(config.ProAPIURL),
		RegistryURL:        strings.TrimSpace(config.RegistryURL),
		Platform:           goos + "/" + goarch,
		CanRegisterScheme:  proSchemeCanAutoRegister(goos),
	}
	if hint := proSchemeCommandHint(goos); hint != "" {
		result.SchemeCommandHint = hint
	}
	if result.ProAPIURL == "" {
		if derived, deriveErr := proAPIURLFromConfig(config); deriveErr == nil {
			result.ProAPIURL = derived
		}
	}
	if result.RegistryURL != "" {
		result.CurrentStatus = append(result.CurrentStatus, "registry_configured")
	} else {
		result.CurrentStatus = append(result.CurrentStatus, "registry_not_configured")
	}
	if result.ProAPIURL != "" {
		result.CurrentStatus = append(result.CurrentStatus, "pro_api_configured")
	} else {
		result.CurrentStatus = append(result.CurrentStatus, "pro_api_not_configured")
	}
	if result.Authenticated {
		result.CurrentStatus = append(result.CurrentStatus, "authenticated")
	} else {
		result.CurrentStatus = append(result.CurrentStatus, "not_authenticated")
	}
	if result.HasPendingLogin {
		result.CurrentStatus = append(result.CurrentStatus, "pending_login")
	}
	if !result.CanRegisterScheme {
		result.CurrentStatus = append(result.CurrentStatus, "scheme_registration_manual")
	}
	result.RecommendedActions = append(result.RecommendedActions, buildProSetupActions(result)...)
	return result
}

func loadProSetupPreview(paths Paths, config Config) (ProSetupResult, error) {
	auth, err := LoadAuth(paths.AuthFile)
	if err != nil {
		if IsErrorCode(err, CodeInvalidArgument) {
			result := buildProSetupResult(paths, config, AuthState{SchemaVersion: 1}, runtime.GOOS, runtime.GOARCH)
			result.CurrentStatus = appendUniqueStrings(result.CurrentStatus, "auth_invalid")
			result.RecommendedActions = buildProSetupActions(result)
			return result, nil
		}
		return ProSetupResult{}, err
	}
	return buildProSetupResult(paths, config, auth, runtime.GOOS, runtime.GOARCH), nil
}

func withProRecoveryDetails(err error, paths Paths, config Config) error {
	coreErr := ErrorFrom(err)
	if coreErr == nil {
		return err
	}
	switch coreErr.Code {
	case CodeUnauthorized, CodeSubscriptionRequired, CodeDeviceLimitExceeded:
	default:
		return err
	}

	details := cloneDetailsMap(coreErr.Details)
	setup, setupErr := loadProSetupPreview(paths, config)
	if setupErr == nil {
		details["pro_setup"] = setup
		details["next_actions"] = buildProRecoveryActions(coreErr.Code, setup)
	} else {
		details["pro_setup_error"] = ErrorFrom(setupErr)
	}
	return NewError(coreErr.Code, coreErr.Message, details)
}

func buildProRecoveryActions(code string, setup ProSetupResult) []ProSetupAction {
	actions := []ProSetupAction{}
	switch code {
	case CodeUnauthorized:
		if setup.ProAPIURL != "" {
			actions = append(actions, ProSetupAction{
				ID:       "restart_login",
				Title:    "Restart Pro login",
				Summary:  "Start a fresh Pro login flow to replace missing, expired, or rejected credentials.",
				Blocking: true,
				Command:  "agtx pro login --open",
				MCPTool:  "start_pro_login",
				AppliesWhen: []string{
					"pro_api_configured",
				},
			})
		}
	case CodeSubscriptionRequired:
		actions = append(actions, checkProStatusAction("Inspect subscription status before retrying a Pro-gated registry refresh or package download."))
	case CodeDeviceLimitExceeded:
		actions = append(actions,
			checkProStatusAction("Inspect device-limit state before retrying a Pro-gated registry refresh or package download."),
			ProSetupAction{
				ID:       "list_devices",
				Title:    "List Pro devices",
				Summary:  "Inspect active devices and choose one to revoke if this subscription reached its device limit.",
				Blocking: false,
				Command:  "agtx pro devices",
				MCPTool:  "list_pro_devices",
				AppliesWhen: []string{
					"authenticated",
				},
			},
		)
	}

	actions = append(actions, setup.RecommendedActions...)
	return uniqueProActions(actions)
}

func checkProStatusAction(summary string) ProSetupAction {
	return ProSetupAction{
		ID:       "check_status",
		Title:    "Check Pro status",
		Summary:  summary,
		Blocking: false,
		Command:  "agtx pro status --json",
		MCPTool:  "get_pro_status",
		AppliesWhen: []string{
			"authenticated",
		},
	}
}

func uniqueProActions(actions []ProSetupAction) []ProSetupAction {
	if len(actions) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]ProSetupAction, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.ID) == "" || seen[action.ID] {
			continue
		}
		seen[action.ID] = true
		out = append(out, action)
	}
	return out
}

func cloneDetailsMap(details any) map[string]any {
	out := map[string]any{}
	existing, ok := details.(map[string]any)
	if !ok {
		if details != nil {
			out["remote_details"] = details
		}
		return out
	}
	for key, value := range existing {
		out[key] = value
	}
	return out
}

func appendUniqueStrings(values []string, items ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		values = append(values, item)
	}
	return values
}

func remoteHTTPError(message, rawURL string, statusCode int, status string, body []byte) error {
	if len(strings.TrimSpace(string(body))) > 0 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details any    `json:"details"`
			} `json:"error"`
		}
		if err := decodeJSONStrict(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Code) != "" {
			details := cloneDetailsMap(envelope.Error.Details)
			details["url"] = safeURLForDetails(rawURL)
			details["status_code"] = statusCode
			details["status"] = status
			return NewError(envelope.Error.Code, envelope.Error.Message, details)
		}
	}
	return httpStatusError(message, rawURL, statusCode, status)
}

func httpStatusError(message, rawURL string, statusCode int, status string) error {
	details := map[string]any{"url": safeURLForDetails(rawURL), "status_code": statusCode, "status": status}
	switch statusCode {
	case http.StatusUnauthorized:
		return NewError(CodeUnauthorized, message, details)
	case http.StatusPaymentRequired, http.StatusForbidden:
		return NewError(CodeSubscriptionRequired, message, details)
	case http.StatusConflict, http.StatusTooManyRequests:
		return NewError(CodeDeviceLimitExceeded, message, details)
	default:
		return NewError(CodeInternal, message, details)
	}
}
