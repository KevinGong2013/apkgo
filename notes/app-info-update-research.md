# 应用基本信息（图标 / 简介 / 截图）更新调研

> 调研日期：2026-07-07
> 状态：**仅调研，尚未实现**。apkgo 当前 `UploadRequest` 只携带 `AppName`/`PackageName`/`VersionCode`/`VersionName`（从 APK 本身解析）+ `ReleaseNotes`（发布说明），
> 不支持修改应用名称、icon、简介（短/长描述）、截图/宣传图。本文记录各商店在这方面的 API 支持情况，供日后实现参考。

## TL;DR

| 商店 | 应用名 | 简介/描述 | icon | 截图/宣传图 | 形态 |
|---|---|---|---|---|---|
| vivo | ✅ | ✅ | ✅ | ✅ | 素材先上传拿 `serialNumber`，再填入 `app.update.language.info` |
| OPPO | ➖（未见字段） | ✅ | ✅ | ✅ | `/resource/v1/app/updm`：**不产生新版本**的纯资料更新接口 |
| Google Play | ✅ | ✅ | ✅ | ✅ | `edits.listings` + `edits.images`，与 fastlane supply 同款 |
| 华为 huawei | ✅ | ✅ | ✅ | ✅ | `app-language-info`（按语言）+ `app-file-info`（按语言，icon/截图/宣传图） |
| 小米 xiaomi | ❌ 名称不可改 | ✅ | ✅ | ✅ | 同一个 `/dev/push`，`appInfo.desc`/`brief` + 截图 multipart 字段 |
| 三星 samsung | ✅ | ✅ | ✅ | ✅ | `contentUpdate`，但**仅限应用已 FOR SALE（已上架）状态**才能调 |
| 荣耀 honor | ✅ | ✅ | ❌ 无 API | ❌ 无 API | 仅 `update-language-info` 能改名称/简介，icon/截图只能控制台换 |
| 腾讯应用宝 tencent | ❌ | ❌ | ❌ | ❌ | `update_app` 只有包信息+发布说明，无任何资料字段；官方指南证实为控制台专属 |
| 蒲公英 pgyer | ➖ | ➖（有未用字段） | ❌ 只读 | ❌ 只读 | icon/截图由后端从 APK 自动提取，只读返回；有个 `buildDescription` 字段 apkgo 未使用 |
| fir.im | ➖ | ❌ | ❌ 只读 | ❌ | 上传接口只有 changelog，无任何资料字段 |

**4 家可以一站式改全部四项（vivo / OPPO / Google Play / 华为），小米/三星部分受限，荣耀只能改名称+简介，腾讯/蒲公英/fir 基本没有 API。**

---

## 全量支持的商店

### vivo ✅

统一签名网关 `https://developer-api.vivo.com.cn/router/rest`（apkgo 已用同一套 access_key/access_secret HMAC-SHA256 签名），method 风格。

- **非本地化字段**：`app.update.basic.info` —— `categoryId`（分类）、`copyRights`（资质证书）、`website`、`email`、`phone`、`iarc`（内容分级）、`privacyStatement`。不含应用名/icon。
- **本地化字段（应用名/icon/截图/简介真正设置的地方）**：`app.update.language.info` —— `appName`（≤50字符）、`icon`（素材 serialNumber）、`gifIcon`、`screenshots`、`description`（≤4000字符）、`updateDesc`（必填，≤500字符）、`recommendDesc`。
- 相关：`app.update.language.materials`（提审后的资料修改）、`app.delete.language.info`。
- **素材上传（先拿 serialNumber 再引用）**：
  - `app.upload.icon`：PNG/WEBP，512×512px，≤1MB。
  - `app.upload.gif.icon`：GIF，512×512px，≤1MB。
  - `app.upload.screenshot`：PNG/JPEG/JPG/WEBP，边长 320–3840px，**每个应用需 4–8 张**。
  - `app.upload.image`：通用图片（资质证书等）。
- **约束**：应用处于审核中/待发布/测试中（错误码 `A0305`/`A0306`/`A0307`）时不能改；改完需另调 `app.update.submit` / `app.sub.update.submit` 才会真正进入审核。`app.detail` 可读取当前 icon/简介/截图用于核对。

### OPPO ✅

`https://oop-openapi-cn.heytapmobi.com`，apkgo 现有 `client_id`/`client_secret` → `access_token` + HMAC-SHA256 签名同样适用，无新增权限。

- `/resource/v1/app/upd`（POST，form-urlencoded）：apkgo 上传时已在用的同一接口，也接受 `icon_url`、`summary`（简介，约13–15字符）、`detail_desc`（详细描述，≥20字符）、`second_category_id`/`third_category_id`。**会创建新版本并重新进入审核**。
- `/resource/v1/app/updm`（POST，form-urlencoded）：**"更新资料"，不新增版本**，仍需 `pkg_name`+`version_code`，但无需 `apk_url`。字段同上，另需 `update_desc`、`privacy_source_url`、`test_desc`、`copyright_url`、`business_*`、`customer_contact`。
- **截图/宣传图**：`pic_url`（竖版，2–5张，1080×1920，JPG/PNG，<1MB/张，逗号分隔）+ 可选 `landscape_pic_url`（横版，3–5张，1915×1080，同规格）。走 apkgo 现有的通用文件上传流程（`get-upload-url` → multipart POST，`type=photo`）拿 URL 后传入。
- **前提**：应用必须已存在于 OPPO 控制台（`pkg_name`/`version_code` 定位），apkgo 现有 `queryApp` 已在做同样的存在性检查。
- **caveat**：官方文档为 JS 渲染页面无法直接抓取，字段以第三方镜像（yimenapp.com）交叉验证，且与 apkgo 现有 `oppo.go` 里硬编码的 `appData` 结构完全一致，可信度高但非 100% 确认（尤其 `updm` 的必填字段全集）。

### Google Play ✅

Android Publisher API v3，与现有 `androidpublisher.googleapis.com` 上传流程同一个 edits 事务。

- **标题/简介**：`edits.listings.update`（`PUT .../edits/{editId}/listings/{language}`），`Listing` 资源：`language`（BCP-47）、`title`、`fullDescription`、`shortDescription`、`video`。每个语言需单独 PUT 一次，无批量多语言接口。已知实际限制（文档未写，来自经验）：title ≤30字符，shortDescription ≤80，fullDescription ≤4000。
- **截图/图标/宣传图**：`edits.images`（`upload`/`list`/`delete`/`deleteall`），`.../listings/{language}/{imageType}[/{imageId}]`。`imageType` 枚举：`phoneScreenshots`、`sevenInchScreenshots`、`tenInchScreenshots`、`tvScreenshots`、`wearScreenshots`、`icon`、`featureGraphic`、`tvBanner`（**没有** `promoGraphic`，已从 Play 移除）。
- **icon 说明**：确认可通过 API 更新——这是 Play 商店列表用的"高清 icon"（传统 512×512 PNG），与 APK 里内嵌运行时图标是两个东西，改它不影响 APK 本身图标。fastlane `supply` 生产环境同款用法。已知坑：若该 listing 从未设置过 icon 或刚被删除，上传会失败（fastlane#20359）。
- 具体像素/格式规格文档没写全，需查 Play Console 帮助中心而非 API 参考页。

### 华为 huawei ✅

`connect-api.cloud.huawei.com`，与 apkgo 现有 upload-url/app-file-info/app-info 同一套鉴权（service-account JWT / client_credentials + client_id），未发现独立权限 scope。

- **非本地化**：`PUT /api/publish/v2/app-info` —— apkgo 现在已经在用这个接口改 `newFeatures`（见 `pkg/store/huawei/huawei.go:251-272`），也支持 `defaultLang`、`privacyPolicy`、`isFree`/`price`/`priceDetail`、`publishCountry`、`childType`/`grandChildType`（分类）。
- **本地化**：`PUT /api/publish/v2/app-language-info` —— `lang`（必填）、`appName`（≤64字符）、`appDesc`（长描述，≤8000字符）、`briefInfo`（简介，≤80字符）、`newFeatures`（≤500字符）。
- **文件类（icon/截图/宣传图）**：走与 APK 相同的三步流程（`upload-url` → multipart 上传 → `PUT /api/publish/v2/app-file-info`）。`fileType` 枚举：`0`=icon，`1`=介绍视频+海报，`2`=截图，`3`=宣传视频+海报，`4`=推广/特色图，`5`=应用包（apkgo 现用），`6-16`=证书/VR素材。
  - icon：216×216px PNG（限制 ≤2MB 或 ≤500KB，两份文档不一致，取严格值），仅 1 张。
  - 截图：450×800（竖）或 800×450（横），JPG/JPEG/PNG，手机端 3–5 张，≤2MB/张。
  - 推广图：PNG/JPG/JPEG/WebP，≤2MB。
- **本地化维度**：`app-language-info` 要求 `lang`；`app-file-info` 更新图片/视频类文件时**同样要求语言参数**——即 icon/截图上传也要按语言分别提交，设备类型（手机/手表/Vision 等）不同也有不同图片规格。
- **前提**：应用需已存在于 AGC（`appid-list` 解析 appId），无迹象表明可纯 API 创建新应用。

---

## 部分支持的商店

### 小米 xiaomi ⚠️

全部走 apkgo 已用的同一个 `/dev/push`（`https://api.developer.xiaomi.com/devupload`），鉴权（email + private_key + cert）无需改动。

- 可改：`appInfo.desc`（应用介绍）、`appInfo.brief`（一句话简介）、`appInfo.category`、`appInfo.keyWords`、`appInfo.privacyUrl`、`appInfo.updateDesc`；`icon` 为必传 multipart 字段（apkgo 已传，取自 APK）；截图 `screenshot_1`..`screenshot_4`（手机）+ `screenshot_pad_1`..`screenshot_pad_5`（平板，需 `suitableType` 为1或2）。
- **不可改：应用名称**——官方文档（pId=1248《应用更新、修改操作指南》）明确写"修改应用信息不支持修改应用名称"。
- **未证实的点**：`category`/`keyWords`/`desc`/`brief`/截图字段文档标注"新增(`synchroType=0`)时必选"，但没写清楚在**更新**(`synchroType=1`，apkgo 现有正常路径)时是否同样生效——需要拿真实更新请求实测验证。
- 未发现 icon/截图的像素尺寸规格文档，仅有整体 2GB 上传大小限制。

### 三星 samsung ⚠️

`https://devapi.samsungapps.com`，鉴权同现有 Bearer JWT + service_account_id + content_id 方案。

- `POST /seller/contentUpdate`（"Modify App Data"）：`contentId`、`defaultLanguageCode`、`paid`、`publicationType` 必填；可改 `appTitle`（≤100字节）、`shortDescription`（≤40字节）、`longDescription`（≤4000字节）、`newFeature`、`iconKey`。用字符串 `"null"` 保留字段，`[]` 清空字段。
- **截图/图标**：先 `POST /seller/createUploadSessionId`（会话24h有效）拿上传通道，上传后拿 `fileKey`，再作为 `iconKey`/`screenshotKey`/`heroImageKey`/`edgescreenKey`/`edgescreenplusKey` 填入 `contentUpdate` 的 `screenshots[]`（`reuseYn` 可保留原有）。规格：icon 512×512 PNG ≤1024KB；heroImage 1200×675 JPG/PNG；edgescreen 160×2560 PNG；edgescreenplus 550×2560 PNG；截图 320–3840px（最大2:1），需 4–8 张。
- **多语言**：`supportedLanguages` + `addLanguage[]`（每项含 `languagecode`/`appTitle`/`description`/`newFeature`/`screenshots`），与三星按国家/语言的上架模型一致。
- **关键限制**：`contentUpdate` **只有应用已处于 FOR SALE（已上架）状态才能调**，审核中/被拒状态不可用——不能跟首次上传合并成一步，只能是独立的后续调用。
- 未在本次调研中确认独立的"分类"字段名，`screenshots[]` 子结构的精确字段名也未逐一核实，实现前需要拉原始参数表核对。

### 荣耀 honor ⚠️

`https://appmarket-openapi-drcn.cloud.honor.com`，鉴权同 apkgo 现有 client_id/client_secret/app_id。

- `POST /openapi/v1/publish/update-language-info`：apkgo **已经在调用**这个接口改 `newFeature`（`honor.go:661-700`），同一调用也能改 `appName`、`intro`（长描述/应用简介）、`briefIntro`（简介）——现在只是把这三个字段从 `get-app-detail` 原样传回避免被荣耀清空，改成可配置成本很低。
- **未见分类字段**——分类大概率只能控制台设置。
- **icon/截图：没有找到任何接口**。已知的荣耀 openapi 全部 8 个接口（get-app-id / get-app-detail / get-app-current-release / get-file-upload-url / update-file-info / update-language-info / submit-audit / get-audit-result）里，唯一文件类型常量只有 `fileTypeAPK=100`，没有 icon/截图专用类型。控制台搜索结果也只描述"点击更换图标"的手动操作。
- **caveat**：官方文档是 JS SPA，内部 AJAX 接口返回 500 无法绕过，结论基于 apkgo 自己引用的逆向客户端（github.com/Xigong93/XiaoZhuan）+控制台描述，不能 100% 排除未公开接口。

---

## 无 API / 仅控制台的商店

### 腾讯应用宝 tencent ❌

`https://p.open.qq.com/open_file/developer_api`。已确认的接口只有 `get_file_upload_info`（拿 COS 上传地址）、`update_app`（提交版本更新，参数仅 `pkg_name`/`app_id`/`deploy_type`/`deploy_time`/apk32/64标志+`feature`发布说明）、`query_app_detail`（只读，仅返回 `app_name`/`category`/`feature`）、`query_app_update_status`（只读审核状态）。**没有任何一个接口能改 icon/简介/截图**。第三方运营指南（腾讯开放平台"基础信息修改操作指南"）明确描述这是控制台操作：打开管理中心 → 应用详情页 → 编辑 → 保存 → 提审，全程无 API。官方 wiki（wikinew.open.qq.com）为 JS 渲染 SPA 抓取受限，不能 100% 排除未公开接口，但现有实现参数列表 + 独立第三方指南互相印证。

### 蒲公英 pgyer ➖

`getCOSToken` 上传接口里，除了 apkgo 已用的 `buildUpdateDescription`（版本更新说明）外，还有个 apkgo 未使用的 `buildDescription`（应用介绍/应用简介）字段——可以顺手加上。**icon 和截图没有任何设置参数**，接口响应里返回的 `buildIconUrl`/`buildScreenShots` 都是后端从 APK 二进制自动提取的只读值。

### fir.im ➖

`POST /apps` 拿 token → multipart 上传到七牛，字段仅 `key`/`token`/`x:name`/`x:version`/`x:build`/`x:release_type`（iOS专用）/`x:changelog`（发布说明，apkgo 已用）。**没有任何 description/icon/截图字段**，icon 同样自动从二进制提取；文档未写清楚描述/截图是否有控制台编辑入口，属于文档空白而非确认的"仅控制台"。

---

## 对 apkgo 的实现考量

与分阶段发布调研（`notes/phased-release-research.md`）不同，这里大部分是**一次性字段更新**，比较适合直接扩展现有 `UploadRequest`：

1. **优先做这 4 家**：vivo / OPPO / Google Play / 华为——鉴权机制已就位，只是加字段+加接口调用。华为和荣耀已经有 `updateAppInfo`/`update-language-info` 调用点，扩展成本最低。
2. **OPPO 的 `/updm`** 值得特别关注：是唯一一个"改资料不触发新版本审核"的接口，如果只是想改 icon/简介不想动版本号，这条路径更合适；其余商店改资料基本都会连带触发一次审核流程（尤其 huawei/vivo 需要显式提审）。
3. **按语言/地区维度**：huawei、honor、vivo、samsung、Google Play 的资料字段都是**按语言/地区**设置的，不是全局一份——`UploadRequest` 如果加这些字段，需要考虑多语言输入的形态（例如只支持默认语言，或允许传 `map[lang]info`）。
4. **小米应用名不可改**、**三星只能在已上架状态改**、**荣耀 icon/截图没有 API**——这些限制要在 CLI 报错/文档里明确说明，不能假装"全商店统一支持"。
5. **腾讯/蒲公英/fir**：建议明确标注"不支持"，而不是尝试传参数后静默失败。
6. **图片规格差异很大**（尺寸/格式/数量上限每家都不同），校验逻辑应该放在各 store 包内部，而不是 `pkg/store` 的通用层。

> 各店确切端点/字段以本文为准；实现前建议用真实账号核实一遍，尤其小米"更新时字段是否生效"、三星"仅 FOR SALE 可改"、OPPO `updm` 完整必填字段，以及荣耀/腾讯是否存在未公开接口。
