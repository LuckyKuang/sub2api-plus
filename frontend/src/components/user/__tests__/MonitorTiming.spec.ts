import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCard from '../monitor/MonitorCard.vue'
import MonitorTimeline from '../monitor/MonitorTimeline.vue'
import MonitorRunResultDialog from '@/components/admin/monitor/MonitorRunResultDialog.vue'

enableAutoUnmount(afterEach)
vi.mock('@/utils/featureFlags', () => ({ isChannelMonitorQuotaVisible: () => false }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key, te: () => true }),
}))

// docs/CHANNEL_MONITOR.md: first byte, total duration and HEAD Ping are
// independent; old observations must not manufacture a first-byte value.
describe('V1 independent timing display', () => {
  const item: UserMonitorView = {
    id: 1, name: 'Channel', provider: 'openai', group_name: '', primary_model: 'model',
    primary_status: 'degraded', primary_first_byte_ms: 17, primary_latency_ms: 6000,
    primary_ping_latency_ms: 3, availability_7d: 100, extra_models: [], timeline: [],
  }
  it('labels card timings separately and keeps probe health from the total result', () => {
    const wrapper = mount(MonitorCard, { props: { item, window: '7d', availabilityValue: 100, countdownSeconds: 0 } })
    expect(wrapper.text()).toContain('usage.latencyFirstByte 17 ms')
    expect(wrapper.text()).toContain('usage.latencyDuration')
    expect(wrapper.text()).toContain('6000')
    expect(wrapper.text()).toContain('monitorCommon.endpointPing')
    expect(wrapper.text()).toContain('monitorCommon.status.degraded')
    const metrics = wrapper.findComponent({ name: 'MonitorMetricPair' })
    expect(metrics.props('primaryValue')).toBe('6000')
    expect(metrics.props('secondaryValue')).toBe('3')
  })
  it('shows independent bytes and total in timeline and manual results', () => {
    const timeline = mount(MonitorTimeline, { props: { buckets: [{ status: 'degraded', first_byte_ms: 17, latency_ms: 6000, ping_latency_ms: 3, checked_at: '2026-10-12T00:00:00Z' }], countdownSeconds: 0, length: 1 } })
    const title = timeline.get('[title]').attributes('title')
    expect(title).toContain('usage.latencyFirstByte 17ms')
    expect(title).toContain('usage.latencyDuration 6000ms')
    const dialog = mount(MonitorRunResultDialog, {
      props: { show: true, results: [{ model: 'model', status: 'degraded', first_byte_ms: 17, latency_ms: 6000, ping_latency_ms: 3, checked_at: '2026-10-12T00:00:00Z', message: '' }] },
      global: { stubs: { BaseDialog: { template: '<section><slot /></section>' } } },
    })
    expect(dialog.text()).toContain('usage.latencyFirstByte 17 ms')
    expect(dialog.text()).toContain('usage.latencyDuration 6000 ms')
  })
  it('retains missing first bytes in historical timeline entries', () => {
    const wrapper = mount(MonitorTimeline, { props: { buckets: [{ status: 'operational', first_byte_ms: null, latency_ms: 123, ping_latency_ms: null, checked_at: '2026-10-12T00:00:00Z' }], countdownSeconds: 0, length: 1 } })
    const title = wrapper.get('[title]').attributes('title')
    expect(title).toContain('usage.latencyFirstByte monitorCommon.latencyEmpty')
    expect(title).toContain('usage.latencyDuration 123ms')
    expect(title).not.toContain('usage.latencyFirstByte 123ms')
  })
})
