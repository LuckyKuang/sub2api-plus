## Why

当前 `first_token_ms` 在不同流式协议中分别代表首行、首事件、首结构 chunk、首 token 或终止事件时间。图片、Responses/Chat 转换及 WebSocket 多轮路径会产生错误或不可比较的数据；部分 HTTP→WebSocket 图片流还会把 partial image 缓冲到终态，破坏预览实时性。WS v2 passthrough 的单轮时钟又从首个上游事件才开始，漏掉请求写出到首事件的延迟。

## What Changes

- 保留 `first_token_ms` 作为严格的文本/推理/工具 token-like 增量延迟。
- 为用量记录新增 `first_output_ms` 与 `first_output_kind`，记录任意下游可消费输出的首次到达时间及模态。
- 引入载荷感知的 Responses、Chat、Anthropic、Gemini/媒体流输出观察器，排除生命周期、role-only、空 delta、usage-only 和无输出终态。
- 协议转换路径按转换后的下游输出计时；不能表达图片的 Chat 转换路径不得静默吞图。
- 将指标状态与下游提交、缓冲、故障转移和终态状态解耦。
- 让 HTTP→WebSocket 上游的首个图片 partial 立即释放缓冲并下发。
- 修复 WS v2 passthrough 每轮计时起点，使单轮首输出和总耗时从对应 `response.create` 写上游前开始。
- 前端延迟列主展示固定为“首字/总耗时/TPS”；首输出模态与时间放入详情 hover，CSV 保留首 Token 并新增首输出字段。
- 账号调度器继续只消费 `first_token_ms`；图片和音频首输出不得进入 TTFT EWMA。
- 流中途失败但保留部分 usage 时，同步保留已观测到的首 Token、首输出和输出模态。
- 使用记录延迟列显示基于严格首 Token 的估算 TPS；只对明确完整的请求显示，并从输出 Token 中排除图片和音频 Token；对过短生成窗（`<300ms`）、过少文本 Token（`<8`）与异常偏高结果（`>500`）隐藏 TPS；混合模态的较早首输出在详情 hover 中展示，主列仍只显示严格首字。

## Capabilities

### New Capabilities

- `stream-first-output-timing`: 定义跨协议首 Token、首输出、输出模态、转换可见性、WebSocket 多轮计时和历史兼容语义。

### Modified Capabilities

无。

## Impact

- **数据库**：`usage_logs` 新增可空的 `first_output_ms`、`first_output_kind`、`is_complete`，以及默认值为零的 `audio_output_tokens`；历史行不回填完成状态。
- **后端**：网关流处理、协议转换、图片流、WebSocket relay、用量持久化、调度和 Ops 聚合。
- **公共 API**：UsageLog DTO 新增首输出、音频输出 Token 与可空完成状态字段；保留 `first_token_ms`。
- **前端**：使用记录延迟展示和 CSV 导出新增首输出语义，列表从现有字段派生 TPS，中英文 locale 同步。
- **兼容性**：非流式文本请求保持首 Token 为空；返回实际媒体或终态聚合输出的路径可记录首输出，但不得反推首 Token。旧行保持原值且以缺少 `first_output_kind` 识别为 legacy，`is_complete=NULL`，因此不显示 TPS。Chat 不可表达的生图请求改为明确错误，而不是成功但丢失图片。
- **总耗时**：除 WS v2 passthrough 单轮计时起点外，不改变 `duration_ms` 计算。
- **非目标**：本变更不持久化 TPS，不回写历史用量，不改变计费 token/图片/音频数量，也不新增 TPS 排序或筛选。
