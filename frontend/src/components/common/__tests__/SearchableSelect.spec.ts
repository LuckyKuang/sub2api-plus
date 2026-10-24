import { DOMWrapper, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../SearchableSelect.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
const originalInnerHeight = window.innerHeight
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const setViewportHeight = (height: number) => {
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: height })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async () => {
  const wrapper = mount(Select, {
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await nextTick()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  setViewportHeight(originalInnerHeight)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('preserves the existing 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('aligns leftward near the right edge so compact controls keep readable menus', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('112px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('200px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })

  it('keeps an offscreen-right trigger menu inside the viewport with readable options', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('112px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('200px')
  })

  const mockVerticalLayout = () => {
    let top = 131
    let bottom = 171
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      x: 200, y: top, top, right: 344, bottom, left: 200, width: 144, height: bottom - top,
      toJSON: () => ({}),
    }))
    const chromeHeight = (panel: HTMLElement) => panel.querySelector('.select-search') ? 39 : 2
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
      if (this.classList.contains('select-dropdown-portal')) {
        const limit = Number.parseFloat(this.style.maxHeight)
        return Math.min(320 + chromeHeight(this), Number.isFinite(limit) ? limit : Infinity)
      }
      if (this.classList.contains('select-options')) {
        const panel = this.parentElement!
        return Math.max(0, panel.offsetHeight - chromeHeight(panel))
      }
      return this.getAttribute('role') === 'option' ? 28 : 40
    })
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function (this: HTMLElement) {
      return this.classList.contains('select-options') ? 320 : this.offsetHeight
    })
    vi.spyOn(HTMLElement.prototype, 'offsetTop', 'get').mockImplementation(function (this: HTMLElement) {
      return this.getAttribute('role') === 'option'
        ? 6 + Array.from(this.parentElement!.children).indexOf(this) * 28
        : 0
    })
    return (nextTop: number, nextBottom: number) => { top = nextTop; bottom = nextBottom }
  }

  const mountLongSelect = async (searchable = true) => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: {
        modelValue: 0,
        searchable,
        options: Array.from({ length: 11 }, (_, value) => ({ value, label: `Status ${value}` })),
      },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('.select-trigger').trigger('click')
    await nextTick()
    return wrapper
  }

  it.each([true, false])('contains all eleven options in a 480px viewport and reaches the last by keyboard (searchable=%s)', async (searchable) => {
    setViewportWidth(1024)
    setViewportHeight(480)
    mockVerticalLayout()
    const wrapper = await mountLongSelect(searchable)
    const panel = document.querySelector<HTMLElement>('[role="listbox"]')!
    const list = panel.querySelector<HTMLElement>('.select-options')!
    expect(panel.style.top).toBe('175px')
    expect(Number.parseFloat(panel.style.maxHeight)).toBe(297)
    expect(Number.parseFloat(panel.style.top) + panel.offsetHeight).toBeLessThanOrEqual(472)
    expect(panel.querySelectorAll('[role="option"]')).toHaveLength(11)
    const search = panel.querySelector<HTMLInputElement>('.select-search-input')
    expect(!!search).toBe(searchable)
    if (search) expect(document.activeElement).toBe(search)
    const keyboardTarget = search ? new DOMWrapper(search) : wrapper.get('.select-trigger')
    await keyboardTarget.trigger('keydown', { key: 'ArrowUp' })
    const last = list.querySelectorAll<HTMLElement>('[role="option"]')[10]!
    expect(list.scrollTop).toBeGreaterThan(0)
    expect(last.offsetTop + last.offsetHeight).toBeLessThanOrEqual(list.scrollTop + list.offsetHeight)
    expect(last.classList).toContain('select-option-focused')
    await keyboardTarget.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toEqual([[10]])
    expect(document.activeElement).toBe(wrapper.get('.select-trigger').element)
  })

  it('uses the larger space above when neither side can fit the full searchable menu, even after it has shrunk', async () => {
    setViewportHeight(480)
    const moveTrigger = mockVerticalLayout()
    moveTrigger(340, 380)
    await mountLongSelect()
    const panel = document.querySelector<HTMLElement>('[role="listbox"]')!
    expect(panel.style.top).toBe('')
    expect(panel.style.bottom).toBe('144px')
    expect(Number.parseFloat(panel.style.maxHeight)).toBe(328)
    expect(480 - Number.parseFloat(panel.style.bottom) - panel.offsetHeight).toBeGreaterThanOrEqual(8)
    // Another measurement must not mistake the already constrained menu for its full height.
    window.dispatchEvent(new Event('scroll'))
    await nextTick(); await nextTick()
    expect(panel.style.bottom).toBe('144px')
    expect(panel.style.maxHeight).toBe('328px')
  })

  it('recomputes the viewport bounds on resize and the opening side when the trigger scrolls', async () => {
    setViewportHeight(700)
    const moveTrigger = mockVerticalLayout()
    await mountLongSelect()
    const panel = document.querySelector<HTMLElement>('[role="listbox"]')!
    expect(panel.style.top).toBe('175px')
    expect(panel.offsetHeight).toBe(359)
    setViewportHeight(480)
    window.dispatchEvent(new Event('resize'))
    await nextTick(); await nextTick()
    expect(panel.style.maxHeight).toBe('297px')
    expect(Number.parseFloat(panel.style.top) + panel.offsetHeight).toBeLessThanOrEqual(472)
    moveTrigger(340, 380)
    window.dispatchEvent(new Event('scroll'))
    await nextTick(); await nextTick()
    expect(panel.style.top).toBe('')
    expect(panel.style.bottom).toBe('144px')
    expect(panel.style.maxHeight).toBe('328px')
  })
})

describe('Select remote search', () => {
  const mountRemoteSelect = (props: Record<string, unknown> = {}) => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        remote: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
        ...props,
      },
    })
    unmountWrapper = () => wrapper.unmount()
    return wrapper
  }

  const openDropdown = async () => {
    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    return dropdown as HTMLElement
  }

  const typeSearchQuery = async (query: string) => {
    const dropdown = await openDropdown()
    const input = dropdown.querySelector<HTMLInputElement>('.select-search-input')
    expect(input).not.toBeNull()
    input!.value = query
    input!.dispatchEvent(new Event('input'))
    await nextTick()
  }

  it('emits debounced search events and skips local filtering in remote mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('zzz')

    // 防抖窗口内不触发。
    expect(wrapper.emitted('search')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toEqual([['zzz']])
    // 远程模式不做本地过滤：无命中的 query 下选项仍完整展示（由父组件更新 options）。
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toContain('Alpha account')
    expect(labels).toContain('Beta account')
  })

  it('does not emit search when the dropdown closes and the query resets', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('hidden')

    // 关闭下拉：排队中的防抖定时器应被取消，也不应因 query 重置而尾随 emit。
    await wrapper.get('button').trigger('click')
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('shows the loading text instead of empty text while loading with no options', async () => {
    const wrapper = mountRemoteSelect({ options: [], loading: true })
    await wrapper.get('button').trigger('click')
    await nextTick()

    const dropdown = await openDropdown()
    expect(dropdown.querySelector('.select-empty')?.textContent).toContain('common.loading')
  })

  it('closes the search popup with Escape without closing its parent dialog', async () => {
    const wrapper = mount(Select, {
      attachTo: document.body,
      props: { modelValue: null, searchable: true, options: [{ value: 1, label: 'Group' }] },
    })
    unmountWrapper = () => wrapper.unmount()
    const parentEscape = vi.fn()
    document.addEventListener('keydown', parentEscape)
    try {
      await wrapper.get('button').trigger('click')
      await nextTick()
      const dropdown = await openDropdown()
      dropdown.querySelector('input')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
      await nextTick()
      expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
      expect(document.activeElement).toBe(wrapper.get('button').element)
      expect(parentEscape).not.toHaveBeenCalled()
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    } finally {
      document.removeEventListener('keydown', parentEscape)
    }
  })

  it('keeps local filtering and emits nothing when remote is not set', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        searchable: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('alpha')
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toEqual(['Alpha account'])
  })
})
