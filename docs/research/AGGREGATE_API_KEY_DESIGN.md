# 聚合 API Key 设计调研与建议

调研日期：2026-11-10。范围已按用户后续意见收敛。状态：前期设计调研记录。当前实现和协议以 [聚合 API Key](../AGGREGATE_API_KEYS.md) 为准；本笔记保留当时的证据与建议，不替代现行协议、安全或计费规范。
外站证据来自公开一手文档及主代理在已登录 Orca 浏览器的现场观察；本笔记不保存凭据、Key 值或账户数据。
本次只阅读源码和文档、修改文档，未运行应用或验证套件；测试文件的存在不表示本次测试通过。

## 结论

用户最新范围：独立新增聚合 API Key，默认“全部分组”自动纳入当前用户有权使用的按量分组；“自定义分组”使用明确勾选的按量分组。
按实际选中组的现行按量价格计费，从用户余额扣减，不消耗组订阅额度。
原有单组 Key、Composite 分组及其“配置所有模型统一使用的端点”继续使用原创建、编辑和执行流程。
新聚合入口不提供统一转换选项，旧 Composite 仍保留该项及原配置；不批量关闭或迁移旧 Key 的转换状态。
费用/延迟/稳定性偏好、调度权重和独立界面改版不在本次范围。

“全部”是真正动态选择全部符合条件的按量分组，不绑定默认 Composite、不保存创建时的全量快照，也不复制账号成员。
建议首版候选为启用、当前用户有绑定权限、`subscription_type=standard` 的普通分组；Composite 和订阅分组仍通过原有入口使用。
这样既能保持按量语义，也能让新模式与原 Composite 产品完全隔离。若以后要将按量 Composite 纳入新候选，需要另行明确其原生能力与组级政策范围。
自定义集合不自动扩展；全部集合会随组启停及用户授权增减更新，始终受当前权限约束。
现有源码与明确未实现范围见 [Composite 路由](../../backend/internal/service/composite_route_resolver.go#L134)、
[Key 数据结构](../../backend/internal/service/api_key.go#L30)、[现行限制](../COMPOSITE_GROUPS.md#limits)。

原建议以新增 Key 绑定模式及分组关联表实现；实现采用同一 Key 行上的 JSONB 固定成员数组，以单次写入保证模式与成员原子更新。在进入原有组处理链路之前增加选组步骤；既有 Key 仍走原入口。

## WandAPI：可确认与不可确认

公开一手资料：[API Key 管理](https://wandapi.com/zh/docs/apikey)、
[模型列表](https://wandapi.com/zh/docs/api/models)、[API 概览](https://wandapi.com/zh/docs/api/index)。
主代理同时查看了 [API Keys 控制台](https://wandapi.com/zh/console/api-keys) 的未保存创建弹窗。

| 观察 | 证据与边界 |
| --- | --- |
| 调度分组跨厂商多选，带厂商筛选、搜索、全选、倍率、说明和已选标签 | 控制台现场观察；曾同时勾选 OpenAI 与 Anthropic，未保存 |
| 不选择时，所有可见分组参与 | 控制台说明；不代表已经证明其运行时授权判定 |
| 智能路由 | 界面描述为管理员货源优先级，同段对话固定货源 |
| 最省费用 | 界面描述为最低倍率，同段对话固定货源 |
| 最低延迟、最稳定 | 界面分别描述近期首字速度、近期成功率；公开文档对延迟采用“延时测试”口径，不能据此还原实际算法 |
| 高级设置折叠收纳 | 消费总额、过期时间、IP/CIDR 白名单和启用状态；现场未提交修改 |
| 模型发现与协议兼容 | 公开文档描述按 Key 聚合可调模型；API 概览同时说明协议支持不等于功能完全兼容 |

这证明了交互和公开产品语义，未验证实际选路、定价、故障切换、统计窗口或会话归属算法。

## 仓库现状核对

| 主题 | 已具备能力与真实边界 | 一手源码 |
| --- | --- | --- |
| Key 与组 | 一个可空 `group_id`，并非多组数组；创建、编辑 API 同样为单组 | [schema](../../backend/ent/schema/api_key.go#L47)、[请求 DTO](../../backend/internal/service/api_key_service.go#L218) |
| 组与平台 | 组可以是具体平台或 `composite`；账号通过多对多 `account_groups` 绑定，Composite 可从具体组复制账号。复制的是账号成员关系，不是继承源组政策 | [平台判定](../../backend/internal/domain/platforms.go#L78)、[成员表](../../backend/ent/schema/account_group.go#L23)、[复制流程](../../backend/internal/service/admin_group.go#L607) |
| 跨平台路由 | Composite 显式路由优先，其次账号模型归属，最后内置模型识别；路由输出具体平台与上游模型，不输出另一个组 | [解析器](../../backend/internal/service/composite_route_resolver.go#L134)、[路由存储](../../backend/ent/schema/composite_model_route.go#L36) |
| 路由优先级 | exact 优于 prefix，专用 endpoint 优于 any，长前缀优先，再比较 priority 和 ID；未解析或歧义请求在网关返回 400 | [排序](../../backend/internal/service/composite_route_resolver.go#L219)、[网关拒绝](../../backend/internal/server/routes/gateway.go#L628) |
| 组内账号路由 | `model_routing` 是模型到账号 ID 集合的组内功能；不同于 Composite 的模型到平台路由，也不同于 Key 跨组选择 | [组内路由](../../backend/internal/service/group.go#L267) |
| 用户授权 | 标准公开组默认可绑定；受限用户及专属组需 `AllowedGroups`；订阅组需要该用户对该组的有效订阅。目录可见不等于可绑定 | [用户规则](../../backend/internal/service/user.go#L82)、[绑定规则](../../backend/internal/service/api_key_service.go#L491)、[可见与可绑定区分](../../backend/internal/service/api_key_service.go#L1158) |
| 模型授权 | 模型 allowlist 属于 Group，检查客户端公开模型名，位于 Composite 改写之前；Key schema/DTO 没有独立模型或平台白名单 | [中间件](../../backend/internal/server/middleware/group_model_allowlist.go#L15)、[模型规则](../../backend/internal/service/group_model_allowlist.go#L84)、[Key 字段](../../backend/internal/service/api_key.go#L30) |
| 订阅与额度 | 订阅属于 `(user, group)`，额度窗口为组的 USD 用量；余额模式另查 `user × concrete platform` 配额，订阅模式豁免该平台配额 | [订阅对象](../../backend/internal/service/user_subscription.go#L14)、[准入](../../backend/internal/service/billing_cache_service.go#L836) |
| 客户价格 | 当前组基准/个人价、平台与公开模型系数、高峰及独立媒体规则共同决定价格；新请求捕获不可变定价证据，同组重试复用 | [快照捕获](../../backend/internal/service/billing_pricing_snapshot.go#L282)、[个人价实时读取](../../backend/internal/service/user_group_rate_resolver.go#L61)、[计费契约](../COMPOSITE_GROUPS.md#consumption-rate-policies-and-sale-catalogs) |
| Key 限制 | 启停、IP 白/黑名单、总 USD 额度、过期、5h/1d/7d USD 窗口已实现；窗口不是 RPM。并发为用户跨 Key/组共享，无独立 Key RPM | [Key 字段](../../backend/internal/service/api_key.go#L30)、[鉴权](../../backend/internal/server/middleware/api_key_auth.go#L129)、[RPM/并发契约](../GATEWAY_ADMISSION.md#user-and-user-group-rpm) |
| 消费记账 | 订阅额度或余额按 `ActualCost` 扣减；同一实际费用同时进入该 Key 总额度和金额窗口；供应商账号成本独立 | [统一结算命令](../../backend/internal/service/gateway_usage_billing.go#L317) |
| 模型列表 | Composite 会合并平台可用模型与精确公开路由，经过组 allowlist；通用 `/v1/models` 不是逐模型端点能力的完整证明 | [列表实现](../../backend/internal/handler/gateway_handler.go#L1185)、[合并源](../../backend/internal/handler/gateway_handler.go#L1324) |
| 原生与统一协议 | 默认原生；Key 可显式开启固定 Responses/Chat/Messages HTTP/SSE 转换；专用协议、WS 和 provider-owned continuation 有独立约束 | [访问政策](../../backend/internal/service/composite_access_policy.go#L39)、[续会话限制](../../backend/internal/service/composite_access_policy.go#L78)、[现行规范](../COMPOSITE_GROUPS.md#optional-unified-endpoint-and-native-access) |
| 安全能力发现 | 已有授权后的 `/groups/:id/native-access`，返回公开模型及原生端点/可转换端点；整体转换选择取各模型能力交集，不泄漏账号或凭据 | [授权与投影](../../backend/internal/handler/api_key_native_access.go#L25)、[交集](../../backend/internal/handler/api_key_native_access.go#L141) |
| 创建体验 | 已有分组搜索、分平台/计费单位报价、模型详情、原生/转换说明及配置示例；但 Composite 在厂商分类中归 `other`，初始分类为 Anthropic | [报价与能力组件](../../frontend/src/views/user/KeysView.vue#L555)、[原生/转换界面](../../frontend/src/components/keys/NativeAccessFields.vue#L1)、[分类](../../frontend/src/utils/keyGroupProviders.ts#L7)、[初始分类](../../frontend/src/views/user/KeysView.vue#L1510) |

现有 OpenAI advanced scheduler 已使用账号级 TTFT、错误率、优先级、负载、队列和可选上游成本因素，并支持会话账号偏好。
它接受单个 `GroupID` 与账号候选集合，开关及权重来自管理员设置；上游成本是供应商成本，不能等同客户应付价格。
因此可以复用统计或账号选择基础，但不能把现有调度器宣称为 WandAPI 式 Key 跨组偏好。
证据：[请求范围](../../backend/internal/service/openai_account_scheduler.go#L71)、[按账号采样](../../backend/internal/service/openai_account_scheduler.go#L956)、
[成本因子](../../backend/internal/service/openai_account_scheduler.go#L1044)、[评分](../../backend/internal/service/openai_account_scheduler.go#L1090)、
[管理员设置](../../backend/internal/service/openai_account_scheduler.go#L2149)。

## 收敛后的方案（建议，非现行规范）

| 入口/模式 | 存储和执行 | 计费归属 |
| --- | --- | --- |
| 原有单组 Key | 继续使用现有 `group_id`；Composite 路由和统一端点政策不变 | 原有余额/订阅规则 |
| 新聚合 Key：全部分组（默认） | 存储 `all` 模式，请求时读取全部符合条件的按量组 | 实际选中组的按量价格，扣用户余额 |
| 新聚合 Key：自定义分组 | 存储 `custom` 模式与非空分组关联，只在所选且当前有权使用的按量组中选择 | 实际选中组的按量价格，扣用户余额 |

建议增加显式 `group_binding_mode=single|all|custom`，现有记录默认 `single`，保持原 `group_id` 及转换字段原值。
新模式的 `group_id` 不充当路由来源；`all` 不存成员，`custom` 通过关联表保存成员并确保唯一性。
不能用 `group_id=null` 推断“全部”，因为既有未分组 Key 已有自己的行为；模式、单组字段和成员关系必须保持一致并原子更新。
这是新增 schema 的前向迁移，不需要重写旧 Key 的组绑定、订阅或转换配置。

### 创建与编辑

1. 保留现有“新建 Key”和单组/Composite 编辑流程，新增“创建聚合 Key”入口；新入口默认选“全部分组”，也可切换“自定义分组”。
2. 全部模式说明“自动使用你有权访问的全部按量分组，按实际使用组价格扣余额”，展示当前范围与报价；后续新增且已授权的按量组自动加入。
   模式是动态范围声明，不是创建时把所有复选框永久写入关联表；零候选时显示不可用，调用须明确拒绝，不能回退到全站账号池。
3. 自定义项展开项目现有风格的多选下拉：搜索、厂商筛选和复选项留在同一弹层，显示各组报价与已选数量；至少选一个。
   手动全选仍属于固定成员的 `custom` 模式，不因数量相同自动切换为动态 `all`。
4. 新聚合 Key 的创建/编辑均不展示统一协议转换项；原 Composite 的该项、校验及提交逻辑保留。
   额度、IP、有效期复用现有能力，不展示新增路由偏好；模型原生端点、基础地址和客户端示例按实际能力生成。
5. 原有 Key 不自动改成聚合模式；新聚合 Key 的编辑只修改其范围和通用限制。首版无需提供旧 Key 与新类型互转。

界面与复用依据：[选择控件约定](../../CONTRIBUTING.md#development-checks)、[现有报价](../AVAILABLE_CHANNELS.md#api-key-group-rate-disclosure)、
[原生能力组件](../../frontend/src/components/keys/NativeAccessFields.vue#L38)。

### 两种新聚合模式共用的执行规则

1. `all` 在请求开始时取得当前授权且启用的普通按量组集合；`custom` 是存储成员与该集合的交集，保存时也逐组校验。
   同一次请求使用可信的范围快照；下一次请求反映组和权限变化。被撤权成员不能由黏性或旧缓存继续授权。
2. 按公开模型、端点及现有能力/限制确定候选；需要明确模型归属，不能只凭模型名字前缀假定某个组可用。
   同一模型有多个可用组选项时，采用现有分组 `SortOrder` 再按 ID 确定结果，不新增用户排序或智能评分。
3. 按基本验证、只读解析实际组、审计、原有准入/账号调度/计费的边界接入；新模式只增加选组层，不复制账号或建立用户专属 Composite。
4. 每次请求固定 selected group、resolved platform、公开/上游模型和定价快照，沿用目标组个人价、按量价格、allowlist、RPM、客户端限制和渠道规则。
   采用标准余额分支及其 user-platform quota 检查，不挂接订阅、不因用户持有订阅自动改计费模式；组变成订阅类型后退出新聚合候选。
   使用请求内独立 effective-group 上下文，不能并发修改共享缓存的 `APIKey.GroupID/Group`。
5. Key 总额度及金额窗口跨组共享，用户全局并发/RPM共享；日志和账单保留 Key、实际组、供应商及价格证据，不虚构统一倍率。
   模型列表合并当前范围内可调用模型并去重、过滤端点能力；报价与执行使用相同范围、个人价和实际组归属。
6. 同一会话维持选定组及其现有账号亲和性；不因额度不足、审计拒绝、故障或已发生输出而新增跨组重试。
   新聚合调用遇到现有组级 fallback，也必须限制在本请求允许的按量组集合并检查目标组规则；原单组 Key 的 fallback 行为不变。
7. 两种新聚合模式都使用目标组/账号支持的原生协议，不提供 Key 级统一转换；WS、续会话与资源操作按下述所有权规则执行，无法确定所属组时返回明确错误。

依据：[组绑定授权](../../backend/internal/service/api_key_service.go#L491)、[组排序字段](../../backend/internal/service/group.go#L117)、
[订阅/余额分支](../../backend/internal/service/billing_cache_service.go#L836)、[定价捕获](../../backend/internal/service/billing_pricing_snapshot.go#L282)、
[跨组 RPM 规则](../GATEWAY_ADMISSION.md#one-count-per-external-request)。

### 协议转换的处理建议

按用户最新澄清，原 Composite 的统一转换能力、创建/编辑控件、服务端限制、配置字段及已有 Key 值均保留。
新增聚合 Key 不显示该配置；其 API 必须拒绝启用统一转换，防止绕过新类型的原生协议约束。
不做旧配置清理、批量原生化、统一转换废弃或依赖转换客户端的迁移。
模型原生端点、Base URL 和客户端示例在新入口按其真实模型范围生成；示例选择不影响凭据所有者身份。

依据：[入口策略](../../backend/internal/service/composite_access_policy.go#L39)、[现行转换契约](../COMPOSITE_GROUPS.md#optional-unified-endpoint-and-native-access)。

## 必须保留的实现边界

- **审计顺序**：鉴权/基本验证后，允许只读解析候选/目标组以确定审计作用域；首次选中组的 canonical audit 必须先于账号选择、RPM/并发、计费预留、协议转换及上游写入。
  每次跨组 failover 都须按新组作用域处理原始规范内容，并在新组的所有副作用前通过审计；不能一次 audit 后任意跨组。拒绝应终止，不能寻找宽松组。
  原始有效但无法提取的结构仍按现行 pass-through 契约处理，不变成审计 503/违规。
  依据：[权威审计规则](../SECURITY_AUDIT_CONTENT_COVERAGE.md#boundary-and-ordering)、[统一协议规则](../SECURITY_AUDIT_CONTENT_COVERAGE.md#composite-endpoint-admission-and-optional-conversion)、
  [Messages 审计先于准入](../../backend/internal/handler/gateway_handler.go#L237)。
- **认证缓存**：当前快照含一个 Group 及其模型/计费配置，Key 更新清理 L1/L2 并发布失效消息，旧版本快照回源。
  新字段必须同步 schema、DTO、repository projection、snapshot、失效索引与缓存版本；旧记录以 `single` 原样加载，不能因 Group 为空识别成新聚合。
  `all` 不能永久缓存创建时成员，需覆盖新组授权、撤权、启停及计费类型变化；`custom` 另覆盖成员更新。黏性不能保留已撤销权限。
  依据：[快照结构](../../backend/internal/service/api_key_auth_cache.go#L12)、[版本检查](../../backend/internal/service/api_key_auth_cache_impl.go#L318)、
  [跨实例失效](../../backend/internal/service/api_key_auth_cache_impl.go#L254)、[按组失效](../../backend/internal/service/api_key_auth_cache_invalidate.go#L26)。
- **有状态资源**：Files、Gemini cache、异步任务、`previous_response_id` 等保持创建时的 Key/Group/Account 归属与价格证据；
  不能按今天的最便宜组重定位。WS 每个 turn 仍须审计，但连接、续会话和 provider-owned ID 不能随偏好任意跨组。
  已有资源通过持久化归属定位组；新建操作需由模型、可信会话或明确绑定确定组，缺失或歧义须返回明确错误。选定组后沿用其原有能力与访问约束。
  依据：[Composite 状态/WS/缓存边界](../COMPOSITE_GROUPS.md#route-registry)、[续会话限制](../../backend/internal/service/composite_access_policy.go#L78)、
  [WS turn 契约](../GATEWAY_ADMISSION.md#one-count-per-external-request)。
- **出站身份**：客户端示例选择只影响展示；实际供应商身份仍由所选凭据所有者与既定优先级决定。
  不允许把客户端 UI 选择写入 outbound identity。依据：[身份规范](../OUTBOUND_IDENTITY.md)、[现行客户端字段语义](../COMPOSITE_GROUPS.md#optional-unified-endpoint-and-native-access)。

实施前创建本地不入库的 OpenSpec 计划，明确按量候选资格、动态全部/固定自定义、同名模型选组、授权、计费、错误和新增 schema。
验收至少覆盖：全部模式动态纳入新增授权按量组并移除失效成员、自定义不自动扩展、无候选/越权/订阅组及 Composite 隔离、
真实所选组个人价与余额扣费且不消耗订阅、同 Key 跨组共享总额、allowlist/RPM、同名模型稳定选组、fallback 不越出集合、
新增聚合无统一转换、原 Composite 统一转换开关/配置/执行保持、旧单组及未分组 Key 原行为保持、
审计拒绝无副作用、认证缓存失效、不可重放与资源所有权不变；测试预期来自这些业务要求，不从实现输出反推。
所有测试、lint、类型检查及策略验证必须在仓库规定的验证容器运行，结束后执行对应范围清理。
依据：[贡献与验证规则](../../CONTRIBUTING.md)、[迁移规范](../../backend/migrations/README.md)。

## 本次发现并修正的文档漂移

1. `COMPOSITE_GROUPS.md` 原先笼统称具体平台参与用户额度检查，并称订阅请求按具体平台检查 quota；
   [实际实现](../../backend/internal/service/billing_cache_service.go#L849) 仅在余额模式检查 user-platform quota，订阅检查所选组订阅额度。
   已在[平台说明](../COMPOSITE_GROUPS.md#supported-providers)和[管理工作流](../COMPOSITE_GROUPS.md#admin-workflows)区分两种额度，不改变实现。
2. 同文原先固定写认证快照 version 27；[源码常量](../../backend/internal/service/api_key_auth_cache_impl.go#L17)已为 28。
   已改为“versioned auth snapshot”，避免重复维护数字，保留旧版本回源语义。

在上述核对范围内未发现其他已证实的文档/实现矛盾；本次没有进行全仓库审计，也没有验证外站实现。
