# Security Policy

🌐 **Language / 语言**: **English** · [简体中文](#安全策略-中文)

## Supported Versions

Only the latest minor release line receives security fixes. Older versions are
not patched — please upgrade.

| Version | Supported          |
| ------- | ------------------ |
| 3.1.x   | :white_check_mark: |
| < 3.1   | :x:                |

## Reporting a Vulnerability

**Please do not open public GitHub issues for security problems.**

Use one of the following private channels:

1. **GitHub Private Vulnerability Reporting** (preferred) —
   <https://github.com/KevinGong2013/apkgo/security/advisories/new>
2. **Email** — `aoxianglele2010@gmail.com` with subject prefix `[apkgo security]`.

Please include:

- A description of the issue and its potential impact.
- Steps to reproduce, ideally a minimal proof-of-concept.
- Affected version(s) (`apkgo version` output is helpful).
- Your suggested fix, if any.

### What to expect

- **Acknowledgement:** within 72 hours.
- **Initial assessment:** within 7 days, including a tentative severity rating.
- **Fix & disclosure:** coordinated with the reporter. We aim to ship a patched
  release and publish a GitHub Security Advisory within 30 days for
  high/critical issues. Lower-severity issues may be bundled into the next
  scheduled release.

We will credit reporters in the advisory unless you request otherwise.

## Scope

In scope:

- The `apkgo` CLI (`cmd/apkgo`).
- The telemetry server (`cmd/telemetry-server`).
- Store adapters under `pkg/store/`.
- Release artifacts published from this repository.

Out of scope:

- Third-party app store APIs themselves (report to the respective vendor).
- Vulnerabilities that require a malicious local config file (`apkgo.yaml`)
  the user has authored — this is treated as trusted input.
- The hosted `apkgo cloud` service (<https://apkgo.baici.tech>) — please
  contact that service directly.

## Handling of Credentials

`apkgo` reads store credentials from local config or environment variables and
forwards them only to the matching vendor API over TLS. Credentials are never
logged, never sent to third parties, and never included in telemetry events.
If you discover a code path that leaks credentials, please treat it as a
high-severity finding and report it through the channels above.

---

## 安全策略 (中文)

## 支持的版本

仅最新次要版本线接收安全修复,旧版本不再打补丁,请及时升级。

| 版本    | 是否支持 |
| ------- | -------- |
| 3.1.x   | ✅       |
| < 3.1   | ❌       |

## 漏洞报告

**请不要在公共 Issue 中提交安全问题。** 请使用以下私密渠道之一:

1. **GitHub 私密漏洞报告**(推荐) ——
   <https://github.com/KevinGong2013/apkgo/security/advisories/new>
2. **邮件** —— `aoxianglele2010@gmail.com`,主题加前缀 `[apkgo security]`。

报告中请包含:

- 问题描述与潜在影响。
- 复现步骤,最好附带最小化 PoC。
- 受影响版本(`apkgo version` 的输出最有帮助)。
- 如果有的话,你建议的修复方案。

### 处理时效

- **确认收到:** 72 小时内。
- **初步评估:** 7 天内,包含初步严重性评级。
- **修复与披露:** 与报告者协同推进。高/严重等级问题目标在 30 天内发布修复版本与
  GitHub Security Advisory;低等级问题可能并入下个常规版本。

如非报告者要求匿名,我们会在 Advisory 中致谢。

## 范围

**在范围内:**

- `apkgo` CLI(`cmd/apkgo`)。
- 遥测服务(`cmd/telemetry-server`)。
- `pkg/store/` 下的应用商店适配器。
- 本仓库发布的二进制制品。

**不在范围内:**

- 第三方应用商店 API 自身的漏洞(请联系对应厂商)。
- 依赖用户主动提供恶意本地配置文件(`apkgo.yaml`)才能触发的问题——本地配置文件视为可信输入。
- 托管版 `apkgo cloud`(<https://apkgo.baici.tech>)——请直接联系该服务。

## 凭证处理说明

`apkgo` 仅从本地配置或环境变量读取商店凭证,并通过 TLS 仅转发给对应厂商 API。
凭证不会写入日志、不会发送给第三方、也不会包含在遥测事件中。如果你发现任何会泄露
凭证的代码路径,请将其按高危问题通过上述渠道报告。
