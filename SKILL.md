---
name: apkgo
version: "2026.07.27"
description: The apkgo-cloud CLI and distribution skill. Install the apkgo-cloud CLI (one-line installer, browser login, no local store secrets), then `preview` an APK to get a public 内测 download link (share it, anyone can install), `release` to distribute to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Meizu, Tencent, Google Play, Samsung, Pgyer, fir.im), or `submit-copyright` to hand off 软著 (software copyright) materials for filing. Use this when the user already knows what they want to do and just needs the tool. If a developer publishing for the first time says 「开始上架」 or doesn't know where to start, use the `apkgo-start-publishing` skill instead — it assesses their situation and routes them. A REST Open API (X-API-Key, curl) is available as a fallback for CI/CD.
---

<!-- Canonical source: apkgo-cloud repo, web/public/skill.md — served at
     https://apkgo.baici.tech/skill.md. The copy in the apkgo repo is a
     mirror; edit the canonical file and re-sync. -->

# apkgo

Ship an Android app through the hosted **apkgo cloud** service. Two ways in:

- **CLI (`apkgo-cloud`)** — recommended for agents and interactive use. Install once, log in through the browser, then run one command to get a shareable download link, distribute to stores, or file a software-copyright application. No store credentials held locally.
- **Open API (`X-API-Key`, curl)** — for CI/CD runners where you don't want to install a binary. See [Open API — CI/CD fallback](#open-api--cicd-fallback) at the end.

Hosted at **`https://apkgo.baici.tech`**. Store credentials are encrypted server-side; multiple apps and team members per organization; async distribution with optional webhook callbacks; dashboard-visible audit log.

## When to use

Use this skill when the user wants to:

- Get a **public 内测 (beta) download link** for an APK they can send to testers → `preview`
- **Distribute/publish/release** an APK to Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor, Meizu, Tencent, Google Play, Samsung, Pgyer, fir.im) → `release`
- Just finished building an app and says **「开始上架」 / "help me publish my first app"** → use the `apkgo-start-publishing` skill instead — it assesses the user's situation (账号/软著/备案) and routes them
- File a **软著 (software copyright)** application → `submit-copyright`
- Automate distribution in **CI/CD** without shipping store secrets → [Open API](#open-api--cicd-fallback)

## Install & log in (CLI)

If `apkgo-cloud` isn't installed yet, install it with the one-line installer (prebuilt binary, no Go toolchain needed), then log in through the browser:

```bash
curl -fsSL https://apkgo.baici.tech/install.sh | sh   # installs the `apkgo-cloud` binary onto PATH
apkgo-cloud login      # opens the browser; user clicks 同意授权
apkgo-cloud whoami     # confirm the connected organization
```

Windows (PowerShell): download `https://apkgo.baici.tech/dl/apkgo-cloud-windows-amd64.exe`, rename it to `apkgo-cloud.exe`, and put it on your PATH. Full agent setup doc: **https://apkgo.baici.tech/doc/cli-setup.md**.

Keep the skills local and fresh: `apkgo-cloud skill install` drops the apkgo skills into **your** skills directory — pass `--dir` to choose it (default `.trae/skills` for Trae IDE; use `~/.trae/skills` for user-level, or your own agent's skills dir). `apkgo-cloud skill update --dir <dir>` refreshes them when a new version ships.

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

## File a 软著 (software copyright) application — `submit-copyright`

Most Android stores require a software-copyright certificate before listing. If the org bought the 软著代提交 (copyright filing) add-on, the CLI can package the materials and hand them to an operator who files with the copyright center.

```bash
apkgo-cloud auth check                    # exit 0 = entitled, exit 1 = not purchased
apkgo-cloud submit-copyright ./软著材料目录   # zips the folder and submits
```

`auth check`'s exit code gates scripts. `submit-copyright` also refuses to run without the entitlement. Direct users who haven't purchased it to the 增值服务 / 首次上架服务包 in the dashboard.

**Don't have the materials yet?** Generating a 软著 registration package (source-code document + manual + form fields) from the codebase is its own skill: **https://apkgo.baici.tech/skill-copyright.md**. It produces the folder that `submit-copyright` then submits.

## First-time publishing (「开始上架」) — hand off

**This skill does not drive first-time publishing.** When a developer says 「开始上架」/「帮我上架」/「我要发布应用」, or doesn't know where to start, switch to the **`apkgo-start-publishing`** skill (https://apkgo.baici.tech/skill-start-publishing.md). It assesses what they still need (developer accounts, 软著, App 备案) and routes to the right sub-skill:

| 用户说 | 用这个 skill | 它负责 |
|---|---|---|
| 「开始上架」「帮我上架」「不知道从哪开始」 | `apkgo-start-publishing` | 总入口：评估现状（账号/软著/备案），分流到各环节 |
| 「注册开发者账号」「注册华为/小米账号」 | `apkgo-onboarding` | 各商店开发者账号注册（8 家分店指南） |
| 「生成软著材料」「写软著源代码文档」 | `apkgo-copyright` | 从代码库生成软著申请材料 |
| 「提交软著」「代我申请软著」 | `apkgo-copyright-submit` | 编排完整提交：生成 → 索要资质 → 校验 → 上传 |
| 「准备上架资料」「写应用描述」「创建 App 提审」 | `apkgo-app-listing` | 图标/截图/文案/隐私政策/资质/测试账号，创建条目提审 |
| 「上传 APK」「生成内测链接」「发布到商店」 | `apkgo`（本 skill） | CLI 本体：preview / release / submit-copyright |

Come back here once they reach the actual distribution step — that's what `release` is for.

**Steps that must be the user's own action — stop and wait, never do these for them or try to bypass:** 人脸核身 (face verification), 短信/图形验证码, 对公打款 (corporate bank verification), payment. When you hit one, tell the user exactly what to do and continue only after they confirm.

## Supported stores

huawei, xiaomi, oppo, vivo, honor, meizu, tencent, googleplay, samsung, pgyer, fir

## CLI command reference

| Command | Purpose |
|---|---|
| `apkgo-cloud login` | Browser OAuth login; writes `~/.apkgo-cloud/config.json` |
| `apkgo-cloud whoami` | Show the connected organization |
| `apkgo-cloud logout` | Remove local credentials |
| `apkgo-cloud preview <apk> [--notes ...] [--password ...]` | Public 内测 download link (`/d/<slug>`), no store creds; optional download password |
| `apkgo-cloud release <apk> [--stores a,b] [--notes ...] [--no-wait]` | Distribute to bound app stores |
| `apkgo-cloud auth check` | Exit 0/1 — is the org entitled to 软著代提交 |
| `apkgo-cloud submit-copyright <dir\|zip>` | Package & submit 软著 materials |
| `apkgo-cloud skill <list\|install\|update\|check>` | Install/refresh these skills into `.trae/skills/` (`check` exits 1 if outdated) |
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

Configure a Webhook URL + optional HMAC secret in the dashboard (API 密钥 page). Each finished job POSTs `{"event":"upload.completed"|"upload.failed","job_id":...,"status":...,"results":[...]}`. Verify `X-Webhook-Signature: sha256=<hex>` (HMAC-SHA256 of the raw body).

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
| GET  | `/openapi/v1/copyright/eligibility` | Is the org entitled to 软著代提交 |
| POST | `/openapi/v1/copyright/tickets` | Upload ticket for a 软著 materials zip |
| POST | `/openapi/v1/copyright` | Record a 软著 submission |

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
