# 分阶段发布（灰度 / Phased / Staged Rollout）调研

> 调研日期：2026-06-12 ・ 关联 issue #10「目前好像还不支持分阶段发布」
> 状态：**仅调研，尚未实现**。本文记录各商店的支持情况与确切接口，供日后实现参考。

分阶段发布 = 新版本先向**一定比例**的用户灰度放量，逐步提升比例验证稳定性，再转全量。
与 apkgo 现有的"一次 `upload` 提交即结束"不同，分阶段发布是**有状态的持续过程**
（创建分阶段 → 查询进度 → 调整比例 / 暂停 / 恢复 → 转全量），见文末「对 apkgo 的实现考量」。

## TL;DR

| 商店 | API 支持 | 形态 |
|---|---|---|
| 华为 huawei | ✅ | 提交时 `releaseType=3` + 分阶段字段；独立接口控制放量/暂停 |
| 荣耀 honor | ✅ | `submit-audit releaseType=3` + 查询/控制接口 |
| vivo | ✅ | `app.sync.create.update.stage.app` + 详情查询 |
| 三星 samsung | ✅ | `PUT /seller/v2/content/stagedRolloutRate` |
| Google Play | ✅ | track release `status=inProgress` + `userFraction` |
| OPPO oppo | ❌ 仅后台 | 管理中心 UI，API 传包无阶段参数 |
| 小米 xiaomi | ❌ 仅后台 | `/dev/push` 无比例字段 |
| 腾讯应用宝 tencent | ❌ 无 API | `update_app` 无灰度参数 |
| 蒲公英 / fir | ➖ 不适用 | 内测分发，无灰度概念 |

**5 家可通过 API 做分阶段（华为/荣耀/vivo/三星/Google Play），3 家仅后台或无（OPPO/小米/腾讯）。**

---

## 支持 API 的商店

### 华为 huawei ✅

- **发起**：提交发布接口 `PUT /api/publish/v2/app-submit?appId=&releaseType=3`（`releaseType=3` 即分阶段）。
- **Body**（`releaseType=3` 时必填）：
  | 字段 | 类型 | 说明 |
  |---|---|---|
  | `phasedReleaseStartTime` | String(64) | 分阶段生效开始时间，UTC：`yyyy-MM-ddTHH:mm:ssZZ`（如 `2026-01-01T01:01:01+0800`） |
  | `phasedReleaseEndTime` | String(64) | 分阶段生效结束时间 |
  | `phasedReleasePercent` | String(10) | 分阶段百分比，0.00–100.00 |
  | `phasedReleaseDescription` | String | 分阶段说明 |
  | `state` | String | 无需设置 |
- **控制**：独立接口「更新分阶段发布」可把状态改为 SUSPEND（暂停）/ RELEASE（暂停后恢复/放量）。
- **约束**：分阶段发布要求该应用**已存在一个全网在架版本**。

### 荣耀 honor ✅（与华为同源，机制类似）

- **发起**：`POST /openapi/v1/publish/submit-audit?appId=` Body `releaseType=3`（1全网/2指定时间/3分阶段）+ `phasedReleaseInfo`。
  - `phasedReleaseInfo`：`releasePercentage`(0.00–100.00)、`releaseStartDate`(北京时间) 等。
  - 约束：要求至少存在一个已全网发布的版本。
- **查询**：`POST /openapi/v1/publish/get-app-current-release?appId=`（Body 无）→ `PhasedReleaseInfo`：
  - `releaseStatus`：**1 审核通过待发布 / 2 分阶段发布中 / 3 分阶段发布已暂停 / 4 已全网发布**
  - `releasePercentage`(0.00–100.00)、`releaseStartDate`、`versionName`、`versionCode`
  - （注：`get-phased-release-info` 亦可，按发布流程查；上面这个按 appId 查最新）
- **控制**：`POST /openapi/v1/publish/update-phased-release-info?appId=` Body `operationType`：
  - **3 = 暂停分阶段发布**（发布中可暂停）
  - **0 = 重启分阶段发布**（已暂停可重启）
  - **5 = 取消分阶段发布**（待发布/发布中/已暂停可取消；取消后不可逆）

### vivo ✅

- **发起 / 调整**：method `app.sync.create.update.stage.app`（应用分阶段创建更新，doc 882）。
  | 字段 | 类型 | 必填 | 说明 |
  |---|---|---|---|
  | `packageName` | String | 是 | 包名 |
  | `subPackage` | Integer | 是 | 是否分包：1 是 / 0 否 |
  | `stagedStartTime` | String | 是 | 分阶段开始时间 `yyyy-MM-dd HH:mm:ss` |
  | `stagedEndTime` | String | 是 | 分阶段结束时间 |
  | `stagedProportion` | Integer | 是 | 分阶段比例（**1–99**） |
  | `apkUuid32` | String | 否 | 32 位 apk 流水号（分包且有 32 位包时必传） |
  | `apkUuid64` | String | 否 | 64 位 apk 流水号（非分包场景必传） |
  | …metadata | | | updateDesc / icon / screenshots 等 |
- **查询**：`分阶段详情获取`（doc 884）。
- **指南**：分阶段发布能力使用指南（doc 666）。
- 走 `https://developer-api.vivo.com.cn/router/rest` 的统一签名网关（HMAC-SHA256），同上传接口。

### 三星 samsung ✅

- **设置 / 更新比例**：`PUT /seller/v2/content/stagedRolloutRate`
  | 字段 | 类型 | 说明 |
  |---|---|---|
  | `contentId` | String | 12 位应用 ID |
  | `appStatus` | String | `REGISTRATION` 或 `SALE` |
  | `function` | String | `ENABLE_ROLLOUT` / `DISABLE_ROLLOUT` |
  | `rolloutRate` | Integer | 比例百分比（启用时必填；`SALE` 状态下必须大于上次设置值，即**只增不减**） |
  | `countries[].countryCode` / `countries[].rolloutRate` | | 可选，按国家设比例 |
- **查询**：View Staged Rollout Rate（GET 配套接口）。
- 另有 Update / View **Staged Rollout Binary** 管理灰度对应的二进制。

### Google Play ✅

- Android Publisher API，track release 对象：
  - `status`：`inProgress`（灰度中）/ `halted`（暂停）/ `completed`（转全量）/ `draft`
  - `userFraction`：**0.0–1.0**，灰度用户比例；逐步调高 `userFraction` 放量，最后 `status=completed` 转全量。
  - 通过 `PUT /edits/{editId}/tracks/{track}` 设置 `releases[].status` + `releases[].userFraction`。

---

## 仅后台 / 无 API 的商店

### OPPO oppo ❌（仅控制台）

- 分阶段发布是**管理后台操作**：管理中心 → 应用服务平台 → 移动应用列表 → 应用详情 → 版本升级。整篇文档（doc id=11546）描述的都是点按钮/弹窗操作，无任何 API。
- API 传包接口 `/resource/v1/app/upd` **没有阶段相关参数**；只在 `app/info` 响应里**只读返回**：
  - `release_type`：1 全量 / 2 分阶段 / 3 内部分阶段包
  - `release_status`：0 未设置 / 1 发布中 / 2 暂停 / 3 取消 / 4 阶段结束
  - `release_over_type`：1 分阶段结束转全量 / 2 分阶段结束下架
- 后台规则（供参考）：阶段一起始时间需 ≥ 当前 +24h；整个周期 ≤ 30 天；百分比 >0 且 <100（整数或小数，下一阶段须大于上一阶段）；最多 10 个阶段；到最后阶段结束自动转全量。

### 小米 xiaomi ❌（仅控制台）

- 唯一的发布 API `/dev/push` 的 `appInfo` **没有任何比例 / 阶段字段**（有 `onlineTime` 定时但无灰度）。
- 分阶段发布是小米开发者后台的功能，未开放 API。

### 腾讯应用宝 tencent ❌（API 未暴露）

- API 传包 `update_app` 参数里**没有灰度 / 分阶段字段**（pkg_name / app_id / deploy_type / deploy_time / apk flags / feature）。
- 官方 wiki（wikinew.open.qq.com）访问受限；应用宝控制台可能有灰度，但 API 传包未暴露。

### 蒲公英 / fir ➖

- 内测分发平台，上传即时对测试者可见，无灰度 / 分阶段概念。

---

## 对 apkgo 的实现考量（日后做 #10 时参考）

分阶段发布与 `--release-time`、URL 透传不同——它**不是一次性动作**，套不进现有"一条 `upload` 即结束"的模型。建议拆成两部分：

1. **upload 以分阶段方式提交**：给支持 API 的 5 家加类似 `--phased-percent <n>` + 时间窗口的选项（类比 `--release-time`）。注意各家模型不一：
   - 华为/荣耀：百分比 + 起止时间窗（一次提交一个百分比）
   - vivo：`stagedProportion` 1–99 + 起止时间
   - 三星：`rolloutRate` %（只增不减）
   - Google Play：`userFraction` 0–1
2. **独立的分阶段管理命令**（如 `apkgo rollout`）：查询进度 / 提升比例 / 暂停 / 恢复 / 转全量 / 取消——因为放量是后续多次操作（类比独立的 `apkgo audit`）。

共性约束：
- 多数商店要求**已有一个全网在架版本**才能分阶段（华为/荣耀/OPPO 明确）。
- 状态机大体一致：待发布 → 发布中 →（可暂停/恢复）→ 转全量 / 取消。
- 时区与时间格式沿用各店上传接口的既有约定（华为/荣耀带 UTC 偏移；vivo/OPPO 北京本地时间）。

> 各店确切端点 / 字段以本文为准；实现前建议用真实账号再核一遍（尤其 OPPO/小米/腾讯需确认是否有未公开的灰度 API）。
