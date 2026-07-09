---
name: apkgo-cli-usage
description: Reference for configuring apkgo.yaml, writing before/after upload hooks (protocol + stdin JSON schemas), and the typical init/dry-run/upload workflow for the apkgo CLI. Use when setting up store credentials, writing or debugging a hook script, or walking an agent through uploading an APK end-to-end.
---

## Configuration

YAML file (`apkgo.yaml`) or environment variables (`APKGO_<STORE>_<KEY>`):

```yaml
stores:
  huawei:
    service_account: ""        # recommended; raw JSON or base64
    service_account_file: ""   # alternative; path to JSON credential file
    client_id: ""              # legacy API key (deprecated by Huawei)
    client_secret: ""          # legacy API key
    app_id: ""                 # optional, auto-detected from package name
  xiaomi:
    email: ""          # required, developer account email
    private_key: ""    # required, the value Xiaomi's SDK calls "password"
    cert: ""           # required (one of cert / cert_file); raw PEM or base64
    cert_file: ""      # path to public-key certificate downloaded from dev.mi.com
  oppo:
    client_id: ""      # required
    client_secret: ""  # required
  vivo:
    access_key: ""     # required
    access_secret: ""  # required
  honor:
    client_id: ""      # required
    client_secret: ""  # required
    app_id: ""         # required
  tencent:
    user_id: ""        # required, from open.qq.com
    access_secret: ""  # required, API access secret
    app_id: ""         # single-app default; required if app_id_map is empty
    app_id_map: ""     # multi-app: JSON string like '{"com.foo":"111","com.bar":"222"}'; map wins over app_id when key matches
    package_name: ""   # optional; auto-detected from APK if omitted
  script:
    command: "./deploy.sh"  # required, shell command or script path

  # Multiple script instances via "script.<name>" prefix:
  script.cdn-upload:
    command: "./upload-cdn.sh"
  script.dingtalk:
    command: "./notify-dingtalk.sh"
```

Env var example: `APKGO_HUAWEI_SERVICE_ACCOUNT=$(base64 -w0 huawei-sa.json) apkgo upload -f app.apk --store huawei`

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

**Errors:** stderr is captured as the error message.

### Stdin JSON schemas

**Global before** (`hooks.before`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "stores": ["huawei", "xiaomi"]
}
```

**Global after** (`hooks.after`):
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

**Per-store before** (`stores.<name>.before`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei"
}
```

**Per-store after** (`stores.<name>.after`):
```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei",
  "result": {"store": "huawei", "success": true, "duration_ms": 12300}
}
```

## Typical agent workflow

```bash
# 1. Check if apkgo is installed
which apkgo

# 2. Generate config for needed stores
apkgo init --store huawei,xiaomi -c apkgo.yaml

# 3. Discover required config fields
apkgo stores

# 4. Dry-run to validate
apkgo upload -f app.apk --dry-run

# 5. Upload
apkgo upload -f app.apk --notes "v1.0.0 release" --timeout 15m

# 6. Parse JSON result from stdout, check exit code
```
