---
name: apkgo
version: "2026.09.24"
description: The apkgo-cloud CLI and distribution skill. Install the apkgo-cloud CLI (one-line installer, browser login, no local store secrets), then `preview` an APK to get a public 内测 download link (share it, anyone can install), `release` to distribute to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Meizu, Tencent, Google Play, Samsung, Pgyer, fir.im), the Apple App Store (.ipa) and HarmonyOS 鸿蒙 AppGallery (.app). Use this when the user wants to share a build or ship a release. To prepare store listing materials and create the app entries in each store before the first release, use the `apkgo-app-listing` skill. A REST Open API (X-API-Key, curl) is available as a fallback for CI/CD.
---

<!-- Canonical source: apkgo-cloud repo, web/public/skill.md — served at
     https://apkgo.baici.tech/skill.md. The copy in the apkgo repo is a
     mirror; edit the canonical file and re-sync. -->

# apkgo

Ship an Android app through the hosted **apkgo cloud** service. Two ways in:

- **CLI (`apkgo-cloud`)** — recommended for agents and interactive use. Install once, log in through the browser, then run one command to get a shareable download link or distribute to stores. No store credentials held locally.
- **Open API (`X-API-Key`, curl)** — for CI/CD runners where you don't want to install a binary. See [Open API — CI/CD fallback](#open-api--cicd-fallback) at the end.

Hosted at **`https://apkgo.baici.tech`**. Store credentials are encrypted server-side; multiple apps and team members per organization; async distribution with optional webhook callbacks; dashboard-visible audit log.

## When to use

Use this skill when the user wants to:

- Get a **public 内测 (beta) download link** for an APK they can send to testers → `preview`
- **Distribute/publish/release** an APK to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Meizu, Tencent, Google Play, Samsung, Pgyer, fir.im), an .ipa to the App Store, or a HarmonyOS 鸿蒙 `.app` pack to AppGallery → `release`
- Prepare listing materials or create the app entry in each store before its first release (「准备上架资料」「创建 App 提审」) → use the `apkgo-app-listing` skill instead
- Automate distribution in **CI/CD** without shipping store secrets → [Open API](#open-api--cicd-fallback)

## Install & log in (CLI)

If `apkgo-cloud` isn't installed yet, install it with the one-line installer (prebuilt binary, no Go toolchain needed), then log in through the browser:

```bash
curl -fsSL https://apkgo.baici.tech/install.sh | sh   # installs the `apkgo-cloud` binary onto PATH
apkgo-cloud login      # opens the browser; user clicks 同意授权
apkgo-cloud whoami     # confirm the connected organization
```

Windows (PowerShell): `iwr https://apkgo.baici.tech/install.ps1 -UseBasicParsing | iex` — installs to `%LOCALAPPDATA%\apkgo-cloud` and adds it to your user PATH (open a new terminal afterwards). Full agent setup doc: **https://apkgo.baici.tech/doc/cli-setup.md**.

Keep the skills local and fresh: `apkgo-cloud skill install` drops the apkgo skills into **your** skills directory — pass `--dir` to choose it (default `.claude/skills`; for Trae IDE pass `--dir .trae/skills`, or `~/.trae/skills` for user-level; otherwise your own agent's skills dir). `apkgo-cloud skill update --dir <dir>` refreshes them when a new version ships.

**Login is browser-only.** The CLI starts a local callback, opens the approve page, and receives the credential automatically — it's written to `~/.apkgo-cloud/config.json` (0600). **Never ask the user for an API key or password.** If the browser doesn't open, hand the user the URL the CLI printed.

## Get a 内测 download link — `preview`

The fastest thing you can do for a freshly-built app. No store credentials required — works on day one.

```bash
apkgo-cloud preview ./app-release.apk --notes "首个内测版本"
apkgo-cloud preview ./app-release.apk --password 8888   # 加下载密码
```

The CLI parses the APK locally (package, version), uploads it directly to object storage, and prints a public link like `https://apkgo.baici.tech/d/AbC123...`. Send it to anyone — they get a download page and install the APK. Re-running `preview` for the **same package** refreshes the **same** link (one link per app), so testers keep a stable URL across builds.

`--password` gates the download page — visitors must enter it before downloading (share the password alongside the link). Omitting `--password` on a later `preview` leaves an existing link's password unchanged. Expiry and download-count limits are also supported on the link, settable in the dashboard.

## Distribute to app stores — `release`

Push the APK to the app's bound stores and wait for each result.

```bash
apkgo-cloud release ./app-release.apk                     # all bound stores
apkgo-cloud release ./app-release.apk --stores huawei,oppo  # only these
apkgo-cloud release ./app-release.apk --no-wait            # create job, don't wait
```

- Prerequisite: the org has at least one store account (商店账号) added in the dashboard. Any store with exactly one account publishes automatically — there is no separate "bind" step. Only when a store has several accounts must the user pick one under 应用 → 发布商店 (the app page); a store can also be switched off there. If no store resolves, `release` fails with a message saying which account is missing. Use `preview` until stores are set up.
- Without `--no-wait`, the CLI polls until every store finishes and prints a `✓/✗` per-store summary; it exits non-zero if the job fails (usable in CI).
- The worker re-parses the binary after download; its metadata wins. A package mismatch fails the job before any store upload.

## Before the first release — hand off

`release` only works for apps that already exist in each store. When the app has never been listed — the user says 「帮我上架」 for a new app, or needs icons, screenshots, copy, a privacy policy or to submit the store entry for review — switch to the **`apkgo-app-listing`** skill (https://apkgo.baici.tech/skill-app-listing.md):

| 用户说 | 用这个 skill | 它负责 |
|---|---|---|
| 「准备上架资料」「写应用描述」「创建 App 提审」 | `apkgo-app-listing` | 图标/截图/文案/隐私政策/资质/测试账号，创建条目提审 |
| 「上传 APK」「生成内测链接」「发布到商店」 | `apkgo`（本 skill） | CLI 本体：preview / release |

Come back here once the store entries are approved — every later version ships with `release`.

**Steps that must be the user's own action — stop and wait, never do these for them or try to bypass:** 人脸核身 (face verification), 短信/图形验证码, 对公打款 (corporate bank verification), payment. When you hit one, tell the user exactly what to do and continue only after they confirm.

## Supported stores

huawei, xiaomi, oppo, vivo, honor, meizu, tencent, googleplay, samsung, pgyer, fir, appstore (iOS .ipa), harmony (HarmonyOS 鸿蒙 .app)

HarmonyOS notes: `harmony` reuses the huawei AGC Service Account (add a「鸿蒙」store account with the same JSON); it only accepts the signed **`.app` App Pack** from DevEco Studio (Build → Build APP(s)), not a bare `.hap`. The app must exist in AppGallery Connect as a HarmonyOS app (same bundleName) with category / privacy policy / content rating filled in; the platform picks the HarmonyOS app automatically from the file extension. `preview` (内测分发) is Android APK only.

## CLI command reference

| Command | Purpose |
|---|---|
| `apkgo-cloud login` | Browser OAuth login; writes `~/.apkgo-cloud/config.json` |
| `apkgo-cloud whoami` | Show the connected organization |
| `apkgo-cloud logout` | Remove local credentials |
| `apkgo-cloud preview <apk> [--notes ...] [--password ...]` | Public 内测 download link (`/d/<slug>`), no store creds; optional download password |
| `apkgo-cloud release <apk\|ipa\|app> [--stores a,b] [--notes ...] [--no-wait] [--app name\|id]` | Distribute to bound app stores; `.ipa` matches the iOS app by bundle id, `--app` picks the app when that fails |
| `apkgo-cloud skill <list\|install\|update\|check> [--dir <dir>]` | Install/refresh these skills (default dir `.claude/skills/`; `check` exits 1 if outdated) |
| `apkgo-cloud version` | Print version |

`APKGO_CLOUD_BASE` overrides the service URL (default `https://apkgo.baici.tech`).

---

## Open API — CI/CD fallback

For runners where installing the CLI isn't practical. Everything the CLI does maps to the same REST surface at base path **`/openapi/v1`**, authenticated **only** by the `X-API-Key` header — the organization is bound to the key itself, so `orgId` never appears in a URL. Mint a key in the dashboard's **API 密钥** page.

```bash
curl -H "X-API-Key: apkgo_your_key" https://apkgo.baici.tech/openapi/v1/uploads
```

JWT bearer tokens are not accepted on `/openapi/v1`. All responses are enveloped: `{"data": ...}` on success, `{"error": "..."}` on failure — remember the `.data` prefix in jq.

Dashboard-minted keys track the subscription: API keys are a 专业版及以上 feature, so once the subscription lapses they return `403` with 「当前套餐（免费版）不支持 API 密钥」. Renewing restores the same key — no need to mint a new one. Keys issued by `apkgo-cloud login` for the CLI are exempt and work on every plan.

### Upload the binary (shared by link + distribute)

APK bytes **never transit the apkgo cloud server**. Get the binary into storage one of two ways, both ending at a JSON create call.

**Option A — three-step direct upload (local APK):**

```bash
KEY="apkgo_your_key"; BASE="https://apkgo.baici.tech/openapi/v1"; APK="app-release.apk"

T=$(curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d "{\"file_name\":\"$APK\"}" $BASE/uploads/tickets)
OBJ=$(echo "$T" | jq -r .data.object_key)

curl -sf -F "token=$(echo "$T" | jq -r .data.token)" \
  -F "key=$OBJ" -F "file=@$APK" "$(echo "$T" | jq -r .data.upload_url)"
```

**Option B — server-side fetch from a URL (APK already on your CDN):** skip the ticket; pass `file_url` in the create call instead of `object_key`. Public http(s) only, 1GB cap, contacted exactly once at create time; `file_url` and `object_key` are mutually exclusive.

### Create a 内测 download link (`preview`)

```bash
curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d "{\"object_key\":\"$OBJ\",\"package_name\":\"com.example.app\",\"version_name\":\"1.0.0\"}" \
  $BASE/dist
# → {"data":{"slug":"...","url":"https://apkgo.baici.tech/d/...","package_name":"..."}}
```

No store credentials needed. One link per app; a later create for the same package refreshes it.

### Create a store-distribution job (`release`)

```bash
curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d "{\"object_key\":\"$OBJ\",\"package_name\":\"com.example.app\"}" \
  $BASE/uploads
# → 202 Accepted, job id at .data.id
```

Create-job fields (`POST /uploads`, JSON):

| Field | Required | Meaning |
|---|---|---|
| `object_key` / `file_url` | one of the two | the APK binary (Option A / Option B) |
| `package_name` / `app_id` | one of the two | app resolution; auto-created by package name if new |
| `version_code`, `version_name`, `app_name` | no | display metadata; the worker re-parses the binary and its values win |
| `sha256` | no | integrity check performed by the worker after download |
| `release_notes` | no | release notes for the store listings |
| `release_time` | no | RFC3339 future instant for scheduled release (e.g. `2026-06-15T10:00:00+08:00`) |
| `target_stores` | no | array like `["huawei","oppo"]`; omitted = every store the app has bound |

The legacy multipart upload (`-F "apk=@..."` straight to `/uploads`) has been **removed** and returns 400.

### Poll job status

```bash
curl -sH "X-API-Key: $KEY" $BASE/uploads/{jobId}
```

Status flow: `pending` → `processing` → `completed` | `failed`. Per-store outcomes in `.data.results[]` (`store_name`, `success`, `error`, `duration_ms`) as each store finishes; `.data.progress` carries live per-store byte progress.

### Webhook callbacks (alternative to polling)

**Pro plan (专业版) and above.** Configure one org-level Webhook URL + optional HMAC secret at the bottom of the dashboard's **API 密钥** page. Saving first POSTs a signed `webhook.test` event to the URL — it's saved **only if the endpoint answers HTTP 200** (any other status or a connection error is shown inline). The URL must be `http(s)` and publicly reachable; loopback / private / link-local addresses (127.0.0.1, 10.x, 192.168.x, …) are rejected.

Events (all `POST`, `Content-Type: application/json`):

| `event` | When | Body |
|---|---|---|
| `upload.completed` | A job finished and every store succeeded | `job_id`, `app_name`, `package_name`, `version_name`, `version_code`, `status`, `results[]` (`store_name`, `success`, `error`, `duration_ms`), `timestamp` |
| `upload.failed` | A job finished with at least one store failed | same as above |
| `review.changed` | A store's review verdict came in — one request **per store** | `app_id`, `app_name`, `package_name`, `store`, `version_name`, `version_code`, `review_state` (`approved` \| `rejected` \| `withdrawn`), `previous_state` (usually `reviewing`), `review_detail` (store's reason, when rejected), `timestamp` |
| `webhook.test` | Sent once when the URL is saved | `org_name`, `message`, `timestamp` — just answer 200 |

```json
{"event":"review.changed","app_id":"7c9e…","app_name":"我的应用","package_name":"com.example.app","store":"huawei","version_name":"1.2.0","version_code":12,"review_state":"rejected","previous_state":"reviewing","review_detail":"应用存在隐私政策问题","timestamp":"2026-04-11T09:30:00+08:00"}
```

Branch on `event` and ignore unknown values (new events may be added). If a secret is set, verify `X-Webhook-Signature: sha256=<hex>` = HMAC-SHA256 of the **raw** body with the secret (no header when the secret is empty). Delivery is fire-and-forget: 10 s timeout, any 2xx counts as success, **no retries** — keep polling `GET /openapi/v1/uploads/{jobId}` as the source of truth if you can't afford a missed callback.

### Endpoint reference

| Method | Path | Purpose |
|---|---|---|
| POST | `/openapi/v1/uploads/tickets` | Get a direct-to-storage upload ticket |
| POST | `/openapi/v1/dist` | Create/refresh a public 内测 download link (`object_key` + `package_name`) |
| POST | `/openapi/v1/uploads` | Create a store-distribution job (`object_key` or `file_url`) |
| GET  | `/openapi/v1/uploads` | List recent jobs (`limit`, `offset`) |
| GET  | `/openapi/v1/uploads/{jobId}` | Job status + per-store results |
| POST | `/openapi/v1/uploads/{jobId}/cancel` | Cancel a pending/processing job |
| POST | `/openapi/v1/uploads/{jobId}/retry` | Re-run a failed job |

Every endpoint requires the key to carry the **`upload`** permission (default for new keys); `"*"` grants everything. App and store-account management (including per-app store switches) stays dashboard-only.

Errors: `{"error": "..."}` with HTTP `400` (bad request / over-size / invalid `file_url`), `401` (missing/invalid/expired key), `403` (missing permission, or org over plan quota), `429` (>600 req/min per key), `502` (`file_url` fetch failed).

## Notes

- No script store — running arbitrary shell commands in a multi-tenant SaaS is out of scope; only the app-store integrations listed above are supported.
- Store accounts and per-app store settings are managed entirely in the dashboard; the CLI holds only a browser-minted API key.
- Tencent needs `app_id_map` bound in the dashboard's key/value editor (package → app_id).
- Monthly store-distribution quota is plan-based and org-level; an over-quota `release` returns 403 before any bytes move. 内测 links are not charged against it.

## Agent 执行须知

- **一步一确认**：完成一步、用户确认后再进下一步，不要一次性倾倒全部信息。
- **以文档为准**：需要细节时用 WebFetch 打开对应文档 URL 读取，不要凭记忆补规则；拿不准就直说，并让用户以商店后台的实时提示为准。
- **URL 原样输出**：展示给用户的链接不做任何改写（不编码/解码、不加标点、不重新拼接）。
- **本人环节停下来**：人脸核身、验证码、对公打款、支付、身份材料（身份证/营业执照/公章）必须用户本人完成——讲清要做什么、等确认后再继续，不要尝试代替或绕过。
