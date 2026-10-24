const i18nT = vi.fn((key: string, params?: Record<string, unknown>) => {
  const map: Record<string, string> = {
    'channelMonitorV2.matrix.title': '色块矩阵',
    'channelMonitorV2.matrix.description': '趋势色块视图',
    'channelMonitorV2.bucket.minutes': '{count}分钟',
    'channelMonitorV2.matrix.wheelZoomX': '滚轮缩放横向',
    'channelMonitorV2.matrix.resetZoom': '重置缩放',
    'channelMonitorV2.matrix.dimension': '维度',
    'channelMonitorV2.metrics.successRate': '成功率',
    'channelMonitorV2.metrics.ttft': '首 Token',
    'channelMonitorV2.metrics.tps': '每秒 Token',
    'channelMonitorV2.metrics.cacheRate': '缓存率',
    'channelMonitorV2.matrix.scoreLine': '评分 {score}',
    'channelMonitorV2.metrics.successRateValue': '成功率 {value}',
    'channelMonitorV2.metrics.errorRateValue': '错误率 {value}',
    'channelMonitorV2.metrics.rpmValue': 'RPM {value}',
    'channelMonitorV2.metrics.tpmValue': 'TPM {value}',
    'channelMonitorV2.metrics.tpsValue': '每秒 Token {value}',
    'channelMonitorV2.metrics.ttftValue': '首 Token {value}',
    'channelMonitorV2.metrics.durationValue': '时长 {value}',
    'channelMonitorV2.metrics.cacheRateValue': '缓存率 {value}',
    'channelMonitorV2.matrix.noTrafficAt': '{time} 无流量',
    'channelMonitorV2.matrix.noTraffic': '无流量',
    'channelMonitorV2.matrix.legendAria': '图例',
    'channelMonitorV2.matrix.bad': '差',
    'channelMonitorV2.matrix.good': '好',
    'channelMonitorV2.matrix.healthyLegend': '健康',
    'channelMonitorV2.matrix.warningLegend': '警告',
    'channelMonitorV2.matrix.criticalLegend': '异常',
    'channelMonitorV2.matrix.unknownLegend': '未知',
  }
  const template = map[key] || key
  return template.replace(/\{(\w+)\}/g, (_, name) => String(params?.[name] ?? ''))
})

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: i18nT,
      te: (key: string) => key.startsWith('channelMonitorV2.'),
      locale: { value: 'zh' },
    }),
  }
})

import { mount, enableAutoUnmount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RelayPulseMatrix from '../RelayPulseMatrix.vue'
import matrixSource from '../RelayPulseMatrix.vue?raw'
import type { MonitorCoverage, MonitorHealth, MonitorMatrixRow, MonitorMetric } from '@/api/channelMonitorV2'

const health: MonitorHealth = {
  overall: 'warning',
  error_rate: 'warning',
  ttft: 'healthy',
  cache: 'warning',
  score: 52,
  error_rate_score: 40,
  ttft_score: 100,
  cache_score: 50,
  minimum_sample: 20,
}

function metrics(requestCount: number): MonitorMetric {
  return {
    success_requests: requestCount ? requestCount - 1 : 0,
    error_requests: requestCount ? 1 : 0,
    request_count: requestCount,
    token_count: 100,
    rpm: 1.5,
    tpm: 10.2,
    error_rate: requestCount ? 1 / requestCount : 0,
    cache_rate: 0.5,
    cache_rate_numerator: 50,
    cache_rate_denominator: 100,
    ttft: { sample_count: requestCount, p50_ms: 100, p95_ms: 300, avg_ms: 150 },
    duration: { sample_count: requestCount, p50_ms: 500, p95_ms: 900, avg_ms: 600 },
    upstream_affected_requests: 2,
    upstream_attempt_count: 3,
  }
}

enableAutoUnmount(afterEach)
let viewportWidth = 1024
let viewportHeight = 240
let rowHeight = 36
let deferScrollEvents = false
const delayedScrollTargets = new Set<HTMLElement>()
const observers: Array<{ targets: Set<Element>; callback: ResizeObserverCallback }> = []
let matrixStyle: HTMLStyleElement
const rect = (width: number, height: number) => ({ x: 0, y: 0, left: 0, top: 0, right: width, bottom: height, width, height, toJSON() {} })
function size(element: Element) {
  // The fixed header consumes part of the matrix's total viewport height.
  if (element.classList.contains('matrix-scroll')) return rect(viewportWidth, viewportHeight - rowHeight)
  if (element.classList.contains('matrix-table')) return rect(viewportWidth, parseFloat((element as HTMLElement).querySelector<HTMLElement>('.matrix-virtual-body')?.style.height || '0'))
  if (element.classList.contains('matrix-data-row')) return rect(viewportWidth, rowHeight)
  if (element.classList.contains('matrix-header')) return rect(viewportWidth, rowHeight)
  if (element.classList.contains('matrix-floating-tooltip')) return rect(220, 190)
  return rect(30, 16)
}
function notifyResize() {
  for (const observer of observers) {
    const entries = [...observer.targets].map((target) => ({
      target,
      borderBoxSize: [{ inlineSize: size(target).width, blockSize: size(target).height }],
      contentRect: size(target),
    } as unknown as ResizeObserverEntry))
    observer.callback(entries, {} as ResizeObserver)
  }
}
beforeEach(() => {
  // Apply real component geometry so static separation cannot regress to a
  // transparent sticky overlay or a JS-updated vertical clipping mask.
  matrixStyle = document.createElement('style')
  matrixStyle.textContent = matrixSource.match(/<style scoped>([\s\S]*?)<\/style>/)![1]
  document.head.append(matrixStyle)
  viewportWidth = 1024; viewportHeight = 240; rowHeight = 36
  deferScrollEvents = false
  delayedScrollTargets.clear()
  observers.length = 0
  i18nT.mockClear()
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) { return size(this) })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) { return size(this).width })
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) { return size(this).height })
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(function (this: HTMLElement) { return size(this).height })
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(function (this: HTMLElement) { return size(this).width })
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function (this: HTMLElement) {
    if (this.classList.contains('matrix-scroll')) return parseFloat(this.querySelector<HTMLElement>('.matrix-virtual-body')?.style.height || '0')
    return size(this).height
  })
  if (!HTMLElement.prototype.scrollTo) HTMLElement.prototype.scrollTo = () => {}
  vi.stubGlobal('ResizeObserver', class {
    targets = new Set<Element>()
    constructor(public callback: ResizeObserverCallback) { observers.push(this) }
    observe(target: Element) {
      if (this.targets.has(target)) return
      this.targets.add(target)
      queueMicrotask(() => { if (this.targets.has(target)) notifyResize() })
    }
    unobserve(target: Element) { this.targets.delete(target) }
    disconnect() { this.targets.clear() }
  })
  const cancelled = new Set<number>()
  let frame = 0
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    const id = ++frame
    queueMicrotask(() => { if (!cancelled.has(id)) callback(performance.now()) })
    return id
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => { cancelled.add(id) })
  vi.spyOn(HTMLElement.prototype, 'scrollTo').mockImplementation(function (this: HTMLElement, options: ScrollToOptions | number, y?: number) {
    this.scrollTop = typeof options === 'number' ? y || 0 : options.top ?? this.scrollTop
    this.scrollLeft = typeof options === 'number' ? options : options.left ?? this.scrollLeft
    if (deferScrollEvents) delayedScrollTargets.add(this)
    else this.dispatchEvent(new Event('scroll'))
  })
})
afterEach(() => { matrixStyle.remove(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

const coverage: MonitorCoverage = {
  requested_start: '2026-08-01T00:00:00Z', requested_end: '2026-08-01T00:18:00Z',
  coverage_start: '2026-08-01T00:00:00Z', data_through: '2026-08-01T00:18:00Z',
  computed_at: '2026-08-01T00:18:00Z', aggregation_lag_seconds: 0,
  coverage_complete: true, bucket_seconds: 60,
}
function rows(count = 1): MonitorMatrixRow[] {
  return Array.from({ length: count }, (_, index) => ({
    platform: 'openai', group_id: index + 1, group_name: `group-${index + 1}`, model: 'gpt-5',
    metrics: metrics(10), health,
    buckets: [
      { bucket_start: coverage.requested_start, metrics: metrics(10), health },
      { bucket_start: '2026-08-01T00:01:00Z', metrics: metrics(0), health },
    ],
  }))
}
async function settle() { await nextTick(); await flushPromises(); await nextTick() }
function mountMatrix(count = 1, overrides: Record<string, unknown> = {}) {
  return mount(RelayPulseMatrix, { attachTo: document.body, props: {
    rows: rows(count), coverage, healthMode: 'overall', dimensionKey: '90m:platform_group', ...overrides,
  } })
}
async function scroll(wrapper: ReturnType<typeof mountMatrix>, top: number) {
  const element = wrapper.get('.matrix-scroll').element as HTMLElement
  element.scrollTop = top
  await wrapper.get('.matrix-scroll').trigger('scroll')
  await settle()
}
function expectSeparatedHeader(wrapper: ReturnType<typeof mountMatrix>) {
  const header = wrapper.get('.matrix-header').element
  const rail = wrapper.get('.matrix-header-rail').element
  const viewport = wrapper.get('.matrix-scroll').element
  expect(header.closest('.matrix-scroll')).toBeNull()
  expect(rail.nextElementSibling).toBe(viewport)
  expect(rail.parentElement).toBe(viewport.parentElement)
  expect(getComputedStyle(rail).overflow).toBe('hidden')
  expect(wrapper.get('.matrix-scroll').classes()).toContain('overflow-auto')
  expect(wrapper.get('.matrix-header').classes()).not.toContain('sticky')
  expect(['', 'static']).toContain(getComputedStyle(header).position)
  expect(['', 'none']).toContain(getComputedStyle(wrapper.get('.matrix-virtual-body').element).clipPath)
}

describe('RelayPulseMatrix', () => {
  it('renders the actual privacy-safe hover card, colors and summary without per-cell tooltip DOM', async () => {
    const wrapper = mountMatrix()
    await settle()
    const cells = wrapper.findAll('.pulse-cell')
    expect(cells).toHaveLength(18)
    expect(wrapper.find('.pulse-tooltip').exists()).toBe(false)
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await cells[0].trigger('mouseenter', { clientX: 5, clientY: 5 })
    await settle()
    const tooltip = document.querySelector<HTMLElement>('[role="tooltip"]')!
    expect(tooltip.textContent).toContain('90.0%')
    for (const label of ['成功率', '首 Token', '每秒 Token', '缓存率', '错误率', 'RPM', '时长']) {
      expect(tooltip.textContent).toContain(label)
    }
    for (const label of ['请求数', '用户错误', '上游受影响']) expect(tooltip.textContent).not.toContain(label)
    expect(parseFloat(tooltip.style.left)).toBeGreaterThanOrEqual(12)
    expect(parseFloat(tooltip.style.top)).toBeGreaterThanOrEqual(12)
    expect(cells[0].attributes('aria-label')).toContain('90.0%')
    expect(cells[0].classes()).toContain('health-score5')
    expect(cells[1].classes()).toContain('health-score5')
    expect(cells[2].classes()).toContain('health-unknown')
    const header = wrapper.get('.matrix-header')
    for (const label of ['成功率', '首 Token', '每秒 Token', '缓存率']) expect(header.text()).toContain(label)
    expectSeparatedHeader(wrapper)
    await cells[0].trigger('mouseleave')
    await cells[2].trigger('focus')
    await settle()
    expect(document.querySelector('[role="tooltip"]')?.textContent).toContain('无流量')
    await cells[2].trigger('blur')
    expect(document.querySelector('[role="tooltip"]')).toBeNull()
    await cells[0].trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('omits throughput from the real visible tooltip and accessible label when hidden', async () => {
    const wrapper = mountMatrix(1, { showThroughput: false })
    await settle()
    await wrapper.get('.pulse-cell').trigger('focus')
    await settle()
    const tooltip = document.querySelector('[role="tooltip"]')!
    expect(tooltip.textContent).toContain('成功率')
    expect(tooltip.textContent).not.toMatch(/RPM|TPM|每秒 Token/)
    expect(wrapper.get('.pulse-cell').attributes('aria-label')).not.toMatch(/RPM|TPM|每秒 Token/)
    expect(wrapper.get('.matrix-header').text()).not.toContain('每秒 Token')
  })

  it('keeps 417 rows virtual through repeated top, middle and bottom scrolling', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    expect(wrapper.findAll('.matrix-data-row').length).toBeLessThan(25)
    expect(wrapper.findAll('.pulse-cell').length).toBeLessThan(450)
    expect(wrapper.find('[data-index="0"]').exists()).toBe(true)
    expect(wrapper.find('[data-index="416"]').exists()).toBe(false)
    for (const top of [7200, 14900, 0, 7200, 14900, 0]) {
      await scroll(wrapper, top)
      expect(wrapper.findAll('.matrix-data-row').length).toBeGreaterThan(5)
      expect(wrapper.findAll('.matrix-data-row').length).toBeLessThan(25)
      const expected = top === 0 ? 0 : top === 7200 ? 200 : 416
      expect(wrapper.find(`[data-index="${expected}"]`).exists()).toBe(true)
    }
  })

  it('keeps transparent headers outside the body scrollport without a vertical paint mask or changed virtual geometry', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const body = wrapper.get('.matrix-virtual-body').element as HTMLElement
    const bodyHeight = body.style.height
    const observerCount = observers.length
    for (const top of [0, 4, 16, 30, 120, 1229, 0]) {
      await scroll(wrapper, top)
      // Native overflow clips the body in a separate box; fast compositor
      // scrolling cannot paint rows under the header between JS events.
      expectSeparatedHeader(wrapper)
      expect(body.style.height).toBe(bodyHeight)
      expect(wrapper.findAll('.matrix-data-row').length).toBeLessThan(25)
    }
    expect(observers).toHaveLength(observerCount)
    const first = wrapper.get('[data-index="0"] [data-column="0"]')
    ;(first.element as HTMLElement).focus()
    await first.trigger('keydown', { key: 'ArrowDown' })
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="1"] [data-column="0"]').element)
  })

  it('releases detached row nodes from the virtualizer cache after repeated scrolling', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const virtualizer = (wrapper.vm as unknown as {
      $: { setupState: { rowVirtualizer: { elementsCache: Map<string, Element> } } }
    }).$.setupState.rowVirtualizer
    for (const top of [7200, 14900, 0, 7200, 14900, 0]) {
      await scroll(wrapper, top)
      const cached = [...virtualizer.elementsCache.values()]
      expect(cached.length).toBeGreaterThan(0)
      expect(cached.length).toBeLessThan(25)
      expect(cached.every(element => element.isConnected)).toBe(true)
    }
  })

  it('batches tooltip moves without rerendering matrix cells or constructing Intl formatters', async () => {
    const DateFormatter = Intl.DateTimeFormat
    const NumberFormatter = Intl.NumberFormat
    const dateFormats = vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function (locales, options) { return new DateFormatter(locales, options) })
    const numberFormats = vi.spyOn(Intl, 'NumberFormat').mockImplementation(function (locales, options) { return new NumberFormatter(locales, options) })
    const wrapper = mountMatrix(417)
    await settle()
    expect(dateFormats.mock.calls.length).toBeLessThanOrEqual(2)
    expect(numberFormats.mock.calls.length).toBeLessThanOrEqual(5)
    const cell = wrapper.get('.pulse-cell')
    await cell.trigger('mouseenter', { clientX: 50, clientY: 300 })
    await settle()
    const translations = i18nT.mock.calls.length
    const dates = dateFormats.mock.calls.length
    const numbers = numberFormats.mock.calls.length
    for (let i = 0; i < 30; i++) cell.element.dispatchEvent(new MouseEvent('mousemove', { clientX: 100 + i, clientY: 300 }))
    await settle()
    expect(i18nT).toHaveBeenCalledTimes(translations)
    expect(dateFormats).toHaveBeenCalledTimes(dates)
    expect(numberFormats).toHaveBeenCalledTimes(numbers)
    expect(document.querySelectorAll('[role="tooltip"]')).toHaveLength(1)
    expect(document.querySelector<HTMLElement>('[role="tooltip"]')?.style.left).toBe('19px')
  })

  it('allows ordinary wheel scrolling, uses Ctrl for zoom and preserves zoom across ordinary refreshes', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const viewport = wrapper.get('.matrix-scroll')
    const plain = new WheelEvent('wheel', { deltaY: -100, cancelable: true, bubbles: true })
    viewport.element.dispatchEvent(plain)
    await settle()
    expect(plain.defaultPrevented).toBe(false)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    const zoom = new WheelEvent('wheel', { deltaY: -100, ctrlKey: true, cancelable: true, bubbles: true })
    viewport.element.dispatchEvent(zoom)
    await settle()
    expect(zoom.defaultPrevented).toBe(true)
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    const count = wrapper.get('.matrix-data-row').findAll('.pulse-cell').length
    await scroll(wrapper, 7200)
    await wrapper.setProps({ rows: rows(417), coverage: { ...coverage,
      data_through: '2026-08-01T00:19:00Z', computed_at: '2026-08-01T00:19:00Z',
      requested_start: '2026-08-01T00:00:10Z', requested_end: '2026-08-01T00:18:10Z',
    } })
    await settle()
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('.matrix-data-row').findAll('.pulse-cell').length).toBeLessThanOrEqual(count + 1)
    expect((viewport.element as HTMLElement).scrollTop).toBe(7200)
    await wrapper.setProps({ dimensionKey: '24h:platform_group' })
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })

  it('retains and clamps scroll after filtering and restores it across view reconstruction', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    await scroll(wrapper, 7200)
    await wrapper.setProps({ rows: rows(417).slice(150) })
    await settle()
    expect((wrapper.get('.matrix-scroll').element as HTMLElement).scrollTop).toBe(7200)
    expectSeparatedHeader(wrapper)
    await wrapper.setProps({ rows: rows(12) })
    await settle()
    const top = (wrapper.get('.matrix-scroll').element as HTMLElement).scrollTop
    // Twelve 36px records fill the 204px body below a 36px static header.
    expect(top).toBe(228)
    expectSeparatedHeader(wrapper)
    const viewState = wrapper.emitted('update:viewState')!.at(-1)![0]
    wrapper.unmount()
    const rebuilt = mountMatrix(12, { viewState })
    await settle()
    expect((rebuilt.get('.matrix-scroll').element as HTMLElement).scrollTop).toBe(top)
    expectSeparatedHeader(rebuilt)
    expect(rebuilt.findAll('.matrix-data-row').length).toBeGreaterThan(0)
  })

  it('aligns header and body horizontal scroll in both directions and restores the same view', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const viewport = wrapper.get('.matrix-scroll')
    viewport.element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, ctrlKey: true, cancelable: true, bubbles: true }))
    await settle()
    await scroll(wrapper, 7200)
    ;(viewport.element as HTMLElement).scrollLeft = 128
    await viewport.trigger('scroll')
    const rail = wrapper.get('.matrix-header-rail')
    expect((rail.element as HTMLElement).scrollLeft).toBe(128)
    expect(wrapper.emitted('update:viewState')!.at(-1)![0]).toMatchObject({ scrollTop: 7200, scrollLeft: 128 })

    // Programmatic focus/scrolling of the static rail must keep body columns
    // aligned too, without changing its vertical reading position.
    ;(rail.element as HTMLElement).scrollLeft = 64
    await rail.trigger('scroll')
    expect((viewport.element as HTMLElement).scrollLeft).toBe(64)
    expect((viewport.element as HTMLElement).scrollTop).toBe(7200)
    const viewState = wrapper.emitted('update:viewState')!.at(-1)![0]
    wrapper.unmount()
    const rebuilt = mountMatrix(417, { viewState })
    await settle()
    expect((rebuilt.get('.matrix-scroll').element as HTMLElement).scrollLeft).toBe(64)
    expect((rebuilt.get('.matrix-header-rail').element as HTMLElement).scrollLeft).toBe(64)
    expect((rebuilt.get('.matrix-scroll').element as HTMLElement).scrollTop).toBe(7200)
    expectSeparatedHeader(rebuilt)
  })

  it('measures mobile two-line rows and remeasures resize without blanks or unreachable bottom rows', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    viewportWidth = 390; rowHeight = 52
    notifyResize()
    await settle()
    expect(wrapper.findAll('.matrix-data-row').length).toBeLessThan(20)
    const first = wrapper.get('[data-index="0"]')
    const second = wrapper.get('[data-index="1"]')
    expect(first.attributes('style')).toContain('translateY(0px)')
    expect(second.attributes('style')).toContain('translateY(52px)')
    await scroll(wrapper, 417 * 48 - viewportHeight)
    expect(wrapper.find('[data-index="416"]').exists()).toBe(true)
    viewportWidth = 1024; rowHeight = 36
    notifyResize()
    await settle()
    expect(wrapper.findAll('.matrix-data-row').length).toBeGreaterThan(0)
    await scroll(wrapper, 0)
    expect(wrapper.find('[data-index="0"]').exists()).toBe(true)
  })

  it('keeps keyboard focus mounted, reaches both ends and tabs between unmounted rows', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const first = wrapper.get('[data-index="0"] [data-column="0"]')
    ;(first.element as HTMLElement).focus()
    await settle()
    await scroll(wrapper, 7200)
    expect(document.activeElement).toBe(first.element)
    expect(wrapper.find('[data-index="0"]').exists()).toBe(true)
    await first.trigger('keydown', { key: 'End', ctrlKey: true })
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="416"] [data-column="17"]').element)
    await wrapper.get('[data-index="416"] [data-column="17"]').trigger('keydown', { key: 'Home', ctrlKey: true })
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="0"] [data-column="0"]').element)
    await wrapper.get('[data-index="0"] [data-column="0"]').trigger('keydown', { key: 'PageDown' })
    await settle()
    const lastCell = wrapper.get('[data-index="5"] [data-column="17"]')
    ;(lastCell.element as HTMLElement).focus()
    await lastCell.trigger('keydown', { key: 'Tab' })
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="6"] [data-column="0"]').element)
    await wrapper.setProps({ rows: rows(3) })
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="2"] [data-column="0"]').element)
  })

  it('transfers focus across virtual rows before delayed native scroll events, including a pinned source outside the viewport', async () => {
    // Browser rAF yields to event tasks; an immediate microtask rAF would spin the
    // virtualizer's scroll reconciliation while this test intentionally holds scroll.
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => window.setTimeout(() => callback(performance.now()), 16))
    vi.stubGlobal('cancelAnimationFrame', (frame: number) => window.clearTimeout(frame))
    const wrapper = mountMatrix(417)
    await settle()
    const first = wrapper.get('[data-index="0"] [data-column="0"]')
    ;(first.element as HTMLElement).focus()
    await settle()
    deferScrollEvents = true
    expect(wrapper.find('[data-index="416"]').exists()).toBe(false)
    await first.trigger('keydown', { key: 'End', ctrlKey: true })
    await settle()
    // Browser scrolling has not notified the virtualizer yet. The requested row
    // must mount and receive focus solely from the pending keyboard request.
    expect(delayedScrollTargets.size).toBeGreaterThan(0)
    const last = wrapper.get('[data-index="416"] [data-column="17"]')
    expect(document.activeElement).toBe(last.element)
    for (const target of delayedScrollTargets) target.dispatchEvent(new Event('scroll'))
    delayedScrollTargets.clear()
    await settle()
    expect(document.activeElement).toBe(last.element)

    // Pointer-scroll away without moving keyboard focus, then navigate from the
    // retained source. It must survive until its offscreen destination is focused.
    await scroll(wrapper, 7200)
    expect(document.activeElement).toBe(last.element)
    expect(wrapper.find('[data-index="0"]').exists()).toBe(false)
    await last.trigger('keydown', { key: 'Home', ctrlKey: true })
    await settle()
    const restored = wrapper.get('[data-index="0"] [data-column="0"]')
    expect(document.activeElement).toBe(restored.element)
    expect(delayedScrollTargets.size).toBeGreaterThan(0)
    for (const target of delayedScrollTargets) target.dispatchEvent(new Event('scroll'))
    delayedScrollTargets.clear()
    await settle()
    expect(document.activeElement).toBe(restored.element)
    expect(wrapper.findAll('.matrix-data-row').length).toBeLessThan(25)
  })

  it('cancels obsolete pending focus when rows disappear and never revives it after an empty result or unmount', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const first = wrapper.get('[data-index="0"] [data-column="0"]')
    ;(first.element as HTMLElement).focus()
    first.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', ctrlKey: true, bubbles: true, cancelable: true }))
    // Change the data before Vue can mount the pending destination.
    await wrapper.setProps({ rows: rows(3) })
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="0"] [data-column="0"]').element)
    expect(wrapper.find('[data-index="416"]').exists()).toBe(false)
    await wrapper.setProps({ rows: [] })
    await settle()
    expect(document.activeElement).toBe(document.body)
    await wrapper.setProps({ rows: rows(417) })
    await settle()
    expect(document.activeElement).toBe(document.body)
    const restored = wrapper.get('[data-index="0"] [data-column="0"]')
    ;(restored.element as HTMLElement).focus()
    restored.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', ctrlKey: true, bubbles: true, cancelable: true }))
    wrapper.unmount()
    await settle()
    expect(document.activeElement).toBe(document.body)
  })

  it('keeps pending focus attached to the requested row key when refreshed rows reorder', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const first = wrapper.get('[data-index="0"] [data-column="0"]')
    ;(first.element as HTMLElement).focus()
    first.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', ctrlKey: true, bubbles: true, cancelable: true }))
    await wrapper.setProps({ rows: rows(417).reverse() })
    await settle()
    const cell = wrapper.get('[data-index="0"] [data-column="17"]')
    expect(cell.element.closest('[data-row-key]')?.getAttribute('data-row-key')).toBe('openai:417:gpt-5')
    expect(document.activeElement).toBe(cell.element)
  })

  it('clamps pending and current keyboard focus when Ctrl wheel changes visible buckets', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    const first = wrapper.get('[data-index="0"] [data-column="17"]')
    ;(first.element as HTMLElement).focus()
    first.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', ctrlKey: true, bubbles: true, cancelable: true }))
    wrapper.get('.matrix-scroll').element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, ctrlKey: true, clientX: 0, bubbles: true, cancelable: true }))
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="416"] [data-column="15"]').element)
    // Zooming again while the end cell is already focused must also retain focus.
    wrapper.get('.matrix-scroll').element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, ctrlKey: true, clientX: 0, bubbles: true, cancelable: true }))
    await settle()
    expect(document.activeElement).toBe(wrapper.get('[data-index="416"] [data-column="13"]').element)
  })

  it('reconnects measurement and restores a nonempty viewport after an empty filter result', async () => {
    const wrapper = mountMatrix(417)
    await settle()
    await scroll(wrapper, 7200)
    await wrapper.setProps({ rows: [] })
    await settle()
    expect(wrapper.find('.matrix-scroll').exists()).toBe(false)
    await wrapper.setProps({ rows: rows(417) })
    await settle()
    expect(wrapper.findAll('.matrix-data-row').length).toBeGreaterThan(0)
    expect(wrapper.findAll('.matrix-data-row').length).toBeLessThan(25)
    await scroll(wrapper, 14900)
    expect(wrapper.find('[data-index="416"]').exists()).toBe(true)
  })

  it('spans the requested axis with empty cells during partial backfill', async () => {
    const wrapper = mountMatrix(1, { coverage: { ...coverage,
      requested_end: '2026-08-01T00:05:00Z', coverage_start: '2026-08-01T00:02:00Z',
      data_through: '2026-08-01T00:03:00Z', coverage_complete: false,
    } })
    await settle()
    expect(wrapper.findAll('.pulse-cell')).toHaveLength(5)
  })
})
