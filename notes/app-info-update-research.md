# 应用基本信息（图标 / 简介 / 截图）更新调研

> 调研日期：2026-07-07；2026-09-29 更新（补充魅族、鸿蒙，明确应用名策略，移除蒲公英/fir）；
> 2026-09-29 二次更新：**逐家拉取官方文档原文核实**，推翻了荣耀、腾讯「无 API」、三星「仅已上架可改」的旧结论，并纠正 vivo 误用海外平台接口。
> 状态：**仅调研，尚未实现**。apkgo 当前 `UploadRequest` 只携带 `AppName`/`PackageName`/`VersionCode`/`VersionName`（从 APK 本身解析）+ `ReleaseNotes`（发布说明），
> 不支持修改一句话介绍、长描述、icon、截图/宣传图。本文记录各商店在这方面的 API 支持情况，供日后实现参考。
>
> 范围：国内商店 + Google Play。**蒲公英、fir 不在范围内**（分发平台，icon/截图由后端从安装包自动提取，无资料概念）；`script` 为用户自定义脚本，不适用。
>
> **目标形态：只在上传新版本时同步更新资料，不提供单独更新资料的能力**（不做 `apkgo listing` 之类的独立命令，也不走「不发版改资料」的接口）。
> 因此下文关注的是：各家能否把资料修改**并入现有的版本更新流程、随新版本一起提审**。

## TL;DR

计划支持的资料项为 **一句话介绍 / 长描述 / icon / 截图**；**应用名不做修改，只做一致性检查**（见下文「应用名」一节）。

**结论：10 家商店的四项资料全部有 API 可改**，全部以官方文档为据（Google Play 除外，它的依据是 API 参考页）。按「随新版本一起改」的目标，**10 家都能并入现有上传流程**，其中小米「更新版本时传资料是否生效」待实测。

| 商店 | 一句话介绍（字段 / 上限） | 长描述 | icon | 截图 | 随新版本更新的接入点 |
|---|---|---|---|---|---|
| 华为 huawei | ✅ `briefInfo` ≤80 | ✅ `appDesc` ≤8000 | ✅ | ✅ | 传包后、`app-submit` 前调 v2 `app-language-info` + `app-file-info`（按语言） |
| 鸿蒙 harmony | ✅ `briefInfo` ≤80 | ✅ `appDesc` ≤8000 | ✅ | ✅ | 现有 v3 `app-language-info` 调用点加字段 + v3 `app-file-info`（按语言 × 设备类型），`app-submit` 前 |
| 荣耀 honor | ✅ `briefIntro` ≤80 | ✅ `intro` ≤8000 | ✅ fileType=1 | ✅ fileType=2/3 | 现有 `update-language-info` 调用点改字段；图片 `get-file-upload-url` + `update-file-info`；`submit-audit` 前 |
| vivo | ✅ `simpleDesc` 5–16字 | ✅ `detailDesc` 50–1000 | ✅ | ✅ 3–5 张 | 现有 `app.sync.update.app` / `app.update.app` 直接加字段（图片先传拿流水号） |
| OPPO | ✅ `summary` ≤13 | ✅ `detail_desc` ≥20 | ✅ | ✅ 竖版≥2 | 现有 `/app/upd` 已回传这些字段，替换为新值即可 |
| 小米 xiaomi | ✅ `brief` ≤34字符 | ✅ `desc` | ✅ | ✅ 3–5 张 | 现有 `/dev/push`（`synchroType=1`）带上 `desc`/`brief`/截图；**是否生效待实测** |
| 魅族 meizu | ✅ `recommendDesc`（推荐语） | ✅ `appDesc` 100–1000 | ✅ | ✅ | 现有 `publish` / `failapp/update` 的 `publishBody` 替换对应字段 |
| 三星 samsung | ✅ `shortDescription` ≤40B | ✅ `longDescription` ≤4000B | ✅ | ✅ 4–8 张 | 现有 `contentUpdate` 加字段（官方更新流程即在此步改资料） |
| 腾讯 tencent | ✅ `one_word_summary` 5–15字 | ✅ `introduce` 60–500字 | ✅ | ✅ 4–5 张 | 现有 `update_app` 加字段（不变不填） |
| Google Play | ✅ `shortDescription` ≤80 | ✅ `fullDescription` ≤4000 | ✅ | ✅ | 现有 edit 事务内 `edits.listings` + `edits.images`，commit 前 |

补充说明：

- **请求形态**：华为、鸿蒙、荣耀、vivo（可选字段，不传是否保留原值待实测）、腾讯、Google Play、三星（`"null"` 保留）按字段增量提交；OPPO `/app/upd`、魅族 `publish` 本来就是整套提交，apkgo 已从详情接口读回现有资料回填，只需覆盖用户指定的项。
- **不采用的接口**（只用于「不发版改资料」）：OPPO `/app/updm`、腾讯不传 APK 的 `update_app`、小米 `synchroType=2`、魅族 `saleapp/update`、华为/鸿蒙「同版本升级」。下文各商店详情保留这些信息仅作参考。
- **仍需实测**：vivo 可选字段不传时是否保留原值；小米 `synchroType=1` 时资料字段是否生效；魅族 `recommendDesc` 长度、icon/截图规格；三星中文按字节计的长度。

---

## 应用名：不改，只检查一致性

商店展示名在各家 API 里基本都是可改字段，但**国内实际上必须与以下几处保持一致**，审核会交叉比对，不一致是常见驳回理由：

1. APK 里的 `android:label`（安装后桌面显示名）；
2. 工信部 **APP 备案**名称（2023 年起强制备案，改名需走备案变更）；
3. 软著名称（多数商店上架/改名时会核对）。

各家 API 对改名的额外限制（官方文档）：

- **小米**：「修改应用信息不支持修改应用名称，如需修改应用名称，可在提交审核或者版本更新状态下修改」——即 `synchroType=2`（内容更新）不能改名，更新包时可以。
- **腾讯**：`app_name` 每个自然年最多改 2 次，改名需填 `modify_app_name_reason` 并重新上传软著。
- **OPPO**：`updm` 没有 `app_name` 参数，不能改名。
- **魅族**：审核规范要求名称 ≤10 个中文字符或 ≤20 个英文字符（含副标题）。
- **荣耀**：`appName` ≤15 个汉字或 ≤30 个其他字符。
- **vivo**：`mainTitle`/`subTitle` ≤20 字符，每年最多改 4 次。
- Google Play 是例外，标题（≤30字符）可与 APK 名不同。

正确的改名流程是：备案变更（及软著）→ 改 APK `android:label`（默认 `values/` 和 `values-zh/` 都要改）→ 改各商店展示名 → 随新版本提审。这不应是上传的副作用。

**apkgo 现状：上传从不改名。** 小米曾因按 APK 解析名回填，把国际化应用（默认 `values/` 为英文）的商店名改成英文（#48 → #50），修复后更新时一律沿用 `/dev/query` 返回的商店现有名称（`pkg/store/xiaomi/xiaomi.go` 的 `upload`），仅新建应用时用 APK 名；荣耀、OPPO、魅族也是把商店现有名称原样回传。

**建议**：不提供自由填写的 `--app-name`；在 `doctor` / `--dry-run` 中增加一致性检查，比对各商店现有名称与 APK 解析出的简体中文名，不一致时告警。若确实需要 API 改名，做成显式开关（「将商店名同步为 APK 简体中文名」），默认关闭。

---

## 各商店详情

### 华为 huawei ✅

`connect-api.cloud.huawei.com`，与 apkgo 现有 upload-url/app-file-info/app-info 同一套鉴权（service-account JWT / client_credentials + client_id），未发现独立权限 scope。

- **非本地化**：`PUT /api/publish/v2/app-info` —— apkgo 现在已经在用这个接口改 `newFeatures`（见 `pkg/store/huawei/huawei.go`），也支持 `defaultLang`、`privacyPolicy`、`isFree`/`price`/`priceDetail`、`publishCountry`、`childType`/`grandChildType`（分类）。
- **本地化**：`PUT /api/publish/v2/app-language-info` —— `lang`（必填）、`appName`（≤64字符）、`appDesc`（长描述，≤8000字符）、`briefInfo`（简介，≤80字符）、`newFeatures`（≤500字符）。
- **文件类（icon/截图/宣传图）**：走与 APK 相同的三步流程（`upload-url` → multipart 上传 → `PUT /api/publish/v2/app-file-info`）。`fileType` 枚举：`0`=icon，`1`=介绍视频+海报，`2`=截图，`3`=宣传视频+海报，`4`=推广/特色图，`5`=应用包（apkgo 现用），`6-16`=证书/VR素材。
  - icon：216×216px，PNG ≤500KB / WEBP ≤100KB，仅 1 张（附录「应用文件要求」→ Android应用，2026-03）。
  - 截图：以 **AGC 控制台**为准——手机 3–10 张，最低 1080×1920 且宽高比 9:16，PNG/JPG/JPEG ≤5MB/张。附录（2026-03）仍写竖 720×1280 / 横 1280×720、3–5 张，已落后于控制台。
  - ⚠️ 附录表格里「竖屏 450×800、≤2MB」是**应用介绍视频海报**的规格，不是截图，早先调研误读。
  - 推广图：PNG/JPG/JPEG/WebP，≤2MB。
- **本地化维度**：`app-language-info` 要求 `lang`；`app-file-info` 更新图片/视频类文件时**同样要求语言参数**。
- **生效方式**（官方帮助中心「更新应用信息」）：不换包做「同版本升级」，改资料后提交审核，通过才生效；已提交审核后要改资料需先「撤销审核」。
- **前提**：应用需已存在于 AGC（`appid-list` 解析 appId），无迹象表明可纯 API 创建新应用。

### 鸿蒙 harmony ✅（官方文档确认）

与华为共用 AGC Service Account（`huawei.CredentialFields` / `huawei.NewClient`），走 **v3** 接口（`pkg/store/harmony/harmony.go`）；apkgo 目前只调 `PUT /api/publish/v3/app-language-info` 写 `newFeatures`。

- **文字**：`PUT /api/publish/v3/app-language-info?appId=`（可选 `releaseType=1`、`releasePhase`），字段与 v2 一致：`appName`（≤64，新增语种时必填）、`appDesc`（≤8000）、`briefInfo`（≤80）、`newFeatures`（≤500）。三个描述字段的值**不能相同**。
- **icon / 截图**：`PUT /api/publish/v3/app-file-info?appId=`，结构与 v2 不同——**没有 fileType 枚举**，按素材类型分列表：`appIconList`、`screenShotList`（另有 `introVideoList` / `rcmdPicList` / `updateScreenShotList` 等）。每个列表为 `List<LangFileInfo>`：`{lang(必填), fileInfoList:[{deviceType, objectIdList, showType}]}`。
  - `deviceType`：4 手机、5 平板、8 智慧屏、12 智能手表、14 运动手表、19 PC/2in1；`showType`：0 竖屏、1 横屏。
  - 文件先经 `upload-url/for-obs` 上传拿 `objectId`（与现有传包流程相同）。
  - 规格：icon 仅 1 个，216×216 或 1024×1024，PNG ≤3MB / WEBP ≤100KB，**必须与软件包内图标一致**；手机截图 3–10 张，竖 1080×1920 / 横 1920×1080，PNG/JPG ≤5MB 或 WEBP ≤200KB；平板截图 3–10 张，1280×1920 / 1920×1280。
- **生效方式**：同华为，同版本升级 + 提审。
- **待实测**：纯 API 同版本升级能否不调 `app-package-info`、直接 `app-submit`。
- 旁证：GitHub `x007xyz/harmony-publish` 调用了这两个 v3 接口，icon 按多个 deviceType 各写一份。

### 荣耀 honor ✅（官方文档确认，推翻旧结论）

`https://appmarket-openapi-drcn.cloud.honor.com`，鉴权同 apkgo 现有 client_id/client_secret/app_id。

- **文字**：`POST /openapi/v1/publish/update-language-info` —— apkgo **已经在调用**这个接口改 `newFeature`（`pkg/store/honor/honor.go`）。字段上限：`appName` 15 个汉字或 30 个其他字符，`intro`（长描述，必填）≤8000，`briefIntro`（简介，选填）≤80，`newFeature` ≤500。
  - ⚠️ `setAll` 默认 1，**未列出的语言会被删除**；传 0 只更新列出的语言。
- **icon / 截图**：旧结论「无 API」是错的。`fileType` 在前一步 `get-file-upload-url`（或 `upload-by-url`）里填，编号与华为不同：
  - `1` = 图标，512×512，≤200KB，每种语言 1 张，须带 `languageId`；
  - `2` = 横向截图 1920×1080，`3` = 纵向截图 1080×1920，每组 3–5 张，单张 ≤5MB，横竖二选一，用 `order` 从 0 排序；
  - 另有 10/11/12 视频、33 头图、13–39 资质文件、100 = APK（apkgo 现用）。
  - 然后 `update-file-info` 按 `objectId` 绑定（可选 `languageId`、`order`），只传要改的文件，其余沿用上一版。
- **基础信息**：另有 `update-app-info` 可改分类、官网、客服、隐私政策等。
- **生效方式**：所有改动走 `submit-audit` 审核通过后生效。
- 来源：https://developer.honor.com/cn/doc/guides/101359（SPA，正文 JSON：`https://developer.honor.com/document/portal/tree/101359?platformNo=10001&lang=cn` → `data.documentInfo.text`）

### vivo ✅（官方文档确认，推翻旧结论）

国内网关 `https://developer-api.vivo.com.cn/router/rest`（apkgo 已用同一套 access_key/access_secret HMAC-SHA256 签名），method 风格。

- ⚠️ **旧笔记把 vivo 国内和海外平台混了**：`app.update.language.info` / `app.update.basic.info` / `app.update.language.materials` / `app.update.submit` 及错误码 A0305/A0306/A0307 都属于 **vivo 海外开发者平台**（developer.vivo.com，网关 `developer-api.vivo.com`）。国内文档中心全部页面中没有这些方法，apkgo 不应调用。
- **资料直接随版本更新提交**：`app.sync.update.app` / `app.sync.update.subpackage.app`（文件上传方式）和 `app.update.app` / `app.update.subpackage.app`（URL 方式）——apkgo 现用的四个版本更新接口——都**直接带可选资料字段**。传包介绍页原文：调更新接口时「把当次更新所有流水号和其他非文件更新信息一并提交」。国内**没有单独的提审接口，也没有单独的资料修改接口**。

  | 资料 | 文件上传方式（`sync.update`） | URL 方式（`update.app`） | 规格 |
  |---|---|---|---|
  | 一句话简介 | `simpleDesc` | `simpleDesc` | 5–16 个汉字（审核规范） |
  | 长描述 | `detailDesc` | `detailDesc` | 50–1000 字符（错误码 20017） |
  | icon | `icon`（`app.upload.icon` 返回的流水号） | `iconUrl` | PNG，正方形，256–512px，≤500KB |
  | 截图 | `screenshot`（流水号，逗号分隔） | `screenshotUrl` | 3–5 张（错误码 20009），竖图 1080×1920，jpg/png，≤2MB/张 |
  | 应用名 | `mainTitle` / `subTitle` | 同左 | ≤20 字符，**每年最多改 4 次**（错误码 12006） |

- **素材上传**：`app.upload.icon`、`app.upload.screenshot` 返回流水号（响应字段是小写 `serialnumber`）；应用审核中/待上架时也会报 12010/12022。URL 方式的截图须为公网 URL，只有本地文件时先上传拿流水号、改走文件上传方式。
- **状态前提 / 错误码**：12010 审核中不允许操作；12021/12022 待上架不允许更新；21003 应用资料正在审核中（后台单独改的资料还在审）；12004 应用修改审核中（URL 模式）；20020 icon 流水号错误；18007/18013 附件流水号错误。
- **apkgo 接入点**：`pkg/store/vivo/vivo.go` 的 `upload()`（构建 `updateReq` 处）和 `maybeURLPush()`；需要时先传 icon/截图拿流水号，再把字段追加进现有更新请求。`app.query.details` 返回 `icon`/`screenshot`/`detailDesc`/`simpleDesc` 可用于核对。
- **仍未确认**：`sync.update` 是否本身即进入审核（文档未写明；旁证是国内无单独 submit 接口、`app.query.details` status 有 2=待审核、存在 12010「审核中不允许」）；可选字段不传时是否保留原值（建议实测）。
- 来源：国内文档中心 https://dev.vivo.com.cn/documentCenter/doc/343（sync.update.app）、517（sync.update.subpackage.app）、356（update.app）、519（update.subpackage.app）、329（upload.icon）、331（upload.screenshot）、346（查询详细信息）、344（字典）、326（传包介绍）、12（审核规范）。

### OPPO ✅（官方文档确认）

`https://oop-openapi-cn.heytapmobi.com`，apkgo 现有 `client_id`/`client_secret` → `access_token` + HMAC-SHA256 签名同样适用，无新增权限。

- `/resource/v1/app/upd`（发布版本）：apkgo 上传时已在用，**会新增版本**，异步处理。
- `/resource/v1/app/updm`（更新资料）：官方原文「此接口用于更新资源相关信息，**不会新增版本**」，同步接口（文档建议等待 ≥10 秒）；成功响应 `{"errno":0,"data":{"success":true}}`。没有 `app_name`、分类、`online_type`、`apk_url` 参数。另有 `/app/multi-updm`（多包资料更新）。
- **是否审核**：`updm` 页面没明说，但应用详情接口的 `update_info_check`（1-审核中 / 0-不在审核中）表明资料更新走**单独一条审核**。
- **字段（upd 与 updm 一致）**：

  | 字段 | 必传 | 规格 |
  |---|---|---|
  | `pkg_name` / `version_code` | 是 | 定位应用 |
  | `summary` | 是 | 一句话简介，不能有标点和空格；updm 表格写 ≤15，但 updm 更新说明（2022-06-08）和 upd 都写 **≤13**，按 13 实现 |
  | `detail_desc` | 是 | ≥20 字 |
  | `update_desc` | 是 | ≥5 字 |
  | `privacy_source_url` | 是 | 隐私政策网址 |
  | `icon_url` | 是 | 512×512 PNG，<1MB |
  | `pic_url` | 是 | 竖版截图，逗号分隔，≥2 张（文案又写 3–5 张），1080×1920 JPG/PNG，≤1MB/张 |
  | `landscape_pic_url` | 否 | 横版 3–5 张，1915×1080 |
  | `video_url` / `video_url_material` | 否 | MP4 <30MB |
  | `test_desc` | 是 | ≤400 字符 |
  | `copyright_url` | 是 | 软件版权证明 |
  | `business_username` / `business_email` / `business_mobile` | 是 | 商务联系人 |

  必传项多，`updm` 实际是**整套资料提交**，需先查应用详情（doc 11004）再合并。
- **图片**：`/resource/v1/upload/get-upload-url` 拿上传地址 + 一次性 sign → multipart 上传（`type=photo`）拿 URL。
- **待实测**：审核中能否调 `updm`（文档未列状态前提）；建议调用前查 `audit_status` / `update_info_check`，`update_info_check=1` 时跳过。
- 来源：官方文档 SPA，后端 `POST https://open.oppomobile.com/oneoppoapi/doc/detail`（body `{"doc_id":N}`，无需登录）；10999 发布版本、11000 更新资料、11002 多包资料更新、11003 文件上传、11004 应用详情（浏览器：`https://open.oppomobile.com/new/developmentDoc/info?id=11000`）。

### 小米 xiaomi ✅（字段确认，更新时是否生效待实测）

全部走 apkgo 已用的 `/dev/push`（`https://api.developer.xiaomi.com/devupload`），鉴权（email + private_key + cert）无需改动。

- **synchroType**（官方）：`0`=新增，`1`=更新包，`2`=**内容更新**。`apk` 仅「新增和更新时必传」，推断 `synchroType=2` 可不传 APK 只改资料（文档无单独说明/示例，**需实测**）；`icon` 在所有类型下都必选（apkgo 已传，取自 APK）。`/dev/query` 返回的 `updateInfo`（是否允许应用信息更新）与 `updateVersion` 是两个独立开关，印证存在「只改信息」模式。
- **资料字段**：`appInfo.desc`、`appInfo.brief`、`appInfo.category`、`appInfo.keyWords`、`appInfo.privacyUrl`、`appInfo.updateDesc`；截图 `screenshot_1`..`screenshot_5`（1–3 在 `synchroType=0` 时必选，最多 5 张）+ 平板 `screenshot_pad_*`。
  - `brief`：≤17 个汉字 / 34 个字符，句末不加标点。
  - icon：PNG 512×512；截图单张 ≤5MB，竖 1080×1920 / 横 1920×1080（出自控制台前端提示，非 API 文档）；控制台要求 4–5 张，API 文档只要求 3 张必传，不一致。
- **生效方式**：编辑资料后与更新版本一样「提交审核 → 等待 1–3 个工作日」；**驳回状态下不能编辑资料**，只能走更新版本。
- **待实测**：`synchroType=1`（apkgo 现有路径）时传 desc/brief/截图是否生效（第三方 BioforestChain/android-auto-distribute 会一起传，但只证明接口接受）；`synchroType=2` 不带 APK 能否调通。
- 来源：https://dev.mi.com/distribute/doc/details?pId=1134 、pId=1248（SPA，正文 JSON：`https://dev.mi.com/uiueapi/doc/getArticle?aId=1134`）

### 魅族 meizu ✅（官方文档确认，部分规格缺失）

`https://developer.meizu.com`，apkgo 已接入（`pkg/store/meizu`），鉴权同现有 clientId/clientSecret → accessToken + SHA-256 签名头。详见 `notes/meizu-store-research.md`。

- **接口**：`app/publish`（新版本）/ `app/failapp/update`（审核不通过重提）/ `app/saleapp/update`（**上架应用修改**）。没有单独改资料的接口，每次都**全量提交**，所有字段必填（`failapp`/`saleapp` 另需 `verisonId`，官方拼写）。
  - `saleapp/update` 的 `packageUrl` 必填，但 APK 未变时填 `app/detail` 原值即可，**不需重新上传**。
  - 提交后重新审核：`replaceStatus` 0 =「apk同版本替换，待审核」、1 =「内容信息修改，待审核」；只支持上架版本，审核 1–2 天通过后才对外显示。
  - 错误码：113042 非审核不通过应用不允许提交；113043 非上架应用不允许提交；113044 上架应用修改失败；113045 审核不通过应用修改失败。
- **字段**：`appName`、`appDesc`（审核规范：100–1000 字符，仅中英文，不含特殊字符/极限用语）、`recommendDesc`（文档只写「推荐语」，**长度未给**）、`keyword`（空格分隔）、`verDesc`、`icon`、`screenShots`（List）、`certificates`（List；`app/detail` 返回的是逗号分隔 String）等。
- **素材上传**：`POST /open/api/v1/app/image/upload`（multipart，只有 `file` 参数）返回 `value.destFileName`；只接受 JPG/PNG/JPEG（错误码 113007）。**icon/截图尺寸、大小、张数文档均未给出**，审核规范只要求图标与安装后一致、无拉伸/模糊/黑白边、截图不能带其他品牌手机外观。
- **apkgo 现状**：`appDetail.publishBody` 从 `app/detail` 读现有资料原样回填，只替换 `packageUrl` 和 `verDesc`——资料字段已在请求体里，改成可配置成本很低。
- 来源：`https://apiopen.flyme.cn/api/web/v1/doc-wiki/detail?id=333`（API 文档 v1.07，2026-01-05）、id=110 审核规范、id=14 应用发布与管理、id=27 常见问题。

### 三星 samsung ✅（官方文档确认，推翻「仅已上架可改」）

`https://devapi.samsungapps.com`，鉴权同现有 Bearer JWT + service_account_id + content_id 方案。

- **流程**：官方「Submit an Update」指南为 `contentInfo` → `contentUpdate` → `POST /seller/v2/content/binary` → `contentSubmit`，并写明应在 `contentUpdate` 这一步完成所需资料修改——**资料与新二进制在同一更新版本里一起提审**。apkgo `pkg/store/samsung/samsung.go` 已是这个流程，只是 `contentUpdate` 只回传必填字段，加上资料字段即可。
  - 旧笔记的「仅 FOR SALE 可调」是误读：原文指应用须已提交并上架过（有在架版本），不是「审核中不能随上传改资料」。
  - 调用后应用进入 REGISTERING（后台显示 Updating）；`contentSubmit` 要求 REGISTERING（错误码 3201）。`publicationType=03`（手动发布）时审核通过后还需 `contentStatusUpdate`。
  - 2026-07 起 `contentUpdate` 不再接受 `binaryList`。
- **请求规则**：必填 `contentId`、`defaultLanguageCode`、`paid`、`publicationType`；`screenshots` / `addLanguage` / `sellCountryList` 传字符串 `"null"` 保持原样、传 `[]` 清空。
- **字段**：`appTitle` ≤100B、`shortDescription` ≤40B、`longDescription` ≤4000B（分发到多国时须英文）、`newFeature` ≤4000B、`iconKey`（PNG 512×512 ≤1024KB）、`heroImageKey`（1200×675，游戏类）。
- **screenshots[]**：`screenshotKey`（保留原图填 null，替换填新 fileKey）、`reuseYn`（更新截图时必填，true 沿用 / false 替换）、`screenshotPath`（仅响应）；删除即从数组去掉该项。JPG/PNG，320–3840px，宽高比 ≤2:1，**4–8 张**（<4 报 3001；替换未带 key 报 4125）。
- **多语言**：`addLanguage[]` 每项 `languagecode`（c 小写）、`appTitle`、`description` 必填，`newFeature` 可选，各带 `screenshots[]`。
- **图片上传**：`POST /seller/createUploadSessionId`（24h 有效）→ multipart `POST https://seller.samsungapps.com/galaxyapi/fileUpload`（`file` + `sessionId`）→ `fileKey`。
- **待实测**：字节长度是否按 UTF-8 计（中文 ~3 字节）；REGISTERING 状态下能否再次 `contentUpdate`；`addLanguage` 中 `appTitle` 上限。
- 来源：developer.samsung.com/galaxy-store/galaxy-store-developer-api/content-publish-api/ 下 `modify-app-data.html`、`reference.html`、`user-guide-submit-an-update.html`、`submit-app.html`、`create-session-id.html`、`file-upload.html`、`failure-response-codes.html`。

### 腾讯应用宝 tencent ✅（官方文档确认，推翻「完全无 API」）

`https://p.open.qq.com/open_file/developer_api`，共 4 个接口：`get_file_upload_info`、`update_app`、`query_app_detail`、`query_app_update_status`。

- `update_app` 官方原文「也可仅更新基础信息，而不更新文件信息」——**可以只改资料不传 APK**，每天最多 50 次。所有字段「不变更则不填」：
  - `one_word_summary`：一句话简介，5–15 字（错误码 4000022）；
  - `introduce`：简介，60–500 字（错误码 4000021）；
  - `icon_file_serial_number`：512×512 PNG 直角图标，≤200KB；
  - `snapshots_file_serial_number`：截图 4–5 张，流水号用 `|` 分隔，建议 1080×1920，所有图片宽高一致，≤1MB/张；
  - `app_name`：每自然年最多改 2 次，需 `modify_app_name_reason` 并重新上传软著；
  - 另可改分类、运营方、年龄分级、资质文件。
- **图片**：`get_file_upload_info` 以 `file_type=img` 取流水号（每天最多 100 次）。
- 旧结论依据的「第三方运营指南只描述控制台操作」只说明控制台能改，不代表无 API。
- 来源：https://wikinew.open.qq.com/index.html#/iwiki/4015262492

### Google Play ✅

Android Publisher API v3，与现有 `androidpublisher.googleapis.com` 上传流程同一个 edits 事务。

- **标题/简介**：`edits.listings.update`（`PUT .../edits/{editId}/listings/{language}`），`Listing` 资源：`language`（BCP-47）、`title`、`fullDescription`、`shortDescription`、`video`。每个语言需单独 PUT 一次，无批量多语言接口。已知实际限制（文档未写，来自经验）：title ≤30字符，shortDescription ≤80，fullDescription ≤4000。
- **截图/图标/宣传图**：`edits.images`（`upload`/`list`/`delete`/`deleteall`），`.../listings/{language}/{imageType}[/{imageId}]`。`imageType` 枚举：`phoneScreenshots`、`sevenInchScreenshots`、`tenInchScreenshots`、`tvScreenshots`、`wearScreenshots`、`icon`、`featureGraphic`、`tvBanner`（**没有** `promoGraphic`，已从 Play 移除）。
- **icon 说明**：这是 Play 商店列表用的"高清 icon"（512×512 PNG），与 APK 内嵌运行时图标是两个东西。fastlane `supply` 生产环境同款用法。已知坑：若该 listing 从未设置过 icon 或刚被删除，上传会失败（fastlane#20359）。
- 具体像素/格式规格需查 Play Console 帮助中心而非 API 参考页。

---

## 文档抓取经验

几家文档站都是 SPA，直接 WebFetch 只拿到壳，但正文都有**免登录的后端 JSON 接口**。旧结论「无 API」多数是因为只看了渲染壳：

| 商店 | 正文数据接口 |
|---|---|
| 华为 / 鸿蒙 | `POST https://svc-drcn.developer.huawei.com/community/servlet/consumer/cn/documentPortal/getDocumentById`，body `{"objectId":"<文档ID>","catalogName":"app","language":"cn"}` |
| 荣耀 | `GET https://developer.honor.com/document/portal/tree/<id>?platformNo=10001&lang=cn` → `data.documentInfo.text` |
| OPPO | `POST https://open.oppomobile.com/oneoppoapi/doc/detail`，body `{"doc_id":N}` |
| 小米 | `GET https://dev.mi.com/uiueapi/doc/getArticle?aId=<pId>` |
| vivo（国内） | `GET https://dev.vivo.com.cn/webapi/doc/info?id=<id>` → `data.content`（目录：`/webapi/doc/tree`） |
| 魅族 | `GET https://apiopen.flyme.cn/api/web/v1/doc-wiki/detail?id=<id>`（目录：`.../doc-wiki/directory`） |
| 腾讯 | wikinew.open.qq.com 需浏览器渲染后读取 |

---

## 对 apkgo 的实现考量

与分阶段发布调研（`notes/phased-release-research.md`）不同，这里大部分是**一次性字段更新**，比较适合直接扩展现有 `UploadRequest`：

1. **范围**：一句话介绍 / 长描述 / icon / 截图四项；应用名只做一致性检查不修改（见上文）。
2. **优先做**（现有上传调用点已在请求里带着资料字段，改成可配置成本最低）：鸿蒙 / 荣耀（`language-info` 调用点）、OPPO（`/app/upd`）、魅族（`publishBody`）、三星（`contentUpdate`）、腾讯（`update_app`）、vivo（`sync.update` / `update.app`）；其次华为 / Google Play（需新增调用，但在同一流程内）；小米待实测。
3. **尽力而为，逐项上报**：每家在结果里分别报告四项的处理情况，失败或不适用的项明确报出，不静默跳过，也不因此把整次上传判为失败。
4. **只随新版本更新，不单独改资料**：资料修改一律放在传包之后、提审之前，随新版本一起审核；不提供独立命令，也不使用 OPPO `updm`、小米 `synchroType=2`、魅族 `saleapp/update` 等「不发版改资料」接口。未指定资料参数时行为与现在完全一致。
5. **全量提交的商店**（OPPO `/app/upd`、魅族 `publish`）：沿用现有「读回现有资料再回填」的做法，只覆盖用户指定的项；荣耀 `update-language-info` 要传 `setAll=0`，否则未列出的语言会被删。
6. **按语言/地区维度**：huawei、harmony、honor、samsung、Google Play 的资料都**按语言**设置（鸿蒙还按设备类型）——输入形态需考虑多语言（例如只支持默认语言，或允许 `map[lang]info`）。
7. **长度/规格差异很大**，校验（最好加自动缩放）放在各 store 包内部，而不是 `pkg/store` 通用层：
   - 一句话介绍：OPPO ≤13（无标点空格）、腾讯 5–15 字、vivo 5–16 字、小米 ≤17 字、三星 ≤40 字节、华为/鸿蒙/荣耀/Google Play ≤80；
   - 长描述：魅族 100–1000、vivo 50–1000、腾讯 60–500、OPPO ≥20；
   - icon：华为/鸿蒙 216×216（鸿蒙也可 1024×1024），vivo 256–512 正方形，其余 512×512；荣耀/腾讯 ≤200KB、华为/vivo ≤500KB；
   - 截图张数：腾讯 4–5、三星 4–8、vivo/荣耀 3–5、华为/鸿蒙 3–10、OPPO 竖版 ≥2；尺寸多为 1080×1920（华为/鸿蒙为最低 1080×1920 且 9:16）。
8. **实现前先实测**：见 TL;DR「仍需实测」清单。
