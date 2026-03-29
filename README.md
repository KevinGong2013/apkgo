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
| 自定义服务器 | HTTP Header |

## 安装

```bash
# AI Agent Skill (支持 Claude Code、Cursor、Windsurf 等 40+ agent)
npx skills add KevinGong2013/apkgo

# Linux (direct download)
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# Go install
go install github.com/KevinGong2013/apkgo@latest

# Docker
docker pull ghcr.io/kevingong2013/apkgo:latest

# 或从 Releases 页面下载预编译二进制
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
stores:
  huawei:
    client_id: "your-client-id"
    client_secret: "your-client-secret"
    # app_id: ""  # 可选，不填则自动通过包名查询

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

  custom:
    url: "https://your-server.com/api/upload"
    method: "POST"
    field_name: "file"
    header_Authorization: "Bearer your-token"
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

## 全部命令

```
apkgo init      [-s store1,store2] [-c config.yaml]
apkgo upload    -f <apk> [--file64 <apk>] [-s stores] [-n notes] [--notes-file path] [--dry-run] [-t timeout]
apkgo stores    [-o json|text]
apkgo version   [-o json|text]
```

## 全局参数

```
-c, --config     配置文件路径 (默认: apkgo.yaml)
-o, --output     输出格式: json 或 text (默认: json)
-t, --timeout    全局超时 (默认: 10m)
-v, --verbose    详细日志输出到 stderr
```

## License

[Apache License 2.0](./LICENSE)
