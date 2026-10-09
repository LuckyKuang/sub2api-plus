import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { cssPixels, installAppStyles } from '@/__tests__/appStyles'
import MonitorDetailDialog from '../MonitorDetailDialog.vue'

enableAutoUnmount(afterEach)
const { status } = vi.hoisted(() => ({ status: vi.fn() }))
vi.mock('@/api/channelMonitor', () => ({ status }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('channel model detail table', () => {
  let removeStyles: () => void
  // Compile the real stylesheet within the container's bounded CPU budget.
  beforeAll(async () => { removeStyles = await installAppStyles() }, 30_000)
  afterAll(() => removeStyles?.())

  it('insets model names from the border and aligns every heading with its values', async () => {
    status.mockResolvedValue({ models: [{
      model: 'model-long-name', latest_status: 'up', latest_latency_ms: 123,
      availability_7d: 0.99, availability_15d: 0.95, availability_30d: 0.9,
      avg_latency_7d_ms: 150,
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
    expect(headers).toHaveLength(7)
    expect(cells).toHaveLength(7)
    expect(cells[0].text()).toBe('model-long-name')
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
