import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import {
  BUILTIN_PLATFORM_CATALOG,
  resetPlatformCatalog,
  setPlatformCatalog
} from '@/constants/platformCatalog'

const {
  copyToClipboard,
  showError,
  showSuccess,
  showInfo,
  showWarning,
  syncUpstreamModels,
  syncUpstreamModelsPreview
} = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  showWarning: vi.fn(),
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => key === 'common.copy' ? '复制' : key === 'admin.accounts.modelMappingConflict' ? `Model mapping conflict: ${params?.from} → ${params?.to}` : key
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo,
    showWarning
  })
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels,
    syncUpstreamModelsPreview
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import { cnOAuthModels } from '@/api/admin/cnOAuth'
vi.mock('@/api/admin/cnOAuth', () => ({ cnOAuthModels: vi.fn() }))

function mountSelector(props: Record<string, unknown> = {}) {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai',
      ...props,
    },
    global: {
      stubs: {
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    copyToClipboard.mockClear()
    showError.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    syncUpstreamModels.mockReset()
    syncUpstreamModelsPreview.mockReset()
    vi.mocked(cnOAuthModels).mockReset()
  })

  it('offers official Step-Code candidates and the fill action before credentials are entered', async () => {
    const wrapper = mountSelector({ platform: 'stepfun' })
    const fill = wrapper.findAll('button').find(b => b.text() === 'admin.accounts.fillRelatedModels')
    expect(fill, 'the existing fill action must remain visible without API Key or OAuth').toBeDefined()
    await wrapper.get('div.cursor-pointer').trigger('click')
    const candidates = ['step-5-preview', 'step-3.7-flash', 'step-3.5-flash-2603', 'step-3.5-flash', 'step-router-v1']
    expect(wrapper.findAll('[data-testid="select-model"]').map(button => button.text())).toEqual(candidates)
    await fill!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([candidates])
    expect(syncUpstreamModelsPreview).not.toHaveBeenCalled()
    expect(cnOAuthModels).not.toHaveBeenCalled()
  })

  it.each(['apikey', 'oauth'] as const)('lets StepFun %s users search, deselect and reselect discovered models', async kind => {
    const catalog = { models: [' step-3.7-flash ', 'step-future-chat', 'step-3.7-flash'] }
    syncUpstreamModelsPreview.mockResolvedValue(catalog)
    vi.mocked(cnOAuthModels).mockResolvedValue(catalog)
    const wrapper = mountSelector({ platform: 'stepfun', ...(kind === 'apikey'
      ? { syncCredentials: { platform: 'stepfun', type: 'apikey', base_url: 'https://api.stepfun.ai/v1', api_key: 'step-key' } }
      : { oauthSessionId: 'ready-session' }) })
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(findModelRow(wrapper, 'step-5-preview').exists()).toBe(true)
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([['step-3.7-flash', 'step-future-chat']])
    expect(wrapper.findAll('[data-testid="select-model"]').map(button => button.text())).toEqual([
      'step-5-preview', 'step-3.7-flash', 'step-3.5-flash-2603', 'step-3.5-flash', 'step-router-v1', 'step-future-chat'
    ])
    await wrapper.setProps({ modelValue: ['step-3.7-flash', 'step-future-chat'] })
    await findModelRow(wrapper, 'step-future-chat').get('[data-testid="select-model"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['step-3.7-flash']])
    await wrapper.setProps({ modelValue: ['step-3.7-flash'] })
    await wrapper.get('input[placeholder="admin.accounts.searchModels"]').setValue('future')
    expect(wrapper.findAll('[data-testid="model-option"]')).toHaveLength(1)
    await findModelRow(wrapper, 'step-future-chat').get('[data-testid="select-model"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['step-3.7-flash', 'step-future-chat']])
    await wrapper.setProps({ modelValue: [] })
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.fillRelatedModels')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[
      'step-3.7-flash', 'step-future-chat', 'step-5-preview', 'step-3.5-flash-2603', 'step-3.5-flash', 'step-router-v1'
    ]])
    if (kind === 'oauth') {
      expect(cnOAuthModels).toHaveBeenCalledWith('ready-session')
      expect(syncUpstreamModelsPreview).not.toHaveBeenCalled()
      expect(wrapper.emitted('upstream-synced')).toBeUndefined()
    }
    wrapper.unmount()
  })

  it('preserves saved StepFun whitelist IDs alongside the official candidates when editing', async () => {
    const wrapper = mountSelector({ platform: 'stepfun', accountId: 42, modelValue: ['step-private-model'] })
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.findAll('[data-testid="select-model"]').map(button => button.text())).toEqual([
      'step-5-preview', 'step-3.7-flash', 'step-3.5-flash-2603', 'step-3.5-flash', 'step-router-v1', 'step-private-model'
    ])
    expect(findModelRow(wrapper, 'step-private-model').exists()).toBe(true)
  })

  it('discards a late OAuth catalog after cancellation and keeps the current selection', async () => {
    let resolve!: (value: { models: string[] }) => void
    vi.mocked(cnOAuthModels).mockImplementation(() => new Promise(done => { resolve = done }))
    const wrapper = mountSelector({ platform: 'stepfun', oauthSessionId: 'old-session', modelValue: ['step-selected'] })
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await wrapper.setProps({ oauthSessionId: undefined })
    resolve({ models: ['step-old-account'] })
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.text()).not.toContain('step-old-account')
    expect(findModelRow(wrapper, 'step-selected').exists()).toBe(true)
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('drops discovered options when API key credentials change', async () => {
    const credentials = { platform: 'stepfun', type: 'apikey', api_key: 'first-key' }
    syncUpstreamModelsPreview.mockResolvedValue({ models: ['step-first-account'] })
    const wrapper = mountSelector({ platform: 'stepfun', syncCredentials: credentials })
    await wrapper.findAll('button').find(b => b.text() === 'admin.accounts.syncUpstreamModels')!.trigger('click')
    await flushPromises()
    await wrapper.get('div.cursor-pointer').trigger('click')
    expect(findModelRow(wrapper, 'step-first-account').exists()).toBe(true)
    await wrapper.setProps({ syncCredentials: { ...credentials, api_key: 'second-key' } })
    expect(wrapper.text()).not.toContain('step-first-account')
    expect(findModelRow(wrapper, 'step-5-preview').exists()).toBe(true)
  })

  afterEach(() => {
    resetPlatformCatalog()
  })

  it.each(['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'command_code', 'cline'])(
    'supports upstream sync for %s saved accounts and creation previews',
    (platform) => {
      const wrappers = [
        mountSelector({ platform, accountId: 46 }),
        mountSelector({ platform, syncCredentials: { platform, type: 'apikey', api_key: 'test-key' } })
      ]
      for (const wrapper of wrappers) {
        expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(true)
        wrapper.unmount()
      }
    }
  )

  it.each(['typesafe', 'unregistered'])(
    'hides upstream sync for unsupported %s saved accounts and creation previews',
    (platform) => {
      const wrappers = [
        mountSelector({ platform, accountId: 46 }),
        mountSelector({ platform, syncCredentials: { platform, type: 'apikey', api_key: 'test-key' } })
      ]
      for (const wrapper of wrappers) {
        expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(false)
        wrapper.unmount()
      }
      expect(syncUpstreamModels).not.toHaveBeenCalled()
      expect(syncUpstreamModelsPreview).not.toHaveBeenCalled()
    }
  )

  it('requires a supported request builder for newly registered platforms', async () => {
    const wrapper = mountSelector({ platform: 'acme_router', accountId: 46 })
    const platforms = [
      ...BUILTIN_PLATFORM_CATALOG.platforms,
      { id: 'acme_router', display_name: 'Acme Router', gateway: 'openai' as const, cn_provider: false }
    ]
    setPlatformCatalog({ ...BUILTIN_PLATFORM_CATALOG, platforms })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(false)

    setPlatformCatalog({
      ...BUILTIN_PLATFORM_CATALOG,
      platforms: platforms.map(spec => spec.id === 'acme_router'
        ? { ...spec, multi_protocol: { default_mode: 'default', routing: 'by_inbound', modes: [] } }
        : spec)
    })
    await flushPromises()
    expect(wrapper.findAll('button').some(button => button.text() === 'admin.accounts.syncUpstreamModels')).toBe(true)
    wrapper.unmount()
  })

  it('rejects a custom whitelist model that is already mapped to a different target', async () => {
    const wrapper = mountSelector({ modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue(' gpt-latest ')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(showInfo).toHaveBeenCalledWith(expect.stringContaining('gpt-latest → deepseek-chat'))
  })

  it('keeps the existing duplicate identity warning before checking mappings', async () => {
    const wrapper = mountSelector({ modelValue: ['gpt-latest'], modelMappings: [{ from: 'gpt-latest', to: 'deepseek-chat' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('gpt-latest')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(showInfo).toHaveBeenCalledWith('admin.accounts.modelExists')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('allows matching identity mapping as a whitelist model', async () => {
    const wrapper = mountSelector({ modelMappings: [{ from: 'gpt-latest', to: 'gpt-latest' }] })
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('gpt-latest')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-latest']]])
  })

  it('still allows custom models without a mapping prop', async () => {
    const wrapper = mountSelector()
    await wrapper.get('input[placeholder="admin.accounts.enterCustomModelName"]').setValue('custom-model')
    await wrapper.findAll('button').find(button => button.text() === 'admin.accounts.addModel')!.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[['custom-model']]])
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')

    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('warns when model IDs sync but capability metadata is incomplete', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['x-preview-f-free'],
      warnings: [
        {
          code: 'upstream_model_metadata_incomplete',
          message: 'Model IDs were synced, but capability metadata could not be updated.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataIncomplete')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('shows success and a partial warning when some capabilities were saved', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['gpt-6-astra', 'gpt-image-2'],
      warnings: [
        {
          code: 'upstream_model_metadata_partial',
          message: 'Some model capabilities were saved; remaining models are still incomplete.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-6-astra', 'gpt-image-2']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataPartial')
  })

  it('reports a successful preview so account creation can persist metadata', async () => {
    syncUpstreamModelsPreview.mockResolvedValue({
      models: ['x-preview-f-free'],
      metadata: {
        'x-preview-f-free': {
          id: 'x-preview-f-free',
          reasoning: true,
          supported_reasoning_levels: ['low', 'high', 'max'],
        },
      },
    })
    const wrapper = mountSelector({
      syncCredentials: {
        platform: 'openai',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/v1',
        api_key: 'test-key',
      },
    })
    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    await syncButton?.trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsPreview).toHaveBeenCalledOnce()
    expect(wrapper.emitted('upstream-synced')).toEqual([[]])
    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
  })

  it('shows the upstream sync button for OpenCode Go create-account credentials', () => {
    const wrapper = mountSelector({
      platform: 'opencode_go',
      syncCredentials: {
        platform: 'opencode_go',
        type: 'apikey',
        base_url: 'https://opencode.ai/zen/go/v1',
        api_key: 'sk-test',
      },
    })
    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')

    expect(syncButton).toBeDefined()
    expect(syncButton?.exists()).toBe(true)
  })
})
