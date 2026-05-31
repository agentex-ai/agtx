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
agtx config set registry_url https://example.com/agtx/registry.json
agtx config set pro_api_url https://agtx-pro.example.com
agtx config set package_max_bytes 268435456
agtx config set extracted_max_bytes 1073741824
agtx config set extracted_max_files 8192
agtx registry sources
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

`agtx mcp` implements a minimal newline-delimited JSON-RPC MCP server with tools for search, install, run, list, upgrade, rollback, status, doctor, verify, and Pro auth/device management.

The server also accepts `Content-Length` framed JSON-RPC messages used by MCP clients.
Its `tools/list` metadata includes strict JSON Schema with required fields, positive integer minima, and `additionalProperties: false` to help agent clients form valid calls without guessing.
It also exposes agent-bootstrap helpers so an external client can call `list_agent_targets` and `get_agent_target` over MCP instead of shelling out to `agtx agent ...`, and those tools now advertise output schema for `setup_steps`, `writes_files`, and `artifacts`. Search/list/status/refresh/install-plan/install/upgrade/rollback/uninstall/doctor/verify/run and Pro auth/device tools also publish output schema so setup UIs, confirmation flows, local-status panels, login flows, callback-scheme registration, and result viewers can be prepared from discovery metadata alone. `get_pro_setup` adds a no-side-effect preflight path so wrappers can inspect whether `pro_api_url` is configured, whether a login is pending, and which CLI/MCP actions should come next before attempting any auth mutation. Pro-related failures such as `unauthorized`, `subscription_required`, and `device_limit_exceeded` now also carry a structured `error.details.pro_setup` preview plus `error.details.next_actions` recovery steps, so wrappers can recover without hard-coded heuristics. Each tool also exposes `errorOutputSchema` so wrappers can model the shared failure envelope, including partial `data` on preserved-error paths such as verify/run failures.

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
