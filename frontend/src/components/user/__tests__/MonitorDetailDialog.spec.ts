import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { cssPixels, installAppStyles } from '@/__tests__/appStyles'
import MonitorDetailDialog from '../MonitorDetailDialog.vue'

enableAutoUnmount(afterEach)
const { status } = vi.hoisted(() => ({ status: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ status }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { n: number }) => params ? `${key}:${params.n}` : key }) }))

describe('channel model detail table', () => {
  let removeStyles: () => void
  // Compile the real stylesheet within the container's bounded CPU budget.
  beforeAll(async () => { removeStyles = await installAppStyles() }, 30_000)
  afterAll(() => removeStyles?.())

  it('insets model names from the border and aligns every heading with its values', async () => {
    status.mockResolvedValue({ models: [{
      model: 'model-long-name', latest_status: 'up', latest_latency_ms: 123,
      availability_7d: 0.99, availability_15d: 0.95, availability_30d: 0.9,
      avg_latency_7d_ms: 150, latest_first_byte_ms: 17, latest_first_text_ms: 0,
      avg_first_text_7d_ms: 80, first_text_samples_7d: 2,
      avg_first_byte_7d_ms: 40, first_byte_samples_7d: 3,
    }] })
    const wrapper = mount(MonitorDetailDialog, {
      props: { show: true, monitorId: 7, title: 'Channel details' },
      attachTo: document.body,
      global: { stubs: { BaseDialog: { template: '<section><slot /></section>' } } },
    })
    await flushPromises()
    expect(status).toHaveBeenCalledWith(7)
    const headers = wrapper.findAll('thead th')
    const cells = wrapper.findAll('tbody tr td')
    // The timing contract exposes independent first-body-byte and total duration columns.
    expect(headers).toHaveLength(11)
    expect(cells).toHaveLength(11)
    expect(cells[0].text()).toBe('model-long-name')
    expect(headers[2].text()).toBe('monitorCommon.latestFirstText')
    expect(headers[3].text()).toBe('monitorCommon.latestFirstByte')
    expect(headers[4].text()).toBe('usage.latestTotalDuration')
    expect(cells[2].text()).toBe('0')
    expect(cells[3].text()).toBe('17')
    expect(cells[4].text()).toBe('123')
    expect(cells[8].text()).toContain('80')
    expect(cells[8].text()).toContain('monitorCommon.textSamples:2')
    expect(cells[9].text()).toContain('40')
    expect(cells[9].text()).toContain('usage.firstByteSamples:3')
    expect(cells[10].text()).toBe('150')
    cells.forEach((cell, index) => {
      const valueStyle = getComputedStyle(cell.element)
      const headerStyle = getComputedStyle(headers[index].element)
      for (const side of ['paddingLeft', 'paddingRight'] as const) {
        expect(cssPixels(valueStyle[side])).toBeGreaterThanOrEqual(12)
        expect(cssPixels(headerStyle[side])).toBe(cssPixels(valueStyle[side]))
      }
    })
  })
})
