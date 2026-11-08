import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { toast } from '@/components/ui'

export interface TotpLoginModalProps {
  open: boolean
  tempToken: string
  userEmailMasked?: string
  verifying?: boolean
  errorMessage?: string
  onVerify?: (code: string) => void | Promise<void>
  onCancel?: () => void
}

const CODE_LENGTH = 6

export function TotpLoginModal({
  open,
  tempToken: _tempToken,
  userEmailMasked = '',
  verifying = false,
  errorMessage = '',
  onVerify,
  onCancel
}: TotpLoginModalProps) {
  const { t } = useTranslation()
  const [code, setCode] = useState<string[]>(() => Array<string>(CODE_LENGTH).fill(''))
  const inputRefs = useRef<Array<HTMLInputElement | null>>([])
  const hiddenOtpInputRef = useRef<HTMLInputElement | null>(null)

  const joinedCode = code.join('')

  // Auto-submit once 6 digits are entered.
  useEffect(() => {
    if (joinedCode.length === CODE_LENGTH && !verifying) {
      void onVerify?.(joinedCode)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [joinedCode, verifying])

  useEffect(() => {
    if (open) {
      inputRefs.current[0]?.focus()
    }
  }, [open])

  // A failed verification clears the cells and surfaces the backend message.
  useEffect(() => {
    if (!errorMessage) return
    toast.error(errorMessage)
    setCode(Array<string>(CODE_LENGTH).fill(''))
    inputRefs.current.forEach((input) => {
      if (input) input.value = ''
    })
    if (hiddenOtpInputRef.current) {
      hiddenOtpInputRef.current.value = ''
    }
    inputRefs.current[0]?.focus()
  }, [errorMessage])

  const setInputRef = (el: HTMLInputElement | null, index: number) => {
    inputRefs.current[index] = el
  }

  const handleCodeInput = (event: React.ChangeEvent<HTMLInputElement>, index: number) => {
    const value = event.target.value.replace(/[^0-9]/g, '')
    setCode((previous) => {
      const next = [...previous]
      next[index] = value
      return next
    })

    if (value && index < CODE_LENGTH - 1) {
      inputRefs.current[index + 1]?.focus()
    }
  }

  // Handle autofill from password managers via the hidden autocomplete="one-time-code" input
  const handleHiddenOtpInput = (event: React.ChangeEvent<HTMLInputElement>) => {
    const digits = event.target.value.replace(/[^0-9]/g, '').slice(0, CODE_LENGTH).split('')

    const next = Array<string>(CODE_LENGTH).fill('')
    digits.forEach((digit, index) => {
      next[index] = digit
      if (inputRefs.current[index]) {
        inputRefs.current[index]!.value = digit
      }
    })
    setCode(next)
  }

  const handleKeydown = (event: React.KeyboardEvent<HTMLInputElement>, index: number) => {
    if (event.key === 'Backspace') {
      const input = event.currentTarget
      // If current cell is empty and not the first, move to previous cell
      if (!input.value && index > 0) {
        event.preventDefault()
        inputRefs.current[index - 1]?.focus()
      }
      // Otherwise, let the browser handle the backspace naturally
      // The input event will sync code via handleCodeInput
    }
  }

  const handlePaste = (event: React.ClipboardEvent<HTMLInputElement>) => {
    event.preventDefault()
    const pastedData = event.clipboardData?.getData('text') || ''
    const digits = pastedData.replace(/[^0-9]/g, '').slice(0, CODE_LENGTH).split('')

    const next = Array<string>(CODE_LENGTH).fill('')
    digits.forEach((digit, index) => {
      next[index] = digit
      if (inputRefs.current[index]) {
        inputRefs.current[index]!.value = digit
      }
    })
    setCode(next)

    const focusIndex = Math.min(digits.length, CODE_LENGTH - 1)
    inputRefs.current[focusIndex]?.focus()
  }

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 overflow-y-auto">
      <div className="flex min-h-full items-center justify-center p-4">
        <div className="fixed inset-0 transition-opacity surface-overlay" />

        <div className="relative w-full max-w-md transform rounded-xl p-6 shadow-xl transition-all surface-panel">
          {/* Header */}
          <div className="mb-6 text-center">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
              <svg
                className="h-6 w-6 text-primary-600 dark:text-primary-400"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                strokeWidth="1.5"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z"
                />
              </svg>
            </div>
            <h3 className="mt-4 text-xl font-semibold text-gray-900 dark:text-white">
              {t('profile.totp.loginTitle')}
            </h3>
            <p className="mt-2 text-sm surface-muted">{t('profile.totp.loginHint')}</p>
            {userEmailMasked && <p className="mt-1 text-sm font-medium surface-muted">{userEmailMasked}</p>}
          </div>

          {/* Code Input */}
          <div className="mb-6">
            {/* Hidden input for password manager autofill (autocomplete="one-time-code") */}
            <input
              ref={hiddenOtpInputRef}
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={CODE_LENGTH}
              className="pointer-events-none absolute left-0 top-0 h-px w-px opacity-0"
              aria-hidden="true"
              tabIndex={-1}
              onChange={handleHiddenOtpInput}
            />
            <div className="flex justify-center gap-1.5 sm:gap-2">
              {Array.from({ length: CODE_LENGTH }, (_, index) => (
                <input
                  key={index}
                  ref={(el) => setInputRef(el, index)}
                  type="text"
                  maxLength={1}
                  inputMode="numeric"
                  pattern="[0-9]"
                  autoComplete="off"
                  disabled={verifying}
                  className="h-12 min-w-0 max-w-10 flex-1 rounded-lg border border-gray-300 text-center text-lg font-semibold focus:border-primary-500 focus:ring-primary-500 dark:border-dark-600 dark:surface-inset"
                  onChange={(event) => handleCodeInput(event, index)}
                  onKeyDown={(event) => handleKeydown(event, index)}
                  onPaste={handlePaste}
                />
              ))}
            </div>
            {/* Loading indicator */}
            {verifying && (
              <div className="mt-3 flex items-center justify-center gap-2 text-sm surface-muted">
                <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-primary-500" />
                {t('common.verifying')}
              </div>
            )}
          </div>

          {/* Cancel button only */}
          <button type="button" className="btn btn-secondary w-full" disabled={verifying} onClick={() => onCancel?.()}>
            {t('common.cancel')}
          </button>
        </div>
      </div>
    </div>,
    document.body
  )
}

export default TotpLoginModal
