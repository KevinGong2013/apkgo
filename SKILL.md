---
name: apkgo
description: Upload APK files to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Tencent, Google Play, Samsung, Pgyer, fir.im) via the hosted apkgo cloud SaaS REST API. Designed for AI agents and CI/CD — API-key auth, async jobs with webhook callbacks, structured JSON, no credentials to manage locally.
---

# apkgo

Upload APK files to multiple Android app stores through the hosted **apkgo cloud** service — no binary to install, no store credentials to hold locally.

Hosted at **`https://apkgo.baici.tech`** — credentials are encrypted server-side, multiple apps and team members per organization, async upload with optional webhook callbacks, and a dashboard-visible audit log. Every upload goes through an API key scoped to one organization, so access can be revoked or rate-limited centrally instead of trusting whatever environment holds a local copy of the credentials.

## When to use

Use this skill when the user wants to:
- Upload/publish/distribute an APK to Android app stores
- Release an Android app to Huawei, Xiaomi, OPPO, vivo, Honor, Tencent, Google Play, or Samsung stores
- Upload an APK to Pgyer or fir.im for beta distribution
- Automate APK distribution in CI/CD pipelines without shipping app-store secrets to the runner

## Supported stores

huawei, xiaomi, oppo, vivo, honor, tencent, googleplay, samsung, pgyer, fir

## Setup (one-time, in dashboard)

1. Sign in at `https://apkgo.baici.tech`, create or join an organization
2. Add store credentials in **凭证 (Credentials)** — server validates each credential against the store API before saving
3. Bind credentials to apps in **应用 (Apps) → 配置发布凭证**
4. Generate an API key in **API 密钥**

## Authentication

The Open API base path is **`/openapi/v1`**. Pass the API key in the `X-API-Key` header — that's it. The organization is bound to the key itself, so `orgId` does **not** appear in any URL (and there is no org-discovery step).

```bash
curl -H "X-API-Key: apkgo_your_key" https://apkgo.baici.tech/openapi/v1/uploads
```

JWT bearer tokens are not accepted on `/openapi/v1` — that surface is dashboard-only at `/api/v1/orgs/{orgId}/...`.

## Upload an APK

Simplest form — distributes to every store the app has bound:

```bash
curl -X POST \
  -H "X-API-Key: apkgo_your_key" \
  -F "apk=@app-release.apk" \
  https://apkgo.baici.tech/openapi/v1/uploads
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
  https://apkgo.baici.tech/openapi/v1/uploads
```

## Poll job status

```bash
curl -H "X-API-Key: apkgo_your_key" \
  https://apkgo.baici.tech/openapi/v1/uploads/{jobId}
```

Status flow: `pending` → `processing` → `completed` | `failed`. Per-store results appear in the `results` array as each store finishes; partial completion is visible mid-job.

## Webhook callbacks (alternative to polling)

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

## Public API endpoints

| Method | Path | Purpose |
|---|---|---|
| POST | `/openapi/v1/uploads` | Upload APK + dispatch |
| GET  | `/openapi/v1/uploads` | List recent jobs (`limit`, `offset` query params) |
| GET  | `/openapi/v1/uploads/{jobId}` | Job status + per-store results |
| POST | `/openapi/v1/uploads/{jobId}/cancel` | Cancel a pending/processing job |
| POST | `/openapi/v1/uploads/{jobId}/retry` | Re-run a failed job |

Uploads are the only resource exposed to the Open API. App and credential management — including binding credentials to apps — is dashboard-only; uploads auto-create the app from the APK's package name and resolve target stores from the bindings configured there.

Every endpoint above requires the API key to carry the **`upload`** permission (default for newly-created keys). The wildcard `"*"` permission grants all current and future endpoints.

Errors: `{"error": "..."}` with HTTP `401` (missing/invalid/expired key), `403` (missing permission, or org over plan quota), `429` (>600 req/min per key).

## Notes

- No script store — running arbitrary shell commands in a multi-tenant SaaS is out of scope; only the app-store integrations listed above are supported.
- No local config file — credentials and app/store bindings are managed entirely in the dashboard.
- Tencent needs `app_id_map` bound in the dashboard's key/value editor (package → app_id); binding is rejected if the app's package isn't in the map.

## Workflow

```bash
# 1. Upload — orgId is bound to the API key, no discovery needed
JOB=$(curl -sX POST -H "X-API-Key: $APKGO_KEY" \
  -F "apk=@app-release.apk" -F "release_notes=v1.0.0" \
  https://apkgo.baici.tech/openapi/v1/uploads | jq -r '.id')

# 2. Poll until done (or rely on webhook)
while :; do
  STATUS=$(curl -sH "X-API-Key: $APKGO_KEY" \
    https://apkgo.baici.tech/openapi/v1/uploads/$JOB | jq -r '.status')
  case "$STATUS" in
    completed) echo "✅ done"; break ;;
    failed)    echo "❌ failed"; exit 1 ;;
    *)         sleep 10 ;;
  esac
done
```
