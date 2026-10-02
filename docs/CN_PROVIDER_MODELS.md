# DeepSeek、Kimi、Zhipu、MiniMax 模型目录

官方文档核对日期：**2026-10-02**。当前对话及视觉理解模型维护在
[`models.json`](../backend/internal/pkg/cnmodels/models.json)，后端通过 Go embed、
前端通过静态导入共用该文件。更新目录时同时核对下列官方来源，不再分别维护两份列表。

## 官方来源与使用限制

| 平台 | 官方来源 | 当前目录与注意事项 |
| --- | --- | --- |
| DeepSeek | [模型与价格](https://api-docs.deepseek.com/quick_start/pricing) | 展示 `deepseek-flash`、`deepseek-v4.1-flash`、`deepseek-v4-flash-vision-exp`、`deepseek-v4-flash`、`deepseek-v4-pro`。`deepseek-flash` 当前指向 V4.1 Flash；官方接受两个 V4 Flash 旧名并由上游路由到新版 Flash。上述名称在项目中分别保留，不自动相互映射。开源权重、蒸馏模型和旧 R1/V3 名称不作为官方 API 当前预设。 |
| Kimi | [模型列表](https://platform.kimi.ai/docs/models)、[Kimi Code 接入说明](https://www.kimi.com/code/docs/en/benefits.html) | K3、K2.7 Code／highspeed、K2.6；Coding 端点另有 `kimi-for-coding`、`kimi-for-coding-highspeed`，需要对应会员权限。旧 moonshot-v1、K2、K2.5、kimi-latest 已下线，不加入新预设。 |
| Zhipu | [模型概览](https://docs.bigmodel.cn/cn/guide/start/model-overview)、[价格](https://docs.bigmodel.cn/cn/guide/start/pricing)、[GLM-5.3 Flash／FlashX](https://docs.bigmodel.cn/cn/guide/models/vlm/glm-5.3-flash) | 采用上述文档及对话补全接口仍列出的文本／视觉模型，最新系列为 `glm-5.3`、`glm-5.3-flash`、`glm-5.3-flashx`。仍在官方文档中的历史、免费型号继续提供；旧 chatglm 预设及独立图像生成、音频、视频生成、向量接口模型不加入本目录。 |
| MiniMax | [模型概览](https://platform.minimaxi.com/docs/guides/models-intro)、[Chat Completions](https://platform.minimaxi.com/docs/api-reference/text-chat-openai)、[M Plan](https://platform.minimaxi.com/docs/m-plan/intro) | 按官方接口枚举维护 9 个模型 ID，保留 `MiniMax-` 大小写及 `highspeed` 后缀。最新 `MiniMax-M3.1-Flash-Preview` 暂时仅通过 M Plan 和 MiniMax Code 提供；M3、M2.7 及官方仍列出的 M2 历史型号继续提供。旧 abab 系列不加入新预设。 |

候选列表是平台的维护目录，不代表当前账号、套餐或地区必然可以调用全部条目。
账号选择器的上游同步仍用于取得该账号实际可用的模型；管理员也可以手工添加模型、映射与通配符。
目录更新只更新模型 ID，不自动调整价格、能力声明或请求参数。

DeepSeek 的 `deepseek-v4.1-flash` 按维护者要求加入版本化候选；此次核对的官方价格页
明确推荐 `deepseek-flash`，未单独列出该版本化 API ID。项目允许并原样发送这个名称，
是否支持、指向哪个版本由上游决定。`deepseek-flash` 也不在项目中固定为 V4.1，
不预设它未来的版本绑定。以上规则不新增默认映射；管理员主动配置的映射仍按既有规则执行。

## 分组模型白名单

`GET /api/v1/admin/groups/:id/model-allowlist-candidates` 沿用管理员认证：

- `id=0&platform=...`：创建流程，返回所选平台的默认模型。
- `id>0&platform=...`：编辑流程，返回所选平台的默认模型，并合并该分组同平台可调度账号 `model_mapping` 的**键**；其他平台账号的映射不混入。
- 省略 `platform` 时，已有分组使用保存的平台；创建流程仍默认 Anthropic。非法平台返回 400，不回退到 Claude。
- 复合分组取各平台默认模型的去重并集，再合并具体平台账号的映射键。

四个平台不再因为缺少分支而返回 Claude 默认列表；账号显式配置的 Claude 兼容映射名称仍可以作为候选。
创建流程保留现有默认全选行为。编辑已有白名单时保留已保存的模型、顺序、通配符和自定义别名，新增官方候选默认不选中。
平台切换继续重置新表单的候选状态；前端读取代次防止旧平台响应覆盖当前结果。

此次更新不迁移或改写数据库中已保存的白名单。若某个分组此前误保存了默认 Claude 候选，管理员应在编辑页面重新选择适用模型后保存。

## 客户端兼容与构建

普通 Claude 兼容网关模型列表保持原有映射语义；管理端原生候选不以 Claude 兼容默认列表作为来源。
现有 DeepSeek／MiniMax Codex 原生模型回退列表共用本目录，账号映射仍优先按既有规则生效。
目录更新不改变模型白名单的客户端模型匹配规则、入站审计顺序、计费、账号选择或可信出站身份。

Docker 前端构建阶段必须复制 `backend/internal/pkg/cnmodels/models.json` 到相同相对路径。
这份目录是静态构建输入，不增加部署变量，也不在打开管理页面时请求官网。
按 [CONTRIBUTING.md](../CONTRIBUTING.md) 在验证容器中运行目录、分组候选、前端组件及网关模型回归；
前端生产构建需使用 Docker 前端阶段的源文件集合验证该跨目录输入。
