import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import { BUILTIN_PLATFORM_CATALOG, resetPlatformCatalog, setPlatformCatalog } from '@/constants/platformCatalog'

import {
  allModels,
  buildModelMappingObject,
  getModelsByPlatform,
  getPresetMappingsByPlatform,
  splitModelMappingObject
} from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('includes platform-specific TypeSafe and OpenCode candidates once in combined options', () => {
    const values = allModels.map(model => model.value)
    expect(new Set(values).size).toBe(values.length)
    for (const model of ['jev-latest', 'longcat-2.0', 'mimo-v2.5', 'minimax-m3', 'qwen3.8-max', 'hy4-preview', 'omen-alpha']) {
      expect(values).toContain(model)
    }
    expect(getModelsByPlatform('typesafe')).toEqual(['jev-latest'])
    expect(getModelsByPlatform('opencode_go')).toContain('mimo-v2.5')
  })

  it('offers current native Gemini mappings while retaining Antigravity legacy identities', () => {
    const presets = getPresetMappingsByPlatform('gemini')
    for (const model of ['gemini-3.1-flash-image', 'gemini-3.1-pro-preview', 'gemini-3.8-flash', 'gemini-4-argon']) {
      expect(presets).toContainEqual(expect.objectContaining({ from: model, to: model }))
    }
    expect(presets.some(preset => preset.from.startsWith('gemini-2.') || preset.to.startsWith('gemini-2.'))).toBe(false)
    expect(getModelsByPlatform('antigravity')).toContain('gemini-2.5-flash')
    expect(getPresetMappingsByPlatform('antigravity')).toContainEqual(expect.objectContaining({
      from: 'gemini-2.5-flash-lite', to: 'gemini-2.5-flash-lite'
    }))
  })

  it.each([
    ['deepseek', ['deepseek-flash', 'deepseek-v4.1-flash', 'deepseek-v4-flash-vision-exp', 'deepseek-v4-flash', 'deepseek-v4-flash-0731', 'deepseek-v4-pro', 'deepseek-v4-pro-0813'], ['deepseek-r1']],
    ['kimi', ['kimi-k3', 'kimi-k2.7-code-highspeed', 'kimi-for-coding-highspeed'], ['moonshot-v1-8k', 'kimi-latest', 'kimi-k2']],
    ['zhipu', ['glm-5.3-flashx', 'glm-5.2', 'glm-4.6v'], ['chatglm_turbo', 'cogvideo']],
    ['minimax', ['MiniMax-M3.1-Flash', 'MiniMax-M3', 'MiniMax-M2.7-highspeed'], ['abab6.5-chat', 'abab5.5-chat']],
    ['stepfun', ['step-5-preview', 'step-3.7-flash', 'step-3.5-flash-2603', 'step-3.5-flash', 'step-router-v1'], ['step-image', 'step-tts']],
  ])('uses current official %s models in both platform and combined options', (platform, current, removed) => {
    const models = getModelsByPlatform(platform as string)
    expect(new Set(models).size).toBe(models.length)
    expect(models.some(model => model.startsWith('claude-'))).toBe(false)
    for (const model of current) {
      expect(models).toContain(model)
      expect(allModels.some(option => option.value === model)).toBe(true)
    }
    for (const model of removed) expect(models).not.toContain(model)
  })

  it('平台清单中没有内置模型列表的多协议供应商不预填白名单', () => {
    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: [
        ...BUILTIN_PLATFORM_CATALOG.platforms,
        {
          id: 'acme_router',
          display_name: 'Acme Router',
          gateway: 'openai',
          cn_provider: false,
          multi_protocol: {
            default_mode: 'standard',
            routing: 'by_model',
            modes: [{ mode: 'standard', base_urls: { chat_completions: 'https://api.acme-router.example/v1' } }]
          }
        }
      ]
    })
    try {
      expect(getModelsByPlatform('acme_router')).toEqual([])
      expect(getModelsByPlatform('kimi').length).toBeGreaterThan(0)
      // 非多协议供应商的未知平台维持原有回退。
      expect(getModelsByPlatform('bedrock')).toEqual(getModelsByPlatform('anthropic'))
    } finally {
      resetPlatformCatalog()
    }
  })

  it('openai 模型列表包含 GPT-5.4 官方快照', () => {
    const models = getModelsByPlatform('openai')

    expect(models).toContain('gpt-5.4')
    expect(models).toContain('gpt-5.4-mini')
    expect(models).toContain('gpt-5.4-2026-03-05')
    expect(models).toContain('codex-auto-review')
    expect(models).toContain('gpt-5.6')
    expect(models).toContain('gpt-6-astra')
    expect(models).toContain('gpt-6.1-sol')
    expect(models).toContain('gpt-6.1-terra')
    expect(models).toContain('gpt-6-sol')
    expect(models).toContain('gpt-6-luna')
    expect(models).not.toContain('gpt-6')
    expect(new Set(models).size).toBe(models.length)
  })

  it('openai 预设映射将最新旗舰模型放在首位', () => {
    const presets = getPresetMappingsByPlatform('openai')

    expect(presets[0]).toMatchObject({
      from: 'gpt-6-astra',
      to: 'gpt-6-astra'
    })
    expect(presets.some(preset => preset.from === 'gpt-6')).toBe(false)
    expect(presets.map(preset => preset.from).slice(0, 12)).toEqual([
      'gpt-6-astra',
      'gpt-6.1-sol',
      'gpt-6.1-terra',
      'gpt-6-sol',
      'gpt-6-luna',
      'gpt-5.6',
      'gpt-5.6-sol',
      'gpt-5.6-terra',
      'gpt-5.6-luna',
      'gpt-5.5',
      'gpt-5.4',
      'claude-haiku-4-5-20251001'
    ])
  })

  it('openai 模型列表按新系列优先排列', () => {
    expect(getModelsByPlatform('openai').slice(0, 11)).toEqual([
      'gpt-6-astra',
      'gpt-6.1-sol',
      'gpt-6.1-terra',
      'gpt-6-sol',
      'gpt-6-luna',
      'gpt-5.6',
      'gpt-5.6-sol',
      'gpt-5.6-terra',
      'gpt-5.6-luna',
      'gpt-5.5',
      'gpt-5.4'
    ])
  })

  it('openai 模型列表不再暴露已下线的 ChatGPT 登录 Codex 模型', () => {
    const models = getModelsByPlatform('openai')

    expect(models).not.toContain('gpt-5')
    expect(models).not.toContain('gpt-5.1')
    expect(models).not.toContain('gpt-5.1-codex')
    expect(models).not.toContain('gpt-5.1-codex-max')
    expect(models).not.toContain('gpt-5.1-codex-mini')
    expect(models).not.toContain('gpt-5.2-codex')
  })

  it('antigravity 模型列表包含图片模型兼容项', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models).toContain('gemini-3-pro-image')
  })

  it('Claude 模型列表包含新发布的 Claude 模型', () => {
    expect(getModelsByPlatform('claude')).toContain('claude-fable-5-1')
    expect(getModelsByPlatform('antigravity')).toContain('claude-fable-5-1')
    expect(getModelsByPlatform('claude')).toContain('claude-fable-5')
    expect(getModelsByPlatform('antigravity')).toContain('claude-fable-5')
    expect(getModelsByPlatform('claude')).toContain('claude-opus-5-5')
    expect(getModelsByPlatform('antigravity')).not.toContain('claude-opus-5-5')
    expect(getModelsByPlatform('claude')).toContain('claude-sonnet-5-5')
    expect(getModelsByPlatform('antigravity')).not.toContain('claude-sonnet-5-5')
    expect(getModelsByPlatform('claude')).toContain('claude-opus-4-8')
    expect(getModelsByPlatform('antigravity')).toContain('claude-opus-4-8')
  })

  it('Claude Sonnet 5.5 预设使用各平台的官方模型 ID', () => {
    expect(getPresetMappingsByPlatform('claude')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'Sonnet 5.5', from: 'claude-sonnet-5-5', to: 'claude-sonnet-5-5' })
    ]))
    expect(getPresetMappingsByPlatform('bedrock')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'Sonnet 5.5', from: 'claude-sonnet-5-5', to: 'global.anthropic.claude-sonnet-5-5' })
    ]))
  })

  it('Claude 默认白名单保存后允许 Haiku 5.5，预设使用同一模型 ID', () => {
    const models: string[] = getModelsByPlatform('anthropic')
    const mapping: Record<string, string> | null = buildModelMappingObject('whitelist', models, [])

    expect(mapping?.['claude-haiku-5-5']).toBe('claude-haiku-5-5')
    expect(getPresetMappingsByPlatform('anthropic')).toEqual(expect.arrayContaining([
      expect.objectContaining({ label: 'Haiku 5.5', from: 'claude-haiku-5-5', to: 'claude-haiku-5-5' })
    ]))
  })

  it('xAI 模型列表包含 Grok 4.5 官方模型和别名', () => {
    const models = getModelsByPlatform('grok')

    expect(models).toContain('grok-4.6')
    expect(models).toContain('grok-4.6-latest')
    expect(models).toContain('grok-4.5')
    expect(models).toContain('grok-4.5-latest')
    expect(models).toContain('grok-build-latest')
    expect(models).toContain('grok-imagine-image-2.0')
    expect(models).toContain('grok-imagine-video-1.5')
  })

  it('combined 模式支持 Grok 4.5 官方别名映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['grok-4.5'],
      [
        { from: 'grok-latest', to: 'grok-4.5' },
        { from: 'grok-4.5-latest', to: 'grok-4.5' },
        { from: 'grok-build-latest', to: 'grok-4.5' }
      ]
    )

    expect(mapping).toEqual({
      'grok-4.5': 'grok-4.5',
      'grok-latest': 'grok-4.5',
      'grok-4.5-latest': 'grok-4.5',
      'grok-build-latest': 'grok-4.5'
    })
  })

  it('grok 模型列表包含 Composer 默认项和兼容别名', () => {
    const models = getModelsByPlatform('grok')

    expect(models).toContain('grok-composer-2.5-fast')
    expect(models).not.toContain('grok-composer')
    expect(models).toContain('composer-2.5')
  })

  it('gemini 模型列表包含原生生图模型', () => {
    const models = getModelsByPlatform('gemini')

    expect(models).toContain('gemini-3.1-flash-image')
    expect(models).toContain('gemini-3-pro-image')
    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(models.indexOf('gemini-3-flash'))
    expect(models.indexOf('gemini-3-pro-image')).toBeLessThan(models.indexOf('gemini-3.5-flash'))
  })

  it('gemini 模型列表不含已停用的 2.x 模型与 Antigravity 档位别名', () => {
    const models = getModelsByPlatform('gemini')

    for (const model of ['gemini-2.0-flash', 'gemini-2.5-flash', 'gemini-2.5-pro', 'gemini-2.5-flash-image', 'gemini-3-pro-preview']) {
      expect(models).not.toContain(model)
    }
    for (const model of ['gemini-3.1-pro', 'gemini-3.1-pro-high', 'gemini-3.1-pro-low']) {
      expect(models).not.toContain(model)
    }
    expect(models).toContain('gemini-3.8-flash')
  })

  it('antigravity 模型列表会把新的 Gemini 图片模型排在前面', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(models.indexOf('gemini-2.5-flash'))
    expect(models.indexOf('gemini-2.5-flash-image')).toBeLessThan(models.indexOf('gemini-2.5-flash-lite'))
  })

  it('antigravity 模型列表包含 Gemini 3.1 Pro 通用别名', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-3.1-pro')
  })

  it('Gemini 4 Argon 在 gemini 与 antigravity 平台使用对应真实模型 ID 与预设映射', () => {
    expect(getModelsByPlatform('gemini')).toContain('gemini-4-argon')
    const agModels = getModelsByPlatform('antigravity')
    expect(agModels).not.toContain('gemini-4-argon')
    expect(agModels).toContain('gemini-4-argon-high')
    expect(agModels).toContain('gemini-4-argon-medium')
    expect(agModels).toContain('gemini-4-argon-low')
    expect(getPresetMappingsByPlatform('antigravity')).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'gemini-4-argon', to: 'gemini-4-argon-high' }),
      expect.objectContaining({ from: 'gemini-4-argon-high', to: 'gemini-4-argon-high' }),
      expect.objectContaining({ from: 'gemini-4-argon-medium', to: 'gemini-4-argon-medium' }),
      expect.objectContaining({ from: 'gemini-4-argon-low', to: 'gemini-4-argon-low' })
    ]))
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-3.1-flash-image'], [])
    expect(mapping).toEqual({
      'gemini-3.1-flash-image': 'gemini-3.1-flash-image'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })

  it('combined 模式会同时保留白名单身份映射和模型映射', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['gpt-5.4', 'claude-*'],
      [
        { from: 'gpt-latest', to: 'gpt-5.4' },
        { from: 'gpt-5.4', to: 'gpt-5.4-mini' }
      ]
    )

    expect(mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-mini',
      'gpt-latest': 'gpt-5.4'
    })
  })

  it('combined mode retains a mapping when a whitelist entry has the same source', () => {
    expect(buildModelMappingObject('combined', ['gpt-latest'], [{ from: 'gpt-latest', to: 'deepseek-chat' }])).toEqual({
      'gpt-latest': 'deepseek-chat'
    })
  })

  it('split mapping keeps only identity entries in the whitelist after reopening', () => {
    expect(splitModelMappingObject({ 'gpt-latest': 'deepseek-chat', 'gpt-5.4': 'gpt-5.4' })).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }]
    })
  })

  it('splitModelMappingObject 会把身份映射还原成白名单，其余保留为映射', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.4': 'gpt-5.4',
      'gpt-latest': 'gpt-5.4',
      ' ': 'gpt-empty',
      broken: 123
    })

    expect(parsed).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'gpt-5.4' }]
    })
  })
})
