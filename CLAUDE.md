# apkgo

CLI tool for uploading APK files to Chinese Android app stores. All output is structured JSON on stdout; logs go to stderr.

## Install

```bash
go install github.com/KevinGong2013/apkgo@latest
# or download binary from https://github.com/KevinGong2013/apkgo/releases
```

## Commands

```bash
apkgo init [-s store1,store2] [-c config.yaml]   # Generate config file
apkgo upload -f <apk> [flags]                     # Upload APK to stores
apkgo stores                                      # List stores and config schema (JSON)
apkgo version                                     # Version info (JSON)
```

## Upload flags

```
-f, --file         APK file path (required)
    --file64       64-bit APK for split-arch uploads
-s, --store        Comma-separated store names (default: all configured)
-n, --notes        Release notes text
    --notes-file   Read release notes from file (overrides --notes)
    --dry-run      Validate without uploading
-t, --timeout      Global timeout (default: 10m)
-c, --config       Config file path (default: apkgo.yaml)
-o, --output       Output format: json or text (default: json)
```

## Supported stores

huawei, xiaomi, oppo, vivo, honor, custom

## Configuration

YAML file (`apkgo.yaml`) or environment variables (`APKGO_<STORE>_<KEY>`):

```yaml
stores:
  huawei:
    client_id: ""      # required
    client_secret: ""  # required
    app_id: ""         # optional, auto-detected from package name
  xiaomi:
    email: ""          # required
    private_key: ""    # required
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
  custom:
    url: ""            # required
    method: "POST"
    field_name: "file"
    header_Authorization: "Bearer token"
```

Env var example: `APKGO_HUAWEI_CLIENT_ID=xxx APKGO_HUAWEI_CLIENT_SECRET=yyy apkgo upload -f app.apk --store huawei`

## Output format

stdout is always parseable JSON:

```json
{
  "apk": {"package": "com.example", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "results": [
    {"store": "huawei", "success": true, "duration_ms": 12300},
    {"store": "xiaomi", "success": false, "error": "auth: invalid private key", "duration_ms": 400}
  ]
}
```

## Exit codes

- **0**: All uploads succeeded
- **1**: Some uploads failed (partial success)
- **2**: All uploads failed
- **3**: Input/config error

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

## Project structure

```
cmd/           CLI commands (cobra)
pkg/store/     Store interface + implementations (self-registering via init())
pkg/config/    YAML config + env var loading
pkg/apk/       APK metadata parser
pkg/uploader/  Concurrent upload orchestrator
```

Adding a new store: create `pkg/store/<name>/<name>.go`, implement `store.Store` interface, call `store.Register()` in `init()`. Zero changes to existing code.
