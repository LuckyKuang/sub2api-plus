## ADDED Requirements

### Requirement: 登录失败封禁必须只归属来源 IP

系统 SHALL 以可信客户端身份解析得到的规范化来源 IP 作为登录失败统计主体。同一 IP 对任意账号和受支持认证方式产生的凭据级失败 SHALL 进入同一固定窗口。系统 MUST NOT 锁定账号，也 MUST NOT 因任一成功认证清除来源 IP 的失败窗口。

#### Scenario: 同一 IP 依次尝试不同账号

- **WHEN** 同一规范化 IP 在一个窗口内对两个不同账号各产生一次真实认证失败，阈值为 2
- **THEN** 第二次失败 MUST 使该 IP 达到阈值
- **THEN** 两个账号从其他未封禁 IP 登录 MUST 不受影响

#### Scenario: 失败后成功再失败

- **WHEN** 一个 IP 在窗口内先失败一次、随后成功认证、再失败一次，阈值为 2
- **THEN** 成功认证 MUST NOT 清除第一次失败
- **THEN** 最后一次失败 MUST 触发 IP 自动封禁

#### Scenario: Passkey 会话或依赖失败

- **WHEN** 客户端提交伪造、过期或已消费的 Passkey 会话，或者 Passkey 凭据/用户存储不可用
- **THEN** 系统 MUST NOT 将该结果计入来源 IP 的凭据失败窗口
- **THEN** 只有实际 WebAuthn 凭据验证被拒绝时才 MAY 增加失败计数

#### Scenario: TOTP 临时会话无效

- **WHEN** 客户端提交伪造、过期或已经消费的 TOTP 临时登录会话
- **THEN** 系统 MUST NOT 将该结果计入来源 IP 的凭据失败窗口
- **THEN** 只有有效临时会话内的 TOTP code 校验失败才 MAY 增加失败计数

#### Scenario: 紧急恢复来源认证失败

- **WHEN** 已验证来源 IP 命中部署级紧急 allowlist，或者身份链不安全时直连对端命中该名单
- **THEN** 登录失败 MUST NOT 增加 IP 失败计数或创建自动 block
- **THEN** 认证端点 MUST 返回原始认证失败，而不得返回 `403 / IP_BANNED` 或风控身份不可用 503
- **THEN** 安全可信代理的直连对端命中名单 MUST NOT 豁免未命中名单的已验证转发客户端 IP

### Requirement: 封禁与不可用响应必须可区分

系统 SHALL 仅在确认 active durable block 规则存在时返回 `403 / IP_BANNED`。风控依赖失败或事务提交结果无法确认时 MUST 返回 `503 / IP_ACCESS_CONTROL_UNAVAILABLE`，并 MUST NOT 在审计中把该 503 标记为已封禁。

#### Scenario: 阈值事务成功提交

- **WHEN** 失败计数达到阈值且自动 block 规则提交成功
- **THEN** 当前认证请求 MUST 返回 403 和 `IP_BANNED`
- **THEN** 后续请求 MUST 在认证前由内存策略拒绝

#### Scenario: 提交返回错误但规则可确认

- **WHEN** 提交调用返回错误，但使用新的数据库操作确认 exact active auto block 已存在
- **THEN** 系统 MUST 将结果视为已持久化并返回 403

#### Scenario: 提交结果无法确认

- **WHEN** 提交调用返回错误且无法确认 active auto block
- **THEN** 系统 MUST 返回 503 和 `IP_ACCESS_CONTROL_UNAVAILABLE`
- **THEN** 操作日志 MUST 将风控结果记录为不可用而不是已封禁

### Requirement: 认证前策略判定不得由攻击请求回源数据库

系统 SHALL 在接受生产流量前预热完整设置与规则快照。预热成功后的认证前请求 MUST 只读取进程内快照；周期到期和失效事件 MUST 由 singleflight 合并的后台刷新处理。

#### Scenario: 大量请求同时到达刷新边界

- **WHEN** 快照到达周期刷新时间且大量认证请求并发到达
- **THEN** 请求 MUST 继续读取最后已知正确快照
- **THEN** PostgreSQL 全量规则加载 MUST NOT 按请求数放大

#### Scenario: 短期 block 自然到期

- **WHEN** 缓存中 block 的 `expires_at` 已到达
- **THEN** 请求判定 MUST 停止应用该 block
- **THEN** 系统 MUST NOT 等待固定一天 TTL 或管理员删除缓存

#### Scenario: 完整快照过度陈旧

- **WHEN** 后台刷新持续失败且完整快照超过允许的最大陈旧时间
- **THEN** readiness MUST 失败
- **THEN** 无法安全判定的请求 MUST fail closed 为 503

### Requirement: 管理状态必须分离规则与执行结果

失败状态 API SHALL 分别返回 active block 规则、运行时执行开关、持久化 allow 覆盖、部署级紧急 allowlist 覆盖和最终有效封禁状态，并 SHALL 返回失败阈值和状态时间点。系统 MUST NOT 使用一个布尔值同时表达以上状态。

#### Scenario: 规则存在但执行关闭

- **WHEN** IP 存在未过期 active block，但全局或页面执行开关关闭
- **THEN** `active_block_rule` MUST 为 true
- **THEN** `runtime_enforcement_enabled` 与 `effectively_blocked` MUST 为 false

#### Scenario: allow 覆盖 block

- **WHEN** IP 同时匹配未过期 allow 和 block
- **THEN** `active_block_rule` 与 `suppressed_by_allow_rule` MUST 为 true
- **THEN** `effectively_blocked` MUST 为 false

#### Scenario: 部署级紧急 allowlist 覆盖 block

- **WHEN** IP 同时匹配未过期 active block 和部署级紧急 allowlist
- **THEN** `active_block_rule` 与 `emergency_allowlisted` MUST 为 true
- **THEN** `effectively_blocked` MUST 为 false

#### Scenario: 页面停留期间产生第二次失败

- **WHEN** 管理页面已显示第一次失败，随后同一 IP 达到阈值
- **THEN** 管理员 MUST 能手动刷新
- **THEN** 页面可见时的低频自动刷新 MUST 在无需重新进入页面的情况下更新计数与状态
- **THEN** 多个刷新请求重叠时 MUST 只允许最新请求更新页面状态

#### Scenario: 后台刷新与本地封禁提交重叠

- **WHEN** 完整快照读取开始后，本实例提交并补丁了新的 active block
- **THEN** 较早读取的快照 MUST NOT 覆盖该本地 block
- **THEN** 刷新 MUST 从新的 mutation epoch 重试并保留有效封禁

### Requirement: 管理操作必须保持独立语义

“重置失败计数” SHALL 只删除失败窗口；若 active block 仍存在，认证前请求 MUST 继续返回 403。“解封并重置” SHALL 释放 block 并删除对应失败窗口。allow SHALL 只覆盖 block 而不得删除它。

#### Scenario: 已封禁 IP 只重置计数

- **WHEN** 管理员对 active blocked IP 执行“重置失败计数”
- **THEN** 失败状态行 MAY 消失
- **THEN** active block 与认证前 403 MUST 保持不变

#### Scenario: 移除 allow

- **WHEN** allow 覆盖一个仍未过期的 block，随后管理员移除 allow
- **THEN** 系统 MUST 立即恢复该 block 的执行

### Requirement: 登录失败状态必须支持可确认的手动 IP 封禁

系统 SHALL 在登录失败状态行提供经过管理员 step-up 验证的 exact-IP 手动封禁动作。该动作 MUST 创建 `manual_block`、MUST NOT 锁定账号、MUST NOT 清除失败计数，并且 MUST 以服务端当前配置的登录失败封禁时长计算到期时间。系统 SHALL 仅在确认目标 IP 当前会被运行时策略拒绝时返回 `effectively_blocked=true`。

#### Scenario: 未封禁 IP 被管理员手动封禁

- **WHEN** 失败状态中的 IP 没有 allow 覆盖、运行时执行已开启且身份链安全
- **THEN** 系统 MUST 创建覆盖该 exact IP 的 active `manual_block`
- **THEN** 当前实例 MUST 在成功响应前应用并确认该规则
- **THEN** 原失败计数 MUST 保持不变

#### Scenario: 重复点击手动封禁

- **WHEN** 两个相同 IP 的手动封禁请求并发或依次到达
- **THEN** 系统 MUST 至多创建一条新的 active exact-IP `manual_block`
- **THEN** 后续请求 MUST 幂等返回覆盖该 IP 的现有 block，并设置 `already_blocked=true`

#### Scenario: 手动封禁与自动封禁并发

- **WHEN** 管理员手动封禁与同一 IP 的阈值自动封禁并发发生
- **THEN** 两个动作 MUST 按该 IP 串行化
- **THEN** 系统 MUST 返回一个有效 block，而不得依赖重复规则才能执行封禁

#### Scenario: allow 覆盖目标 IP

- **WHEN** exact IP 或其上级 CIDR 存在未过期 active allow
- **THEN** 快捷动作 MUST NOT 报告手动封禁成功
- **THEN** API MUST 返回 409 和 `IP_BLOCK_SUPPRESSED_BY_ALLOW`
- **THEN** 即使尚无 block，失败状态 API 与管理页也 MUST 显示该 IP 已被 allow 覆盖并禁用快捷封禁

#### Scenario: 部署级紧急恢复名单覆盖目标 IP

- **WHEN** 目标 IP 匹配部署配置中的紧急恢复 allowlist
- **THEN** 快捷动作 MUST NOT 创建一个宣称可执行的手动封禁
- **THEN** API MUST 返回 409 和 `IP_BLOCK_SUPPRESSED_BY_EMERGENCY_ALLOW`

#### Scenario: 执行条件不安全或不可确认

- **WHEN** 全局执行关闭、可信客户端身份链不安全、策略快照过度陈旧或提交后的执行结果无法确认
- **THEN** API MUST NOT 返回 `effectively_blocked=true`
- **THEN** API MUST 分别返回稳定的冲突或不可用错误码，以便管理员处理后重试

#### Scenario: 自动封禁关闭但允许手动封禁

- **WHEN** 运行时执行开启但登录失败自动封禁关闭
- **THEN** 管理页 MUST 仍允许管理员查看和修改快捷手动封禁所使用的服务端封禁时长

#### Scenario: 确认前设置或目标状态变更

- **WHEN** 管理员准备快捷封禁但服务端时长无法重新读取，或目标失败状态行在确认/提交前消失
- **THEN** 页面 MUST 禁止提交陈旧的快捷封禁动作
- **THEN** 请求体 MUST NOT 携带页面缓存的封禁时长
