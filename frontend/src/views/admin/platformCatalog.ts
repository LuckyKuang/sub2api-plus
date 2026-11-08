/**
 * 平台清单：后端 ProviderProfile 的前端镜像。
 *
 * 数据来自后端测试 TestFrontendBuiltinPlatformCatalogInSync 生成并校验的
 * platformCatalog.builtin.json（前端随后端一同构建发布，无需运行时拉取）。
 * 本模块是视图层的本地副本：src/constants/platformCatalog.ts 仍由其他负责人
 * 迁移，这里提供账号/设置视图需要的清单查询能力。
 */

export type PlatformGateway = 'anthropic' | 'openai' | 'gemini' | 'antigravity'
export type ProviderRouting = 'by_inbound' | 'by_model'
export type ProviderNativeProtocol = 'chat_completions' | 'anthropic' | 'responses'

export interface ProviderProtocolRule {
  pattern: string
  /** 首选协议；入站协议不在 protocols 中时使用。 */
  protocol: string
  /** 模型支持的协议集合（首项即 protocol）；入站协议在其中时同协议直通。仅一个协议时省略。 */
  protocols?: string[]
}

export interface ProviderModeInfo {
  mode: string
  /** 以上游协议为键的官方默认基址；缺少某协议即该模式不提供该原生端点。 */
  base_urls: Partial<Record<string, string>>
  protocol_rules?: ProviderProtocolRule[]
}

export interface ProviderProfileInfo {
  default_mode: string
  routing: ProviderRouting
  responses_path?: string
  /** 上游 /models 为每个模型给出 supported_endpoints，按模型分流时未命中规则的模型据此选协议。 */
  model_catalog?: boolean
  /** 默认模式在前。 */
  modes: ProviderModeInfo[]
}

export interface PlatformSpec {
  id: string
  display_name: string
  gateway: PlatformGateway
  cn_provider: boolean
  litellm_provider?: string
  multi_protocol?: ProviderProfileInfo
}

export interface PlatformCatalog {
  platforms: PlatformSpec[]
  /** 组合分组逐平台回退的顺序（先命中者生效）。 */
  composite_precedence: string[]
}

export const BUILTIN_PLATFORM_CATALOG: PlatformCatalog = {
  platforms: [
    { id: 'anthropic', display_name: 'Anthropic', gateway: 'anthropic', cn_provider: false, litellm_provider: 'anthropic' },
    { id: 'openai', display_name: 'OpenAI', gateway: 'openai', cn_provider: false, litellm_provider: 'openai' },
    { id: 'gemini', display_name: 'Gemini', gateway: 'gemini', cn_provider: false, litellm_provider: 'gemini' },
    { id: 'antigravity', display_name: 'Antigravity', gateway: 'antigravity', cn_provider: false, litellm_provider: 'anthropic' },
    { id: 'grok', display_name: 'Grok', gateway: 'openai', cn_provider: false, litellm_provider: 'xai' },
    {
      id: 'kimi',
      display_name: 'Kimi',
      gateway: 'openai',
      cn_provider: true,
      litellm_provider: 'moonshot',
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_inbound',
        modes: [
          {
            mode: 'payg',
            base_urls: {
              anthropic: 'https://api.moonshot.cn/anthropic',
              chat_completions: 'https://api.moonshot.cn/v1',
              responses: 'https://api.moonshot.cn/v1'
            }
          },
          {
            mode: 'coding',
            base_urls: {
              anthropic: 'https://api.kimi.com/coding',
              chat_completions: 'https://api.kimi.com/coding/v1',
              responses: 'https://api.kimi.com/coding/v1'
            }
          }
        ]
      }
    },
    {
      id: 'zhipu',
      display_name: 'Zhipu GLM',
      gateway: 'openai',
      cn_provider: true,
      litellm_provider: 'zhipu',
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_inbound',
        modes: [
          {
            mode: 'payg',
            base_urls: {
              anthropic: 'https://open.bigmodel.cn/api/anthropic',
              chat_completions: 'https://open.bigmodel.cn/api/paas/v4'
            }
          },
          {
            mode: 'coding',
            base_urls: {
              anthropic: 'https://open.bigmodel.cn/api/anthropic',
              chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4'
            }
          }
        ]
      }
    },
    {
      id: 'deepseek',
      display_name: 'DeepSeek',
      gateway: 'openai',
      cn_provider: true,
      litellm_provider: 'deepseek',
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_inbound',
        responses_path: '/responses',
        modes: [
          {
            mode: 'payg',
            base_urls: {
              anthropic: 'https://api.deepseek.com/anthropic',
              chat_completions: 'https://api.deepseek.com',
              responses: 'https://api.deepseek.com'
            }
          }
        ]
      }
    },
    {
      id: 'minimax',
      display_name: 'MiniMax',
      gateway: 'openai',
      cn_provider: true,
      litellm_provider: 'minimax',
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_inbound',
        modes: [
          {
            mode: 'payg',
            base_urls: {
              anthropic: 'https://api.minimaxi.com/anthropic',
              chat_completions: 'https://api.minimaxi.com/v1',
              responses: 'https://api.minimaxi.com/v1'
            }
          },
          {
            mode: 'coding',
            base_urls: {
              anthropic: 'https://api.minimaxi.com/anthropic',
              chat_completions: 'https://api.minimaxi.com/v1',
              responses: 'https://api.minimaxi.com/v1'
            }
          }
        ]
      }
    },
    {
      id: 'stepfun',
      display_name: 'StepFun',
      gateway: 'openai',
      cn_provider: true,
      litellm_provider: 'stepfun',
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_inbound',
        modes: [
          { mode: 'payg', base_urls: { chat_completions: 'https://api.stepfun.com/v1' } },
          { mode: 'coding', base_urls: { chat_completions: 'https://api.stepfun.com/step_plan/v1' } }
        ]
      }
    },
    {
      id: 'opencode_go',
      display_name: 'OpenCode',
      gateway: 'openai',
      cn_provider: false,
      litellm_provider: 'opencode-go',
      multi_protocol: {
        default_mode: 'go',
        routing: 'by_model',
        modes: [
          {
            mode: 'go',
            base_urls: {
              anthropic: 'https://opencode.ai/zen/go',
              chat_completions: 'https://opencode.ai/zen/go/v1',
              responses: 'https://opencode.ai/zen/go/v1'
            },
            protocol_rules: [
              { pattern: 'grok-*', protocol: 'responses' },
              { pattern: 'gpt-*', protocol: 'responses' },
              { pattern: 'muse-spark-*', protocol: 'responses' },
              { pattern: 'minimax-*', protocol: 'anthropic' },
              { pattern: 'qwen*', protocol: 'anthropic' }
            ]
          },
          {
            mode: 'zen',
            base_urls: {
              anthropic: 'https://opencode.ai/zen',
              chat_completions: 'https://opencode.ai/zen/v1',
              responses: 'https://opencode.ai/zen/v1'
            },
            protocol_rules: [
              { pattern: 'grok-*', protocol: 'responses' },
              { pattern: 'gpt-*', protocol: 'responses' },
              { pattern: 'muse-spark-*', protocol: 'responses' },
              { pattern: 'claude-*', protocol: 'anthropic' },
              { pattern: 'qwen3.8-max', protocol: 'chat_completions' },
              { pattern: 'qwen*', protocol: 'anthropic' }
            ]
          }
        ]
      }
    },
    { id: 'typesafe', display_name: 'TypeSafe / Jev', gateway: 'anthropic', cn_provider: false, litellm_provider: 'typesafe' },
    {
      id: 'command_code',
      display_name: 'Command Code',
      gateway: 'openai',
      cn_provider: false,
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_model',
        model_catalog: true,
        modes: [
          {
            mode: 'payg',
            base_urls: {
              anthropic: 'https://api.commandcode.ai/provider',
              chat_completions: 'https://api.commandcode.ai/provider/v1',
              responses: 'https://api.commandcode.ai/provider/v1'
            },
            protocol_rules: [
              { pattern: 'claude-*', protocol: 'anthropic' },
              { pattern: 'gpt-*', protocol: 'responses', protocols: ['responses', 'chat_completions'] }
            ]
          }
        ]
      }
    },
    {
      id: 'cline',
      display_name: 'Cline',
      gateway: 'openai',
      cn_provider: false,
      multi_protocol: {
        default_mode: 'payg',
        routing: 'by_inbound',
        modes: [{ mode: 'payg', base_urls: { chat_completions: 'https://api.cline.bot/api/v1' } }]
      }
    }
  ],
  composite_precedence: [
    'anthropic',
    'gemini',
    'openai',
    'antigravity',
    'grok',
    'kimi',
    'zhipu',
    'deepseek',
    'minimax',
    'stepfun',
    'opencode_go',
    'typesafe',
    'command_code',
    'cline'
  ]
}

let catalogState: PlatformCatalog = BUILTIN_PLATFORM_CATALOG

/** 替换当前清单（测试用于模拟新登记的平台）。 */
export function setPlatformCatalog(catalog: PlatformCatalog): void {
  catalogState = catalog
}

/** 恢复内置清单（测试用）。 */
export function resetPlatformCatalog(): void {
  catalogState = BUILTIN_PLATFORM_CATALOG
}

/** 全部具体平台，按展示顺序。 */
export function listPlatforms(): PlatformSpec[] {
  return catalogState.platforms
}

export function listPlatformIds(): string[] {
  return listPlatforms().map(spec => spec.id)
}

export function getPlatformSpec(platform: string | null | undefined): PlatformSpec | undefined {
  if (!platform) return undefined
  return catalogState.platforms.find(spec => spec.id === platform)
}

export function isKnownPlatform(platform: string | null | undefined): boolean {
  return !!getPlatformSpec(platform)
}

/** 与后端 buildUpstreamModelsRequest 支持的平台保持一致。 */
export function supportsUpstreamModelSync(platform: string | null | undefined): boolean {
  const spec = getPlatformSpec(platform?.trim().toLowerCase())
  if (!spec) return false
  return !!spec.multi_protocol || ['anthropic', 'openai', 'gemini', 'antigravity', 'grok'].includes(spec.id)
}

/** 清单中声明了定价目录 provider 的平台才支持同步定价模型。 */
export function supportsPricingModelSync(platform: string | null | undefined): boolean {
  return !!getPlatformSpec(platform?.trim().toLowerCase())?.litellm_provider?.trim()
}

/** 平台展示名；未登记的值原样返回。 */
export function platformDisplayName(platform: string | null | undefined): string {
  return getPlatformSpec(platform)?.display_name ?? platform ?? ''
}

export function compositePrecedencePlatformIds(): string[] {
  return catalogState.composite_precedence
}

export function platformUsesGateway(platform: string | null | undefined, gateway: PlatformGateway): boolean {
  return getPlatformSpec(platform)?.gateway === gateway
}

export function getProviderProfile(platform: string | null | undefined): ProviderProfileInfo | undefined {
  return getPlatformSpec(platform)?.multi_protocol ?? undefined
}

/** 指定接入模式的端点；未知模式回落默认模式（与后端 ProviderProfile.Endpoints 一致）。 */
export function getProviderMode(platform: string | null | undefined, mode: string | null | undefined): ProviderModeInfo | undefined {
  const profile = getProviderProfile(platform)
  if (!profile) return undefined
  if (!mode) return profile.modes[0]
  return profile.modes.find(item => item.mode === mode) ?? profile.modes[0]
}

export interface PlatformOption {
  value: string
  label: string
}

/** 账号与路由支持的具体上游平台选项，按展示顺序。 */
export const CONCRETE_PLATFORM_OPTIONS: PlatformOption[] = listPlatforms().map(spec => ({
  value: spec.id,
  label: spec.display_name
}))
