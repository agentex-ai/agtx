# Capability Bundles

Capability bundles are task-scoped working sets of Agentex skills. They let an
agent return a coordinated set of capabilities in one pass instead of repeatedly
searching for one skill at a time.

The goal is to move selection from single-skill matching to task orchestration.
For a complex user request, the agent should identify the task shape, assemble
the primary, supporting, validation, conditional, and fallback skills, then use
that bundle as execution context.

## Why bundles exist

User tasks rarely map to exactly one skill. A realistic workflow might require
source discovery, page fetching, document extraction, OCR, synthesis, local
verification, and a manual fallback. If the agent discovers each skill only when
it gets stuck, execution becomes slower and less explainable.

A bundle answers a richer question:

```text
Which capabilities should be assembled for this task, what role does each one
play, when should each one run, and when should it be skipped?
```

This keeps agent behavior stable while still allowing runtime judgment.

## Bundle shape

Recommended fields:

- `bundle_id`: Stable identifier for the bundle template or generated bundle.
- `summary`: Human-readable explanation of the task the bundle supports.
- `confidence`: Agent confidence that this bundle fits the current task.
- `task_profile`: Intent, domains, required needs, risk level, and whether user
  input is required.
- `packs`: Ordered capability entries with id, role, priority, stage, and reason
  or condition.
- `execution_notes`: Short coordination rules for the agent.

Recommended pack priorities:

- `required`: Needed to complete the task safely.
- `recommended`: Improves quality, reliability, or verification.
- `conditional`: Use only when a runtime condition is met.
- `fallback`: Use when the preferred path is unavailable.
- `excluded`: Mention when a tempting skill should not be used for this task.

Recommended roles:

- `discovery`: Inspect local context, registry entries, sources, or task inputs.
- `implementation`: Perform the core transformation or workflow.
- `asset_creation`: Create supporting media or generated assets.
- `validation`: Check quality, outputs, evidence, rendering, or invariants.
- `handoff`: Prepare a human confirmation or manual command path.
- `fallback`: Recover when the preferred capability is unavailable.

Recommended stages:

- `task_profile`
- `before_mutation`
- `before_editing`
- `editing`
- `after_dev_server`
- `verification`
- `handoff`

## Example

```json
{
  "bundle_id": "research_answer",
  "summary": "Discover sources, read selected pages, and synthesize an evidence-backed answer.",
  "confidence": 0.9,
  "task_profile": {
    "intent": "answer_with_research",
    "domains": ["web", "research", "synthesis"],
    "needs": ["discover_sources", "fetch_sources", "synthesize_findings"],
    "risk_level": "medium",
    "requires_user_input": false
  },
  "packs": [
    {
      "id": "web_search",
      "role": "discovery",
      "priority": "required",
      "stage": "task_profile",
      "reason": "Find candidate sources and references for the user's question."
    },
    {
      "id": "web_fetch",
      "role": "implementation",
      "priority": "required",
      "stage": "editing",
      "reason": "Read selected pages and extract evidence from known URLs."
    },
    {
      "id": "deep_research",
      "role": "validation",
      "priority": "recommended",
      "stage": "verification",
      "reason": "Synthesize findings, caveats, evidence trail, and next actions."
    }
  ],
  "execution_notes": [
    "Prefer recent and primary sources when freshness or authority matters.",
    "Fetch only sources that are relevant enough to support the answer.",
    "Expose caveats when evidence is incomplete or conflicting."
  ]
}
```

## Agent behavior

Agents should treat bundles as capability context, not rigid execution plans.
They may skip conditional skills, change sequence after inspecting local state,
or choose a fallback path when a dependency is unavailable. They should preserve
the bundle's roles, trigger reasons, and constraints when explaining decisions.

For installation flows, an agent should plan the whole bundle before mutating
local state. It should call `plan_install` or the matching MCP planning tool for
required and likely conditional skills, show relevant commerce metadata for paid
or metered capabilities, and only install with confirmation where required.

For execution flows, an agent should avoid over-invocation. Returning a bundle
does not mean every listed skill must run. Conditional skills should have clear
activation rules, and fallback skills should remain dormant unless the preferred
path fails.

## Relationship to commerce packs

Commerce packs such as `web_search`, `pdf`, `documents`, `standard`, and
`advanced` are installable products. Capability bundles are task-level
orchestration bundles. They may reference skills from one or more commerce
packs, but they do not define billing by themselves.

The built-in pack catalog mirrors the first-wave packs shown on `agentex.cc`:
`web_search`, `web_fetch`, `deep_research`, `ocr` (aliases: `rapidocr`,
`ppocrv6`), `audio`, `imagen`, `docx`, `xlsx`, `pptx`, and `pdf`. The
registry-compatible `documents` pack groups the four native document packs. The
`standard` and `advanced` packs remain ordinary and advanced bundles for users
who want one install action to provision a whole working set.

Billing, revenue share, and CPA/CPS attribution stay in the Capability Commerce
Standard under `docs/standards/`. A task-level capability bundle should carry
only the metadata needed for selection, coordination, cost awareness, and
explainability.

## Built-in scenario views

agtx ships a small set of first-party scenario views for website and MCP
integrations. They are not separate billable products; they are task templates
that map realistic workflows to the installable `standard` or `advanced`
commerce pack. Websites can still filter scenarios by a first-wave pack such as
`pdf`, `xlsx`, or `deep_research` to show which real workflows use that capability.

Current built-in scenarios include:

- `invoice_processing`: PDF/OCR/spreadsheet invoice extraction and accounting
  handoff, recommended for `standard`.
- `due_diligence_research`: web discovery, source fetching, PDF reading, and
  evidence-backed synthesis, recommended for `standard`.
- `contract_review`: Word/PDF contract intake with OCR fallback and human-review
  risk notes, recommended for `standard`.
- `support_knowledge_base`: manuals, tickets, spreadsheets, and public docs into
  support articles, recommended for `standard`.
- `meeting_to_presentation`: meeting audio and notes into a slide-ready deck,
  recommended for `advanced`.
- `marketing_asset_generation`: research-backed campaign assets, generated
  visuals, and presentation handoff, recommended for `advanced`.

Each scenario view includes the recommended pack, install readiness, missing
skills, required scenario skills, expected inputs, deliverables, workflow steps,
acceptance criteria, a pack install plan, and billing preview totals. Websites
can query them with:

```sh
agtx commerce scenarios --json
agtx commerce scenarios --pack-id pdf --json
agtx commerce scenarios --scenario-id invoice_processing --json
agtx commerce install-pack pdf --plan --json
agtx commerce install-pack pdf --yes --json
agtx commerce install-scenario invoice_processing --plan --json
agtx commerce install-scenario invoice_processing --yes --json
agtx commerce scenario-ledger invoice_processing --json
agtx commerce install-records --pack-id pdf --json
agtx commerce billing-records --pack-id pdf --json
agtx commerce install-records --scenario-id invoice_processing --json
agtx commerce billing-records --scenario-id invoice_processing --json
agtx run <installed-skill> --scenario-id invoice_processing --json
agtx commerce snapshot --pack-id pdf --json
agtx commerce snapshot --pack-id standard --json
agtx commerce snapshot --scenario-id invoice_processing --json
```

Scenario-driven installs are still installs of the recommended commerce pack,
but the install record action is `install_scenario` and matching install and
billing ledger records are tagged with the canonical `scenario_id`. This lets a
website show history and invoices for a workflow such as invoice processing
without losing the underlying pack and skill details.

Scenario metadata is intended to be website-renderable. A marketplace or account
page can show which files a workflow needs, what deliverables will be produced,
which steps the agent will run, and which acceptance criteria remain before a
human marks the workflow complete. The ledger remains compact: install and
billing records keep the canonical `scenario_id`, while the current scenario
catalog supplies the human-readable workflow context.

For account and invoice pages that want one response per workflow,
`agtx commerce scenario-ledger <scenario> --json` and
`GET /v1/commerce/scenario-ledger?scenario_id=<scenario>` return the scenario
view, latest install record, matching install records, billing records, totals,
and a split between `pack_install` and `skill_usage` records. MCP exposes the
same shape as `get_capability_scenario_ledger`.

Skill runs can also pass a scenario id, for example
`agtx run <installed-skill> --scenario-id invoice_processing --json` or MCP
`run_skill` with `scenario_id`. Metered usage events and local `skill_usage`
billing records then carry the same canonical scenario id, so a website can
show both installation charges and actual workflow usage under one scenario.

The local HTTP API exposes `GET /v1/commerce/scenarios`,
`GET /v1/commerce/scenario-install-plan`, and
`POST /v1/commerce/install-scenario`. MCP exposes
`list_capability_scenarios`, `get_capability_scenario`,
`plan_capability_scenario_install`, `install_capability_scenario`, and
`get_capability_scenario_ledger` for agents that want to plan a whole task
before installing or running individual skills.
