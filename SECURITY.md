# Security Policy

## Supported Versions

Unless a release states otherwise, security fixes target the latest published
custom release. Older releases may no longer receive fixes.

## Reporting a Vulnerability

Use [Sub2API Plus private vulnerability reporting](https://github.com/LuckyKuang/sub2api-plus/security/advisories/new)
when it is available. Do not post sensitive vulnerability details in public
issues, pull requests or discussions, including credentials, production data,
private endpoints or exploit details.

If private reporting is unavailable, open a minimal issue requesting a private
contact channel without including sensitive technical details.

Include the affected version, deployment type, impact, reproduction conditions,
and any relevant logs with secrets removed. Maintainers will assess scope and
coordinate disclosure based on severity and available fixes.

Reports cover the Plus backend, frontend, distribution artifacts and deployment
files. Keep technical details private while maintainers investigate and prepare
a fix; disclosure follows the fix release. Reporters can request anonymous
credit.

## Dependency Audit Exceptions

CI 以 `pnpm audit --prod --audit-level=high` 守护生产依赖。除能在 `frontend/package.json` 的 `pnpm.overrides` 中强制打补丁的传递依赖外，**无安全版本可用**的依赖只能通过 `.github/audit-exceptions.yml` 显式接受，并须在此登记依据与再评估条件；`tools/check_pnpm_audit_exceptions.py` 是唯一执行者，过期例外与「未匹配任何已报告 advisory」的例外都会使其失败。

当前清单为空（`.github/audit-exceptions.yml` 的 `exceptions: []`）。

### 已移除的例外：`xlsx`

原先登记的 `xlsx@0.18.5` 例外（`CVE-2023-30533`/`GHSA-4r6h-8v6p-xvw6` 原型污染、`CVE-2024-22363`/`GHSA-5pgg-2g8v-p4x9` ReDoS）已随依赖替换一并删除，不再需要风险接受：

- **来源与版本**：`frontend/third-party/xlsx-0.20.3.tgz`，即官方 SheetJS Community Edition `0.20.3`（`license: Apache-2.0`，内部清单 `"dependencies": {}`，因此 `0.18.5` 时期的 7 个传递依赖已消失）。`0.20.3` 高于两条 advisory 的修复边界（`0.19.3` 与 `0.20.2`），`pnpm audit --prod --audit-level=high` 不再报告 `xlsx`。
- **复现并校验哈希**：

  ```bash
  curl -fL -o xlsx-0.20.3.tgz https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz
  shasum -a 256 xlsx-0.20.3.tgz   # 8dc73fc3b00203e72d176e85b50938627c7b086e607c682e8d3c22c02bb99fe8
  shasum -a 512 xlsx-0.20.3.tgz   # a0b0eade3c3b01c2ea2961f60210a9553665f267fa5f661178ff8d7a1d12254cd5fc1759623b61f78b46e6da22301d4f3eb62dc4e09f6a850292fb6e1fedc024
  tar -xzOf xlsx-0.20.3.tgz package/package.json
  ```

  `frontend/third-party/SHA256SUMS` 记录了文件名、大小、sha256、sha512、来源 URL 与获取日期，可用 `shasum -a 256 -c SHA256SUMS` 校验。SheetJS 不发布 per-release 校验文件（`.tgz.sha256`/`.tgz.sha512`/`SHA256SUMS` 均 404），故须由第二人独立重复下载并核对哈希。
- **不得重新引入 `pnpm.auditConfig.ignoreCves`**：该设置会把 advisory 从 `pnpm audit --json` 的 `advisories` 中移除（只留在 `muted`），而 `tools/check_pnpm_audit_exceptions.py` 消费的正是 `advisories`，于是「缺少例外」与「例外过期」两项检查同时失效——旧配置下审计长期返回 `advisories: []` 却仍有 `metadata.vulnerabilities.high = 2`。例外必须写进 `.github/audit-exceptions.yml` 才能被检查器看到。
- **回归守护**：`frontend/src/views/admin/__tests__/UsageView.spec.ts` 的 `UsageView xlsx audit exception` 断言 `frontend/src` 下（测试目录与守卫文件自身除外）不存在 `XLSX.read*`/`readFile*`/`sheet_to_*` 解析调用、`frontend/package.json` 不 pin `ignoreCves`、例外清单不含 `xlsx`。
- **再评估条件**：SheetJS CE 发布新版本；出现针对 `xlsx` 的新 advisory；或任何代码开始读取/解析外部 xlsx（`XLSX.read`/`readFile` 等）——最后一种情况下「只写不读」前提失效，必须立即重新评估该依赖。

接受新例外前，先确认能通过 `pnpm.overrides` 打补丁；确无补丁且风险路径不可达时，才在 `.github/audit-exceptions.yml` 新增带 `expires_on` 与 `owner` 的条目，并在此登记同一依据与再评估条件。

## Operational and Upstream Risk

Provider terms-of-service questions, exposed deployment credentials, and
operator misconfiguration are not automatically product vulnerabilities.
Reports are still welcome when the project can improve defaults, validation,
or documentation.
