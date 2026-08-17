## Security model

失败主体是规范化后的可信来源 IP，不是邮箱、用户 ID 或认证方式。同一个 IP 尝试不同账号时共享固定统计窗口；同一账号从其他未封禁 IP 登录不受影响。成功密码、TOTP、OAuth 或 Passkey 认证只证明本次凭据有效，不能证明同一 NAT 出口此前的失败均应被免除，因此不得重置 IP 窗口。

认证响应合同为：真实认证失败但未达阈值返回原始 `401` 或业务错误；达到阈值且 active 自动 block 已确认持久化时返回 `403 / IP_BANNED`；认证前频率保护返回 `429 / RATE_LIMITED`；风控设置、身份、计数、规则加载或提交状态不可确认时返回 `503 / IP_ACCESS_CONTROL_UNAVAILABLE`。操作日志必须携带明确的风控结果，不能通过 HTTP 503 推断封禁成功。

## State model

管理 API 不再返回 `currently_blocked`。每行返回 `active_block_rule`、`runtime_enforcement_enabled`、`suppressed_by_allow_rule`、`emergency_allowlisted` 和 `effectively_blocked`。其中：

`effectively_blocked = active_block_rule && runtime_enforcement_enabled && !suppressed_by_allow_rule && !emergency_allowlisted`

同时返回当前阈值、代表性 block 规则 ID/类型/IP 或 CIDR/创建与到期时间以及 `as_of`。代表性规则优先选择匹配前缀更具体的规则，再选择较新的规则；该选择只用于解释和让管理页准确定位规则，实际判定仍是“任一 block 匹配且没有 allow 匹配”。

## Authentication-time snapshot

进程启动必须同步读取完整设置与 active 规则，失败则不接受生产流量。成功预热后，认证前请求只读取进程内编译快照并检查规则绝对 `expires_at`，正常路径不访问 PostgreSQL 或 Redis。

快照刷新由三种来源触发：本实例持久化成功后直接应用返回规则；跨实例失效事件触发立即后台刷新；固定周期对账补偿 Pub/Sub 漏消息。所有回源由 singleflight 合并，并在成功获得完整设置与规则后原子替换最后已知正确快照。每次本地持久化成功都推进 mutation epoch；若数据库读取期间 epoch 发生变化，刷新结果不得覆盖本地补丁，必须从新 epoch 重新读取。较早返回的 release 结果也不得删除同一 natural key 下已经补丁进来的更高 ID active 规则。刷新失败保留旧快照，但不得延长规则自身的 `expires_at`。

固定“一天 TTL”不作为 block 生效时间。短期规则只能生效到自己的 `expires_at`，长期或永久规则留在快照中并周期确认。完整快照超过最大陈旧时间且无法刷新时，服务 readiness 失败，无法安全判定的请求返回 503；旧 allow 规则尤其不得无限期继续放行。

## Local mutations

创建 manual/allow、创建 auto block 和释放规则都在数据库提交成功后用返回的完整规则修补当前实例快照，再发布跨实例刷新事件。不得先清空快照并让下一批攻击请求负责回源。释放 allow 只移除 allow，未到期 block 会立即恢复；释放 block 同时重置相应 IP 失败窗口。

自动 block 事务提交返回错误时，仓储使用新的数据库操作确认 exact active auto block。只有确认规则存在才把结果恢复为成功并返回 403；无法确认时保持 503。

认证失败事务只处理当前 exact IP 的 advisory lock、固定窗口 UPSERT 和必要的 durable block。其他 IP 的过期窗口由有界后台任务清理，不得在每次攻击请求中重复扫描和删除。

## Management refresh

管理页首次进入、搜索、分页和管理操作后刷新；此外提供显式刷新按钮和最后更新时间。页面可见时使用低频定时刷新，隐藏时停止请求。并发刷新使用单调请求序号，只允许最新请求更新列表、加载状态和错误状态，避免旧响应覆盖新状态。失败状态根据分层字段展示“观察中 n/threshold”“已封禁”“规则存在但执行关闭”“被白名单覆盖”“被部署级紧急白名单覆盖”或“状态不可用”，而不是把所有情况显示为“未封禁”。

## Failure-state manual block action

登录失败状态行使用专用的 `POST /api/v1/admin/ip-access-control/failure-state/block`，而不是让前端直接拼装通用规则创建请求。请求只携带 exact IP 和可选原因；封禁时长由服务端当前 `login_failure_block_minutes` 决定，避免页面设置陈旧改变安全语义。该端点必须经过管理员认证、step-up 验证和安全客户端身份链检查。

服务端在创建前确认系统总开关与运行时执行开关均开启、完整策略快照可用且没有 active allow 覆盖。创建结果固定为 `manual_block`，不得伪装成 `auto_block`，也不得删除失败窗口。相同 IP 的快捷手动封禁和自动阈值封禁使用同一事务级互斥键串行化：若任一未过期 manual/auto/CIDR block 已覆盖该 IP，则幂等返回代表性现有规则并设置 `already_blocked=true`；否则创建 exact-IP manual block。

提交后当前实例必须更新并重新确认完整快照。响应中的 `effectively_blocked=true` 只在运行时执行开启、没有 allow 覆盖且新快照实际匹配 block 时返回；无法确认时返回 `503 / IP_ACCESS_CONTROL_UNAVAILABLE`，即使重试时可能发现前一次规则已经成功提交。allow 覆盖返回 `409 / IP_BLOCK_SUPPRESSED_BY_ALLOW`，部署级紧急恢复名单覆盖返回 `409 / IP_BLOCK_SUPPRESSED_BY_EMERGENCY_ALLOW`，执行关闭返回 `409 / IP_ACCESS_ENFORCEMENT_DISABLED`，身份链不安全返回 `409 / IP_ACCESS_IDENTITY_UNSAFE`。已有效封禁是幂等成功而不是冲突。

管理页只在失败状态数据和服务端封禁时长均可用、代理身份链可安全执行且运行时执行开启时显示可提交的“手动封禁”。已有效封禁时提供“查看规则”；allow 覆盖时提示先处理白名单；部署级紧急 allowlist 覆盖时显示不可在页面解除的覆盖状态；状态陈旧、设置不可用、执行关闭或身份不安全时禁用危险动作。打开确认框前重新读取服务端设置；若目标行在此期间消失或提交前不再存在于当前结果中，则拒绝使用陈旧目标。确认框必须明确封禁 IP、页面刚读取的服务端配置时长、“提交时以后端最新值为准”以及“不会清除失败计数”；自动封禁关闭时仍显示该服务端封禁时长的配置项；提交期间禁止重复点击，成功后同时刷新失败状态和规则列表。

Passkey 失败计数只接受实际 WebAuthn 凭据验证拒绝。伪造、过期或重复消费的一次性 Passkey 会话不代表凭据验证，不能计数；PostgreSQL、Redis 或用户/凭据仓储错误必须保持依赖错误，不得折叠为可计数的验证失败。同理，伪造、过期或已经消费的 TOTP 临时登录会话只表示会话无效，不能计数；只有在有效会话内校验失败的 TOTP code 才属于凭据级失败。

部署级紧急 allowlist 在失败记录入口复用与认证前判定完全相同的优先级：安全身份只按已验证来源 IP 匹配；身份链不安全时才允许按直连对端匹配以恢复管理能力。命中后跳过失败计数和自动封禁，避免创建一个实际不会执行的 block 或错误返回 `IP_BANNED`。可信代理的传输对端不得替代已验证转发客户端参与该豁免。

## Deferred Redis phase

后续阶段可把失败固定窗口迁到 Redis Lua，并使用 ZSET 索引、generation、持久化 leader 和短期 quarantine；只有 PostgreSQL durable block 确认后才能返回 `IP_BANNED`。该阶段需要独立 OpenSpec 和 forward-only migration，不在本修复中留下半实现键或兼容分支。
