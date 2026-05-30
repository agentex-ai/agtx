# Dependency Policy

`agtx` v1 keeps the dependency surface deliberately small.

## Runtime

- No Python, NPM, Homebrew, dynamic plugin host, or external service is required to run the CLI.
- Skills are installed as native executable packages described by a manifest.
- v1 built-in registry entries are stubs until real native skill packages are published.

## Go Code

- Standard library first.
- No third-party Go modules in v1.
- CLI parsing, JSON, HTTP, archive extraction, checksums, process execution, and MCP stdio are implemented with the Go standard library.
- Registry configuration is JSON file based; no embedded database is used.
- Config files are strictly decoded: unknown keys, `null` values, trailing JSON values, unsupported schema versions, invalid registry URLs, and non-positive numeric limits fail fast with structured `invalid_argument` errors.
- Registry validation and package install support local files without adding archive or database dependencies.
- Local diagnostics (`doctor` and `verify`) inspect JSON manifests, current pointers, and executable files with only the standard library.
- Registry refresh, skill package reads, and archive extraction enforce configurable byte/file limits (`registry_max_bytes`, `package_max_bytes`, `extracted_max_bytes`, `extracted_max_files`) before decoding, extracting, or writing files.
- Local state reads are bounded before decoding: config files, installed skill manifests, and `current` version pointers all have small fixed caps.
- Local state writes use same-directory temporary files, file sync, atomic rename, and directory sync on POSIX platforms for stronger crash consistency without adding a database dependency.
- Registry files and installed skill manifests are strictly decoded as a single JSON value and revalidated before use; manifest name/version identity must match the installation path and current pointer.
- Skill names, versions, OS IDs, and architecture IDs are treated as path segments with a small ASCII-safe character set; `/`, `\`, `.`, `..`, NUL bytes, surrounding whitespace, and shell-punctuation characters are rejected before filesystem access.
- Non-stub bundle manifests are validated before download: `url`, `sha256`, `archive`, platform IDs, and entrypoints must be well-formed.
- Bundle URLs are limited to local paths, local `file://`, `http://`, and `https://`; unsupported schemes are rejected before network access, and `file://` bundle URLs may not include query strings or fragments.
- Registry refresh URLs must be `http://` or `https://` with a host.
- Remote registry refresh and HTTP package downloads use configurable timeouts (`registry_download_timeout_ms` and `package_download_timeout_ms`, both default 30000) and return structured `timeout` errors for agents.
- Skill archives only extract directories and regular files; symlinks, hard links, devices, and other special entries are rejected.
- Archive and entrypoint paths are interpreted with POSIX-style forward-slash semantics on every host; they must be non-empty, unique after cleaning, and contained inside the skill directory.
- Extracted file permissions are normalized to `0644` or `0755`; setuid, setgid, sticky, and overly broad write bits from archives are not preserved.

## macOS Build

Recommended release build:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags "netgo osusergo" -trimpath -ldflags "-s -w" -o dist/agtx-darwin-arm64 ./cmd/agtx
```

`CGO_ENABLED=0` does not mean a macOS binary has no system runtime linkage. The target is no third-party dynamic libraries and no third-party runtime.

## Release Audit

Run on macOS:

```sh
otool -L dist/agtx-darwin-arm64
```

The output should not include third-party dynamic libraries.
