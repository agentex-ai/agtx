# Agent Integration

Agents should use `agtx` through MCP when possible, then fall back to JSON CLI.

## MCP

Run:

```sh
agtx mcp
```

Available tools:

- `search_skills`
- `list_skills`
- `list_agent_targets`
- `get_agent_target`
- `get_status`
- `list_config_keys`
- `get_pro_status`
- `get_pro_setup`
- `start_pro_login`
- `complete_pro_login`
- `list_pro_devices`
- `revoke_pro_device`
- `logout_pro`
- `register_pro_scheme`
- `doctor`
- `verify_skill`
- `refresh_registry`
- `plan_install`
- `install_skill`
- `upgrade_skill`
- `rollback_skill`
- `uninstall_skill`
- `run_skill`

Mutation tools require `yes=true`. Agents should first explain the intended install, upgrade, or rollback.
For installs, agents should call `plan_install` first. For upgrade and rollback, use `plan=true` on the relevant tool before setting `yes=true`.
For uninstall, use `plan=true` first and set `yes=true` only after stating which versions will be removed.
Use `doctor` when an agent starts a session or detects a local failure. Use `verify_skill` before running a newly installed non-stub skill.
Tool results include both text content and `structuredContent`. On tool failure, `isError=true` and `structuredContent` contains the same `ok/error/trace_id` envelope used by the JSON CLI.
Some failing tools, such as `verify_skill` and `run_skill`, also include partial diagnostic or run data in `structuredContent.data`.
MCP requests are bounded to avoid accidental large-message hangs; oversized requests terminate the server with a size-limit error.
MCP tool arguments are strict: non-object argument payloads, unknown fields, wrong JSON types, trailing JSON values, and non-positive integer limits return structured `invalid_argument` errors instead of silently falling back to defaults.
Unknown MCP tool names include `supported_tools`, and argument-shape or argument-name errors include `supported_arguments`, so wrappers can retry after a spelling or schema mismatch without hard-coded tool lists.
Confirmation errors from mutating MCP tools preserve `retry_with` and also include the tool name, `yes` argument, and `supported_arguments`, so wrappers can build confirmation prompts and resend the call without special-casing each mutation tool.
`tools/list` publishes strict JSON Schema for each tool, including required fields, positive integer bounds, and `additionalProperties: false`, so agent clients can validate calls before sending them. For `list_agent_targets` and `get_agent_target`, the metadata now also declares output schema for `setup_steps`, `writes_files`, and `artifacts`, letting clients build setup UIs from discovery alone. The same now applies to skill, status, registry, Pro, diagnostic, and mutation tools, so wrappers can pre-wire result viewers, plan/confirm flows, and local-status panels before making the first call. Each tool now also advertises `errorOutputSchema`, which describes the shared failure envelope and any partial `data` payload preserved on errors.
The JSON-RPC envelope is also strict: `jsonrpc` must be `2.0`, `id` must be string/number/null, `params` must be an object or null, and unknown top-level fields are rejected. Envelope validation errors include `field` and `expected` details; unknown methods return `supported_methods`, and malformed `tools/call` params include `supported_params`.
Agent-facing wrappers can self-bootstrap from MCP too: call `list_agent_targets` to discover supported targets, then `get_agent_target` to fetch structured snippets, aliases, and path hints for a specific integration.
If `get_agent_target` is called with an unsupported target, the error includes both `supported_targets` and `supported_arguments` for the failed `target` argument.
Pro-aware failures are now more actionable too: when a tool returns `unauthorized`, `subscription_required`, or `device_limit_exceeded`, inspect `structuredContent.error.details.pro_setup` and `structuredContent.error.details.next_actions` before asking the user what to do. This lets a wrapper branch directly into `start_pro_login`, `get_pro_status`, `list_pro_devices`, or `register_pro_scheme` without maintaining its own recovery matrix.

## JSON CLI Fallback

Use `--json` for one-shot calls:

```sh
agtx search "extract invoice totals" --json
agtx install xlsx --plan --json
agtx install xlsx --yes --json
agtx uninstall xlsx --plan --json
agtx doctor --json
agtx verify xlsx --json
agtx config keys --json
agtx registry validate ./registry.json --json
agtx run xlsx --input input.json --json
agtx pro status --json
```

Long-running tasks should use `--ndjson`:

```sh
agtx run research --input task.json --ndjson
```

Do not pass `--json` and `--ndjson` together. If initialization or input reading fails before a run starts, `--ndjson` still emits a single `failed` event instead of falling back to plain stderr.
For large inputs, set `--output-limit-bytes`; CLI mode uses the same byte limit for captured stdout/stderr and `--input file|-` reads.
If `run_skill` fails before execution, inspect the structured error: missing, directory, escaping, or non-executable entrypoints are reported as `invalid_argument` instead of raw process-launch errors.
CLI value flags are strict: missing values for flags such as `--input`, `--to`, `--timeout-ms`, and `--output-limit-bytes` return structured `invalid_argument` errors instead of being ignored.
Config key discovery is available through `agtx config keys --json` and MCP `list_config_keys`; unknown config-key errors include `supported_keys`.
CLI recovery details mirror the MCP path: unknown top-level commands include `supported_commands`, unknown or missing nested subcommands include `supported_subcommands`, missing positional arguments include `expected_args`, and bad, mutually exclusive, or unexpected flags include `supported_flags` along with the offending `flag`, `flags`, or `args` where applicable.
For `agtx run`, use `--` to stop agtx flag parsing before skill arguments when the skill itself expects `-x` or `--name` style flags. Output-mode flags only count before that separator, so `agtx run demo -- --json` passes `--json` to the skill instead of switching agtx into JSON mode.

## Config Snippets

Print examples:

```sh
agtx agent targets
agtx agent targets --json
agtx agent init codex --print
agtx agent init cc --print
agtx agent init cursor --print
agtx agent init codex --json
```

Use `agtx agent targets` first when a wrapper needs to discover supported agent names and summaries.
`--print` is meant for humans to paste into config files. `--json` returns the same snippet as structured data (`target`, `display_name`, `aliases`, `summary`, `config_format`, `config_path_hints`, `config_snippet`, `command_snippet`, `rule_path_hints`, `rule_snippet`) plus `setup_steps`, a normalized ordered list of config/command/rule actions that wrappers can turn into guided setup UIs without scraping text. Each step now includes `priority`, `blocking`, `verification`, `platforms`, `applies_when`, `writes_files`, and `artifacts` metadata so launchers can order tasks, decide which ones must complete first, branch between user-level vs workspace-shared setup flows, and warn about which files will change before writing anything.

## Pro-aware Registry

Agents should call MCP `get_pro_setup` first when Pro-aware installs are possible. That preflight is no-side-effect: it does not refresh tokens, does not open a browser, and does not call the network. Use it to inspect whether `pro_api_url` is configured, whether a PKCE login is already pending, whether callback-scheme registration can be attempted automatically on the current platform, and which CLI/MCP actions are recommended next.

After `get_pro_setup`:

- If `recommended_actions` includes `configure_pro_api`, ask the user to configure `pro_api_url` before attempting login.
- If it includes `register_callback_scheme` and the platform supports it, call `register_pro_scheme` before launching browser login so `agtx://` callbacks can return cleanly. In v1 this is automated on macOS and Windows.
- If it includes `start_login`, call `start_pro_login` and show the returned `login_url` to the user.
- If it includes `complete_login`, pass the resulting `agtx://pro/callback?...` URI into `complete_pro_login`.
- If it includes `check_status`, call `get_pro_status` before Pro-only installs to confirm subscription and device state.

Outside MCP, mirror the same flow with `agtx pro setup --json`, `agtx pro register-scheme`, `agtx pro login --open`, `agtx pro callback ...`, and `agtx pro status --json`.

If the user wants to remove local auth state, call `logout_pro`. If registry refresh or package download returns `subscription_required`, surface the subscription state; if it returns `device_limit_exceeded`, call `list_pro_devices` and only call `revoke_pro_device` with `yes=true` after the user explicitly chooses a device. In both cases, prefer the server-provided `error.details.next_actions` list over hard-coded branching when it is available.
