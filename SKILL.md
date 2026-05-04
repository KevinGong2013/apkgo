---
name: apkgo
description: Upload APK files to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Tencent, Google Play, Samsung, Pgyer, fir.im) via the standalone CLI or the hosted apkgo cloud SaaS. Designed for AI agents with structured JSON output, semantic exit codes, and zero interaction.
---

# apkgo

Upload APK files to multiple Android app stores in one command.

**Two ways to use:**

- **CLI** (`apkgo upload`): single-binary, credentials live on the box that runs it. Best for solo developers, local releases, single-machine CI.
- **Cloud SaaS** (`apkgo.baici.tech`, REST API + dashboard): credentials encrypted server-side and shared across a team, multi-app workspaces, async upload with webhook callbacks. Best for CI/CD without secret-shipping, multi-developer teams, audit trails. See the `Cloud SaaS` section below.

The CLI sections below describe the local binary; everything cloud-specific is grouped at the end.

## Install

```bash
# macOS / Linux — auto-detects OS/arch, verifies SHA-256
curl -fsSL https://apkgo.com.cn/install.sh | sh

# If /usr/local/bin is not writable:
#   curl -fsSL https://apkgo.com.cn/install.sh | sudo sh
#   APKGO_INSTALL_DIR="$HOME/.local/bin" sh -c "$(curl -fsSL https://apkgo.com.cn/install.sh)"

# Alternatives
go install github.com/KevinGong2013/apkgo@latest          # Go toolchain
docker pull ghcr.io/kevingong2013/apkgo:latest            # Docker

# Windows: download apkgo_Windows_x86_64.zip from
#   https://github.com/KevinGong2013/apkgo/releases/latest
# then add apkgo.exe to PATH.
```

## When to use

Use this skill when the user wants to:
- Upload/publish/distribute an APK to Android app stores
- Release an Android app to Huawei, Xiaomi, OPPO, vivo, Honor, Tencent, Google Play, or Samsung stores
- Upload an APK to Pgyer or fir.im for beta distribution
- Automate APK distribution in CI/CD pipelines
- Run custom upload/notify scripts via the script store

## Supported stores

huawei, xiaomi, oppo, vivo, honor, tencent, googleplay, samsung, pgyer, fir, script

## Commands

```bash
apkgo stores                    # Discover config schema for each store (JSON)
apkgo init [--store names]      # Generate config file with comments
apkgo upload -f <apk> [flags]   # Upload APK to configured stores
apkgo version                   # Version info
```

## Upload flags

```
-f, --file         APK file path (required)
    --file64       64-bit APK for split-arch uploads
-s, --store        Comma-separated store names (default: all configured)
-n, --notes        Release notes text
    --notes-file   Read release notes from file
    --dry-run      Validate without uploading
-t, --timeout      Timeout duration (default: 10m)
-c, --config       Config file path (default: apkgo.yaml)
```

## Configuration

Create `apkgo.yaml` or use environment variables `APKGO_<STORE>_<KEY>`:

```yaml
# Hooks (optional): shell commands executed before/after uploads.
# hooks:
#   before: "./scripts/validate.sh"
#   after: "./scripts/notify.sh"

stores:
  huawei:
    # Recommended: paste the AGC Service Account JSON (raw or base64).
    # client_id/client_secret still work but are deprecated by Huawei.
    service_account: ""
    # service_account_file: "/path/to/sa.json"  # alternative to inline
  xiaomi:
    email: ""           # required - developer account email
    private_key: ""     # required - from dev.mi.com API management
    cert: ""            # required - public-key certificate (PEM/base64)
    # cert_file: "/path/to/pubkey.cer"  # alternative to inline
  oppo:
    client_id: ""       # required - from open.oppomobile.com
    client_secret: ""   # required
  vivo:
    access_key: ""      # required - from dev.vivo.com.cn
    access_secret: ""   # required
  honor:
    client_id: ""       # required - from developer.honor.com
    client_secret: ""   # required
    # app_id auto-detected from APK package name; set only to override.
  tencent:
    user_id: ""         # required - from app.open.qq.com
    access_secret: ""   # required - API access secret
    # Multi-app: map APK package_name → tencent app_id.
    app_id_map: '{"com.example.app":"1234567"}'
    # Single-app fallback (used when app_id_map is empty):
    # app_id: ""
  googleplay:
    json_key_path: ""   # required - service account JSON key file
    track: "internal"   # release track (default: internal)
  samsung:
    service_account_id: ""  # required
    private_key_path: ""    # required

  # Script store: run any shell command or script.
  # Receives APK metadata as JSON on stdin; exit 0 = success.
  # script:
  #   command: "./deploy.sh"

  # Multiple script instances via "script.<name>" prefix:
  # script.cdn-upload:
  #   command: "./upload-cdn.sh"
  # script.dingtalk:
  #   command: "./notify-dingtalk.sh"
```

Environment variable example:
```bash
APKGO_HUAWEI_CLIENT_ID=xxx APKGO_HUAWEI_CLIENT_SECRET=yyy apkgo upload -f app.apk --store huawei
```

## Hooks

Hooks are optional shell commands executed before/after uploads. They receive JSON context on stdin and environment variables `APKGO_STORE`, `APKGO_PACKAGE`, `APKGO_VERSION`.

- `before` hook exit non-zero → abort upload
- `after` hook exit non-zero → warning only

**Global before** (`hooks.before`) stdin:
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "stores": ["huawei", "xiaomi"]
}
```

**Global after** (`hooks.after`) stdin:
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

**Per-store before** (`stores.<name>.before`) stdin:
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei"
}
```

**Per-store after** (`stores.<name>.after`) stdin:
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei",
  "result": {"store": "huawei", "success": true, "duration_ms": 12300}
}
```

## Script store stdin

The script store (`command` field) receives the same JSON on stdin:
```json
{
  "file_path": "/path/to/app.apk",
  "file_64_path": "",
  "app_name": "MyApp",
  "package_name": "com.example.app",
  "version_code": 42,
  "version_name": "1.2.0",
  "release_notes": "Bug fixes"
}
```

Exit 0 = success, non-zero = failure (stderr as error message).

## Output format

All output is structured JSON on stdout (logs go to stderr):

```json
{
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1},
  "results": [
    {"store": "huawei", "success": true, "duration_ms": 12300},
    {"store": "xiaomi", "success": false, "error": "invalid private key", "duration_ms": 400}
  ]
}
```

## Exit codes

- 0: All succeeded
- 1: Partial failure
- 2: All failed
- 3: Input/config error

## Workflow

```bash
# 1. Install (if needed)
which apkgo || curl -fsSL https://apkgo.com.cn/install.sh | sh

# 2. Discover required fields
apkgo stores

# 3. Generate config
apkgo init --store huawei,xiaomi

# 4. Fill in credentials (or use env vars)

# 5. Validate
apkgo upload -f app.apk --dry-run

# 6. Upload and parse result
apkgo upload -f app.apk --notes "v1.0.0" --timeout 15m
echo "Exit code: $?"
```

## Cloud SaaS (apkgo cloud)

Hosted at **`https://apkgo.baici.tech`** — credentials are encrypted server-side, multiple apps and team members per organization, async upload with optional webhook callbacks. Use this instead of the CLI when you don't want to ship credentials to your CI runners or when a team needs to share publishing config.

### When to prefer cloud over CLI

- CI/CD pipelines that should not hold app-store secrets
- Multi-developer teams (credentials live in one place, dashboard for management)
- Need an upload history / audit log without building one yourself
- Want webhook callbacks instead of polling

### Setup (one-time, in dashboard)

1. Sign in at `https://apkgo.baici.tech`, create or join an organization
2. Add store credentials in **凭证 (Credentials)** — server validates each credential against the store API before saving (apkgo doctor)
3. Bind credentials to apps in **应用 (Apps) → 配置发布凭证**
4. Generate an API key in **API 密钥**

### Authentication

Pass the API key in the `X-API-Key` header. Discover your `orgId` once:

```bash
curl -H "X-API-Key: apkgo_your_key" https://apkgo.baici.tech/api/v1/orgs
```

### Upload an APK

Simplest form — distributes to every store the app has bound:

```bash
curl -X POST \
  -H "X-API-Key: apkgo_your_key" \
  -F "apk=@app-release.apk" \
  https://apkgo.baici.tech/api/v1/orgs/{orgId}/uploads
```

Returns **`202 Accepted`** with a `job_id`. Upload runs asynchronously on the server.

Optional form fields:

- `target_stores` — JSON array of store names to limit distribution: `'["huawei","oppo"]'`. Stores without a credential binding for this app are skipped silently.
- `release_notes` — release notes text shown in the store-specific listing
- `app_id` — override the app to associate with (defaults to lookup-or-create by package name)

`package_name`, `version_code`, `version_name`, `app_name` are parsed from the APK automatically; the app is created if it doesn't exist yet.

```bash
curl -X POST \
  -H "X-API-Key: apkgo_your_key" \
  -F "apk=@app-release.apk" \
  -F 'target_stores=["huawei","xiaomi","oppo","vivo"]' \
  -F "release_notes=v1.2.0 bug fixes" \
  https://apkgo.baici.tech/api/v1/orgs/{orgId}/uploads
```

### Poll job status

```bash
curl -H "X-API-Key: apkgo_your_key" \
  https://apkgo.baici.tech/api/v1/orgs/{orgId}/uploads/{jobId}
```

Status flow: `pending` → `processing` → `completed` | `failed`. Per-store results appear in the `results` array as each store finishes; partial completion is visible mid-job.

### Webhook callbacks (alternative to polling)

Configure a Webhook URL + optional HMAC secret in the dashboard. Each completed job POSTs:

```json
{
  "event": "upload.completed",  // or "upload.failed"
  "job_id": "...",
  "package_name": "com.example.app",
  "version_name": "1.2.0",
  "status": "completed",
  "results": [
    {"store_name": "huawei", "success": true, "duration_ms": 3200},
    {"store_name": "xiaomi", "success": false, "error": "...", "duration_ms": 1500}
  ],
  "timestamp": "..."
}
```

Verify signature with `X-Webhook-Signature: sha256=<hex>` (HMAC-SHA256 of the raw body using the secret).

### Public API endpoints

| Method | Path | Purpose |
|---|---|---|
| GET  | `/api/v1/orgs` | List orgs the API key belongs to |
| POST | `/api/v1/orgs/{orgId}/uploads` | Upload APK + dispatch |
| GET  | `/api/v1/orgs/{orgId}/uploads/{jobId}` | Job status + per-store results |

Errors: `{"error": "..."}` with HTTP `401` (bad key), `403` (no permission), `429` (>100 req/min).

### Differences from CLI

- **No script store** — running arbitrary commands in a multi-tenant SaaS is RCE-shaped, so the script store is CLI-only.
- **No local `apkgo.yaml`** — credentials are managed in the dashboard, not config files.
- **Tencent uses `app_id_map`** — the dashboard provides a key/value editor for package → app_id mappings. The bind-time check rejects associations whose app's package isn't in the map.
- **Hooks → webhooks** — instead of `before`/`after` shell hooks, the cloud sends an HTTP POST when a job finishes.

### Cloud workflow

```bash
# 1. Discover orgId
ORG=$(curl -sH "X-API-Key: $APKGO_KEY" https://apkgo.baici.tech/api/v1/orgs | jq -r '.[0].id')

# 2. Upload
JOB=$(curl -sX POST -H "X-API-Key: $APKGO_KEY" \
  -F "apk=@app-release.apk" -F "release_notes=v1.0.0" \
  https://apkgo.baici.tech/api/v1/orgs/$ORG/uploads | jq -r '.id')

# 3. Poll until done (or rely on webhook)
while :; do
  STATUS=$(curl -sH "X-API-Key: $APKGO_KEY" \
    https://apkgo.baici.tech/api/v1/orgs/$ORG/uploads/$JOB | jq -r '.status')
  case "$STATUS" in
    completed) echo "✅ done"; break ;;
    failed)    echo "❌ failed"; exit 1 ;;
    *)         sleep 10 ;;
  esac
done
```
