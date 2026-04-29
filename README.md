# apkgo

![GitHub release](https://img.shields.io/github/v/release/KevinGong2013/apkgo) ![build](https://img.shields.io/github/actions/workflow/status/kevingong2013/apkgo/release.yml?style=flat-square) ![go](https://img.shields.io/github/go-mod/go-version/kevingong2013/apkgo?style=flat-square) ![license](https://img.shields.io/github/license/kevingong2013/apkgo?style=flat-square)

一行命令，将 APK 发布到所有主流安卓应用商店。为 CI/CD 和 AI Agent 设计。

## 支持的商店

| 商店 | 认证方式 |
|------|---------|
| 华为 AppGallery | OAuth2 |
| 小米开放平台 | RSA 签名 |
| OPPO 开放平台 | HMAC-SHA256 |
| vivo 开放平台 | HMAC-SHA256 |
| 荣耀开发者平台 | OAuth2 |
| 腾讯应用宝 | HMAC-SHA256 |
| Google Play | Service Account JWT |
| Samsung Galaxy Store | Service Account JWT |
| 蒲公英 (Pgyer) | API Key |
| fir.im | API Token |
| 脚本 (Script) | 自定义脚本 |

## 为什么不直接用「应用生态联盟」一键多发？

[中国安卓应用生态联盟](https://www.appchinaalliance.org)（华为/小米/OPPO/vivo 等共同发起）提供过"一次上传同步到多家商店"的能力。听起来很方便，但一旦你严肃发版就会撞到下面这些限制 —— 这也是 apkgo 选择直接调用每家商店原生 API 的原因：

| 痛点 | 联盟同步 | apkgo 直发 |
|------|---------|------------|
| **同步时间** | 不可控，少则数小时多则数天，等一家慢的拖累整体 | 各家并发，整体时间 ≈ 最慢那一家的 API 时间 |
| **过程可观测** | 看不到每家具体进度，出问题靠"等" | 每家结构化 JSON 状态码 + 错误消息直出 |
| **差异化包** | 强制同包，不支持给不同商店投不同 APK（渠道号、签名、白名单功能） | 每家可单独传，`--file` / `--file64` / 渠道包灵活组合 |
| **撤回 / 下架** | 联盟统一控制，无法只撤回某一家 | 每家用自己后台/脚本独立处理，apkgo 只管发不管控 |
| **凭证归属** | 凭证集中在联盟方 | 凭证留在自家 CI，每家商店只看到自己的 token |
| **可审计** | 上传历史散落在联盟系统 | 你的 CI 日志就是发布日志 |

简单总结：联盟方案是为"图省事"设计的，apkgo 是为"自己说了算"设计的。如果你只是偶尔丢个内部测试包，联盟够用；如果你要做正经发布、自动化、多渠道、版本回滚，原生 API + apkgo 才是可行路径。

## 安装

```bash
# AI Agent Skill (支持 Claude Code、Cursor、Windsurf 等 40+ agent)
npx skills add KevinGong2013/apkgo

# macOS (Apple Silicon)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Darwin_arm64.tar.gz | tar xz -C /usr/local/bin apkgo

# macOS (Intel)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Darwin_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# Linux (x86_64)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# Linux (arm64)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_arm64.tar.gz | tar xz -C /usr/local/bin apkgo

# Windows (PowerShell)
# 从 https://github.com/KevinGong2013/apkgo/releases/latest 下载 apkgo_Windows_x86_64.zip
# 解压后将 apkgo.exe 添加到 PATH

# Go
go install github.com/KevinGong2013/apkgo@latest

# Docker
docker pull ghcr.io/kevingong2013/apkgo:latest
```

## 快速开始

```bash
# 1. 生成配置文件
apkgo init

# 2. 填写商店凭证
vim apkgo.yaml

# 3. 上传
apkgo upload -f app.apk
```

## 用法

### 上传

```bash
# 上传到所有配置的商店
apkgo upload -f app.apk

# 上传到指定商店
apkgo upload -f app.apk --store huawei,xiaomi

# 带更新日志
apkgo upload -f app.apk --notes "修复了登录问题"
apkgo upload -f app.apk --notes-file CHANGELOG.md

# 分架构包
apkgo upload -f app-arm32.apk --file64 app-arm64.apk

# 只验证不上传
apkgo upload -f app.apk --dry-run
```

### 初始化配置

```bash
# 生成包含所有商店的配置
apkgo init

# 只生成指定商店的配置
apkgo init --store huawei,xiaomi

# 指定配置文件路径
apkgo init -c production.yaml
```

### 查看支持的商店

```bash
apkgo stores
```

### 体检（验证商店配置）

`doctor` 在不上传文件的前提下，校验已配置商店的凭证和权限是否到位，避免真实上传到一半才发现问题：

```bash
# 校验所有已配置商店的凭证
apkgo doctor

# 只校验指定商店
apkgo doctor -s huawei

# 提供包名后会跑更深入的检查（包名映射、发布权限等）
apkgo doctor -s huawei -p com.example.app
apkgo doctor -s huawei -f app.apk         # 从 APK 自动取包名
```

任一探针失败时，退出码为 1。目前 Huawei 已支持，其他商店标记为 `doctor not implemented`。

## 配置文件

`apkgo.yaml`:

```yaml
# hooks 为可选配置，不需要可以不写
hooks:
  before: "./scripts/validate.sh"          # 所有上传前执行
  after: "./scripts/notify.sh"             # 所有上传后执行

stores:
  huawei:
    # 推荐：服务账号（PS256 JWT）
    service_account_file: "/secure/path/huawei-sa.json"
    # 或者: service_account: "<base64(JSON)>"
    # app_id: ""  # 可选，不填则自动通过包名查询
    before: "./scripts/before-huawei.sh"   # 可选，该商店上传前执行
    after: "./scripts/after-huawei.sh"     # 可选，该商店上传后执行

  xiaomi:
    email: "your@email.com"
    private_key: "your-private-key"             # 小米后台的「接口密钥」（被 SDK 当作 password 使用）
    cert_file: "/secure/path/xiaomi-pubkey.cer" # 公钥证书（也支持 cert: <PEM 内容> 或 cert: <base64>）

  oppo:
    client_id: "your-client-id"        # 19 位数字
    client_secret: "your-client-secret"

  vivo:
    access_key: "your-access-key"
    access_secret: "your-access-secret"

  honor:
    client_id: "your-client-id"
    client_secret: "your-client-secret"
    app_id: "your-app-id"

  tencent:
    user_id: "your-user-id"
    access_secret: "your-access-secret"
    app_id: "your-app-id"
    # 多 app: app_id_map: '{"com.foo":"111","com.bar":"222"}'

  pgyer:
    api_key: "your-pgyer-api-key"

  fir:
    api_token: "your-fir-api-token"

  # 单个脚本
  script:
    command: "./deploy.sh"

  # 多个脚本实例 (script.实例名)
  script.cdn-upload:
    command: "./upload-cdn.sh"
  script.dingtalk:
    command: "./notify-dingtalk.sh"
```

#### Hooks 说明

Hooks 是可选功能，不配置则不生效。Hook 脚本通过 stdin 接收 JSON 上下文，通过退出码控制流程：

- `before` hook 失败（非零退出码）→ 中止上传
- `after` hook 失败 → 仅记录警告，不影响结果
- 自动注入环境变量：`APKGO_STORE`、`APKGO_PACKAGE`、`APKGO_VERSION`
- stderr 输出作为错误信息

**全局 before hook** (`hooks.before`) stdin：

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "stores": ["huawei", "xiaomi"]
}
```

**全局 after hook** (`hooks.after`) stdin：

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

**商店 before hook** (`stores.<name>.before`) stdin：

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei"
}
```

**商店 after hook** (`stores.<name>.after`) stdin：

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei",
  "result": {"store": "huawei", "success": true, "duration_ms": 12300}
}
```

Hook 脚本示例（上传完成后发送钉钉通知）：

```bash
#!/bin/bash
input=$(cat)
results=$(echo "$input" | jq -r '.results[] | "\(.store): \(if .success then "✓" else "✗ "+.error end)"')
curl -s -X POST "$DINGTALK_WEBHOOK" \
  -H "Content-Type: application/json" \
  -d "{\"msgtype\":\"text\",\"text\":{\"content\":\"APK 发布完成 v${APKGO_VERSION}\n${results}\"}}"
```

### 环境变量

CI/CD 环境中可通过环境变量配置凭证，无需配置文件：

```bash
# 格式: APKGO_<商店名>_<字段名>=值
export APKGO_HUAWEI_SERVICE_ACCOUNT="$(base64 -w0 huawei-sa.json)"  # 推荐
export APKGO_XIAOMI_EMAIL="your@email.com"
export APKGO_XIAOMI_PRIVATE_KEY="your-接口密钥"
export APKGO_XIAOMI_CERT="$(base64 -w0 xiaomi-pubkey.cer)"
export APKGO_OPPO_CLIENT_ID="your-19-digit-id"
export APKGO_OPPO_CLIENT_SECRET="your-secret"
export APKGO_VIVO_ACCESS_KEY="your-key"
export APKGO_VIVO_ACCESS_SECRET="your-secret"
export APKGO_TENCENT_USER_ID="your-user-id"
export APKGO_TENCENT_ACCESS_SECRET="your-secret"
export APKGO_TENCENT_APP_ID="your-app-id"
# 多 app: APKGO_TENCENT_APP_ID_MAP='{"com.foo":"111","com.bar":"222"}'
export APKGO_PGYER_API_KEY="your-pgyer-key"
export APKGO_FIR_API_TOKEN="your-fir-token"

# 环境变量会覆盖配置文件中的同名字段
# 如果没有配置文件，完全通过环境变量配置也可以
apkgo upload -f app.apk --store huawei
```

### 凭证获取指南

| 商店 | 控制台地址 | 说明 |
|------|-----------|------|
| 华为 | [AppGallery Connect](https://developer.huawei.com/consumer/cn/console) | 用户与权限 > 服务账号（[详细步骤](#华为-appgallery-connect)） |
| 小米 | [小米开放平台](https://dev.mi.com) | 账号管理 > 接口密钥（[详细步骤](#小米开放平台)） |
| OPPO | [OPPO 开放平台](https://open.oppomobile.com) | 管理中心 > API 密钥管理（[详细步骤](#oppo-开放平台)） |
| vivo | [vivo 开放平台](https://dev.vivo.com.cn) | 账号管理 > API 接入（[详细步骤](#vivo-开放平台)） |
| 荣耀 | [荣耀开发者平台](https://developer.honor.com) | API 管理（[详细步骤](#荣耀开发者平台)） |
| 腾讯 | [腾讯开放平台](https://app.open.qq.com) | 应用 > 账户管理 > API 发布接口 > 申请开通（[详细步骤](#腾讯应用宝)） |
| 蒲公英 | [pgyer.com](https://www.pgyer.com/account/api) | 账户设置 > API 密钥（[详细步骤](#蒲公英-pgyer)） |
| fir.im | [betaqr.com.cn](https://www.betaqr.com.cn) | 账户 > API Token（[详细步骤](#firim)） |

每家的凭证申请流程都以官方文档为准（链接见下文），README 这边只描述 **apkgo 特有的事**：要哪几个字段、`doctor` 怎么验、需要注意的非显然行为。

#### 华为 AppGallery Connect

📖 官方文档：[Service Account 接入介绍](https://developer.huawei.com/consumer/cn/doc/AppGallery-connect-Guides/agcapi-getstarted-0000001111845114#section1785535363715)

推荐用**开发者级**服务账号（PS256 JWT 鉴权），不要选项目级——访问发布 API 会被拒。下载到的 JSON 凭证文件直接交给 apkgo：

```yaml
stores:
  huawei:
    service_account_file: "/secure/path/huawei-sa.json"
    # 或 base64(JSON) 内联：service_account: "ewogICJrZXlfaWQiOiAi..."
```

旧版 `client_id` + `client_secret` 仍兼容，但华为已不推荐。

```bash
apkgo doctor -s huawei -p com.example.app
```

3 项探针：`token` / `appid-list`（包名 → appId）/ `release-permission`（应用发布权限）。

#### 小米开放平台

📖 官方文档：[API 上传应用](https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1134)

要在小米后台「接口密钥」页面拿两样东西：**接口密钥**（SDK 里叫 password）和**公钥证书**（`.cer` 文件）。两个都是开发者账号绑定的。

```yaml
stores:
  xiaomi:
    email: "<开发者账号邮箱>"
    private_key: "<接口密钥>"
    cert_file: "/secure/path/xiaomi-pubkey.cer"
    # 也支持: cert: "-----BEGIN CERTIFICATE-----..." 或 base64(.cer)
```

```bash
apkgo doctor -s xiaomi -p com.example.app
```

> ⚠️ apkgo v3.0 之前内置了一份公钥证书，但那份 **2023-05 已过期**（且来源不明），从 v3.0 起必须自己提供。

#### OPPO 开放平台

📖 官方文档：[发布接口接入指引](https://open.oppomobile.com/new/developmentDoc/info?id=10998)

```yaml
stores:
  oppo:
    client_id: "<19 位数字>"
    client_secret: "<密钥>"
```

```bash
apkgo doctor -s oppo -p com.example.app
```

OPPO 的发布是异步任务，apkgo 会自动处理两个非显然的状态：撞 `911216 任务处理中` 时跳过 publish 直接等任务结束；撞 `911215 应用审核中` 视为成功（已进入审核队列）。

#### vivo 开放平台

📖 官方文档：[开放接口指引](https://dev.vivo.com.cn/documentCenter/doc/326)

```yaml
stores:
  vivo:
    access_key: "<...>"
    access_secret: "<...>"
```

```bash
apkgo doctor -s vivo -p com.example.app
```

vivo 的错误码分两层：网关 `code` + 业务 `subCode`。apkgo 同时识别两层，错误信息直接打印中文消息（比如 `[15042] 请上传与历史签名一致的APK包...`）。

#### 荣耀开发者平台

📖 官方文档：[发布接口指南](https://developer.honor.com/cn/doc/guides/101159)

```yaml
stores:
  honor:
    client_id: "<...>"
    client_secret: "<...>"
    # app_id: ""  # 可选，不填则按 APK 包名自动查
```

```bash
apkgo doctor -s honor -p com.example.app
```

doctor `app-detail` 探针会预检 *应用简介*（intro）—— 这个字段在荣耀后台必须填，否则 `update-language-info` 会以 `[20076] app introduction is empty` 拒绝。先在控制台填好再发版。

#### 腾讯应用宝

📖 官方文档：[API 接口传包-接入介绍](https://wikinew.open.qq.com/index.html#/iwiki/4015262492)

腾讯没有 list 或 pkg→id 反查接口，所以 `app_id` 必须手填。一份 yaml 服务多个应用用 `app_id_map`：

```yaml
stores:
  tencent:
    user_id: "<开发者 ID>"
    access_secret: "<接口密钥>"
    # 单 app:
    app_id: "<应用 ID>"
    # 多 app: 按 APK 包名命中
    # app_id_map: '{"com.example.foo":"111","com.example.bar":"222"}'
```

```bash
apkgo doctor -s tencent -p com.example.app
```

发布是异步任务，apkgo 会轮询 `query_app_update_status` 直到 `audit_status` 终态（最长 5 分钟）；超时视为成功（任务已交给腾讯）。

#### 蒲公英 (Pgyer)

📖 官方文档：[API 上传应用](https://www.pgyer.com/doc/view/app_upload)

```yaml
stores:
  pgyer:
    api_key: "<...>"
```

```bash
apkgo doctor -s pgyer -p com.example.app
```

#### fir.im

📖 官方文档：[betaqr.com.cn/docs](https://www.betaqr.com.cn/docs)

```yaml
stores:
  fir:
    api_token: "<...>"
```

```bash
apkgo doctor -s fir
```

> ⚠️ **fir 上传要求账号已完成实名认证**，否则 `/apps` 接口会以 `没有实名认证不能上传app` 拒绝。先去后台做实名再用。

## AI Agent 集成

apkgo 的输出格式专为 AI Agent 和自动化场景设计：

**结构化 JSON 输出** (stdout):
```json
{
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1},
  "results": [
    {"store": "huawei", "success": true, "duration_ms": 12300},
    {"store": "xiaomi", "success": false, "error": "invalid private key", "duration_ms": 400}
  ]
}
```

**语义化退出码**:
| Code | 含义 |
|------|------|
| 0 | 全部成功 |
| 1 | 部分失败 |
| 2 | 全部失败 |
| 3 | 输入错误 |

**可发现的配置 schema**:
```bash
apkgo stores  # 返回每个商店需要的配置字段
```

**非交互**: 无 prompt、无确认，适合无人值守环境。

**实时进度流（NDJSON）**：父进程 fork apkgo 想实时拿进度时，加 `--progress-stream`，stdout 变成每行一个 JSON 事件：

```bash
apkgo upload -f app.apk --progress-stream
```

```json
{"type":"start","apk":{"package":"...","version_name":"1.2.0","version_code":120},"stores":["huawei","xiaomi"]}
{"type":"phase","store":"huawei","phase":"auth"}
{"type":"phase","store":"huawei","phase":"uploading"}
{"type":"total","store":"huawei","total_bytes":62914560}
{"type":"bytes","store":"huawei","sent":7045120,"total":62914560}
{"type":"bytes","store":"huawei","sent":23560192,"total":62914560}
{"type":"phase","store":"huawei","phase":"submitting"}
{"type":"result","store":"huawei","success":true,"duration_ms":34570}
{"type":"done","apk":{...},"results":[...]}
```

`bytes` 事件每 ~100ms 一条（throttled），多家并发各自一条流，按 `store` 字段区分。Go 父进程消费示例：

```go
cmd := exec.CommandContext(ctx, "apkgo", "upload", "-f", apkPath, "--progress-stream")
out, _ := cmd.StdoutPipe()
cmd.Start()
sc := bufio.NewScanner(out)
for sc.Scan() {
    var evt map[string]any
    json.Unmarshal(sc.Bytes(), &evt)
    switch evt["type"] {
    case "bytes":
        ui.UpdateProgress(evt["store"].(string), evt["sent"].(float64), evt["total"].(float64))
    case "result":
        ui.MarkStoreDone(evt["store"].(string), evt["success"].(bool))
    case "done":
        ui.Finish(evt["results"])
    }
}
cmd.Wait()
```

## 嵌入式调用（Go SDK）

apkgo 同时是一个 CLI **和** 一个 Go 库。Cloud worker、自定义 CI 工具、IDE 插件可以直接 import `pkg/apkgo` 调用上传流程，免去 spawn 子进程 + 解析 stdout 的麻烦。

```go
import (
    "context"
    "github.com/KevinGong2013/apkgo/pkg/apkgo"
    "github.com/KevinGong2013/apkgo/pkg/config"
    "github.com/KevinGong2013/apkgo/pkg/uploader"
)

cfg := &config.Config{
    Stores: map[string]map[string]string{
        "huawei":  {"service_account": vault.Get("huawei-sa-base64")},
        "tencent": {
            "user_id":       "...",
            "access_secret": vault.Get("tencent-secret"),
            "app_id_map":    `{"com.foo":"111","com.bar":"222"}`,
        },
    },
}

result, err := apkgo.Run(ctx, apkgo.Job{
    APKFile:  "https://artifacts.example.com/v1.2.0.apk",
    Stores:   []string{"huawei", "tencent"},
    Notes:    "Bug fixes",
    Config:   cfg,
    Progress: uploader.NopManager,  // or NewNDJSONManager(w) for streamed events
})
```

特性：

- **零全局状态**：`apkgo.Run` 不动 `slog.Default`、不设 exit code，安全地在长生命周期进程中调用
- **支持 URL 输入**：`APKFile` / `APKFile64` 接受本地路径或 http(s) URL（自动 fetch + 临时文件 + 退出清理）
- **可插拔进度报告**：`uploader.ProgressManager` 接口可以接 mpb / NDJSON / 自定义实现（push 到 Prometheus、emit 到 Kafka 等）
- **Pre-upload 错误才 return error**：`Run` 返回的 `error` 只覆盖 fetch / 解析 / config 阶段；进入上传后每家的失败都在 `Result.Results[i].Error` 里。Cloud orchestrator 可以基于此分类重试

完整 API 见 [`pkg/apkgo`](pkg/apkgo) 的 godoc。

## CI/CD 示例

### GitHub Actions

```yaml
- name: Upload to app stores
  env:
    APKGO_HUAWEI_SERVICE_ACCOUNT: ${{ secrets.HUAWEI_SERVICE_ACCOUNT }}  # base64(JSON 凭证)
    APKGO_XIAOMI_EMAIL: ${{ secrets.XIAOMI_EMAIL }}
    APKGO_XIAOMI_PRIVATE_KEY: ${{ secrets.XIAOMI_PRIVATE_KEY }}
    APKGO_XIAOMI_CERT: ${{ secrets.XIAOMI_CERT }}             # base64(.cer 文件)
    APKGO_OPPO_CLIENT_ID: ${{ secrets.OPPO_CLIENT_ID }}
    APKGO_OPPO_CLIENT_SECRET: ${{ secrets.OPPO_CLIENT_SECRET }}
    APKGO_VIVO_ACCESS_KEY: ${{ secrets.VIVO_ACCESS_KEY }}
    APKGO_VIVO_ACCESS_SECRET: ${{ secrets.VIVO_ACCESS_SECRET }}
    APKGO_TENCENT_USER_ID: ${{ secrets.TENCENT_USER_ID }}
    APKGO_TENCENT_ACCESS_SECRET: ${{ secrets.TENCENT_ACCESS_SECRET }}
    APKGO_TENCENT_APP_ID_MAP: ${{ secrets.TENCENT_APP_ID_MAP }}     # 多 app: '{"com.foo":"111",...}'
  run: |
    apkgo upload \
      -f app/build/outputs/apk/release/app-release.apk \
      --notes-file CHANGELOG.md \
      --store huawei,xiaomi,oppo,vivo,tencent \
      --timeout 15m
```

### Docker

```bash
docker run --rm \
  -v $(pwd)/apkgo.yaml:/apkgo.yaml \
  -v $(pwd)/app.apk:/app.apk \
  ghcr.io/kevingong2013/apkgo:latest \
  upload -f /app.apk --notes "Bug fixes"
```

## Web GUI

不想用命令行？`apkgo serve` 启动本地 Web 界面：

```bash
apkgo serve                    # http://localhost:8080
apkgo serve -p 9090            # 自定义端口
apkgo serve -c production.yaml # 指定配置文件
```

打开浏览器，拖拽 APK、勾选商店、填写更新日志、点击上传。适合运营人员使用。

## 跨机器同步配置

通过加密导出/导入，安全地在多台机器或 CI 间共享商店凭证：

```bash
# 机器 A：加密导出
apkgo config export --out config.enc
# 输入密码（或设置 APKGO_CONFIG_KEY 环境变量）

# 提交到私有仓库
git add config.enc && git commit -m "sync config" && git push

# 机器 B：解密导入
git pull
apkgo config import config.enc
```

CI 中使用环境变量免交互：

```yaml
- name: Import apkgo config
  env:
    APKGO_CONFIG_KEY: ${{ secrets.APKGO_CONFIG_KEY }}
  run: apkgo config import config.enc
```

加密方式：AES-256-GCM + scrypt 密钥派生，密码错误会明确提示。

## 全部命令

```
apkgo init          [-s store1,store2] [-c config.yaml]
apkgo upload        -f <apk> [--file64 <apk>] [-s stores] [-n notes] [--notes-file path] [--dry-run] [-t timeout]
apkgo doctor        [-s stores] [-f <apk> | -p <package>]
apkgo serve         [-p port] [-c config.yaml]
apkgo config export --out <file>
apkgo config import <file>
apkgo stores        [-o json|text]
apkgo history       [-n limit]
apkgo upgrade
apkgo version       [-o json|text]
```

## 全局参数

```
-c, --config        配置文件路径 (默认: apkgo.yaml)
-o, --output        输出格式: json 或 text (默认: json)
-t, --timeout       全局超时 (默认: 10m)
-v, --verbose       详细日志输出到 stderr
    --no-telemetry  禁用匿名使用统计
```

## 隐私

apkgo 收集匿名使用统计以改进产品，**不收集任何敏感信息**：

| 收集 | 不收集 |
|------|--------|
| 匿名安装 ID (随机 UUID) | 账号、凭证 |
| 使用的商店名称 | 包名、应用名 |
| 上传成功/失败 | APK 文件内容 |
| CLI/GUI 使用方式 | 更新日志内容 |
| apkgo 版本、OS/架构 | IP 地址 |

关闭方式：`--no-telemetry` 或 `APKGO_TELEMETRY=off`

## License

[Apache License 2.0](./LICENSE)
