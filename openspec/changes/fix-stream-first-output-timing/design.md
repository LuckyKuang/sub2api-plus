## Metric model

`first_token_ms` 仅由非空文本、推理或工具生成增量触发。`first_output_ms` 由首个下游协议可消费的非空输出触发；文本增量通常同时触发两者，图片/音频只触发首输出。`first_output_kind` 为 `text`、`reasoning`、`tool`、`image` 或 `audio`。

观察时间以本次上游尝试的既有 `startTime`/`turnStart` 为起点，以网关解析并确认输出可表达的时刻为终点，不包含慢客户端 socket 写阻塞。

## State separation

流处理必须分别维护：

- `responseCommitted`：控制缓冲和故障转移安全性；
- `firstOutputMs/Kind`：控制跨模态首输出；
- `firstTokenMs`：控制 TTFT 和文本调度；
- `terminalSeen`：控制完成、usage drain 和错误语义。

任何路径不得再使用 `firstTokenMs == nil` 同时代表上述状态。

## Protocol observations

- Responses：非空 text/reasoning/tool delta 同时构成 token 与 output；音频 transcript delta 是文本 token/output；图片/音频媒体 partial 或最终媒体只构成 output；文本 annotation、工具列表、工具结果和加密推理等元数据/聚合项只构成 output；生命周期和空终态均不构成输出。
- Chat：非空 content/refusal/reasoning/tool name/arguments 构成 token 与 output；role-only、空 delta、finish-only、usage-only 和 `[DONE]` 不构成输出。
- Anthropic/Bedrock：非空 text/thinking/input_json delta 或有名字的 tool_use/server_tool_use 构成 token 与 output；citation、签名、脱敏推理和服务端工具结果只构成 output；message_start/message_stop 不构成输出。
- Gemini：非空 text、function call、可执行代码或已识别的图片/音频 part 构成输出；grounding/citation 元数据、代码执行结果和仅签名推理只构成 output；finish reason、usage 及无法映射到现有 kind 的其他媒体不构成输出。
- Images：首个非空 partial image 构成 output；没有 partial 时，首个非空最终图片构成 output，即使它只出现在 terminal 中。

## Conversion visibility

协议转换路径在转换后观察下游事件。Responses `response.created` 转成的 Chat role chunk不构成输出。若 terminal 携带此前未流出的文本/工具数据，转换器可在去重后补发下游内容，再记录首输出。

Anthropic 转 Responses/Chat 的兼容路径只能观察转换器确实能够表达的事件。当前转换器会丢弃的 signature、citation、redacted thinking、server tool result 等 Anthropic 专有数据不得触发首 Token 或首输出。

Chat 上游的官方 `refusal` delta 与兼容期内仍可能出现的旧式 `function_call` delta 必须转换为下游可见文本或工具调用，不能仅在原始 Chat 透传路径计时后由 Responses/Anthropic 回退链丢弃。

Chat Completions 没有通用的图片生成输出格式。Responses→Chat 或 Chat endpoint 上的 Responses-shaped 生图请求必须路由到能保持 Responses/Images 协议的路径，或返回明确的 unsupported error；不得静默删除工具或图片事件。

## WebSocket turns

首轮开始时间在 adapter 首次上游写之前捕获并传入 relay。后续轮在通过策略检查的 `response.create` 写上游前创建 pending timing，写失败时撤销。首个带 response ID 的上游事件绑定 pending timing；terminal 删除 timing并产生 per-turn result。跨 goroutine 状态必须同步。

该统计范围只覆盖网关能够观察到实际输出载荷的 Responses WebSocket v2。`/backend-api/codex/realtime/calls` 的 Live 会话通过 WebRTC 向客户端传输音频/媒体，Sub2API 的 sideband WebSocket 只承载控制事件，无法确定客户端收到首个媒体帧的时刻。因此 Live 记录仅保留会话级 `duration_ms`，不根据 sideband 事件设置 `first_token_ms`、`first_output_ms` 或 `first_output_kind=audio`。

## Persistence and rollout

新增字段可空，不回填历史数据。新流式记录在识别到输出时写 `first_output_ms/kind`；严格 token-like 输出另写 `first_token_ms`。Ops 新语义查询只纳入带 `first_output_kind` 的新记录，避免与 legacy 首事件数据混合；过滤条件不能限定首输出模态，因为混合模态流可能先出图片/音频、随后才产生有效首 Token。升级迁移同时将现有小时/日派生聚合及系统指标快照中的旧 TTFT 列置空，将存在样本数字段的聚合样本数归零，其他指标保持不变；后续采集与定时聚合按新语义写入或重建仍可由原始用量日志判定的数据。

使用记录保留历史 `first_token_ms` 原值，但当 `first_output_kind` 为空时必须标示为旧版首事件，不能把它展示成新口径的严格首 Token。CSV/Excel 表头也必须说明该列可能承载旧版首事件。
