import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { BackgroundImageError, prepareConsoleBackground } from '../consoleBackground'

// docs/UI_INTERACTION.md: accept common raster uploads including the user's
// >5 MiB original, resize to <=2560 px, and reject unsupported/oversized input.
let width = 3840
let height = 2160
let decodeFailure = false
let encoded: Blob | null
let dimensions: { width: number; height: number }
const drawImage = vi.fn()
class UploadImage {
  naturalWidth = width
  naturalHeight = height
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  set src(_value: string) { queueMicrotask(() => decodeFailure ? this.onerror?.() : this.onload?.()) }
}
beforeEach(() => {
  width = 3840; height = 2160; decodeFailure = false
  encoded = new Blob(['jpeg'], { type: 'image/jpeg' })
  vi.stubGlobal('Image', UploadImage)
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = vi.fn(() => 'blob:source')
    static revokeObjectURL = vi.fn()
  })
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function (this: HTMLCanvasElement) {
    dimensions = { width: this.width, height: this.height }
    return { fillRect: vi.fn(), drawImage, fillStyle: '' } as unknown as CanvasRenderingContext2D
  })
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(callback => callback(encoded))
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('background raster preparation', () => {
  it.each(['image/jpeg', 'image/png', 'image/webp'])('compresses a %s upload within the documented size and dimensions', async type => {
    const file = new File(['source'], 'wallpaper', { type })
    Object.defineProperty(file, 'size', { value: 6_777_663 })
    const result = await prepareConsoleBackground(file)
    expect(dimensions.width).toBe(2560)
    expect(dimensions.height).toBe(1440)
    expect(result.blob.type).toBe('image/jpeg')
    expect(result.dataURL).toMatch(/^data:image\/jpeg;base64,/)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:source')
  })

  it('rejects SVGs and input over 10 MiB before decoding', async () => {
    await expect(prepareConsoleBackground(new File(['svg'], 'a.svg', { type: 'image/svg+xml' }))).rejects.toMatchObject({ reason: 'format' })
    const tooLarge = new File(['x'], 'a.jpg', { type: 'image/jpeg' })
    Object.defineProperty(tooLarge, 'size', { value: 10 * 1024 * 1024 + 1 })
    await expect(prepareConsoleBackground(tooLarge)).rejects.toMatchObject({ reason: 'size' })
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })

  it('rejects corrupt, excessive-pixel and uncompressible images and releases temporary URLs', async () => {
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    decodeFailure = true
    await expect(prepareConsoleBackground(file)).rejects.toBeInstanceOf(BackgroundImageError)
    decodeFailure = false; width = 10000; height = 10000
    await expect(prepareConsoleBackground(file)).rejects.toMatchObject({ reason: 'size' })
    width = 3840; height = 2160; encoded = new Blob([new Uint8Array(2 * 1024 * 1024 + 1)])
    await expect(prepareConsoleBackground(file)).rejects.toMatchObject({ reason: 'size' })
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(3)
  })
})
