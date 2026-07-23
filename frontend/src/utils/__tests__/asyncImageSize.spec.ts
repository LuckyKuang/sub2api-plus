import { describe, expect, it } from 'vitest'
import { isGPTImage2, isGPTImage2ExperimentalSize, validateGPTImage2CustomSize } from '../asyncImageSize'

describe('GPT-image-2 resolution validation', () => {
  it('accepts supported custom dimensions and identifies the experimental range', () => {
    expect(validateGPTImage2CustomSize(1920, 1088)).toBeNull()
    expect(validateGPTImage2CustomSize(2560, 1440)).toBeNull()
    expect(validateGPTImage2CustomSize(3840, 2160)).toBeNull()
    expect(isGPTImage2ExperimentalSize(2560, 1440)).toBe(false)
    expect(isGPTImage2ExperimentalSize(3840, 2160)).toBe(true)
  })

  it('rejects dimensions outside the documented rules', () => {
    expect(validateGPTImage2CustomSize(1920, 1080)).toBe('multipleOf16')
    expect(validateGPTImage2CustomSize(4096, 2160)).toBe('maxEdge')
    expect(validateGPTImage2CustomSize(3840, 3840)).toBe('pixelRange')
    expect(validateGPTImage2CustomSize(3072, 1008)).toBe('aspectRatio')
  })

  it('only enables expanded controls for the exact model', () => {
    expect(isGPTImage2('gpt-image-2')).toBe(true)
    expect(isGPTImage2('gpt-image-1')).toBe(false)
    expect(isGPTImage2('gpt-image-2-preview')).toBe(false)
  })
})
