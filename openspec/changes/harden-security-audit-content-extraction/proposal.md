## Why

安全审计入口虽然位于账号选择、计费和上游调用之前，但 Content Moderation 与 Prompt Audit 使用两套独立协议解析器。`v0.1.178+custom.001` 扩展 API Key 自定义工具和 Responses WebSocket 工具桥接后，真实流量开始大量携带 `function_call_output.output`、`custom_tool_call_output.output`、官方 `tool_search_output.tools`、shell/computer/MCP/PTC 结果及 reusable prompt variables，两套解析器会将部分内容承载轮次误判为无文本。Alpha Search 的 `commands.search_query[].q` 也未被任何解析器覆盖。

这属于安全边界缺陷：审核 gate 被调用不等于内容已被审核，而且账号类型、会话黏性和协议适配会放大解析覆盖缺口。需要用共享的规范化提取契约消除双实现漂移，并让内容承载但提取失败的请求可观测、在阻断模式下 fail closed。

## What Changes

- 新增独立共享的安全审核内容提取包，统一解析协议、角色、当前请求增量、instructions、工具定义、工具结果和文本来源。
- Content Moderation 与 Prompt Audit 共同消费共享提取结果；两者保留各自的风险模型、配置、持久化和副作用。
- 覆盖 Responses HTTP/WS 顶层、`response.*`、session-update 载荷、`prompt.variables`、官方 shell/computer/MCP/PTC/tool-search 形态、Alpha Search、Live 初始 session、新旧转录 prompt/keywords 与每个 Sideband/Responses passthrough 客户端数据帧、Embeddings 字符串数组，以及 Chat/Anthropic/Gemini 工具结果。
- 结构化文本审核前剥离媒体 URL、data URL、长 base64 和加密载荷；Content Moderation 图片路径复用 canonical current 规则。
- Content Moderation 提取不可用在协调器中分类为 unavailable；confirmed policy match 才分类为 block。
- 将入口当前增量（包括伪装为 assistant/model 的消息）和工具结果视为客户端控制输入；阻断模式的 latest-turn 策略必须优先扫描当前增量，而不是回退到历史用户消息。
- 区分显式控制帧与内容承载载荷。请求根对象、嵌套 response/session 和转录配置的未知非空兄弟字段，以及结构化内容规范化或序列化失败，都必须产生不完整提取；启用阻断审核时必须在任何账号、计费或上游副作用前拒绝。
- 增加真实协议载荷的双引擎语义测试和提取尝试、成功、空内容、失败计数。
- 固化仓库级不可绕过安全审核规约，使任何协议、账号、会话、路由或转换变更必须同步更新覆盖矩阵和测试。

## Capabilities

### New Capabilities

- `security-audit-content-coverage`: 定义共享内容提取、协议覆盖、工具结果处理、内容承载分类、阻断失败语义和可观测性。

### Modified Capabilities

- `prompt-input-audit`: Prompt Audit 改为消费共享规范化内容段。
- `prompt-input-guard`: 阻断模式对内容承载提取失败执行 fail closed，并覆盖工具结果续轮。

## Impact

- 后端新增 `backend/internal/auditcontent/`，修改 Content Moderation、Prompt Audit 及相关测试。
- 管理运行态 API 增加提取覆盖计数，不新增配置或数据库迁移。
- 现有安全审核开关、Moderations 阈值、Prompt Guard 分类、数据库表和外部请求 schema 不变。
- 阻断审核启用时，过去被静默放行的内容承载解析失败请求将返回既有安全审核不可用语义；观察模式继续放行但必须计数和告警。
