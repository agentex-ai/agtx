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
		{Name: "list_capability_packs", Method: http.MethodGet, Path: "/v1/commerce/packs", Description: "Return standard and advanced capability-pack state.", Query: []string{"pack_id"}},
		{Name: "plan_capability_pack_install", Method: http.MethodGet, Path: "/v1/commerce/install-plan", Description: "Preview skill changes and billing records before installing a standard or advanced pack.", Query: []string{"pack_id"}},
		{Name: "install_capability_pack", Method: http.MethodPost, Path: "/v1/commerce/install-pack", Description: "Install a standard or advanced capability pack after explicit confirmation.", Headers: []string{"X-AGTX-Commerce-Token"}, Body: []string{"pack_id", "yes"}},
		{Name: "list_install_records", Method: http.MethodGet, Path: "/v1/commerce/install-records", Description: "Return local capability-pack and skill install records.", Query: []string{"pack_id", "skill", "status", "from", "to", "limit"}},
		{Name: "list_billing_records", Method: http.MethodGet, Path: "/v1/commerce/billing-records", Description: "Return local pack-install and skill-usage billing records.", Query: []string{"pack_id", "skill", "status", "type", "currency", "from", "to", "limit"}},
		{Name: "get_commerce_snapshot", Method: http.MethodGet, Path: "/v1/commerce/snapshot", Description: "Return packs, install records, and billing records in one website-friendly snapshot.", Query: []string{"pack_id", "skill", "status", "type", "currency", "from", "to", "limit"}},
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
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-AGTX-Commerce-Token")
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
	case "/v1/commerce/install-plan":
		if !h.requireMethod(writer, request, http.MethodGet) {
			return
		}
		h.planCapabilityPackInstall(writer, request)
	case "/v1/commerce/install-pack":
		if !h.requireMethod(writer, request, http.MethodPost) {
			return
		}
		if !h.authorizeMutation(writer, request) {
			return
		}
		h.installCapabilityPack(writer, request)
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
		filtered := packs[:0]
		for _, pack := range packs {
			if normalizeName(pack.Pack.ID) == normalizeName(options.PackID) {
				filtered = append(filtered, pack)
			}
		}
		packs = filtered
	}
	h.writeOK(writer, packs)
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

func (h *commerceHTTPHandler) listInstallRecords(writer http.ResponseWriter, request *http.Request) {
	options, err := recordQueryOptionsFromURL(request)
	if err != nil {
		h.writeError(writer, httpStatusForError(err), err)
		return
	}
	records, err := h.service.ListInstallRecords(options)
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
		filtered := snapshot.Packs[:0]
		for _, pack := range snapshot.Packs {
			if normalizeName(pack.Pack.ID) == normalizeName(options.PackID) {
				filtered = append(filtered, pack)
			}
		}
		snapshot.Packs = filtered
	}
	h.writeOK(writer, snapshot)
}

func recordQueryOptionsFromURL(request *http.Request) (RecordQueryOptions, error) {
	query := request.URL.Query()
	options := RecordQueryOptions{
		PackID:   strings.TrimSpace(query.Get("pack_id")),
		Skill:    strings.TrimSpace(query.Get("skill")),
		Status:   strings.TrimSpace(query.Get("status")),
		Type:     strings.TrimSpace(query.Get("type")),
		Currency: strings.TrimSpace(query.Get("currency")),
		From:     strings.TrimSpace(query.Get("from")),
		To:       strings.TrimSpace(query.Get("to")),
		Limit:    100,
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
