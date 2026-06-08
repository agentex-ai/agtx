# Pro Subscription Distribution

`agtx` Pro uses a hybrid design: the CLI stores a local device token, while Cloudflare Worker enforces subscription status, active-device limits, registry visibility, and package downloads.

## CLI

```sh
agtx config set pro_api_url https://agtx-pro.example.com
agtx pro setup
agtx pro register-scheme
agtx pro login --open
agtx pro callback "agtx://pro/callback?code=...&state=..."
agtx pro status --json
agtx pro devices
agtx pro revoke <device-id> --yes
agtx pro logout
```

Auth is stored in `auth.json` next to `config.json`. It contains a random `device_id`, device name, access token, refresh token, expiry, and a temporary PKCE login state. `config show` never prints token values.

`agtx pro setup` is the recommended first step for both humans and agents. It is a local-only preflight: it reads config/auth state, does not refresh tokens, and does not call the network. It reports whether login is already complete or pending, whether `pro_api_url` is configured, whether callback-scheme registration can be attempted automatically, and which next actions are recommended.

`agtx pro register-scheme` is automated on macOS and Windows. On macOS it creates a small local app bundle in the agtx config directory, registers `agtx://` through LaunchServices, and forwards the callback URI back into `agtx pro callback`.

`registry refresh`, Pro API calls, and HTTP package downloads refresh an expired access token when a refresh token is available. They attach `Authorization: Bearer <token>` and `X-AGTX-Device-ID` only when the target origin matches `pro_api_url` or `registry_url`; the Worker rejects requests where the device header does not match the bearer token.

When a Pro-gated request fails with `unauthorized`, `subscription_required`, or `device_limit_exceeded`, agtx now enriches the structured error details with a local `pro_setup` preview and ordered `next_actions`. That keeps both CLI JSON callers and MCP wrappers aligned on the same retry flow instead of re-deriving login/device/subscription recovery logic independently.

`agtx doctor` validates `auth.json` without blocking ordinary non-Pro commands. If the auth file is corrupt, run `agtx pro logout` and then `agtx pro login --open`.

## Worker

The starter Worker in `workers/agtx-pro` exposes:

- `/v1/registry`
- `/v1/packages/...`
- `/v1/cli/login/start`
- `/v1/cli/token`
- `/v1/pro/status`
- `/v1/devices`
- `/v1/devices/:id/revoke`
- `/webhooks/stripe`

D1 stores accounts, subscriptions, active devices, authorization codes, and refresh-token hashes. R2 stores the Pro registry at `registry/pro.json` and package archives under `/v1/packages/...` keys.

Apply the schema and run locally:

```sh
cd workers/agtx-pro
wrangler d1 execute agtx-pro --local --file schema.sql
wrangler dev
```

The starter has a demo account path for local testing. Replace that with real hosted login and Stripe Customer Portal before production.

The Worker returns the public registry to unauthenticated clients and merges Pro entries only for active/trialing subscriptions. Package downloads require an active subscription and reject unsafe package keys. Refresh tokens are one-time use and rotated through `/v1/cli/token` with `grant_type=refresh_token`.
