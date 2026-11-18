# 模型质量巡检

按分组对**单个模型**做定时巡检：内置题库判卷、保留原始回答、作品题可在沙箱中预览并按时间对比。
本功能**仅管理员可见**，不提供用户端入口、匿名访问或分享链接。

## 开关与费用

功能受 `系统设置 → 功能开关 → 模型质量巡检` 控制，**默认关闭**。

| 层级 | 行为 |
|---|---|
| 总开关关闭 | 调度器每轮直接返回；管理接口统一 403；侧边栏无入口 |
| 总开关开启、无计划 | 调度器空转（30 秒心跳），上游请求数 0 |
| 计划未启用 | 不能手动运行；定时不参与 |
| 计划启用但无可用账号 | 记一轮「账号不可用」，`request_count=0` |
| 计划启用且有账号 | 按 `账号数 × 启用题请求次数` 发起真实请求并计费 |

单轮请求上限由 `单轮请求上限`（默认 30）约束，保存与运行两处都会校验；手动运行需要二次确认。

## 检测模型与账号

- 一个计划只测**一个 `model`**，不枚举账号下的其他模型。需要比较两个模型时建两个计划。
- 平台由分组决定，认证方式由账号自身决定：OAuth 与 API Key 账号都支持，同一计划内可混合。
- 已接入的平台：`anthropic`、`openai`（Codex OAuth / API Key）、`gemini`（含 Vertex）、`grok`、
  国产厂商（`kimi` / `zhipu` / `deepseek` / `stepfun` / `minimax`）、`antigravity`。
- **复合分组**（`platform=composite`）支持，但必须指定模型：运行时用复合路由把公开模型解析成
  目标平台与上游模型；无匹配路由、或多平台歧义时拒绝保存。
- 出站身份沿用 `docs/OUTBOUND_IDENTITY.md` 的既有解析（凭据持有账号 > 全局预设 > 编译默认），巡检不新增旁路。

## 内置题库

| ID | 标题 | 类型 | 默认答案 | 判定方式 |
|---|---|---|---|---|
| `candy` | 糖果题 | number | 21 | 只认「最终答案：N」/ `\boxed{}` / 单行裸答案 |
| `logic-choice` | 逻辑选择题 | choice | 3 | 单字符且属于选项 |
| `numeric` | 数值计算 | number | 323 | 同上 |
| `json-contract` | JSON 指令遵循 | json | `{"answer":42,"unit":"items"}` | 键集 + 类型 + 值全等 |
| `fingerprint` | 指纹观察 | reference | — | 不判定，不进通过率 |
| `pelican` | 鹈鹕骑自行车 | drawing | — | `<svg>…</svg>` 完整即通过，不评质量 |

计划内可多选题目，并修改**题干、标准答案、每题请求次数（1–3）**。任何改动都会改变题目版本
（`version`），历史结果按版本隔离；跨版本对比会提示不可直接比较。

## 判读口径

- **本轮结论**：该轮所有启用的标准答案题聚合，优先级 `error > quota > fail > unknown > reference > pass`。
  单轮 `fail` 是排查线索，不等同「模型不合格」。
- **稳定性**：结果页用近 N 轮（默认 5）有明确判定的记录计算通过率；样本少于 3 轮时显示「样本不足」。

请求故障（超时、5xx、鉴权失败）与额度不足单独记录，不判为答错。

## 结果与作品

- 轮次时间轴 + 单轮详情：每题显示判定、识别答案、标准答案、耗时、原始回答。
- 作品题（`drawing`）在通过时把 HTML 写入**与异步生图同一个对象存储桶**，键为
  `<prefix>/quality-inspection/<yyyy/mm/dd>/<planID>/<runID>-<resultID>.html`。
- 作品经本服务固定路由转出，携带 `sandbox allow-scripts` + `default-src 'none'` 的 CSP；
  **不使用预签名 URL**（会在桶域名上绕过 CSP）。
- 浏览器 iframe 无法携带 `Authorization`，因此作品链接由管理员会话换取 10 分钟签名。
- 对比视图支持勾选 2–4 条历史并排渲染。

> 对象存储是**硬依赖**：未配置或被关闭时，含 `drawing` 题的计划无法启用。纯标准答案题不受影响。

## 自动剔除

设置页与计划中提供「自动剔除 + 冷却时长 + 连续 N 轮」的配置项，默认关闭且需要全局能力闸门先开启。

**当前版本仅保存配置并展示剔除记录，不执行任何调度写入。** 自动剔除属于高危链路（会修改生产账号的
`schedulable`），按方案分期在 P2 接入，接入后行为为：

1. 连续 `quarantine_fail_rounds` 轮（默认 2）明确答错，且这些轮次不含请求故障或额度不足
2. 写 `schedulable=false` 并回读确认
3. 冷却期满后，保持不可调度直连复测原账号，全部通过才恢复

`drawing`、`reference`、`unknown`、`error`、`quota` 均不会触发剔除。

## 接口

全部位于 `/api/v1/admin/quality-inspections`，需管理员 JWT：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `` | 计划列表 + 全局设置 + 内置题库 |
| POST | `` | 新建计划 |
| PUT | `/plans/:id` | 更新（带 `revision` 乐观锁） |
| DELETE | `/plans/:id` | 删除计划与历史 |
| POST | `/plans/:id/run` | 手动运行（`confirmed=true`） |
| GET | `/plans/:id/runs` | 轮次列表 |
| GET | `/runs/:runId` | 轮次详情（含每题结果） |
| POST | `/runs/:runId/stop` | 取消排队中的轮次 |
| GET | `/results/:id` | 单条结果 |
| GET | `/results/:id/artifact-url` | 换取作品签名 URL（10 分钟） |
| GET | `/results/:id/artifact?exp=&token=` | 作品字节流（签名校验，CSP 沙箱） |
| GET | `/compare?ids=` | 2–4 条记录对比元数据 |
| GET | `/questions` | 内置题库 |
| POST | `/estimate` | 预估请求数 |
| GET | `/quarantines` | 剔除记录（只读） |
| POST | `/quarantines/:accountId/handover` | 停止托管 |

## 已知限制

1. **思考强度目前只记录在快照中，不注入上游请求**：探测复用账号连通性测试的既有适配，该路径
   不传递 `reasoning_effort`。需要按平台注入时另行改造，不能靠修改 prompt 伪装。
2. 自动剔除执行链路未接入（见上）。
3. 轮次淘汰按 `max_saved_rounds` 删除结果与作品对象；若对象存储适配器未实现 `ImageStorageDeleter`，
   仅跳过删除并记日志，需依赖桶生命周期规则兜底。
4. 巡检复用账号连通性测试的探测路径，因此单次探测的超时与输出上限受该路径约束。
