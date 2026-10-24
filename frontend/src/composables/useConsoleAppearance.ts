import { onScopeDispose, watch, watchEffect } from 'vue'
import type { ConsoleAppearance } from '@/types/appearance'
import { resolveConsoleBackgroundURL } from '@/utils/consoleAppearance'

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
  })

  onScopeDispose(() => {
    clearBackground()
    for (const key of ['--console-light-mask-alpha', '--console-dark-mask-alpha']) root.style.removeProperty(key)
  })
}
