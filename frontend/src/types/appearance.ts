export interface ConsoleAppearance {
  background_url: string
  light_mask: number
  dark_mask: number
  /** null preserves the original theme-specific card surfaces. */
  card_transparency: number | null
}

export interface ConsoleAppearanceUpdate {
  background_image?: string
  light_mask: number
  dark_mask: number
  card_transparency: number | null
}

export function defaultConsoleAppearance(): ConsoleAppearance {
  return { background_url: '', light_mask: 30, dark_mask: 48, card_transparency: null }
}
