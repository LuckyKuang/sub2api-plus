import { consoleCardAlpha } from '@/utils/consoleAppearanceState'

/** Canvas overlays use the same arrowless card shape and palette as HTML panels. */
export function chartTooltipStyle(isDark: boolean) {
  return {
    cornerRadius: 16,
    caretSize: 0,
    padding: 12,
    // Reading the shared ref also invalidates each chart's computed options
    // when the saved or previewed appearance changes while it stays mounted.
    backgroundColor: isDark ? `rgba(38, 44, 45, ${consoleCardAlpha.value})` : `rgba(248, 250, 249, ${consoleCardAlpha.value})`,
    titleColor: isDark ? '#eaf3f2' : '#182e31',
    bodyColor: isDark ? '#c1cdd1' : '#52676b',
    footerColor: isDark ? '#eaf3f2' : '#182e31',
    borderColor: isDark ? 'rgba(203, 213, 225, 0.28)' : 'rgba(100, 116, 139, 0.34)',
    borderWidth: 1,
  }
}
