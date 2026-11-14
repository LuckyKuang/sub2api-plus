import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ImageTasksView from '../ImageTasksView.vue'

const { modes } = vi.hoisted(() => ({ modes: { current: null as null | { ordinary: { enabled: boolean; can_view: boolean; can_submit: boolean; reason?: string }, batch: { enabled: boolean; can_view: boolean; can_submit: boolean; reason?: string } } } }))

vi.mock('@/composables/useImageTaskModes', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useImageTaskModes: () => ({
      modes: ref(modes.current),
      loading: ref(false),
      ordinaryAvailable: ref(modes.current?.ordinary.can_view === true),
      batchAvailable: ref(modes.current?.batch.can_view === true),
      anyAvailable: ref(modes.current?.ordinary.can_view === true || modes.current?.batch.can_view === true),
      refreshImageTaskModes: vi.fn().mockResolvedValue(undefined),
    }),
  }
})

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params?.reason ? key + ':' + params.reason : key }) }
})

const shellStub = { template: '<div><slot /></div>' }
const panelStub = { props: { embedded: { type: Boolean, default: false } }, template: '<div class="panel" :data-embedded="embedded ? \'true\' : \'false\'" />' }

function mountView() {
  return mount(ImageTasksView, {
    global: {
      stubs: {
        AppLayout: shellStub,
        AsyncImageView: panelStub,
        BatchImageGuideView: panelStub,
        Icon: true,
      },
    },
  })
}

describe('ImageTasksView mode selection', () => {
  it('shows the switcher and the ordinary panel when both modes are enabled', async () => {
    modes.current = { ordinary: { enabled: true, can_view: true, can_submit: true }, batch: { enabled: true, can_view: true, can_submit: true } }
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="image-task-mode-switcher"]').exists()).toBe(true)
    expect(wrapper.find('.panel').attributes('data-embedded')).toBe('true')
  })

  it('hides the switcher when only batch is enabled', async () => {
    modes.current = { ordinary: { enabled: false, can_view: false, can_submit: false, reason: 'disabled' }, batch: { enabled: true, can_view: true, can_submit: true } }
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="image-task-mode-switcher"]').exists()).toBe(false)
    expect(wrapper.find('.panel').exists()).toBe(true)
  })

  it('explains why nothing is available instead of rendering an empty list', async () => {
    modes.current = { ordinary: { enabled: false, can_view: false, can_submit: false, reason: 'not_configured' }, batch: { enabled: false, can_view: false, can_submit: false, reason: 'disabled' } }
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="image-tasks-unavailable"]').exists()).toBe(true)
    expect(wrapper.find('.panel').exists()).toBe(false)
  })
})
