# 我用一行命令把 APK 发布到了 11 个应用商店

做 Android 开发的人都知道，国内应用分发是一件多么痛苦的事。

华为、小米、OPPO、vivo、荣耀、应用宝，每个商店一套后台，每个后台一套登录方式，每次发版要手动上传 6 次，填 6 遍更新日志，点 6 次提交。如果再加上 Google Play、三星、内测平台……

**我受够了。**

所以我写了 [apkgo](https://github.com/KevinGong2013/apkgo) —— 一个 CLI 工具，一行命令上传 APK 到所有商店。

```bash
apkgo upload -f app-release.apk --notes "修复了若干问题"
```

就这样，11 个商店，并发上传，JSON 输出结果。

## 支持的商店

| 商店 | 认证方式 |
|------|---------|
| 华为 AppGallery | OAuth2 |
| 小米开放平台 | RSA 签名 |
| OPPO 开放平台 | HMAC-SHA256 |
| vivo 开放平台 | HMAC-SHA256 |
| 荣耀开发者平台 | OAuth2 |
| 腾讯应用宝 | HMAC-SHA256 |
| Google Play | Service Account |
| Samsung Galaxy Store | Service Account |
| 蒲公英 | API Key |
| fir.im | API Token |
| 自定义服务器 | HTTP |

没错，**国内有公开 API 的主流商店全覆盖了**。

## 3 分钟上手

```bash
# 安装
curl -fsSL https://github.com/KevinGong2013/apkgo/releases/latest/download/apkgo_Linux_x86_64.tar.gz | tar xz -C /usr/local/bin apkgo

# 生成配置（只选你需要的商店）
apkgo init --store huawei,xiaomi,oppo

# 填入各商店的 API 密钥
vim apkgo.yaml

# 上传！
apkgo upload -f app-release.apk --notes-file CHANGELOG.md
```

输出：

```json
{
  "apk": {"package": "com.example.app", "version_name": "2.1.0", "version_code": 42},
  "results": [
    {"store": "huawei", "success": true, "duration_ms": 18420},
    {"store": "xiaomi", "success": true, "duration_ms": 5230},
    {"store": "oppo", "success": true, "duration_ms": 12100}
  ]
}
```

## 为什么不用 Fastlane / Gradle 插件？

- **Fastlane** 主要覆盖 Google Play 和 App Store，国内商店基本没有
- **Gradle 插件** 只有华为有官方的，且只能在 Android 项目中使用
- **apkgo** 是独立二进制，不依赖任何构建系统，CI/CD 里一行就能跑

## 为 AI Agent 设计

这是 apkgo 最不一样的地方。从第一天起，它就是为**机器消费**设计的：

**结构化输出**：所有结果都是 JSON，stdout 给机器读，stderr 给人看。

**语义化退出码**：`0` 全部成功，`1` 部分失败，`2` 全部失败，`3` 输入错误。Agent 不需要解析文本就能判断结果。

**配置自发现**：
```bash
$ apkgo stores
{"stores": [{"name": "huawei", "console_url": "https://...", "fields": [{"key": "client_id", "required": true}, ...]}]}
```

Agent 可以自动发现每个商店需要什么配置，甚至用 browser-use 从控制台页面抓取凭证。

**零交互**：没有任何 prompt、确认框、进度条（在 stdout 里）。适合 CI/CD 和 AI Agent 无人值守执行。

已经发布了 [skills.sh 技能包](https://skills.sh)，支持 Claude Code、Cursor、Windsurf 等 40+ AI Agent：

```bash
npx skills add KevinGong2013/apkgo
```

## GitHub Actions 集成

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

密钥放在 GitHub Secrets，不需要配置文件。也支持加密同步：

```bash
# 加密导出配置
apkgo config export --out config.enc
# 另一台机器导入
apkgo config import config.enc
```

## 不只是 CLI

还有 Web GUI：

```bash
apkgo serve
# 打开 http://localhost:8080
```

拖拽 APK、勾选商店、点击上传。适合不想碰命令行的运营同学。

## 架构：加一个商店 = 一个文件

apkgo 用 Go 的 `init()` 自注册模式。新增一个商店只需要：

1. 创建 `pkg/store/<name>/<name>.go`
2. 实现 `store.Store` 接口（`Name()` + `Upload()`）
3. 在 `init()` 里调用 `store.Register()`

**主流程零改动。** 这也是为什么能这么快从 6 个商店扩展到 11 个。

## 其他亮点

- **自动升级**：`apkgo upgrade` 一键更新到最新版
- **本地历史**：`apkgo history` 查看上传记录，避免重复上传
- **更新提醒**：每 30 天自动检查新版本（可配置或关闭）
- **Dry-run**：`--dry-run` 只验证不上传

## 开源

MIT... 不，Apache 2.0 协议。完全开源，欢迎 PR。

- GitHub: [github.com/KevinGong2013/apkgo](https://github.com/KevinGong2013/apkgo)
- 官网: [apkgo.com.cn](https://apkgo.com.cn)

如果对你有帮助，给个 Star 吧。

---

*遇到问题？提 [Issue](https://github.com/KevinGong2013/apkgo/issues)。想加新商店？看看 `pkg/store/` 下任意一个文件，照着写就行。*
