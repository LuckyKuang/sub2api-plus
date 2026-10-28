import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'
import Select from '@/components/common/Select.vue'
import PricingEntryCard from '@/components/admin/channel/PricingEntryCard.vue'
import GroupModelRateFields from '@/components/admin/group/GroupModelRateFields.vue'
import { adminAPI } from '@/api/admin'
import en from '@/i18n/locales/en/admin/overview'
import zh from '@/i18n/locales/zh/admin/overview'
import type { BillingRatePolicy } from '@/types/billingRate'

const {
  listGroups,
  duplicateGroup,
  createGroup,
  updateGroup,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  listAccounts,
  getGroupById,
  getAccountById,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  duplicateGroup: vi.fn(),
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  listAccounts: vi.fn(),
  getGroupById: vi.fn(),
  getAccountById: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

const authState = vi.hoisted(() => ({ isSimpleMode: false }))
const translationState = vi.hoisted(() => ({
  translate: (key: string, _params?: Record<string, unknown>): string => key
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getById: getGroupById,
      duplicate: duplicateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: vi.fn(),
      create: createGroup,
      update: updateGroup,
      delete: vi.fn(),
      updateSortOrder: vi.fn()
    },
    accounts: {
      list: listAccounts,
      getById: getAccountById
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => translationState.translate(key, params) })
  }
})

const sourceGroup: AdminGroup = {
  id: 42,
  name: 'Primary',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  messages_dispatch_model_config: undefined,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-16T00:00:00Z',
  updated_at: '2026-07-16T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  model_allowlist: undefined,
  sort_order: 10
}

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>'
})

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
})

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true
      }
    }
  })
}

describe('GroupsView duplicate action', () => {
  beforeEach(() => {
    authState.isSimpleMode = false
    translationState.translate = key => key
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listGroups,
      duplicateGroup,
      createGroup,
      updateGroup,
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      listAccounts,
      getGroupById,
      getAccountById,
      showSuccess,
      showError
    ]) {
      fn.mockReset()
    }

    listGroups.mockResolvedValue({
      items: [sourceGroup],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getGroupById.mockImplementation(async () => (
      await listGroups.mock.results[listGroups.mock.results.length - 1].value
    ).items[0])
    duplicateGroup.mockResolvedValue({
      ...sourceGroup,
      id: 43,
      name: 'Primary (Copy)',
      status: 'inactive'
    })
    getModelAllowlistCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
    listAccounts.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100, pages: 0 })
    createGroup.mockResolvedValue(sourceGroup)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it.each([
    { locale: 'en', messages: en, reference: ['Base per-image reference', 'Base per-second reference', 'Base per-call reference: $2'], hint: 'platform, model, personal and applicable peak factors' },
    { locale: 'zh', messages: zh, reference: ['基础单张参考价', '基础每秒参考价', '基础单次参考价：$2'], hint: '平台、模型、个人及适用的高峰倍率' }
  ])('distinguishes base media/search references from final model charges in $locale for create and edit', async ({ messages, reference, hint }) => {
    type PreviewForm = {
      platform: string
      subscription_type: string
      rate_multiplier: number
      billing_rate_policy: BillingRatePolicy
      image_price_1k: number
      video_price_480p: number
      web_search_price_per_call: number
      image_rate_independent: boolean
      image_rate_multiplier: number
      video_rate_independent: boolean
      video_rate_multiplier: number
      peak_rate_enabled: boolean
      peak_rate_multiplier: number
    }
    const w = mountView()
    await flushPromises()
    for (const mode of ['create', 'edit'] as const) {
      await w.findAll('button').find(button => button.text() === (mode === 'create' ? 'admin.groups.createGroup' : 'common.edit'))!.trigger('click')
      await flushPromises()
      const vm = w.vm as unknown as { createForm: PreviewForm; editForm: PreviewForm }
      const state = mode === 'create' ? vm.createForm : vm.editForm
      state.platform = 'grok'
      state.subscription_type = 'subscription'
      await flushPromises()
      Object.assign(state, {
        rate_multiplier: 1, image_price_1k: 2, video_price_480p: 2, web_search_price_per_call: 2,
        image_rate_independent: false, image_rate_multiplier: 0.5,
        video_rate_independent: false, video_rate_multiplier: 0.5,
        peak_rate_enabled: true, peak_rate_multiplier: 1.5,
        billing_rate_policy: { platform_multipliers: { grok: 2, openai: 2 }, model_multipliers: [
          { platform: 'grok', model: 'model001', multiplier: 3 },
          { platform: 'openai', model: 'model001', multiplier: 3 }
        ] }
      })
      translationState.translate = (key, params) => {
        const value = key.replace(/^admin\./, '').split('.').reduce<unknown>((current, part) =>
          current && typeof current === 'object' ? (current as Record<string, unknown>)[part] : undefined, messages)
        return typeof value === 'string' ? value.replace(/\{(\w+)\}/g, (_, name: string) => String(params?.[name] ?? `{${name}}`)) : key
      }
      await flushPromises()
      const form = () => w.get(`#${mode}-group-form`)
      // $2 × platform 2 × model 3 × peak 1.5 is $18. This generic
      // size preview lacks a model, so $2 may only be a base reference.
      expect(form().text()).toContain('1K: $2')
      expect(form().text()).toContain('480p: $2')
      expect(form().text()).toContain(reference[0])
      expect(form().text()).toContain(reference[1])
      expect(form().text()).toContain(hint)
      state.image_rate_independent = true
      state.video_rate_independent = true
      await flushPromises()
      expect(form().text()).toContain('1K: $1')
      expect(form().text()).toContain('480p: $1')
      state.image_rate_multiplier = 0
      state.video_rate_multiplier = 0
      await flushPromises()
      expect(form().text()).toContain('1K: $0')
      expect(form().text()).toContain('480p: $0')
      state.platform = 'openai'
      await flushPromises()
      expect(form().text()).toContain(reference[2])
      expect(form().text()).toContain(hint)
      // Restore key labels before locating the next action.
      translationState.translate = key => key
      w.vm.$forceUpdate()
      await flushPromises()
    }
    w.unmount()
  })

  it('saves different exact model factors from one price card together with the same CAS group update', async () => {
    getGroupById.mockResolvedValue({ ...sourceGroup, billing_policy_version: 7,
      billing_rate_policy: { platform_multipliers: { openai: 1.2 }, model_multipliers: [
        { platform: 'openai', model: 'b', multiplier: 1.5 },
        { platform: 'grok', model: 'a', multiplier: 7 }
      ] },
      model_pricing: [{ platform: 'openai', models: ['a', 'b'], billing_mode: 'token', input_price: 0.000001, output_price: 0.000002, intervals: [] }]
    })
    const w = mountView()
    await flushPromises()
    await w.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const fields = w.getComponent(GroupModelRateFields).findAll('input[type="number"]')
    expect(fields.map(field => (field.element as HTMLInputElement).value)).toEqual(['', '1.5'])
    await fields[0]!.setValue('2')
    await fields[1]!.setValue('0')
    await w.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(sourceGroup.id, expect.objectContaining({ expected_billing_policy_version: 7,
      billing_rate_policy: { platform_multipliers: { openai: 1.2 }, model_multipliers: [
        { platform: 'grok', model: 'a', multiplier: 7 },
        { platform: 'openai', model: 'a', multiplier: 2 },
        { platform: 'openai', model: 'b', multiplier: 0 }
      ] },
      model_pricing: [expect.objectContaining({ platform: 'openai', models: ['a', 'b'], input_price: 0.000001, output_price: 0.000002 })]
    }))
    w.unmount()
  })

  it.each(['set', 'clear', 'default'] as const)('saves a newly added price-free model card without inventing a price override: %s', async mode => {
    // A selling factor may be configured without a custom unit price or model
    // mapping. Blank/cleared M means the independent default of 1, represented
    // by no stored model rule; it must not create an empty/free price override.
    getGroupById.mockResolvedValue({ ...sourceGroup, billing_policy_version: 11,
      billing_rate_policy: {
        platform_multipliers: { openai: 1.2, grok: 2 },
        model_multipliers: [{ platform: 'grok', model: 'public-model', multiplier: 7 }]
      },
      model_pricing: [], model_routing: null, model_routing_enabled: false,
      default_mapped_model: '', messages_dispatch_model_config: undefined
    })
    const w = mountView()
    await flushPromises()
    await w.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const form = w.get('#edit-group-form')
    expect(w.findComponent(PricingEntryCard).exists()).toBe(false)
    await form.findAll('button').find(button => button.text() === 'admin.groups.modelPricing.add')!.trigger('click')
    const card = w.getComponent(PricingEntryCard)
    card.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['public-model'])
    await flushPromises()
    const field = w.getComponent(GroupModelRateFields).get('input[type="number"]')
    expect(field.element).toHaveProperty('value', '')
    expect(field.attributes('placeholder')).toBe('1')
    if (mode !== 'default') await field.setValue('1.5')
    if (mode === 'clear') await field.setValue('')
    await form.trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledExactlyOnceWith(sourceGroup.id, expect.objectContaining({
      expected_billing_policy_version: 11,
      model_pricing: [],
      billing_rate_policy: {
        platform_multipliers: { openai: 1.2, grok: 2 },
        model_multipliers: mode === 'set'
          ? [{ platform: 'grok', model: 'public-model', multiplier: 7 }, { platform: 'openai', model: 'public-model', multiplier: 1.5 }]
          : [{ platform: 'grok', model: 'public-model', multiplier: 7 }]
      }
    }))
    w.unmount()
  })

  it.each([false, true])('preserves independent supplier service-tier overrides when saving a group: tier-only=%s', async tierOnly => {
    getGroupById.mockResolvedValue({ ...sourceGroup, platform: 'composite', billing_policy_version: 13,
      billing_rate_policy: { platform_multipliers: { openai: 2, grok: 0.5 }, model_multipliers: [] },
      model_pricing: [
        { platform: 'openai', models: ['shared-model'], billing_mode: 'token', input_price: tierOnly ? null : 0.000001, fast_multiplier: 3, flex_multiplier: 0.25, intervals: [] },
        { platform: 'grok', models: ['shared-model'], billing_mode: 'token', input_price: tierOnly ? null : 0.000002, fast_multiplier: 4, flex_multiplier: 0.75, intervals: [] }
      ]
    })
    const w = mountView()
    await flushPromises()
    await w.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const cards = w.findAllComponents(PricingEntryCard)
    expect(cards).toHaveLength(2)
    expect(cards.map(card => [card.props('entry').fast_multiplier, card.props('entry').flex_multiplier])).toEqual([[3, 0.25], [4, 0.75]])
    await w.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledExactlyOnceWith(sourceGroup.id, expect.objectContaining({
      expected_billing_policy_version: 13,
      billing_rate_policy: { platform_multipliers: { openai: 2, grok: 0.5 }, model_multipliers: [] },
      model_pricing: [
        expect.objectContaining({ platform: 'openai', models: ['shared-model'], input_price: tierOnly ? null : 0.000001, fast_multiplier: 3, flex_multiplier: 0.25 }),
        expect.objectContaining({ platform: 'grok', models: ['shared-model'], input_price: tierOnly ? null : 0.000002, fast_multiplier: 4, flex_multiplier: 0.75 })
      ]
    }))
    w.unmount()
  })

  it.each([false, true])('removes card rules by default and preserves only an explicit rate-only choice: %s', async keepRates => {
    getGroupById.mockResolvedValue({ ...sourceGroup,
      billing_rate_policy: { platform_multipliers: {}, model_multipliers: [{ platform: 'openai', model: 'a', multiplier: 1.5 }] },
      model_pricing: [{ platform: 'openai', models: ['a'], billing_mode: 'token', input_price: 0.000001, output_price: 0.000002, intervals: [] }]
    })
    const w = mountView()
    await flushPromises()
    await w.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    if (keepRates) w.getComponent(GroupModelRateFields).vm.$emit('rateOnly')
    else w.getComponent(PricingEntryCard).vm.$emit('remove')
    await flushPromises()
    expect(w.findComponent(PricingEntryCard).exists()).toBe(false)
    expect(w.find('#edit-model-billing-rate-model-0-name').exists()).toBe(keepRates)
    await w.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(sourceGroup.id, expect.objectContaining({
      model_pricing: [], billing_rate_policy: { platform_multipliers: {}, model_multipliers: keepRates ? [{ platform: 'openai', model: 'a', multiplier: 1.5 }] : [] }
    }))
    w.unmount()
  })

  it('keeps the exclusive switch visible, defaults it on for subscriptions, and submits that value', async () => {
    const wrapper = mountView()
    await flushPromises()
    const createButton = wrapper.findAll('button').find(button => button.text() === 'admin.groups.createGroup')
    expect(createButton).toBeTruthy()
    await createButton!.trigger('click')
    const form = wrapper.get('#create-group-form')
    const exclusive = () => form.get('[aria-label="admin.groups.form.exclusive"]')
    expect(exclusive().attributes('aria-checked')).toBe('false')
    const billing = wrapper.findAllComponents(Select).find(select =>
      select.props('options').some(option => option.value === 'subscription'))
    expect(billing).toBeTruthy()
    billing!.vm.$emit('update:modelValue', 'subscription')
    await flushPromises()
    expect(exclusive().attributes('aria-checked')).toBe('true')
    await form.get('input').setValue('Subscription default')
    await form.trigger('submit')
    await flushPromises()
    expect(createGroup).toHaveBeenCalledWith(expect.objectContaining({
      subscription_type: 'subscription', is_exclusive: true,
    }))
    wrapper.unmount()
  })

  it('preserves manual exclusivity after switching back to standard billing', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.groups.createGroup')!.trigger('click')
    const billing = wrapper.findAllComponents(Select).find(select =>
      select.props('options').some(option => option.value === 'subscription'))!
    billing.vm.$emit('update:modelValue', 'subscription')
    await flushPromises()
    billing.vm.$emit('update:modelValue', 'standard')
    await flushPromises()
    const exclusive = wrapper.get('#create-group-form [aria-label="admin.groups.form.exclusive"]')
    expect(exclusive.attributes('aria-checked')).toBe('true')
    await exclusive.trigger('click')
    expect(exclusive.attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })

  it('offers only bound OpenAI OAuth reset sources after copying accounts from another group', async () => {
    listAccounts.mockImplementation(async (_page: number, _pageSize: number, filters?: { group?: string }) => {
      if (filters?.group === '42') {
        return {
          items: [
            { id: 501, name: 'Bound OAuth', platform: 'openai', type: 'oauth', parent_account_id: null },
            { id: 503, name: 'OAuth shadow', platform: 'openai', type: 'oauth', parent_account_id: 501 },
          ],
          total: 2,
          page: 1,
          page_size: 100,
          pages: 1,
        }
      }
      return {
        items: [
          { id: 999, name: 'Unrelated OAuth', platform: 'openai', type: 'oauth', parent_account_id: null },
        ],
        total: 1,
        page: 1,
        page_size: 100,
        pages: 1,
      }
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.groups.createGroup')!.trigger('click')
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      quotaResetSourceAccounts: Array<{ id: number }>
      createForm: {
        platform: string
        subscription_type: string
        quota_reset_source_account_id: number | null
        copy_accounts_from_group_ids: number[]
      }
    }
    expect(vm.quotaResetSourceAccounts).toEqual([])
    vm.createForm.copy_accounts_from_group_ids = [42]
    await flushPromises()
    expect(listAccounts).toHaveBeenCalledWith(1, 100, { platform: 'openai', type: 'oauth', group: '42' })
    expect(vm.quotaResetSourceAccounts.map(account => account.id)).toEqual([501])
    vm.createForm.platform = 'openai'
    vm.createForm.subscription_type = 'subscription'
    vm.createForm.quota_reset_source_account_id = 501
    await flushPromises()
    await wrapper.get('#create-group-form input').setValue('Follow upstream reset')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()

    expect(createGroup).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'openai',
      subscription_type: 'subscription',
      quota_reset_source_account_id: 501,
      quota_reset_include_monthly: false,
    }))
    wrapper.unmount()
  })

  it.each(['resolve', 'reject', 'clear'] as const)('ignores an obsolete source lookup after %s', async (outcome) => {
    let resolveOld!: (value: unknown) => void
    let rejectOld!: (reason: Error) => void
    listAccounts.mockImplementationOnce(() => new Promise((resolve, reject) => {
      resolveOld = resolve
      rejectOld = reject
    }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.groups.createGroup')!.trigger('click')
    const vm = wrapper.vm as unknown as {
      quotaResetSourceAccounts: Array<{ id: number }>
      quotaResetSourcesLoading: boolean
      createForm: { platform: string, subscription_type: string, copy_accounts_from_group_ids: number[], quota_reset_source_account_id: number | null }
    }
    vm.createForm.platform = 'openai'
    vm.createForm.subscription_type = 'subscription'
    vm.createForm.copy_accounts_from_group_ids = [42]
    await flushPromises()
    expect(vm.quotaResetSourcesLoading).toBe(true)
    const latest = [{ id: 502, name: 'Latest', platform: 'openai', type: 'oauth' }]
    listAccounts.mockResolvedValue({ items: latest, total: 1 })
    vm.createForm.copy_accounts_from_group_ids = outcome === 'clear' ? [] : [43]
    await flushPromises()
    expect(vm.quotaResetSourcesLoading).toBe(false)
    if (outcome !== 'clear') vm.createForm.quota_reset_source_account_id = 502
    if (outcome === 'reject') rejectOld(new Error('Old lookup failed'))
    else resolveOld({ items: [{ ...latest[0], id: 501 }], total: 1 })
    await flushPromises()
    expect(vm.quotaResetSourceAccounts.map(account => account.id)).toEqual(outcome === 'clear' ? [] : [502])
    expect(vm.createForm.quota_reset_source_account_id).toBe(outcome === 'clear' ? null : 502)
    wrapper.unmount()
  })

  it('clears the saved reset source when replacement members exclude it', async () => {
    listGroups.mockResolvedValue({ items: [{ ...sourceGroup, subscription_type: 'subscription', quota_reset_source_account_id: 501 }], total: 1 })
    listAccounts.mockResolvedValue({ items: [{ id: 501, name: 'Original', platform: 'openai', type: 'oauth' }], total: 1 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const vm = wrapper.vm as unknown as {
      editForm: { copy_accounts_from_group_ids: number[], quota_reset_source_account_id: number | null }
      editQuotaResetSourceOptions: Array<{ value: number | null }>
    }
    expect(vm.editForm.quota_reset_source_account_id).toBe(501)
    listAccounts.mockRejectedValueOnce(new Error('Lookup failed'))
    vm.editForm.copy_accounts_from_group_ids = [44]
    await flushPromises()
    expect(vm.editForm.quota_reset_source_account_id).toBe(501)
    listAccounts.mockResolvedValue({ items: [], total: 0 })
    vm.editForm.copy_accounts_from_group_ids = [43]
    await flushPromises()
    expect(vm.editForm.quota_reset_source_account_id).toBeNull()
    expect(vm.editQuotaResetSourceOptions.map(option => option.value)).not.toContain(501)
    wrapper.unmount()
  })

  it('loads the current weekly baseline before opening the editor', async () => {
    const fresh = { ...sourceGroup, subscription_type: 'subscription' as const,
      quota_reset_source_account_id: 501, quota_reset_source_status: 'active' as const,
      quota_reset_source_reset_at: '2026-09-19T06:00:00Z' }
    vi.mocked(adminAPI.groups.getById).mockResolvedValueOnce(fresh)
    listAccounts.mockResolvedValue({ items: [{ id: 501, name: 'Source', platform: 'openai', type: 'oauth' }], total: 1 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const vm = wrapper.vm as unknown as { editingGroup: AdminGroup; editForm: { quota_reset_source_account_id: number | null } }
    expect(vm.editingGroup.quota_reset_source_reset_at).toBe(fresh.quota_reset_source_reset_at)
    expect(vm.editForm.quota_reset_source_account_id).toBe(501)
    wrapper.unmount()
  })

  it.each(['switch', 'close', 'unmount'] as const)('ignores old routing loads after editor %s', async (action) => {
    const first = { ...sourceGroup, model_routing: { 'gpt-*': [501] } }
    const second = { ...sourceGroup, id: 43, name: 'Second' }
    vi.mocked(adminAPI.groups.getById).mockResolvedValueOnce(first).mockResolvedValueOnce(second)
    let resolveAccount!: (value: Awaited<ReturnType<typeof adminAPI.accounts.getById>>) => void
    vi.mocked(adminAPI.accounts.getById).mockImplementationOnce(() => new Promise(resolve => { resolveAccount = resolve }))
    const wrapper = mountView()
    await flushPromises()
    const vm = wrapper.vm as unknown as {
      handleEdit: (group: AdminGroup) => Promise<void>
      closeEditModal: () => void
      editingGroup: AdminGroup | null
      showEditModal: boolean
      editModelRoutingRules: unknown[]
    }
    const firstLoad = vm.handleEdit(first)
    await flushPromises()
    if (action === 'switch') await vm.handleEdit(second)
    else if (action === 'close') vm.closeEditModal()
    else wrapper.unmount()
    resolveAccount({ id: 501, name: 'Old routing account' } as Awaited<ReturnType<typeof adminAPI.accounts.getById>>)
    await firstLoad
    await flushPromises()
    expect(vm.editModelRoutingRules).toEqual([])
    expect(vm.showEditModal).toBe(action === 'switch')
    if (action === 'switch') expect(vm.editingGroup?.id).toBe(second.id)
    if (action !== 'unmount') wrapper.unmount()
  })

  it('does not open a stale editor when loading current group detail fails', async () => {
    vi.mocked(adminAPI.groups.getById).mockRejectedValueOnce(new Error('Unavailable'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    expect((wrapper.vm as unknown as { showEditModal: boolean }).showEditModal).toBe(false)
    expect(showError).toHaveBeenCalledWith('admin.groups.failedToLoad')
    wrapper.unmount()
  })

  it('refreshes the open baseline without overwriting unsaved group settings', async () => {
    const initial = { ...sourceGroup, subscription_type: 'subscription' as const,
      quota_reset_source_account_id: 501, quota_reset_source_status: 'waiting' as const,
      quota_reset_source_reset_at: null }
    vi.mocked(adminAPI.groups.getById).mockResolvedValueOnce(initial)
    listAccounts.mockResolvedValue({ items: [{ id: 501, name: 'Source', platform: 'openai', type: 'oauth' }], total: 1 })
    const wrapper = mountView()
    await flushPromises()
    vi.useFakeTimers()
    try {
      await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
      await flushPromises()
      const vm = wrapper.vm as unknown as { editingGroup: AdminGroup; editForm: { name: string } }
      vm.editForm.name = 'Unsaved name'
      const baseline = '2026-09-19T06:00:00Z'
      vi.mocked(adminAPI.groups.getById).mockResolvedValueOnce({ ...initial, quota_reset_source_status: 'active', quota_reset_source_reset_at: baseline })
      await vi.advanceTimersByTimeAsync(15_000)
      await flushPromises()
      expect(vm.editingGroup.quota_reset_source_reset_at).toBe(baseline)
      expect(vm.editForm.name).toBe('Unsaved name')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([true, false])('shows the saved exclusivity of an existing subscription: %s', async (isExclusive) => {
    listGroups.mockResolvedValue({
      items: [{ ...sourceGroup, subscription_type: 'subscription', is_exclusive: isExclusive }],
      total: 1, page: 1, page_size: 20, pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const exclusive = wrapper.get('#edit-group-form [aria-label="admin.groups.form.exclusive"]')
    expect(exclusive.attributes('aria-checked')).toBe(String(isExclusive))
    wrapper.unmount()
  })

  it('duplicates the selected group, reports success, and refreshes the list', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(duplicateGroup).toHaveBeenCalledTimes(1)
    expect(duplicateGroup).toHaveBeenCalledWith(42)
    expect(showSuccess).toHaveBeenCalledWith('admin.groups.duplicateSuccess')
    expect(listGroups).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('hides advanced group actions in simple mode', async () => {
    authState.isSimpleMode = true
    const compositeGroup = { ...sourceGroup, platform: 'composite' }
    listGroups.mockResolvedValueOnce({ items: [compositeGroup], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="group-duplicate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-composite-routes"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-rate-multipliers"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="group-rpm-overrides"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('ignores repeated clicks while the duplicate request is in flight', async () => {
    let resolveDuplicate!: (value: AdminGroup) => void
    duplicateGroup.mockImplementationOnce(
      () => new Promise<AdminGroup>((resolve) => { resolveDuplicate = resolve })
    )
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.get('[data-testid="group-duplicate"]')
    void button.trigger('click')
    void button.trigger('click')
    await wrapper.vm.$nextTick()

    expect(duplicateGroup).toHaveBeenCalledTimes(1)
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('title')).toBe('admin.groups.duplicating')

    resolveDuplicate({ ...sourceGroup, id: 43, name: 'Primary (Copy)', status: 'inactive' })
    await flushPromises()
    expect(wrapper.get('[data-testid="group-duplicate"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('shows the API error and restores the action when duplication fails', async () => {
    duplicateGroup.mockRejectedValueOnce(new Error('duplicate failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('duplicate failed')
    expect(wrapper.get('[data-testid="group-duplicate"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('does not report a successful duplicate as failed when the refresh fails', async () => {
    listGroups
      .mockResolvedValueOnce({
        items: [sourceGroup],
        total: 1,
        page: 1,
        page_size: 20,
        pages: 1
      })
      .mockRejectedValueOnce(new Error('refresh failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-duplicate"]').trigger('click')
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('admin.groups.duplicateSuccess')
    expect(showError).toHaveBeenCalledWith('admin.groups.failedToLoad')
    expect(showError).not.toHaveBeenCalledWith('admin.groups.duplicateFailed')
    wrapper.unmount()
  })

  it('keeps OAuth and privacy switches independent and submits their selected values', async () => {
    updateGroup.mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    await flushPromises()
    const editButton = wrapper.findAll('button').find(button => button.text() === 'common.edit')
    await editButton!.trigger('click')
    await flushPromises()
    const oauth = wrapper.get('[aria-label="admin.groups.accountFilters.oauthOnly"]')
    const privacy = wrapper.get('[aria-label="admin.groups.accountFilters.privacySetOnly"]')
    await oauth.trigger('click')
    expect(oauth.attributes('aria-checked')).toBe('true')
    expect(privacy.attributes('aria-checked')).toBe('false')
    await privacy.trigger('click')
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      require_oauth_only: true,
      require_privacy_set: true,
    }))
    wrapper.unmount()
  })

  it('sends the billing version originally loaded in the editor and keeps stale drafts on conflict', async () => {
    getGroupById.mockResolvedValue({ ...sourceGroup, billing_policy_version: 7, billing_rate_policy: { platform_multipliers: { openai: 1 }, model_multipliers: [] } })
    updateGroup.mockRejectedValueOnce({ status: 409, reason: 'GROUP_CONFIGURATION_CHANGED', message: 'reload group' })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
    await flushPromises()
    const vm = wrapper.vm as unknown as { editForm: { name: string; expected_billing_policy_version?: number }; showEditModal: boolean }
    vm.editForm.name = 'My unsaved group'
    // A later server value must never be silently adopted by an open editor.
    getGroupById.mockResolvedValue({ ...sourceGroup, billing_policy_version: 8 })
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({ name: 'My unsaved group', expected_billing_policy_version: 7 }))
    expect(showError).toHaveBeenCalledWith('billingRates.editConflict')
    expect(vm.editForm.name).toBe('My unsaved group')
    expect(vm.editForm.expected_billing_policy_version).toBe(7)
    expect(vm.showEditModal).toBe(true)
    wrapper.unmount()
  })

  it('shows the standardized API message when updating a group fails', async () => {
    updateGroup.mockRejectedValueOnce({
      status: 409,
      code: 409,
      message: 'group name already exists',
      reason: 'GROUP_EXISTS'
    })
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('group name already exists')
    wrapper.unmount()
  })

  it('updates manifest controls immediately and submits the displayed selection', async () => {
    vi.useFakeTimers()
    vi.mocked(adminAPI.accounts.list).mockResolvedValue({
      items: [{ id: 5, name: 'Manifest account' }]
    } as never)
    updateGroup.mockResolvedValue(sourceGroup)
    const wrapper = mountView()
    try {
      await flushPromises()
      const editButton = wrapper.findAll('button').find((button) => button.text() === 'common.edit')!
      await editButton.trigger('click')
      await flushPromises()

      const toggle = wrapper.get('[data-testid="codex-manifest-toggle"]')
      await toggle.trigger('click')
      expect(toggle.attributes('aria-checked')).toBe('true')
      const search = wrapper.get('[data-testid="codex-manifest-search"]')
      await search.trigger('focus')
      await vi.advanceTimersByTimeAsync(300)
      await flushPromises()
      expect(adminAPI.accounts.list).toHaveBeenCalledWith(
        1, 20, { search: '', platform: 'openai', group: '42' }, expect.anything()
      )
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')

      await wrapper.get('[aria-label="remove account 5"]').trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-selected-tags"]').exists()).toBe(false)
      await wrapper.get('#edit-group-form').trigger('submit')
      expect(updateGroup).not.toHaveBeenCalled()
      expect(wrapper.find('[data-testid="codex-manifest-validation-error"]').exists()).toBe(true)

      await search.trigger('focus')
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')
      const fallback = wrapper.get('[data-testid="codex-manifest-fallback-toggle"]')
      await fallback.trigger('click')
      expect(fallback.attributes('aria-checked')).toBe('true')
      await fallback.trigger('click')
      expect(fallback.attributes('aria-checked')).toBe('false')
      await fallback.trigger('click')

      await toggle.trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
      await toggle.trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('Manifest account')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
        codex_models_manifest_config: {
          enabled: true, account_ids: [5], fallback_to_scheduler: true
        }
      }))

      // Reopening reads the saved group afresh, without retaining the prior draft.
      await editButton.trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="codex-manifest-toggle"]').attributes('aria-checked')).toBe('false')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

})
