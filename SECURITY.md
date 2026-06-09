# Security Policy

## Supported Versions

Security fixes target the `main` branch until versioned releases are published.

## Reporting a Vulnerability

Please do not open public issues for suspected vulnerabilities.

Report security issues to the maintainers by private GitHub security advisory, or contact the repository owner privately if advisories are unavailable. Include:

- Affected commit or version.
- Reproduction steps or proof of concept.
- Expected impact.
- Any relevant logs with secrets removed.

We aim to acknowledge reports promptly and coordinate disclosure after a fix is available.

## Handling Secrets

Do not commit local auth files, Pro tokens, `.env` files, Cloudflare/Stripe credentials, generated release binaries, or private worker code. If a secret is accidentally pushed, revoke it first, then report the exposure so history can be cleaned.
