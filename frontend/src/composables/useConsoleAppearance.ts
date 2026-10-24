import { onScopeDispose, watch, watchEffect } from 'vue'
import type { ConsoleAppearance } from '@/types/appearance'
import { resolveConsoleBackgroundURL } from '@/utils/consoleAppearance'
import { consoleAccentPalette } from '@/utils/consoleAccent'
import { consoleCardAlpha } from '@/utils/consoleAppearanceState'

export function useConsoleAppearance(source: () => ConsoleAppearance): void {
  const root = document.documentElement
  const clearBackground = () => {
    root.removeAttribute('data-console-background')
    root.style.removeProperty('--console-custom-background-image')
  }
  watch(() => source().background_url, (path, _previous, onCleanup) => {
    clearBackground()
    const url = resolveConsoleBackgroundURL(path)
    if (!url) return
    const image = new Image()
    let cancelled = false
    image.onload = () => {
      if (cancelled) return
      root.style.setProperty('--console-custom-background-image', 'url(' + JSON.stringify(url) + ')')
      root.setAttribute('data-console-background', '')
    }
    image.onerror = () => { if (!cancelled) clearBackground() }
    image.src = url
    onCleanup(() => { cancelled = true; image.onload = null; image.onerror = null })
  }, { immediate: true })

  watchEffect(() => {
    const appearance = source()
    const percent = (value: number, fallback: number) => Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : fallback
    root.style.setProperty('--console-light-mask-alpha', String(percent(appearance.light_mask, 30) / 100))
    root.style.setProperty('--console-dark-mask-alpha', String(percent(appearance.dark_mask, 48) / 100))
    consoleCardAlpha.value = 1 - percent(appearance.card_transparency ?? 0, 0) / 100
    root.style.setProperty('--console-card-alpha', String(consoleCardAlpha.value))
    root.toggleAttribute('data-console-transparent-cards', consoleCardAlpha.value < 1)
    for (const [shade, channels] of Object.entries(consoleAccentPalette(appearance.accent_color))) {
      root.style.setProperty('--console-accent-' + shade, channels)
    }
  })

  onScopeDispose(() => {
    consoleCardAlpha.value = 1
    root.removeAttribute('data-console-transparent-cards')
    clearBackground()
    for (const key of ['--console-light-mask-alpha', '--console-dark-mask-alpha', '--console-card-alpha',
      ...Object.keys(consoleAccentPalette(null)).map(shade => '--console-accent-' + shade)]) root.style.removeProperty(key)
  })
}
