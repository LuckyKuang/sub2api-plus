import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { useQuoteRefresh } from '../useQuoteRefresh'

describe('current quote refresh lifecycle', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('refreshes on focus and every visible minute without overlapping requests, then removes its listeners', async () => {
    let finish!: () => void
    const refresh = vi.fn().mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve })).mockResolvedValue(undefined)
    const wrapper = mount(defineComponent({ setup() { useQuoteRefresh(refresh); return () => null } }))
    window.dispatchEvent(new Event('focus'))
    expect(refresh).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new Event('focus'))
    vi.advanceTimersByTime(60_000)
    expect(refresh).toHaveBeenCalledTimes(1)
    finish()
    await flushPromises()
    vi.advanceTimersByTime(60_000)
    await flushPromises()
    expect(refresh).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    window.dispatchEvent(new Event('focus'))
    document.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(120_000)
    expect(refresh).toHaveBeenCalledTimes(2)
  })

  it('waits while hidden and can retry a failed refresh when the page is visible again', async () => {
    const visible = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    const refresh = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue(undefined)
    const wrapper = mount(defineComponent({ setup() { useQuoteRefresh(refresh); return () => null } }))
    window.dispatchEvent(new Event('focus'))
    vi.advanceTimersByTime(60_000)
    expect(refresh).not.toHaveBeenCalled()
    visible.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(refresh).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(refresh).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
