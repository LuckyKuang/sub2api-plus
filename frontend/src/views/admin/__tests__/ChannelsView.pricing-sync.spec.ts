import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import ChannelsView from '@/views/admin/ChannelsView.vue'
import PricingEntryCard from '@/components/admin/channel/PricingEntryCard.vue'
import type {
  Channel,
  ModelPricingReference,
  ReferencePricingCard,
  SyncPricingModelsResult,
} from '@/api/admin/channels'

// 「同步最新模型」/手动添加回归：同步回来的模型必须每个都带着自己的官方价进入
// 独立计价条目，已有规则一律不改写，manual/缺价/刷新失败必须可见可区分。

const {
  channelsList,
  channelsSync,
  groupsGetAll,
  modelDefaultPricing,
  showSuccess,
  showError,
  showWarning,
} = vi.hoisted(() => ({
  channelsList: vi.fn(),
  channelsSync: vi.fn(),
  groupsGetAll: vi.fn(),
  modelDefaultPricing: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: {
      list: (...args: unknown[]) => channelsList(...args),
      syncPricingModels: (...args: unknown[]) => channelsSync(...args),
      create: vi.fn(),
      update: vi.fn(),
      remove: vi.fn(),
    },
    groups: { getAll: (...args: unknown[]) => groupsGetAll(...args) },
    accounts: { getById: vi.fn() },
  },
}))

vi.mock('@/api/admin/channels', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/channels')>('@/api/admin/channels')
  return {
    ...actual,
    default: {
      ...actual,
      getModelDefaultPricing: (...args: unknown[]) => modelDefaultPricing(...args),
      syncPricingModels: (...args: unknown[]) => channelsSync(...args),
    },
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showSuccess,
    showError,
    showWarning,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const SlotStub = defineComponent({
  template: '<div><slot /><slot name="filters" /><slot name="actions" /><slot name="table" /><slot name="pagination" /></div>',
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /></div>',
})

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: `<div>
    <template v-for="(row, i) in data" :key="i"><slot name="cell-actions" :row="row" :index="i" /></template>
    <slot name="empty" />
  </div>`,
})

const card = (over: Partial<ReferencePricingCard> = {}): ReferencePricingCard => ({
  platform: 'openai',
  models: [],
  billing_mode: 'token',
  input_price: null,
  output_price: null,
  cache_write_price: null,
  cache_write_1h_price: null,
  cache_read_price: null,
  fast_multiplier: null,
  flex_multiplier: null,
  reasoning_effort_multipliers: null,
  image_input_price: null,
  image_output_price: null,
  per_request_price: null,
  intervals: [],
  ...over,
})

const priced = (model: string, over: Partial<ModelPricingReference> = {}): ModelPricingReference => ({
  model,
  matched_model: model,
  platform: 'openai',
  status: 'priced',
  source: 'release_catalog',
  reason_code: '',
  pricing: card({ models: [model], ...over }),
  ...over,
})

const snapshot = (
  models: ModelPricingReference[],
  over: Partial<SyncPricingModelsResult> = {},
): SyncPricingModelsResult => ({
  platform: 'openai',
  refresh_status: 'refreshed',
  catalog_version: 'abc123',
  warning_code: '',
  models,
  ...over,
})

const solCard = priced('gpt-6-sol', {
  input_price: 2e-6,
  output_price: 10e-6,
  cache_write_price: 2.5e-6,
  cache_read_price: 0.2e-6,
  fast_multiplier: 2,
  flex_multiplier: 0.5,
})

const lunaCard = priced('gpt-6-luna', {
  input_price: 0.1e-6,
  output_price: 0.5e-6,
  cache_write_price: 0.125e-6,
  cache_read_price: 0.01e-6,
  fast_multiplier: 2,
})

const sonnet5Card: ModelPricingReference = {
  model: 'claude-sonnet-5',
  matched_model: 'claude-sonnet-5',
  platform: 'anthropic',
  status: 'priced',
  source: 'release_catalog',
  reason_code: '',
  pricing: card({
    platform: 'anthropic',
    models: ['claude-sonnet-5'],
    input_price: 2e-6,
    output_price: 10e-6,
    cache_write_price: 2.5e-6,
    cache_write_1h_price: 4e-6,
    cache_read_price: 0.2e-6,
  }),
}

function expectSonnet5Prices(wrapper: VueWrapper) {
  expect(wrapper.props('entry').models).toEqual(['claude-sonnet-5'])
  for (const [label, value] of [
    ['inputPrice', '2'],
    ['outputPrice', '10'],
    ['cacheWrite5mPrice', '2.5'],
    ['cacheWrite1hPrice', '4'],
    ['cacheReadPrice', '0.2'],
  ]) {
    expect(priceInput(wrapper, `admin.channels.form.${label}`).value).toBe(value)
  }
}

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: SlotStub,
        TablePageLayout: SlotStub,
        DataTable: DataTableStub,
        Pagination: SlotStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: SlotStub,
        EmptyState: SlotStub,
        Select: SlotStub,
        Icon: SlotStub,
        PlatformIcon: SlotStub,
        Toggle: SlotStub,
      },
    },
  })
}

function priceInput(cardWrapper: VueWrapper, labelKey: string): HTMLInputElement {
  const label = cardWrapper.findAll('label').find(l => l.text() === labelKey)
  if (!label) throw new Error(`label ${labelKey} not found`)
  return label.element.parentElement!.querySelector('input')!
}

async function openCreateDialog(wrapper: VueWrapper) {
  const createButton = wrapper.findAll('button').find(b => b.text().includes('admin.channels.createChannel'))
  await createButton!.trigger('click')
  await flushPromises()
}

async function enableOpenAI(wrapper: VueWrapper) {
  await enablePlatform(wrapper, 'openai')
}

async function enableAnthropic(wrapper: VueWrapper) {
  await enablePlatform(wrapper, 'anthropic')
}

async function enablePlatform(wrapper: VueWrapper, platform: string) {
  // The basic form's first checkbox is restrict_models, before the platform
  // controls. Select the platform by its label rather than its position.
  const labelKey = `admin.groups.platforms.${platform}`
  const platformLabel = wrapper.findAll('label').find(label => label.text() === labelKey)
  expect(platformLabel, `${platform} platform checkbox must be present`).toBeDefined()
  await platformLabel!.get('input[type="checkbox"]').setValue(true)
  await flushPromises()
  const platformTab = wrapper.findAll('button.channel-tab').find(button => button.text() === labelKey)
  expect(platformTab, `enabled ${platform} pricing tab must be present`).toBeDefined()
  await platformTab!.trigger('click')
  await flushPromises()
}

function syncButton(wrapper: VueWrapper) {
  return wrapper.findAll('button').find(b => b.text().includes('admin.channels.form.syncLatestModels'))!
}

/** 计价规则的新建按钮与同步按钮在同一工具栏，避免误点映射/账号统计的新建。 */
function addPricingRuleButton(wrapper: VueWrapper) {
  const sync = syncButton(wrapper)
  const toolbar = sync.element.parentElement!
  const add = Array.from(toolbar.querySelectorAll('button'))
    .find(b => b.textContent?.includes('common.add'))!
  return wrapper.findAll('button').find(b => b.element === add)!
}

function cardWrappers(wrapper: VueWrapper) {
  return wrapper.findAllComponents(PricingEntryCard)
}

beforeEach(() => {
  vi.clearAllMocks()
  channelsList.mockResolvedValue({ items: [], total: 0 })
  groupsGetAll.mockResolvedValue([])
})

describe('ChannelsView pricing sync', () => {
  it.each(['add', 'sync'] as const)('fills exact Sonnet 5 reference prices via %s without borrowing Sonnet 5.5', async (path) => {
    // Deliberately different synthetic reference prices expose any accidental
    // reuse across model rows; these are not assertions of Sonnet 5.5's tariff.
    const sonnet55: ModelPricingReference = {
      ...sonnet5Card,
      model: 'claude-sonnet-5-5',
      matched_model: 'claude-sonnet-5-5',
      pricing: card({ platform: 'anthropic', models: ['claude-sonnet-5-5'], input_price: 3e-6, output_price: 15e-6 }),
    }
    channelsSync.mockResolvedValue(snapshot([sonnet5Card, sonnet55], { platform: 'anthropic' }))
    modelDefaultPricing.mockResolvedValue(sonnet5Card)
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableAnthropic(wrapper)

    if (path === 'sync') {
      await syncButton(wrapper).trigger('click')
      await flushPromises()
      expect(channelsSync).toHaveBeenCalledWith('anthropic')
      const cards = cardWrappers(wrapper)
      expect(cards).toHaveLength(2)
      expectSonnet5Prices(cards.find(c => c.props('entry').models[0] === 'claude-sonnet-5')!)
      const newer = cards.find(c => c.props('entry').models[0] === 'claude-sonnet-5-5')!
      expect(priceInput(newer, 'admin.channels.form.inputPrice').value).toBe('3')
      expect(priceInput(newer, 'admin.channels.form.outputPrice').value).toBe('15')
    } else {
      await addPricingRuleButton(wrapper).trigger('click')
      await flushPromises()
      await cardWrappers(wrapper)[0]!.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['claude-sonnet-5'])
      await flushPromises()
      expect(modelDefaultPricing).toHaveBeenCalledWith('anthropic', 'claude-sonnet-5')
      expectSonnet5Prices(cardWrappers(wrapper)[0]!)
    }
    expect(cardWrappers(wrapper).some(c => c.props('entry').models.includes('claude-connect-5'))).toBe(false)
  })

  it('keeps manual Sonnet 5 overrides including explicit zero when syncing the exact model', async () => {
    channelsSync.mockResolvedValue(snapshot([sonnet5Card], { platform: 'anthropic' }))
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableAnthropic(wrapper)
    await addPricingRuleButton(wrapper).trigger('click')
    await flushPromises()
    const manual = cardWrappers(wrapper)[0]!
    manual.vm.$emit('update', { ...manual.props('entry'), input_price: '0', output_price: '7', cache_write_1h_price: '9' })
    await flushPromises()
    await cardWrappers(wrapper)[0]!.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['claude-sonnet-5'])
    await flushPromises()
    expect(modelDefaultPricing).not.toHaveBeenCalled()

    await syncButton(wrapper).trigger('click')
    await flushPromises()
    expect(channelsSync).toHaveBeenCalledWith('anthropic')
    expect(cardWrappers(wrapper)).toHaveLength(1)
    expect(priceInput(cardWrappers(wrapper)[0]!, 'admin.channels.form.inputPrice').value).toBe('0')
    expect(priceInput(cardWrappers(wrapper)[0]!, 'admin.channels.form.outputPrice').value).toBe('7')
    expect(priceInput(cardWrappers(wrapper)[0]!, 'admin.channels.form.cacheWrite1hPrice').value).toBe('9')
    expect(showSuccess).toHaveBeenLastCalledWith('admin.channels.form.syncModelsAlreadyUpToDate')
  })

  it.each(['add', 'sync'] as const)('resolves the exact Jev card on the typesafe platform via %s', async (path) => {
    const jev: ModelPricingReference = {
      model: 'jev-latest',
      matched_model: 'jev-latest',
      platform: 'typesafe',
      status: 'priced',
      source: 'builtin_fallback',
      reason_code: '',
      // USD per token: $0.042/MTok input and an explicit free output.
      pricing: card({ platform: 'typesafe', models: ['jev-latest'], input_price: 0.042e-6, output_price: 0 }),
    }
    channelsSync.mockResolvedValue(snapshot([jev], { platform: 'typesafe' }))
    modelDefaultPricing.mockResolvedValue(jev)
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enablePlatform(wrapper, 'typesafe')

    if (path === 'sync') {
      await syncButton(wrapper).trigger('click')
      await flushPromises()
      expect(channelsSync).toHaveBeenCalledWith('typesafe')
    } else {
      await addPricingRuleButton(wrapper).trigger('click')
      await flushPromises()
      await cardWrappers(wrapper)[0]!.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['jev-latest'])
      await flushPromises()
      expect(modelDefaultPricing).toHaveBeenCalledWith('typesafe', 'jev-latest')
    }

    const cards = cardWrappers(wrapper)
    expect(cards).toHaveLength(1)
    expect(cards[0]!.props('entry').models).toEqual(['jev-latest'])
    expect(priceInput(cards[0]!, 'admin.channels.form.inputPrice').value).toBe('0.042')
    // Explicit zero output is a price, not a missing field.
    expect(priceInput(cards[0]!, 'admin.channels.form.outputPrice').value).toBe('0')
  })

  it('keeps an explicit zero Jev operator price when syncing the exact model', async () => {
    const jev: ModelPricingReference = {
      model: 'jev-latest',
      matched_model: 'jev-latest',
      platform: 'typesafe',
      status: 'priced',
      source: 'builtin_fallback',
      reason_code: '',
      pricing: card({ platform: 'typesafe', models: ['jev-latest'], input_price: 0.042e-6, output_price: 0 }),
    }
    channelsSync.mockResolvedValue(snapshot([jev], { platform: 'typesafe' }))
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enablePlatform(wrapper, 'typesafe')
    await addPricingRuleButton(wrapper).trigger('click')
    await flushPromises()
    const manual = cardWrappers(wrapper)[0]!
    manual.vm.$emit('update', { ...manual.props('entry'), input_price: '0', output_price: '0' })
    await flushPromises()
    await cardWrappers(wrapper)[0]!.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['jev-latest'])
    await flushPromises()
    expect(modelDefaultPricing).not.toHaveBeenCalled()

    await syncButton(wrapper).trigger('click')
    await flushPromises()
    expect(channelsSync).toHaveBeenCalledWith('typesafe')
    expect(cardWrappers(wrapper)).toHaveLength(1)
    expect(priceInput(cardWrappers(wrapper)[0]!, 'admin.channels.form.inputPrice').value).toBe('0')
    expect(priceInput(cardWrappers(wrapper)[0]!, 'admin.channels.form.outputPrice').value).toBe('0')
  })

  it('creates one independently priced rule per synced model', async () => {
    channelsSync.mockResolvedValue(snapshot([solCard, lunaCard]))
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableOpenAI(wrapper)

    expect(cardWrappers(wrapper)).toHaveLength(0)

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    const cards = cardWrappers(wrapper)
    expect(cards).toHaveLength(2)
    // 每个模型一条规则，各自只含自己的型号。
    expect(channelsSync).toHaveBeenCalledWith('openai')
    expect(cards[0]!.props('entry').models).toEqual(['gpt-6-sol'])
    expect(cards[1]!.props('entry').models).toEqual(['gpt-6-luna'])

    // Sol $2/$10，Luna $0.10/$0.50（USD/MTok）：不得共享第一个型号的价。
    expect(priceInput(cards[0]!, 'admin.channels.form.inputPrice').value).toBe('2')
    expect(priceInput(cards[0]!, 'admin.channels.form.outputPrice').value).toBe('10')
    expect(priceInput(cards[0]!, 'admin.channels.form.cacheWrite5mPrice').value).toBe('2.5')
    expect(priceInput(cards[0]!, 'admin.channels.form.cacheReadPrice').value).toBe('0.2')
    expect(priceInput(cards[1]!, 'admin.channels.form.inputPrice').value).toBe('0.1')
    expect(priceInput(cards[1]!, 'admin.channels.form.outputPrice').value).toBe('0.5')

    expect(showSuccess).toHaveBeenCalledWith('admin.channels.form.syncModelsSuccess')
  })

  it('is idempotent: repeating sync adds no rules', async () => {
    channelsSync.mockResolvedValue(snapshot([solCard, lunaCard]))
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableOpenAI(wrapper)

    await syncButton(wrapper).trigger('click')
    await flushPromises()
    expect(cardWrappers(wrapper)).toHaveLength(2)

    await syncButton(wrapper).trigger('click')
    await flushPromises()
    expect(cardWrappers(wrapper)).toHaveLength(2)
    expect(showSuccess).toHaveBeenLastCalledWith('admin.channels.form.syncModelsAlreadyUpToDate')
    expect(channelsSync.mock.calls).toEqual([['openai'], ['openai']])
  })

  it('keeps manual_required models visible in their own incomplete rule', async () => {
    const manual: ModelPricingReference = {
      model: 'omen-alpha',
      matched_model: 'omen-alpha',
      platform: 'opencode_go',
      status: 'manual_required',
      source: 'none',
      reason_code: 'exact_price_unavailable',
      pricing: null,
    }
    channelsSync.mockResolvedValue(
      snapshot([
        { ...solCard, platform: 'opencode_go', pricing: { ...solCard.pricing!, platform: 'opencode_go' } },
        manual,
      ], { platform: 'opencode_go' }),
    )
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enablePlatform(wrapper, 'opencode_go')

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    const cards = cardWrappers(wrapper)
    expect(cards).toHaveLength(2)
    // manual 型号是独立未完成规则：没有价格、状态可见。
    const manualCard = cards.find(c => c.props('entry').models[0] === 'omen-alpha')!
    expect(manualCard).toBeTruthy()
    expect(priceInput(manualCard, 'admin.channels.form.inputPrice').value).toBe('')
    const status = manualCard.find('[data-testid="pricing-lookup-status"]')
    expect(status.exists()).toBe(true)
    expect(status.text()).toContain('admin.channels.form.pricingLookupManualRequired')
    // 部分完成要单独报数，不能宣称全部自动填价。
    expect(channelsSync).toHaveBeenCalledWith('opencode_go')
    expect(cards.every(c => c.props('platform') === 'opencode_go')).toBe(true)
    expect(showSuccess).toHaveBeenCalledWith('admin.channels.form.syncModelsPartial')
  })

  it('still adds models from a stale snapshot but warns about the catalog', async () => {
    channelsSync.mockResolvedValue(
      snapshot([solCard], { refresh_status: 'stale', warning_code: 'CATALOG_REFRESH_FAILED' }),
    )
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableOpenAI(wrapper)

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(cardWrappers(wrapper)).toHaveLength(1)
    expect(showWarning).toHaveBeenCalledWith('admin.channels.form.syncModelsStaleCatalog')
    expect(channelsSync).toHaveBeenCalledWith('openai')
  })

  it('reports a failed sync without creating rules', async () => {
    channelsSync.mockRejectedValue(new Error('boom'))
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableOpenAI(wrapper)

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    expect(cardWrappers(wrapper)).toHaveLength(0)
    // 失败必须显式报错，不能静默吞掉；消息经 extractApiErrorMessage 提取。
    expect(showError).toHaveBeenCalled()
    expect(cardWrappers(wrapper)).toHaveLength(0)
    expect(channelsSync).toHaveBeenCalledWith('openai')
  })

  it('editing an existing channel never rewrites existing rules', async () => {
    const existing: Channel = {
      id: 7,
      name: 'existing',
      description: '',
      status: 'active',
      billing_model_source: 'channel_mapped',
      restrict_models: false,
      group_ids: [1],
      model_pricing: [
        {
          platform: 'openai',
          models: ['gpt-5.4'],
          billing_mode: 'token',
          input_price: 2.5e-6,
          output_price: 15e-6,
          cache_write_price: 3.125e-6,
          cache_write_1h_price: null,
          cache_read_price: 0.25e-6,
          fast_multiplier: null,
          flex_multiplier: null,
          reasoning_effort_multipliers: null,
          image_input_price: null,
          image_output_price: null,
          per_request_price: null,
          intervals: [],
          time_pricing: null,
        },
      ],
      model_mapping: {},
      apply_pricing_to_account_stats: false,
      account_stats_pricing_rules: [],
      created_at: '2026-09-01T00:00:00Z',
      updated_at: '2026-09-01T00:00:00Z',
    }
    channelsList.mockResolvedValue({ items: [existing], total: 1 })
    channelsSync.mockResolvedValue(snapshot([solCard, lunaCard]))

    const wrapper = mountView()
    await flushPromises()

    // 通过 DataTable #actions 槽打开编辑对话框。
    const editButton = wrapper.findAll('button').find(b => b.text().includes('common.edit'))!
    expect(editButton, 'row edit button must be rendered through the DataTable cell-actions slot').toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()

    const before = cardWrappers(wrapper)
    expect(before).toHaveLength(1)
    expect(before[0]!.props('entry').models).toEqual(['gpt-5.4'])
    const inputBefore = priceInput(before[0]!, 'admin.channels.form.inputPrice').value
    expect(inputBefore).toBe('2.5')

    await syncButton(wrapper).trigger('click')
    await flushPromises()

    const after = cardWrappers(wrapper)
    // 旧规则原位不动，新模型只追加新规则。
    expect(after[0]!.props('entry').models).toEqual(['gpt-5.4'])
    expect(priceInput(after[0]!, 'admin.channels.form.inputPrice').value).toBe('2.5')
    expect(priceInput(after[0]!, 'admin.channels.form.outputPrice').value).toBe('15')
    expect(after.map(c => c.props('entry').models[0])).toEqual(['gpt-5.4', 'gpt-6-sol', 'gpt-6-luna'])
    expect(channelsSync).toHaveBeenCalledWith('openai')
  })

  it('splits pasted models into independent rules', async () => {
    modelDefaultPricing.mockImplementation((_platform: string, model: string) =>
      Promise.resolve(
        model === 'gpt-6-sol'
          ? solCard
          : model === 'gpt-6-luna'
            ? lunaCard
            : {
                model,
                matched_model: model,
                platform: 'openai',
                status: 'manual_required' as const,
                source: 'none' as const,
                reason_code: 'exact_price_unavailable' as const,
                pricing: null,
              },
      ),
    )
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableOpenAI(wrapper)

    // 手动新增一条空白规则，再一次性粘贴两个型号。
    const addButton = addPricingRuleButton(wrapper)
    await addButton.trigger('click')
    await flushPromises()
    expect(cardWrappers(wrapper)).toHaveLength(1)

    const modelInput = cardWrappers(wrapper)[0]!.findComponent({ name: 'ModelTagInput' })
    await modelInput.vm.$emit('update:models', ['gpt-6-sol', 'gpt-6-luna'])
    await flushPromises()

    const cards = cardWrappers(wrapper)
    // 一次粘贴不得共享第一个型号的价格：必须拆成两条独立规则。
    expect(cards).toHaveLength(2)
    expect(cards[0]!.props('entry').models).toEqual(['gpt-6-sol'])
    expect(priceInput(cards[0]!, 'admin.channels.form.inputPrice').value).toBe('2')
    expect(cards[1]!.props('entry').models).toEqual(['gpt-6-luna'])
    expect(priceInput(cards[1]!, 'admin.channels.form.inputPrice').value).toBe('0.1')
    expect(modelDefaultPricing).toHaveBeenCalledWith('openai', 'gpt-6-sol')
    expect(modelDefaultPricing).toHaveBeenCalledWith('openai', 'gpt-6-luna')
  })

  it('shows the reference price source for a manually added model', async () => {
    modelDefaultPricing.mockResolvedValue(solCard)
    const wrapper = mountView()
    await flushPromises()
    await openCreateDialog(wrapper)
    await enableOpenAI(wrapper)

    const addButton = addPricingRuleButton(wrapper)
    await addButton.trigger('click')
    await flushPromises()

    const modelInput = cardWrappers(wrapper)[0]!.findComponent({ name: 'ModelTagInput' })
    await modelInput.vm.$emit('update:models', ['gpt-6-sol'])
    await flushPromises()

    const single = cardWrappers(wrapper)[0]!
    expect(priceInput(single, 'admin.channels.form.inputPrice').value).toBe('2')
    // 来源必须可见：受信任目录 vs 内置兜底 vs 代理参考价不能混为一谈。
    const source = single.find('[data-testid="pricing-lookup-source"]')
    expect(source.exists()).toBe(true)
    expect(source.text()).toContain('admin.channels.form.pricingLookupSourceCatalog')
    expect(modelDefaultPricing).toHaveBeenCalledWith('openai', 'gpt-6-sol')
  })
})
