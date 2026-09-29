# apkgo

![GitHub release](https://img.shields.io/github/v/release/KevinGong2013/apkgo) ![build](https://img.shields.io/github/actions/workflow/status/kevingong2013/apkgo/release.yml?style=flat-square) ![go](https://img.shields.io/github/go-mod/go-version/kevingong2013/apkgo?style=flat-square) [![license](https://img.shields.io/badge/license-PolyForm--Noncommercial--1.0.0-blue?style=flat-square)](./LICENSE) [![skills.sh](https://skills.sh/b/KevinGong2013/apkgo)](https://skills.sh/KevinGong2013/apkgo)

🌐 **Language / 语言**: [English](./README.en.md) · **简体中文**

一行命令，将 APK 发布到所有主流安卓应用商店。

> **不想搭 CI、不想碰命令行？** 试试托管版 [**apkgo cloud**](https://apkgo.baici.tech) —— 浏览器打开就能发版，凭证云端托管、多人协作、发布历史可追溯，免装免运维，运营和产品同事也能独立上手。除命令行版覆盖的安卓各大商店外，云端还支持 **iOS（App Store）** 与 **鸿蒙（HarmonyOS）** 上架。

## 安装

**macOS / Linux** — 一行脚本自动识别 OS/架构、下载、校验 SHA-256：

```bash
curl -fsSL https://apkgo.com.cn/install.sh | sh
```

> 默认装到 `/usr/local/bin`，不可写时会提示用 `sudo` 或 `APKGO_INSTALL_DIR=$HOME/.local/bin sh`。
> 锁版本：`APKGO_VERSION=v4.0.0 sh`。

**其他方式：**

```bash
# AI Agent Skill (支持 Claude Code、Cursor、Windsurf 等 40+ agent)
npx skills add KevinGong2013/apkgo

# Go
go install github.com/KevinGong2013/apkgo/v4@latest

# Docker
docker pull ghcr.io/kevingong2013/apkgo:latest
```

<details>
<summary>手动下载 / Windows</summary>

```bash
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
```

> 如果 `apkgo version` 报 `unknown command` 或输出的帮助里只有 `doctor` / `upload`，说明 PATH 里优先命中了旧版 apkgo（常见于曾执行过不带 `/v4` 的 `go install`，旧版 v1/v3 位于 `%USERPROFILE%\go\bin`）。用 `where apkgo`（macOS/Linux：`which -a apkgo`）找出所有副本，删除旧的即可。

</details>

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

# vivo 走沙箱，其他渠道只验证不上传
apkgo upload -f app.apk --sandbox

# 直接用 http(s) 地址（自动下载到临时文件；私有地址可加请求头，可重复）
apkgo upload -f https://artifacts.example.com/app-v1.apk --store huawei
apkgo upload -f https://private.example.com/app.apk --fetch-header "Authorization: Bearer xxx"

# 定时发布（RFC3339 时间）
apkgo upload -f app.apk --release-time 2026-06-20T10:00:00+08:00

# 随新版本同步更新商店资料（一句话介绍 / 描述 / icon / 截图）
apkgo upload -f app.apk --listing listing.yaml

# Google Play 上传 AAB（.aab 只会发到 googleplay）
apkgo upload -f app-release.aab -s googleplay
```

- `-f` 为 URL 时，huawei / honor / vivo 直接让商店从该地址拉包（honor 仅对 ≥ `url_push_min_mb`，默认 100MB 的文件），其他商店由 apkgo 下载后上传。
- `--release-time` 支持 huawei、harmony、honor、xiaomi、oppo、vivo、samsung、tencent，其他商店立即发布。
- 文件类型：`.apk` 发往所有安卓商店；`.aab` 仅 googleplay；鸿蒙 `.app` 仅 harmony。

#### 同步更新商店资料（`--listing`）

发新版本时可以顺带更新商店资料：**一句话介绍、长描述、icon、截图**。资料在传包之后、提交审核之前写入，随新版本一起审核；不支持只改资料不发版，也不会修改应用名（应用名须与 APK、APP 备案、软著一致）。只更新商店的默认语言。

```bash
apkgo upload -f app.apk --listing listing.yaml --dry-run   # 先按各商店规格校验
apkgo upload -f app.apk --listing listing.yaml
```

```yaml
# listing.yaml —— 通用默认值 + 按商店覆盖；相对路径以本文件所在目录为准
brief: 一句话介绍
description_file: desc.md        # 或 description: |
icon: assets/icon-512.png
screenshots: [assets/s1.png, assets/s2.png, assets/s3.png, assets/s4.png]
stores:
  oppo:
    brief: 十三字以内的简介       # OPPO 限 13 字且不能有标点空格
  huawei:
    icon: assets/icon-216.png
    screenshots: [assets/hw1.png, assets/hw2.png, assets/hw3.png]   # 华为截图为 450×800
```

- 每个字段都可选，没写的保持商店现有值；`stores.<商店>` 覆盖默认值，`script.cdn` 这样的实例名会继承 `script` 的覆盖。
- 上传开始前按各商店规格逐项校验（长度、图片格式/尺寸/大小、截图张数），**一次性列出全部问题**，退出码 3；`--dry-run` 同样校验。
- 成功的结果里 `listing` 字段列出本次提交的资料项；版本已在商店侧（`already_done`）时不列。pgyer、fir 不支持资料，会给出警告并照常上传；`script` 商店在 stdin JSON 的 `Listing` 字段收到资料。
- 各商店规格以 `apkgo stores` 输出的 `listing` 为准，摘要如下：

| 商店 | 一句话介绍 | 长描述 | icon | 截图 |
|---|---|---|---|---|
| huawei | ≤80 | ≤8000 | PNG 216×216，≤2MB | 450×800，3–5 张 |
| harmony | ≤80，且与描述不同 | ≤8000 | PNG/WebP 216×216 或 1024×1024 | 1080×1920，3–10 张 |
| honor | ≤80 | ≤8000 | PNG/JPG 512×512，≤200KB | 1080×1920，≤5MB，3–5 张 |
| vivo | 5–16 个汉字 | 50–1000 | PNG 正方形 256–512px，≤500KB | 1080×1920，≤2MB，3–5 张 |
| oppo | ≤13，无标点空格 | ≥20 | PNG 512×512，≤1MB | 1080×1920，≤1MB，2–5 张 |
| xiaomi | ≤17 个汉字，句末无标点 | — | PNG 512×512 | 1080×1920 或 1920×1080（不混用），≤5MB，3–5 张 |
| meizu | — | 100–1000 | PNG/JPG | PNG/JPG |
| samsung | ≤40 字节 | ≤4000 字节 | PNG 512×512，≤1MB | 320–3840px，宽高比 ≤2:1，4–8 张 |
| tencent | 5–15 | 60–500 | PNG 512×512，≤200KB | ≤1MB，宽高一致，4–5 张 |
| googleplay | ≤80 | ≤4000 | PNG 512×512，≤1MB | 320–3840px，宽高比 ≤2:1，2–8 张 |

#### 鸿蒙（HarmonyOS）上架

```bash
# 上传 DevEco Studio 打出的签名 .app 整包到华为应用市场（鸿蒙）
apkgo upload -f demo-default-signed.app -s harmony --notes "适配 HarmonyOS 5"
apkgo audit -f demo-default-signed.app -s harmony     # 查询鸿蒙审核状态
apkgo doctor -f demo-default-signed.app -s harmony    # 体检凭证 / 应用 ID / 上架权限
```

`harmony` 商店走 AppGallery Connect 的 HarmonyOS Publishing API，**复用 huawei 的 Service Account 凭证**（同一个 AGC 账号同时覆盖安卓与鸿蒙应用），流程为：按 bundleName 查 appId（`packageTypes=7`）→ `upload-url/for-obs` 取签名上传地址 → PUT 上传 `.app` → 绑定软件包 → 等待编译解析 → 提交审核。只接受 **`.app` 整包**（AGC 不接受单个 `.hap` 模块）；`-f` 为鸿蒙包时会自动跳过安卓商店（不传 `-s` 时），显式指定安卓商店则直接报错。支持 `--release-time` 定时发布，不支持 `--file64` 与 URL 直传。

`--sandbox` 与 `--dry-run` 互斥。目前只有 vivo 支持沙箱：vivo 会真实调用沙箱 API，其他目标渠道执行与 `--dry-run` 相同的本地校验。沙箱运行不执行 hooks、不写上传历史、不触发生命周期事件回调，也不上报上传遥测。结果顶层包含 `"sandbox": true`，vivo 结果包含 `"sandbox": true`，其他渠道包含 `"dry_run": true`。

vivo 的[沙箱环境](https://dev.vivo.com.cn/documentCenter/doc/327#s-l67kfh1m)与正式环境的应用数据、`access_key` 和密钥完全隔离；请按[在线测试环境说明](https://dev.vivo.com.cn/documentCenter/doc/327#s-b9qi52f4)先在沙箱创建应用并申请独立凭据。测试环境限制每个接口 100 次/天。

### 查询审核状态

上传成功只代表「已提交（审核中）」，审核结果用 `audit` 单独查询：

```bash
apkgo audit -p com.example.app
apkgo audit -f app.apk -s tencent,huawei
# 持续轮询，直到每个商店都出结果（通过 / 驳回 / 撤回）或达到 -t 超时
apkgo audit -p com.example.app --watch --interval 1m -t 1h
```

目前支持 huawei、harmony、honor、xiaomi、oppo、vivo、meizu、tencent、samsung。

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

# 使用内置 YAML 解析器，只输出当前配置中的商店名，不输出凭据
apkgo -c apkgo.yaml stores --configured
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

任一探针失败时，退出码为 1。除 googleplay 与 script 外的商店均已支持，未实现的会标记为 `doctor not implemented`。

## 配置文件

`apkgo.yaml`:

```yaml
# 可选：检查新版本的频率，如 "30d"、"7d"，"0" 关闭
# update_check: "7d"

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
    timeout: 8m                            # 可选，该商店单独超时（覆盖全局 --timeout）

  harmony:
    # 鸿蒙（HarmonyOS）应用：凭证与 huawei 相同，可直接复用同一份 Service Account
    service_account_file: "/secure/path/huawei-sa.json"
    # app_id: ""                 # 可选，不填则按 bundleName 自动查询（仅匹配鸿蒙应用）
    # lang: "zh-CN"              # 可选，更新日志写入的语言，默认取 AGC 里应用的默认语言
    # chinese_mainland_flag: "1" # 可选，开发者注册地非中国大陆时 AGC 要求填写

  xiaomi:
    email: "your@email.com"
    private_key: "your-private-key"               # 小米后台的「接口密钥」（被 SDK 当作 password 使用）
    # cert_file: "/secure/path/xiaomi-pubkey.cer" # 可选，默认使用内置公钥证书（也支持 cert: <PEM 内容> 或 cert: <base64>）

  oppo:
    client_id: "your-client-id"        # 19 位数字
    client_secret: "your-client-secret"

  vivo:
    access_key: "your-access-key"
    access_secret: "your-access-secret"
    sandbox_access_key: "your-sandbox-access-key"        # --sandbox 时使用
    sandbox_access_secret: "your-sandbox-access-secret"  # --sandbox 时使用

  honor:
    client_id: "your-client-id"
    client_secret: "your-client-secret"
    # app_id: ""          # 可选，不填则按包名自动查询
    # url_push_min_mb: "100" # 可选，-f 为 URL 时 ≥ 该大小（MB）才让荣耀从 URL 拉包

  meizu:
    client_id: "your-client-id"       # 魅族开放平台「客户端凭证」
    client_secret: "your-client-secret"

  tencent:
    user_id: "your-user-id"
    access_secret: "your-access-secret"
    app_id: "your-app-id"
    # 多 app: app_id_map: '{"com.foo":"111","com.bar":"222"}'

  samsung:
    service_account_id: "your-service-account-id"  # Seller Portal 服务账号 ID
    private_key: "-----BEGIN PRIVATE KEY-----..."   # Seller Portal 下载的 RSA 私钥（PEM）
    content_id: "your-content-id"                  # Galaxy Store 中应用的 content ID

  googleplay:
    json_key_file: "/secure/path/play-sa.json"     # 服务账号 JSON 密钥
    package_name: "com.example.app"
    # track: "production"  # 可选：production（默认）/ beta / alpha / internal

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
- 通过 `sh -c` 执行（Windows 为 `cmd /C`）
- `apk.platform` 为 `android`（APK/AAB）或 `harmony`（鸿蒙 `.app`）

**全局 before hook** (`hooks.before`) stdin：

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "stores": ["huawei", "xiaomi"]
}
```

**全局 after hook** (`hooks.after`) stdin：

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

**商店 before hook** (`stores.<name>.before`) stdin：

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei"
}
```

**商店 after hook** (`stores.<name>.after`) stdin：

```json
{
  "file_path": "/path/to/app.apk",
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1, "app_name": "MyApp"},
  "store": "huawei",
  "result": {"store": "huawei", "success": true, "category": "success", "duration_ms": 12300}
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
export APKGO_MEIZU_CLIENT_ID="your-client-id"
export APKGO_MEIZU_CLIENT_SECRET="your-secret"
export APKGO_TENCENT_USER_ID="your-user-id"
export APKGO_TENCENT_ACCESS_SECRET="your-secret"
export APKGO_TENCENT_APP_ID="your-app-id"
# 多 app: APKGO_TENCENT_APP_ID_MAP='{"com.foo":"111","com.bar":"222"}'
export APKGO_SAMSUNG_SERVICE_ACCOUNT_ID="your-sa-id"
export APKGO_SAMSUNG_PRIVATE_KEY="$(cat samsung-private-key.pem)"
export APKGO_SAMSUNG_CONTENT_ID="your-content-id"
export APKGO_GOOGLEPLAY_JSON_KEY_FILE="/secure/path/play-sa.json"
export APKGO_GOOGLEPLAY_PACKAGE_NAME="com.example.app"
export APKGO_PGYER_API_KEY="your-pgyer-key"
export APKGO_FIR_API_TOKEN="your-fir-token"

# 环境变量会覆盖配置文件中的同名字段
# 如果没有配置文件，完全通过环境变量配置也可以
apkgo upload -f app.apk --store huawei
```

### 凭证不落盘（cloud worker / CI / 多租户）

`--creds-from` 让 apkgo 从非磁盘源读取 JSON 格式的凭证。orchestrator 把凭证从 secrets manager（Vault / AWS SM / GCP SM 等）取出后通过 stdin 或文件描述符直接注入子进程，**全程不写盘、不进 env**：

```bash
# 方式 A: stdin
vault read -format=json secret/apkgo | jq .data \
  | apkgo upload -f app.apk --creds-from=stdin

# 方式 B: 文件描述符（适合 stdin 已经被占用的场景）
apkgo upload -f app.apk --creds-from=fd:3 3<<<"$(vault-creds-as-json)"
```

JSON 格式跟 yaml 一一对应：
```json
{
  "stores": {
    "huawei": {"service_account": "<base64>"},
    "tencent": {
      "user_id": "...",
      "access_secret": "...",
      "app_id_map": "{\"com.foo\":\"111\"}"
    }
  },
  "hooks": {"before": "...", "after": "..."}
}
```

apkgo 解析完立刻把输入字节 zero-out，避免 secret 留在缓冲区。设了 `--creds-from` 时 `--config` 和 `APKGO_*` env 都被忽略。

## AI Agent 集成

apkgo 的输出格式专为 AI Agent 和自动化场景设计：

**结构化 JSON 输出** (stdout):
```json
{
  "apk": {"platform": "android", "package": "com.example.app", "version_name": "1.0.0", "version_code": 1},
  "results": [
    {"store": "huawei", "success": true, "category": "success", "duration_ms": 12300},
    {"store": "oppo",   "success": true, "category": "already_done", "duration_ms": 3200},
    {"store": "xiaomi", "success": false, "category": "policy_block", "error": "签名不一致...", "duration_ms": 400}
  ]
}
```

**Category（重试决策提示）**：每个 result 带一个 `category` 字段，把各家千奇百怪的错误码归成 cloud orchestrator 友好的几个桶，避免父进程解析中文错误。可能的值：

| Category | 含义 | 建议处理 |
|---|---|---|
| `success` | 上传成功 | mark done |
| `already_done` | 该版本已在商店侧（如 OPPO 911215 应用审核中） | mark done，不重试 |
| `auth_failed` | 凭证错 | 让用户改 secret，不重试 |
| `network_retry` | 网络超时 / 5xx | 退避后重试 |
| `store_busy` | 商店限流 / 上次任务还没完 | 等几分钟再重试 |
| `policy_block` | 签名不一致、审核驳回等业务规则拒绝 | 让用户处理，不重试 |
| `config_invalid` | 后台元数据缺失（intro / 分类 / publisher entity） | 让用户去后台填，不重试 |
| `unknown` | 还没分类到 | 默认按"不可重试"处理 |

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

## 全部命令

```
apkgo init          [-s store1,store2] [-c config.yaml]
apkgo upload        -f <apk|aab|app|url> [--file64 <apk|url>] [-s stores] [-n notes] [--notes-file path]
                    [--release-time <RFC3339>] [--listing <file>] [--fetch-header "Name: value"] [--dry-run | --sandbox]
                    [--progress-stream] [-t timeout]
apkgo audit         [-s stores] (-f <apk> | -p <package>) [--watch] [--interval 30s]
apkgo doctor        [-s stores] [-f <apk> | -p <package>]
apkgo config export --out <file>
apkgo config import <file>
apkgo stores        [--configured] [-o json|text]
apkgo history       [-n limit]
apkgo upgrade
apkgo version       [-o json|text]
```

## 全局参数

```
-c, --config        配置文件路径 (默认: apkgo.yaml)
    --creds-from    从 stdin 或 fd:N 读取 JSON 凭证（覆盖 --config）
-o, --output        输出格式: json 或 text (默认: json)
-t, --timeout       全局超时 (默认: 10m)
-v, --verbose       详细日志输出到 stderr
```

## License

[PolyForm Noncommercial License 1.0.0](./LICENSE) —— 仅限非商业用途免费使用，商业使用请联系作者授权。
