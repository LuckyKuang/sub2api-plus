# 七平台已实现网关业务验收范围

本轮从 `fix/seven-platforms-main-sync@27c5e62f4` 完成 B0–B5 七平台能力补齐：保留合并
`85fb3c82c` 中目标 `2dab72aac` 与 surgeonfish 检查点 `e2bbc80dc` 的实现，新增首批 Files、
Start 权益和可选 off-peak 持久恢复。完整后端 unit/integration/lint、前端检查和构建、
扩展关键门禁、14 条 Chromium 管理链及定向 race 已在 Apple Containers 通过。
各项适用范围、断言与实际结果见[七平台测试矩阵](SEVEN_PROVIDER_TEST_MATRIX.md#capability-completion-verification)。
旧提交的通过记录只对各自源码树有效，不代替新增能力的验证。
本清单描述网关验收行为；具体使用哪一层测试证明，以矩阵和实际运行记录为准。
不能仅凭测试文件存在、父用例通过或覆盖率数字认定一项业务完成。

验收按适用的账号类型、协议、地区、套餐和独立发送入口展开。某平台提供的能力
不会自动扩展到其他平台；协议转换入口也不能证明上游原生支持同名协议。
本清单不承诺所有维度的任意排列组合均已串成端到端自动化用例。

## 平台与原生业务

| 平台 | 已实现的账号与授权 | 已实现的推理和控制业务 | 必须保留的边界 |
| --- | --- | --- | --- |
| OpenAI / Codex | API Key、既有 OAuth 账号；授权码、设备授权、刷新与撤销等独立操作 | Responses HTTP/WS、Chat/Messages 适配、图像兼容与异步任务/S3 结果存储、模型目录、用量与配额重置、已有 Alpha Search 和 Live 创建/sideband 路径；可选 OAuth 传输插件宿主 | HTTP、WS、图像任务/存储、插件进程、模型发现、预检、登录和控制请求分别验收；各账号类型不自动共享授权能力。Codex 身份优先级及字段省略规则保持不变。 |
| xAI / Grok | API Key、OAuth 浏览器授权；可选 SSO/device 转换 | Responses、Chat、Messages、搜索、语音/Realtime、图像、视频及状态查询；模型、计费/配额与压缩能力探测 | 媒体、Realtime、控制和辅助 HTTP 发送是独立入口；SSO 原文不落库。实际模型/套餐权限由上游决定。 |
| DeepSeek | API Key；Harness 授权码与 PKCE 登录、重授权 | 原生 Messages；Chat/Responses 适配；模型能力过滤、用户与钱包观测；API Key 余额入口；首批 Anthropic 图片 Files 上传、本地列表/元数据、到期、删除与推理引用 | 原生 OAuth 使用 `x-dsh-auth-token`；官方协议未公布刷新操作，不能虚构刷新成功。当前只有一个授权地区。Files 仅支持已冻结的 Anthropic 家族与媒体类型，不泛称任意文件或 OpenAI Files 支持。 |
| Kimi | API Key 的 payg/coding 配置；中国/国际设备授权、刷新和重授权 | 原生 Messages 及已有适配；模型发现、coding 用量和 API Key 余额/配额控制 | 登录、轮询、刷新、推理和控制保留同一地区、设备身份与凭据 owner；不能以通用 base URL 改写原生 issuer。 |
| MiniMax | API Key/BYOK；中国/国际设备授权、PKCE、刷新和重授权 | 原生 Messages、BYOK Anthropic 独立入口及已有适配；既有套餐/用量控制链；global API Key Chat 的 `video_understanding` 文件上传和视频引用 | OAuth 与 BYOK 有各自身份声明；不能混用。首批 Files 仅允许已核实 profile；删除撤销本地引用并明确报告不支持远程删除，不虚构上游 list/metadata/delete。 |
| StepFun / Step-Code | API Key；中国/国际 Step Plan 云端浏览器登录与重授权；独立的可选 Passport 账号会话 | 官方执行主协议 Chat Completions；Responses/Messages HTTP 转换；模型/能力与白名单、Step Plan 用量、现有 token 计费 | OAuth Plan、API Key PayG、API Key Plan × CN/global 共六 profiles 分别验收。原生 OAuth 固定地区和 Plan；静态模型 key 与可选账号会话分离。两桥不能证明上游原生 Responses、Messages 或 WS。 |
| Zhipu / ZCode | 手工 API Key；BigModel/Z.ai 账号关联；个人/团队 coding、Start Plan 与 off-peak | coding 项目解析、短期 PAT、模型/配额；Start Bearer 与独立 balance 权益/模型观测；off-peak Take/Status/Settle、可选持久恢复 | Start/off-peak 保留 Anthropic 通道，权限观测分别处理；团队需明确组织/项目。持久恢复默认关闭，启用需要专用外部密钥环；关闭时仍为进程内票据。PAT 轮换不等于业务登录续期。 |

平台契约来源：
[OpenAI Responses](protocols/OPENAI_RESPONSES.md)、
[Codex client profiles](protocols/CODEX_CLIENT_PROFILES.md)、
[Grok](providers/GROK.md)、
[DeepSeek/Kimi/MiniMax 原生授权](providers/DOMESTIC_OAUTH.md)、
[StepFun](providers/STEPFUN.md)、[Zhipu](providers/ZHIPU.md)、[Files](providers/FILES.md)、
[共享模型目录](CN_PROVIDER_MODELS.md)、[异步图片任务](ASYNC_IMAGE_TASKS.md)、
[插件宿主协议](PLUGIN_DEVELOPMENT.md)。
公共入口以[网关路由](../backend/internal/server/routes/gateway.go)为准；服务方法存在
不代表新增公共路由，也不代表任意平台均允许访问。

## 每个平台适用的共同验收项

以下是业务不变量，覆盖状态仍需逐项关联矩阵和可执行断言。

| 编号 | 业务维度 | 必须观察的结果与禁止的副作用 |
| --- | --- | --- |
| G01 | 账号管理 | 创建/编辑、暂停、凭据替换、删除和重建遵循现有认证与管理边界；修改代理不丢凭据，不复用已删除账号的 owner/cache。API 创建与 UI 创建分别记录。 |
| G02 | 授权会话 | 对实际实现的 Start、URL、Poll、Exchange、Complete、Cancel 分别覆盖成功、拒绝、过期、取消和存储故障。会话绑定管理员、平台、地区、身份与代理；迟到结果不能完成替代流程。 |
| G03 | 凭据刷新/重授权 | 只执行平台提供的操作；保留完整 owner。轮换、重关联、并发写和保存失败不能发布旧 token、误清错误/冷却或重放创建。Step 模型 key 与可选会话、ZCode PAT 与登录各自验收。 |
| G04 | 身份与严格出站 | 每个独立发送入口保留可信身份和捕获的代理快照；绑定丢失/错误及超时、拒绝、407 不允许直连重试。入站声明和通用覆写不能替换身份。正向成功和明确解绑的直连控制也必须存在。 |
| G05 | 模型与准入 | 模型目录、能力、地区/套餐、客户端最低版本、手工映射和分组白名单按各平台契约生效；未知能力不能伪造，迟到发现不能污染替换后的 owner。 |
| G06 | 推理与协议 | 对实现的原生协议和适配路径分别验收普通/流式响应、工具参数片段与结果回传、媒体字段和错误语义。只断言最终文本或所选账号 ID 不足以证明实际出站正确。 |
| G07 | 调度、换号与会话 | 实际 A 失败后 B 接管；后续会话在相同及其他实例仍使用 B。断言发送凭据、代理、重试边界、槽位释放及旧结果隔离；中断、取消和失败保存不能遗留容量或会话映射。 |
| G08 | 用量与计费 | 以独立价格/用量预期检查成功消费、失败不误计、流式/WS 去重、账户/用户/key/订阅约束及幂等。需要证明持久化的行为使用真实 PostgreSQL/Redis。 |
| G09 | 配额、余额与恢复 | 仅识别完整合法观测；未知、部分损坏、无限或币种不一致不能制造零用量/余额。旧凭据或代理的响应不能推进新 owner freshness 或改变其调度。重置/恢复与 outbox 原子提交。 |
| G10 | 代理管理与跨实例 | 新增、绑定、空闲/途中修改、故障、修复、解绑、删除、到期 fallback 和恢复原路按保存状态运行。事务、并发绑定/删除、renewal 及 outbox 故障不能留下部分状态；旧探测不能证明新端点可用。 |
| G11 | 安全审计 | 每个接受的 HTTP/WS 请求或 turn 在账号选择、计费、槽位和上游写之前进入审计；两引擎使用同一内容提取契约。未识别/不可提取内容按规范透传，已识别的同级内容仍可审计。 |
| G12 | 专属生命周期 | 对确实实现的长生命周期逐项验收，如 Live 创建/租约/sideband、ZCode 票据结算与重试、Grok 媒体 start/poll/status；不能由普通 Chat 请求推断这些入口已覆盖。 |

共同契约来源：[出站身份](OUTBOUND_IDENTITY.md)、
[代理生命周期](PROXY_LIFECYCLE.md)、
[安全审计提取边界](SECURITY_AUDIT_CONTENT_COVERAGE.md)、
[需求驱动测试设计](../CONTRIBUTING.md#requirement-based-test-design)。

Grok 的 issuer 失败隔离同样绑定实际发送时捕获的凭据及完整代理行，不能只比较
代理 ID。端点、代理认证或生命周期变化不得把旧失败写到替代路由；单纯显示名称
变化保留正常隔离。隔离写入失败的 owner CAS 只能恢复相同 refresh grant、身份
和路由的 access-token 修复，新 grant、subject/team/client、base URL 或身份必须
重新选择账号。持久化隔离和 scheduler outbox 共享事务，cache 仅在提交后发布；
仍丢失的绑定和已经恢复、正在并发恢复的同 ID 代理需分别验收。

Files 列表的游标同样属于调用方权限边界：其他用户、key、group 或平台的文件 ID
不得改变分页结果，未知和无权访问的游标保持相同结果。Start 的 composite route
和分组白名单也不构成平台授权，公开别名必须有其实际目标模型的有效权益证据；
这项过滤不应移除其他平台的正常模型和路由。

## 自动化证据的界限

[测试矩阵](SEVEN_PROVIDER_TEST_MATRIX.md)维护每个入口的实际测试和验证限制。
使用下列证据时应明确观察层：

- 单元/组件测试可以证明参数、状态机和调用前后副作用；替身返回值不能证明生产网络
  发出了正确请求，也不能证明数据库事务提交。
- 生产客户端配合本地 HTTP/TLS/WS 目标和真实代理，可以检查线上的协议、凭据、
  身份、重试及直连 TCP 计数；它不证明真实订阅授权或公网服务可用。
- 真实 PostgreSQL/Redis 集成测试检查持久化、owner CAS、事务/outbox、跨实例缓存、
  幂等和账务；不能据此声称浏览器按钮或操作系统回调已跑过。
- [浏览器管理链](../frontend/e2e/README.md)定义七平台 × API Key/OAuth 共 14 例，
  驱动真实 Vue、后端和数据库。账号记录由真实管理 API 准备；这不是每个平台的
  UI 创建账号或真实 OAuth 授权，也不执行推理、刷新或计费。授权面板组件与登录
  协议测试分别使用后端替身和本地 issuer 响应器，不能借此认定真实外部授权、
  订阅或套餐权限已验收。未完成实际运行的用例保持待验证，不能标为覆盖。
- MinIO 存储链通过独立 `storage` 门禁验收，要求显式临时 PostgreSQL/Redis/MinIO；
  普通 unit/integration 环境和当前 CI 不自动配置该环境，跳过不能计为通过。
- 本地可信签名插件包需经过生产 Installer、真实独立进程与 RPC 转发；同进程
  broker 测试不能替代。该本地包不证明正式外部插件兼容，缺少外部签名包时仍保留
  那一项未验证边界。

新增独立发送入口、账号类型或受支持能力时，应同时更新本清单、平台契约及测试
矩阵，并为适用的成功、失败、边界和禁止副作用补充测试。关键用例缺失、跳过或
未结束须阻止门禁通过；门禁通过仍需复核断言是否证明了业务要求。

## 前端授权与探测的并发边界

返回基本设置、修改代理、关闭或替换授权流程后，旧授权结果不能发布 URL、创建
后续账号、修改替代流程的错误或结束其加载状态。Grok 的授权码、RT 批次和 SSO
导入是独立入口，分别验收。SSO 导入或账号创建的 POST 已经发出时，服务端仍可能
完成原写入；旧响应不能据此关闭新流程、显示旧通知或继续同步新流程的模型。
账号创建完成与模型同步完成也各有一次异步边界。

已有账号的重授权是另一条管理入口。OpenAI/Grok 的授权码、RT、SSO（仅 Grok）与
OpenAI 设备流程按账号、凭据、保存的代理和弹窗代次保留 owner；切换账号、同 ID
换代理、关闭重开或卸载后，旧 grant/apply 的成功或失败不能污染新流程。
设备 start、poll、apply 和轮询间隔分别验证；创建弹窗的设备回归不能替代它们。
管理 DTO 隐去真实凭据，因此仅比较公开凭据字段不足以识别同 ID 换号；前端使用
服务端的 `oauth_credential_owner` 不透明摘要，并在列表自动刷新时传播该摘要。
只有用量、账号观测时间或代理名称/更新时间变化、owner 未变化时，应保留进行中的
授权和已获得的 grant；不能再用公开代理的 `updated_at` 覆盖权威 owner 判断。
`POST /api/v1/admin/accounts/:id/apply-oauth-credentials` 提交携带流程开始时的
`expected_owner`，值来自该账号 DTO 的 `oauth_credential_owner`，是由 64 个小写
十六进制字符组成的摘要。缺失或格式错误时，前端不发送新凭据；owner 冲突后必须重新开始
授权，不得读回当前 owner 后自动重放旧 grant。已发出的 apply POST 不能靠前端
关闭弹窗撤回：服务端以原 `expected_owner` 和实际持久化 owner 的条件提交裁决。
owner 已改变的提交不能替换凭据、清除新错误或恢复新 owner 的调度状态；owner
仍匹配时，原提交可能完成，但旧响应仍不能结束替代流程。原子性需由后端事务
用例证明，前端迟到响应隔离不能替代服务端条件提交。
协议细节见[认证文档](AUTHENTICATION.md)。

DeepSeek/Kimi/MiniMax/StepFun 的菜单重授权和编辑页重授权都复用原生授权面板，
将已有 `account_id` 与保存地区传给原生服务，由服务端捕获该账号的保存代理。
其完成后的账号重读也是独立异步边界：旧 complete 不继续重读，旧重读不关闭新框。
不能把这四个平台送入 Claude 授权器。Zhipu 目前只有新建链接账号的 UI，没有
已有账号原位重关联 UI；菜单不提供该动作，也不调用其他平台 issuer。
DeepSeek 与 Zhipu 没有公布业务登录刷新操作，菜单不提供 **Refresh token**，
也不能回退到其他平台刷新器。Kimi、MiniMax、StepFun 保留各自支持的刷新入口；
StepFun 的可选账号会话刷新和 Zhipu 的短期 PAT 轮换不等于业务登录续期。

Grok 创建弹窗目前没有显示密码登录选项。保留的密码事件处理器有批次取消回归，
该测试不能作为可见密码登录 UI 或浏览器授权的证据。SSO 与授权码回归则通过现有
可见表单动作进入。需要区分可见交互与内部处理器，避免用隐藏功能扩大验收范围。

代理管理页面的单项按钮和批量按钮都可以命中同一代理。同一保存版本的在途探测
共享请求，批量操作等待该请求完成；编辑路由后，新探测拥有自己的结果和加载状态。
无论旧探测成功还是失败，都不能证明替代端点可用或提前释放新探测的状态。
离开代理管理页面也撤销该页面发布结果的资格：成功、失败均不能再弹全局通知、
刷新列表或继续发送排队探测。连接测试和质量检查的单项、批量入口分别验收。
对应组件回归及其实际执行状态见测试矩阵，不能由
14 例浏览器管理链的单项连接测试推断批量并发与质量检查已经通过。

## 明确排除或仍有限制的能力

- 七个官方 coding agent 的本地工作区、编辑器、终端/TUI、本地 MCP、工具执行循环、
  操作系统集成和所有 agent 内部恢复策略不属于本次网关等价实现。
- Files 以[分平台契约](providers/FILES.md)为准。DeepSeek 首批仅 Anthropic 图片家族；
  MiniMax 首批仅 global API Key Chat 视频上传/引用及本地访问撤销。DeepSeek 其他文件
  家族及 MiniMax 的其他地区、managed OAuth、purpose，另有未列入首批的远程管理
  操作和跨账号重传保持明确不支持。已有原生引用
  和内联媒体继续独立验收，不因此获得网关文件 ACL 或生命周期保证。
  DeepSeek 网关本地文件仅支持原生 Messages 推理，MiniMax 仅支持原生 Chat；其他
  转换入口明确拒绝本地引用，不能静默丢弃媒体后继续推理、计费。合法 JSON 转义
  与字面 ID 遵循相同入口和权限规则，普通文本提及文件 ID 不请求 Files 能力。
- StepFun 不宣称上游原生 Responses、Messages、Responses WS；现有转换入口仍需
  独立验收。远程搜索 MCP 本批完成契约审计，尚未开发搜索网关；专项语音/图像接口、
  WS 桥和额外计费不由 Chat 的默认 token 定价推断。
- ZCode 不恢复官方 agent 已有 `task_id` 的历史队列位置，也不重放模型请求。可选持久
  模式只恢复本网关已保存票据及原 owner 结算意图；未知 Take 结果不能自动再取号，
  确认失效后的新票接受新排位。Start 使用独立 balance 观测，不借用 coding 项目模型；
  off-peak 不借用 Start 权限。业务登录失效需重授权，Settle 不返还滚动窗口取号额度。
  持久模式提供管理员 GET 当前段与 POST 精确 `record_id/generation` 终止；响应仅确认
  退休意图已落库。旧控制流程不得重取号或加入新段，已获准物理请求按共享租约结束，
  原快照后台结算；旧终止重复到达不得影响后来新段。内存模式明确返回 501；账号停用/
  删除与单请求取消不等同于该管理终止，也不承诺强杀已获准的物理请求。
- Grok enterprise deployment 按用户要求不进入本轮对齐范围。官方 turn/resubmit/
  tracing lineage、doom-loop 恢复和与 Rustls 完全一致的 TLS 指纹也未作为已实现能力。
- 真实平台账号的地区/套餐权限、官方服务可用性及完整浏览器/操作系统授权流程，
  需要使用实际账号和对应环境单独验收；隔离响应器的成功不构成这些结论。
- 全量 integration、全量 race 或所有协议 × 类型 × 套餐 × 生命周期的组合，
  只有在对应实际运行和断言存在时才可报告通过；局部测试不能代替。
