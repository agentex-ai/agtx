# agtx

`agtx` is a macOS and Windows friendly, low-dependency native skill manager for Agentex skills.

The v1 implementation is intentionally small: one Go binary, standard library first, no Python/NPM/Homebrew runtime dependency, and no third-party Go modules.

## Commands

```sh
agtx search "summarize PDFs and Word files"
agtx install pdf --plan --json
agtx install pdf docx --yes
agtx run pdf --timeout-ms 30000 --output-limit-bytes 1048576 --json
agtx uninstall pdf --plan --json
agtx list --json
agtx status
agtx doctor --json
agtx verify pdf --json
agtx config init
agtx config keys --json
agtx config set registry_url https://example.com/agtx/registry.json
agtx config set pro_api_url https://agtx-pro.example.com
agtx config set package_max_bytes 268435456
agtx config set extracted_max_bytes 1073741824
agtx config set extracted_max_files 8192
agtx registry sources
agtx commerce packs --json
agtx commerce install-pack standard --plan --json
agtx commerce install-pack standard --yes --json
agtx commerce snapshot --pack-id standard --json
agtx pro setup
agtx pro login --open
agtx pro status --json
agtx mcp
```

Mutating commands require confirmation. Agent callers should pass `--json --yes` where appropriate and consume the fixed response shape:

```json
{
  "ok": true,
  "data": {},
  "warnings": [],
  "trace_id": "agtx-..."
}
```

For `agtx run`, `--output-limit-bytes` bounds captured stdout/stderr and `--input file|-` reads in CLI agent calls. Use `--` before skill arguments; any `--json` or `--ndjson` after that separator is passed through to the skill, not treated as an agtx output flag.

Structured errors include recovery hints for agent callers: unknown commands include `supported_commands`, nested command errors include `supported_subcommands`, missing positional arguments include `expected_args`, flag parse/unexpected-argument errors include `supported_flags`, and MCP parse/envelope/method/tool/params/argument errors include `field`, `expected`, `supported_fields`, `supported_methods`, `supported_tools`, `supported_params`, or `supported_arguments`. MCP required-argument, argument-shape, and confirmation errors also include the tool name plus expected argument shape or `yes=true` retry details.

## Build

Release builds should disable cgo and prefer Go-native resolver/user lookup paths:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags "netgo osusergo" -trimpath -ldflags "-s -w" -o dist/agtx-darwin-arm64 ./cmd/agtx
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -tags "netgo osusergo" -trimpath -ldflags "-s -w" -o dist/agtx-darwin-amd64 ./cmd/agtx
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/agtx-windows-amd64.exe ./cmd/agtx
```

On macOS, audit dynamic links before release:

```sh
otool -L dist/agtx-darwin-arm64
```

## Agent Integration

Prefer MCP stdio:

```sh
agtx agent targets
agtx agent init codex --print
agtx agent init cursor --print
agtx agent init cc --print
agtx agent init codex --json
```

Use `agtx agent targets` to discover supported agent names first. Use `--print` for paste-ready snippets and `--json` when another tool needs structured setup metadata, aliases, path hints, and ordered `setup_steps` with `priority` / `blocking` / `verification` / `platforms` / `applies_when` / `writes_files` / `artifacts` hints instead of human-formatted text.

`agtx mcp` implements a minimal newline-delimited JSON-RPC MCP server with tools for search, install, run, list, upgrade, rollback, status, registry inspection/validation, doctor, verify, capability-pack commerce, and Pro auth/device management.

The server also accepts `Content-Length` framed JSON-RPC messages used by MCP clients.
Its `tools/list` metadata includes strict JSON Schema with required fields, positive integer minima, and `additionalProperties: false` to help agent clients form valid calls without guessing.
It also exposes agent-bootstrap helpers so an external client can call `list_agent_targets` and `get_agent_target` over MCP instead of shelling out to `agtx agent ...`, and those tools now advertise output schema for `setup_steps`, `writes_files`, and `artifacts`. Search/list/status/registry/refresh/install-plan/install/upgrade/rollback/uninstall/doctor/verify/run, capability-pack commerce, and Pro auth/device tools also publish output schema so setup UIs, confirmation flows, registry panels, local-status panels, login flows, callback-scheme registration, website commerce panels, and result viewers can be prepared from discovery metadata alone. `get_pro_setup` adds a no-side-effect preflight path so wrappers can inspect whether `pro_api_url` is configured, whether a login is pending, and which CLI/MCP actions should come next before attempting any auth mutation. Pro-related failures such as `unauthorized`, `subscription_required`, and `device_limit_exceeded` now also carry a structured `error.details.pro_setup` preview plus `error.details.next_actions` recovery steps, so wrappers can recover without hard-coded heuristics. Each tool also exposes `errorOutputSchema` so wrappers can model the shared failure envelope, including partial `data` on preserved-error paths such as verify/run failures.

For multi-skill tasks, agents should assemble a capability bundle before
installing or running individual skills. See `docs/capability-bundles.md` for
the recommended task profile, priorities, roles, stages, and execution notes.

## Pro

Pro login is CLI-managed and server-authorized. The CLI stores local device auth in `auth.json`, while a Cloudflare Worker checks Stripe subscription state, enforces the default 3 active device limit, filters registry entries, and gates package downloads:

```sh
agtx config set pro_api_url https://agtx-pro.example.com
agtx pro setup
agtx pro register-scheme
agtx pro login --open
agtx pro callback "agtx://pro/callback?code=...&state=..."
agtx pro devices
agtx pro revoke <device-id>
agtx pro logout
```

`registry refresh` and HTTP package downloads send `Authorization: Bearer <token>` only to the same origin as `pro_api_url` or `registry_url`.
`agtx pro setup` is a no-side-effect preflight check: it does not refresh tokens or call the network, and instead reports current local status plus recommended next actions for either humans or agents.
`agtx pro register-scheme` now targets both macOS and Windows; on macOS it installs a tiny local callback app bundle under the agtx config directory so browser login can return through `agtx://`.

## Registry

`agtx` starts with a built-in registry so it can run offline. Optional registry overlays can be configured in `config.json`:

```json
{
  "schema_version": 1,
  "registry_url": "https://example.com/agtx/registry.json",
  "pro_api_url": "https://agtx-pro.example.com",
  "registry_files": ["/path/to/local-registry.json"],
  "channel": "stable",
  "telemetry": "off",
  "registry_max_bytes": 16777216,
  "registry_download_timeout_ms": 30000,
  "package_max_bytes": 268435456,
  "package_download_timeout_ms": 30000,
  "extracted_max_bytes": 1073741824,
  "extracted_max_files": 8192
}
```

`config.json` is strict: unknown keys, `null` values, trailing JSON values, invalid URLs, unsupported schema versions, and non-positive limits are rejected instead of silently falling back.
Use `agtx config keys --json` to discover supported settings; unknown-key errors also include `supported_keys` for agent recovery.

## Capability Commerce Standard

`agtx` is the source-of-truth repo for Agentex capability-pack standards. The
standards under `docs/standards/` define optional manifest commerce metadata,
including vendor identity, capability class, billing meters, CPA/CPS attribution,
revenue share, support links, and settlement rules. `agentex.cc` publishes static
copies for agents, ISVs, and the website.

After changing a standard here, publish the website copy from the sibling
`agentex.cc` repo:

```powershell
cd ..\agentex.cc
npm run sync:agtx-standards
npm run build
```

The built-in first-wave skills already declare default billing meters:

- `web_search`: `call`
- `web_fetch`: `page`, `call`
- `research`: `task`
- `ocr` and `pdf`: `page`
- `audio`: `minute`
- `imagen`: `task`, `credit`
- `docx`, `xlsx`, and `pptx`: `task`

Example ISV manifests are available under `docs/standards/examples/` for
usage-metered packs and CPA/CPS outcome packs.
`agtx install --plan --json` and MCP `plan_install` expose a compact commerce
summary so agents can show vendor, capability class, billing meters, attribution
events, and support URL before asking for install confirmation.

The built-in capability-pack layer mirrors the first-wave packs shown on
`agentex.cc` and keeps two bundle packs for ordinary and advanced installs.

Website first-wave packs:

- `web_search`: web discovery and ranked source candidates.
- `web_fetch`: known-URL reading, article extraction, metadata, and relay fallback.
- `research`: multi-step evidence gathering, synthesis, analysis, and UI review.
- `ocr`: screenshots, scans, PDF pages, UI images, and photo text extraction.
- `audio`: ASR, TTS, meeting notes, and batch audio jobs.
- `imagen` (alias: `mediagen`): text-to-image, image-to-video, and media generation.
- `docx`, `xlsx`, `pptx`, and `pdf`: native document-family packs.

Compatibility and bundle packs:

- `documents`: registry-compatible document-family pack for `docx`, `xlsx`, `pptx`, and `pdf`.
- `standard`: ordinary bundle for web, document, OCR, and research workflows.
- `advanced`: full bundle with all built-in first-wave skills, including audio, media generation, and presentation handling.

Website integrations can query pack state, install history, and billing history
through the CLI, the local HTTP API, or the local MCP server. They can also
query task scenarios such as invoice processing, contract review, meeting-to-deck
handoff, marketing asset generation, and support knowledge-base creation. Each
scenario returns the recommended pack, missing skills, real task inputs,
deliverables, workflow steps, acceptance criteria, install plan, and billing
preview needed for a website purchase/install flow:

```sh
agtx commerce packs --json
agtx commerce packs --pack-id pdf --json
agtx commerce scenarios --pack-id standard --json
agtx commerce scenarios --pack-id pdf --json
agtx commerce scenarios --scenario-id invoice_processing --json
agtx commerce install-pack pdf --plan --json
agtx commerce install-pack pdf --yes --json
agtx commerce install-pack standard --plan --json
agtx commerce install-pack standard --yes --json
agtx commerce install-scenario invoice_processing --plan --json
agtx commerce install-scenario invoice_processing --yes --json
agtx commerce scenario-ledger invoice_processing --json
agtx commerce install-records --pack-id standard --json
agtx commerce install-records --pack-id pdf --json
agtx commerce billing-records --pack-id standard --json
agtx commerce billing-records --pack-id pdf --json
agtx commerce billing-records --scenario-id invoice_processing --json
agtx commerce billing-records --pack-id standard --type pack_install --currency USD --status local_only --json
agtx run <installed-skill> --scenario-id invoice_processing --json
agtx commerce snapshot --pack-id standard --json
agtx commerce snapshot --pack-id pdf --json
agtx commerce snapshot --scenario-id invoice_processing --json
agtx commerce snapshot --pack-id standard --out ./commerce-snapshot.json --json
agtx commerce serve --addr 127.0.0.1:8765 --allow-origin https://example.com
```

The HTTP API returns the same response envelope as CLI JSON output. Website
panels can call:

- `GET /commerce` for the local capability-pack dashboard
- `GET /v1/commerce/packs`
- `GET /v1/commerce/packs?pack_id=pdf`
- `GET /v1/commerce/scenarios?scenario_id=invoice_processing`
- `GET /v1/commerce/scenarios?pack_id=pdf`
- `GET /v1/commerce/install-plan?pack_id=pdf`
- `GET /v1/commerce/install-plan?pack_id=standard`
- `GET /v1/commerce/scenario-install-plan?scenario_id=invoice_processing`
- `POST /v1/commerce/install-pack` with header `X-AGTX-Commerce-Token`
  and body `{"pack_id":"pdf","yes":true}`
- `POST /v1/commerce/install-pack` with header `X-AGTX-Commerce-Token`
  and body `{"pack_id":"standard","yes":true}`
- `POST /v1/commerce/install-scenario` with header `X-AGTX-Commerce-Token`
  and body `{"scenario_id":"invoice_processing","yes":true}`
- `GET /v1/commerce/scenario-ledger?scenario_id=invoice_processing&limit=100`
- `GET /v1/commerce/install-records?pack_id=standard&status=installed&limit=100`
- `GET /v1/commerce/install-records?pack_id=pdf&status=installed&limit=100`
- `GET /v1/commerce/install-records?scenario_id=invoice_processing&limit=100`
- `GET /v1/commerce/billing-records?pack_id=standard&type=pack_install&currency=USD&status=local_only&limit=100`
- `GET /v1/commerce/billing-records?pack_id=pdf&type=pack_install&limit=100`
- `GET /v1/commerce/billing-records?scenario_id=invoice_processing&limit=100`
- `GET /v1/commerce/snapshot?pack_id=standard&limit=100`
- `GET /v1/commerce/snapshot?pack_id=pdf&limit=100`
- `GET /v1/commerce/snapshot?scenario_id=invoice_processing&limit=100`

`commerce serve` prints the dashboard URL and one-session mutation token used
by the dashboard when installing packs.

The matching MCP tools are `list_capability_packs`,
`list_capability_scenarios`, `get_capability_scenario`,
`plan_capability_pack_install`, `install_capability_pack`,
`plan_capability_scenario_install`, `install_capability_scenario`,
`get_capability_scenario_ledger`, `list_install_records`,
`list_billing_records`, and `get_commerce_snapshot`.
Local install and billing ledgers are stored as JSONL under the agtx config
directory and returned through both surfaces with stable JSON Schema discovery
metadata on MCP. Scenario-driven installs write `install_scenario` install
records and tag matching install and billing records with `scenario_id`. Skill
runs can also pass `--scenario-id`, and MCP `run_skill` accepts `scenario_id`;
metered usage events and local `skill_usage` billing records carry the
canonical scenario id for website invoices and workflow history.
For website account pages, `scenario-ledger` and
`get_capability_scenario_ledger` return the scenario view, latest install,
matching install records, billing totals, and a split between pack-install and
skill-usage records in one response.

Plan before mutating, then refresh a configured remote registry:

```sh
agtx install pdf --plan --json
agtx registry validate ./registry.json --json
agtx registry refresh --json
agtx doctor --json
agtx verify pdf --json
```

## Local Paths

On macOS:

- Config: `~/Library/Application Support/agtx/config.json`
- Registry cache: `~/Library/Caches/agtx/registry/`
- Skills: `~/Library/Application Support/agtx/skills/<name>/<version>/`
- Logs: `~/Library/Logs/agtx/`

Set `AGTX_HOME` to redirect all state into a single directory for tests or isolated agent runs.

On Windows:

- Config/auth: `%APPDATA%\agtx\config.json` and `%APPDATA%\agtx\auth.json`
- Registry cache: `%LOCALAPPDATA%\agtx\Cache\registry\`
- Skills: `%APPDATA%\agtx\skills\<name>\<version>\`
- Logs: `%LOCALAPPDATA%\agtx\Logs\`

Local Windows test example:

```powershell
$env:AGTX_HOME="$PWD\.tmp-agtx"
go test ./...
go run ./cmd/agtx status
```
