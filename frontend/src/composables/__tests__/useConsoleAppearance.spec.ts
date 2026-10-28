import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, type EffectScope } from 'vue'
import { useConsoleAppearance } from '../useConsoleAppearance'
import { defaultConsoleAppearance } from '@/types/appearance'

// Requirements: approved global appearance audit and docs/UI_INTERACTION.md:
// only backgrounds fade; foregrounds stay readable without opacity or filters.
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
  it('publishes background alpha independently from foregrounds and canvas masks', async () => {
    const appearance = ref(defaultConsoleAppearance())
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    expect(root.hasAttribute('data-console-custom-cards')).toBe(false)
    expect(root.style.getPropertyValue('--console-light-mask-alpha')).toBe('0.3')
    expect(root.style.getPropertyValue('--console-dark-mask-alpha')).toBe('0.48')
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
    expect(root.style.getPropertyValue('--console-primary-end')).toBe('45 212 191')
    for (const [transparency, alpha] of [[0, '1'], [45, '0.55'], [100, '0']] as const) {
      appearance.value.card_transparency = transparency
      await nextTick()
      expect(root.hasAttribute('data-console-custom-cards')).toBe(false)
      expect(root.style.getPropertyValue('--console-card-alpha')).toBe(alpha)
      expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
      expect(root.style.getPropertyValue('--console-primary-end')).toBe('45 212 191')
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
    expect(root.style.getPropertyValue('--console-card-alpha')).toBe('1')
  })

  it('updates primary label contrast with the accent and removes its tokens on disposal', async () => {
    const appearance = ref({ ...defaultConsoleAppearance(), accent_color: '#ffff00' })
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
    expect(root.style.getPropertyValue('--console-primary-end')).toBe('255 255 82')
    appearance.value.accent_color = '#000080'
    await nextTick()
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#ffffff')
    expect(root.style.getPropertyValue('--console-primary-end')).toBe('0 0 105')
    appearance.value.accent_color = 'invalid'
    await nextTick()
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
    expect(root.style.getPropertyValue('--console-primary-end')).toBe('45 212 191')
    scope.stop()
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('')
    expect(root.style.getPropertyValue('--console-primary-end')).toBe('')
    expect(root.style.getPropertyValue('--console-primary-dark-foreground')).toBe('')
    expect(root.style.getPropertyValue('--console-primary-dark-end')).toBe('')
  })

  it('updates both theme foregrounds when transparency changes without fading the text', async () => {
    const appearance = ref({ ...defaultConsoleAppearance(), accent_color: '#ffffff', card_transparency: 50 })
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
    expect(root.style.getPropertyValue('--console-primary-dark-foreground')).toBe('#000000')
    appearance.value.accent_color = '#000000'
    await nextTick()
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
    expect(root.style.getPropertyValue('--console-primary-dark-foreground')).toBe('#ffffff')
    expect(root.style.getPropertyValue('--console-primary-end')).toBe('82 82 82')
    expect(root.style.getPropertyValue('--console-primary-dark-end')).toBe('0 0 0')
    appearance.value.card_transparency = 0
    await nextTick()
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#ffffff')
    appearance.value.card_transparency = 100
    await nextTick()
    expect(root.style.getPropertyValue('--console-primary-foreground')).toBe('#000000')
    expect(root.style.getPropertyValue('--console-primary-dark-foreground')).toBe('#ffffff')
  })

  it('preserves the exact default accent and updates the whole palette without reloading', async () => {
    const appearance = ref(defaultConsoleAppearance())
    scope.run(() => useConsoleAppearance(() => appearance.value))
    const root = document.documentElement
    expect(root.style.getPropertyValue('--console-accent-500')).toBe('20 184 166')
    expect(root.style.getPropertyValue('--console-accent-700')).toBe('15 118 110')
    appearance.value.accent_color = '#f59e0b'
    await nextTick()
    expect(root.style.getPropertyValue('--console-accent-500')).toBe('245 158 11')
    expect(root.style.getPropertyValue('--console-accent-700')).toBe('164 106 7')
    appearance.value.accent_color = 'url(javascript:invalid)'
    appearance.value.card_transparency = null
    await nextTick()
    expect(root.style.getPropertyValue('--console-accent-500')).toBe('20 184 166')
    expect(root.style.getPropertyValue('--console-card-alpha')).toBe('1')
    scope.stop()
    expect(root.style.getPropertyValue('--console-accent-500')).toBe('')
    expect(root.style.getPropertyValue('--console-card-alpha')).toBe('')
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
