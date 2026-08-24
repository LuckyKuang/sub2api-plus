## Context

网关已在各协议 Handler 中统一调用安全审核协调器，但 Content Moderation 使用 gjson 的 latest-item 解析器，Prompt Audit 使用 `encoding/json` 的完整转录解析器。两者对协议字段、工具结果和 WebSocket envelope 的支持不一致。仅补字段会继续保留双实现漂移，无法满足安全边界长期要求。

## Decisions

### 1. 使用独立共享规范化包

新增 `internal/auditcontent`，只依赖标准库并暴露稳定的协议无关结果：

```text
Document{Segments, Images, ContentBearing, Incomplete}
Segment{Text, Role, Source, Current, ClientControlled}
Image{URL, Role, Source, Current, ClientControlled}
```

`Source` 区分 message、instruction、tool_definition、tool_output、tool_call、search_query、embedding_input、media_prompt、prompt_variable 和 reasoning。`Current` 标记本次新增增量；`ClientControlled` 标记应在 Prompt Audit 最新轮阻断策略中优先扫描的内容。入口角色标签不构成 Prompt Audit 的信任边界，当前 assistant/model 段同样属于客户端控制内容。图片保留与所属内容项相同的角色、来源和当前轮属性，避免文本和图片使用两套协议解析器。

共享包不依赖 `service` 或 `securityaudit`，避免包环；它不执行风险判断、脱敏、持久化、采样或副作用。

### 2. 两套引擎共享解析但保留选择策略

Prompt Audit 消费所有规范化段，并继续按最新客户端输入优先生成快照。启用 `blocking_latest_turn_only` 时，选择当前客户端控制段、当前 instructions/工具定义上下文及其最近的历史 assistant/model 输出。

Content Moderation 在同一规范化结果上执行独立的直接用户归因选择，恢复 `v0.1.177+custom.003` 的产品语义：普通消息协议只选择最后一个当前直接用户消息中的文本和图片；Chat/Anthropic 要求显式 `user` 角色，Responses/Live/Gemini 允许其协议定义的无角色用户简写。Alpha Search 查询、Embeddings 字符串和 Images/media prompt 属于直接用户输入。instructions、system/developer 上下文、assistant/model 消息、reasoning、工具定义/调用/结果、审批响应、reusable prompt variables 及其图片均不进入 Content Moderation，避免把平台或外部内容归因为用户违规。它们仍完整保留给 Prompt Audit。

规范化成功但没有直接用户内容时，Content Moderation 产生合法空选择并跳过外部 Moderations 请求。`Incomplete=true` 的检查发生在引擎选择之前，因此未知内容或部分提取仍按配置模式记录失败或 fail closed，不能以空选择绕过。

### 3. 明确协议覆盖

- Chat Completions：instructions、工具/函数定义、messages 文本；最后 message 为当前增量；tool content 和 tool call arguments 可审核。
- Anthropic Messages：system、工具定义、messages/thinking、客户端或服务端 tool_use input、tool_result content；最后 message 为当前增量，redacted_thinking 的密文不作为文本。
- Responses HTTP/WS：顶层或 `response` 内的 instructions/tools/input、`prompt.variables`；支持 text/content/reasoning/refusal、function/custom/tool-search、local/hosted shell、apply-patch、computer、MCP、code-interpreter、program/program-output 与 additional-tools；尾部连续结果共同视为当前增量。官方 client tool search 使用 `tool_search_output.tools`，兼容形态可使用 `output`。
- Gemini：system instruction、工具定义、contents/content/requests/instances、functionCall args、functionResponse response；最后 content 为当前增量。
- Alpha Search：`commands.search_query[].q`。
- Live：初始 session instructions/input，以及 Sideband 的 session.update、conversation.item.create、response.create；显式音频缓冲、取消、删除、截断、读取和关闭事件为控制帧。Sideband 每个客户端文本帧在写上游前独立审核。
- Embeddings：input 字符串或字符串数组。
- Images/media：确定性的 prompt 类文本键，排除 URL 和大块媒体载荷。

结构化工具参数和输出先移除图片/文件 URL、data URL、长 base64、encrypted content、computer screenshot 和 image-generation binary result，再采用确定性 JSON 编码审核；同一结构中的普通文本必须保留。这样 Prompt Audit 不会持久化媒体或不透明载荷，规范化图片仍保留来源归因；Content Moderation 只选择直接用户图片，不选择工具输出或 prompt variable 图片。

已识别的纯图片块属于显式无文本；其中只有当前直接用户图片由 Content Moderation 的图像输入审核。Embeddings token-ID 数组无法在无模型 tokenizer 的共享层可靠还原，因此不得把数字 JSON 当作已审核文本；阻断模式按提取失败处理。

### 4. 内容承载分类与失败语义

共享解析器在认识到协议字段或内容项存在时设置 `ContentBearing=true`。只有已明确分类且实际不含内容字段的 WebSocket 控制帧、空控制对象和没有内容字段的请求可以是非内容承载；顶层 `type` 值不能压制同一载荷中实际存在的 `input`、`instructions` 或嵌套 `response.input`。

已知消息、item 和 control 类型使用允许字段集合。任何未知非空兄弟字段都会设置 `Incomplete=true`，不能被已经成功提取的正文掩盖。若 `Incomplete=true`，或 `ContentBearing=true` 但既没有可审核文本段也没有已识别图片，即使同一载荷的其他兄弟项已成功提取也属于提取失败。规范化成功后，某个引擎因自身归因策略选择为空不属于提取失败：

Responses HTTP 根对象、嵌套 `response`、Responses/Live session 及 Live 转录配置都遵循相同规则。Live 初始 HTTP session 和 Sideband 必须选择 `openai_live` 适配器；适配器同时审核旧式 `input_audio_transcription.prompt`/`keywords` 与现行 `audio.input.transcription.prompt`/`keywords`。结构化内容清洗后的 JSON 序列化失败同样设置 `Incomplete=true`，不得静默返回。

- Content Moderation observe 模式记录 extraction failure 后继续请求。
- Content Moderation pre_block 模式返回 503 并阻止后续阶段。
- Prompt Audit async 模式计入 dropped/extraction failure，不创建空任务。
- Prompt Audit blocking 模式使用既有 invalid-response 503 语义阻止后续阶段。

无效 JSON 继续由 Handler 基础校验优先拒绝；共享解析器仍返回显式错误供单元测试和防御性调用处理。
Content Moderation 的 503 使用 `content_moderation_unavailable`；命中内容策略才使用 `content_policy_violation`。
协调器把前者映射为 `DecisionUnavailable` 并保留原始 503、错误码和 Legacy facts；只有确认的引擎策略命中才映射为 `DecisionBlock`。

### 5. 可观测性

两套运行态分别暴露 extraction_attempted、extraction_succeeded、extraction_empty 和 extraction_failed。日志保留 endpoint、protocol、stage、body_bytes 和错误码，不记录原始内容。

### 6. 验证策略

建立真实载荷表驱动测试，并对每个载荷同时断言：

- 共享解析结果非空且当前增量正确。
- Content Moderation 对直接用户载荷输入非空，对 instructions、assistant/model、工具结果和 prompt variables 为空。
- Prompt Audit 快照保持非空，latest-turn 策略包含当前工具/搜索内容。
- HTTP 与 WebSocket 审核仍位于账号、计费、并发和上游副作用之前。
- Live Sideband 的客户端帧审核 hook 位于 `upstream.WriteFrame` 之前。
- Responses passthrough 的所有客户端数据帧进入审核 hook；非 create 帧被拒绝时上游写计数为零。
- compact keepalive 和 channel mapping 在审核通过后启动；审核读取不可变的原始请求体，协议归一化不能删除被审核字段。

现有只验证 AST 调用顺序的测试保留，但不能作为内容覆盖的唯一证据。

## Rejected Alternatives

- 分别给两个解析器补字段：短期改动小，但会继续产生协议漂移，违反新的仓库级安全审核契约。
- 回滚会话黏性或 API Key 工具支持：只能降低触发量，旧版本仍存在相同解析缺陷。
- 对所有空提取全局 fail closed：会误伤合法控制帧，必须先由协议适配器分类内容承载状态。
