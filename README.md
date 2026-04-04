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

## 配置文件

`apkgo.yaml`:

```yaml
# hooks 为可选配置，不需要可以不写
hooks:
  before: "./scripts/validate.sh"          # 所有上传前执行
  after: "./scripts/notify.sh"             # 所有上传后执行

stores:
  huawei:
    client_id: "your-client-id"
    client_secret: "your-client-secret"
    # app_id: ""  # 可选，不填则自动通过包名查询
    before: "./scripts/before-huawei.sh"   # 可选，该商店上传前执行
    after: "./scripts/after-huawei.sh"     # 可选，该商店上传后执行

  xiaomi:
    email: "your@email.com"
    private_key: "your-private-key"

  oppo:
    client_id: "your-client-id"
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
    package_name: "com.example.app"

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
export APKGO_HUAWEI_CLIENT_ID="your-client-id"
export APKGO_HUAWEI_CLIENT_SECRET="your-client-secret"
export APKGO_XIAOMI_EMAIL="your@email.com"
export APKGO_XIAOMI_PRIVATE_KEY="your-key"

# 环境变量会覆盖配置文件中的同名字段
# 如果没有配置文件，完全通过环境变量配置也可以
apkgo upload -f app.apk --store huawei
```

### 凭证获取指南

| 商店 | 控制台地址 | 说明 |
|------|-----------|------|
| 华为 | [AppGallery Connect](https://developer.huawei.com/consumer/cn/console) | 用户与权限 > API 密钥 > Connect API |
| 小米 | [小米开放平台](https://dev.mi.com) | 管理中心 > API 管理 |
| OPPO | [OPPO 开放平台](https://open.oppomobile.com) | 管理中心 > API 密钥管理 |
| vivo | [vivo 开放平台](https://dev.vivo.com.cn) | 账号管理 > API 管理 |
| 荣耀 | [荣耀开发者平台](https://developer.honor.com) | API 管理 |
| 腾讯 | [腾讯开放平台](https://app.open.qq.com) | 账户管理 > API 发布接口 > 申请开通 |

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

## CI/CD 示例

### GitHub Actions

```yaml
- name: Upload to app stores
  env:
    APKGO_HUAWEI_CLIENT_ID: ${{ secrets.HUAWEI_CLIENT_ID }}
    APKGO_HUAWEI_CLIENT_SECRET: ${{ secrets.HUAWEI_CLIENT_SECRET }}
    APKGO_XIAOMI_EMAIL: ${{ secrets.XIAOMI_EMAIL }}
    APKGO_XIAOMI_PRIVATE_KEY: ${{ secrets.XIAOMI_PRIVATE_KEY }}
  run: |
    apkgo upload \
      -f app/build/outputs/apk/release/app-release.apk \
      --notes-file CHANGELOG.md \
      --store huawei,xiaomi \
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
