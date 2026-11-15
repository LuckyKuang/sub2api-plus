import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import type { ImageTaskModes } from '@/api/imageTasks'
import ImageTasksView from '../ImageTasksView.vue'

enableAutoUnmount(afterEach)

const state = reactive({ modes: null as ImageTaskModes | null, loading: false })
const route = reactive({ query: {} as Record<string, string> })

vi.mock('@/composables/useImageTaskModes', async () => {
  const { computed, ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useImageTaskModes: () => ({
      modes: computed(() => state.modes),
      loading: computed(() => state.loading),
      scopeKey: ref('user-1'),
      ordinaryAvailable: computed(() => state.modes?.ordinary.can_view === true),
      batchAvailable: computed(() => state.modes?.batch.can_view === true),
      anyAvailable: computed(() => state.modes?.ordinary.can_view === true || state.modes?.batch.can_view === true),
      refreshImageTaskModes: vi.fn().mockResolvedValue(undefined),
    }),
  }
})

vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace: vi.fn((target: { query: Record<string, string> }) => { route.query = target.query }) }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { default: messages } = await import('@/i18n/locales/en/imageTasks')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => {
    if (key.startsWith('imageTasks.reasons.')) {
      const reason = key.slice('imageTasks.reasons.'.length) as keyof typeof messages.imageTasks.reasons
      return messages.imageTasks.reasons[reason] || key
    }
    return params?.reason ? key + ':' + params.reason : key
  } }) }
})

const shellStub = { template: '<div><slot /></div>' }
function panelStub(name: string) {
  return {
    name,
    props: { embedded: Boolean, canSubmit: Boolean },
    setup: () => ({ filter: ref('') }),
    template: '<div class="panel" :data-mode="$options.name" :data-can-submit="canSubmit" :data-embedded="embedded"><input v-model="filter" /></div>',
  }
}

function mountView() {
  return mount(ImageTasksView, {
    global: { stubs: {
      AppLayout: shellStub,
      AsyncImageView: panelStub('ordinary'),
      BatchImageGuideView: panelStub('batch'),
      Icon: true,
    } },
  })
}

const available = { enabled: true, can_view: true, can_submit: true }
const unavailable = { enabled: false, can_view: false, can_submit: false, reason: 'disabled' }

describe('ImageTasksView mode selection (proposal R1/R2/R5)', () => {
  beforeEach(() => {
    state.loading = false
    state.modes = { ordinary: { ...available }, batch: { ...available } }
    route.query = {}
  })

  it('preserves the ordinary filter when switching modes and back', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('.panel').attributes('data-embedded')).toBe('true')
    await wrapper.get('.panel input').setValue('saved filter')
    await wrapper.get('[data-testid="image-task-mode-batch"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.panel').attributes('data-mode')).toBe('batch')
    await wrapper.get('[data-testid="image-task-mode-ordinary"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('.panel input').element as HTMLInputElement).value).toBe('saved filter')
  })

  it('renders no panel while capability is pending, even with stale cached permissions', async () => {
    state.loading = true
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('.panel').exists()).toBe(false)
    state.modes = { ordinary: { ...unavailable }, batch: { ...unavailable } }
    state.loading = false
    await flushPromises()
    expect(wrapper.find('[data-testid="image-tasks-unavailable"]').exists()).toBe(true)
    expect(wrapper.find('.panel').exists()).toBe(false)
  })

  it.each(['ordinary', 'batch'] as const)('keeps %s history visible but forbids creation when disabled', async mode => {
    state.modes = { ordinary: { ...unavailable }, batch: { ...unavailable } }
    state.modes[mode] = { ...unavailable, can_view: true }
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.find('[data-testid="image-task-mode-switcher"]').exists()).toBe(false)
    expect(wrapper.get('.panel').attributes('data-mode')).toBe(mode)
    expect(wrapper.get('.panel').attributes('data-can-submit')).toBe('false')
    expect(wrapper.get('[data-testid="image-tasks-readonly"]').text()).toContain('switched off')
    expect(wrapper.get('h2').text()).toBe('imageTasks.modes.' + mode + '.title')
  })

  it('explains an unavailable requested mode while showing the accessible alternative', async () => {
    route.query = { mode: 'batch' }
    state.modes = { ordinary: { ...available }, batch: { ...unavailable } }
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="image-tasks-requested-unavailable"]').text()).toContain('switched off')
    expect(wrapper.get('.panel').attributes('data-mode')).toBe('ordinary')
    expect(route.query.mode).toBe('batch')
  })
})
