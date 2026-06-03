# Agentex Capability Commerce Standard

Version: `2026-06-03`

Canonical source: `agtx/docs/standards/capability-commerce-standard.md`
Published copy: `https://agentex.cc/registry/capability-commerce-standard.md`

The Agentex Capability Commerce Standard defines how independent software vendors (ISVs) publish capability packs, declare billable units, receive CPA/CPS attribution, and earn revenue from agent-driven workflows.

This standard is designed for agents, marketplaces, and ISVs. A capability pack should be understandable before it is installed, measurable after it is invoked, and payable after it creates value.

## Goals

- Make every capability pack discoverable through a predictable manifest.
- Let ISVs price capabilities by clear units instead of vague subscriptions only.
- Support both usage-based revenue and outcome-based CPA/CPS programs.
- Give agents enough metadata to choose, budget, and attribute capability usage.
- Keep user trust high through explicit permissions, quality gates, and audit trails.

## 1. Capability pack standard

Every ISV capability pack must publish an Agentex manifest with these fields:

- `id`: stable lowercase pack identifier.
- `vendor_id`: stable ISV identifier.
- `version`: semantic version for the packaged capability.
- `capability_class`: one of `tool`, `workflow`, `model_adapter`, `connector`, `content`, or `commerce`.
- `use_when`: short agent-readable trigger guidance.
- `inputs` and `outputs`: human and machine-readable contract names.
- `permissions`: network, filesystem, model, browser, payment, or user-data access.
- `quality`: eval score, harness coverage, supported platforms, and rollback channel.
- `billing`: supported billable meters and price model.
- `attribution`: supported CPA/CPS events if the pack drives acquisition or sales.
- `support`: vendor support URL, privacy policy, and incident contact.

Third-party monetized packs must include `support.url`, `support.privacy_url`, and `support.incident_email` before they can pass registry validation. First-party Agentex packs may inherit platform support defaults.

Recommended manifest URL:

```text
https://agentex.cc/registry/vendors/{vendor_id}/packs/{pack_id}.json
```

## 2. Capability billing standard

Capability billing is meter-first. Subscriptions may bundle capacity, but the billable unit must remain explicit so ISVs can earn from real usage.

Supported billing meters:

- `call`: one successful capability invocation.
- `task`: one complete user-visible workflow.
- `page`: document, OCR, PDF, or browser page processed.
- `minute`: audio, video, meeting, or streaming duration.
- `token`: model input/output token usage.
- `credit`: normalized cross-capability unit for bundles.
- `seat`: named user or workspace member.
- `storage_gb_day`: persisted storage capacity over time.
- `success`: verified completion of a declared business outcome.

Required billing fields:

- `meter`: billing meter name.
- `unit_price`: numeric price in minor currency units or credit units.
- `currency`: ISO 4217 currency or `AGTX_CREDIT`.
- `free_quota`: optional included quantity.
- `hard_limit_supported`: whether agents can enforce a spending cap.
- `refund_policy`: failed invocation, partial completion, or dispute handling.

Default platform split for paid capability usage:

- ISV revenue share: `70%`
- Agentex platform share: `30%`
- Payment processor and tax costs are deducted before net revenue share unless a regional contract says otherwise.

## 3. CPA/CPS attribution standard

CPA/CPS applies when a capability pack produces a measurable acquisition, activation, or sale event.

Supported event types:

- `lead_created`: qualified lead, form submit, waitlist signup, or demo request.
- `account_created`: new account or workspace creation.
- `activation_completed`: user completes the product-defined activation milestone.
- `checkout_started`: user starts a paid checkout.
- `purchase_completed`: one-time purchase completes.
- `subscription_started`: paid subscription starts.
- `subscription_renewed`: subscription renews after the initial period.

Required event fields:

- `event_id`: idempotency key for the conversion event.
- `event_type`: one supported event type.
- `occurred_at`: ISO timestamp.
- `pack_id` and `vendor_id`: source capability attribution.
- `account_id_hash`: privacy-safe account or user hash.
- `attribution_window_days`: default `30` for CPA and `45` for CPS.
- `amount`: purchase amount for CPS events.
- `currency`: event currency.
- `evidence`: signed reference, checkout id, webhook id, or partner proof.

Default outcome commission:

- CPA lead/account/activation events: fixed bounty declared per pack or campaign.
- CPS purchase/subscription events: `15%` of first purchase or first paid period by default.
- Renewal CPS: disabled by default; enabled only when the campaign declares renewal commission.

## 4. ISV lifecycle

ISVs can earn only after a pack completes the lifecycle:

1. `draft`: manifest and pricing exist but are not discoverable.
2. `review`: Agentex validates permissions, billing meters, support links, and safety claims.
3. `verified`: pack can appear in the registry and marketplace.
4. `stable`: pack has passing harness/eval coverage and can be recommended by agents.
5. `monetized`: billing or CPA/CPS attribution is enabled.
6. `restricted`: pack is hidden from recommendation after quality, fraud, or policy issues.

## 5. Settlement and trust

- Usage revenue is settled monthly after refunds, disputes, tax, and payment costs are reconciled.
- CPA/CPS revenue is settled after the declared attribution window and fraud review.
- ISVs must provide support and privacy-policy URLs before monetization.
- ISV manifests with billing meters, CPA/CPS attribution events, or `commerce` capability class must include support, privacy, and incident-contact metadata.
- Agents must expose cost estimates when a user asks or when a hard limit is configured.
- Failed invocations must not be billed unless the manifest explicitly declares partial-value billing.

## Example commerce manifest fragment

```json
{
  "billing": {
    "meters": [
      {
        "meter": "page",
        "unit_price": 2,
        "currency": "AGTX_CREDIT",
        "free_quota": 20,
        "hard_limit_supported": true,
        "refund_policy": "Do not bill pages that fail extraction."
      }
    ],
    "revenue_share": {
      "isv": 70,
      "platform": 30
    }
  },
  "attribution": {
    "events": ["lead_created", "purchase_completed"],
    "default_window_days": {
      "cpa": 30,
      "cps": 45
    },
    "default_cps_rate": 15
  }
}
```

## Full manifest examples

- Canonical usage-metered pack: `docs/standards/examples/usage-metered-skill.json`
- Canonical CPA/CPS outcome pack: `docs/standards/examples/outcome-cpa-cps-skill.json`
- Published examples: `https://agentex.cc/registry/examples/`
