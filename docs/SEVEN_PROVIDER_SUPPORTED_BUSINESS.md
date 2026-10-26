# 七平台已实现网关业务验收范围

本清单以 `fix/seven-platforms-main-sync` 上的语义合并为审计对象：保留目标
`2dab72aac` 与基于 `1818c2993` 的 surgeonfish 检查点 `e2bbc80dc` 两边实现，
限定本轮“七平台业务覆盖”的功能边界。合并前的验证只对各自源码树有效；
当前合并树须重新执行相应容器门禁，不能沿用旧结果宣称通过。它描述需要验收的既有网关行为；各项是否已经通过、
使用哪一层测试证明，以[七平台测试矩阵](SEVEN_PROVIDER_TEST_MATRIX.md)及实际运行记录为准。
不能仅凭测试文件存在、父用例通过或覆盖率数字认定一项业务完成。

验收按适用的账号类型、协议、地区、套餐和独立发送入口展开。某平台提供的能力
不会自动扩展到其他平台；协议转换入口也不能证明上游原生支持同名协议。
本清单不承诺所有维度的任意排列组合均已串成端到端自动化用例。

## 平台与原生业务

| 平台 | 已实现的账号与授权 | 已实现的推理和控制业务 | 必须保留的边界 |
| --- | --- | --- | --- |
| OpenAI / Codex | API Key、既有 OAuth 账号；授权码、设备授权、刷新与撤销等独立操作 | Responses HTTP/WS、Chat/Messages 适配、图像兼容与异步任务/S3 结果存储、模型目录、用量与配额重置、已有 Alpha Search 和 Live 创建/sideband 路径；可选 OAuth 传输插件宿主 | HTTP、WS、图像任务/存储、插件进程、模型发现、预检、登录和控制请求分别验收；各账号类型不自动共享授权能力。Codex 身份优先级及字段省略规则保持不变。 |
| xAI / Grok | API Key、OAuth 浏览器授权；可选 SSO/device 转换 | Responses、Chat、Messages、搜索、语音/Realtime、图像、视频及状态查询；模型、计费/配额与压缩能力探测 | 媒体、Realtime、控制和辅助 HTTP 发送是独立入口；SSO 原文不落库。实际模型/套餐权限由上游决定。 |
| DeepSeek | API Key；Harness 授权码与 PKCE 登录、重授权 | 原生 Messages；Chat/Responses 适配；模型能力过滤、用户与钱包观测；API Key 余额入口 | 原生 OAuth 使用 `x-dsh-auth-token`；官方协议未公布刷新操作，不能虚构刷新成功。当前只有一个授权地区。 |
| Kimi | API Key 的 payg/coding 配置；中国/国际设备授权、刷新和重授权 | 原生 Messages 及已有适配；模型发现、coding 用量和 API Key 余额/配额控制 | 登录、轮询、刷新、推理和控制保留同一地区、设备身份与凭据 owner；不能以通用 base URL 改写原生 issuer。 |
| MiniMax | API Key/BYOK；中国/国际设备授权、PKCE、刷新和重授权 | 原生 Messages、BYOK Anthropic 独立入口及已有适配；既有套餐/用量控制链 | OAuth 与 BYOK 有各自身份声明；不能混用。模型目录候选不等于账号权限，原生文件引用不等于文件上传生命周期。 |
| StepFun / Step-Code | API Key；中国/国际 Step Plan 云端浏览器登录与重授权；独立的可选 Passport 账号会话 | 原生 Chat Completions；Responses/Messages 转换；模型/能力与白名单、Step Plan 用量、现有 token 计费 | 原生 OAuth 固定到所选地区和 Plan；静态模型 key 与可选账号会话分离。不能据 SDK 适配器推断原生 Responses、Messages 或 WS。 |
| Zhipu / ZCode | 手工 API Key；BigModel/Z.ai 账号关联；个人/团队 coding、Start Plan 与 off-peak | coding 项目解析、短期 PAT、模型/配额；Start Plan Bearer；off-peak 票据 Take/Status/Settle 与恢复 | Start/off-peak 只支持其 Anthropic 通道；团队需明确组织/项目。PAT 轮换不等于业务登录续期，不能回退复制出的长效 key。 |

平台契约来源：
[OpenAI Responses](protocols/OPENAI_RESPONSES.md)、
[Codex client profiles](protocols/CODEX_CLIENT_PROFILES.md)、
[Grok](providers/GROK.md)、
[DeepSeek/Kimi/MiniMax 原生授权](providers/DOMESTIC_OAUTH.md)、
[StepFun](providers/STEPFUN.md)、[Zhipu](providers/ZHIPU.md)、
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

- 七个官方 coding agent 的本地工作区、编辑器、终端/TUI、MCP、工具执行循环、
  操作系统集成和所有 agent 内部恢复策略不属于本次网关等价实现。
- DeepSeek/MiniMax 通用 Files 上传、元数据、所有权映射、到期和删除未由当前
  网关路由提供；保留原生引用和内联媒体不能替代这些能力。
- StepFun 不宣称上游原生 Responses、Messages、Responses WS；现有转换入口仍需
  独立验收。专项语音/图像接口和额外计费也不能由 Chat 的默认 token 定价推断。
- ZCode 不恢复官方 off-peak `task_id` 的既有队列位置；网关有自己的票据生命周期
  和有界等待。Start/off-peak 无个人/团队 coding 的模型发现路径，业务登录失效需
  重授权。
- Grok 不伪造 enterprise deployment 授权、turn/resubmit/tracing lineage，
  不宣称实现官方 doom-loop 恢复或与 Rustls 完全一致的 TLS 指纹。
- 真实平台账号的地区/套餐权限、官方服务可用性及完整浏览器/操作系统授权流程，
  需要使用实际账号和对应环境单独验收；隔离响应器的成功不构成这些结论。
- 全量 integration、全量 race 或所有协议 × 类型 × 套餐 × 生命周期的组合，
  只有在对应实际运行和断言存在时才可报告通过；局部测试不能代替。
