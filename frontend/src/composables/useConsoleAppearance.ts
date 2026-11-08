import { useEffect } from 'react'
import type { ConsoleAppearance } from '@/types/appearance'
import { resolveConsoleBackgroundURL } from '@/utils/consoleAppearance'
import { consoleAccentPalette, consolePrimaryColors } from '@/utils/consoleAccent'
import { setConsoleCardAlpha } from '@/utils/consoleAppearanceState'

const APPEARANCE_PROPERTIES = [
  '--console-light-mask-alpha',
  '--console-dark-mask-alpha',
  '--console-card-alpha',
  '--console-primary-foreground',
  '--console-primary-end',
  '--console-primary-dark-foreground',
  '--console-primary-dark-end',
  ...Object.keys(consoleAccentPalette(null)).map((shade) => `--console-accent-${shade}`)
]

const clampPercent = (value: number, fallback: number): number =>
  Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : fallback

/**
 * Applies the administrator's console appearance to the document root: the
 * accent palette that drives every primary surface, the card transparency, the
 * light/dark masks and the optional background image.
 *
 * The background is only attached once the image has loaded, so a slow or
 * missing image never leaves a half-applied background behind.
 */
export function useConsoleAppearance(appearance: ConsoleAppearance): void {
  const backgroundUrl = appearance.background_url

  useEffect(() => {
    const root = document.documentElement
    const clearBackground = () => {
      root.removeAttribute('data-console-background')
      root.style.removeProperty('--console-custom-background-image')
    }

    clearBackground()
    const url = resolveConsoleBackgroundURL(backgroundUrl)
    if (!url) return

    let cancelled = false
    const image = new Image()
    image.onload = () => {
      if (cancelled) return
      root.style.setProperty('--console-custom-background-image', `url(${JSON.stringify(url)})`)
      root.setAttribute('data-console-background', '')
    }
    image.onerror = () => {
      if (!cancelled) clearBackground()
    }
    image.src = url

    return () => {
      cancelled = true
      image.onload = null
      image.onerror = null
      clearBackground()
    }
  }, [backgroundUrl])

  useEffect(() => {
    const root = document.documentElement
    const alpha = 1 - clampPercent(appearance.card_transparency ?? 0, 0) / 100

    root.style.setProperty('--console-light-mask-alpha', String(clampPercent(appearance.light_mask, 30) / 100))
    root.style.setProperty('--console-dark-mask-alpha', String(clampPercent(appearance.dark_mask, 48) / 100))
    setConsoleCardAlpha(alpha)
    root.style.setProperty('--console-card-alpha', String(alpha))
    root.toggleAttribute('data-console-transparent-cards', alpha < 1)

    for (const [shade, channels] of Object.entries(consoleAccentPalette(appearance.accent_color))) {
      root.style.setProperty(`--console-accent-${shade}`, channels)
    }

    const primary = consolePrimaryColors(appearance.accent_color, { alpha })
    root.style.setProperty('--console-primary-foreground', primary.foreground)
    root.style.setProperty('--console-primary-end', primary.end)

    const darkPrimary = consolePrimaryColors(appearance.accent_color, { alpha, dark: true })
    root.style.setProperty('--console-primary-dark-foreground', darkPrimary.foreground)
    root.style.setProperty('--console-primary-dark-end', darkPrimary.end)

    return () => {
      setConsoleCardAlpha(1)
      root.removeAttribute('data-console-transparent-cards')
      for (const property of APPEARANCE_PROPERTIES) root.style.removeProperty(property)
    }
  }, [
    appearance.accent_color,
    appearance.card_transparency,
    appearance.light_mask,
    appearance.dark_mask
  ])
}

export default useConsoleAppearance
