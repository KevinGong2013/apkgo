# apkgo

![GitHub release](https://img.shields.io/github/v/release/KevinGong2013/apkgo) ![build](https://img.shields.io/github/actions/workflow/status/kevingong2013/apkgo/release.yml?style=flat-square) ![go](https://img.shields.io/github/go-mod/go-version/kevingong2013/apkgo?style=flat-square) ![license](https://img.shields.io/github/license/kevingong2013/apkgo?style=flat-square) [![skills.sh](https://skills.sh/b/KevinGong2013/apkgo)](https://skills.sh/KevinGong2013/apkgo)

🌐 **Language / 语言**: [English](./README.en.md) · **简体中文**

一行命令，将 APK 发布到所有主流安卓应用商店。为 CI/CD 和 AI Agent 设计。

> **不想搭 CI、不想碰命令行？** 试试托管版 [**apkgo cloud**](https://apkgo.baici.tech) —— 浏览器打开就能发版，凭证云端托管、多人协作、发布历史可追溯，免装免运维，运营和产品同事也能独立上手。除命令行版覆盖的安卓各大商店外，云端还支持 **iOS（App Store）** 与 **鸿蒙（HarmonyOS）** 上架。
>
> **用 fastlane？** [**fastlane-plugin-apkgo**](https://github.com/KevinGong2013/fastlane-plugin-apkgo) 一个 lane 即可发布到所有商店（走 apkgo cloud，凭证云端托管）：`fastlane add_plugin apkgo`。
>
> **不止安卓？** [**白辞 baici.tech**](https://baici.tech) 提供 iOS / 鸿蒙 / 微信、支付宝、抖音小程序的一站式上架代办，覆盖 ICP 备案、软件著作权、应用核准全流程。98% 一次过审、24h 极速响应、不过退款，已服务 500+ 开发者与企业。

## 安装

**macOS / Linux** — 一行脚本自动识别 OS/架构、下载、校验 SHA-256：

```bash
curl -fsSL https://apkgo.com.cn/install.sh | sh
```

> 默认装到 `/usr/local/bin`，不可写时会提示用 `sudo` 或 `APKGO_INSTALL_DIR=$HOME/.local/bin sh`。
> 锁版本：`APKGO_VERSION=v3.1.0 sh`。

**其他方式：**

```bash
# AI Agent Skill (支持 Claude Code、Cursor、Windsurf 等 40+ agent)
npx skills add KevinGong2013/apkgo

# Go
go install github.com/KevinGong2013/apkgo@latest

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
  "apk": {"package": "com.example.app", "version_name": "1.0.0", "version_code": 1},
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
apkgo upload        -f <apk> [--file64 <apk>] [-s stores] [-n notes] [--notes-file path] [--dry-run] [-t timeout]
apkgo doctor        [-s stores] [-f <apk> | -p <package>]
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
```

## License

[PolyForm Noncommercial License 1.0.0](./LICENSE) —— 仅限非商业用途免费使用，商业使用请联系作者授权。
