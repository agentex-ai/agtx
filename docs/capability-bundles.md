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
      "id": "research",
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

Commerce packs such as `standard` and `advanced` are installable product bundles.
Capability bundles are task-level orchestration bundles. They may reference
skills from one or more commerce packs, but they do not define billing by
themselves.

Billing, revenue share, and CPA/CPS attribution stay in the Capability Commerce
Standard under `docs/standards/`. A task-level capability bundle should carry
only the metadata needed for selection, coordination, cost awareness, and
explainability.
