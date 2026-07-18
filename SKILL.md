---
name: apkgo
description: Upload APK files to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Meizu, Tencent, Google Play, Samsung, Pgyer, fir.im) via the hosted apkgo cloud SaaS REST API. Designed for AI agents and CI/CD — API-key auth, direct-to-storage upload or server-side fetch from your CDN, async jobs with webhook callbacks, structured JSON, no credentials to manage locally.
---

<!-- Canonical source: apkgo-cloud repo, web/public/skill.md — served at
     https://apkgo.baici.tech/skill.md. The copy in the apkgo repo is a
     mirror; edit the canonical file and re-sync. -->

# apkgo

Upload APK files to multiple Android app stores through the hosted **apkgo cloud** service — no binary to install, no store credentials to hold locally.

Hosted at **`https://apkgo.baici.tech`** — credentials are encrypted server-side, multiple apps and team members per organization, async upload with optional webhook callbacks, and a dashboard-visible audit log. Every upload goes through an API key scoped to one organization, so access can be revoked or rate-limited centrally instead of trusting whatever environment holds a local copy of the credentials.

## When to use

Use this skill when the user wants to:
- Upload/publish/distribute an APK to Android app stores
- Release an Android app to Huawei, Xiaomi, OPPO, vivo, Honor, Meizu, Tencent, Google Play, or Samsung stores
- Upload an APK to Pgyer or fir.im for beta distribution
- Automate APK distribution in CI/CD pipelines without shipping app-store secrets to the runner

## Supported stores

huawei, xiaomi, oppo, vivo, honor, meizu, tencent, googleplay, samsung, pgyer, fir

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

All responses are wrapped in an envelope: `{"data": ...}` on success, `{"error": "..."}` on failure. Remember the `.data` prefix when extracting fields with jq.

## Upload an APK

APK bytes **never transit the apkgo cloud server**. There are two ways to get the binary in; both end with the same `POST /openapi/v1/uploads` JSON call.

### Option A — three-step direct upload (local APK file)

```bash
KEY="apkgo_your_key"
BASE="https://apkgo.baici.tech/openapi/v1"
APK="app-release.apk"

# 1. Get an upload ticket (server-chosen object_key + storage token)
T=$(curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d "{\"file_name\":\"$APK\"}" $BASE/uploads/tickets)
OBJ=$(echo "$T" | jq -r .data.object_key)

# 2. Upload the APK directly to object storage (Qiniu form upload)
curl -sf -F "token=$(echo "$T" | jq -r .data.token)" \
  -F "key=$OBJ" -F "file=@$APK" "$(echo "$T" | jq -r .data.upload_url)"

# 3. Create the distribution job around the uploaded object
curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d "{\"object_key\":\"$OBJ\",\"package_name\":\"com.example.app\"}" \
  $BASE/uploads
```

The ticket is valid for a bounded window, writes only to that exact `object_key`, and caps the file at 1GB.

### Option B — server-side fetch from a URL (APK already on your CDN)

If CI has already published the APK to a CDN or artifact host, skip steps 1–2: pass `file_url` instead of `object_key` and the storage backend pulls it server-side.

```bash
curl -s -X POST -H "X-API-Key: apkgo_your_key" -H "Content-Type: application/json" \
  -d '{
    "file_url": "https://cdn.example.com/builds/app-release-1.2.0.apk",
    "package_name": "com.example.app"
  }' \
  https://apkgo.baici.tech/openapi/v1/uploads
```

Rules: public http(s) URL only (signed query params are fine); 1GB cap; the URL is contacted **exactly once**, at create time — retries and store-side downloads use the stored copy, so the link may expire afterwards. The fetch is synchronous; for very large files (several hundred MB+) prefer Option A. `file_url` and `object_key` are mutually exclusive.

### Create-job fields (`POST /uploads`, JSON)

| Field | Required | Meaning |
|---|---|---|
| `object_key` / `file_url` | one of the two | the APK binary (Option A / Option B) |
| `package_name` / `app_id` | one of the two | app resolution; the app is auto-created by package name if new |
| `version_code`, `version_name`, `app_name` | no | display metadata; the worker re-parses the binary and its values win (a package mismatch fails the job) |
| `sha256` | no | integrity check performed by the worker after download |
| `release_notes` | no | release notes for the store listings |
| `release_time` | no | RFC3339 future instant for scheduled release (e.g. `2026-06-15T10:00:00+08:00`) |
| `target_stores` | no | array like `["huawei","oppo"]`; omitted = every store the app has bound. Stores without a credential binding are skipped |

Returns **`202 Accepted`**; the job id is `.data.id`. Upload runs asynchronously on the server.

The legacy multipart upload (`-F "apk=@..."` straight to `/uploads`) has been **removed** and returns 400.

## Poll job status

```bash
curl -sH "X-API-Key: apkgo_your_key" \
  https://apkgo.baici.tech/openapi/v1/uploads/{jobId}
```

Status flow: `pending` → `processing` → `completed` | `failed`. Per-store outcomes appear in `.data.results[]` (with `store_name`, `success`, `error`, `duration_ms`) as each store finishes — partial completion is visible mid-job. `.data.progress` carries live per-store byte progress while transferring.

## Webhook callbacks (alternative to polling)

Configure a Webhook URL + optional HMAC secret in the dashboard (API 密钥 page). Each completed job POSTs:

```json
{
  "event": "upload.completed",
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

`event` is `upload.completed` or `upload.failed`. Verify the signature with `X-Webhook-Signature: sha256=<hex>` (HMAC-SHA256 of the raw body using the secret).

## Public API endpoints

| Method | Path | Purpose |
|---|---|---|
| POST | `/openapi/v1/uploads/tickets` | Get a direct-to-storage upload ticket |
| POST | `/openapi/v1/uploads` | Create a distribution job (JSON: `object_key` or `file_url`) |
| GET  | `/openapi/v1/uploads` | List recent jobs (`limit`, `offset` query params) |
| GET  | `/openapi/v1/uploads/{jobId}` | Job status + per-store results |
| POST | `/openapi/v1/uploads/{jobId}/cancel` | Cancel a pending/processing job |
| POST | `/openapi/v1/uploads/{jobId}/retry` | Re-run a failed job |

Uploads are the only resource exposed to the Open API. App and credential management — including binding credentials to apps — is dashboard-only; uploads auto-create the app from the package name and resolve target stores from the bindings configured there.

Every endpoint above requires the API key to carry the **`upload`** permission (default for newly-created keys). The wildcard `"*"` permission grants all current and future endpoints.

Errors: `{"error": "..."}` with HTTP `400` (bad request / over-size / invalid `file_url`), `401` (missing/invalid/expired key), `403` (missing permission, or org over plan quota), `429` (>600 req/min per key), `502` (`file_url` fetch failed).

## Notes

- No script store — running arbitrary shell commands in a multi-tenant SaaS is out of scope; only the app-store integrations listed above are supported.
- No local config file — credentials and app/store bindings are managed entirely in the dashboard.
- Tencent needs `app_id_map` bound in the dashboard's key/value editor (package → app_id); binding is rejected if the app's package isn't in the map.
- Monthly upload quota is plan-based and org-level; an over-quota create returns 403 before any bytes move.

## Workflow

```bash
# 1. Create the job (Option B shown; use the three-step flow for a local file)
JOB=$(curl -s -X POST -H "X-API-Key: $APKGO_KEY" -H "Content-Type: application/json" \
  -d '{"file_url":"https://cdn.example.com/app-release.apk","package_name":"com.example.app","release_notes":"v1.0.0"}' \
  https://apkgo.baici.tech/openapi/v1/uploads | jq -r '.data.id')

# 2. Poll until done (or rely on webhook)
while :; do
  STATUS=$(curl -sH "X-API-Key: $APKGO_KEY" \
    https://apkgo.baici.tech/openapi/v1/uploads/$JOB | jq -r '.data.status')
  case "$STATUS" in
    completed) echo "✅ done"; break ;;
    failed)    echo "❌ failed"; exit 1 ;;
    *)         sleep 10 ;;
  esac
done
```
