# Dependency Policy

`agtx` v1 keeps the dependency surface deliberately small.

## Runtime

- No Python, NPM, Homebrew, dynamic plugin host, or external service is required to run the CLI.
- Skills are installed as native executable packages described by a manifest.
- RapidOCR/PP-OCRv6 support is exposed through the built-in `ocr` manifest and
  aliases (`rapidocr`, `ppocrv6`). The OCR runtime path is native-only: Python
  and NPM wrappers are not used. The default binary provides the built-in OCR
  manifest and native probe path; optional adapter builds load ONNX Runtime or
  ncnn model files from the configured built-in OCR model directory.

## Go Code

- Standard library first.
- The default CLI build stays standard-library first; optional native OCR
  adapter builds may use a small Go binding to a native inference runtime.
- The `ocr_onnxruntime` build tag uses `github.com/yalue/onnxruntime_go` to
  load `onnxruntime.dll`, `libonnxruntime.so`, or `libonnxruntime.dylib`
  directly. This is a native runtime bridge, not a Python or NPM wrapper.
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
- Bundle URLs are limited to local paths, local `file://`, localhost or loopback `http://`, and remote `https://`; unsupported schemes are rejected before network access, and `file://` bundle URLs may not include query strings or fragments.
- Registry refresh and Pro API URLs must use `https://` with a host, except `http://localhost` and loopback URLs for local development.
- Remote registry refresh and HTTP package downloads use configurable timeouts (`registry_download_timeout_ms` and `package_download_timeout_ms`, both default 30000) and return structured `timeout` errors for agents.
- Skill archives only extract directories and regular files; symlinks, hard links, devices, and other special entries are rejected.
- Archive and entrypoint paths are interpreted with POSIX-style forward-slash semantics on every host; they must be non-empty, unique after cleaning, and contained inside the skill directory.
- Extracted file permissions are normalized to `0644` or `0755`; setuid, setgid, sticky, and overly broad write bits from archives are not preserved.

## Release Builds

Recommended release builds:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags "netgo osusergo" -trimpath -ldflags "-s -w" -o dist/agtx-darwin-arm64 ./cmd/agtx
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/agtx-windows-amd64.exe ./cmd/agtx
```

`CGO_ENABLED=0` does not mean a macOS binary has no system runtime linkage. The target is no third-party dynamic libraries and no third-party runtime.

Native OCR adapter builds are intentionally separate from the default release
profile:

```sh
CGO_ENABLED=1 go build -tags ocr_onnxruntime -trimpath -ldflags "-s -w" -o dist/agtx-ocr ./cmd/agtx
```

At runtime, set `AGTX_OCR_ONNXRUNTIME_LIBRARY` or place the ONNX Runtime shared
library next to the executable, under `AGTX_OCR_RUNTIME_DIR`, or under the OCR
model directory's `runtime` subdirectory. `agtx run rapidocr --
--download-runtime` downloads the Microsoft ONNX Runtime CPU archive for the
current platform, extracts only the native shared library, and removes the
archive unless `--keep-archive` is set; add `--dry-run` to inspect the URL and
destination first. The default runtime version is `1.26.0` to match the Go
binding headers. Microsoft does not publish a macOS Intel CPU archive for ONNX
Runtime 1.26.0, so Intel Mac users should point
`AGTX_OCR_ONNXRUNTIME_LIBRARY` at a locally installed compatible library or
explicitly select a compatible older runtime. Model files default to
`ppocrv6-det.onnx`, `ppocrv6-rec.onnx`, and `keys.txt`, and can be overridden
with `AGTX_OCR_DET_MODEL`, `AGTX_OCR_REC_MODEL`, and `AGTX_OCR_KEYS` or the
matching `agtx run rapidocr --` arguments.
For PP-OCRv6 ONNX exports from PaddlePaddle Hugging Face repositories, agtx
recognizes `PP-OCRv6_tiny_det_onnx/inference.onnx`,
`PP-OCRv6_tiny_rec_onnx/inference.onnx`, and the corresponding `small` and
`medium` directories. A recognizer `inference.yml` with
`PostProcess.character_dict` is accepted as the key dictionary, so no Python YAML
loader is required.
`agtx run rapidocr -- --download-models --model-size tiny|small|medium` downloads
those ONNX assets directly with the Go standard library HTTP client; add
`--dry-run` to inspect the asset plan without writing files.

The native OCR pipeline exposes RapidOCR/PaddleOCR-style tuning without adding
a Python runtime: `AGTX_OCR_DET_LIMIT_SIDE_LEN` (default `736`),
`AGTX_OCR_DET_THRESHOLD` (`0.3`), `AGTX_OCR_BOX_THRESHOLD` (`0.5`),
`AGTX_OCR_UNCLIP_RATIO` (`1.6`), `AGTX_OCR_MAX_CANDIDATES` (`1000`), and
`AGTX_OCR_TEXT_SCORE` (`0.5`). Recognition crop dimensions can be overridden
with `AGTX_OCR_REC_WIDTH` and `AGTX_OCR_REC_HEIGHT`; dynamic-width recognizer
models scale each crop up to `AGTX_OCR_REC_MAX_WIDTH` (`1600`) unless
`AGTX_OCR_REC_WIDTH` forces a fixed width.

## Release Audit

Run on macOS:

```sh
otool -L dist/agtx-darwin-arm64
```

The output should not include third-party dynamic libraries.
