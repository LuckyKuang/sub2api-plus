/** Shared with canvas renderers, which cannot resolve CSS custom properties. */
let cardAlpha = 1

export function getConsoleCardAlpha(): number {
  return cardAlpha
}

export function setConsoleCardAlpha(alpha: number): void {
  cardAlpha = alpha
}
