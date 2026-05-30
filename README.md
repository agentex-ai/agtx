# agtx

`agtx` is a macOS-first, low-dependency native skill manager for Agentex skills.

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
agtx config set package_max_bytes 268435456
agtx config set extracted_max_bytes 1073741824
agtx config set extracted_max_files 8192
agtx registry sources
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

For `agtx run`, `--output-limit-bytes` bounds captured stdout/stderr and `--input file|-` reads in CLI agent calls.

## Build

macOS release builds should disable cgo and prefer Go-native resolver/user lookup paths:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags "netgo osusergo" -trimpath -ldflags "-s -w" -o dist/agtx-darwin-arm64 ./cmd/agtx
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -tags "netgo osusergo" -trimpath -ldflags "-s -w" -o dist/agtx-darwin-amd64 ./cmd/agtx
```

On macOS, audit dynamic links before release:

```sh
otool -L dist/agtx-darwin-arm64
```

## Agent Integration

Prefer MCP stdio:

```sh
agtx agent init codex --print
agtx agent init cursor --print
agtx agent init cc --print
```

`agtx mcp` implements a minimal newline-delimited JSON-RPC MCP server with tools for search, install, run, list, upgrade, rollback, status, doctor, and verify.

The server also accepts `Content-Length` framed JSON-RPC messages used by MCP clients.

## Registry

`agtx` starts with a built-in registry so it can run offline. Optional registry overlays can be configured in `config.json`:

```json
{
  "schema_version": 1,
  "registry_url": "https://example.com/agtx/registry.json",
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
