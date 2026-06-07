package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CommerceHTTPOptions struct {
	AllowedOrigin string
	MutationToken string
}

type CommerceHTTPIndex struct {
	SchemaVersion int                    `json:"schema_version"`
	GeneratedAt   string                 `json:"generated_at"`
	Endpoints     []CommerceHTTPEndpoint `json:"endpoints"`
}

type CommerceHTTPEndpoint struct {
	Name        string   `json:"name"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Headers     []string `json:"headers,omitempty"`
	Query       []string `json:"query,omitempty"`
	Body        []string `json:"body,omitempty"`
}

func CommerceHTTPEndpoints() []CommerceHTTPEndpoint {
	return []CommerceHTTPEndpoint{
		{Name: "commerce_index", Method: http.MethodGet, Path: "/v1/commerce", Description: "List website commerce endpoints."},
		{Name: "list_capability_packs", Method: http.MethodGet, Path: "/v1/commerce/packs", Description: "Return first-wave website capability packs plus standard and advanced bundle state.", Query: []string{"pack_id"}},
		{Name: "list_capability_scenarios", Method: http.MethodGet, Path: "/v1/commerce/scenarios", Description: "Return real task scenarios mapped to recommended capability packs, install state, and billing preview.", Query: []string{"scenario_id", "pack_id"}},
		{Name: "plan_capability_pack_install", Method: http.MethodGet, Path: "/v1/commerce/install-plan", Description: "Preview skill changes and billing records before installing a capability pack.", Query: []string{"pack_id"}},
		{Name: "plan_capability_scenario_install", Method: http.MethodGet, Path: "/v1/commerce/scenario-install-plan", Description: "Preview recommended pack changes and billing records for a real task scenario.", Query: []string{"scenario_id"}},
		{Name: "install_capability_pack", Method: http.MethodPost, Path: "/v1/commerce/install-pack", Description: "Install a capability pack after explicit confirmation.", Headers: []string{"X-AGTX-Commerce-Token"}, Body: []string{"pack_id", "yes"}},
		{Name: "install_capability_scenario", Method: http.MethodPost, Path: "/v1/commerce/install-scenario", Description: "Install the recommended pack for a real task scenario and tag install/billing records with scenario_id.", Headers: []string{"X-AGTX-Commerce-Token"}, Body: []string{"scenario_id", "yes"}},
		{Name: "get_capability_scenario_ledger", Method: http.MethodGet, Path: "/v1/commerce/scenario-ledger", Description: "Return one task scenario with matching install records, billing records, totals, and latest install state.", Query: []string{"scenario_id", "skill", "status", "type", "currency", "from", "to", "limit"}},
		{Name: "list_install_records", Method: http.MethodGet, Path: "/v1/commerce/install-records", Description: "Return local capability-pack and skill install records.", Query: []string{"pack_id", "scenario_id", "skill", "status", "from", "to", "limit"}},
		{Name: "list_billing_records", Method: http.MethodGet, Path: "/v1/commerce/billing-records", Description: "Return local pack-install and skill-usage billing records.", Query: []string{"pack_id", "scenario_id", "skill", "status", "type", "currency", "from", "to", "limit"}},
		{Name: "list_commerce_receipts", Method: http.MethodGet, Path: "/v1/commerce/receipts", Description: "Return local server receipt records for submitted commerce proofs.", Query: []string{"status", "from", "to", "limit"}},
		{Name: "get_commerce_integrity", Method: http.MethodGet, Path: "/v1/commerce/integrity", Description: "Return local commerce ledger integrity, anchor, and private-file checks for website account/security views."},
		{Name: "get_commerce_proof", Method: http.MethodGet, Path: "/v1/commerce/proof", Description: "Return a challenge-bound Ed25519 proof over local commerce ledger integrity for website verification.", Query: []string{"challenge"}},
		{Name: "submit_commerce_proof", Method: http.MethodPost, Path: "/v1/commerce/proof/submit", Description: "Submit a challenge-bound commerce proof to Pro for a server receipt and record the signed receipt locally.", Headers: []string{"X-AGTX-Commerce-Token"}, Body: []string{"challenge", "yes"}},
		{Name: "get_commerce_snapshot", Method: http.MethodGet, Path: "/v1/commerce/snapshot", Description: "Return packs, scenarios, install records, and billing records in one website-friendly snapshot.", Query: []string{"pack_id", "scenario_id", "skill", "status", "type", "currency", "from", "to", "limit"}},
	}
}

func (s *Service) CommerceHTTPHandler(options CommerceHTTPOptions) http.Handler {
	handler := &commerceHTTPHandler{service: s, options: options}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler.route)
	return mux
}

type commerceHTTPHandler struct {
	service *Service
	options CommerceHTTPOptions
}

func (h *commerceHTTPHandler) route(writer http.ResponseWriter, request *http.Request) {
	allowedOrigin := h.allowedOrigin(request)
	if allowedOrigin != "" {
		writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-AGTX-Commerce-Token, X-AGTX-Commerce-Challenge")
		writer.Header().Set("Access-Control-Max-Age", "600")
		if allowedOrigin != "*" {
			writer.Header().Add("Vary", "Origin")
		}
	}
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimSuffix(request.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch path {
	case "/":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.writeHTML(writer, commerceDashboardHTML(h.options.MutationToken))
	case "/commerce":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.writeHTML(writer, commerceDashboardHTML(h.options.MutationToken))
	case "/healthz":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.writeOK(writer, map[string]any{"ok": true, "time": time.Now().UTC().Format(time.RFC3339)})
	case "/v1/commerce":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.writeOK(writer, CommerceHTTPIndex{SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Endpoints: CommerceHTTPEndpoints()})
	case "/v1/commerce/packs":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.listCapabilityPacks(writer, request)
	case "/v1/commerce/scenarios":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.listCapabilityScenarios(writer, request)
	case "/v1/commerce/install-plan":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.planCapabilityPackInstall(writer, request)
	case "/v1/commerce/scenario-install-plan":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.planCapabilityScenarioInstall(writer, request)
	case "/v1/commerce/install-pack":
		if !h.requireMethod(writer, request, http.MethodPost) {
			return
		}
		if !h.authorizeMutation(writer, request) {
			return
		}
		h.installCapabilityPack(writer, request)
	case "/v1/commerce/install-scenario":
		if !h.requireMethod(writer, request, http.MethodPost) {
			return
		}
		if !h.authorizeMutation(writer, request) {
			return
		}
		h.installCapabilityScenario(writer, request)
	case "/v1/commerce/scenario-ledger":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.getCapabilityScenarioLedger(writer, request)
	case "/v1/commerce/install-records":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.listInstallRecords(writer, request)
	case "/v1/commerce/billing-records":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.listBillingRecords(writer, request)
	case "/v1/commerce/receipts":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.listCommerceReceipts(writer, request)
	case "/v1/commerce/integrity":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.getCommerceIntegrity(writer, request)
	case "/v1/commerce/proof":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.getCommerceProof(writer, request)
	case "/v1/commerce/proof/submit":
		if !h.requireMethod(writer, request, http.MethodPost) {
			return
		}
		if !h.authorizeMutation(writer, request) {
			return
		}
		h.submitCommerceProof(writer, request)
	case "/v1/commerce/snapshot":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.getCommerceSnapshot(writer, request)
	default:
		h.writeError(writer, http.StatusNotFound, NewError(CodeNotFound, "commerce endpoint not found", map[string]any{"path": request.URL.Path, "endpoints": CommerceHTTPEndpoints()}))
	}
}

func (h *commerceHTTPHandler) authorizeMutation(writer http.ResponseWriter, request *http.Request) bool {
	token := strings.TrimSpace(h.options.MutationToken)
	if token == "" {
		err := NewError(CodeUnauthorized, "commerce mutation token is not configured", map[string]any{"header": "X-AGTX-Commerce-Token"})
		h.writeError(writer, httpStatusForError(err), err)
		return false
	}
	if request.Header.Get("X-AGTX-Commerce-Token") != token {
		err := NewError(CodeUnauthorized, "commerce mutation token is required", map[string]any{"header": "X-AGTX-Commerce-Token"})
		h.writeError(writer, httpStatusForError(err), err)
		return false
	}
	return true
}

func (h *commerceHTTPHandler) requireMethod(writer http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}
	writer.Header().Set("Allow", method+", OPTIONS")
	h.writeError(writer, http.StatusMethodNotAllowed, NewError(CodeInvalidArgument, "commerce endpoint does not support method", map[string]any{"method": request.Method, "expected_method": method}))
	return false
}

func (h *commerceHTTPHandler) listCapabilityPacks(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	packs, err := h.service.ListCapabilityPacks()
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	if options.PackID != "" {
		packs = filterCapabilityPackViewsByPack(packs, options.PackID)
	}
	h.writeOK(writer, packs)
}

func (h *commerceHTTPHandler) listCapabilityScenarios(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	scenarioID := strings.TrimSpace(request.URL.Query().Get("scenario_id"))
	var scenarios []CapabilityScenarioView
	if scenarioID != "" {
		scenario, err := h.service.GetCapabilityScenario(scenarioID)
		if err != nil {
			h.writeError(writer, httpStatusForError(err), err)
			return
		}
		scenarios = []CapabilityScenarioView{scenario}
	} else {
		scenarios, err = h.service.ListCapabilityScenarios()
		if err != nil {
			h.writeError(writer, httpStatusForError(err), err)
			return
		}
	}
	if options.PackID != "" {
		scenarios = filterCapabilityScenariosByPack(scenarios, options.PackID)
	}
	h.writeOK(writer, scenarios)
}

func (h *commerceHTTPHandler) planCapabilityPackInstall(writer http.ResponseWriter, request *http.Request) {
	packID := strings.TrimSpace(request.URL.Query().Get("pack_id"))
	if packID == "" {
		h.writeError(writer, http.StatusBadRequest, NewError(CodeInvalidArgument, "pack_id is required", map[string]any{"parameter": "pack_id"}))
		return
	}
	plan, err := h.service.PlanCapabilityPackInstall(packID)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, plan)
}

func (h *commerceHTTPHandler) planCapabilityScenarioInstall(writer http.ResponseWriter, request *http.Request) {
	scenarioID := strings.TrimSpace(request.URL.Query().Get("scenario_id"))
	if scenarioID == "" {
		h.writeError(writer, http.StatusBadRequest, NewError(CodeInvalidArgument, "scenario_id is required", map[string]any{"parameter": "scenario_id"}))
		return
	}
	plan, err := h.service.PlanCapabilityScenarioInstall(scenarioID)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, plan)
}

func (h *commerceHTTPHandler) installCapabilityPack(writer http.ResponseWriter, request *http.Request) {
	body, err := decodeCapabilityPackInstallRequest(writer, request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	if !body.Yes {
		err := NewError(CodeConfirmationRequired, "install-pack requires explicit confirmation", map[string]any{
			"action":           "install-pack",
			"pack":             body.PackID,
			"expected":         "yes=true",
			"retry_with":       map[string]any{"pack_id": body.PackID, "yes": true},
			"supported_fields": []string{"pack_id", "yes"},
		})
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	result, err := h.service.InstallCapabilityPack(request.Context(), body.PackID)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, result)
}

func (h *commerceHTTPHandler) installCapabilityScenario(writer http.ResponseWriter, request *http.Request) {
	body, err := decodeCapabilityScenarioInstallRequest(writer, request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	if !body.Yes {
		err := NewError(CodeConfirmationRequired, "install-scenario requires explicit confirmation", map[string]any{
			"action":           "install-scenario",
			"scenario":         body.ScenarioID,
			"expected":         "yes=true",
			"retry_with":       map[string]any{"scenario_id": body.ScenarioID, "yes": true},
			"supported_fields": []string{"scenario_id", "yes"},
		})
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	result, err := h.service.InstallCapabilityScenario(request.Context(), body.ScenarioID)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, result)
}

func (h *commerceHTTPHandler) listInstallRecords(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	records, err := h.service.ListInstallRecordsWithIntegrity(options)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, records)
}

func (h *commerceHTTPHandler) listBillingRecords(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	records, err := h.service.ListBillingRecords(options)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, records)
}

func (h *commerceHTTPHandler) listCommerceReceipts(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	records, err := h.service.ListCommerceReceipts(options)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, records)
}

func (h *commerceHTTPHandler) getCapabilityScenarioLedger(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	scenarioID := strings.TrimSpace(request.URL.Query().Get("scenario_id"))
	if scenarioID == "" {
		h.writeError(writer, http.StatusBadRequest, NewError(CodeInvalidArgument, "scenario_id is required", map[string]any{"parameter": "scenario_id"}))
		return
	}
	ledger, err := h.service.CapabilityScenarioLedger(scenarioID, options)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, ledger)
}

func (h *commerceHTTPHandler) getCommerceSnapshot(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	snapshot, err := h.service.CommerceSnapshot(options)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	if options.PackID != "" {
		snapshot.Packs = filterCapabilityPackViewsByPack(snapshot.Packs, options.PackID)
		snapshot.Scenarios = filterCapabilityScenariosByPack(snapshot.Scenarios, options.PackID)
	}
	h.writeOK(writer, snapshot)
}

func (h *commerceHTTPHandler) getCommerceIntegrity(writer http.ResponseWriter, request *http.Request) {
	result, err := h.service.CommerceIntegrity()
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, result)
}

func (h *commerceHTTPHandler) getCommerceProof(writer http.ResponseWriter, request *http.Request) {
	challenge := strings.TrimSpace(request.URL.Query().Get("challenge"))
	if challenge == "" {
		challenge = strings.TrimSpace(request.Header.Get("X-AGTX-Commerce-Challenge"))
	}
	result, err := h.service.CommerceProof(challenge)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, result)
}

func (h *commerceHTTPHandler) submitCommerceProof(writer http.ResponseWriter, request *http.Request) {
	body, err := decodeCommerceProofSubmitRequest(writer, request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	if !body.Yes {
		err := NewError(CodeConfirmationRequired, "submit-proof requires explicit confirmation", map[string]any{
			"action":           "submit-proof",
			"challenge":        body.Challenge,
			"expected":         "yes=true",
			"retry_with":       map[string]any{"challenge": body.Challenge, "yes": true},
			"supported_fields": []string{"challenge", "yes"},
		})
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	result, err := h.service.SubmitCommerceProof(request.Context(), body.Challenge)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	h.writeOK(writer, result)
}

func filterCapabilityScenariosByPack(scenarios []CapabilityScenarioView, packID string) []CapabilityScenarioView {
	return filterCapabilityScenarioViewsByPack(scenarios, packID)
}

func recordQueryOptionsFromURL(request *http.Request) (RecordQueryOptions, error) {
	query := request.URL.Query()
	options := RecordQueryOptions{
		PackID:     strings.TrimSpace(query.Get("pack_id")),
		ScenarioID: strings.TrimSpace(query.Get("scenario_id")),
		Skill:      strings.TrimSpace(query.Get("skill")),
		Status:     strings.TrimSpace(query.Get("status")),
		Type:       strings.TrimSpace(query.Get("type")),
		Currency:   strings.TrimSpace(query.Get("currency")),
		From:       strings.TrimSpace(query.Get("from")),
		To:         strings.TrimSpace(query.Get("to")),
		Limit:      100,
	}
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			return RecordQueryOptions{}, NewError(CodeInvalidArgument, "limit must be a positive integer", map[string]any{"parameter": "limit", "reason": "invalid_positive_integer"})
		}
		options.Limit = limit
	}
	if err := ValidateRecordQueryOptions(options); err != nil {
		return RecordQueryOptions{}, err
	}
	return options, nil
}

type capabilityPackInstallRequest struct {
	PackID string `json:"pack_id"`
	Yes    bool   `json:"yes"`
}

type capabilityScenarioInstallRequest struct {
	ScenarioID string `json:"scenario_id"`
	Yes        bool   `json:"yes"`
}

type commerceProofSubmitHTTPRequest struct {
	Challenge string `json:"challenge"`
	Yes       bool   `json:"yes"`
}

func decodeCapabilityPackInstallRequest(writer http.ResponseWriter, request *http.Request) (capabilityPackInstallRequest, error) {
	defer request.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, defaultConfigMaxBytes))
	if err != nil {
		return capabilityPackInstallRequest{}, NewError(CodeSizeLimitExceeded, "commerce install request body exceeds size limit", map[string]any{"limit": defaultConfigMaxBytes})
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return capabilityPackInstallRequest{}, NewError(CodeInvalidArgument, "commerce install request body is required", map[string]any{"supported_fields": []string{"pack_id", "yes"}})
	}
	var requestBody capabilityPackInstallRequest
	if err := decodeJSONStrict(body, &requestBody); err != nil {
		return capabilityPackInstallRequest{}, NewError(CodeInvalidArgument, "invalid commerce install request", map[string]any{"error": err.Error(), "supported_fields": []string{"pack_id", "yes"}})
	}
	requestBody.PackID = strings.TrimSpace(requestBody.PackID)
	if requestBody.PackID == "" {
		return capabilityPackInstallRequest{}, NewError(CodeInvalidArgument, "pack_id is required", map[string]any{"field": "pack_id", "supported_fields": []string{"pack_id", "yes"}})
	}
	return requestBody, nil
}

func decodeCapabilityScenarioInstallRequest(writer http.ResponseWriter, request *http.Request) (capabilityScenarioInstallRequest, error) {
	defer request.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, defaultConfigMaxBytes))
	if err != nil {
		return capabilityScenarioInstallRequest{}, NewError(CodeSizeLimitExceeded, "commerce scenario install request body exceeds size limit", map[string]any{"limit": defaultConfigMaxBytes})
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return capabilityScenarioInstallRequest{}, NewError(CodeInvalidArgument, "commerce scenario install request body is required", map[string]any{"supported_fields": []string{"scenario_id", "yes"}})
	}
	var requestBody capabilityScenarioInstallRequest
	if err := decodeJSONStrict(body, &requestBody); err != nil {
		return capabilityScenarioInstallRequest{}, NewError(CodeInvalidArgument, "invalid commerce scenario install request", map[string]any{"error": err.Error(), "supported_fields": []string{"scenario_id", "yes"}})
	}
	requestBody.ScenarioID = strings.TrimSpace(requestBody.ScenarioID)
	if requestBody.ScenarioID == "" {
		return capabilityScenarioInstallRequest{}, NewError(CodeInvalidArgument, "scenario_id is required", map[string]any{"field": "scenario_id", "supported_fields": []string{"scenario_id", "yes"}})
	}
	return requestBody, nil
}

func decodeCommerceProofSubmitRequest(writer http.ResponseWriter, request *http.Request) (commerceProofSubmitHTTPRequest, error) {
	defer request.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, defaultConfigMaxBytes))
	if err != nil {
		return commerceProofSubmitHTTPRequest{}, NewError(CodeSizeLimitExceeded, "commerce proof submit request body exceeds size limit", map[string]any{"limit": defaultConfigMaxBytes})
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return commerceProofSubmitHTTPRequest{}, NewError(CodeInvalidArgument, "commerce proof submit request body is required", map[string]any{"supported_fields": []string{"challenge", "yes"}})
	}
	var requestBody commerceProofSubmitHTTPRequest
	if err := decodeJSONStrict(body, &requestBody); err != nil {
		return commerceProofSubmitHTTPRequest{}, NewError(CodeInvalidArgument, "invalid commerce proof submit request", map[string]any{"error": err.Error(), "supported_fields": []string{"challenge", "yes"}})
	}
	requestBody.Challenge = strings.TrimSpace(requestBody.Challenge)
	if requestBody.Challenge == "" {
		return commerceProofSubmitHTTPRequest{}, NewError(CodeInvalidArgument, "challenge is required", map[string]any{"field": "challenge", "supported_fields": []string{"challenge", "yes"}})
	}
	return requestBody, nil
}

func (h *commerceHTTPHandler) allowedOrigin(request *http.Request) string {
	allowed := strings.TrimSpace(h.options.AllowedOrigin)
	if allowed == "" {
		return ""
	}
	if allowed == "*" {
		return "*"
	}
	if request.Header.Get("Origin") == allowed {
		return allowed
	}
	return ""
}

func (h *commerceHTTPHandler) writeOK(writer http.ResponseWriter, data any) {
	h.writeResponse(writer, http.StatusOK, NewResponse(data, nil))
}

func (h *commerceHTTPHandler) writeHTML(writer http.ResponseWriter, body []byte) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (h *commerceHTTPHandler) writeError(writer http.ResponseWriter, status int, err error) {
	h.writeResponse(writer, status, NewErrorResponse(err, nil))
}

func (h *commerceHTTPHandler) writeResponse(writer http.ResponseWriter, status int, response Response) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(response)
}

func httpStatusForError(err error) int {
	coreErr := ErrorFrom(err)
	switch coreErr.Code {
	case CodeConfirmationRequired:
		return http.StatusPreconditionRequired
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeNotFound, CodeNotInstalled:
		return http.StatusNotFound
	case CodeLockBusy, CodeDeviceLimitExceeded:
		return http.StatusConflict
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeSubscriptionRequired:
		return http.StatusPaymentRequired
	case CodeSizeLimitExceeded:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}
