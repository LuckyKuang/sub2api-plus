import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import zh from '@/i18n/locales/zh/channelMonitorV3'
import extensions from '@/i18n/locales/zh/featureExtensions'
import ChannelStatusV3View from '../ChannelStatusV3View.vue'
import { getStatusSnapshot, type ServiceStatus, type StatusSnapshot } from '@/api/channelMonitorV3'

vi.mock('@/api/channelMonitorV3', () => ({ getStatusSnapshot: vi.fn() }))
vi.mock('@/composables/useAutoRefresh', () => ({ useAutoRefresh: () => ({ setEnabled: vi.fn() }) }))
const fixture = (): StatusSnapshot => ({
  monitoring_enabled: true, computed_at: '2026-10-01T12:00:00Z', data_through: '2026-10-01T12:00:00Z',
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
      const message = key.split('.').reduce<any>((value, part) => value?.[part], { ...zh, ...extensions, common: { refresh: '刷新', loading: '加载中' } })
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
  // Current observations and historical aggregation have independent clocks.
  it('shows the current observation cutoff even when historical aggregation lags', async () => {
    const value = fixture()
    value.current_through = '2026-10-01T12:00:17Z'
    value.data_through = '2026-10-01T11:58:00Z'
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    const cutoff = wrapper.get('[data-testid="current-through"]').text()
    expect(cutoff).toContain('12:00:17')
    expect(cutoff).not.toContain('11:58:00')
  })
  // docs/CHANNEL_MONITOR.md: opening details refreshes current observations;
  // window boundaries and incident progress remain independent and visible.
  it('refreshes when opening details and shows the exact window and last observation', async () => {
    await render()
    const fresh = fixture()
    fresh.current_window_start = '2026-10-01T11:55:17Z'
    fresh.current_window_end = '2026-10-01T12:00:17Z'
    fresh.platforms[0].models[0] = { ...fresh.platforms[0].models[0], status: 'normal', last_observed_at: '2026-10-01T12:00:10Z', observation_reason: 'observed', incident_phase: 'awaiting_data' }
    vi.mocked(getStatusSnapshot).mockResolvedValueOnce(fresh)
    await wrapper.find('.platform-card').trigger('click')
    await flushPromises()
    expect(getStatusSnapshot).toHaveBeenCalledTimes(2)
    const detail = wrapper.get('[data-testid="detail"]')
    expect(detail.get('[data-testid="current-window"]').text()).toContain(':17')
    expect(detail.get('tbody .status-badge').text()).toBe('运行正常')
    expect(detail.get('tbody tr').text()).toContain(':10')
    expect(detail.get('tbody tr').text()).toContain('等待新观测')
    for (const secret of ['标准分组', '样本数', 'group_id', 'monitor_id']) expect(detail.text()).not.toContain(secret)
  })
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
  // User requirement: "模型状态；不要有分组的概念". Headings, rows and
  // incidents must present models without exposing group/monitor names.
  it('shows model details without group concepts and keeps empty observations explicit', async () => {
    await render()
    expect(wrapper.findAll('.platform-card')[1].text()).toContain('样本不足')
    expect(wrapper.findAll('.platform-card')[1].text()).toContain('—')
    await wrapper.find('.platform-card').trigger('click')
    const detail = wrapper.get('[data-testid="detail"]')
    expect(detail.get('h3').text()).toBe('模型状态')
    expect(detail.findAll('th').map(cell => cell.text())).toEqual(['模型', '近 5 分钟状态', '最后观测', '历史事件进度'])
    expect(detail.get('tbody tr').text()).toContain('gpt-test')
    expect(detail.get('.status-partial').text()).toBe('部分服务异常')
    expect(detail.text()).not.toContain('分组')
    expect(detail.text()).not.toContain('范围')
    await wrapper.findAll('.platform-card')[1].trigger('click')
    expect(wrapper.get('[data-testid="detail"]').text()).toContain('暂无模型观测数据。')
    expect(wrapper.findAll('[data-testid="detail"] tbody tr')).toHaveLength(0)
  })
  it('combines request and probe observations into one model row and omits names in incidents', async () => {
    const value = fixture()
    value.platforms[0].models[0].status = 'normal'
    value.platforms[0].models.push({ source_kind: 'probe', monitor_id: 1, group_id: null, group_name: '主动检查', model: 'gpt-test', status: 'outage' })
    value.incidents.push({ ...value.incidents[0], id: 'probe-event', source_kind: 'probe', monitor_id: 1, group_id: null, group_name: '主动检查' })
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    await wrapper.find('.platform-card').trigger('click')
    const detail = wrapper.get('[data-testid="detail"]')
    expect(detail.findAll('tbody tr')).toHaveLength(1)
    expect(detail.text()).toContain('真实请求')
    expect(detail.text()).toContain('主动探测')
    expect(detail.get('tbody .status-badge').text()).toBe('部分服务异常')
    expect(detail.text()).not.toContain('标准分组')
    expect(detail.text()).not.toContain('主动检查')
    await wrapper.find('#v3-events-tab').trigger('click')
    const events = wrapper.get('#v3-events-panel')
    expect(events.text()).toContain('真实请求 · gpt-test')
    expect(events.text()).toContain('主动探测 · gpt-test')
    expect(events.text()).not.toContain('标准分组')
    expect(events.text()).not.toContain('主动检查')
  })
  // docs/CHANNEL_MONITOR.md: combined models retain the latest observation
  // instant across sources, including RFC3339 timestamps with different offsets.
  it('selects the latest observation by instant across timestamp offsets', async () => {
    const value = fixture()
    value.platforms[0].models[0].last_observed_at = '2026-10-01T20:00:09+08:00'
    value.platforms[0].models.push({ source_kind: 'probe', monitor_id: 1, group_id: null, group_name: '', model: 'gpt-test', status: 'normal', last_observed_at: '2026-10-01T12:00:10Z' })
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    await wrapper.find('.platform-card').trigger('click')
    const observed = wrapper.get('[data-testid="detail"] tbody tr').findAll('td')[2].text()
    expect(observed).toContain(':10')
    expect(observed).not.toContain(':09')
  })
  it('deduplicates models across groups and monitors without merging different models', async () => {
    const value = fixture()
    value.platforms[0].models.push(
      { group_id: 2, group_name: '专属分组', model: 'gpt-test', status: 'normal' },
      { source_kind: 'probe', monitor_id: 7, group_id: null, group_name: '主监控', model: 'gpt-test', status: 'normal' },
      { source_kind: 'probe', monitor_id: 8, group_id: null, group_name: '备用监控', model: 'gpt-test', status: 'normal' },
      { group_id: 2, group_name: '专属分组', model: 'gpt-other', status: 'normal' },
    )
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    await wrapper.find('.platform-card').trigger('click')
    const detail = wrapper.get('[data-testid="detail"]')
    const rows = detail.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows.map(row => row.get('td').text())).toEqual(['gpt-other真实请求', 'gpt-test真实请求主动探测'])
    expect(rows[0].get('.status-badge').text()).toBe('运行正常')
    expect(rows[1].get('.status-badge').text()).toBe('部分服务异常')
    for (const name of ['标准分组', '专属分组', '主监控', '备用监控']) expect(detail.text()).not.toContain(name)
  })
  // docs/CHANNEL_MONITOR.md, V3 联合主动探测: failed evidence must not be
  // masked; all known failures mean outage; absent evidence is not recovery.
  // These expected labels are fixed business examples, not helper outputs.
  it.each<[ServiceStatus, ServiceStatus, string]>([
    ['normal', 'normal', '运行正常'],
    ['normal', 'outage', '部分服务异常'],
    ['outage', 'normal', '部分服务异常'],
    ['outage', 'outage', '服务中断'],
    ['outage', 'unknown', '服务中断'],
    ['partial', 'normal', '部分服务异常'],
    ['degraded', 'normal', '性能下降'],
    ['degraded', 'recovering', '性能下降'],
    ['recovering', 'normal', '恢复观察'],
    ['unknown', 'unknown', '暂无近期数据'],
    ['insufficient', 'unknown', '样本不足'],
    ['insufficient', 'insufficient', '样本不足'],
    ['insufficient', 'normal', '运行正常'],
  ])('shows %s requests and %s probes as one model with %s', async (request, probe, expected) => {
    const value = fixture()
    value.incidents = []
    value.platforms[0].models[0].status = request
    value.platforms[0].models.push({ source_kind: 'probe', monitor_id: 1, group_id: null, group_name: '探测名称', model: 'gpt-test', status: probe })
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    await wrapper.find('.platform-card').trigger('click')
    const rows = wrapper.findAll('[data-testid="detail"] tbody tr')
    expect(rows).toHaveLength(1)
    expect(rows[0].get('.status-badge').text()).toBe(expected)
  })
  it.each<ServiceStatus>(['normal', 'recovering'])('shows recent %s independently from a historical incident awaiting evidence', async (healthyStatus) => {
    const value = fixture()
    value.platforms[0].models[0].status = 'unknown'
    value.platforms[0].models.push({ source_kind: 'probe', monitor_id: 1, group_id: null, group_name: '探测名称', model: 'gpt-test', status: healthyStatus })
    value.incidents[0].phase = 'awaiting_data'
    value.platforms[0].models[0].incident_phase = 'awaiting_data'
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    await wrapper.find('.platform-card').trigger('click')
    expect(wrapper.get('[data-testid="detail"] tbody .status-badge').text()).toBe(healthyStatus === 'normal' ? '运行正常' : '恢复观察')
    expect(wrapper.get('[data-testid="detail"]').text()).toContain('等待新观测')
  })
  it('does not let resolved incidents or incidents on another scope change a healthy model', async () => {
    const value = fixture()
    value.platforms[0].models[0].status = 'unknown'
    value.platforms[0].models.push({ source_kind: 'probe', monitor_id: 1, group_id: null, group_name: '探测名称', model: 'gpt-test', status: 'normal' })
    value.incidents[0].resolved_at = '2026-10-01T11:55:00Z'
    value.incidents.push({ ...value.incidents[0], id: 'other-scope', group_id: 2, group_name: '其他分组', resolved_at: null })
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    await wrapper.find('.platform-card').trigger('click')
    expect(wrapper.get('[data-testid="detail"] tbody .status-badge').text()).toBe('运行正常')
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
  it('clears a disabled selected platform and its open details after refresh', async () => {
    await render()
    await wrapper.find('select').setValue('openai')
    await wrapper.find('.platform-card').trigger('click')
    const value = fixture()
    value.platforms = value.platforms.filter(item => item.platform !== 'openai')
    value.incidents = []
    value.summary = { status: 'unknown', normal: 0, affected: 0, unknown: 1, recovering: 0, active_events: 0 }
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await wrapper.find('[aria-label="刷新"]').trigger('click'); await flushPromises()
    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe('')
    expect(wrapper.find('[data-testid="detail"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('gpt-test')
    vi.mocked(getStatusSnapshot).mockResolvedValue(fixture())
    await wrapper.find('[aria-label="刷新"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="detail"]').exists()).toBe(false)
  })
  it('shows monitoring disabled when all platform switches are off', async () => {
    const value = fixture()
    value.monitoring_enabled = false
    value.platforms = []
    value.incidents = []
    value.summary = { status: 'unknown', normal: 0, affected: 0, unknown: 0, recovering: 0, active_events: 0 }
    vi.mocked(getStatusSnapshot).mockResolvedValue(value)
    await render()
    expect(wrapper.text()).toContain('暂无启用的监控平台')
    expect(wrapper.findAll('.platform-card')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('已观测服务运行正常')
  })
})
