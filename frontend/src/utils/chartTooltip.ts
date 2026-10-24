/** Canvas overlays use the same arrowless card shape and palette as HTML panels. */
export function chartTooltipStyle(isDark: boolean) {
  return {
    cornerRadius: 16,
    caretSize: 0,
    padding: 12,
    backgroundColor: isDark ? '#262c2d' : '#f8faf9',
    titleColor: isDark ? '#eaf3f2' : '#182e31',
    bodyColor: isDark ? '#c1cdd1' : '#52676b',
    footerColor: isDark ? '#eaf3f2' : '#182e31',
    borderColor: isDark ? 'rgba(255, 255, 255, 0.16)' : 'rgba(255, 255, 255, 0.68)',
    borderWidth: 1,
  }
}
