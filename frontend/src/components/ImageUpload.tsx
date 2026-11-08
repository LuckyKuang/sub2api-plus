import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Icon } from './Icon'
import { sanitizeSvg } from '@/utils/sanitize'

export interface ImageUploadProps {
  value: string
  mode?: 'image' | 'svg'
  size?: 'sm' | 'md'
  uploadLabel?: string
  removeLabel?: string
  hint?: string
  maxSize?: number // bytes
  className?: string
  onValueChange?: (value: string) => void
}

export function ImageUpload({
  value,
  mode = 'image',
  size = 'md',
  uploadLabel = '',
  removeLabel = '',
  hint = '',
  maxSize = 300 * 1024,
  className,
  onValueChange
}: ImageUploadProps) {
  const { t } = useTranslation()
  const [error, setError] = useState('')

  const resolvedUploadLabel = uploadLabel || t('common.upload')
  const resolvedRemoveLabel = removeLabel || t('common.remove')

  const acceptTypes = mode === 'svg' ? '.svg' : 'image/*'

  const sanitizedValue = mode === 'svg' ? sanitizeSvg(value ?? '') : ''

  const previewSizeClass = size === 'sm' ? 'h-14 w-14' : 'h-20 w-20'
  const innerSizeClass = size === 'sm' ? 'h-7 w-7' : 'h-12 w-12'
  const placeholderSizeClass = size === 'sm' ? 'h-5 w-5' : 'h-8 w-8'

  const handleUpload = (event: React.ChangeEvent<HTMLInputElement>) => {
    const input = event.target
    const file = input.files?.[0]
    setError('')

    if (!file) return

    if (maxSize && file.size > maxSize) {
      setError(
        t('common.fileTooLargeKb', {
          size: (file.size / 1024).toFixed(1),
          max: (maxSize / 1024).toFixed(0)
        })
      )
      input.value = ''
      return
    }

    const reader = new FileReader()
    if (mode === 'svg') {
      reader.onload = (e) => {
        const text = e.target?.result as string
        if (text) onValueChange?.(text.trim())
      }
      reader.readAsText(file)
    } else {
      if (!file.type.startsWith('image/')) {
        setError(t('common.selectImageFile'))
        input.value = ''
        return
      }
      reader.onload = (e) => {
        onValueChange?.(e.target?.result as string)
      }
      reader.readAsDataURL(file)
    }

    reader.onerror = () => {
      setError(t('common.fileReadFailed'))
    }
    input.value = ''
  }

  return (
    <div className={cn('flex items-start gap-4', className)}>
      {/* Preview Box */}
      <div className="flex-shrink-0">
        <div
          className={cn(
            'flex items-center justify-center overflow-hidden rounded-xl border-2 border-dashed border-gray-300 surface-inset dark:border-dark-600 surface-decoration',
            previewSizeClass,
            !!value && 'border-solid'
          )}
        >
          {/* SVG mode: render inline */}
          {mode === 'svg' && value ? (
            <span
              className={cn('[&>svg]:h-full [&>svg]:w-full surface-muted', innerSizeClass)}
              // The value is sanitized with sanitizeSvg before rendering.
              dangerouslySetInnerHTML={{ __html: sanitizedValue }}
            />
          ) : mode === 'image' && value ? (
            /* Image mode: show as img */
            <img src={value} alt="" className="h-full w-full object-contain" />
          ) : (
            /* Empty placeholder */
            <svg
              className={cn('surface-muted', placeholderSizeClass)}
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="1.5"
                d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
              />
            </svg>
          )}
        </div>
      </div>

      {/* Controls */}
      <div className="flex-1 space-y-2">
        <div className="flex items-center gap-2">
          <label className="btn btn-secondary btn-sm cursor-pointer">
            <input
              type="file"
              accept={acceptTypes}
              className="hidden"
              onChange={handleUpload}
            />
            <Icon name="upload" size="sm" className="mr-1.5" strokeWidth={2} />
            {resolvedUploadLabel}
          </label>
          {value && (
            <button
              type="button"
              className="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
              onClick={() => onValueChange?.('')}
            >
              <Icon name="trash" size="sm" className="mr-1.5" strokeWidth={2} />
              {resolvedRemoveLabel}
            </button>
          )}
        </div>
        {hint && <p className="text-xs surface-muted">{hint}</p>}
        {error && <p className="text-xs text-red-500">{error}</p>}
      </div>
    </div>
  )
}
