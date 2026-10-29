import { describe, expect, it } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { useProductAppearance } from '../useProductAppearance'

describe('public and user presentation ownership', () => {
  it('keeps portals themed during overlapping layouts and restores admin appearance', async () => {
    const user = ref(true)
    const first = effectScope()
    const second = effectScope()
    first.run(() => useProductAppearance(() => user.value))
    second.run(() => useProductAppearance())
    const portal = document.createElement('div')
    document.body.append(portal)
    try {
      expect(portal.closest('html')?.hasAttribute('data-product-ui')).toBe(true)
      first.stop()
      expect(document.documentElement.hasAttribute('data-product-ui')).toBe(true)
      second.stop()
      expect(document.documentElement.hasAttribute('data-product-ui')).toBe(false)

      const layout = effectScope()
      try {
        layout.run(() => useProductAppearance(() => user.value))
        user.value = false
        await nextTick()
        expect(document.documentElement.hasAttribute('data-product-ui')).toBe(false)
        user.value = true
        await nextTick()
        expect(document.documentElement.hasAttribute('data-product-ui')).toBe(true)
      } finally { layout.stop() }
      expect(document.documentElement.hasAttribute('data-product-ui')).toBe(false)
    } finally { first.stop(); second.stop(); portal.remove() }
  })
})
