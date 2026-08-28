/**
 * Token percentages are only meaningful when the numerator and denominator
 * describe the same request or filtered period. Keep the calculation in one
 * place so every usage view follows the same rounding and empty-state rules.
 */
export function tokenShare(part: number | null | undefined, total: number | null | undefined): number | null {
  const safePart = Number(part)
  const safeTotal = Number(total)
  if (!Number.isFinite(safePart) || !Number.isFinite(safeTotal) || safePart <= 0 || safeTotal <= 0) {
    return null
  }
  return Math.min(100, Math.max(0, (safePart / safeTotal) * 100))
}

export function formatTokenShare(part: number | null | undefined, total: number | null | undefined): string | null {
  const share = tokenShare(part, total)
  if (share == null) return null
  if (share > 0 && share < 0.1) return '<0.1%'
  return `${share.toFixed(1)}%`
}

export function sumTokenBuckets(...values: Array<number | null | undefined>): number {
  return values.reduce<number>((total, value) => {
    const numberValue = Number(value)
    return total + (Number.isFinite(numberValue) && numberValue > 0 ? numberValue : 0)
  }, 0)
}
