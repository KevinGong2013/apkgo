---
name: apkgo-cli-usage
description: Reference for configuring apkgo.yaml (all 12 stores + script), writing before/after upload hooks (protocol + stdin JSON schemas), and the typical init/doctor/dry-run/upload/audit workflow for the apkgo CLI. Use when setting up store credentials, writing or debugging a hook script, checking review status, or walking an agent through uploading an APK / AAB / HarmonyOS .app end-to-end.
---

## Configuration

YAML file (`apkgo.yaml`, override with `-c`), environment variables (`APKGO_<STORE>_<KEY>`), or JSON piped in with `--creds-from stdin` / `--creds-from fd:N` (keeps secrets off disk; overrides `--config`). `apkgo stores` prints the live schema (`*` = required) — trust it over this list if they disagree.

```yaml
update_check: "7d"   # optional; how often to check for a new apkgo release, "0" disables

stores:
  huawei:
    service_account: ""        # recommended; raw JSON or base64
    service_account_file: ""   # alternative; path to JSON credential file
    client_id: ""              # legacy API key (deprecated by Huawei)
    client_secret: ""          # legacy API key
    app_id: ""                 # optional, auto-detected from package name
  harmony:                     # HarmonyOS 鸿蒙 .app packs (AppGallery Connect)
    service_account: ""        # recommended; raw JSON or base64 (same as huawei)
    service_account_file: ""
    client_id: ""              # deprecated
    client_secret: ""          # deprecated
    app_id: ""                 # optional, auto-detected from bundleName
    lang: ""                   # optional, release-notes language e.g. zh-CN
    chinese_mainland_flag: ""  # "1"/"0"; required by AGC for developers registered outside mainland China
  xiaomi:
    email: ""          # required, developer account email
    private_key: ""    # required, the value Xiaomi's SDK calls "password"
    cert: ""           # optional; raw PEM or base64 (defaults to built-in dev.api.public.cer)
    cert_file: ""      # optional; path to the certificate downloaded from dev.mi.com
  oppo:
    client_id: ""      # required
    client_secret: ""  # required
  vivo:
    access_key: ""     # required
    access_secret: ""  # required
    sandbox_access_key: ""     # required only for --sandbox
    sandbox_access_secret: ""  # required only for --sandbox
  honor:
    client_id: ""      # required
    client_secret: ""  # required
    app_id: ""         # optional, auto-detected from package name
    url_push_min_mb: "" # optional; with -f <URL>, files >= this size (default 100) are pulled by honor from the URL instead of uploaded
  meizu:
    client_id: ""      # required, 魅族开放平台客户端凭证
    client_secret: ""  # required
  tencent:
    user_id: ""        # required, from open.qq.com
    access_secret: ""  # required, 账户管理 → API 发布接口 → 申请开通
    app_id: ""         # single-app default; required if app_id_map is empty
    app_id_map: ""     # multi-app: JSON string like '{"com.foo":"111","com.bar":"222"}'; map wins over app_id when key matches
    package_name: ""   # optional; auto-detected from APK if omitted
  samsung:
    service_account_id: ""  # required, Seller Portal service account ID
    private_key: ""         # required, RSA private key (PEM) from Seller Portal
    content_id: ""          # required, the app's Galaxy Store content ID
  googleplay:
    json_key_file: ""  # required, path to service account JSON key
    package_name: ""   # required
    track: ""          # optional: production (default) / beta / alpha / internal
  pgyer:
    api_key: ""        # required
  fir:
    api_token: ""      # required, 控制台 → 账号 → API Token
  script:
    command: "./deploy.sh"  # required, shell command or script path

  # Multiple script instances via "script.<name>" prefix:
  script.cdn-upload:
    command: "./upload-cdn.sh"
  script.dingtalk:
    command: "./notify-dingtalk.sh"
```

Every store block also accepts `before` / `after` (per-store hooks, below) and `timeout` (per-store cap, any Go duration like `90s` / `20m`; unset inherits the global `-t`).

Env var example: `APKGO_HUAWEI_SERVICE_ACCOUNT=$(base64 -w0 huawei-sa.json) apkgo upload -f app.apk --store huawei`

Share a config across machines with `apkgo config export --out apkgo.enc` / `apkgo config import apkgo.enc` (encrypted; `APKGO_CONFIG_KEY` supplies the passphrase non-interactively).

### File types

`-f` takes a local path or an `http(s)` URL (add `--fetch-header "Authorization: Bearer …"` for private URLs; repeatable). `.apk` goes to every store; `.aab` is **googleplay-only**; a HarmonyOS `.app` pack is **harmony-only**. `--file64` adds a separate 64-bit APK for split-arch uploads.

### Listing (商店资料)

`--listing listing.yaml` updates the store listing — one-line intro, long
description, icon, screenshots — **together with the new version**, before it
is submitted for review. There is no listing-only update and the app name is
never changed. Only the store's default language is updated.

```yaml
brief: 一句话介绍
description_file: desc.md          # or description: |
icon: assets/icon-512.png
screenshots: [assets/s1.png, assets/s2.png, assets/s3.png, assets/s4.png]
stores:                            # per-store overrides (instances inherit their type's)
  oppo:   { brief: 十三字以内的简介 }
  huawei: { icon: assets/icon-216.png }
```

Omitted fields keep the store's current value; relative paths resolve against
the file. Specs differ a lot per store (oppo intro ≤13 chars, huawei icon
216×216, tencent 4–5 screenshots, …) — read them from
`apkgo stores` (`listing` per store) and put per-store variants under
`stores:`. Every target is validated before any upload (also in `--dry-run`);
all problems are listed at once, exit code 3. pgyer/fir don't support listings
(warning, upload continues); `script` gets it as `Listing` in its stdin JSON.

### Sandbox

For vivo sandbox uploads, run `apkgo upload -f app.apk --sandbox`. vivo uses
its separate sandbox credentials and endpoint; all other selected stores are
dry-runs. `--sandbox` skips all hooks, history, telemetry, and lifecycle event
callbacks, and it cannot be combined with `--dry-run`.

## Hooks

Shell commands executed before/after uploads. Receive context as JSON on stdin.

### Configuration

```yaml
hooks:
  before: "./scripts/before-all.sh"   # runs before any upload
  after: "./scripts/after-all.sh"     # runs after all uploads

stores:
  huawei:
    client_id: "..."
    before: "./scripts/before-huawei.sh"  # runs before this store
    after: "./scripts/after-huawei.sh"    # runs after this store
```

### Protocol

**Exit codes:**
- `0` — success (continue)
- non-zero — failure (`before` hooks abort the upload; `after` hooks log warning only)

**Environment variables** (set automatically):
- `APKGO_STORE` — store name (empty for global hooks)
- `APKGO_PACKAGE` — package name (e.g. `com.example.app`)
- `APKGO_VERSION` — version name (e.g. `1.2.0`)

Hooks run via `sh -c` (`cmd /C` on Windows) with the working directory unchanged.

**Errors:** stderr is captured as the error message.

### Stdin JSON schemas

**Global before** (`hooks.before`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "stores": ["huawei", "xiaomi"]
}
```

**Global after** (`hooks.after`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "results": [
    {"store": "huawei", "success": true, "category": "success", "duration_ms": 12300},
    {"store": "xiaomi", "success": false, "error": "auth failed", "category": "auth_failed", "duration_ms": 400}
  ]
}
```

**Per-store before** (`stores.<name>.before`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei"
}
```

The `apk` object also carries `"platform"`: `"android"` for APK/AAB, `"harmony"` for a HarmonyOS `.app`.

**Per-store after** (`stores.<name>.after`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei",
  "result": {"store": "huawei", "success": true, "duration_ms": 12300}
}
```

## Upload results

Each result: `store`, `success`, `error`, `duration_ms`, plus optional `category`, `external_id` (per-store submission id, e.g. honor's releaseId), `listing` (listing fields submitted with this version), `dry_run`, `sandbox`. `category` buckets the outcome: `success`, `already_done`, `auth_failed`, `network_retry`, `store_busy`, `policy_block`, `config_invalid`, `unknown` — retry only `network_retry` / `store_busy`; surface `auth_failed` / `config_invalid` / `policy_block` to the user.

Exit code: `0` all stores succeeded, `1` some failed, `2` all failed. `--progress-stream` emits NDJSON progress events on stdout for a parent process.

## Typical agent workflow

```bash
# 1. Check if apkgo is installed
which apkgo

# 2. Generate config for needed stores
apkgo init --store huawei,xiaomi -c apkgo.yaml

# 3. Discover required config fields (and what's configured)
apkgo stores
apkgo stores --configured

# 4. Probe credentials/permissions without uploading
apkgo doctor -p com.example.app

# 5. Dry-run to validate (add --listing listing.yaml to check the listing against every store's spec)
apkgo upload -f app.apk --dry-run

# Optional: real vivo sandbox upload; dry-run all other configured stores
apkgo upload -f app.apk --sandbox

# 6. Upload (optionally schedule a timed release with --release-time <RFC3339>;
#    supported by huawei, harmony, honor, xiaomi, oppo, vivo, samsung, tencent;
#    add --listing listing.yaml to update the store listing with this version)
apkgo upload -f app.apk --notes "v1.0.0 release" --timeout 15m

# 7. Parse JSON result from stdout, check exit code

# 8. Upload ends at "submitted (审核中)" — follow the review separately
apkgo audit -p com.example.app --watch --interval 1m -t 1h

# Local record of past uploads
apkgo history -n 10
```
