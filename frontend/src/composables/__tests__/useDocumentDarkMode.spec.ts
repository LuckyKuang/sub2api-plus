import { afterEach, describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useDocumentDarkMode } from '../useDocumentDarkMode'

afterEach(() => document.documentElement.classList.remove('dark'))

describe('useDocumentDarkMode', () => {
  it('updates mounted chart consumers when the theme changes in either direction', async () => {
    const wrapper = mount(defineComponent({ setup() {
      const dark = useDocumentDarkMode()
      return () => h('span', dark.value ? 'light chart labels' : 'dark chart labels')
    } }))
    expect(wrapper.text()).toBe('dark chart labels')
    document.documentElement.classList.add('dark')
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
    expect(wrapper.text()).toBe('light chart labels')
    document.documentElement.classList.remove('dark')
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
    expect(wrapper.text()).toBe('dark chart labels')
    wrapper.unmount()
  })
})
