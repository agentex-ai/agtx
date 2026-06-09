# Contributing

Thanks for helping improve `agtx`.

## Development

Use a recent Go toolchain and keep the dependency surface small:

```sh
go test ./...
```

Before opening a pull request:

- Keep changes focused and avoid unrelated rewrites.
- Add or update tests for behavior changes.
- Do not commit generated binaries, local config, auth files, `.env`, `.dev.vars`, or private service code.
- Prefer standard-library solutions unless a dependency is clearly justified.
- Run `gofmt` on changed Go files.

## Security-Sensitive Changes

For changes touching auth, package downloads, registry validation, archive extraction, Pro flows, local HTTP serving, or filesystem writes, include tests for the failure path as well as the success path.

Report vulnerabilities privately; see `SECURITY.md`.
