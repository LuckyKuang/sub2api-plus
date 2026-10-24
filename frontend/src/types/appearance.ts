import { DEFAULT_CONSOLE_ACCENT } from '@/utils/consoleAccent'

export interface ConsoleAppearance {
  background_url: string
  light_mask: number
  dark_mask: number
  accent_color: string
  /** Older stored null values use the default 0% transparency. */
  card_transparency: number | null
}

export interface ConsoleAppearanceUpdate {
  background_image?: string
  light_mask: number
  dark_mask: number
  accent_color?: string
  card_transparency: number | null
}

export function defaultConsoleAppearance(): ConsoleAppearance {
  return { background_url: '', light_mask: 30, dark_mask: 48, accent_color: DEFAULT_CONSOLE_ACCENT, card_transparency: 0 }
}
