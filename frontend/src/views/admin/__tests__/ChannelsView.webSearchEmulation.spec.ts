import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ChannelsView from '@/views/admin/ChannelsView.vue'
import type { Channel } from '@/api/admin/channels'

const { listChannels, createChannel, updateChannel, getGroups, getWebSearchConfig, showError } = vi.hoisted(() => ({
  listChannels: vi.fn(),
  createChannel: vi.fn(),
  updateChannel: vi.fn(),
  getGroups: vi.fn(),
  getWebSearchConfig: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: { list: listChannels, create: createChannel, update: updateChannel },
    groups: { getAll: getGroups },
    settings: { getWebSearchEmulationConfig: getWebSearchConfig },
  },
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

// Exercise the editor and its real Toggle controls through the saved API payload.
// No check below depends on the form's private state or its source text.
const editablePlatforms = ['anthropic', 'openai', 'command_code', 'opencode_go', 'minimax', 'zhipu']
const groups = [...editablePlatforms, 'gemini'].map((platform, index) => ({
  id: index + 1, platform, name: `${platform}-group`, rate_multiplier: 1,
}))
const initialDimensions = Object.fromEntries([...editablePlatforms, 'future_platform'].map(platform => [platform, true]))

function savedChannel(): Channel {
  return {
    id: 42, name: 'Search channel', description: '', status: 'active',
    billing_model_source: 'channel_mapped', restrict_models: false,
    group_ids: groups.map(group => group.id), model_pricing: [], model_mapping: {},
    features_config: {
      web_search_emulation: { ...initialDimensions },
      custom_feature: { retained: true },
    },
    apply_pricing_to_account_stats: false, account_stats_pricing_rules: [],
    created_at: '', updated_at: '',
  }
}

function mountView() {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({ template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>' }),
        BaseDialog: defineComponent({ props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' }),
        DataTable: defineComponent({
          props: ['data'],
          template: '<section><template v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></template></section>',
        }),
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        GroupModelRateLink: true,
        Icon: true,
        PlatformIcon: true,
      },
    },
  })
}

type View = ReturnType<typeof mountView>

async function openEdit(wrapper: View) {
  await flushPromises()
  await wrapper.findAll('button').find(button => button.text() === 'common.edit')!.trigger('click')
  await flushPromises()
}

async function openTab(wrapper: View, platform: string) {
  await wrapper.findAll('button[role="tab"]').find(button => button.text() === `admin.groups.platforms.${platform}`)!.trigger('click')
}

function visibleSearchSwitches(wrapper: View) {
  return wrapper.findAll('button[role="switch"][aria-label="admin.channels.form.webSearchEmulation"]').filter(control => control.isVisible())
}

async function submit(wrapper: View) {
  await wrapper.get('#channel-form').trigger('submit')
  await flushPromises()
  expect(showError).not.toHaveBeenCalled()
}

describe('ChannelsView web search configuration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    listChannels.mockResolvedValue({ items: [savedChannel()], total: 1 })
    getGroups.mockResolvedValue(groups)
    getWebSearchConfig.mockResolvedValue({ enabled: true, providers: [{ name: 'test' }] })
    createChannel.mockResolvedValue({})
    updateChannel.mockResolvedValue({})
  })

  it.each(editablePlatforms)('saves an explicit %s disable without erasing other dimensions', async platform => {
    const wrapper = mountView()
    await openEdit(wrapper)
    await openTab(wrapper, platform)
    const controls = visibleSearchSwitches(wrapper)
    expect(controls).toHaveLength(1)
    expect(controls[0].attributes('aria-checked')).toBe('true')
    await controls[0].trigger('click')
    expect(controls[0].attributes('aria-checked')).toBe('false')
    await submit(wrapper)
    expect(updateChannel).toHaveBeenCalledOnce()
    expect(updateChannel).toHaveBeenCalledWith(42, expect.objectContaining({
      features_config: expect.objectContaining({
        web_search_emulation: { ...initialDimensions, [platform]: false },
        custom_feature: { retained: true },
      }),
    }))
    expect(createChannel).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('writes false for a disabled platform section while retaining its other settings', async () => {
    const wrapper = mountView()
    await openEdit(wrapper)
    const label = wrapper.findAll('label').find(candidate => candidate.text() === 'admin.groups.platforms.opencode_go')!
    await label.get('input[type="checkbox"]').setValue(false)
    await submit(wrapper)
    expect(updateChannel).toHaveBeenCalledWith(42, expect.objectContaining({
      group_ids: expect.arrayContaining([1, 2, 3, 5, 6, 7]),
      features_config: expect.objectContaining({
        web_search_emulation: { ...initialDimensions, opencode_go: false },
        custom_feature: { retained: true },
      }),
    }))
    expect(updateChannel.mock.calls[0][1].group_ids).toHaveLength(6)
    wrapper.unmount()
  })

  it('keeps saved dimensions when the global switch hides the controls', async () => {
    getWebSearchConfig.mockResolvedValue({ enabled: false, providers: [] })
    const wrapper = mountView()
    await openEdit(wrapper)
    await openTab(wrapper, 'openai')
    expect(visibleSearchSwitches(wrapper)).toHaveLength(0)
    await submit(wrapper)
    expect(updateChannel).toHaveBeenCalledWith(42, expect.objectContaining({
      features_config: expect.objectContaining({
        web_search_emulation: initialDimensions,
        custom_feature: { retained: true },
      }),
    }))
    wrapper.unmount()
  })

  it('does not offer emulation for a platform outside the editor capability list', async () => {
    const wrapper = mountView()
    await openEdit(wrapper)
    await openTab(wrapper, 'gemini')
    expect(visibleSearchSwitches(wrapper)).toHaveLength(0)
    wrapper.unmount()
  })

  it('creates a channel with the selected platform and its search preference', async () => {
    listChannels.mockResolvedValue({ items: [], total: 0 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.createChannel')!.trigger('click')
    await flushPromises()
    await wrapper.get('input[required]').setValue('New search channel')
    const platformLabel = wrapper.findAll('label').find(candidate => candidate.text() === 'admin.groups.platforms.openai')!
    await platformLabel.get('input[type="checkbox"]').setValue(true)
    await openTab(wrapper, 'openai')
    const groupLabel = wrapper.findAll('label').find(candidate => candidate.text().includes('openai-group'))!
    await groupLabel.get('input[type="checkbox"]').setValue(true)
    await visibleSearchSwitches(wrapper)[0].trigger('click')
    await submit(wrapper)
    expect(createChannel).toHaveBeenCalledOnce()
    expect(createChannel).toHaveBeenCalledWith(expect.objectContaining({
      name: 'New search channel', group_ids: [2],
      features_config: expect.objectContaining({ web_search_emulation: { openai: true } }),
    }))
    expect(updateChannel).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
