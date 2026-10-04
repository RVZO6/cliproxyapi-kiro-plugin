# Kiro Plugin

[中文](./README.md) | English

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that connects Kiro OAuth accounts to CLIProxyAPI as an upstream and exposes a standard Claude Messages interface. The upstream speaks AWS CodeWhisperer's private protocol; the plugin handles the Claude ↔ CodeWhisperer translation in both directions, supporting tool calls, image input, and streaming output.

## Build

### Maintained fork

Ryan's maintained fork is `RVZO6/cliproxyapi-kiro-plugin`, based on
`xiaokui-dev/cliproxyapi-kiro-plugin` (the original MIT license and module path
are retained). Custom patches should be built from this fork rather than
overwritten with plugin-store binaries.

Image blocks inside Claude `tool_result` content (including translated Codex
browser screenshots and image-viewing results) are forwarded in the enclosing
CodeWhisperer user message's `images` array. Tool-result text and call IDs are
preserved; images remain attached to their original conversation turn, including
history. Only inline base64 sources are supported; URL-only images are not fetched.

Before transmission, the entire request (including historical tool screenshots)
is counted and normalized using [Claude's documented vision limits](https://platform.claude.com/docs/en/build-with-claude/vision).
Requests with more than 20 images use a 2000-pixel maximum edge; smaller requests
retain resolution up to 8000 pixels. Images exceeding a conservative 5 MiB
base64 partner-transport budget are downscaled further. Resizing preserves aspect
ratio and uses PNG; it never upscales, drops images, or modifies stored originals.
Decoding is bounded to 64 megapixels and 16 MiB input base64 per image; unsafe or
invalid images return a local 400 instead of an opaque upstream failure. Identical
images are normalized once per request and reused across retries. This does not
assert Kiro's image-count or total-payload limits match the public Claude API.

The regression tests cover direct images, image-only/mixed tool results,
multiple images, invalid sources, duplicate results, and conversation history.
After installing a rebuilt library, restart CLIProxyAPI to load it.

When account-scoped model discovery is unavailable, the fallback catalog uses
[documented Kiro context windows](https://kiro.dev/docs/models/): 1M for GPT-5.6
Sol/Terra/Luna, Opus 4.6/4.7/4.8/5/5.5 and Sonnet 4.6/5; 128K for DeepSeek 3.2;
256K for Qwen3 Coder Next; 200K for the remaining listed routes. DeepSeek, GLM-5,
MiniMax M2.1/M2.5 and Qwen3 Coder Next explicitly advertise text-only input.
Positive account-reported token limits and explicit modalities override these
fallbacks. Opus 4.8 uses Kiro's documented 128K output; other routes keep the
conservative 8K output fallback pending Kiro-specific evidence.

Usage accounting is approximate, not exact tokenization or billing. Base64 image
bytes are excluded from the text estimate (with a 1600-token-per-image heuristic),
and output tool arguments are included. Tool errors retain their error status.
Non-streaming OpenAI Responses requests use the SSE intermediate representation
expected by the host's translator, identified from the original client envelope
because the host rewrites SourceFormat. Chat uses native OpenAI output (both
streaming and non-streaming); Messages keeps Claude JSON. This avoids silent
empty responses from host translators that expect an SSE buffer.

Remaining transport limitations: upstream generation is buffered before streaming;
reasoning deltas are not exposed; count-tokens is not implemented; URL images,
documents and audio are not supported; generation parameters such as max_tokens,
temperature and reasoning settings are not mapped into CodeWhisperer's private
request schema. Capability metadata does not enable these unimplemented features.

```bash
gofmt -w .
go test ./...
go vet ./...

# macOS (native arch, e.g. arm64)
CGO_ENABLED=1 go build -buildmode=c-shared -o kiro.dylib .

# Linux (build on a machine of the same GOOS/GOARCH)
# CGO_ENABLED=1 go build -buildmode=c-shared -o kiro.so .

# Windows
# CGO_ENABLED=1 go build -buildmode=c-shared -o kiro.dll .
```

## Install into CLIProxyAPI

> Note: do not use the official release archives with a `no-plugin` suffix (the musl/OpenWrt portable build, FreeBSD arm64) — they cannot load dynamic library plugins. The mainstream macOS / Windows / default Linux / Docker builds all load them fine.

Place the shared library in the plugin directory, in per-platform subdirectories:

```
plugins/darwin/arm64/kiro.dylib
plugins/linux/amd64/kiro.so
plugins/windows/amd64/kiro.dll
```

Enable it in the host's `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    kiro:
      enabled: true
      # Leave idc_start_url empty to log in with AWS Builder ID; set it to use organization IDC. See the table below.
      # idc_start_url: "https://d-xxxx.awsapps.com/start"
      # idc_region: "us-east-1"
```

## Configuration fields

`plugins.configs.kiro.*`:

| Field | Type | Description |
|---|---|---|
| `idc_start_url` | string | Organization IAM Identity Center portal start URL, e.g. `https://d-xxxx.awsapps.com/start`. **When set, login goes through organization IDC; when empty, login uses AWS Builder ID (a personal account).** |
| `idc_region` | string | AWS Region that hosts your Identity Center instance. Used only for IDC login; defaults to `us-east-1` when empty. |

## Login methods

The login method is **inferred** from whether `idc_start_url` is set:

- **AWS Builder ID (default, personal free account)**: leave `idc_start_url` empty. After saving, start the device-code login from the "Kiro OAuth" page in the panel and authorize in the browser — no other fields needed.
- **Organization IDC (IAM Identity Center)**: fill in `idc_start_url` (the organization portal URL), and `idc_region` if needed (defaults to `us-east-1` when empty). Start the device-code login from the "Kiro OAuth" page after saving.
- **Google / GitHub**: no interactive login (limited by the upstream Cognito callback allowlist). Sign in with the Kiro desktop app, then export the credential JSON into `auth-dir`. The JSON must contain `accessToken`, `refreshToken`, `profileArn`, `authMethod` (value `social`), and `region` (usually `us-east-1`); it refreshes automatically after import.

Verify after login:

```bash
curl -s http://127.0.0.1:8317/v1/models | grep -i claude
```

## License

[MIT](./LICENSE)
