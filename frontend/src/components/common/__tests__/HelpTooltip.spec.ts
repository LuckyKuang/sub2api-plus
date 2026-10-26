import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

function getTooltipElement(): HTMLDivElement {
  const tooltip = document.body.querySelector('[role="tooltip"]')
  if (!(tooltip instanceof HTMLDivElement)) {
    throw new Error('tooltip element not found')
  }
  return tooltip
}

describe('HelpTooltip', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('keeps the existing hover interaction by default', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'hover details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave')
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('keeps a hover tooltip open while the pointer moves between the trigger and the tooltip', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'copyable details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave', { relatedTarget: tooltip })
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    tooltip.dispatchEvent(new MouseEvent('mouseleave', { relatedTarget: trigger.element }))
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    tooltip.dispatchEvent(new MouseEvent('mouseleave', { relatedTarget: null }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('supports click-to-toggle details and closes on outside click', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'click details',
        trigger: 'click',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('click details')

    const closeButton = tooltip.querySelector('button[aria-label="Close"]')
    if (!(closeButton instanceof HTMLButtonElement)) {
      throw new Error('close button not found')
    }
    closeButton.click()
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  // Rounded hover cards must also be reachable without a mouse and dismiss
  // before an enclosing dialog handles Escape (user's full-site card audit).
  it('names the closed default help trigger even when details are provided by a rich slot', () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: { trigger: 'click' },
      slots: { default: '<p>Rich account scheduling details</p>' },
    })
    try {
      const trigger = wrapper.get('.help-tooltip-trigger')
      expect(trigger.attributes('role')).toBe('button')
      expect(trigger.attributes('aria-label')).toBe('common.info')
      expect(trigger.attributes('aria-expanded')).toBe('false')
    } finally {
      wrapper.unmount()
    }
  })

  it('opens on keyboard focus, describes its trigger and dismisses on Escape', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { content: 'keyboard details' } })
    const parentEscape = vi.fn()
    document.addEventListener('keydown', parentEscape)
    try {
      const trigger = wrapper.get<HTMLElement>('.help-tooltip-trigger')
      const tooltip = getTooltipElement()

      expect(trigger.attributes('tabindex')).toBe('0')
      trigger.element.focus()
      await nextTick()
      expect(tooltip.style.display).not.toBe('none')
      expect(trigger.attributes('aria-describedby')).toBe(tooltip.id)
      expect(tooltip.querySelector('.rotate-45')).toBeNull()

      await trigger.trigger('mouseleave')
      expect(tooltip.style.display).not.toBe('none')
      document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
      await nextTick()
      expect(tooltip.style.display).toBe('none')
      expect(trigger.attributes('aria-describedby')).toBeUndefined()
      expect(parentEscape).not.toHaveBeenCalled()
    } finally {
      document.removeEventListener('keydown', parentEscape)
      wrapper.unmount()
    }
  })

  it('closes after keyboard focus leaves the trigger and panel', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { content: 'focus details' } })
    try {
      const trigger = wrapper.get<HTMLElement>('.help-tooltip-trigger')
      trigger.element.focus()
      await nextTick()
      trigger.element.blur()
      await nextTick()
      expect(getTooltipElement().style.display).toBe('none')
    } finally {
      wrapper.unmount()
    }
  })

  it('supports keyboard toggling for a click trigger without an interactive slot', async () => {
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { content: 'click details', trigger: 'click' } })
    try {
      const trigger = wrapper.get('.help-tooltip-trigger')
      expect(trigger.attributes('role')).toBe('button')
      await trigger.trigger('keydown', { key: 'Enter' })
      expect(getTooltipElement().style.display).not.toBe('none')
      expect(trigger.attributes('aria-expanded')).toBe('true')
      await trigger.trigger('keydown', { key: ' ' })
      expect(getTooltipElement().style.display).toBe('none')
      expect(trigger.attributes('aria-expanded')).toBe('false')
    } finally {
      wrapper.unmount()
    }
  })

  it('preserves a slotted link and its existing accessible description without adding a second tab stop', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: { content: 'link details' },
      slots: { trigger: '<a href="https://example.test/" aria-describedby="original-description">Account home</a>' },
    })
    try {
      await nextTick()
      const trigger = wrapper.get('.help-tooltip-trigger')
      const link = wrapper.get<HTMLAnchorElement>('a')
      expect(trigger.attributes('tabindex')).toBeUndefined()
      link.element.focus()
      await nextTick()
      expect(link.attributes('href')).toBe('https://example.test/')
      expect(link.attributes('aria-describedby')?.split(' ')).toEqual(['original-description', getTooltipElement().id])
      link.element.blur()
      await nextTick()
      expect(link.attributes('aria-describedby')).toBe('original-description')
    } finally {
      wrapper.unmount()
    }
  })

  it('keeps a fixed card inside a narrow viewport near either screen edge, including after scrolling', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    vi.stubGlobal('scrollX', 100)
    vi.stubGlobal('scrollY', 600)
    const wrapper = mount(HelpTooltip, { attachTo: document.body, props: { content: 'edge details' } })
    try {
      const trigger = wrapper.get('.help-tooltip-trigger')
      const tooltip = getTooltipElement()
      const triggerRect = vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(350, 100, 20, 20))
      vi.spyOn(tooltip, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 260, 120))
      await trigger.trigger('mouseenter')
      await nextTick()

      // The card is below a trigger near the top, with a 12px right gutter.
      // Fixed positioning uses viewport coordinates, independent of document scroll.
      expect(tooltip.style.left).toBe('118px')
      expect(tooltip.style.top).toBe('128px')
      expect(tooltip.dataset.placement).toBe('below')

      triggerRect.mockReturnValue(new DOMRect(0, 760, 20, 20))
      window.dispatchEvent(new Event('scroll'))
      await nextTick()
      expect(tooltip.style.left).toBe('12px')
      expect(tooltip.style.top).toBe('632px')
      expect(tooltip.dataset.placement).toBe('above')
    } finally {
      wrapper.unmount()
    }
  })
})
