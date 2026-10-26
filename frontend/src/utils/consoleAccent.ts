export const DEFAULT_CONSOLE_ACCENT = '#14b8a6'

export const DEFAULT_CONSOLE_PALETTE = {
  50: '#f0fdfa', 100: '#ccfbf1', 200: '#99f6e4', 300: '#5eead4', 400: '#2dd4bf',
  500: DEFAULT_CONSOLE_ACCENT, 600: '#0d9488', 700: '#0f766e', 800: '#115e59', 900: '#134e4a', 950: '#042f2e'
} as const

export function normalizeConsoleAccent(value: unknown): string {
  return typeof value === 'string' && /^#[\da-f]{6}$/i.test(value) ? value.toLowerCase() : DEFAULT_CONSOLE_ACCENT
}

export function consoleAccentPalette(value: unknown): Record<string, string> {
  const color = normalizeConsoleAccent(value)
  const rgb = (hex: string) => [1, 3, 5].map(start => parseInt(hex.slice(start, start + 2), 16))
  const channels = rgb(color)
  const light: Record<string, number> = { 50: .96, 100: .90, 200: .78, 300: .62, 400: .32 }
  const dark: Record<string, number> = { 600: .82, 700: .67, 800: .50, 900: .35, 950: .20 }
  return Object.fromEntries(Object.entries(DEFAULT_CONSOLE_PALETTE).map(([shade, hex]) => [shade,
    (color === DEFAULT_CONSOLE_ACCENT ? rgb(hex) : channels.map(channel =>
      Math.round(shade in light ? channel + (255 - channel) * light[shade]! : shade in dark ? channel * dark[shade]! : channel)
    )).join(' ')
  ]))
}
