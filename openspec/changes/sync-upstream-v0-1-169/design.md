## Baseline and release identity

官方基线为 `v0.1.169`，提交
`26d894ef4f50645a4bf1030e378ac892f17d0223`。Plus 的发布身份独立管理：Git
tag 与内嵌应用版本为 `v0.1.169+custom.001`，OCI tag 为
`v0.1.169-custom.001`。上游 tag 中的 `VERSION` 值滞后于发布版本，不能直接
覆盖 Plus 元数据。`UPSTREAM.md` 在同一变更中记录该基线和 planned 状态。

## Gateway path validation

任何会拼接到上游 URL path 的客户端输入必须先通过闭集路径片段校验。Responses
子路径、Codex 别名和 Gemini `v1beta/models` 的模型/动作均覆盖在内；分隔符、
空段、`..` 语义、编码后的路径语义和非预期字符被拒绝。校验位于账户调度、额度
扣减、账单记录和网络转发之前，因此失败请求不能消耗池化账号或访问上游任意
端点。合法的 `responses/compact` 与取消类子路径保持兼容。

## Scheduler and authorization composition

`selectAccountWithScheduler` 的外层顺序如下：

1. 先执行 Plus 的 OpenAI OAuth 共享会话和 `previous_response_id` 跨组访问校验。
2. 使用常规代理隔离偏好执行一次完整选择。
3. 选择成功则立即返回；OAuth 授权错误直接返回。
4. 仅当平台为 OpenAI、结果为无可用账号/紧凑账号，且存在活跃代理隔离时，使用
   仅绕过代理隔离的上下文重试一次。

实际候选筛选继续执行 OAuth 组、模型、能力、渠道、配额、父账号健康度、运行时
限制和临时不可调度条件。故障熔断能 fail-open，授权不能 fail-open。三秒窗口内
并发断流折叠为一次失败事件，配置
`gateway.openai_proxy_stream_circuit.disabled=true` 时完全关闭该行为。

## Release resources

运行时定价在远端刷新失败时需要相对运行目录下的
`resources/model-pricing/model_prices_and_context_window.json`。镜像构建复制
`backend/resources`，直接归档也以相同相对路径携带该资源。验证应解包真实或
等效发布归档、禁用远端价格获取并确认兜底加载成功，避免只修复镜像用户。

## Deployment and hardening

四个 Compose 变体的应用服务均使用
`security_opt: [no-new-privileges:true]`，并向容器传递
`SERVER_TRUSTED_PROXIES`、`SERVER_IP_ACCESS_EMERGENCY_ALLOWLIST`、
`SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL` 与
`GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED`。模板提供生产建议：经审计的
反代 CIDR、关闭 API-key ACL 的转发 IP 信任、启用 URL allowlist，并禁用 HTTP/
私网目标；实际 `.env` 保持由运营者审核后迁移。

Apple Containers 沿用入口脚本以非 root `sub2api` 用户运行应用。Compose 的
`no-new-privileges` 不能被假定为 Apple Containers 等价能力，后者作为运行时
硬化跟进项单独验证。

## Release-bound pricing

Pricing is financial input and cannot trust a mutable branch or a same-origin
checksum. Remote refresh therefore uses a latest-release manifest only as a
discovery pointer. The manifest binds a monotonically increasing release
version, a fixed HTTPS pricing-data URL, and that data's SHA-256. The data path
must be `/releases/download/<version>/model-pricing.json`; a `/latest/` data
asset is rejected so only manifest discovery remains mutable. The
sole-maintainer GitHub Release publication boundary is the source of trust, and
normal deployments require no pricing key configuration.

The service validates the initial URL and every HTTP redirect against the
dedicated pricing host allowlist, regardless of the global upstream URL
allowlist compatibility setting. It strictly parses the manifest, verifies the
data hash before parsing pricing JSON, rejects version rollback, then atomically
persists data and validated manifest state before changing in-memory pricing.
Any failure leaves an already loaded cache intact; an initial failure falls
back to the bundled resource.

The release workflow creates `model-pricing.json`,
and `model-pricing-manifest.json` after the normal release is created. Existing
assets are accepted only when byte-identical, never overwritten. Publication
remains gated by the protected `release` environment and repository governance.

## Browser policy boundary

CSP remains deployment configuration. The default permits HTTPS image origins
for compatibility but never arbitrary HTTP. Deployments that know their image
CDNs may replace the generic HTTPS source with explicit HTTPS origins in
`security.csp.policy`; this is intentionally not an admin database setting.

## CI and deferred governance

`docker/setup-buildx-action` 和 `docker/login-action` 更新为官方原生 Node.js 24
版本及固定 SHA。GoReleaser 配置已迁移到当前 schema：归档使用 `formats`，镜像
使用 `dockers_v2`，并保留 Docker Hub/GHCR、架构专用与多架构标签、版本标签和
`DOCKERHUB_USERNAME=skip` 语义。CI 使用不具发布权限的占位环境变量执行
`goreleaser check`；本地 snapshot 归档验证资源路径。GitHub Ruleset、环境、
Actions allowlist、CODEOWNERS、Dependabot 与真实密钥设置属于仓库外部治理，由
维护者批准后另行执行。
