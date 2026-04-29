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
| vivo | [vivo 开放平台](https://dev.vivo.com.cn) | 账号管理 > API 管理 |
| 荣耀 | [荣耀开发者平台](https://developer.honor.com) | API 管理 |
| 腾讯 | [腾讯开放平台](https://app.open.qq.com) | 账户管理 > API 发布接口 > 申请开通 |

#### 华为 AppGallery Connect

华为推荐使用 **服务账号 (Service Account)** 鉴权。旧的 API 密钥 (Client ID + Key) 也可继续使用，但华为已不再推荐，且发布接口对密钥类型要求很严（团队级而非项目级，且必须勾上 *应用发布* 权限），调错就会撞上 `203886599`/`203890688` 等不友好的错误码。

##### 推荐：服务账号 (Service Account)

服务账号采用 PS256 JWT 鉴权，签名后的 JWT 直接作为 Bearer token 使用，无需 OAuth 交换步骤。

1. 进入 [AGC 控制台](https://developer.huawei.com/consumer/cn/service/josp/agc/index.html#/myApp)
2. 右上角用户名 → **用户与权限** → 左侧 **服务账号**
3. 切到 **开发者** 标签页（**不要选项目级**，项目级访问发布 API 会被拒绝并返回 `403 project credential can not access the developer credentialType api`）
4. **新建服务账号** → 勾选 **Connect API**，展开勾上 **应用发布 (App release)**
5. 创建完成后下载 JSON 凭证文件（包含 `key_id` / `private_key` / `sub_account` 等字段，**只能下载一次**）
6. 配置 `apkgo.yaml`，二选一：

   ```yaml
   stores:
     huawei:
       # 方式 A: 直接指向凭证文件
       service_account_file: "/secure/path/huawei-sa.json"
       # app_id: ""  # 可选，不填则按 APK 包名自动查
   ```

   ```yaml
   stores:
     huawei:
       # 方式 B: 内嵌 base64 (适合 CI/CD secrets)
       service_account: "ewogICJrZXlfaWQiOiAi..."  # base64(JSON 凭证)
   ```

   生成 base64：`base64 -w0 huawei-sa.json`（macOS：`base64 -i huawei-sa.json`）

7. 验证：

   ```bash
   # 只校验凭证 (最快)
   apkgo doctor -s huawei

   # 同时校验包名映射 + 应用发布权限 (推荐)
   apkgo doctor -s huawei -p com.example.app
   ```

   `doctor` 不会上传文件，但会真实调用三个接口：token（凭证类型）、appid-list（包名 → appId）、upload-url（应用发布权限）。三项都 ✓ 才说明真实上传会跑通；任一 ✗ 都能在配置阶段就发现，不必等到上传到一半才报错。

##### 旧版：API 密钥（不推荐，仅兼容旧配置）

1. **用户与权限** → **API 密钥** → 切到 **团队** 标签页 → **新建**
2. 勾选 **Connect API** → 展开勾上 **应用发布 (App release)**
3. 复制 `Client ID` 和 `Key`（Key 只显示一次）
4. 填入配置：

   ```yaml
   stores:
     huawei:
       client_id: "<Client ID>"
       client_secret: "<Key>"
   ```

#### 小米开放平台

小米的发布 API 使用 RSA 签名鉴权：每次请求用**小米给你的公钥**对包含**接口密钥**和文件 MD5 的 JSON 加密生成 SIG。所以需要拿三样东西：开发者账号邮箱、接口密钥、公钥证书。

> ⚠️ 旧版本 apkgo 内置了一个公钥证书，但那份证书 **2023-05-13 已过期**且来历不明，从这个版本开始必须由你提供自己账号的公钥证书。

1. 登录 [小米开放平台](https://dev.mi.com)
2. 进入控制台 → 右上角账号 → **账号管理** → 左侧菜单 **接口密钥** （或「Pub-Key」/「公私钥管理」，HyperOS 后台改版后入口位置略有差异）
3. 在该页面：
   - **接口密钥（Private Key）**：点 *查看私钥* 复制一串字符（重置会失效，复制后立即保存）—— 对应配置里的 `private_key`
   - **公钥证书**：点 *下载公钥* 拿到一个 `.cer` 文件 —— 对应配置里的 `cert_file`（或者用 `base64 -w0 xiaomi-pubkey.cer` 编码后填到 `cert`）
4. 上传 API 不是默认开通，要先在同页面或「权限申请」里申请 **应用上传/发布接口** 权限，审核通过后接口密钥才会出现可用
5. 填入 `apkgo.yaml`：

   ```yaml
   stores:
     xiaomi:
       email: "<开发者账号邮箱>"
       private_key: "<接口密钥>"
       cert_file: "/secure/path/xiaomi-pubkey.cer"
       # 或 cert: "-----BEGIN CERTIFICATE-----..."
       # 或 cert: "<base64(.cer 文件)>"
   ```

6. 验证：

   ```bash
   apkgo doctor -s xiaomi -p com.example.app
   ```

   两项探针：`cert`（公钥可加载、RSA、未过期）、`query`（接口密钥/邮箱/公钥三者匹配，能调通 `/dev/query`）。两项都 ✓ 才说明真实上传会通过鉴权这一关。

   ⚠️ 上传时小米后台还会做**签名一致性检查**：你要发的 APK 必须用跟线上版本相同的 keystore 签名，否则会以 `签名不一致,不满足应用更新条件` 拒绝。这是后台层的反劫持机制，apkgo 无法绕过。

#### OPPO 开放平台

OPPO 用 OAuth2 拿 access_token + 每次请求 HMAC-SHA256 签名。需要 `client_id`（19 位数字）和 `client_secret`。

1. 登录 [OPPO 开放平台](https://open.oppomobile.com)
2. 进入控制台 → 右上角账号 → **管理中心** → 左侧 **API 密钥管理**（HeyTap 改版后入口可能叫「API 接入」/「开放接口」）
3. 新建 / 查看 API 密钥：
   - **Client ID**：19 位数字串
   - **Client Secret**：长字符串（重置会让旧 secret 失效）
4. 上传发布 API 不是默认开通，需要先在「权限申请」里申请相应权限并完成实名 / 主体认证
5. 填入 `apkgo.yaml`：

   ```yaml
   stores:
     oppo:
       client_id: "<19 位数字>"
       client_secret: "<密钥>"
   ```

6. 验证：

   ```bash
   apkgo doctor -s oppo -p com.example.app
   ```

   两项探针：`token`（凭证能换到 access_token）、`app-info`（HMAC-SHA256 签名服务端能验过，且包名在你账号下存在）。

   ⚠️ OPPO 的发布是**异步任务**：`publish` 接口返回成功只代表任务已创建，apkgo 会继续轮询 `task-state` 等到任务终态（最长 5 分钟）。如果撞到 `911216 任务处理中` 表示前一次发版还没完成，apkgo 会自动跳过 publish 直接等任务；撞 `911215 应用审核中` 表示已成功送进 OPPO 审核队列，apkgo 视为成功返回。

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
    APKGO_HUAWEI_SERVICE_ACCOUNT: ${{ secrets.HUAWEI_SERVICE_ACCOUNT }}  # base64(JSON 凭证)
    APKGO_XIAOMI_EMAIL: ${{ secrets.XIAOMI_EMAIL }}
    APKGO_XIAOMI_PRIVATE_KEY: ${{ secrets.XIAOMI_PRIVATE_KEY }}
    APKGO_XIAOMI_CERT: ${{ secrets.XIAOMI_CERT }}             # base64(.cer 文件)
    APKGO_OPPO_CLIENT_ID: ${{ secrets.OPPO_CLIENT_ID }}
    APKGO_OPPO_CLIENT_SECRET: ${{ secrets.OPPO_CLIENT_SECRET }}
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
