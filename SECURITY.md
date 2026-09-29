# Security Policy

## Supported Versions

Unless a release states otherwise, security fixes target the latest published
custom release. Older releases may no longer receive fixes.

## Reporting a Vulnerability

Use the repository's private GitHub vulnerability-reporting feature when it is
available. Do not open a public issue containing credentials, production data,
private endpoints, or exploit details.

If private reporting is unavailable, open a minimal issue requesting a private
contact channel without including sensitive technical details.

Include the affected version, deployment type, impact, reproduction conditions,
and any relevant logs with secrets removed. Maintainers will assess scope and
coordinate disclosure based on severity and available fixes.

## Dependency Audit Exceptions

CI 以 `pnpm audit --prod --audit-level=high` 守护生产依赖。除能在 `frontend/package.json` 的 `pnpm.overrides` 中强制打补丁的传递依赖外，**无安全版本可用**的依赖走 `pnpm.auditConfig.ignoreCves` 显式接受，并须在此登记依据与再评估条件。

- `xlsx@0.18.5`（`CVE-2023-30533`/`GHSA-4r6h-8v6p-xvw6` 原型污染；`CVE-2024-22363`/`GHSA-5pgg-2g8v-p4x9` ReDoS；pnpm 的 `ignoreCves` 匹配 CVE ID，故按 CVE 登记）：两条高危均需“解析不可信的 xlsx 输入”方可触发。本项目仅在 `frontend/src/views/admin/UsageView.vue` 中把**应用自有的使用统计数据导出**为 xlsx，从不解析用户上传的表格，故这些解析路径**不可达**。`UsageView.spec.ts` 对该“只导出、不解析”不变量及本例外清单做回归守护。**再评估条件**：一旦任何代码开始读取/解析外部 xlsx（`XLSX.read`/`readFile` 等），或改用维护中的实现/分叉，须撤销对应 `ignoreCves` 并重新评估。

接受新例外前，先确认能通过 `pnpm.overrides` 打补丁；确无补丁且风险路径不可达时，才新增 `ignoreCves` 并在此说明。

## Operational and Upstream Risk

Provider terms-of-service questions, exposed deployment credentials, and
operator misconfiguration are not automatically product vulnerabilities.
Reports are still welcome when the project can improve defaults, validation,
or documentation.
