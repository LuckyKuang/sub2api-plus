# Codex 上游错误提示实施规格

**交付状态：待实现。** 本文是实现和验收的目标契约，包含完整默认文案和配置标识。当前版本的运行行为见[现行 Responses 协议](OPENAI_RESPONSES.md#downstream-session-error-wording)。范围按最终使用的 OpenAI 凭据归属判定，覆盖 OAuth（含复用 OAuth 路径的 setup-token）、API Key 和自定义 API Key 地址。表内来源包括仓库已识别的信号、官方客户端和 API 声明的信号，以及建议补上的兜底。兼容地址可以返回任意字节，因此“全量覆盖”的验收标准是每个已确认的失败都有安全出口。

## 实施顺序

1. 落实后端错误目录和分类。用下表直接编写期望值，覆盖全部 A/B/C/D 场景以及 C09 的每个分支。客户端只拿到终局分类，服务端按原始原因执行既有的恢复、调度、审计和计费。
2. 实现管理员读写和运行时配置，再接入「系统设置 → 出站身份 → Codex」。用两个管理会话验证保存、冲突、重载和恢复默认。重启实例后仍能读回覆盖，更新默认文案后未覆盖项自动采用新值。
3. 逐一接入实时网关和历史查询出口。用带私有诊断标记的同一组输入，分别验证用户可见输出和管理员 Ops 记录。对 HTTP 头已提交、SSE/WS 分帧、CLI 消费和异步任务单独验收。
4. 在项目规定的容器环境执行末节验收；随实现更新现行协议、界面文字、测试矩阵和发布说明。工作树需要本地 OpenSpec 计划时引用本文，默认文案与配置键以本文为准。[仓库验证与 OpenSpec 约定](../../CONTRIBUTING.md#specifications)

## 现行行为与目标

- 现有设置默认关闭。打开后只认 `upstream_response_interrupted`、`upstream_unavailable`、`session_account_lost` 三个码，前两个共用“输入「继续」”提示。未知码、非字符串码、缺少字符串 message、非 JSON 输入等会从文案转换函数原样退出，之后用户看到什么取决于其他路径是否已经做过清洗。[文案实现](../../backend/internal/service/openai_error_messages.go#L20) · [现有协议](OPENAI_RESPONSES.md#downstream-session-error-wording)
- 确定性 400 会取清洗过的上游 message，并保留上游 type、code、param。模型不可用最终分支可以使用上游文字。管理员透传规则也能提取上游消息。容量耗尽的 WS 分支单独写帧和关闭原因，Live sideband 直接写上游帧。这些出口均需纳入。[400 写出](../../backend/internal/service/openai_upstream_client_error.go#L38) · [故障转移耗尽](../../backend/internal/handler/openai_gateway_handler.go#L3890) · [WS 写出](../../backend/internal/handler/openai_gateway_handler.go#L4430) · [Live 转发](../../backend/internal/service/openai_live.go#L868)
- 用户错误请求页是独立的读取出口。打开该页的访问权限后，列表取 Ops 记录的 `message`，详情还取记录的 `error_body` 和 `upstream_status_code`。即使生成接口已经显示统一提示，用户仍可能从这里看到归一化之前的诊断。建议对归属 OpenAI 的记录复用同一公开分类，历史记录证据不足时走安全兜底。详情可保留既有 `error_body` 结构来承载重建的公开错误，只显示公开状态。管理员 Ops 详情继续按现有脱敏和权限供排查。[用户字段映射](../../backend/internal/service/ops_user_error.go#L97) · [用户错误详情入口](../../backend/internal/handler/usage_handler.go#L356)
- 设置项命名为「上游错误提示」，由系统设置管理员为全站配置。网关始终按下表分类。旧开关升级为「使用自定义提示」。关闭时每项使用自己的内置默认文案。开启时，已配置的项使用自定义文案，其余项取内置默认。升级时已有开关取值可沿用，两种状态在尚无自定义内容时都立即获得整套分类默认提示。旧版本关闭时可能透传原文，这一升级行为需在设置说明和发布说明中写明。
- 上游失败在错误来源处分类。区分真实 HTTP 状态、HTTP 200 内的协议错误、握手或读写错误、输出进度，以及传输是否已提交。恢复成功时丢弃该次失败。仅最终失败进入用户返回，按请求、WS turn 及协议支持的 stream 分开保存。不能用已经被改写的英文提示反推原因。

## 自定义文案与恢复默认

配置入口沿用「系统设置 → 出站身份 → Codex」。站点管理员统一配置，一次保存适用于本站所有经 OpenAI OAuth 或 API Key 转发的调用。内置默认文案逐字取自下表。界面语言切换只翻译管理控件，发送给调用方的文字取编辑器展示的有效文案。

| 管理员操作 | 保存后的结果 |
|---|---|
| 首次进入，尚未自定义 | 页面列出完整目录和各项默认文案，网关已经按错误原因输出默认提示。 |
| 只修改一个错误 | 默认模式下主按钮为「保存并启用自定义提示」。保存文案和启用模式一并生效。已启用时按钮为「保存更改」。其余错误继续使用各自有效文案。 |
| 临时关闭「使用自定义提示」 | 所有返回使用分类默认文案。保留已保存的修改，以后可以重新启用。 |
| 点击某行「恢复默认」 | 该条草稿恢复内置内容，保存后移除这一项的覆盖。其他项照常。 |
| 点击「全部恢复默认」 | 显示将恢复的已自定义条目数，按完整目录计数；保存后移除全部覆盖，仅限本 Codex 文案设置。 |
| 放弃编辑 | 编辑、恢复和模式切换均属于草稿。点击「取消」即可保留当前线上配置。 |

**编辑界面。** 表格按请求与会话、服务与额度、传输、媒体等分组，可搜索错误码或原因，也可只看已自定义项。每行显示错误原因、公开 code、当前生效的提示、文案来源和编辑入口。打开编辑器时并列显示只读默认文案和「自定义提示」输入框，预览用该项实际要发出的文字；停用自定义时区分“已保存的自定义文案”和“当前生效文案”。

例如，`rate_limit_exceeded` 默认显示「当前请求受到频率限制，请稍后再试。」管理员可改成「请求较多，请稍后重试。如持续出现，请联系站点客服。」保存后，OAuth 和 API Key 均返回新文字，HTTP 仍为 429，公开码仍为 `rate_limit_exceeded`。单项恢复并保存后，两条路径重新返回默认文字。管理员变更记录应能核对操作人、时间、受影响的配置项，以及修改或恢复动作。

**配置粒度。** 下列 57 个审计场景用来核对行为。可编辑目录按稳定的“公开错误码及其具体含义”生成。一个场景有多个公开码时拆为多项，即使默认文字相同也能分别编辑，如 `invalid_audio_data` 与 `invalid_audio_format`。若协议为兼容性复用一个公开码，界面用原因区分，例如「模型繁忙」与「服务内部错误」都可输出 `server_error`，但各自有独立文案。多个上游别名映射为同一含义、同一公开码时共用一项。未知错误使用对应兜底项，它们也可编辑。只在已有展示位置呈现 D13–D16 的终态说明。

**存储与生效。** 后端维护一份内置目录，每项有稳定配置键、原因、公开码和默认文案。管理界面和网关都从该目录取默认值。复用系统设置存储，只持久化模式和实际覆盖的文案。恢复默认即删除覆盖，所以下次版本修订默认时，使用默认的项自动更新，已自定义的项继续保留。保存仅提交修改过的条目，删除覆盖有明确操作；普通系统设置保存若未涉及文案，不可清空它。模式和覆盖在同一次写入中生效，同时编辑同一项时提示重新载入，避免旧页面覆盖新值。

**文案边界。** 自定义输入为普通文本，保存时检查空白、长度和控制字符；空白草稿可以通过「恢复默认」清除。系统不执行 HTML、脚本或模板插值，也不接受上游 message、body、headers 作为变量。管理员能编辑展示文字，HTTP 状态、协议 code/type、重试、换号、计费与安全审计由各自的协议规则决定。现有错误透传规则命中该范围时，最终展示仍取本目录对应项。HTTP、SSE、WS、CLI 桥接以及用户侧错误详情复用同一解析结果；WS 关闭原因按协议字节上限保留完整 UTF-8 字符，有错误事件可用时在事件中发送完整文案。

**配置故障与验证。** 保存失败保留上一套已生效配置；进程首次取不到有效配置时使用内置目录。复用现有设置变更通知与后台刷新，使保存后同实例后续尚未发出的错误使用新版本，其他实例按现有机制同步。同一次失败的错误事件和关闭原因使用同一配置快照。验收覆盖首次默认、只改一项、同码不同原因、多个码默认相同、未知错误兜底、切换停用再启用、单项与全部恢复、取消、保存失败和升级继承。[现有设置刷新机制](../../backend/internal/service/setting_codex_safety_buffering.go#L48)

## 管理接口和持久化

实现以下管理子资源，复用系统设置的管理员权限、成功/错误响应包装和配置审计。请求与响应示例展示包装内的业务字段。普通用户的网关 Key 不能读取和修改此设置。

| 操作 | 契约 |
|---|---|
| `GET /api/v1/admin/settings/codex-error-messages` | 返回 `revision`、`custom_enabled`、完整 `items`。每项含 `key`、`group`（request/service/transport/media，对应 A/B/C/D）、`public_code`（仅展示终态时为 null）、`default_message`、`custom_message`（无覆盖时为 null）、`effective_message`。界面按稳定键显示本地化原因标签。C09 复用已有项时只展示一次。 |
| `PATCH /api/v1/admin/settings/codex-error-messages` | 提交读取时的 `revision`，以及可选的 `custom_enabled` 和 `changes`。例如 `{"revision":"r7","custom_enabled":true,"changes":{"rate_limit_exceeded":"调用频率较高，请稍后再试。","invalid_audio":null}}`。字符串写覆盖，null 删除覆盖，缺失键保留。成功返回与 GET 相同形状的新快照。 |
| 恢复全部 | 根据读取的完整快照，将每个已覆盖键放入同一个 `changes` 并设为 null。一次保存，独立于页面筛选、搜索或分页，模式维持用户选择。 |
| 校验和冲突 | 同项修改、模式变化或恢复全部遇到过期 revision，整次写入返回 409，保留草稿以便重新载入核对。空白、非法控制字符、未知键或错误类型返回 400 并标明出错项。文案按修剪首尾空白后 1–500 个 Unicode 码点校验，采用单行纯文本，序列化和 UI 渲染按文本处理。每个键逐字保存，预览与实际正文一致。 |

模式沿用系统设置 `openai_error_messages_normalized`。新设置 `openai_error_message_overrides` 仅存已覆盖键到文案的 JSON 对象，缺失视为 `{}`。沿用旧布尔值：缺失或 false 走分类默认，true 优先使用已保存覆盖。旧客户端显式修改旧布尔字段只改变模式，未传字段保留；新界面在本节专用接口保存，页面通用设置保存要省略该字段，避免旧表单回写覆盖刚保存的模式。旧透传规则的 CustomMessage 不自动导入，没有可靠的一对一目录映射。

数据库内原子比较当前 revision 并提交模式和覆盖；跨实例的并发校验须使用同一数据库写入边界。可采用整个配置的一致性版本，过期修改均返回 409。变更公告在提交成功后发出。读取新快照失败时保留最后一次成功加载的配置，首次无有效快照时用内置默认；未知的历史覆盖键不参与渲染。模式、目录和覆盖按同一快照读取。[设置存储](../../backend/internal/repository/setting_repo.go) · [现有管理员设置路由](../../backend/internal/server/routes/admin.go) · [现有设置保存与通知](../../backend/internal/service/setting_codex_safety_buffering.go)

## 审计表的读法

“HTTP”是普通请求在响应头尚可改写时的目标值。已提交的 SSE 使用当前端点的失败事件；已升级的 WS 使用该协议的错误帧或关闭语义；任务查询可在 HTTP 200 内携带任务失败。表中的 HTTP 值不用于重写已经发送的状态。公开码在支持 `error.code` 的协议中使用；“同码”只指该行逐字列出的允许值，绝不接受任意匹配前缀。Messages 等协议使用自身允许的错误 type。

未单列的 OpenAI JSON `error.type` 按语义取固定值：请求修正取 `invalid_request_error`，请求级权限取 `permission_error`，资源不存在取 `not_found_error`，短时限流取 `rate_limit_error`，服务不可用取 `service_unavailable_error`，内部处理和传输故障取 `server_error`。已有端点或官方客户端依赖的专用 type 在对应协议适配器内保留。各表行号用于审计意见。

每个稳定配置键独立编辑。多个键共用一个默认句子时分别继承该句。A16 的三句按该行的三个键从左到右对应。C09 详见其子表。D13–D16 仅在端点已有原因展示位采用引号内原句，保留协议终态。B01/B02 的对外码相同，管理员按“凭据失效”和“访问受限”区分两项。B07/C08 的对外码也相同，分别使用“模型繁忙”和“服务内部错误”两项。

表中给出多个 HTTP 值时，已有端点的有效状态在列内则沿用。首次合成且端点无更具体契约时取该行第一个数值；A08 无状态的模型错误按现有协议取 400。A12 已有安全的失败状态沿用，必须从零生成未提交的 HTTP 失败时取 503。D13–D16 的协议终态优先于 HTTP 状态兜底。已确定的本地用户错误始终使用本地处理器，包括入站鉴权、余额和安全审计。[模型状态判定](../../backend/internal/service/model_not_found_error.go#L33)

### 请求、模型和会话

| 编号 | 稳定配置键 | 进入条件或上游代表码 | HTTP | 对外 code | 内置默认文案 |
|---|---|---|---:|---|---|
| A01 | `missing_required_parameter` | 缺必填项，如调用方负责提供的 instructions；`missing_required_parameter` | 400 | `missing_required_parameter` | 请求缺少必填参数，请补全后重新提交。 |
| A02 | `invalid_parameter` | 参数值、类型、范围、枚举、工具声明、JSON Schema 无效；语义明确的 400/422 | 400 或 422 | `invalid_parameter` | 请求参数无效，请检查参数值和格式后重新提交。 |
| A03 | `unsupported_parameter` | 已明确为当前模型不支持的调用方参数、功能或组合；已确认不支持的工具或推理选项 | 400 | `unsupported_parameter` | 当前模型不支持所选请求选项，请调整选项或更换模型。 |
| A04 | `invalid_prompt` | `invalid_prompt` | 400 | 同码 | 输入内容无效，请检查后重新提交。 |
| A05 | `context_length_exceeded` | `context_length_exceeded`、`context_too_large`、已识别的上下文长度错误 | 400 | `context_length_exceeded` | 会话内容超出模型的上下文长度，请精简历史或新开会话。 |
| A06 | `request_too_large` | 请求体 413、已识别的 507 重试缓冲上限、请求帧过大且 HTTP 回退也不能完成 | 413 | `request_too_large` | 请求数据过大，请减少输入内容或附件后重新提交。 |
| A07 | `budget_reached` | `budget_reached`；400 或无冲突状态的错误事件 | 400 | 同码 | 当前会话已达到预算上限，请检查会话预算设置。 |
| A08 | `model_not_found` | `model_not_found`；已识别的 `model_not_available`、`unsupported_model`、`invalid_model` 确认为模型不可用 | 400 / 403 / 404，沿用模型契约 | `model_not_found` | 所选模型当前不可用，请更换模型。 |
| A09 | `invalid_access_program`、`unsupported_access_program`、`access_program_not_enabled`、`service_tier_unavailable` | `invalid_access_program`、`unsupported_access_program`、`access_program_not_enabled`；明确由调用方选择的不允许的 service tier | 400 或 403 | 前三个同码；服务层级用 `service_tier_unavailable` | 当前模型不可使用所选服务选项，请调整选项或更换模型。 |
| A10 | `resource_not_found` | 明确属于调用方资源的文件、附件、向量库或引用资源不存在、已删除或过期 | 404 | `resource_not_found` | 引用的资源不存在或已过期，请重新上传或选择资源。 |
| A11 | `previous_response_not_found` | `previous_response_not_found`、已确认无法使用的上一轮引用；网关尝试恢复后仍失败 | 400 | `previous_response_not_found` | 找不到上一轮响应，请携带所需历史重新提交，或新开会话。 |
| A12 | `session_account_lost` | `session_account_lost`，明确原会话已经失去可用绑定 | 沿用该终态原有状态 | `session_account_lost` | 当前会话可能无法恢复，请新开会话继续。 |
| A13 | `invalid_encrypted_content` | `invalid_encrypted_content`；或满足现有严格条件的 `thinking_signature_invalid`，恢复失败 | 400 | `invalid_encrypted_content` | 会话历史无法验证，请新开会话并重新提供必要内容。 |
| A14 | `invalid_tool_history` | 明确为工具调用与结果无法配对、缺少必需调用记录、引用失效 | 400 | `invalid_tool_history` | 工具结果与调用记录不匹配，请补全本轮记录或新开会话。 |
| A15 | `request_conflict` | 同一响应正在运行、操作与资源当前状态冲突；409 或明确等价信号 | 409 | `request_conflict` | 当前操作与已有请求冲突，请确认其状态后再提交。 |
| A16 | `invalid_stream_id`、`websocket_stream_limit_reached`、`websocket_connection_limit_reached` | `invalid_stream_id`；`websocket_stream_limit_reached`；`websocket_connection_limit_reached` | WS 事件 400 | 三个同码，各用对应提示 | 流标识无效，请更正后重试。／此连接已达到流数量上限，请复用已有流或重新连接。／当前连接已到使用时限，请重新连接后继续。 |
| A17 | `transport_unavailable` | 上游拒绝 WS 协议、`websocket_not_supported`、`websocket_unsupported`、`upgrade_required`、协商或能力回退后仍不可用 | 503 | `transport_unavailable` | 当前连接方式暂不可用，请改用其他受支持的连接方式或稍后再试。 |
| A18 | `unsupported_media_type` | 上游明确拒绝请求的媒体类型或编码（415），且调用方能修改 | 415 | `unsupported_media_type` | 请求的内容格式不受支持，请更换格式后重新提交。 |
| A19 | `continuation_unsupported` | `openai_http_continuation_unsupported`，服务不能按此方式处理 HTTP 续接，且没有可用回退 | 400 | `continuation_unsupported` | 当前请求方式不支持续接上一轮，请携带所需历史重新提交。 |

如果错误来自网关添加的参数、隐藏的路由配置、服务层级或代填字段，应按 B05 或 C08 处理。用户只能修正自己提交的内容。WS 单个流上的失败要绑定原 stream；连接寿命与账号并发限流分别处理。[模型和终止码契约](CODEX_CLIENT_PROFILES.md#november-18-protocol-compatibility) · [Codex 固定版本解析](https://github.com/openai/codex/blob/29eb79e9866b3262a777fdf8195c1b9e63684c91/codex-rs/codex-api/src/error.rs) · [OpenAI WebSocket 错误](https://developers.openai.com/api/docs/guides/websocket-mode)

### 凭据、配额、策略和容量

| 编号 | 稳定配置键 | 进入条件或上游代表码 | HTTP | 对外 code | 内置默认文案 |
|---|---|---|---:|---|---|
| B01 | `credential_unavailable` | 上游 401、OAuth access/refresh token 过期、撤销、重复使用或失效、`invalid_grant`；API Key 无效或被禁用；最终无法恢复 | 503 | `service_unavailable` | 服务暂不可用，请稍后再试。若持续出现，请联系管理员。 |
| B02 | `account_access_unavailable` | workspace/account/organization/org 被停用、禁用、暂停；`deactivated_workspace`；已确认上游凭据的 scope、IP、地区或组织访问限制；无请求级证据的上游 403 | 503 | `service_unavailable` | 服务暂不可用，请稍后再试。若持续出现，请联系管理员。 |
| B03 | `service_capacity_unavailable` | 供应侧账单或硬额度：402、`insufficient_quota`、`insufficient_balance`、`credit_balance_exhausted`、`*_spend_limit_exceeded`、已确认的 billing hard limit | 503 | `service_capacity_unavailable` | 当前服务额度不可用，请联系管理员。 |
| B04 | `feature_unavailable` | `usage_not_included`，服务渠道不具备所请求的能力或订阅权益 | 503 | `feature_unavailable` | 当前服务暂不提供此功能，请选择其他可用功能或联系管理员。 |
| B05 | `service_configuration_unavailable` | 供应侧数据驻留不匹配 `data_residency_mismatch`、渠道能力或配置错误；已知网关代填项被上游拒绝 | 503 | `service_configuration_unavailable` | 当前服务配置无法处理此请求，请联系管理员。 |
| B06 | `rate_limit_exceeded` | 短时速率限制、`rate_limit_exceeded`、RPM/TPM、供应侧临时并发限制；无更具体证据的 429 | 429 | `rate_limit_exceeded` | 当前请求受到频率限制，请稍后再试。 |
| B07 | `model_overloaded` | `server_is_overloaded`、Plus 已识别的 `slow_down` 容量分支、明确模型满载、529 | 503 | `server_error`（保留 Plus 的容量兼容行为） | 当前模型繁忙，请稍后再试或更换模型。 |
| B08 | `policy_capacity_unavailable` | 精确 `policy_capacity_unavailable`：原生 HTTP 503 或符合既有契约的 SSE/WS 事件 | 503 | 同码；type 为 `service_unavailable_error` | 当前请求所需的服务容量暂不可用，请稍后再试。 |
| B09 | `flex_unavailable` | `flex_unavailable`，HTTP 429 或相应协议事件 | 429 | 同码 | 当前服务层级容量不足，请稍后再试或调整服务层级。 |
| B10 | `content_policy_violation`、`image_content_policy_violation` | 请求级内容策略：`content_policy_violation`、`image_content_policy_violation`、`moderation_blocked`、明确的安全拒绝 | 400 或既有请求级 403 | 前两个同码；其余用 `content_policy_violation` | 此请求未通过内容安全检查，请调整提交内容。 |
| B11 | `cyber_policy`、`bio_policy`、`misalignment_policy_violation` | `cyber_policy`、`bio_policy`、`misalignment_policy_violation` | 沿用请求级拒绝状态；状态缺失时 400 | 三个同码，文案相同 | 此请求受到安全策略限制，无法完成。 |
| B12 | `inference_hook_denied`、`request_policy_denied` | `inference_hook_denied` 或有可靠证据的请求级工作区策略拒绝 | 403 | 前者同码；其余用 `request_policy_denied` | 当前访问策略不允许此请求，请联系管理员。 |
| B13 | `service_usage_limit_reached` | 已确认供应侧周期用量窗口用尽，如 OAuth 的 `usage_limit_reached`、`GoUsageLimitError` 附有效的窗口证据 | 429 | `service_usage_limit_reached` | 当前服务已达到本周期用量上限，请在额度恢复后重试。 |

凭据别名应在内部记录归因。B01 包括 `invalid_api_key`、`api_key_disabled`、`unauthorized`、`authentication_error`、`authentication_failed`、`unauthorized_error`、`invalid_token`、`access_token_invalid`、`token_revoked`、`token_invalidated`、`invalid_credentials`、`credential_invalid`、`token_expired`、`refresh_token_expired`、`refresh_token_reused` 和 `refresh_token_invalidated`。B02 包括可归因到供应侧的 `insufficient_scope`、`missing_scope`，以及 subject 与 deactivated/disabled/suspended 的既有组合。B03 需要额度证据；普通 429 进入 B06。本平台用户的余额、API Key、配额、分组或政策错误继续走其本地契约。[流式凭据识别](../../backend/internal/service/openai_gateway_passthrough.go#L1559) · [账户停用识别](../../backend/internal/service/openai_gateway_upstream_errors.go#L1135) · [额度类型](OPENAI_RESPONSES.md#upstream-capacity-shed)

B01–B05 对外统一为 503。把供应端硬额度错误从可能的 429 改成 503 会影响第三方 SDK 的重试、告警和统计，实施验收要分别覆盖 B03、B06 和 B13。短时限流保持 429。网关内部的换号、冷却、停用、恢复仍根据原始信号工作。客户端对部分码会显示自己的固定文字，表内中文是网关承诺发出的内容。[Plus 容量契约](OPENAI_RESPONSES.md#upstream-capacity-shed) · [Codex 流式错误解析](https://github.com/openai/codex/blob/29eb79e9866b3262a777fdf8195c1b9e63684c91/codex-rs/codex-api/src/sse/responses_error.rs)

B07 的对外 `server_error` 是 Plus 的兼容约定。固定版本 Codex 的 SSE 解析器把 `slow_down` 归入限流分支，模型过载另有分支。客户端最终怎样展示或重试要以真实 CLI 验收为准。[Plus 改写点](../../backend/internal/service/openai_gateway_passthrough.go#L1451) · [固定版本解析器](https://github.com/openai/codex/blob/29eb79e9866b3262a777fdf8195c1b9e63684c91/codex-rs/codex-api/src/sse/responses_error.rs#L82-L98)

### 传输、服务和未知错误

| 编号 | 稳定配置键 | 进入条件或上游代表码 | HTTP | 对外 code | 内置默认文案 |
|---|---|---|---:|---|---|
| C01 | `service_connection_failed` | DNS、TCP、TLS、代理认证 407、连接拒绝、握手失败、连接在开始响应前重置，最终不可用 | 502 | `service_connection_failed` | 暂时无法连接服务，请稍后再试。 |
| C02 | `response_timeout`、`vector_store_timeout` | 上游 408/504、`first_output_timeout`，等待响应头、首个有效输出或工具/检索结果超时；`vector_store_timeout` | 504 | 检索用 `vector_store_timeout`；其余用 `response_timeout` | 等待服务响应超时，请稍后再试。 |
| C03 | `upstream_response_interrupted` | `upstream_response_interrupted`、`upstream_http2_stream_error`、`upstream_stream_read_error`、`upstream_stream_truncated`、`stream_timeout`、无终止帧的 EOF；已向用户交付部分结果 | 流内；尚可写状态时 502 | `upstream_response_interrupted` | 本次响应已中断。请先检查已收到的内容，再决定是否继续。 |
| C04 | `upstream_unavailable` | `upstream_unavailable`、明确服务维护或暂时不可用的 503；尚无有效内容且无更具体原因 | 503 | `upstream_unavailable` | 服务暂时不可用，请稍后再试。 |
| C05 | `request_outcome_unknown` | 请求已送出，工具操作等可能发生，断开后无法确认执行结果 | 流内或 502 | `request_outcome_unknown` | 无法确认本次操作是否完成，请先检查执行结果，避免重复操作。 |
| C06 | `invalid_service_response` | HTTP 200 带明确失败信号、但结构无法解析；上游成功载体畸形、错误对象截断、响应过大 `response_too_large`、缺失必需结果或终态；尚未交付有效输出 | 502 | `invalid_service_response` | 服务返回的响应不完整，请稍后再试。 |
| C07 | `empty_response` | `openai_silent_refusal` 等已判定为空结果且没有明确政策信号；生图未产出进入 D12 | 502 | `empty_response` | 本次请求未获得有效结果，请调整请求或稍后再试。 |
| C08 | `server_error` | 上游内部错误、`server_error`、500、502、非容量 5xx 等；没有更具体的已知原因 | 502 | `server_error` | 服务处理请求时出错，请稍后再试。 |
| C09 | `request_rejected`、`service_error` | 已认定失败的未知 code/type、大小写变体、无 code/message、数组、非字符串、纯文本、HTML、空体、二进制；没有更具体证据 | 依下方兜底 | 依下方兜底 | 依下方兜底。 |

C09 按下列证据分流，未知状态且已交付部分内容时优先 C03，执行结果不明优先 C05。其余按真实 HTTP 状态兜底。已知来源和状态可复用其他行的配置键；C09 自身只新增 `request_rejected` 和 `service_error` 两项。

| 无法取得详细原因时的证据 | HTTP / code | 内置默认文案 |
|---|---|---|
| 上游明确拒绝请求内容，400/422 | 400 或 422 / `request_rejected` | 本次请求未被接受，请检查请求内容。 |
| 其他已知状态语义 | 401/403 → B01/B02；402 → B03；407 → C01；408/504 → C02；409 → A15；413 → A06；415 → A18；429 → B06；503 → C04；529 → B07 | 使用对应项的有效提示。明确的请求级政策或资源错误优先。 |
| 无资源或模型证据的 404/405/410、意外重定向、未知 4xx、其余 5xx 或非标准状态 | 502 / `server_error` | 服务处理请求时出错，请稍后再试。 |
| 已确定的协议失败，无可用状态、合法错误码或更具体原因，包括错误事件只有文字 | 502 / `service_error` | 本次请求未能完成，请稍后再试。 |
| 未知失败发生在已交付内容之后，或操作可能已生效 | C03 或 C05 | 使用对应阶段提示。 |

已确认的错误事件即使体积超限、缺失换行、错误 JSON 或转换失败，也只能产生本地错误或按协议关闭。普通消息、工具数据、未知但合法的非错误事件及正常下载继续依据各端点契约处理；不要递归搜索任意对象的 `error` 键。连接前失败、仅发送 keepalive、交付可见内容、发出可能有副作用的调用分别记录。已知业务拒绝优先于笼统的“流中断”。[流读取分类](../../backend/internal/service/openai_stream_read_error.go#L12) · [传输分类](../../backend/internal/service/openai_upstream_transport_error.go#L69)

`upstream_ws_failover_exhausted`、“重试耗尽”和“桥接失败”本身只描述经过。能取得最后一次失败原因时按其对应行返回；没有原因时走 C09。若操作可能已生效，以 C05 取代任何可能诱导重复提交的通用重试提示。本站自己的 `session_route_unavailable` 是路由暂不可用，不能借此断言会话已丢失。

### 图像、音频及正常协议终态

| 编号 | 稳定配置键 | 进入条件或上游代表码 | HTTP | 对外 code | 内置默认文案 |
|---|---|---|---:|---|---|
| D01 | `invalid_image`、`invalid_base64_image`、`image_parse_error` | `invalid_image`、`invalid_base64_image`、`image_parse_error` | 400 | 同码 | 图片数据无法读取，请重新导出或上传图片。 |
| D02 | `invalid_image_format`、`invalid_image_mode`、`unsupported_image_media_type` | `invalid_image_format`、`invalid_image_mode`、`unsupported_image_media_type` | 400 或 415 | 同码 | 图片格式或颜色模式不受支持，请转换后重新上传。 |
| D03 | `image_too_large`、`image_file_too_large` | `image_too_large`、`image_file_too_large` | 400 或 413 | 同码 | 图片过大，请降低分辨率或压缩文件后再提交。 |
| D04 | `image_too_small` | `image_too_small` | 400 | 同码 | 图片尺寸过小，请上传更高分辨率的图片。 |
| D05 | `empty_image_file` | `empty_image_file` | 400 | 同码 | 图片文件为空，请重新上传。 |
| D06 | `invalid_image_url`、`failed_to_download_image` | `invalid_image_url`、`failed_to_download_image` | 400 | 同码 | 无法读取图片链接，请检查链接是否可访问，或直接上传图片。 |
| D07 | `image_file_not_found` | `image_file_not_found` | 400 或 404 | 同码 | 找不到引用的图片，请重新上传。 |
| D08 | `audio_too_large` | `audio_too_large` | 400 | 同码 | 音频过大，请缩短时长或分段提交。 |
| D09 | `audio_too_short` | `audio_too_short` | 400 | 同码 | 音频过短，请提交较长的有效录音。 |
| D10 | `invalid_audio`、`invalid_audio_data`、`invalid_audio_format` | `invalid_audio`、`invalid_audio_data`、`invalid_audio_format` | 400 | 同码 | 音频数据或格式无效，请重新录制或转换格式。 |
| D11 | `missing_audio_sample_rate` | `missing_audio_sample_rate` | 400 | 同码 | 音频缺少采样率，请补全采样率后重新提交。 |
| D12 | `image_generation_unavailable` | `image_generation_unavailable`、已确认未执行图像生成或没有图像结果且无更具体原因 | 502 | `image_generation_unavailable` | 本次未生成图片，请稍后再试。 |
| D13 | `output_length_limit` | `response.incomplete` 或 Chat 正常停止原因 `length` 对应输出上限；图像分支可能包装成 `response_incomplete` | 正常终态沿用协议；专用任务按其失败契约 | 已有 `max_output_tokens` / `length` 原因；无需追加 error | 需要展示原因时用“输出已达到长度上限，可缩小请求范围或调整输出上限。” |
| D14 | `output_content_filtered` | `incomplete_details.reason=content_filter` 或 Chat 的 `finish_reason=content_filter`；图片专用端点可将其判为生成失败 | 正常终态沿用协议；已判错误的按 B10 | 保留终止原因；错误时用 B10 | 需要展示原因时用“部分内容因安全策略未能返回。” |
| D15 | `response_cancelled` | 协议确认取消，包括 `response.cancelled`、`response.canceled`；若上游主动异常终止则单独归因 | 保留取消终态 | 无需追加 error | 需要展示时用“本次请求已取消。”；客户端已断开时不再写出。 |
| D16 | `response_incomplete` | 其他 `response.incomplete`、取消或任务失败附带陌生自由文本；没有明确原因 | 沿用该协议终态；已判失败则走 C09 | 平台固定原因或省略可选详情 | 需要展示原因时用“本次响应未完成。” |

图片政策错误走 B10。固定版本 Codex 对六个音频码和 `budget_reached` 的终止处理有状态条件，真实 401/403/429/5xx 不可被同名的音频字段改判成 400。对未完成和取消，表中提示只供已有原因展示位置使用；正常 assistant refusal、工具业务返回和已生成的正常内容保留各自含义。[仓库输入终止契约](../../backend/internal/service/openai_input_terminal_error.go#L13) · [官方 Responses 错误类型](https://github.com/openai/openai-python/blob/main/src/openai/types/responses/response_error.py) · [Images 失败转换](../../backend/internal/service/openai_images_responses.go#L803)

## 写出和验收

1. **最终来源。** 在凭据、HTTP、SSE、WS 和桥接的失败判定处分配内部分类，再交给各协议写出。上游 401/403/额度耗尽与本站用户的 401/403/余额不足分别测试；区分 403 模型不可用、内容政策和账号禁用。只在错误归属明确后选择公开文案，包括 Composite 最终选中的 OpenAI 账号。A 账号失败后由 B 账号成功则用户只看见成功，失败则只取 B 的最终分类。
2. **输出白名单。** 用平台生成的 type、code、message 建立新的错误对象。仅在协议需要时保留经过校验的顺序号、响应/流关联、时间、终态、本站请求 ID 和有界的重试提示。可选 param 只允许平台常量，如 `model`、`input`、`tools`；其余省略。在错误响应、SSE framing、内嵌 headers、WS 关闭原因中移除供应商诊断、URL、账号/组织、套餐、未经核准的扩展、`detail`、`innererror` 和 `misalignment` 的自由文本。上游返回的 `WWW-Authenticate`、`Location`、代理/调试/追踪头不得成为错误侧信道。正常业务响应头、既有本地配额投影与成功响应另外按其协议处理。
3. **分类优先级。** 先判归属与真实状态，再按端点允许的具体码、受信任的内部原因、状态与输出阶段兜底。仅在现有分类器已定义边界时使用诊断文本辅助识别，绝不将其拼入返回。状态冲突按已有协议契约处理，如 401 附带 `policy_capacity_unavailable` 仍按凭据处理。透传规则可以内部匹配和记录；面向用户按审定的错误分类取当前有效文案，禁止规则把任意正文或成功状态写回。规则若已有自定义文字或状态，这属于需要公告的兼容性变化。
4. **出口。** 覆盖 Responses（含 compact、SSE 转 JSON、非流 HTTP 200 失败对象）、Chat、Messages 兼容层、原生 WS、直通、WS↔HTTP、握手和 close reason、官方 CLI 出站桥及传输插件；覆盖实际由 OpenAI 账号服务的 Live/sideband、Images、异步图像提交/轮询/用户历史、Embeddings、模型发现、搜索与 token 计数。接口别名随主路由验收。异步记录要在用户读出时再次使用安全表示，历史行即使存过原文也不能重新露出。只有 OpenAI 负责的调用或任务适用；后台账号授权管理、运维诊断和其他提供商独立处理。[网关路由](../../backend/internal/server/routes/gateway.go#L205) · [异步任务公开结构](../../backend/internal/service/image_task.go#L70)
5. **协议。** 发送心跳后仍可能尚无有效输出。按端点合同区分 JSON、SSE 失败终态、保持连接的 WS 请求级错误、必须断开的 WS 连接级错误及 1009 回退。保证每轮只结束一次、已有 usage 进入既有计费流程；以类型区分失败、未完成、取消和合法业务数据。有副作用或执行结果不明时不因文案增加重放。保留限流/容量协议允许的 Retry-After；若固定客户端依赖原文提取重试时间，先提取并验证有界时长，再通过该协议支持的结构化提示传递。涉及 `model_not_found`、`budget_reached`、六个音频码、`rate_limit_exceeded`、`policy_capacity_unavailable`、容量改写等必须在固定版本真实客户端上验证分类、重试和展示；第三方 UI 有自己的文案。
6. **审计用例。** 每行至少一例。OAuth 和 API Key 都要验证默认模式、已启用自定义且有覆盖、已启用自定义但未覆盖三种情况。对 HTTP/SSE/WS/CLI/异步出口分别用唯一标记污染 message、错误 code/type/param、扩展、header、嵌套对象、WS close、自定义透传及历史任务。断言客户端可见字节无标记，正常消息或工具结果中的同一标记能正常返回。补上 HTML/空体/未知码/错误 MIME、分片与 EOF、超大错误、只发心跳、已输出、重试成功、最终失败、热切换和原诊断仅限管理员的用例。在项目规定的容器内验证。发布时更新中英文设置说明、Responses/CLI 协议和依赖旧三项行为的测试。

### 按出口提交验收证据

| 范围 | 接手位置 | 通过条件 |
|---|---|---|
| 目录与设置 | [旧文案实现](../../backend/internal/service/openai_error_messages.go)、[设置更新](../../backend/internal/handler/admin/setting_handler_update.go)、[管理界面](../../frontend/src/views/admin/SettingsView.vue) | 逐条对照默认目录。覆盖旧开关开/关、编辑与恢复、页面筛选后恢复全部、取消、过期写入、保存失败、进程重启、跨实例同步、未知错误以及两项复用同一公开码。GET 返回的每个配置键都可在界面找到并独立保存。 |
| HTTP/SSE、WS 和 Live | [HTTP 返回](../../backend/internal/service/openai_gateway_upstream_errors.go)、[流式返回](../../backend/internal/service/openai_gateway_response_handling.go)、[WS/HTTP 桥](../../backend/internal/service/openai_ws_http_bridge.go)、[WS handler](../../backend/internal/handler/openai_gateway_handler.go)、[Live](../../backend/internal/service/openai_live.go) | 分别断言 OAuth/API Key、自定义地址、直通及协议转换。HTTP 400/429/5xx、HTTP 200 内失败、只发过心跳、已产生输出、未知/畸形错误、WS 关闭原因都无法释放诊断标记。关闭原因按 UTF-8 字节边界截断，覆盖长中文和 emoji。健康内容、用量、重试次数和失败终态另有正向断言。 |
| 用户历史和运维 | [用户错误 DTO](../../backend/internal/service/ops_user_error.go)、[用户详情](../../backend/internal/handler/usage_handler.go)、[异步图片失败写入和读出](../../backend/internal/handler/image_task_handler.go) | 错误列表、详情、管理员协助用户视图和图片任务用当前有效文案。即使旧行只有诊断正文，也须从安全分类或兜底重建。保留用户可见性设置和归属校验。新记录保存足以重建的稳定分类键；历史记录无可靠细分证据时只使用可证实的状态级兜底。管理员自己的 Ops 界面继续依现有权限及脱敏策略查看原始诊断。 |
| 官方客户端 | [真实消费者测试](../../backend/internal/service/openai_error_messages_official_cli_test.go)、[仓库官方 CLI 验证入口](../../tools/test_codex_cli_wire.py) | 断言实际下游 wire 的默认/覆盖文本、码、终态和调用次数；另外观察固定 CLI 对终止、限流和容量码的行为。CLI 自行生成提示的场景分别记录 wire 结果和 CLI 展示，普通单测中的跳过不能代替专用验证。 |

实现提交同步更新[现行 Responses 契约](OPENAI_RESPONSES.md#downstream-session-error-wording)、[CLI 三码验证说明](../CODEX_CLI.md#downstream-session-error-wording)、[安全审计的出站投影说明](../SECURITY_AUDIT_CONTENT_COVERAGE.md#downstream-session-error-wording-projection)、[测试矩阵](../SEVEN_PROVIDER_TEST_MATRIX.md)及前端中英文设置文案。老测试里“未知错误按原字节透传”和“关闭就显示原诊断”等期望只适用于旧功能；正常内容、其他提供商、真实 CLI、内部计量和审计先后顺序的断言继续覆盖。对供应商 `slow_down` 的旧注释统一对齐上文 B07。交付时报告场景与测试的对应关系、实际运行的容器检查及任何未验证出口。[仓库测试与容器规则](../../CONTRIBUTING.md#requirement-based-test-design)
