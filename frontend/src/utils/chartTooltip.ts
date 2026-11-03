import { consoleCardAlpha } from '@/utils/consoleAppearanceState'

/** Canvas overlays use the same arrowless card shape and palette as HTML panels. */
export function chartTooltipStyle(isDark: boolean) {
  return {
    cornerRadius: 16,
    caretSize: 0,
    padding: 12,
    // Reading the shared ref also invalidates each chart's computed options
    // when the saved or previewed appearance changes while it stays mounted.
    backgroundColor: isDark ? `rgba(41, 38, 36, ${consoleCardAlpha.value})` : `rgba(255, 255, 255, ${consoleCardAlpha.value})`,
    titleColor: isDark ? '#f2f0ed' : '#221f1c',
    bodyColor: isDark ? '#b8b3ac' : '#635e57',
    footerColor: isDark ? '#f2f0ed' : '#221f1c',
    borderColor: isDark ? 'rgba(170, 162, 152, 0.22)' : 'rgba(120, 112, 102, 0.20)',
    borderWidth: 1,
  }
}
