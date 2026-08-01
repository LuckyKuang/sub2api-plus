## ADDED Requirements

### Requirement: 首 Token 与首输出必须具有独立且稳定的语义

系统 SHALL 将 `first_token_ms` 限定为文本、推理或工具 token-like 非空增量的首次到达延迟，并 SHALL 使用 `first_output_ms` 与 `first_output_kind` 表示任意下游可消费输出的首次到达延迟及模态。生命周期、role-only、空 delta、usage-only、finish-only、无输出终态和错误事件 MUST NOT 触发任一指标。

#### Scenario: 文本流先发送生命周期再发送文字

- **WHEN** 流依次产生 `response.created`、空 delta 和非空文本 delta
- **THEN** 系统 MUST 在非空文本 delta 到达时同时设置首 Token 与首输出
- **THEN** 首输出种类 MUST 为 `text`

#### Scenario: 图片流只有最终图片

- **WHEN** 流没有 partial image，且首个非空图片只出现在 output item done 或 terminal response 中
- **THEN** 系统 MUST 设置首输出和 `kind=image`
- **THEN** 系统 MUST NOT 设置首 Token

#### Scenario: 混合模态先出图片再出文字

- **WHEN** 流先产生非空图片输出，随后产生非空文本 delta
- **THEN** 系统 MUST 保留较早的 `first_output_ms` 和 `first_output_kind=image`
- **THEN** 系统 MUST 在文本 delta 到达时设置 `first_token_ms`
- **THEN** Ops TTFT 聚合 MUST 纳入该有效首 Token 样本

### Requirement: 协议转换计时必须以实际下游输出为准

系统 SHALL 在协议转换完成后观察下游可表达的输出。源事件若没有产生有意义的下游 chunk，MUST NOT 触发首 Token 或首输出。不能无损表达的图片生成能力 MUST 被明确拒绝或路由到兼容协议，MUST NOT 静默吞掉。

#### Scenario: Responses created 转换为 Chat role chunk

- **WHEN** `response.created` 只转换成 assistant role chunk
- **THEN** 系统 MUST NOT 记录首 Token或首输出

#### Scenario: Anthropic 专有元数据在兼容转换中不可表达

- **WHEN** Anthropic 转 Responses 或 Chat 的兼容路径收到转换器会丢弃的 signature、citation、redacted thinking 或 server tool result
- **THEN** 系统 MUST NOT 因源事件本身记录首 Token 或首输出
- **THEN** 后续存在实际下游文本、推理或工具输出时 MUST 以该下游输出的到达时间计时

#### Scenario: Chat 路径收到 Responses 生图输出

- **WHEN** Chat 下游协议没有已声明的图片生成输出扩展
- **THEN** 系统 MUST 返回明确的不支持错误或选择保持 Responses/Images 的路由
- **THEN** 系统 MUST NOT 返回成功但丢失图片

### Requirement: WebSocket 每轮延迟必须从该轮请求写出开始

系统 SHALL 从每个已接受 `response.create` 写上游之前开始计算该轮首 Token、首输出和总耗时。系统 MUST NOT 从首个上游事件、连接建立时间或前一轮时间开始计算后续轮次。

#### Scenario: 首个上游事件就是 token

- **WHEN** 请求写出后经过可观测延迟，首个上游事件即为非空 token delta
- **THEN** 该轮首 Token MUST 包含请求到该事件的延迟且 MUST NOT 因事件绑定时钟而变成零
- **THEN** 该轮总耗时 MUST 包含请求到首事件的延迟

#### Scenario: 第二轮请求

- **WHEN** 同一 WebSocket 连接完成第一轮后发送第二个 `response.create`
- **THEN** 第二轮 MUST 使用独立的新起点和首输出状态

#### Scenario: Live 会话的媒体通过 WebRTC 传输

- **WHEN** `/backend-api/codex/realtime/calls` 的音频或其他媒体不经过 Sub2API 的 sideband WebSocket
- **THEN** 系统 MUST NOT 根据 sideband 控制事件推断首媒体延迟或写入 `first_output_kind=audio`
- **THEN** 系统 MAY 记录网关可观测的会话总耗时

### Requirement: 图片 partial 必须保持流式实时性

系统 SHALL 将首个非空图片 partial 视为有意义首输出，并在 HTTP→WebSocket 上游桥接中立即释放此前缓冲的生命周期事件和该图片事件。系统 MUST NOT 等待最终 terminal 才把 partial image 发送给客户端。

#### Scenario: 图片 partial 出现在终态之前

- **WHEN** HTTP→WebSocket 上游先产生生命周期事件，随后产生非空图片 partial，最后才产生 terminal
- **THEN** 系统 MUST 在图片 partial 到达时释放生命周期缓冲并立即转发该 partial
- **THEN** 系统 MUST 设置首输出和 `kind=image`，且 MUST NOT 设置首 Token

### Requirement: 指标不得控制故障转移提交状态

系统 SHALL 独立维护下游提交、首输出、首 Token 和终态状态。系统 MUST NOT 仅根据 `first_token_ms` 是否为空决定能否故障转移或是否继续缓冲。

#### Scenario: 已发送媒体但没有 token

- **WHEN** 系统已经向客户端发送图片 partial 且没有文本 token
- **THEN** 系统 MUST 认为响应已经提交且不可安全故障转移
- **THEN** `first_token_ms` MUST 保持为空

### Requirement: 历史数据和调度必须保持可解释

系统 MUST NOT 回填无法判定触发事件的历史首 Token 数据。账号 TTFT 调度与 Ops TTFT 聚合 MUST 只消费严格 token-like 的 `first_token_ms`，MUST NOT 消费图片或音频首输出延迟。Ops MUST 以存在 `first_output_kind` 区分新语义记录与 legacy 记录，不得以首输出模态排除混合模态记录中稍后产生的有效首 Token。

#### Scenario: 新图片记录参与调度采样

- **WHEN** 新图片请求只有 `first_output_ms` 和 `first_output_kind=image`，且 `first_token_ms` 为空
- **THEN** 系统 MUST 持久化该首输出数据
- **THEN** 账号 TTFT 调度 MUST NOT 将该延迟作为首 Token 样本

#### Scenario: 查询历史记录

- **WHEN** 历史记录没有 `first_output_kind`
- **THEN** 系统 MUST 保留原有数据且 MUST NOT 推断或回填首输出种类
- **THEN** 使用记录界面和导出 MUST NOT 将该历史值标示为新口径的严格首 Token

#### Scenario: 升级时存在旧语义 TTFT 派生聚合

- **WHEN** 小时、日聚合表或系统指标快照中已保存无法与新语义区分的 legacy TTFT
- **THEN** 升级迁移 MUST 将这些派生 TTFT 值置空并将对应样本数归零
- **THEN** 请求数、总耗时、Token 及错误指标 MUST 保持不变
- **THEN** 后续聚合 MUST 仅使用存在 `first_output_kind` 且 `first_token_ms` 非空的原始记录重建 TTFT
