package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultRegistrySkillsDeclareBillingMeters(t *testing.T) {
	registry := DefaultRegistry()
	expected := map[string][]string{
		"web_search": {"call"},
		"web_fetch":  {"page", "call"},
		"research":   {"task"},
		"ocr":        {"page"},
		"audio":      {"minute"},
		"imagen":     {"task", "credit"},
		"docx":       {"task"},
		"xlsx":       {"task"},
		"pptx":       {"task"},
		"pdf":        {"page"},
	}

	for name, meters := range expected {
		t.Run(name, func(t *testing.T) {
			skill, ok := registry.Find(name)
			if !ok {
				t.Fatalf("missing default skill %s", name)
			}
			if skill.VendorID != "agentex" {
				t.Fatalf("expected agentex vendor, got %q", skill.VendorID)
			}
			if skill.Capability == nil || skill.Capability.Class != "tool" {
				t.Fatalf("expected tool capability, got %#v", skill.Capability)
			}
			if skill.Billing == nil {
				t.Fatal("expected billing metadata")
			}
			for _, meter := range meters {
				if !hasBillingMeter(skill.Billing.Meters, meter) {
					t.Fatalf("expected meter %s in %#v", meter, skill.Billing.Meters)
				}
			}
			if skill.Billing.RevenueShare == nil || skill.Billing.RevenueShare.ISV != 70 || skill.Billing.RevenueShare.Platform != 30 {
				t.Fatalf("unexpected revenue share: %#v", skill.Billing.RevenueShare)
			}
		})
	}
}

func TestPlanInstallIncludesCommerceSummary(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	plan, err := service.PlanInstall([]string{"web_fetch"})
	if err != nil {
		t.Fatalf("plan install: %v", err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("expected one planned change, got %#v", plan)
	}
	commerce := plan.Changes[0].Commerce
	if commerce == nil {
		t.Fatal("expected commerce summary")
	}
	if commerce.VendorID != "agentex" || commerce.CapabilityClass != "tool" {
		t.Fatalf("unexpected commerce identity: %#v", commerce)
	}
	if len(commerce.BillingMeters) != 2 || commerce.BillingMeters[0] != "call" || commerce.BillingMeters[1] != "page" {
		t.Fatalf("unexpected billing meters: %#v", commerce.BillingMeters)
	}
}

func TestDefaultCapabilityPacksExposeStandardAndAdvanced(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	packs, err := service.ListCapabilityPacks()
	if err != nil {
		t.Fatalf("list capability packs: %v", err)
	}
	if len(packs) != 2 {
		t.Fatalf("expected standard and advanced packs, got %#v", packs)
	}
	if packs[0].Pack.ID != "standard" || packs[0].Pack.Tier != "standard" {
		t.Fatalf("expected standard pack first, got %#v", packs[0].Pack)
	}
	if packs[1].Pack.ID != "advanced" || packs[1].Pack.Tier != "advanced" {
		t.Fatalf("expected advanced pack second, got %#v", packs[1].Pack)
	}
	if packs[0].Installed || packs[1].Installed {
		t.Fatalf("fresh service should not have packs installed: %#v", packs)
	}
	if packs[0].Pack.Billing == nil || !hasBillingMeter(packs[0].Pack.Billing.Meters, "seat") || !hasBillingMeter(packs[0].Pack.Billing.Meters, "credit") {
		t.Fatalf("expected standard pack billing metadata: %#v", packs[0].Pack.Billing)
	}
}

func TestPlanCapabilityPackInstallShowsChangesAndBillingPreview(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	plan, err := service.PlanCapabilityPackInstall("standard")
	if err != nil {
		t.Fatalf("plan standard pack install: %v", err)
	}
	if plan.Action != "install_pack" || plan.Pack.Pack.ID != "standard" {
		t.Fatalf("unexpected pack plan identity: %#v", plan)
	}
	if len(plan.Changes) != len(plan.Pack.Pack.SkillNames) {
		t.Fatalf("expected one change per skill: %#v", plan.Changes)
	}
	if len(plan.BillingPreview) != 2 || len(plan.Totals) != 2 {
		t.Fatalf("expected seat and credit billing preview: %#v", plan)
	}
	if len(plan.Requires) != 1 || plan.Requires[0] != "confirmation" {
		t.Fatalf("expected confirmation requirement: %#v", plan.Requires)
	}
	records, err := service.ListInstallRecords(RecordQueryOptions{PackID: "standard"})
	if err != nil {
		t.Fatalf("list install records after plan: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("plan should not write install records: %#v", records)
	}
}

func TestPlanCapabilityPackInstallOmitsBillingPreviewWhenAlreadyInstalled(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	plan, err := service.PlanCapabilityPackInstall("standard")
	if err != nil {
		t.Fatalf("plan installed standard pack: %v", err)
	}
	if !plan.Pack.Installed || len(plan.BillingPreview) != 0 {
		t.Fatalf("installed pack should not have billing preview: %#v", plan)
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "already installed") {
		t.Fatalf("expected already installed warning: %#v", plan.Warnings)
	}
}

func TestInstallCapabilityPackRecordsInstallAndBilling(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	result, err := service.InstallCapabilityPack(context.Background(), "advanced")
	if err != nil {
		t.Fatalf("install advanced pack: %v", err)
	}
	if !result.Pack.Installed || result.Pack.Pack.ID != "advanced" {
		t.Fatalf("expected installed advanced pack: %#v", result.Pack)
	}
	if len(result.Results) != len(result.Pack.Pack.SkillNames) {
		t.Fatalf("expected install result per skill: results=%d skills=%d", len(result.Results), len(result.Pack.Pack.SkillNames))
	}
	if result.InstallRecord == nil || result.InstallRecord.PackID != "advanced" || result.InstallRecord.Action != "install_pack" {
		t.Fatalf("expected pack install record: %#v", result.InstallRecord)
	}
	if len(result.BillingRecords) != 2 {
		t.Fatalf("expected seat and credit billing records: %#v", result.BillingRecords)
	}
	records, err := service.ListInstallRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list install records: %v", err)
	}
	if len(records) != 1 || records[0].PackID != "advanced" || len(records[0].Skills) != len(result.Results) {
		t.Fatalf("unexpected install ledger: %#v", records)
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{PackID: "advanced"})
	if err != nil {
		t.Fatalf("list billing records: %v", err)
	}
	if len(billing.Records) != 2 || len(billing.Totals) != 2 {
		t.Fatalf("unexpected billing ledger: %#v", billing)
	}
	filteredInstalls, err := service.ListInstallRecords(RecordQueryOptions{PackID: "advanced", Status: "installed", From: result.InstallRecord.OccurredAt, To: result.InstallRecord.OccurredAt})
	if err != nil {
		t.Fatalf("filter install records: %v", err)
	}
	if len(filteredInstalls) != 1 || filteredInstalls[0].RecordID != result.InstallRecord.RecordID {
		t.Fatalf("unexpected filtered install records: %#v", filteredInstalls)
	}
	filteredBilling, err := service.ListBillingRecords(RecordQueryOptions{PackID: "advanced", Status: usageStatusLocalOnly, Type: "pack_install", Currency: "USD", From: billing.Records[0].OccurredAt, To: billing.Records[0].OccurredAt})
	if err != nil {
		t.Fatalf("filter billing records: %v", err)
	}
	if len(filteredBilling.Records) != 1 || filteredBilling.Records[0].Currency != "USD" || len(filteredBilling.Totals) != 1 {
		t.Fatalf("unexpected filtered billing records: %#v", filteredBilling)
	}
	if _, err := service.ListBillingRecords(RecordQueryOptions{From: "not-a-time"}); !IsErrorCode(err, CodeInvalidArgument) {
		t.Fatalf("expected invalid time filter error, got %v", err)
	}
}

func TestCommerceHTTPHandlerExposesWebsiteQueries(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	if _, err := service.InstallCapabilityPack(context.Background(), "standard"); err != nil {
		t.Fatalf("install standard pack: %v", err)
	}
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{AllowedOrigin: "https://site.example"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/commerce/snapshot?pack_id=standard&limit=5", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Origin", "https://site.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected snapshot status: %s", response.Status)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "https://site.example" {
		t.Fatalf("expected CORS header, got %q", response.Header.Get("Access-Control-Allow-Origin"))
	}
	var snapshotResponse struct {
		OK   bool                       `json:"ok"`
		Data CapabilityCommerceSnapshot `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshotResponse); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if !snapshotResponse.OK || len(snapshotResponse.Data.Packs) != 1 || snapshotResponse.Data.Packs[0].Pack.ID != "standard" {
		t.Fatalf("unexpected snapshot response: %#v", snapshotResponse)
	}
	if len(snapshotResponse.Data.InstallRecords) != 1 || len(snapshotResponse.Data.Billing.Records) != 2 {
		t.Fatalf("expected install and billing records in snapshot: %#v", snapshotResponse.Data)
	}

	response, err = http.Get(server.URL + "/v1/commerce/install-records?skill=pdf")
	if err != nil {
		t.Fatalf("get install records: %v", err)
	}
	defer response.Body.Close()
	var installsResponse struct {
		OK   bool            `json:"ok"`
		Data []InstallRecord `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&installsResponse); err != nil {
		t.Fatalf("decode install records: %v", err)
	}
	if !installsResponse.OK || len(installsResponse.Data) != 1 || !installRecordMatchesSkill(installsResponse.Data[0], "pdf") {
		t.Fatalf("unexpected install records response: %#v", installsResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/billing-records?pack_id=standard")
	if err != nil {
		t.Fatalf("get billing records: %v", err)
	}
	defer response.Body.Close()
	var billingResponse struct {
		OK   bool                    `json:"ok"`
		Data BillingRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&billingResponse); err != nil {
		t.Fatalf("decode billing records: %v", err)
	}
	if !billingResponse.OK || len(billingResponse.Data.Records) != 2 || len(billingResponse.Data.Totals) != 2 {
		t.Fatalf("unexpected billing records response: %#v", billingResponse)
	}

	response, err = http.Get(server.URL + "/v1/commerce/billing-records?pack_id=standard&type=pack_install&currency=USD&status=local_only")
	if err != nil {
		t.Fatalf("get filtered billing records: %v", err)
	}
	defer response.Body.Close()
	var filteredBillingResponse struct {
		OK   bool                    `json:"ok"`
		Data BillingRecordListResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&filteredBillingResponse); err != nil {
		t.Fatalf("decode filtered billing records: %v", err)
	}
	if !filteredBillingResponse.OK || len(filteredBillingResponse.Data.Records) != 1 || filteredBillingResponse.Data.Records[0].Currency != "USD" {
		t.Fatalf("unexpected filtered billing records response: %#v", filteredBillingResponse)
	}
}

func TestCommerceHTTPHandlerPlansCapabilityPackInstall(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/install-plan?pack_id=standard")
	if err != nil {
		t.Fatalf("get install plan: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected install plan status: %s", response.Status)
	}
	var planResponse struct {
		OK   bool                      `json:"ok"`
		Data CapabilityPackInstallPlan `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&planResponse); err != nil {
		t.Fatalf("decode install plan: %v", err)
	}
	if !planResponse.OK || planResponse.Data.Pack.Pack.ID != "standard" || len(planResponse.Data.Changes) == 0 || len(planResponse.Data.BillingPreview) != 2 {
		t.Fatalf("unexpected install plan response: %#v", planResponse)
	}
}

func TestCommerceHTTPHandlerPlanRequiresPackID(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/install-plan")
	if err != nil {
		t.Fatalf("get install plan without pack id: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", response.Status)
	}
}

func TestCommerceHTTPHandlerInstallsCapabilityPackWithConfirmation(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/install-pack", bytes.NewBufferString(`{"pack_id":"advanced","yes":true}`))
	if err != nil {
		t.Fatalf("new install request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post install pack: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected install status: %s", response.Status)
	}
	var installResponse struct {
		OK   bool                        `json:"ok"`
		Data CapabilityPackInstallResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&installResponse); err != nil {
		t.Fatalf("decode install response: %v", err)
	}
	if !installResponse.OK || installResponse.Data.Pack.Pack.ID != "advanced" || !installResponse.Data.Pack.Installed {
		t.Fatalf("unexpected install response: %#v", installResponse)
	}
	if installResponse.Data.InstallRecord == nil || installResponse.Data.InstallRecord.PackID != "advanced" || len(installResponse.Data.BillingRecords) != 2 {
		t.Fatalf("expected install and billing records in response: %#v", installResponse.Data)
	}

	response, err = http.Get(server.URL + "/v1/commerce/snapshot?pack_id=advanced")
	if err != nil {
		t.Fatalf("get snapshot after install: %v", err)
	}
	defer response.Body.Close()
	var snapshotResponse struct {
		OK   bool                       `json:"ok"`
		Data CapabilityCommerceSnapshot `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshotResponse); err != nil {
		t.Fatalf("decode snapshot after install: %v", err)
	}
	if !snapshotResponse.OK || len(snapshotResponse.Data.InstallRecords) != 1 || len(snapshotResponse.Data.Billing.Records) != 2 {
		t.Fatalf("expected installed pack in website snapshot: %#v", snapshotResponse.Data)
	}
}

func TestCommerceHTTPHandlerServesDashboard(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "dashboard-token"}))
	defer server.Close()

	response, err := http.Get(server.URL + "/commerce")
	if err != nil {
		t.Fatalf("get commerce dashboard: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected dashboard status: %s", response.Status)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected html content type, got %q", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	if !bytes.Contains(body, []byte("agtx Commerce")) || !bytes.Contains(body, []byte("/v1/commerce/install-pack")) || !bytes.Contains(body, []byte("dashboard-token")) {
		t.Fatalf("dashboard missing expected content: %s", string(body))
	}
}

func TestCommerceHTTPHandlerInstallRequiresConfirmation(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/commerce/install-pack", bytes.NewBufferString(`{"pack_id":"standard"}`))
	if err != nil {
		t.Fatalf("new install request without confirmation: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-AGTX-Commerce-Token", "token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post install pack without confirmation: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("expected confirmation status, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Action   string `json:"action"`
				Expected string `json:"expected"`
				Pack     string `json:"pack"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode confirmation response: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeConfirmationRequired || errorResponse.Error.Details.Action != "install-pack" || errorResponse.Error.Details.Expected != "yes=true" || errorResponse.Error.Details.Pack != "standard" {
		t.Fatalf("unexpected confirmation response: %#v", errorResponse)
	}
}

func TestCommerceHTTPHandlerInstallRequiresMutationToken(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{MutationToken: "token"}))
	defer server.Close()

	response, err := http.Post(server.URL+"/v1/commerce/install-pack", "application/json", bytes.NewBufferString(`{"pack_id":"standard","yes":true}`))
	if err != nil {
		t.Fatalf("post install pack without token: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Header string `json:"header"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode unauthorized response: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeUnauthorized || errorResponse.Error.Details.Header != "X-AGTX-Commerce-Token" {
		t.Fatalf("unexpected unauthorized response: %#v", errorResponse)
	}
}

func TestCommerceHTTPHandlerRejectsBadLimit(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/snapshot?limit=bad")
	if err != nil {
		t.Fatalf("get bad snapshot: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Parameter string `json:"parameter"`
				Reason    string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeInvalidArgument || errorResponse.Error.Details.Parameter != "limit" || errorResponse.Error.Details.Reason != "invalid_positive_integer" {
		t.Fatalf("unexpected error response: %#v", errorResponse)
	}
}

func TestCommerceHTTPHandlerRejectsBadTimeFilter(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	server := httptest.NewServer(service.CommerceHTTPHandler(CommerceHTTPOptions{}))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/commerce/billing-records?from=bad")
	if err != nil {
		t.Fatalf("get bad time filter: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", response.Status)
	}
	var errorResponse struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("decode time filter error: %v", err)
	}
	if errorResponse.OK || errorResponse.Error.Code != CodeInvalidArgument || errorResponse.Error.Details.Field != "from" {
		t.Fatalf("unexpected time filter error: %#v", errorResponse)
	}
}

func TestCapabilityPackAliasesSupportChineseNames(t *testing.T) {
	service := NewService(PathsForRoot(t.TempDir()))
	standard, err := service.GetCapabilityPack("\u666e\u901a")
	if err != nil {
		t.Fatalf("lookup standard alias: %v", err)
	}
	if standard.Pack.ID != "standard" {
		t.Fatalf("expected standard alias, got %#v", standard.Pack)
	}
	advanced, err := service.GetCapabilityPack("\u9ad8\u7ea7")
	if err != nil {
		t.Fatalf("lookup advanced alias: %v", err)
	}
	if advanced.Pack.ID != "advanced" {
		t.Fatalf("expected advanced alias, got %#v", advanced.Pack)
	}
}

func TestRunSkillRecordsLocalUsageEvents(t *testing.T) {
	service := installBillableEchoSkill(t, nil)
	result, err := service.RunSkill(context.Background(), "echo_metered", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("run billable skill: %v", err)
	}
	if result.InvocationID == "" {
		t.Fatal("expected invocation id")
	}
	if len(result.UsageEvents) != 1 {
		t.Fatalf("expected one usage event, got %#v", result.UsageEvents)
	}
	if result.UsageEvents[0].Meter != "call" || result.UsageEvents[0].Quantity != 1 {
		t.Fatalf("unexpected first usage event: %#v", result.UsageEvents[0])
	}
	if result.UsageEvents[0].Status != usageStatusLocalOnly || result.UsageEvents[0].PackID != "echo_metered" || result.UsageEvents[0].VersionID != "1.0.0" {
		t.Fatalf("unexpected local usage event: %#v", result.UsageEvents[0])
	}
	if result.UsageEvents[0].UnitPriceMinor != 3 || result.UsageEvents[0].GrossAmountMinor != 3 {
		t.Fatalf("unexpected local amount: %#v", result.UsageEvents[0])
	}
	billing, err := service.ListBillingRecords(RecordQueryOptions{Skill: "echo_metered"})
	if err != nil {
		t.Fatalf("list usage billing records: %v", err)
	}
	if len(billing.Records) != 1 || billing.Records[0].Type != "skill_usage" || billing.Records[0].UsageEventID != result.UsageEvents[0].EventID {
		t.Fatalf("expected usage billing record: %#v", billing)
	}
}

func TestRunSkillReportsUsageEventsToPro(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotDevice string
	var got commerceUsageRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotAuth = request.Header.Get("Authorization")
		gotDevice = request.Header.Get("X-AGTX-Device-ID")
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			t.Fatalf("decode usage request: %v", err)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"ok":true,"event_id":"server-event","status":"recorded","gross_amount_minor":42}`))
	}))
	defer server.Close()

	service := installBillableEchoSkill(t, func(service *Service) {
		service.Config.ProAPIURL = server.URL
		service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device"}
		if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
			t.Fatalf("save auth: %v", err)
		}
	})
	result, err := service.RunSkill(context.Background(), "echo_metered", nil, nil)
	if err != nil {
		t.Fatalf("run billable skill: %v", err)
	}
	if gotPath != "/v1/commerce/usage" || gotAuth != "Bearer access" || gotDevice != "device" {
		t.Fatalf("unexpected usage request path/auth/device: path=%q auth=%q device=%q", gotPath, gotAuth, gotDevice)
	}
	if got.EventID == "" || got.PackID != "echo_metered" || got.VersionID != "1.0.0" || got.VendorID != "agentex" || got.Meter != "call" {
		t.Fatalf("unexpected usage payload: %#v", got)
	}
	if got.InvocationID == "" || got.InvocationID != result.InvocationID {
		t.Fatalf("expected invocation id in payload: request=%#v result=%#v", got, result)
	}
	if got.Quantity != 1 || got.UnitPriceMinor != 3 || got.GrossAmountMinor != 3 || got.Currency != "AGTX_CREDIT" {
		t.Fatalf("unexpected usage amount payload: %#v", got)
	}
	if got.Evidence["source"] != "agtx_cli" || got.Evidence["skill"] != "echo_metered" {
		t.Fatalf("unexpected usage evidence: %#v", got.Evidence)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Status != "recorded" || result.UsageEvents[0].EventID != "server-event" || result.UsageEvents[0].GrossAmountMinor != 42 {
		t.Fatalf("unexpected reported usage events: %#v", result.UsageEvents)
	}
}

func TestRunSkillUsageReportFailureDoesNotFailRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"code":"internal_error","message":"usage down"}}`))
	}))
	defer server.Close()

	service := installBillableEchoSkill(t, func(service *Service) {
		service.Config.ProAPIURL = server.URL
		service.Auth = AuthState{SchemaVersion: 1, AccessToken: "access", DeviceID: "device"}
		if err := SaveAuth(service.Paths.AuthFile, service.Auth); err != nil {
			t.Fatalf("save auth: %v", err)
		}
	})
	result, err := service.RunSkill(context.Background(), "echo_metered", nil, nil)
	if err != nil {
		t.Fatalf("run should succeed when usage reporting fails: %v", err)
	}
	if result.ExitCode != 0 || result.Stdout == "" {
		t.Fatalf("unexpected run result: %#v", result)
	}
	if len(result.UsageEvents) != 1 || result.UsageEvents[0].Status != usageStatusReportFailed || result.UsageEvents[0].Error == "" {
		t.Fatalf("expected usage report failure in result: %#v", result.UsageEvents)
	}
}

func TestValidateRegistryAcceptsCommerceMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{
  "schema_version": 1,
  "skills": [
    {
      "schema_version": 1,
      "name": "invoice_leads",
      "version": "1.0.0",
      "vendor_id": "demo_isv",
      "capability": {
        "class": "commerce",
        "use_when": "Use when an agent should qualify invoice-management leads."
      },
      "summary": "Invoice lead workflow",
      "description": "Qualifies invoice-management leads and attributes downstream purchases.",
      "tags": ["commerce", "invoice"],
      "platforms": [{"os": "darwin", "arch": "arm64"}],
      "billing": {
        "meters": [
          {
            "meter": "success",
            "unit_price": 0,
            "currency": "AGTX_CREDIT",
            "free_quota": 0,
            "hard_limit_supported": true,
            "refund_policy": "Do not bill rejected leads."
          }
        ],
        "revenue_share": {
          "isv": 70,
          "platform": 30,
          "basis": "net_revenue_after_payment_processor_tax_and_refunds"
        }
      },
      "attribution": {
        "events": ["lead_created", "purchase_completed"],
        "default_window_days": {"cpa": 30, "cps": 45},
        "default_cps_rate": 15,
        "renewal_cps": "disabled_by_default"
      },
      "support": {
        "url": "https://example.com/support",
        "privacy_url": "https://example.com/privacy",
        "incident_email": "security@example.com"
      },
      "stub": true
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	result, err := ValidateRegistryFile(path)
	if err != nil {
		t.Fatalf("validate registry: %v", err)
	}
	if !result.OK || result.Skills != 1 {
		t.Fatalf("unexpected validation result: %#v", result)
	}
}

func TestValidateRegistryRejectsUnsupportedCommerceMetadata(t *testing.T) {
	tests := map[string]string{
		"capability class":  `"capability":{"class":"unsafe"}`,
		"billing meter":     `"billing":{"meters":[{"meter":"mystery"}]}`,
		"attribution event": `"attribution":{"events":["unknown_event"]}`,
	}

	for name, fragment := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.json")
			data := `{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],` + fragment + `,"stub":true}]}`
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatalf("write registry: %v", err)
			}
			if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestValidateRegistryRequiresSupportForThirdPartyMonetizedSkills(t *testing.T) {
	tests := map[string]string{
		"missing support": `"vendor_id":"demo_isv","billing":{"meters":[{"meter":"call"}]}`,
		"missing privacy": `"vendor_id":"demo_isv","billing":{"meters":[{"meter":"call"}]},"support":{"url":"https://example.com/support","incident_email":"security@example.com"}`,
		"bad support url": `"vendor_id":"demo_isv","billing":{"meters":[{"meter":"call"}]},"support":{"url":"ftp://example.com/support","privacy_url":"https://example.com/privacy","incident_email":"security@example.com"}`,
	}

	for name, fragment := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.json")
			data := `{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0",` + fragment + `,"summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"stub":true}]}`
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatalf("write registry: %v", err)
			}
			if _, err := ValidateRegistryFile(path); !IsErrorCode(err, CodeInvalidArgument) {
				t.Fatalf("expected invalid argument, got %v", err)
			}
		})
	}
}

func TestValidateRegistryAllowsFirstPartyBillingWithoutSupport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"skills":[{"schema_version":1,"name":"demo","version":"1.0.0","vendor_id":"agentex","summary":"Demo","description":"Demo","platforms":[{"os":"darwin","arch":"arm64"}],"billing":{"meters":[{"meter":"call"}]},"stub":true}]}`), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := ValidateRegistryFile(path); err != nil {
		t.Fatalf("first-party billing should not require explicit support metadata: %v", err)
	}
}

func TestCommerceManifestExamplesValidate(t *testing.T) {
	examplesDir := filepath.Join("..", "..", "docs", "standards", "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected commerce manifest examples")
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(examplesDir, entry.Name()))
			if err != nil {
				t.Fatalf("read example: %v", err)
			}
			var manifest SkillManifest
			if err := decodeJSONStrict(data, &manifest); err != nil {
				t.Fatalf("decode example: %v", err)
			}
			if _, err := validateSkillManifest(manifest); err != nil {
				t.Fatalf("validate example: %v", err)
			}
			if manifest.VendorID == "" || manifest.Capability == nil || manifest.Billing == nil || manifest.Support == nil {
				t.Fatalf("example should include vendor, capability, billing, and support metadata: %#v", manifest)
			}
		})
	}
}

func hasBillingMeter(meters []BillingMeter, want string) bool {
	for _, meter := range meters {
		if meter.Meter == want {
			return true
		}
	}
	return false
}

func installBillableEchoSkill(t *testing.T, configure func(*Service)) *Service {
	t.Helper()
	root := t.TempDir()
	entrypoint := "echo.sh"
	if runtime.GOOS == "windows" {
		entrypoint = "echo.bat"
	}
	archivePath := filepath.Join(root, "echo.zip")
	sum := writeEchoPackage(t, archivePath, entrypoint)
	service := NewService(PathsForRoot(root))
	if configure != nil {
		configure(service)
	}
	service.Registry = Registry{SchemaVersion: 1, Skills: []SkillManifest{{
		SchemaVersion: 1,
		Name:          "echo_metered",
		Version:       "1.0.0",
		VendorID:      "agentex",
		Capability:    &CapabilityInfo{Class: "tool"},
		Summary:       "Echo",
		Description:   "Echo test package",
		Platforms: []PlatformBundle{{
			OS:         runtime.GOOS,
			Arch:       runtime.GOARCH,
			URL:        archivePath,
			SHA256:     sum,
			Archive:    "zip",
			Entrypoint: entrypoint,
		}},
		Billing: &BillingInfo{
			Meters: []BillingMeter{
				{Meter: "call", Currency: "AGTX_CREDIT", UnitPrice: 3, RefundPolicy: "Do not bill failed invocations."},
			},
			RevenueShare: &RevenueShare{ISV: 70, Platform: 30, Basis: "net_revenue_after_payment_processor_tax_and_refunds"},
		},
	}}}
	if _, err := service.InstallSkills(context.Background(), []string{"echo_metered"}); err != nil {
		t.Fatalf("install billable skill: %v", err)
	}
	return service
}
