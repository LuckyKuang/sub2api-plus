import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import zh from '@/i18n/locales/zh/channelMonitorV3'
import ChannelStatusV3View from '../ChannelStatusV3View.vue'
import { getStatusSnapshot, type StatusSnapshot } from '@/api/channelMonitorV3'

vi.mock('@/api/channelMonitorV3', () => ({ getStatusSnapshot: vi.fn() }))
vi.mock('@/composables/useAutoRefresh', () => ({ useAutoRefresh: () => ({ setEnabled: vi.fn() }) }))
const fixture = (): StatusSnapshot => ({
  computed_at: '2026-10-01T12:00:00Z', data_through: '2026-10-01T12:00:00Z',
  summary: { status: 'partial', normal: 0, affected: 1, unknown: 1, recovering: 0, active_events: 1 },
  platforms: [
    { platform: 'openai', status: 'partial', success_rate: .99, ttft_p50_ms: 2000, last_request_at: '2026-10-01T11:59:00Z', timeline: [{ at: '2026-10-01T11:30:00Z', status: 'unknown' }], models: [{ group_id: 1, group_name: '标准分组', model: 'gpt-test', status: 'partial' }] },
    { platform: 'gemini', status: 'insufficient', success_rate: null, ttft_p50_ms: null, last_request_at: null, timeline: [], models: [] },
  ],
  incidents: [{ id: 'event-1', platform: 'openai', group_id: 1, group_name: '标准分组', model: 'gpt-test', severity: 'partial', phase: 'detected', started_at: '2026-10-01T11:30:00Z', updated_at: '2026-10-01T11:40:00Z', resolved_at: null, updates: [{ phase: 'detected', severity: 'partial', at: '2026-10-01T11:40:00Z' }] }],
})
let wrapper: VueWrapper
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    locale: { value: 'zh' },
    t: (key: string, params: Record<string, unknown> = {}) => {
      const message = key.split('.').reduce<any>((value, part) => value?.[part], { ...zh, common: { refresh: '刷新', loading: '加载中' } })
      return String(message || key).replace(/\{(\w+)\}/g, (_, name) => String(params[name] ?? ''))
    },
  }),
}))
async function render() {
  wrapper = mount(ChannelStatusV3View, { global: {
    stubs: { AppLayout: { template: '<div><slot /></div>' }, BaseDialog: { props: ['show', 'title'], template: '<div v-if="show" data-testid="detail"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>' }, ProviderIcon: true },
  } })
  await flushPromises()
  return wrapper
}
describe('V3 service status', () => {
  beforeEach(() => { vi.mocked(getStatusSnapshot).mockReset(); vi.mocked(getStatusSnapshot).mockResolvedValue(fixture()) })
  afterEach(() => wrapper?.unmount())
  it('keeps the summary banner and places records only in the incident tab', async () => {
    await render()
    expect(wrapper.text()).toContain('部分服务出现异常')
    expect(wrapper.text()).toContain('1 个事件未结束')
    expect(wrapper.find('details').exists()).toBe(false)
    const card = wrapper.find('.platform-card')
    expect(card.classes()).toContain('bg-white')
    expect(card.classes()).toContain('dark:bg-dark-800')
    expect(card.find('h3').classes()).toContain('text-emerald-600')
    await wrapper.find('[data-testid="view-events"]').trigger('click')
    expect(wrapper.find('#v3-events-panel').exists()).toBe(true)
    expect(wrapper.find('details').text()).toContain('监控到故障')
    expect(wrapper.find('#v3-platform-panel').exists()).toBe(false)
  })
  it('shows unknown samples as missing metrics and opens model details', async () => {
    await render()
    expect(wrapper.findAll('.platform-card')[1].text()).toContain('样本不足')
    expect(wrapper.findAll('.platform-card')[1].text()).toContain('—')
    await wrapper.find('.platform-card').trigger('click')
    expect(wrapper.find('[data-testid="detail"]').text()).toContain('gpt-test')
    expect(wrapper.find('[data-testid="detail"]').text()).toContain('标准分组')
  })
  it('changes range and filters events without adding a duplicate list to platforms', async () => {
    await render()
    await wrapper.find('select').setValue('gemini')
    expect(wrapper.findAll('.platform-card')).toHaveLength(1)
    await wrapper.find('#v3-events-tab').trigger('click')
    expect(wrapper.findAll('details')).toHaveLength(0)
    await wrapper.findAll('[aria-pressed]')[1].trigger('click')
    await flushPromises()
    expect(getStatusSnapshot).toHaveBeenLastCalledWith('7d', expect.any(AbortSignal))
  })
  it('does not retain a healthy-looking snapshot when refresh fails', async () => {
    await render()
    vi.mocked(getStatusSnapshot).mockRejectedValue(new Error('unavailable'))
    await wrapper.find('[aria-label="刷新"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('暂时无法获取服务状态')
    expect(wrapper.findAll('.platform-card')).toHaveLength(0)
  })
})
