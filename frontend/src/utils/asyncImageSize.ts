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
