# apkgo

![GitHub release](https://img.shields.io/github/v/release/KevinGong2013/apkgo) ![build](https://img.shields.io/github/actions/workflow/status/kevingong2013/apkgo/release.yml?style=flat-square) ![go](https://img.shields.io/github/go-mod/go-version/kevingong2013/apkgo?style=flat-square) ![license](https://img.shields.io/github/license/kevingong2013/apkgo?style=flat-square)

🌐 **Language / 语言**: **English** · [简体中文](./README.md)

One command to publish an APK to every major Chinese Android app store. Built for CI/CD and AI agents.

> **Don't want to run CI or touch a terminal?** Try the hosted [**apkgo cloud**](https://apkgo.baici.tech) — release from a browser, with credentials stored server-side, multi-user collaboration, and full release history. No install, no ops, friendly enough for ops and PM teammates.

## Install

```bash
# AI Agent Skill (works with Claude Code, Cursor, Windsurf, and 40+ agents)
npx skills add KevinGong2013/apkgo

# macOS (Apple Silicon)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Darwin_arm64.tar.gz | tar xz -C /usr/local/bin apkgo

# macOS (Intel)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Darwin_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# Linux (x86_64)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# Linux (arm64)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_arm64.tar.gz | tar xz -C /usr/local/bin apkgo

# Windows (PowerShell)
# Download apkgo_Windows_x86_64.zip from https://github.com/KevinGong2013/apkgo/releases/latest
# Extract and add apkgo.exe to PATH

# Go
go install github.com/KevinGong2013/apkgo@latest

# Docker
docker pull ghcr.io/kevingong2013/apkgo:latest
```

## Quick start

```bash
# 1. Generate a config file
apkgo init

# 2. Fill in store credentials
vim apkgo.yaml

# 3. Upload
apkgo upload -f app.apk
```

## Usage

### Upload

```bash
# Upload to every configured store
apkgo upload -f app.apk

# Upload to specific stores
apkgo upload -f app.apk --store huawei,xiaomi

# With release notes
apkgo upload -f app.apk --notes "Fixed login issue"
apkgo upload -f app.apk --notes-file CHANGELOG.md

# Split-arch APKs
apkgo upload -f app-arm32.apk --file64 app-arm64.apk

# Validate without uploading
apkgo upload -f app.apk --dry-run
```

### Initialize config

```bash
# Generate a config containing every supported store
apkgo init

# Only generate config for the stores you need
apkgo init --store huawei,xiaomi

# Custom config path
apkgo init -c production.yaml
```

### List supported stores

```bash
apkgo stores
```

### Doctor (validate store credentials)

`doctor` checks whether your configured credentials and permissions are correct **without uploading any file**, so you don't discover problems halfway through a real release:

```bash
# Validate every configured store
apkgo doctor

# Validate a specific store
apkgo doctor -s huawei

# Pass a package name for deeper checks (package mapping, release permission, ...)
apkgo doctor -s huawei -p com.example.app
apkgo doctor -s huawei -f app.apk         # auto-extract package from APK
```

Exit code is 1 if any probe fails. Huawei is fully supported today; the other stores currently report `doctor not implemented`.

## Configuration file

`apkgo.yaml`:

```yaml
# hooks are optional; omit if you don't need them
hooks:
  before: "./scripts/validate.sh"          # runs before any upload
  after: "./scripts/notify.sh"             # runs after all uploads

stores:
  huawei:
    # Recommended: service account (PS256 JWT)
    service_account_file: "/secure/path/huawei-sa.json"
    # or: service_account: "<base64(JSON)>"
    # app_id: ""  # optional, auto-detected from package name when omitted
    before: "./scripts/before-huawei.sh"   # optional, runs before this store's upload
    after: "./scripts/after-huawei.sh"     # optional, runs after this store's upload

  xiaomi:
    email: "your@email.com"
    private_key: "your-private-key"             # the value Xiaomi's console calls "interface key" (used as `password` by their SDK)
    cert_file: "/secure/path/xiaomi-pubkey.cer" # public-key certificate (also accepts cert: <PEM> or cert: <base64>)

  oppo:
    client_id: "your-client-id"        # 19-digit number
    client_secret: "your-client-secret"

  vivo:
    access_key: "your-access-key"
    access_secret: "your-access-secret"

  honor:
    client_id: "your-client-id"
    client_secret: "your-client-secret"
    app_id: "your-app-id"

  tencent:
    user_id: "your-user-id"
    access_secret: "your-access-secret"
    app_id: "your-app-id"
    # Multi-app: app_id_map: '{"com.foo":"111","com.bar":"222"}'

  pgyer:
    api_key: "your-pgyer-api-key"

  fir:
    api_token: "your-fir-api-token"

  # Single script
  script:
    command: "./deploy.sh"

  # Multiple script instances (script.<name>)
  script.cdn-upload:
    command: "./upload-cdn.sh"
  script.dingtalk:
    command: "./notify-dingtalk.sh"
```

#### Hooks

Hooks are optional; if not configured they don't run. Hook scripts receive a JSON context on stdin and control the flow via exit code:

- `before` hook fails (non-zero exit) → upload is aborted
- `after` hook fails → logged as a warning, result is unaffected
- Auto-injected env vars: `APKGO_STORE`, `APKGO_PACKAGE`, `APKGO_VERSION`
- stderr output is captured as the error message

**Global before hook** (`hooks.before`) stdin:

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "stores": ["huawei", "xiaomi"]
}
```

**Global after hook** (`hooks.after`) stdin:

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "results": [
    {"store": "huawei", "success": true, "duration_ms": 12300},
    {"store": "xiaomi", "success": false, "error": "auth failed", "duration_ms": 400}
  ]
}
```

**Per-store before hook** (`stores.<name>.before`) stdin:

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei"
}
```

**Per-store after hook** (`stores.<name>.after`) stdin:

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei",
  "result": {"store": "huawei", "success": true, "duration_ms": 12300}
}
```

Example hook script (DingTalk notification after upload):

```bash
#!/bin/bash
input=$(cat)
results=$(echo "$input" | jq -r '.results[] | "\(.store): \(if .success then "✓" else "✗ "+.error end)"')
curl -s -X POST "$DINGTALK_WEBHOOK" \
  -H "Content-Type: application/json" \
  -d "{\"msgtype\":\"text\",\"text\":{\"content\":\"APK release done v${APKGO_VERSION}\n${results}\"}}"
```

### Environment variables

In CI/CD you can supply credentials purely via env vars without a config file:

```bash
# Format: APKGO_<STORE>_<FIELD>=value
export APKGO_HUAWEI_SERVICE_ACCOUNT="$(base64 -w0 huawei-sa.json)"  # recommended
export APKGO_XIAOMI_EMAIL="your@email.com"
export APKGO_XIAOMI_PRIVATE_KEY="your-interface-key"
export APKGO_XIAOMI_CERT="$(base64 -w0 xiaomi-pubkey.cer)"
export APKGO_OPPO_CLIENT_ID="your-19-digit-id"
export APKGO_OPPO_CLIENT_SECRET="your-secret"
export APKGO_VIVO_ACCESS_KEY="your-key"
export APKGO_VIVO_ACCESS_SECRET="your-secret"
export APKGO_TENCENT_USER_ID="your-user-id"
export APKGO_TENCENT_ACCESS_SECRET="your-secret"
export APKGO_TENCENT_APP_ID="your-app-id"
# Multi-app: APKGO_TENCENT_APP_ID_MAP='{"com.foo":"111","com.bar":"222"}'
export APKGO_PGYER_API_KEY="your-pgyer-key"
export APKGO_FIR_API_TOKEN="your-fir-token"

# Env vars override fields of the same name in the config file
# A config file is not required if everything is configured through env vars
apkgo upload -f app.apk --store huawei
```

### Credentials never on disk (cloud worker / CI / multi-tenant)

`--creds-from` lets apkgo read JSON-encoded credentials from a non-disk source. An orchestrator can pull credentials from a secrets manager (Vault / AWS SM / GCP SM, etc.) and inject them straight into the child process via stdin or a file descriptor — **never written to disk, never put into env**:

```bash
# Option A: stdin
vault read -format=json secret/apkgo | jq .data \
  | apkgo upload -f app.apk --creds-from=stdin

# Option B: file descriptor (when stdin is already taken)
apkgo upload -f app.apk --creds-from=fd:3 3<<<"$(vault-creds-as-json)"
```

The JSON shape mirrors the YAML one-to-one:

```json
{
  "stores": {
    "huawei": {"service_account": "<base64>"},
    "tencent": {
      "user_id": "...",
      "access_secret": "...",
      "app_id_map": "{\"com.foo\":\"111\"}"
    }
  },
  "hooks": {"before": "...", "after": "..."}
}
```

apkgo zeroes out the input bytes immediately after parsing so secrets don't linger in buffers. When `--creds-from` is set, both `--config` and `APKGO_*` env vars are ignored.

### Where to get credentials

| Store | Console | Notes |
|------|-----------|------|
| Huawei | [AppGallery Connect](https://developer.huawei.com/consumer/cn/console) | Users & permissions > Service account ([details](#huawei-appgallery-connect)) |
| Xiaomi | [Xiaomi Open Platform](https://dev.mi.com) | Account > Interface key ([details](#xiaomi-open-platform)) |
| OPPO | [OPPO Open Platform](https://open.oppomobile.com) | Management > API key management ([details](#oppo-open-platform)) |
| vivo | [vivo Open Platform](https://dev.vivo.com.cn) | Account > API access ([details](#vivo-open-platform)) |
| Honor | [Honor Developer](https://developer.honor.com) | API management ([details](#honor-developer-platform)) |
| Tencent | [Tencent Open Platform](https://app.open.qq.com) | App > Account > API publish > Apply ([details](#tencent-app-store-yingyongbao)) |
| Pgyer | [pgyer.com](https://www.pgyer.com/account/api) | Account > API key ([details](#pgyer)) |
| fir.im | [betaqr.com.cn](https://www.betaqr.com.cn) | Account > API Token ([details](#firim)) |

Each vendor's credential application flow is governed by their official docs (linked below). This README only describes **what is apkgo-specific**: which fields you need, how `doctor` validates them, and any non-obvious behavior.

#### Huawei AppGallery Connect

📖 Official docs: [Service Account integration](https://developer.huawei.com/consumer/cn/doc/AppGallery-connect-Guides/agcapi-getstarted-0000001111845114#section1785535363715)

Use a **developer-level** service account (PS256 JWT auth) — not a project-level one, which would be rejected by the publish API. Hand the downloaded JSON credential straight to apkgo:

```yaml
stores:
  huawei:
    service_account_file: "/secure/path/huawei-sa.json"
    # or inline base64(JSON): service_account: "ewogICJrZXlfaWQiOiAi..."
```

The legacy `client_id` + `client_secret` form is still accepted but deprecated by Huawei.

```bash
apkgo doctor -s huawei -p com.example.app
```

Three probes: `token` / `appid-list` (package name → appId) / `release-permission` (app release permission).

#### Xiaomi Open Platform

📖 Official docs: [API upload](https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1134)

From the Xiaomi console's "interface key" page you need two things: the **interface key** (called `password` inside the SDK) and the **public-key certificate** (`.cer` file). Both are bound to the developer account.

```yaml
stores:
  xiaomi:
    email: "<developer account email>"
    private_key: "<interface key>"
    cert_file: "/secure/path/xiaomi-pubkey.cer"
    # also accepted: cert: "-----BEGIN CERTIFICATE-----..." or base64(.cer)
```

```bash
apkgo doctor -s xiaomi -p com.example.app
```

> ⚠️ Pre-v3.0 versions shipped a built-in public-key certificate, but it **expired in 2023-05** (and its origin was unclear). From v3.0 onward you must provide your own.

#### OPPO Open Platform

📖 Official docs: [Publish API guide](https://open.oppomobile.com/new/developmentDoc/info?id=10998)

```yaml
stores:
  oppo:
    client_id: "<19-digit number>"
    client_secret: "<secret>"
```

```bash
apkgo doctor -s oppo -p com.example.app
```

OPPO's publish flow is asynchronous. apkgo handles two non-obvious states automatically: on `911216 task processing`, it skips the publish call and waits for the task to settle; on `911215 app under review`, it treats the result as success (already in the review queue).

#### vivo Open Platform

📖 Official docs: [Open API guide](https://dev.vivo.com.cn/documentCenter/doc/326)

```yaml
stores:
  vivo:
    access_key: "<...>"
    access_secret: "<...>"
```

```bash
apkgo doctor -s vivo -p com.example.app
```

vivo's error codes come in two layers: gateway `code` plus business `subCode`. apkgo recognizes both layers and surfaces the original message verbatim (e.g. `[15042] please upload an APK signed with the same signature as before...`).

#### Honor Developer Platform

📖 Official docs: [Publish API guide](https://developer.honor.com/cn/doc/guides/101159)

```yaml
stores:
  honor:
    client_id: "<...>"
    client_secret: "<...>"
    # app_id: ""  # optional; resolved by package name when omitted
```

```bash
apkgo doctor -s honor -p com.example.app
```

The doctor `app-detail` probe pre-checks the *app introduction* (intro) field — Honor requires it to be filled in the console, otherwise `update-language-info` fails with `[20076] app introduction is empty`. Fill it in the console before releasing.

#### Tencent App Store (YingYongBao)

📖 Official docs: [API upload integration](https://wikinew.open.qq.com/index.html#/iwiki/4015262492)

Tencent provides no list endpoint and no package→id reverse lookup, so `app_id` must be supplied explicitly. To serve multiple apps from one yaml, use `app_id_map`:

```yaml
stores:
  tencent:
    user_id: "<developer ID>"
    access_secret: "<interface secret>"
    # Single app:
    app_id: "<app ID>"
    # Multi-app: matched by APK package name
    # app_id_map: '{"com.example.foo":"111","com.example.bar":"222"}'
```

```bash
apkgo doctor -s tencent -p com.example.app
```

Publish is asynchronous. apkgo polls `query_app_update_status` until `audit_status` reaches a terminal state (max 5 minutes); a timeout is treated as success (the task has been handed off to Tencent).

#### Pgyer

📖 Official docs: [API upload](https://www.pgyer.com/doc/view/app_upload)

```yaml
stores:
  pgyer:
    api_key: "<...>"
```

```bash
apkgo doctor -s pgyer -p com.example.app
```

#### fir.im

📖 Official docs: [betaqr.com.cn/docs](https://www.betaqr.com.cn/docs)

```yaml
stores:
  fir:
    api_token: "<...>"
```

```bash
apkgo doctor -s fir
```

> ⚠️ **fir uploads require a real-name verified account**. Without it, the `/apps` endpoint refuses with `cannot upload app without real-name verification`. Complete real-name verification in the console first.

## AI agent integration

apkgo's output format is designed for AI agents and automation:

**Structured JSON output** (stdout):

```json
{
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1},
  "results": [
    {"store": "huawei", "success": true, "category": "success", "duration_ms": 12300},
    {"store": "oppo",   "success": true, "category": "already_done", "duration_ms": 3200},
    {"store": "xiaomi", "success": false, "category": "policy_block", "error": "signature mismatch...", "duration_ms": 400}
  ]
}
```

**Category (retry-decision hints)**: every result carries a `category` field that buckets each vendor's wildly different error codes into a small set friendly to a cloud orchestrator, so the parent process doesn't have to parse Chinese error messages. Possible values:

| Category | Meaning | Suggested handling |
|---|---|---|
| `success` | Upload succeeded | mark done |
| `already_done` | Version is already on the store side (e.g. OPPO 911215 under review) | mark done, do not retry |
| `auth_failed` | Bad credentials | ask user to fix the secret, do not retry |
| `network_retry` | Network timeout / 5xx | retry with backoff |
| `store_busy` | Store rate-limit / previous task still running | wait a few minutes and retry |
| `policy_block` | Business rule rejection (signature mismatch, review denied, ...) | hand to user, do not retry |
| `config_invalid` | Backend metadata missing (intro / category / publisher entity) | ask user to fill it in the console, do not retry |
| `unknown` | Not yet classified | default to "non-retryable" |

**Semantic exit codes**:

| Code | Meaning |
|------|------|
| 0 | All succeeded |
| 1 | Partial failure |
| 2 | All failed |
| 3 | Input error |

**Discoverable config schema**:

```bash
apkgo stores  # returns each store's required config fields
```

**Non-interactive**: no prompts, no confirmations — fits headless environments.

**Live progress stream (NDJSON)**: when a parent process forks apkgo and wants real-time progress, add `--progress-stream` and stdout becomes one JSON event per line:

```bash
apkgo upload -f app.apk --progress-stream
```

```json
{"type":"start","apk":{"package":"...","version_name":"1.2.0","version_code":120},"stores":["huawei","xiaomi"]}
{"type":"phase","store":"huawei","phase":"auth"}
{"type":"phase","store":"huawei","phase":"uploading"}
{"type":"total","store":"huawei","total_bytes":62914560}
{"type":"bytes","store":"huawei","sent":7045120,"total":62914560}
{"type":"bytes","store":"huawei","sent":23560192,"total":62914560}
{"type":"phase","store":"huawei","phase":"submitting"}
{"type":"result","store":"huawei","success":true,"duration_ms":34570}
{"type":"done","apk":{...},"results":[...]}
```

`bytes` events are throttled to roughly one every ~100ms; concurrent stores each get their own stream, distinguished by the `store` field. Go parent-process consumer:

```go
cmd := exec.CommandContext(ctx, "apkgo", "upload", "-f", apkPath, "--progress-stream")
out, _ := cmd.StdoutPipe()
cmd.Start()
sc := bufio.NewScanner(out)
for sc.Scan() {
    var evt map[string]any
    json.Unmarshal(sc.Bytes(), &evt)
    switch evt["type"] {
    case "bytes":
        ui.UpdateProgress(evt["store"].(string), evt["sent"].(float64), evt["total"].(float64))
    case "result":
        ui.MarkStoreDone(evt["store"].(string), evt["success"].(bool))
    case "done":
        ui.Finish(evt["results"])
    }
}
cmd.Wait()
```

## Embed in Go (SDK)

apkgo is both a CLI **and** a Go library. Cloud workers, custom CI tooling, and IDE plugins can import `pkg/apkgo` directly to drive the upload pipeline, skipping the spawn-subprocess + parse-stdout dance.

```go
import (
    "context"
    "github.com/KevinGong2013/apkgo/pkg/apkgo"
    "github.com/KevinGong2013/apkgo/pkg/config"
    "github.com/KevinGong2013/apkgo/pkg/uploader"
)

cfg := &config.Config{
    Stores: map[string]map[string]string{
        "huawei":  {"service_account": vault.Get("huawei-sa-base64")},
        "tencent": {
            "user_id":       "...",
            "access_secret": vault.Get("tencent-secret"),
            "app_id_map":    `{"com.foo":"111","com.bar":"222"}`,
        },
    },
}

result, err := apkgo.Run(ctx, apkgo.Job{
    APKFile:  "https://artifacts.example.com/v1.2.0.apk",
    Stores:   []string{"huawei", "tencent"},
    Notes:    "Bug fixes",
    Config:   cfg,
    Progress: uploader.NopManager,  // or NewNDJSONManager(w) for streamed events
})
```

Features:

- **Zero global state**: `apkgo.Run` does not touch `slog.Default` or set the process exit code, so it's safe to call from a long-running process.
- **URL inputs**: `APKFile` / `APKFile64` accept a local path or http(s) URL (auto-fetched to a temp file, cleaned up on exit).
- **Pluggable progress**: the `uploader.ProgressManager` interface lets you plug in mpb / NDJSON / a custom implementation.
- **`error` only for pre-upload failures**: the `error` returned by `Run` only covers fetch / parse / config phases; once uploading begins, each store's failure lives in `Result.Results[i].Error`.
- **Cloud-worker-friendly extras**:

  ```go
  apkgo.Run(ctx, apkgo.Job{
      // ... config / file / stores / etc

      // 1. Per-job logger (cloud injects job_id / tenant_id / trace_id)
      Logger: slog.New(handler).With("job_id", id, "tenant", tid),

      // 2. Metric emit hook (store.start / store.end / hook.run)
      Events: func(ev uploader.Event) {
          if ev.Type == uploader.EventStoreEnd {
              prom.UploadCounter.WithLabelValues(ev.Store, string(ev.Result.Category)).Inc()
              prom.UploadDuration.WithLabelValues(ev.Store).Observe(ev.Duration.Seconds())
          }
      },
  })
  ```

  Each store can also have its own timeout in yaml (overriding the global `--timeout`):

  ```yaml
  stores:
    huawei:
      service_account_file: "..."
      timeout: 8m              # waiting on submit polling — be generous
    pgyer:
      api_key: "..."
      timeout: 30s             # simple upload — keep tight
  ```

For the full API, see the godoc for [`pkg/apkgo`](pkg/apkgo).

## CI/CD examples

### GitHub Actions

```yaml
- name: Upload to app stores
  env:
    APKGO_HUAWEI_SERVICE_ACCOUNT: ${{ secrets.HUAWEI_SERVICE_ACCOUNT }}  # base64(JSON credential)
    APKGO_XIAOMI_EMAIL: ${{ secrets.XIAOMI_EMAIL }}
    APKGO_XIAOMI_PRIVATE_KEY: ${{ secrets.XIAOMI_PRIVATE_KEY }}
    APKGO_XIAOMI_CERT: ${{ secrets.XIAOMI_CERT }}             # base64(.cer file)
    APKGO_OPPO_CLIENT_ID: ${{ secrets.OPPO_CLIENT_ID }}
    APKGO_OPPO_CLIENT_SECRET: ${{ secrets.OPPO_CLIENT_SECRET }}
    APKGO_VIVO_ACCESS_KEY: ${{ secrets.VIVO_ACCESS_KEY }}
    APKGO_VIVO_ACCESS_SECRET: ${{ secrets.VIVO_ACCESS_SECRET }}
    APKGO_TENCENT_USER_ID: ${{ secrets.TENCENT_USER_ID }}
    APKGO_TENCENT_ACCESS_SECRET: ${{ secrets.TENCENT_ACCESS_SECRET }}
    APKGO_TENCENT_APP_ID_MAP: ${{ secrets.TENCENT_APP_ID_MAP }}     # multi-app: '{"com.foo":"111",...}'
  run: |
    apkgo upload \
      -f app/build/outputs/apk/release/app-release.apk \
      --notes-file CHANGELOG.md \
      --store huawei,xiaomi,oppo,vivo,tencent \
      --timeout 15m
```

### Docker

```bash
docker run --rm \
  -v $(pwd)/apkgo.yaml:/apkgo.yaml \
  -v $(pwd)/app.apk:/app.apk \
  ghcr.io/kevingong2013/apkgo:latest \
  upload -f /app.apk --notes "Bug fixes"
```

## Web GUI

Don't want the command line? `apkgo serve` starts a local web UI:

```bash
apkgo serve                    # http://localhost:8080
apkgo serve -p 9090            # custom port
apkgo serve -c production.yaml # custom config file
```

Open the browser, drop in an APK, pick the stores, fill in release notes, click upload. Friendly for non-engineering teammates.

## Sync config across machines

Encrypted export/import for safely sharing store credentials between machines or with CI:

```bash
# Machine A: encrypted export
apkgo config export --out config.enc
# Enter a password (or set the APKGO_CONFIG_KEY env var)

# Push to a private repo
git add config.enc && git commit -m "sync config" && git push

# Machine B: decrypted import
git pull
apkgo config import config.enc
```

Non-interactive use in CI:

```yaml
- name: Import apkgo config
  env:
    APKGO_CONFIG_KEY: ${{ secrets.APKGO_CONFIG_KEY }}
  run: apkgo config import config.enc
```

Encryption: AES-256-GCM with scrypt key derivation; a wrong password produces a clear error.

## All commands

```
apkgo init          [-s store1,store2] [-c config.yaml]
apkgo upload        -f <apk> [--file64 <apk>] [-s stores] [-n notes] [--notes-file path] [--dry-run] [-t timeout]
apkgo doctor        [-s stores] [-f <apk> | -p <package>]
apkgo serve         [-p port] [-c config.yaml]
apkgo config export --out <file>
apkgo config import <file>
apkgo stores        [-o json|text]
apkgo history       [-n limit]
apkgo upgrade
apkgo version       [-o json|text]
```

## Global flags

```
-c, --config        config file path (default: apkgo.yaml)
-o, --output        output format: json or text (default: json)
-t, --timeout       global timeout (default: 10m)
-v, --verbose       verbose logs to stderr
    --no-telemetry  disable anonymous usage telemetry
```

## Privacy

apkgo collects anonymous usage stats to improve the product. **It never collects sensitive information**:

| Collected | Not collected |
|------|--------|
| Anonymous install ID (random UUID) | Accounts, credentials |
| Store names used | Package name, app name |
| Upload success/failure | APK file content |
| CLI/GUI usage | Release notes content |
| apkgo version, OS/arch | IP address |

Opt out: `--no-telemetry` or `APKGO_TELEMETRY=off`

## License

[Apache License 2.0](./LICENSE)
