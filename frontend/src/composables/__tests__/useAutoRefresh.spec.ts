import { createApp, h, ref, nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { retryAfterSeconds, useAutoRefresh, type UseAutoRefreshOptions } from '../useAutoRefresh'

const disposers: (() => void)[] = []
function mountRefresh(options: Partial<UseAutoRefreshOptions> = {}) {
  let state!: ReturnType<typeof useAutoRefresh>
  const app = createApp({ setup() { state = useAutoRefresh({ storageKey: 'refresh-test', intervals: [2, 5], defaultInterval: 2, defaultEnabled: true, onRefresh: vi.fn(), ...options }); return () => h('div') } })
  const element = document.createElement('div')
  app.mount(element)
  let mounted = true
  const unmount = () => { if (mounted) { mounted = false; app.unmount() } }
  disposers.push(unmount)
  return { state, unmount }
}
beforeEach(() => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-01-01T00:00:00Z')); localStorage.clear()
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  Object.defineProperty(navigator, 'onLine', { configurable: true, value: true })
})
afterEach(() => { disposers.splice(0).forEach(dispose => dispose()); vi.useRealTimers() })

describe('automatic refresh interaction contract', () => {
  it('reenables a temporary pause on internal-tab reactivation', async () => {
    const active = ref(true), fetch = vi.fn()
    const { state } = mountRefresh({ temporaryPause: true, active: () => active.value, onRefresh: fetch })
    state.setEnabled(false)
    active.value = false; await nextTick()
    active.value = true; await nextTick(); await vi.advanceTimersByTimeAsync(0)
    expect(state.enabled.value).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  // Approved usage-page lifecycle: interval persists; off is temporary.
  it('ignores a saved off state, keeps manual refresh off, and reactivates on foreground return', async () => {
    localStorage.setItem('refresh-test', JSON.stringify({ enabled: false, interval_seconds: 5 }))
    const fetch = vi.fn()
    const { state } = mountRefresh({ temporaryPause: true, onRefresh: fetch })
    expect(state.enabled.value).toBe(true)
    expect(state.intervalSeconds.value).toBe(5)
    state.setEnabled(false)
    await state.refresh()
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(state.enabled.value).toBe(false)
    expect(state.countdown.value).toBe(0)
    await vi.advanceTimersByTimeAsync(15000)
    expect(fetch).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new Event('online'))
    expect(state.enabled.value).toBe(false)
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    expect(state.enabled.value).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(JSON.parse(localStorage.getItem('refresh-test')!)).toEqual({ interval_seconds: 5 })
  })
  it('coalesces manual and automatic refresh while a request is pending', async () => {
    let finish!: () => void
    const fetch = vi.fn(() => new Promise<void>(resolve => { finish = resolve }))
    const { state } = mountRefresh({ onRefresh: fetch })
    const first = state.refresh(); const second = state.refresh()
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(state.fetching.value).toBe(true)
    finish(); await Promise.all([first, second])
    expect(state.fetching.value).toBe(false)
    expect(state.countdown.value).toBe(2)
  })
  it('pauses hidden tabs and synchronizes once when visible', async () => {
    const fetch = vi.fn()
    mountRefresh({ onRefresh: fetch })
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetch).not.toHaveBeenCalled()
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    expect(fetch).toHaveBeenCalledTimes(1)
  })
  it('honors Retry-After even if the user presses refresh again', async () => {
    const fetch = vi.fn().mockRejectedValueOnce({ status: 429, retryAfter: '60' }).mockResolvedValue(undefined)
    const { state } = mountRefresh({ onRefresh: fetch })
    await state.refresh()
    expect(state.error.value).toBeTruthy()
    expect(state.lastUpdated.value).toBeNull()
    await vi.advanceTimersByTimeAsync(59000); await state.refresh()
    expect(fetch).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(state.error.value).toBeNull()
  })
  it('does not poll offline or leave timers after navigation', async () => {
    const fetch = vi.fn()
    const { unmount } = mountRefresh({ onRefresh: fetch })
    Object.defineProperty(navigator, 'onLine', { configurable: true, value: false })
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetch).not.toHaveBeenCalled()
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
  it('parses an HTTP-date retry deadline without shortening it', () => {
    expect(retryAfterSeconds({ retryAfter: 'Thu, 01 Jan 2026 00:01:00 GMT' })).toBe(60)
    expect(retryAfterSeconds({ retryAfter: 'invalid' })).toBeNull()
  })
})
