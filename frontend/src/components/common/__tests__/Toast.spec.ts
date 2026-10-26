import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import postcss from 'postcss'
import Toast from '../Toast.vue'
import source from '../Toast.vue?raw'
import { useAppStore } from '@/stores/app'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key === 'common.close' ? 'Close' : key }) }
})
vi.mock('@/api/admin/system', () => ({ checkUpdates: vi.fn() }))
vi.mock('@/api/auth', () => ({ getPublicSettings: vi.fn() }))

let wrapper: VueWrapper | undefined
const cards = () => [...document.querySelectorAll<HTMLElement>('.toast-card')]
const styles = postcss.parse(source.match(/<style scoped>([\s\S]*?)<\/style>/)?.[1] ?? '')
function declarations(selector: string) {
  const values: Record<string, string> = {}
  styles.walkRules(rule => {
    if (!rule.selectors.includes(selector)) return
    rule.walkDecls(declaration => { values[declaration.prop] = declaration.value })
  })
  return values
}
function start() {
  wrapper = mount(Toast, {
    attachTo: document.body,
    // The shared setup clears Vue Test Utils' transition stubs. Keep CSS
    // animation frames outside dismissal/timer assertions in jsdom.
    global: { stubs: { TransitionGroup: true } }
  })
  return useAppStore()
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.clearAllTimers()
  vi.useRealTimers()
})

describe('toast cards', () => {
  // User-approved notification contract: all severities share the same opaque,
  // rounded shape; only the icon tile and progress color express severity.
  it('renders all severities with the same card structure and preserves polite announcements', async () => {
    const store = start()
    for (const type of ['success', 'error', 'warning', 'info'] as const) store.showToast(type, `${type} message`, 4000)
    await nextTick()
    expect(cards()).toHaveLength(4)
    expect(cards().map(card => card.getAttribute('data-toast-type'))).toEqual(['success', 'error', 'warning', 'info'])
    expect(new Set(cards().map(card => card.className)).size).toBe(1)
    expect(cards().every(card => card.querySelector('.toast-icon') && card.querySelector('.toast-progress-track'))).toBe(true)
    expect(document.querySelector('[aria-live="polite"]')?.getAttribute('aria-atomic')).toBe('true')
    expect(declarations('.toast-card')).toMatchObject({ 'border-radius': '20px', 'background-color': 'var(--surface-panel)', color: 'var(--surface-text)' })
    expect(declarations('.toast-icon')).toMatchObject({ width: '36px', height: '36px', 'border-radius': '12px' })
    expect(declarations('.toast-close')['border-radius']).toBe('10px')
    expect(declarations('.toast-progress-track')).toMatchObject({ left: '16px', right: '16px', height: '3px', 'border-radius': '9999px' })
    expect(cards().every(card => !card.className.includes('border-l-'))).toBe(true)
  })

  it('keeps long business messages and titles complete and wraps them inside a phone-sized card', async () => {
    const store = start()
    const longMessage = `Upstream request failed\n${'request-id-without-spaces-'.repeat(20)}`
    const id = store.showToast('error', longMessage)
    store.toasts.find(toast => toast.id === id)!.title = 'Connection details'
    await nextTick()
    expect(cards()[0].querySelector('.toast-title')?.textContent?.trim()).toBe('Connection details')
    expect(cards()[0].querySelector('.toast-message')?.textContent?.trim()).toBe(longMessage)
    // CSS is the layout boundary in jsdom; viewport sizing is verified against
    // the approved 376px cap and 12px phone gutters, independent of the DOM text.
    expect(declarations('.toast-stack').width).toBe('min(376px, calc(100vw - 24px))')
    expect(declarations('.toast-stack').right).toContain('var(--shell-gutter, 12px)')
    expect(declarations('.toast-stack')['max-width']).toContain('safe-area-inset-left')
    expect(declarations('.toast-title')).toMatchObject({ 'overflow-wrap': 'anywhere', 'white-space': 'pre-line', color: 'inherit' })
    expect(cards()[0].querySelector('.toast-progress-track')).toBeNull()
  })

  it('closes only the selected notification and preserves the other notifications and their timers', async () => {
    const store = start()
    const persistentId = store.showToast('info', 'Keep this message')
    const timedId = store.showToast('warning', 'Timed message', 2000)
    await nextTick()
    const timedCard = cards().find(card => card.dataset.toastId === timedId)!
    const progress = timedCard.querySelector<HTMLElement>('.toast-progress')!
    expect(progress.style.animationDuration).toBe('2000ms')
    const close = cards().find(card => card.dataset.toastId === persistentId)!.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!
    expect(close.type).toBe('button')
    close.click()
    await nextTick()
    expect(store.toasts.map(toast => toast.id)).toEqual([timedId])
    expect(cards()).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1999)
    expect(store.toasts.map(toast => toast.id)).toEqual([timedId])
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(store.toasts).toHaveLength(0)
    expect(cards()).toHaveLength(0)
  })

  it('retains notifications without a duration until explicitly dismissed', async () => {
    const store = start()
    const id = store.showToast('error', 'Requires manual dismissal')
    await nextTick()
    await vi.advanceTimersByTimeAsync(60000)
    expect(store.toasts.map(toast => toast.id)).toEqual([id])
    expect(cards()).toHaveLength(1)
    expect(cards()[0].querySelector('.toast-progress-track')).toBeNull()
  })
})
