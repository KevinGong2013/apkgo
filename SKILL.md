---
name: apkgo
description: Upload APK files to Chinese Android app stores (Huawei, Xiaomi, OPPO, vivo, Honor) and custom servers. Designed for AI agents with structured JSON output, semantic exit codes, and zero interaction.
---

# apkgo

Upload APK files to multiple Android app stores in one command.

## Install

Detect the platform and install accordingly:

```bash
# Linux (direct binary)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# macOS Apple Silicon (direct binary)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Darwin_arm64.tar.gz | tar xz -C /usr/local/bin apkgo

# macOS Intel (direct binary)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Darwin_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# Windows (PowerShell)
# Download from https://github.com/KevinGong2013/apkgo/releases/latest and add to PATH

# Go
go install github.com/KevinGong2013/apkgo@latest

# Docker
docker pull ghcr.io/kevingong2013/apkgo:latest
```

## When to use

Use this skill when the user wants to:
- Upload/publish/distribute an APK to Android app stores
- Release an Android app to Huawei, Xiaomi, OPPO, vivo, Honor, or Tencent stores
- Automate APK distribution in CI/CD pipelines
- Upload an APK to a custom server endpoint

## Supported stores

huawei, xiaomi, oppo, vivo, honor, tencent, custom

## Commands

```bash
apkgo stores                    # Discover config schema for each store (JSON)
apkgo init [--store names]      # Generate config file with comments
apkgo upload -f <apk> [flags]   # Upload APK to configured stores
apkgo serve [-p port]           # Start web GUI for uploading
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
stores:
  huawei:
    client_id: ""       # required - from AppGallery Connect > API key
    client_secret: ""   # required
  xiaomi:
    email: ""           # required - developer account email
    private_key: ""     # required - from dev.mi.com API management
  oppo:
    client_id: ""       # required - from open.oppomobile.com
    client_secret: ""   # required
  vivo:
    access_key: ""      # required - from dev.vivo.com.cn
    access_secret: ""   # required
  honor:
    client_id: ""       # required - from developer.honor.com
    client_secret: ""   # required
    app_id: ""          # required
  tencent:
    user_id: ""         # required - from app.open.qq.com
    access_secret: ""   # required - API access secret
    app_id: ""          # required
    package_name: ""    # required
  custom:
    url: ""             # required - upload endpoint
    method: "POST"
    field_name: "file"
    header_Authorization: "Bearer token"
```

Environment variable example:
```bash
APKGO_HUAWEI_CLIENT_ID=xxx APKGO_HUAWEI_CLIENT_SECRET=yyy apkgo upload -f app.apk --store huawei
```

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
which apkgo || curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

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
