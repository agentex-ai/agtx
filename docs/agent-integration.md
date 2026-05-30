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
- `get_status`
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

## JSON CLI Fallback

Use `--json` for one-shot calls:

```sh
agtx search "extract invoice totals" --json
agtx install xlsx --plan --json
agtx install xlsx --yes --json
agtx uninstall xlsx --plan --json
agtx doctor --json
agtx verify xlsx --json
agtx registry validate ./registry.json --json
agtx run xlsx --input input.json --json
```

Long-running tasks should use `--ndjson`:

```sh
agtx run research --input task.json --ndjson
```

Do not pass `--json` and `--ndjson` together. If initialization or input reading fails before a run starts, `--ndjson` still emits a single `failed` event instead of falling back to plain stderr.
For large inputs, set `--output-limit-bytes`; CLI mode uses the same byte limit for captured stdout/stderr and `--input file|-` reads.
If `run_skill` fails before execution, inspect the structured error: missing, directory, escaping, or non-executable entrypoints are reported as `invalid_argument` instead of raw process-launch errors.

## Config Snippets

Print examples:

```sh
agtx agent init codex --print
agtx agent init cc --print
agtx agent init cursor --print
```
