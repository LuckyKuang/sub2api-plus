import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { toast } from '@/components/ui'
import { totpAPI } from '@/api/totp'
import type { StepUpController } from './useStepUp'

export interface TotpStepUpDialogProps {
  controller: StepUpController
}

const CODE_LENGTH = 6

export function TotpStepUpDialog({ controller }: TotpStepUpDialogProps) {
  const { t } = useTranslation()
  const [verifying, setVerifying] = useState(false)
  const [code, setCode] = useState<string[]>(() => Array<string>(CODE_LENGTH).fill(''))
  const inputRefs = useRef<Array<HTMLInputElement | null>>([])
  const hiddenOtpInputRef = useRef<HTMLInputElement | null>(null)

  const joinedCode = code.join('')

  const resetInputs = () => {
    setCode(Array<string>(CODE_LENGTH).fill(''))
    inputRefs.current.forEach((input) => {
      if (input) input.value = ''
    })
    if (hiddenOtpInputRef.current) {
      hiddenOtpInputRef.current.value = ''
    }
  }

  // Focus the first cell whenever the dialog opens.
  useEffect(() => {
    if (controller.visible) {
      resetInputs()
      inputRefs.current[0]?.focus()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [controller.visible])

  // Auto-submit once 6 digits are entered.
  useEffect(() => {
    if (joinedCode.length === CODE_LENGTH && !verifying) {
      void submit(joinedCode)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [joinedCode, verifying])

  async function submit(otp: string) {
    setVerifying(true)
    try {
      await totpAPI.stepUp(otp)
      setVerifying(false)
      resetInputs()
      controller.onVerified()
    } catch (err: unknown) {
      setVerifying(false)
      toast.error((err as { message?: string })?.message || t('stepUp.verifyFailed'))
      resetInputs()
      inputRefs.current[0]?.focus()
    }
  }

  function handleCancel() {
    if (verifying) return
    controller.onCancel()
  }

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

  const handleHiddenOtpInput = (event: React.ChangeEvent<HTMLInputElement>) => {
    const digits = event.target.value.replace(/[^0-9]/g, '').slice(0, CODE_LENGTH).split('')
    const next = Array<string>(CODE_LENGTH).fill('')
    for (let index = 0; index < CODE_LENGTH; index++) {
      next[index] = digits[index] || ''
      if (inputRefs.current[index]) {
        inputRefs.current[index]!.value = digits[index] || ''
      }
    }
    setCode(next)
  }

  const handleKeydown = (event: React.KeyboardEvent<HTMLInputElement>, index: number) => {
    if (event.key === 'Backspace') {
      const input = event.currentTarget
      if (!input.value && index > 0) {
        event.preventDefault()
        inputRefs.current[index - 1]?.focus()
      }
    }
  }

  const handlePaste = (event: React.ClipboardEvent<HTMLInputElement>) => {
    event.preventDefault()
    const pastedData = event.clipboardData?.getData('text') || ''
    const digits = pastedData.replace(/[^0-9]/g, '').slice(0, CODE_LENGTH).split('')
    const next = Array<string>(CODE_LENGTH).fill('')
    for (let index = 0; index < CODE_LENGTH; index++) {
      next[index] = digits[index] || ''
      if (inputRefs.current[index]) {
        inputRefs.current[index]!.value = digits[index] || ''
      }
    }
    setCode(next)
    const focusIndex = Math.min(digits.length, CODE_LENGTH - 1)
    inputRefs.current[focusIndex]?.focus()
  }

  if (!controller.visible) return null

  return createPortal(
    <div className="fixed inset-0 z-[60] overflow-y-auto">
      <div className="flex min-h-full items-center justify-center p-4">
        <div className="fixed inset-0 transition-opacity surface-overlay" onClick={handleCancel} />

        <div className="relative w-full max-w-md transform rounded-xl p-6 shadow-xl transition-all surface-panel">
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
                  d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z"
                />
              </svg>
            </div>
            <h3 className="mt-4 text-xl font-semibold text-gray-900 dark:text-white">{t('stepUp.title')}</h3>
            <p className="mt-2 text-sm surface-muted">{t('stepUp.hint')}</p>
          </div>

          <div className="mb-6">
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
            {verifying && (
              <div className="mt-3 flex items-center justify-center gap-2 text-sm surface-muted">
                <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-primary-500" />
                {t('common.verifying')}
              </div>
            )}
          </div>

          <button type="button" className="btn btn-secondary w-full" disabled={verifying} onClick={handleCancel}>
            {t('common.cancel')}
          </button>
        </div>
      </div>
    </div>,
    document.body
  )
}

export default TotpStepUpDialog
