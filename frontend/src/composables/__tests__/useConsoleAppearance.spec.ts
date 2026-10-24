import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { useConsoleAppearance } from '../useConsoleAppearance'
import { defaultConsoleAppearance } from '@/types/appearance'

// Requirements: docs/UI_INTERACTION.md, including failed/stale image fallback
// and solid content surfaces regardless of the stored transparency field.
let scope: EffectScope
const images: FakeImage[] = []
class FakeImage {
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  src = ''
  constructor() { images.push(this) }
}
const path = (letter: string) => '/api/v1/settings/console-background/' + letter.repeat(64)
beforeEach(() => { images.length = 0; vi.stubGlobal('Image', FakeImage); scope = effectScope() })
afterEach(() => { scope.stop(); vi.unstubAllGlobals() })

describe('published console appearance', () => {
  it('applies canvas masks without letting stored transparency fade content or add a filter', async () => {
    const appearance = ref(defaultConsoleAppearance())
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    expect(root.hasAttribute('data-console-custom-cards')).toBe(false)
    expect(root.style.getPropertyValue('--console-light-mask-alpha')).toBe('0.3')
    expect(root.style.getPropertyValue('--console-dark-mask-alpha')).toBe('0.48')
    for (const transparency of [0, 45, 100]) {
      appearance.value.card_transparency = transparency
      await nextTick()
      expect(root.hasAttribute('data-console-custom-cards')).toBe(false)
      expect(root.style.getPropertyValue('--console-card-alpha')).toBe('')
      expect(root.style.opacity).toBe('')
      expect(root.style.getPropertyValue('backdrop-filter')).toBe('')
    }
    appearance.value.light_mask = 0
    appearance.value.dark_mask = 100
    await nextTick()
    expect(root.style.getPropertyValue('--console-light-mask-alpha')).toBe('0')
    expect(root.style.getPropertyValue('--console-dark-mask-alpha')).toBe('1')
    appearance.value = defaultConsoleAppearance()
    await nextTick()
    expect(root.hasAttribute('data-console-custom-cards')).toBe(false)
  })

  it('loads the background before showing it and falls back to the gradient on image failure', async () => {
    const appearance = ref({ ...defaultConsoleAppearance(), background_url: path('a') })
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    expect(root.hasAttribute('data-console-background')).toBe(false)
    images[0]!.onload!()
    expect(root.hasAttribute('data-console-background')).toBe(true)
    expect(root.style.getPropertyValue('--console-custom-background-image')).toContain(path('a'))
    appearance.value.background_url = path('b')
    await nextTick()
    images[1]!.onerror!()
    expect(root.hasAttribute('data-console-background')).toBe(false)
    expect(root.style.getPropertyValue('--console-custom-background-image')).toBe('')
  })

  it('ignores stale image completions after replacement, reset and scope disposal', async () => {
    const appearance = ref({ ...defaultConsoleAppearance(), background_url: path('a') })
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    const oldLoad = images[0]!.onload!
    appearance.value.background_url = path('b')
    await nextTick()
    images[1]!.onload!()
    oldLoad()
    expect(root.style.getPropertyValue('--console-custom-background-image')).toContain(path('b'))
    const replacementLoad = images[1]!.onload!
    appearance.value = defaultConsoleAppearance()
    await nextTick()
    replacementLoad()
    expect(root.hasAttribute('data-console-background')).toBe(false)
    appearance.value.background_url = path('c')
    await nextTick()
    const pendingLoad = images[2]!.onload!
    scope.stop()
    pendingLoad()
    expect(root.hasAttribute('data-console-background')).toBe(false)
  })

  it('rejects external and executable image URLs without requesting them', () => {
    scope.run(() => useConsoleAppearance(() => ({ ...defaultConsoleAppearance(), background_url: 'https://example.com/tracker.png' })))
    expect(images).toHaveLength(0)
  })
})
