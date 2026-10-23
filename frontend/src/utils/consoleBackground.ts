export const MAX_BACKGROUND_INPUT_BYTES = 10 * 1024 * 1024
const MAX_BACKGROUND_BYTES = 2 * 1024 * 1024

export class BackgroundImageError extends Error {
  constructor(public readonly reason: 'format' | 'size' | 'decode') { super(reason) }
}

export async function prepareConsoleBackground(file: File): Promise<{ blob: Blob; dataURL: string }> {
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) throw new BackgroundImageError('format')
  if (file.size > MAX_BACKGROUND_INPUT_BYTES) throw new BackgroundImageError('size')
  const source = URL.createObjectURL(file)
  try {
    const image = new Image()
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new BackgroundImageError('decode'))
      image.src = source
    })
    if (!image.naturalWidth || !image.naturalHeight || image.naturalWidth * image.naturalHeight > 40_000_000) throw new BackgroundImageError('size')
    const scale = Math.min(1, 2560 / Math.max(image.naturalWidth, image.naturalHeight))
    const canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
    const context = canvas.getContext('2d')
    if (!context) throw new BackgroundImageError('decode')
    context.fillStyle = '#ffffff'
    context.fillRect(0, 0, canvas.width, canvas.height)
    context.drawImage(image, 0, 0, canvas.width, canvas.height)
    for (const quality of [0.85, 0.72, 0.6]) {
      const blob = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, 'image/jpeg', quality))
      if (!blob) throw new BackgroundImageError('decode')
      if (blob.size > MAX_BACKGROUND_BYTES) continue
      const dataURL = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result))
        reader.onerror = () => reject(new BackgroundImageError('decode'))
        reader.readAsDataURL(blob)
      })
      return { blob, dataURL }
    }
    throw new BackgroundImageError('size')
  } finally { URL.revokeObjectURL(source) }
}
