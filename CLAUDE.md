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
apkgo doctor [-s stores] [-f apk | -p package]    # Diagnose store credentials/permissions
apkgo audit [-f apk | -p package] [-s stores] [--watch]  # Query review (审核) status
apkgo stores                                      # List stores and config schema (JSON)
apkgo version                                     # Version info (JSON)
```

### Review status (`apkgo audit`)

Upload finishes at **submitted (审核中)** — it does not block waiting for the
review outcome (tencent's old in-upload audit poll was removed). Poll review
progress separately with `apkgo audit -p <package>` (or `-f <apk>`), which runs
on its own context like `doctor`. `--watch [--interval 30s]` loops until every
store reaches a terminal state (approved / rejected / withdrawn) or the global
`-t` timeout. Each store's status is normalised to a unified `state`
(reviewing / approved / rejected / withdrawn / unknown) with the raw label in
`detail`. Supported: **tencent, huawei, honor, vivo, oppo, samsung, meizu**
(stores with a review-status API; others report "audit not supported").

## Upload flags

```
-f, --file         APK or AAB file path (required; .aab is googleplay-only)
    --file64       64-bit APK for split-arch uploads
-s, --store        Comma-separated store names (default: all configured)
-n, --notes        Release notes text
    --notes-file   Read release notes from file (overrides --notes)
    --release-time Schedule a timed release (定时发布) at an RFC3339 time, e.g. 2026-06-20T10:00:00+08:00
    --dry-run      Validate without uploading
-t, --timeout      Global timeout (default: 10m)
-c, --config       Config file path (default: apkgo.yaml)
-o, --output       Output format: json or text (default: json)
```

### Scheduled release (`--release-time`)

Schedules a timed release instead of going live immediately after review.
Value is RFC3339 **with a timezone offset** and must be in the future.
Supported stores: **huawei, honor, xiaomi, oppo, vivo, samsung, tencent**
(see `supports_scheduled_release` in `apkgo stores`). Stores that can't
schedule (googleplay, pgyer, fir, script) log a warning and release
immediately. Each store maps the instant to its own field/format
internally — epoch-based stores use the absolute instant; oppo/vivo/samsung
render it in Beijing time (UTC+8).

### Download mode (URL pass-through)

When `-f` (or `--file64`) is a **public** http(s) URL, stores that support
it pull the APK straight from your OSS instead of apkgo re-uploading the
bytes — faster, especially for large APKs or cloud runs. Supported stores:
**huawei, honor, vivo** (see `supports_url_push` in `apkgo stores`); the
others always upload. apkgo still fetches the APK once locally for metadata.

- The URL must be reachable **without auth** (the store GETs it directly).
  Passing `--fetch-header` (auth) makes apkgo upload instead of passing the
  URL through.
- These flows are **asynchronous**: the store downloads in the background
  and apkgo polls until it finishes. Each store has its own download
  interface (huawei `app-package-file/by-url`, honor `upload-by-url`, vivo
  `app.update.app` + `app.query.task.status`).
- **honor** throttles its status poll to ~once/3min, so it only URL-pushes
  when the APK is at least `url_push_min_mb` MB (default 100); smaller APKs
  upload directly. huawei and vivo URL-push whenever the source is a URL.

## Supported stores

huawei, xiaomi, oppo, vivo, honor, meizu, tencent, googleplay, samsung, pgyer, fir, script

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

## Project structure

```
cmd/           CLI commands (cobra)
pkg/store/     Store interface + implementations (self-registering via init())
pkg/config/    YAML config + env var loading
pkg/apk/       APK metadata parser
pkg/uploader/  Concurrent upload orchestrator
```

See `pkg/store/CLAUDE.md` for adding a new store. See the `apkgo-cli-usage` skill for config schema, hook protocol, and the typical upload workflow.
