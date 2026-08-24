## Why

安全审计入口虽然位于账号选择、计费和上游调用之前，但 Content Moderation 与 Prompt Audit 使用两套独立协议解析器。`v0.1.178+custom.001` 扩展 API Key 自定义工具和 Responses WebSocket 工具桥接后，真实流量开始大量携带 `function_call_output.output`、`custom_tool_call_output.output`、官方 `tool_search_output.tools`、shell/computer/MCP/PTC 结果及 reusable prompt variables，两套解析器会将部分内容承载轮次误判为无文本。Alpha Search 的 `commands.search_query[].q` 也未被任何解析器覆盖。

这属于安全边界缺陷：审核 gate 被调用不等于内容已被审核，而且账号类型、会话黏性和协议适配会放大解析覆盖缺口。需要用共享的规范化提取契约消除双实现漂移，同时保持 `v0.1.177+custom.003` 的兼容语义：成功提取的内容继续审核，未知或提取失败本身可观测但直接放行。

## What Changes

- 新增独立共享的安全审核内容提取包，统一解析协议、角色、当前请求增量、instructions、工具定义、工具结果和文本来源。
- Content Moderation 与 Prompt Audit 共同消费共享提取结果，但保留不同选择策略：Content Moderation 只归因当前直接用户文本/图片，Prompt Audit 保持完整规范化覆盖。
- 覆盖 Responses HTTP/WS 顶层、`response.*`、session-update 载荷、`prompt.variables`、官方 shell/computer/MCP/PTC/tool-search 形态、Alpha Search、Live 初始 session、新旧转录 prompt/keywords 与每个 Sideband/Responses passthrough 客户端数据帧、Embeddings 字符串数组，以及 Chat/Anthropic/Gemini 工具结果。
- 结构化文本审核前剥离媒体 URL、data URL、长 base64 和加密载荷；规范化结果为图片保留角色、来源和当前轮归因，Content Moderation 只选择直接用户图片。
- Content Moderation 和 Prompt Audit 的提取异常只影响指标和日志；只有 confirmed policy match 才分类为 block，独立审核依赖异常继续使用既有 unavailable 语义。
- 将入口当前增量（包括伪装为 assistant/model 的消息）和工具结果视为 Prompt Audit 的客户端控制输入；阻断模式的 latest-turn 策略必须优先扫描当前增量，而不是回退到历史用户消息。Content Moderation 排除 assistant/model、instructions、工具内容和 reusable prompt variables，避免将平台或外部内容报告为用户违规。
- 未知 item type、未知 Responses/Live 帧、未知兄弟字段、合法 JSON 的未识别结构以及结构化内容规范化或序列化失败直接放行；同一载荷中已成功提取的内容仍进入两个审核引擎。
- 增加真实协议载荷的双引擎语义测试和提取尝试、成功、空内容、失败计数。
- 固化仓库级不可绕过安全审核规约，使任何协议、账号、会话、路由或转换变更必须同步更新覆盖矩阵和测试。

## Capabilities

### New Capabilities

- `security-audit-content-coverage`: 定义共享内容提取、协议覆盖、工具结果处理、兼容放行语义和可观测性。

### Modified Capabilities

- `prompt-input-audit`: Prompt Audit 改为消费共享规范化内容段。
- `prompt-input-guard`: 阻断模式继续审核成功提取的内容，但提取失败或空选择本身直接放行，并覆盖工具结果续轮。

## Impact

- 后端新增 `backend/internal/auditcontent/`，修改 Content Moderation、Prompt Audit 及相关测试。
- 管理运行态 API 增加提取覆盖计数，不新增配置或数据库迁移。
- 现有安全审核开关、Moderations 阈值、Prompt Guard 分类、数据库表和外部请求 schema 不变。
- 所有模式下的内容提取失败都直接放行并记录安全结构化日志；确认的内容策略命中和独立审核依赖异常保持既有决策语义。
