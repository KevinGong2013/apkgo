# 魅族（Flyme）应用商店渠道调研

> 调研日期：2026-07-09（issue [#37](https://github.com/KevinGong2013/apkgo/issues/37)）
> 结论：**魅族开放平台 2025-12 新上线了应用发布开放 API，已接入（`pkg/store/meizu`）**。
>
> ⚠️ 初版调研曾误判「无 API、不支持」——当时依据的生态旁证（多商店工具、
> 旧 wiki 存档、fastlane/npm/PyPI 搜索）全部早于 2025-12，未覆盖新文档。
> 教训：判断「平台没有某能力」时必须以官方文档站的实时抓取为准，
> SPA 页面要打后端数据接口（`open.flyme.cn` 正文在
> `apiopen.flyme.cn/api/web/v1/doc-wiki/detail?id=<id>`），不能只看渲染壳。

## API 概览（open.flyme.cn/docs?id=333，2025-12-25 发布）

- 域名：`developer.meizu.com`，响应统一 `{code, msg/message, value}` 信封，`code==200` 为成功。
  实测错误信封用的是 `message` 字段且 code 为整数（`{"code":113002,"message":"invalid param-..."}`）。
- **鉴权**：开发者中心创建「客户端凭证」得 clientId/clientSecret。
  - `GET /open/api/v1/token`，clientId/clientSecret 放**请求头**，返回 `value.accessToken`（带过期时间 `exprireTime`）。
- **签名请求头**（除 token 外所有接口）：`traceId`(UUID)、`clientId`、`timestamp`(毫秒，15 分钟内有效)、`accessToken`、`sign`。
  - `sign` = SHA-256 hex of（`traceId/clientId/timestamp/uri` 四项按 key 排序的 `k=v` 用 `&` 连接 + `":"+clientSecret`）。uri 参与签名但 query 参数不参与。
- **接口**：
  - `GET /open/api/v1/app/cats`、`/app/cat_tags` — 分类/标签
  - `POST /open/api/v1/app/image/upload`、`/app/apk/upload` — multipart 上传，返回 `value.destFileName`
  - `POST /open/api/v1/app/publish` — 新版本发布（JSON），返回 `value.verId`
  - `POST /open/api/v1/app/failapp/update` — 审核不通过版本重新提交（publish 全参数 + `verId`）
  - `POST /open/api/v1/app/saleapp/update` — 上架应用原地修改（同上）
  - `GET /open/api/v1/app/list`（start/limit≤10 分页）、`/app/versions?appId`、`/app/detail?verId`
- **应用状态**（3.12.6）：20 待审核 / 30 审核不通过 / 50 上架 / 70 下架 / 100 审核中。
- publish 参数为全量元数据（应用名/描述/分类/截图/资质/ICP 备案主体信息 dwmc、zjlx 等），全部必填 →
  实现上从 `app/detail` 读现有资料原样回填，只换 `packageUrl` 和 `verDesc`（发布说明）。
  注意 detail 返回的 `certificates` 是逗号分隔字符串，publish 要求 List；`qualifcation` 是官方拼写（少个 i）。

## 实现要点（pkg/store/meizu）

- 流程：`app/list` 按包名找应用 → `app/detail` 回填元数据 → `apk/upload` → 最新版本状态为 30（审核不通过）走 `failapp/update`，否则 `publish`。返回的 `verId` 存入 `UploadResult.ExternalID`。
- 首次上架（资质、备案、截图）仍需控制台人工完成；API 只做版本更新。找不到包名时报错提示。
- 仅支持 64 位包（32 位报 113029/113030），split-arch 上传取 `--file64`。
- 无定时发布、无 URL 拉包、不支持 AAB。
- audit：`app/list` 状态映射 20/100→reviewing、30→rejected、50→approved、70→withdrawn。
- doctor：token 探针 + app-list 包名探针。
- `developer.meizu.com` 从境外握手可超过 Go 默认 10s TLS 超时，client 已放宽到 60s。

## 待验证（需要真实凭证）

- 全流程实测：目前只用假凭证打通了 token 端点（`[113002] invalid param-...`），
  upload/publish/failapp 分支未经真实账号验证。
- token 过期（113036）后是否需要自动刷新——当前实现每次 `New()` 取新 token，单次上传内不刷新。
- 「审核中」状态下重复提交的确切报错（推测 113040）。

## 来源

- 官方文档：https://open.flyme.cn/docs?id=333（正文数据接口：https://apiopen.flyme.cn/api/web/v1/doc-wiki/detail?id=333）
- 实测：`GET https://developer.meizu.com/open/api/v1/token` → `{"code":113002,"message":"invalid param-clientId or clientSecret"}`
