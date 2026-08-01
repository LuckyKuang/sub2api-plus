## MODIFIED Requirements

### Requirement: 首 Token 与首输出必须具有独立且稳定的语义

系统 SHALL 将 `first_token_ms` 限定为文本、推理或工具 token-like 非空增量的首次到达延迟，并 SHALL 使用 `first_output_ms` 与 `first_output_kind` 表示任意下游可消费输出的首次到达延迟及模态。生命周期、role-only、空 delta、usage-only、finish-only、无输出终态和错误事件 MUST NOT 触发任一指标。合并上游 provider、Messages、Chat fallback 或 Live 变更时 MUST 继续按转换后的下游可见输出应用该口径，不得恢复为首网络事件计时。

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

#### Scenario: 合并上游生命周期事件

- **WHEN** 新的上游 provider 或协议先发送生命周期、role-only、usage-only 或 finish-only 事件
- **THEN** 系统 MUST NOT 将该事件记录为首 Token 或首输出
- **THEN** 后续存在实际下游文本、推理、工具或媒体输出时 MUST 以该输出的到达时间计时

#### Scenario: 查询升级前的用量记录

- **WHEN** 历史记录具有 `first_token_ms` 但没有 `first_output_kind`
- **THEN** 系统 MUST 将该值标示为旧版首事件而不是新口径首字
- **THEN** 系统 MUST NOT 推断或回填未知的首输出种类
