# Sub2API Plus ← Official v0.1.173 合并评估

| 字段 | 值 |
| --- | --- |
| 评估日期 | 2026-08-10 |
| 本仓基线 | `v0.1.172+custom.001`（已发布，`main` 干净） |
| 应用版本 | `0.1.172+custom.001`（`backend/cmd/server/VERSION`） |
| 官方目标 | `v0.1.173` |
| 官方 commit | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` |
| 合并基线 | `155c494964c3ea6ecc31f52679525c1034bf0f16`（= `v0.1.172^{}`） |
| 评估方法 | `git fetch upstream`、`git merge-tree HEAD v0.1.173`、迁移内容 SHA256 对照、官方 Release notes |

本文是维护者决策文档，不替代 `UPSTREAM.md` / `release-notes.md`。合并落地后应更新那两份发布元数据，并可将本文标为 historical 或删除。

---

## 1. 结论（摘要）

| 问题 | 结论 |
| --- | --- |
| 要不要合 v0.1.173？ | **要合**。Grok 完整集成 + 渠道监控 V2 体量大，越拖分叉成本越高。 |
| 能否直接在 `main` 上盲合？ | **不能**。先开 `upgrade/upstream-v0.1.173`。 |
| 最大风险是什么？ | **迁移导入策略**（不是 42 个文本冲突本身）。 |
| 代码冲突规模 | **42** 个 content conflict；约 **188** 个文件可自动合并。 |
| 变更规模 | 官方 `v0.1.172..v0.1.173`：**120** commits，约 **352** files，`+33307 / -2271`。 |
| 建议发布形态 | `v0.1.173+custom.001` / OCI `v0.1.173-custom.001` |
| 粗工作量 | **3–5 人日**（冲突 + 迁移重编号 + 回归） |

**一句话：**  
官方 v0.1.173 值得合并；Plus 已发布 `v0.1.172+custom.001` 后，合并窗口干净（merge-base 正好是官方 v0.1.172）。硬门槛是：**只导入官方“新内容”迁移，并按本仓空号重编号；禁止改写已发布迁移；保住 Codex 身份优先级与 five-hour 等 Plus 不变量。**

---

## 2. 相对上次评估的修正 / 补遗

上次口头结论大体成立。本次在「已发布 172+custom.001」前提下复检后，修正与补遗如下：

### 2.1 必须修正：迁移身份是 filename，不是数字前缀

`backend/migrations/README.md` 与 `migrations_runner.go` 约定：

- `schema_migrations.filename` 是主键；
- **完整文件名**才是迁移身份；
- 校验的是 **filename + SHA256**；
- 仓库**历史允许**同号多文件；但 **不要再新增** 新的同号重复；
- 已发布迁移 **不可改、不可删、不可重命名**。

因此：

- 不能简单说「190–201 同号冲突就会覆盖」——同号不同名可以共存；
- 真正危险的是：把官方**同内容、不同文件名**的迁移再引进来 → runner 视为**新迁移**再执行一遍；
- 以及：把官方**新内容**文件用官方原名塞进来，继续扩大同号重复（README 不鼓励）。

### 2.2 内容级对照（比“同号不同义”更准确）

对 `>=180` 迁移做 SHA256 对照后：

#### 已在 Plus 中存在、与官方字节级相同（只是编号不同）

这些官方文件 **不应再导入**（导入会变成新 filename 再跑一遍；多数 `IF NOT EXISTS` 可扛，但无益且增加噪音）：

| 语义 | Plus 文件名 | 官方 v0.1.173 文件名 |
| --- | --- | --- |
| usage_log session_id | `189_add_usage_log_session_id.sql` | `187_add_usage_log_session_id.sql` |
| allow live request type | `190_allow_live_usage_request_type.sql` | `188_allow_live_usage_request_type.sql` |
| group allow live | `191_add_group_allow_live.sql` | `189_add_group_allow_live.sql` |
| email alias dedup | `192_add_users_email_alias_dedup_index_notx.sql` | `190_add_users_email_alias_dedup_index_notx.sql` |
| passkey | `196_passkey_credentials.sql` | `191_passkey_credentials.sql` |
| profit control | `198_group_profit_control.sql` | `192_group_profit_control.sql` |
| profit cache invalidation | `199_group_profit_control_auth_cache_invalidation.sql` | `193_group_profit_control_auth_cache_invalidation.sql` |
| upstream response model | `200_add_usage_log_upstream_response_model.sql` | `194_add_usage_log_upstream_response_model.sql` |
| upstream model mismatch index | `201_add_usage_log_upstream_model_mismatch_index_notx.sql` | `195_add_usage_log_upstream_model_mismatch_index_notx.sql` |

> 说明：官方 v0.1.172 的 audit 迁移在官方树是 `194/195`；Plus 因中间插入自有迁移，已将其落在 `200/201`。内容相同，**filename 不同**。

#### Plus 独有（必须保留，合并时不可丢）

| Plus 文件 | 说明 |
| --- | --- |
| `187_async_image_task_history.sql` | 异步生图任务历史 |
| `188_async_image_ops_error_task_link.sql` | 异步生图错误关联 |
| `193_add_ip_access_control.sql` | IP 访问控制 |
| `194_add_subscription_five_hour_quota.sql` | **订阅五小时额度（Plus 关键能力）** |
| `195_add_usage_log_first_output.sql` | first_output 观测 |
| `197_add_usage_log_tps_metadata.sql` | TPS 元数据 |

#### 官方 v0.1.173 新增、Plus 尚无（必须导入，建议重编号）

共 **17** 个 SQL（内容级 UP_ONLY）：

**渠道监控 V2（13）**

- `194_channel_monitor_v2.sql` … 经官方编号到 `206_channel_monitor_v2_privacy_defaults.sql`
- 官方在 **194/195** 上本身就有“audit + monitor”同号双文件（历史模式）

**Grok 分组计价 / 清理（4）**

- `217_group_video_model_prices.sql`
- `218_group_audio_voice_pricing.sql`
- `219_group_search_price_per_1k.sql`
- `220_clear_non_grok_video_generation_config.sql`（破坏性清理，有 backup 表）

官方树在 **207–216** 号为空档；**221+** 尚无文件。

### 2.3 官方 tag 内嵌 VERSION 滞后

| 引用 | `backend/cmd/server/VERSION` |
| --- | --- |
| 官方 tag `v0.1.172` | `0.1.171`（tag 时未同步） |
| 官方 tag `v0.1.173` | `0.1.172`（tag 时仍未到 0.1.173） |
| `upstream/main` 在 tag 后 1 commit | `chore: sync VERSION to 0.1.173`（`48eb3766d`） |

Plus 合并时 **不要** 采用 tag 树里的 `0.1.172`。应直接写成：

```text
0.1.173+custom.001
```

是否顺手并入 `upstream/main` 那 1 个 VERSION chore 不重要；以 Plus 命名规约为准。

### 2.4 邮箱域名限注：无新 SQL

官方“邮箱域名注册额度”主要是 **设置项 + 注册逻辑**（默认关闭），**没有**对应的新迁移文件。不要在迁移清单里凭空找 `email_domain` SQL。

### 2.5 当前仓状态确认（2026-08-10）

- 分支：`main`，与 `origin/main` 同步，working tree clean
- `UPSTREAM.md` 已将 `v0.1.172+custom.001` 标为 **published**
- 本地已有 tag：`v0.1.172+custom.001`、`v0.1.173`（官方）、`upstream-v0.1.172`
- 相对官方：`HEAD` 不含 173 功能（behind **120**）；Plus 自有提交 ahead 约 **99**（含历史 custom 提交）
- `git fetch upstream` 时 `v0.1.170` tag 拒绝 clobber：本地 annotated tag 与远端不一致属历史问题，**与 173 合并无直接关系**，但不要强推覆盖 tag

---

## 3. 官方 v0.1.173 功能清单

### 3.1 新增功能

1. **Grok/xAI 完整集成**
   - SSO 登录、`refresh_token` 重新授权
   - OAuth 会话跨实例共享（多副本）
   - 图片/视频媒体路由
   - Voice TTS / STT / Realtime、custom voices
   - `/v1/web_search`
   - 视频：模型族 × 分辨率定价
   - 搜索：每千次调用计价
   - 可配置默认文本模型与跨客户端映射开关
   - free 档本地 24h 软门禁；额度耗尽可恢复下线
   - team+model 冷却、流式空闲换号、7d/30d 调度阈值

2. **渠道监控 V2**
   - 基于真实网关流量的被动聚合（不再主动探活）
   - 健康 KPI、脉冲矩阵、趋势、模型/错误排行
   - 系统设置：v1（主动）/ v2（被动）互斥
   - 可对普通用户隐藏 RPM/TPM

3. **邮箱域名限量注册**
   - 白名单外域名按 eTLD+1 限 1 账户
   - **独立开关，默认关闭**

4. **管理端**
   - Grok 真实媒体预览测试
   - 分组页视频价矩阵输入

### 3.2 优化

- 上游响应模型观察热路径
- OpenAI OAuth routing hints；停止注入 legacy beta 头
- Grok free 24h 与付费窗口展示区分
- 导入探测队列有界去重
- 前端账号测试媒体/文件选择 i18n

### 3.3 修复

- Gemini 原生生图按实际上游回吐张数计费
- Gemini 池模式 429 误打账号级限流
- 非流式生图客户端断开导致出图不扣费
- Grok 异步视频 pending/失败误扣与重复扣费
- Grok 流式搜索重复计数
- Grok OAuth 客户端缺失崩溃
- Grok 模型级软封牵连同账号其它模型
- Web 搜索配置空值与重置弹窗滚动

### 3.4 破坏性变更（官方自述）

1. **Grok 跨厂商模型映射默认关闭**  
   `gpt-*` / `claude-*` 不再隐式改写为 `grok-4.5`。要旧行为须在系统设置显式开启。
2. **Grok 邮箱密码登录硬禁用**  
   `gateway.grok.password_auth_enabled` 仅兼容保留，服务端忽略。
3. **迁移 220**  
   清理非 Grok（且非 composite）分组上的历史视频定价；先写入 `groups_video_price_backup_220` 再清空。

---

## 4. 代码冲突清单（merge-tree）

`git merge-tree --write-tree --name-only HEAD v0.1.173` → **42** 个 content conflict。

### 4.1 发布 / 工具链（3）

| 文件 | 处理建议 |
| --- | --- |
| `Makefile` | 保留 Plus `check_openai_codex_identity.py`；并入官方 channel-monitor vitest 列表 |
| `backend/cmd/server/VERSION` | 固定为 `0.1.173+custom.001` |
| `frontend/pnpm-lock.yaml` | 解依赖后 `pnpm install` 重生，勿手合 |

### 4.2 生成物（5）— 不要长期手搓

| 文件 | 处理建议 |
| --- | --- |
| `backend/cmd/server/wire_gen.go` | schema/wire 注入合完后 **regen** |
| `backend/ent/group.go` | 同上 |
| `backend/ent/migrate/schema.go` | 同上 |
| `backend/ent/mutation.go` | 同上 |
| `backend/ent/runtime/runtime.go` | 同上 |

先合 `backend/ent/schema/*` 与 DI 注册，再统一生成。

### 4.3 OpenAI / Codex 网关（9）— Plus 硬规约区

- `backend/internal/handler/openai_gateway_handler_test.go`
- `backend/internal/service/openai_gateway_chat_completions.go`
- `backend/internal/service/openai_gateway_forward.go`
- `backend/internal/service/openai_gateway_grok.go`
- `backend/internal/service/openai_gateway_messages.go`
- `backend/internal/service/openai_gateway_passthrough.go`
- `backend/internal/service/openai_gateway_response_handling.go`
- `backend/internal/service/openai_images_test.go`
- `backend/internal/service/openai_ws_forwarder_payload.go`

**原则（AGENTS.md）：**  
有效 credential-owning account `credentials.user_agent` > 有效全局 `openai_codex_user_agent` > 编译默认。  
版本同步只能改选中身份的版本声明，不得改 source / family / Originator / OS / arch / terminal fingerprint。  
合并官方 routing hints、failover、Grok 桥接时，**不得打穿该优先级**。合并后跑 `tools/check_openai_codex_identity.py`。

### 4.4 Grok / xAI（5）

- `backend/internal/pkg/xai/oauth.go`
- `backend/internal/service/grok_media.go`
- `backend/internal/service/grok_oauth_service.go`
- `backend/internal/service/grok_oauth_service_test.go`
- `backend/internal/service/grok_quota_service_test.go`

以官方 173 能力为主，回填 Plus 已有扩展点；注意密码登录硬禁用与 SSO/RT 路径。

### 4.5 路由 / 网关核心 / 账号（5）

- `backend/internal/server/routes/admin.go`
- `backend/internal/server/routes/gateway.go`
- `backend/internal/service/account_test_service.go`
- `backend/internal/service/admin_account.go`
- `backend/internal/service/gateway_service.go`

### 4.6 设置（3）

- `backend/internal/service/setting_parse.go`
- `backend/internal/service/setting_public.go`
- `backend/internal/service/setting_service.go`

覆盖：channel monitor 模式、hide throughput、Grok 映射、邮箱域名限注开关等。

### 4.7 鉴权缓存 / 利润控制交叠（3）

- `backend/internal/service/api_key_auth_cache.go`
- `backend/internal/service/api_key_auth_cache_impl.go`
- `backend/internal/service/api_key_auth_cache_profit_test.go`

### 4.8 其它后端（4）

- `backend/internal/repository/http_upstream.go`
- `backend/internal/service/account_wildcard_test.go`
- `backend/internal/service/gemini_messages_compat_service.go`
- `backend/internal/service/wire.go`

### 4.9 前端（5）

- `frontend/src/api/__tests__/admin.system.rollback.spec.ts`
- `frontend/src/components/keys/UseKeyModal.vue`
- `frontend/src/i18n/locales/en/dashboard.ts`
- `frontend/src/i18n/locales/zh/dashboard.ts`
- `frontend/src/views/admin/GroupsView.vue`

中英文 locale key 必须对齐（AGENTS.md）。

---

## 5. 推荐迁移导入表（Plus 侧）

本仓当前最大前缀：**201**。  
空号从 **202** 起。  
按 README：**不要新增同号重复**；官方 194–206 / 217–220 的**新内容**在 Plus 侧改为连续新号。

### 5.1 建议映射（官方 filename → Plus 新 filename）

| 顺序 | 官方（勿改内容，只改名导入） | Plus 建议名 |
| ---: | --- | --- |
| 1 | `194_channel_monitor_v2.sql` | `202_channel_monitor_v2.sql` |
| 2 | `195_channel_monitor_mode.sql` | `203_channel_monitor_mode.sql` |
| 3 | `196_channel_monitor_v2_ignored_error_categories.sql` | `204_channel_monitor_v2_ignored_error_categories.sql` |
| 4 | `197_channel_monitor_v2_seed_popular_models.sql` | `205_channel_monitor_v2_seed_popular_models.sql` |
| 5 | `198_channel_monitor_v2_health_thresholds.sql` | `206_channel_monitor_v2_health_thresholds.sql` |
| 6 | `199_channel_monitor_v2_fixed_rollups.sql` | `207_channel_monitor_v2_fixed_rollups.sql` |
| 7 | `200_channel_monitor_v2_rollup_permissions.sql` | `208_channel_monitor_v2_rollup_permissions.sql` |
| 8 | `201_channel_monitor_v2_refresh_5m.sql` | `209_channel_monitor_v2_refresh_5m.sql` |
| 9 | `202_channel_monitor_v2_full_table_permissions.sql` | `210_channel_monitor_v2_full_table_permissions.sql` |
| 10 | `203_channel_monitor_v2_default_ignore_and_cache.sql` | `211_channel_monitor_v2_default_ignore_and_cache.sql` |
| 11 | `204_channel_monitor_hide_throughput.sql` | `212_channel_monitor_hide_throughput.sql` |
| 12 | `205_channel_monitor_v2_reset_factory_cache_thresholds.sql` | `213_channel_monitor_v2_reset_factory_cache_thresholds.sql` |
| 13 | `206_channel_monitor_v2_privacy_defaults.sql` | `214_channel_monitor_v2_privacy_defaults.sql` |
| 14 | `217_group_video_model_prices.sql` | `215_group_video_model_prices.sql` |
| 15 | `218_group_audio_voice_pricing.sql` | `216_group_audio_voice_pricing.sql` |
| 16 | `219_group_search_price_per_1k.sql` | `217_group_search_price_per_1k.sql` |
| 17 | `220_clear_non_grok_video_generation_config.sql` | `218_clear_non_grok_video_generation_config.sql` |

说明：

- **SQL 正文尽量原样**；若正文写死“迁移 220”“backup_220”等操作者可读字符串，可改为 Plus 新号以免运维对照混乱（**改的是注释/backup 表名还是逻辑，需单独审**）。  
  - `220` 正文含 `groups_video_price_backup_220`：若改表名，只影响新库/新升级；确定后全仓搜索引用。
- **不要**导入 §2.2 中“字节级相同”的官方 audit/passkey/profit 等文件。
- **不要**重命名或改写 Plus 已有 `187–201`。

### 5.2 升级路径影响

| 场景 | 行为 |
| --- | --- |
| 已部署 `v0.1.172+custom.001` | `schema_migrations` 已有 Plus `189–201` 等；只会新跑 `202–218`（上表） |
| 全新安装 | 先跑完整 Plus 历史链（含 five_hour 等），再跑 `202–218` |
| 误导入官方原名同内容文件 | 可能重复执行 DDL；`IF NOT EXISTS` 多数可过，但违反整洁性且 notx/数据迁移更危险 |

---

## 6. 合并影响面

### 6.1 对 Plus 必须保住的能力

| 能力 | 来源 | 合并时动作 |
| --- | --- | --- |
| Codex 出站身份优先级 | AGENTS.md | 冲突以 Plus 为准；补测 |
| 订阅 five-hour 额度 | Plus `194_add_subscription_five_hour_quota.sql` + 业务代码 | 保留迁移与计费/展示逻辑 |
| 本地配额展示 / TPS / first_output | Plus 迁移 + 前端 | 回归 |
| 异步生图任务历史 | Plus `187/188` | 保留 |
| IP 访问控制 | Plus `193` | 保留 |
| 模块路径 `github.com/LuckyKuang/sub2api-plus` | go.mod | 勿被官方 module path 覆盖 |
| 发行命名 `+custom.NNN` / OCI `-custom.NNN` | UPSTREAM.md | 发布时更新映射 |
| push/release CLI skills | 本仓 `skills/` | 与官方无冲突，保持 |

### 6.2 对运行中部署的行为变化

- Grok 跨客户端映射**默认关** → 依赖隐式 `gpt→grok` 的流量会透传失败或走错模型，需公告/设置项。
- Grok 密码登录不可用 → 运营文档与 UI 入口对齐。
- 非 Grok 分组视频价可能被清理（导入的 clear 迁移）→ 先确认是否存在误写价；backup 表可回滚数据。
- 渠道监控可切换 V2；默认策略以合并后设置默认值为准（官方倾向兼容 v1）。
- 邮箱域名限注默认关，开启后影响注册。
- Gemini/OpenAI 计费与断开扣费更严（正向修复，但对账数字可能与历史宽松行为不同）。

### 6.3 对发布元数据

合并准备 PR 需同步：

- `backend/cmd/server/VERSION`
- `UPSTREAM.md` 增加 `v0.1.173+custom.001` 行与 Current Version
- `release-notes.md`（破坏性变更、迁移号、已知问题）
- 如有 README 版本徽章/安装示例，按三语文档规则对齐

---

## 7. 建议执行顺序

1. **分支**  
   `git checkout -b upgrade/upstream-v0.1.173`

2. **冻结迁移方案**  
   按 §5 表导入 17 个新 SQL（重编号），明确跳过内容重复的官方文件。

3. **merge**  
   `git merge v0.1.173`（或 merge `upstream/main` 若想带上 VERSION chore；仍以 Plus 版本字符串为准）。

4. **解冲突分层**  
   - 先 routes / service 业务  
   - 再 settings / Grok  
   - Codex 身份专项审  
   - 最后 ent + wire **重新生成**  
   - 前端 i18n + `pnpm-lock` 重生  
   - `VERSION` / `Makefile` 收尾

5. **验证**  
   - 新库：全量 migrate  
   - 从 `0.1.172+custom.001` 数据目录升级：仅新 filename 执行  
   - `go test`（至少 migrations、repository、gateway、grok、settings 相关）  
   - `tools/check_openai_codex_identity.py`  
   - 前端 lint / typecheck / 关键 vitest（含 channel-monitor-v2）  
   - 手工：Grok SSO/RT、映射默认关、监控 v1/v2、five_hour、profit control

6. **发布准备**（另 PR/流程，需明确 publication 请求才打 tag）  
   - `v0.1.173+custom.001`  
   - 更新 `UPSTREAM.md` / `release-notes.md`  
   - 不复用、不移动已发布 tag

---

## 8. 风险登记

| ID | 风险 | 等级 | 缓解 |
| --- | --- | --- | --- |
| R1 | 误导入官方同内容、不同名迁移导致重复执行 | 高 | 内容哈希清单；只导入 §5 十七项 |
| R2 | 手改 ent/wire 导致与 schema 漂移 | 高 | 只 regen，不手搓生成物 |
| R3 | Codex 身份优先级被官方 UA/routing 改动打穿 | 高 | AGENTS 规约 + 专用检查脚本 + 单测 |
| R4 | five_hour / profit / async image 在合并中丢失 | 高 | 合并后 diff 检查 Plus 独有迁移与调用点 |
| R5 | clear 非 Grok 视频价影响存量 | 中 | 读 backup 逻辑；预发环境 dry-run；公告 |
| R6 | Grok 映射默认关导致兼容性投诉 | 中 | release notes + 管理端设置说明 |
| R7 | 前端中英文 key 不对齐 | 中 | locale 对称检查 |
| R8 | 官方 tag VERSION 误导 | 低 | Plus 固定写 `0.1.173+custom.001` |
| R9 | 本地 `v0.1.170` tag 与上游不一致 | 低 | 不 clobber；与 173 无关 |

---

## 9. 明确不建议的做法

- 在 `main` 直接 merge 后边写边发  
- 重命名/修改已发布的 Plus `187–201` 迁移以“对齐官方编号”  
- 为省事保留官方原 filename 并继续制造新的同号重复（历史允许，但 README 禁止新增）  
- 把官方 `go.mod` module path 盖回 `Wei-Shaw/sub2api`  
- 跳过 Codex 身份与 five_hour 回归  
- 无 publication 请求时 push tag / Release / OCI

---

## 10. 最终建议

| 决策项 | 建议 |
| --- | --- |
| 是否合并官方 v0.1.173 | **是** |
| 何时 | 尽快开升级分支；不阻塞在已发布的 172+custom.001 上继续堆业务 |
| 发布号 | `v0.1.173+custom.001` |
| 第一优先级 | 迁移：只导入 17 个新内容文件并重编号为 `202–218` |
| 第二优先级 | 42 文件冲突中 Codex / Grok / settings / 生成物分层处理 |
| 第三优先级 | 双路径 migrate + Plus 不变量回归 + 破坏性变更公告 |
| 是否可与其它功能开发并行 | 升级分支隔离；避免在升级未完成时在 main 再加迁移占号 |

---

## 11. 附录

### 11.1 复现命令

```bash
git fetch upstream --tags --prune
git rev-parse HEAD v0.1.172^{} v0.1.173^{}
git merge-base HEAD v0.1.173   # 期望 = v0.1.172^{}
git merge-tree --write-tree --name-only HEAD v0.1.173
git log --oneline v0.1.172..v0.1.173
git diff --shortstat v0.1.172..v0.1.173
```

### 11.2 关键引用

- 仓库规约：`AGENTS.md`
- 上游映射：`UPSTREAM.md`
- 迁移规则：`backend/migrations/README.md`
- 发布流程：`docs/RELEASING.md`
- 官方源：`https://github.com/Wei-Shaw/sub2api` tag `v0.1.173`

### 11.3 文档状态

| 项 | 值 |
| --- | --- |
| 路径 | `UPSTREAM-v0.1.173-MERGE-ASSESSMENT.md`（仓库根目录） |
| 性质 | 合并前评估；非发布说明 |
| 后续 | 合并 PR 合并后，将结论沉淀进 `release-notes.md` / `UPSTREAM.md`；本文可归档 |
