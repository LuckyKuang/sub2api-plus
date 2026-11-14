export type GPTImage2SizeValidationCode =
  | 'positive'
  | 'multipleOf16'
  | 'maxEdge'
  | 'aspectRatio'
  | 'pixelRange'

export const GPT_IMAGE_2_STABLE_MAX_PIXELS = 2560 * 1440

export function isGPTImage2(model: string): boolean {
  return model.trim() === 'gpt-image-2'
}

// Parameter profiles differ per provider family: GPT image models take quality
// and arbitrary sizes, while the Gemini-compatible aliases (including
// nano-banana) and Grok only offer the three documented aspect orientations.
export type ImageModelFamily = 'gpt' | 'gemini' | 'grok' | 'other'

export function imageModelFamily(model: string): ImageModelFamily {
  const name = model.trim().toLowerCase()
  if (name.startsWith('gpt-image-')) return 'gpt'
  if (name.includes('nano-banana')) return 'gemini'
  if (name.startsWith('gemini-') && (name.endsWith('-image') || name.includes('-image-'))) return 'gemini'
  if (name.startsWith('grok-imagine')) return 'grok'
  return 'other'
}

// Gemini-compatible upstreams reject explicit non-square dimensions and accept
// only the 1K/2K/4K output tiers, with orientation carried by aspect_ratio.
export const GEMINI_IMAGE_SIZES = ['1K', '2K', '4K'] as const
export const GEMINI_ASPECT_RATIOS = ['1:1', '16:9', '9:16', '4:3', '3:4'] as const
export const DEFAULT_GEMINI_ASPECT_RATIO = '1:1'

export function validateGPTImage2CustomSize(width: number, height: number): GPTImage2SizeValidationCode | null {
  if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) return 'positive'
  if (width % 16 !== 0 || height % 16 !== 0) return 'multipleOf16'
  const longEdge = Math.max(width, height)
  const shortEdge = Math.min(width, height)
  if (longEdge > 3840) return 'maxEdge'
  if (longEdge > shortEdge * 3) return 'aspectRatio'
  const pixels = width * height
  if (pixels < 655360 || pixels > 8294400) return 'pixelRange'
  return null
}

export function isGPTImage2ExperimentalSize(width: number, height: number): boolean {
  return Number.isFinite(width) && Number.isFinite(height) && width * height > GPT_IMAGE_2_STABLE_MAX_PIXELS
}
