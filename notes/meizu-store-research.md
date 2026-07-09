# 魅族（Flyme）应用商店渠道调研

> 调研日期：2026-07-09（issue [#37](https://github.com/KevinGong2013/apkgo/issues/37)）
> 结论：**魅族开放平台没有应用上传/更新的开放 API，无法做纯 API 接入，暂不支持**。

## TL;DR

- 魅族开放平台（open.flyme.cn，2023 年起由星纪魅族/DreamSmart 运营）的文档只覆盖：开发服务、流量分发、推送服务、商业变现、车机（Flyme Auto）。**不存在任何「应用上传 / 应用更新」API 文档**。
- 应用商店提交只能走网页控制台：控制台 → 应用管理 → 发布新应用 → 浏览器上传 APK → 填资料 → 提审（1–3 个工作日）。
- 带参数签名的魅族官方 HTTP API 只有 **推送**（github.com/MEIZUPUSH/PushAPI，`api-push.meizu.com`，MD5 签名）、账号 OAuth、支付，均与商店发布无关，容易被误认为有商店 API。
- 旧版开发者 wiki（open-wiki.flyme.cn）已下线，存档页面（「应用发布」「审核规范」等）也全部是控制台操作指引，从未有过发布 API。

## 逆向可行性（为什么不做模拟登录方案）

- 控制台是 Nuxt SPA，后端 `apiopen.flyme.cn`，cookie session 会话制的 `/api/dev/v1/...` 内部接口，未登录一律返回 `{"code":"100001","message":"用户未登陆"}`；商店发包接口未在公开 JS bundle 中暴露、无文档。
- 登录是 Flyme SSO（login.flyme.cn `/sso/unionlogin`）：密码走 JS/RSA 加密 + **极验滑块验证码** + 陌生环境**短信验证码**（有公开逆向实现：TRHX/Python3-Spider-Practice `JSReverse/login_flyme_cn/`）。验证码 + 短信意味着无人值守（CI/云端）根本跑不通。
- 没有任何 access_key/secret、token 端点之类的凭证模型可依托；接口随时可变，且违反平台条款。**结论：不做。**

## 生态旁证

- 多商店发布工具均未实现魅族：
  - BioforestChain/android-auto-distribute：`platforms/meizu/meizu.ts` 是**空壳 stub**，只用公开详情页做只读版本查询。
  - NicoleLab-io/app-store-publisher（华为/小米/OPPO/vivo/荣耀/App Store 六大市场）：无魅族。
  - fastlane / npm / PyPI：搜 meizu/flyme 均无发布插件。
- 上架攻略类文档一致确认魅族为人工控制台提交；个人开发者只能发工具类应用，其他分类需企业账号。

## 对 apkgo 的建议

1. **不实现 meizu store**（本次结论）。
2. 需要自动化的用户可用现有 **`script` 渠道**挂自己的浏览器自动化（Playwright 等），apkgo 会把 APK 路径、版本、发布说明以 JSON 从 stdin 传给脚本。
3. 可选的只读能力：公开详情页 `https://app.meizu.com/apps/public/detail?package_name=<pkg>` 可查当前线上版本（android-auto-distribute 的做法）。如果以后要给 `audit`/`doctor` 加个「线上版本对照」探针可以用它，但它不含审核状态，价值有限。
4. 若魅族日后开放发布 API（关注 open.flyme.cn 文档更新），按 `pkg/store/CLAUDE.md` 流程接入即可。

## 来源

- https://open.flyme.cn/ 及 `/docs`、`/service?type=application`
- https://github.com/MEIZUPUSH/PushAPI
- https://github.com/BioforestChain/android-auto-distribute（`routes/api/platforms/meizu/`）
- https://github.com/NicoleLab-io/app-store-publisher
- https://github.com/TRHX/Python3-Spider-Practice（`JSReverse/login_flyme_cn/`）
- https://www.yimenapp.com/kb-yimen/3334/（提交流程）
- web.archive.org 2022-11-15 的 open-wiki.flyme.cn 快照
