package core

import (
	"encoding/json"
	"strings"
)

func commerceDashboardHTML(mutationToken string) []byte {
	token, err := json.Marshal(mutationToken)
	if err != nil {
		token = []byte(`""`)
	}
	page := strings.Replace(commerceDashboardPage, "__MUTATION_TOKEN__", string(token), 1)
	return []byte(page)
}

const commerceDashboardPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>agtx Commerce</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f5f7fb;
      --panel: #ffffff;
      --ink: #1b2430;
      --muted: #687386;
      --line: #dfe5ee;
      --accent: #176f62;
      --accent-2: #2f5cbe;
      --warn: #a85b00;
      --bad: #b42318;
      --shadow: 0 10px 28px rgba(24, 36, 52, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-width: 320px;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.45 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    button, select, input { font: inherit; }
    button {
      min-height: 34px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--ink);
      padding: 0 12px;
      cursor: pointer;
    }
    button:hover { border-color: #b8c4d6; }
    button.primary {
      border-color: var(--accent);
      background: var(--accent);
      color: #fff;
    }
    button.primary:disabled {
      border-color: #cad2de;
      background: #cad2de;
      cursor: default;
    }
    input, select {
      min-height: 34px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      color: var(--ink);
      padding: 0 10px;
    }
    .topbar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 18px 24px;
      background: #fff;
      border-bottom: 1px solid var(--line);
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 12px;
      min-width: 0;
    }
    .mark {
      display: grid;
      place-items: center;
      flex: 0 0 38px;
      width: 38px;
      height: 38px;
      border-radius: 8px;
      background: linear-gradient(135deg, #176f62, #2f5cbe);
      color: #fff;
      font-weight: 760;
    }
    h1, h2, h3, p { margin: 0; }
    h1 { font-size: 20px; letter-spacing: 0; }
    h2 { font-size: 16px; letter-spacing: 0; }
    h3 { font-size: 14px; letter-spacing: 0; }
    .subtle { color: var(--muted); font-size: 12px; }
    .status {
      min-width: 112px;
      text-align: center;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 7px 12px;
      background: #fbfcfe;
      color: var(--muted);
      white-space: nowrap;
    }
    main {
      max-width: 1180px;
      margin: 0 auto;
      padding: 18px 20px 28px;
    }
    .metrics {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 16px;
    }
    .metric, .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
    }
    .metric {
      min-height: 82px;
      padding: 14px;
    }
    .metric .value {
      margin-top: 6px;
      font-size: 24px;
      font-weight: 760;
    }
    .workspace {
      display: grid;
      grid-template-columns: minmax(0, 1.08fr) minmax(360px, 0.92fr);
      gap: 16px;
      align-items: start;
    }
    .panel { overflow: hidden; }
    .scenarios-panel { margin-bottom: 16px; }
    .panel-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      min-height: 58px;
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
    }
    .toolbar {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }
    .pack-list {
      display: grid;
      gap: 12px;
      padding: 14px;
    }
    .scenario-list {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px;
      padding: 14px;
    }
    .scenario {
      display: grid;
      gap: 10px;
      min-height: 196px;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 14px;
      background: #fff;
    }
    .scenario.ready { border-color: rgba(23, 111, 98, 0.45); }
    .scenario-top {
      display: flex;
      justify-content: space-between;
      gap: 10px;
      align-items: flex-start;
    }
    .scenario-title {
      display: grid;
      gap: 4px;
      min-width: 0;
    }
    .scenario-title h3 {
      overflow-wrap: anywhere;
    }
    .scenario-meta {
      display: grid;
      gap: 4px;
      color: var(--muted);
      font-size: 12px;
    }
    .scenario-meta span {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .scenario-actions {
      display: flex;
      justify-content: flex-end;
      align-items: center;
    }
    .pack {
      border: 1px solid var(--line);
      border-radius: 8px;
      overflow: hidden;
      background: #fff;
    }
    .pack.installed { border-color: rgba(23, 111, 98, 0.45); }
    .pack-main {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px;
      padding: 14px;
    }
    .pack-title {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 0;
      flex-wrap: wrap;
    }
    .badge {
      display: inline-flex;
      align-items: center;
      min-height: 22px;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 0 8px;
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
    }
    .badge.good {
      border-color: rgba(23, 111, 98, 0.32);
      background: rgba(23, 111, 98, 0.08);
      color: var(--accent);
    }
    .pack-summary { margin-top: 6px; color: var(--muted); }
    .skills {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 1px;
      background: var(--line);
      border-top: 1px solid var(--line);
    }
    .skill {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      min-height: 36px;
      padding: 8px 10px;
      background: #fbfcfe;
      min-width: 0;
    }
    .skill span:first-child {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .tabs {
      display: flex;
      border-bottom: 1px solid var(--line);
      background: #fbfcfe;
    }
    .tabs button {
      flex: 1;
      border: 0;
      border-right: 1px solid var(--line);
      border-radius: 0;
      background: transparent;
    }
    .tabs button.active {
      background: #fff;
      color: var(--accent-2);
      font-weight: 700;
    }
    .filters {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
    }
    .records {
      display: grid;
      max-height: 620px;
      overflow: auto;
    }
    .record {
      display: grid;
      gap: 5px;
      min-height: 68px;
      padding: 12px 14px;
      border-bottom: 1px solid var(--line);
      background: #fff;
    }
    .record:last-child { border-bottom: 0; }
    .record-row {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      min-width: 0;
    }
    .record-row strong, .record-row span {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .amount { color: var(--accent-2); font-weight: 700; }
    .empty {
      padding: 22px 14px;
      color: var(--muted);
      text-align: center;
    }
    .toast {
      position: fixed;
      right: 18px;
      bottom: 18px;
      display: none;
      max-width: min(420px, calc(100vw - 36px));
      border: 1px solid var(--line);
      border-left: 4px solid var(--accent);
      border-radius: 8px;
      padding: 12px 14px;
      background: #fff;
      box-shadow: var(--shadow);
      color: var(--ink);
    }
    .toast.bad { border-left-color: var(--bad); }
    @media (max-width: 860px) {
      .topbar { align-items: flex-start; flex-direction: column; }
      .metrics, .workspace, .scenario-list { grid-template-columns: 1fr; }
      .status { text-align: left; }
    }
    @media (max-width: 560px) {
      main { padding: 14px 12px 22px; }
      .metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .pack-main { grid-template-columns: 1fr; }
      .skills { grid-template-columns: 1fr; }
      .filters { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header class="topbar">
    <div class="brand">
      <div class="mark">A</div>
      <div>
        <h1>agtx Commerce</h1>
        <p class="subtle" id="generated">Loading</p>
      </div>
    </div>
    <div class="status" id="status">Syncing</div>
  </header>
  <main>
    <section class="metrics" id="metrics"></section>
    <section class="panel scenarios-panel">
      <div class="panel-head">
        <h2>Task Scenarios</h2>
      </div>
      <div class="scenario-list" id="scenarios"></div>
    </section>
    <section class="workspace">
      <section class="panel">
        <div class="panel-head">
          <h2>Capability Packs</h2>
          <button id="refresh">Refresh</button>
        </div>
        <div class="pack-list" id="packs"></div>
      </section>
      <section class="panel">
        <div class="tabs">
          <button class="active" data-tab="billing">Billing</button>
          <button data-tab="installs">Installs</button>
        </div>
        <div class="filters">
          <select id="packFilter"></select>
          <select id="scenarioFilter"></select>
          <input id="statusFilter" placeholder="status">
          <select id="typeFilter">
            <option value="">All types</option>
            <option value="pack_install">pack_install</option>
            <option value="skill_usage">skill_usage</option>
          </select>
          <input id="currencyFilter" placeholder="currency">
          <input id="fromFilter" type="datetime-local">
          <input id="toFilter" type="datetime-local">
          <input id="limit" type="number" min="1" max="500" value="100">
        </div>
        <div class="records" id="records"></div>
      </section>
    </section>
  </main>
  <div class="toast" id="toast"></div>
  <script>
    const mutationToken = __MUTATION_TOKEN__;
    const state = { packs: [], allPacks: [], scenarios: [], allScenarios: [], installs: [], billing: { records: [], totals: [] }, scenarioLedger: null, activeTab: "billing" };
    const el = (id) => document.getElementById(id);
    const esc = (value) => String(value == null ? "" : value).replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;" }[ch]));

    async function api(path, options) {
      const response = await fetch(path, options);
      const payload = await response.json();
      if (!payload.ok) {
        const err = payload.error || { message: "Request failed" };
        throw new Error(err.message || err.code || "Request failed");
      }
      return payload.data;
    }

    async function load() {
      setStatus("Syncing");
      const params = new URLSearchParams();
      const pack = el("packFilter").value;
      const scenario = el("scenarioFilter").value;
      const limit = Number(el("limit").value || 100);
      if (pack) params.set("pack_id", pack);
      if (scenario) params.set("scenario_id", scenario);
      if (el("statusFilter").value.trim()) params.set("status", el("statusFilter").value.trim());
      if (el("typeFilter").value) params.set("type", el("typeFilter").value);
      if (el("currencyFilter").value.trim()) params.set("currency", el("currencyFilter").value.trim());
      const from = dateTimeLocalToRFC3339(el("fromFilter").value);
      const to = dateTimeLocalToRFC3339(el("toFilter").value);
      if (from) params.set("from", from);
      if (to) params.set("to", to);
      params.set("limit", String(limit > 0 ? limit : 100));
      const dataPath = scenario ? "/v1/commerce/scenario-ledger?" + params.toString() : "/v1/commerce/snapshot?" + params.toString();
      const [data, allPacks, allScenarios] = await Promise.all([
        api(dataPath),
        api("/v1/commerce/packs"),
        api("/v1/commerce/scenarios")
      ]);
      if (scenario) {
        state.scenarioLedger = data;
        state.packs = data.scenario && data.scenario.recommended_pack ? [data.scenario.recommended_pack] : [];
        state.scenarios = data.scenario ? [data.scenario] : [];
      } else {
        state.scenarioLedger = null;
        state.packs = data.packs || [];
        state.scenarios = data.scenarios || [];
      }
      state.allPacks = allPacks || [];
      state.allScenarios = allScenarios || [];
      state.installLedger = Array.isArray(data.install_records) ? { records: data.install_records } : (data.install_records || { records: [] });
      state.integrity = data.integrity || [];
      state.installs = state.installLedger.records || [];
      state.billing = data.billing || { records: [], totals: [] };
      render(data.generated_at);
      setStatus("Ready");
    }

    function render(generatedAt) {
      el("generated").textContent = generatedAt ? "Updated " + generatedAt : "Updated";
      renderMetrics();
      renderPackFilter();
      renderScenarioFilter();
      renderScenarios();
      renderPacks();
      renderRecords();
    }

    function renderMetrics() {
      const installed = state.packs.filter((item) => item.installed).length;
      const totals = state.billing.totals || [];
      const totalLabel = totals.length ? totals.map((item) => esc(item.currency) + " " + esc(item.gross_amount_minor)).join(" / ") : "0";
      el("metrics").innerHTML = [
        metric("Packs", state.packs.length),
        metric("Scenarios", state.scenarios.length),
        metric("Installed", installed),
        metric("Billing Total", totalLabel)
      ].join("");
    }

    function metric(label, value) {
      return '<article class="metric"><p class="subtle">' + esc(label) + '</p><div class="value">' + esc(value) + '</div></article>';
    }

    function renderPackFilter() {
      const select = el("packFilter");
      const selected = select.value;
      const options = ['<option value="">All packs</option>'].concat(state.allPacks.map((item) => {
        const id = item.pack && item.pack.id ? item.pack.id : "";
        return '<option value="' + esc(id) + '">' + esc(id || "pack") + '</option>';
      }));
      select.innerHTML = options.join("");
      select.value = selected;
    }

    function renderScenarioFilter() {
      const select = el("scenarioFilter");
      const selected = select.value;
      const options = ['<option value="">All scenarios</option>'].concat(state.allScenarios.map((item) => {
        const scenario = item.scenario || {};
        const id = scenario.id || "";
        return '<option value="' + esc(id) + '">' + esc(scenario.name || id || "scenario") + '</option>';
      }));
      select.innerHTML = options.join("");
      select.value = selected;
    }

    function renderScenarios() {
      if (!state.scenarios.length) {
        el("scenarios").innerHTML = '<div class="empty">No task scenarios found.</div>';
        return;
      }
      el("scenarios").innerHTML = state.scenarios.map((item) => {
        const scenario = item.scenario || {};
        const pack = item.recommended_pack && item.recommended_pack.pack ? item.recommended_pack.pack : {};
        const packInstalled = item.recommended_pack && item.recommended_pack.installed;
        const missing = item.missing_skills || [];
        const totals = item.billing_preview_totals || [];
        const totalLabel = totals.length ? totals.map((total) => esc(total.currency) + " " + esc(total.gross_amount_minor)).join(" / ") : "0";
        const status = item.ready ? "Ready" : "Needs install";
        const inputs = scenario.inputs || [];
        const deliverables = scenario.deliverables || [];
        const workflow = scenario.workflow || [];
        const inputLabel = inputs.length ? inputs.slice(0, 2).map((input) => input.label || input.id).join(", ") : "none";
        const deliverableLabel = deliverables.length ? deliverables.slice(0, 2).map((item) => item.label || item.id).join(", ") : "none";
        const workflowLabel = workflow.length ? workflow.slice(0, 2).map((step) => step.title || step.id).join(" -> ") : "none";
        const button = packInstalled
          ? '<button class="primary" disabled>Installed</button>'
          : '<button class="primary" data-install-scenario="' + esc(scenario.id) + '">Install</button>';
        const missingLabel = missing.length ? missing.slice(0, 4).map((skill) => skill.name).join(", ") : "none";
        return '<article class="scenario ' + (item.ready ? "ready" : "") + '">' +
          '<div class="scenario-top">' +
            '<div class="scenario-title"><h3>' + esc(scenario.name || scenario.id) + '</h3><div><span class="badge">' + esc(scenario.industry || "scenario") + '</span> <span class="badge">' + esc(pack.id || scenario.recommended_pack_id) + '</span> <span class="badge ' + (item.ready ? "good" : "") + '">' + status + '</span></div></div>' +
            '<div class="scenario-actions">' + button + '</div>' +
          '</div>' +
          '<p class="pack-summary">' + esc(scenario.summary || "") + '</p>' +
          '<div class="scenario-meta">' +
            '<span>inputs: ' + esc(inputLabel) + '</span>' +
            '<span>deliverables: ' + esc(deliverableLabel) + '</span>' +
            '<span>steps: ' + esc(workflowLabel) + '</span>' +
          '</div>' +
          '<div class="record-row subtle"><span>missing: ' + esc(missingLabel) + '</span><span>' + esc(totalLabel) + '</span></div>' +
        '</article>';
      }).join("");
      el("scenarios").querySelectorAll("[data-install-scenario]").forEach((button) => {
        button.addEventListener("click", () => installScenario(button.getAttribute("data-install-scenario")));
      });
    }

    function renderPacks() {
      if (!state.packs.length) {
        el("packs").innerHTML = '<div class="empty">No packs found.</div>';
        return;
      }
      el("packs").innerHTML = state.packs.map((item) => {
        const pack = item.pack || {};
        const skills = item.skills || [];
        const status = item.installed ? "Installed" : "Available";
        const button = item.installed
          ? '<button class="primary" disabled>Installed</button>'
          : '<button class="primary" data-install="' + esc(pack.id) + '">Install</button>';
        return '<article class="pack ' + (item.installed ? "installed" : "") + '">' +
          '<div class="pack-main">' +
            '<div><div class="pack-title"><h3>' + esc(pack.name || pack.id) + '</h3><span class="badge">' + esc(pack.tier) + '</span><span class="badge ' + (item.installed ? "good" : "") + '">' + status + '</span></div>' +
            '<p class="pack-summary">' + esc(pack.summary) + '</p></div>' +
            '<div>' + button + '</div>' +
          '</div>' +
          '<div class="skills">' + skills.map((skill) => '<div class="skill"><span>' + esc(skill.name) + '</span><span class="badge ' + (skill.installed ? "good" : "") + '">' + (skill.installed ? "on" : "off") + '</span></div>').join("") + '</div>' +
        '</article>';
      }).join("");
      el("packs").querySelectorAll("[data-install]").forEach((button) => {
        button.addEventListener("click", () => installPack(button.getAttribute("data-install")));
      });
    }

    function renderRecords() {
      const records = state.activeTab === "billing" ? (state.billing.records || []) : state.installs;
      const title = state.activeTab === "billing" ? "No billing records found." : "No install records found.";
      if (!records.length) {
        el("records").innerHTML = '<div class="empty">' + title + '</div>';
        return;
      }
      el("records").innerHTML = records.map((record) => state.activeTab === "billing" ? billingRecord(record) : installRecord(record)).join("");
    }

    function billingRecord(record) {
      const target = record.skill_name || record.pack_id || "-";
      const amount = (record.currency || "-") + " " + (record.gross_amount_minor || 0);
      const scenario = record.scenario_id ? "scenario=" + record.scenario_id : "scenario=-";
      const integrity = recordIntegrityLabel(record);
      return '<article class="record">' +
        '<div class="record-row"><strong>' + esc(target) + '</strong><span class="amount">' + esc(amount) + '</span></div>' +
        '<div class="record-row subtle"><span>' + esc(record.type || "-") + " / " + esc(record.meter || "-") + '</span><span>' + esc(record.occurred_at || "") + '</span></div>' +
        '<div class="record-row subtle"><span>' + esc(record.status || "-") + '</span><span>' + esc(scenario) + '</span></div>' +
        '<div class="record-row subtle"><span>' + esc(integrity) + '</span><span>' + esc(record.record_id || "") + '</span></div>' +
      '</article>';
    }

    function installRecord(record) {
      const target = record.pack_id || record.skill_name || "-";
      const skills = (record.skills || []).map((skill) => skill.name).join(", ");
      const scenario = record.scenario_id ? "scenario=" + record.scenario_id : "scenario=-";
      const integrity = recordIntegrityLabel(record);
      return '<article class="record">' +
        '<div class="record-row"><strong>' + esc(target) + '</strong><span>' + esc(record.status || "-") + '</span></div>' +
        '<div class="record-row subtle"><span>' + esc(record.action || "-") + '</span><span>' + esc(record.occurred_at || "") + '</span></div>' +
        '<div class="record-row subtle"><span>' + esc(skills || "-") + '</span><span>' + esc(scenario) + '</span></div>' +
        '<div class="record-row subtle"><span>' + esc(integrity) + '</span><span>' + esc(record.record_id || "") + '</span></div>' +
      '</article>';
    }

    function recordIntegrityLabel(record) {
      const integrity = record.integrity || {};
      if (!integrity.status) return "integrity=-";
      return "integrity=" + integrity.status;
    }

    async function installPack(packID) {
      try {
        setStatus("Installing");
        const plan = await api("/v1/commerce/install-plan?pack_id=" + encodeURIComponent(packID));
        const total = (plan.totals || []).map((item) => item.currency + " " + item.gross_amount_minor).join(" / ") || "0";
        const changes = (plan.changes || []).length;
        const ok = window.confirm("Install " + packID + "?\n\nSkill changes: " + changes + "\nBilling preview: " + total);
        if (!ok) {
          setStatus("Ready");
          return;
        }
        await api("/v1/commerce/install-pack", {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-AGTX-Commerce-Token": mutationToken },
          body: JSON.stringify({ pack_id: packID, yes: true })
        });
        toast("Installed " + packID);
        await load();
      } catch (err) {
        setStatus("Ready");
        toast(err.message, true);
      }
    }

    async function installScenario(scenarioID) {
      try {
        setStatus("Installing");
        const plan = await api("/v1/commerce/scenario-install-plan?scenario_id=" + encodeURIComponent(scenarioID));
        const packID = plan.scenario && plan.scenario.recommended_pack && plan.scenario.recommended_pack.pack ? plan.scenario.recommended_pack.pack.id : "";
        const total = plan.pack_plan && plan.pack_plan.totals ? plan.pack_plan.totals.map((item) => item.currency + " " + item.gross_amount_minor).join(" / ") : "0";
        const changes = plan.pack_plan && plan.pack_plan.changes ? plan.pack_plan.changes.length : 0;
        const ok = window.confirm("Install scenario " + scenarioID + "?\n\nRecommended pack: " + packID + "\nSkill changes: " + changes + "\nBilling preview: " + (total || "0"));
        if (!ok) {
          setStatus("Ready");
          return;
        }
        await api("/v1/commerce/install-scenario", {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-AGTX-Commerce-Token": mutationToken },
          body: JSON.stringify({ scenario_id: scenarioID, yes: true })
        });
        toast("Installed " + scenarioID);
        await load();
      } catch (err) {
        setStatus("Ready");
        toast(err.message, true);
      }
    }

    function setStatus(value) {
      el("status").textContent = value;
    }

    function toast(message, bad) {
      const node = el("toast");
      node.textContent = message;
      node.className = "toast" + (bad ? " bad" : "");
      node.style.display = "block";
      clearTimeout(window.__toastTimer);
      window.__toastTimer = setTimeout(() => { node.style.display = "none"; }, 3200);
    }

    function dateTimeLocalToRFC3339(value) {
      if (!value) return "";
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return "";
      return date.toISOString();
    }

    document.querySelectorAll("[data-tab]").forEach((button) => {
      button.addEventListener("click", () => {
        state.activeTab = button.getAttribute("data-tab");
        document.querySelectorAll("[data-tab]").forEach((item) => item.classList.toggle("active", item === button));
        renderRecords();
      });
    });
    el("refresh").addEventListener("click", () => load().catch((err) => { setStatus("Error"); toast(err.message, true); }));
    el("packFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("scenarioFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("statusFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("typeFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("currencyFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("fromFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("toFilter").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    el("limit").addEventListener("change", () => load().catch((err) => toast(err.message, true)));
    load().catch((err) => { setStatus("Error"); toast(err.message, true); });
  </script>
</body>
</html>
`
