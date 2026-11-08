import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import { totpAPI } from '@/api/totp'

/**
 * Step-up (sudo) 2FA helper.
 *
 * Wraps a sensitive admin action so that when the backend responds with a
 * STEP_UP_REQUIRED error, the caller can prompt for a TOTP code, obtain a
 * short-lived grant, and transparently retry the original action.
 */

/** Error codes the backend uses to signal step-up state. */
const STEP_UP_REQUIRED = 'STEP_UP_REQUIRED'
const STEP_UP_TOTP_NOT_ENABLED = 'STEP_UP_TOTP_NOT_ENABLED'
const STEP_UP_ADMIN_API_KEY_FORBIDDEN = 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'

/**
 * Thrown by run() when the user dismisses the TOTP dialog.
 * Callers should treat it as a silent no-op, not an error to toast.
 */
export class StepUpCancelledError extends Error {
  readonly code = 'STEP_UP_CANCELLED'
  constructor() {
    super('step-up verification cancelled by user')
    this.name = 'StepUpCancelledError'
  }
}

interface ApiError {
  status?: number
  code?: string | number
  reason?: string
  message?: string
}

/** Extract the semantic error marker from either envelope shape (code or reason). */
function markerOf(err: unknown): string {
  const e = (err ?? {}) as ApiError
  const candidates = [e.code, e.reason].map((v) => (typeof v === 'string' ? v : ''))
  return candidates.find((v) => v.startsWith('STEP_UP')) || ''
}

export function isStepUpRequired(err: unknown): boolean {
  return markerOf(err) === STEP_UP_REQUIRED
}

export function isStepUpBlocked(err: unknown): boolean {
  const m = markerOf(err)
  return m === STEP_UP_TOTP_NOT_ENABLED || m === STEP_UP_ADMIN_API_KEY_FORBIDDEN
}

export function stepUpBlockReason(err: unknown): string {
  return markerOf(err)
}

export function isStepUpCancelled(err: unknown): boolean {
  return err instanceof StepUpCancelledError
}

export interface StepUpController {
  visible: boolean
  run<T>(action: () => Promise<T>): Promise<T>
  onVerified(): void
  onCancel(): void
}

export function useStepUp(): StepUpController {
  const [visible, setVisible] = useState(false)
  const resolverRef = useRef<((ok: boolean) => void) | null>(null)
  const pendingPromptRef = useRef<Promise<boolean> | null>(null)

  const prompt = useCallback((): Promise<boolean> => {
    if (pendingPromptRef.current) return pendingPromptRef.current
    setVisible(true)
    pendingPromptRef.current = new Promise<boolean>((resolve) => {
      resolverRef.current = resolve
    })
    return pendingPromptRef.current
  }, [])

  const onVerified = useCallback(() => {
    setVisible(false)
    resolverRef.current?.(true)
    resolverRef.current = null
    pendingPromptRef.current = null
  }, [])

  const onCancel = useCallback(() => {
    setVisible(false)
    resolverRef.current?.(false)
    resolverRef.current = null
    pendingPromptRef.current = null
  }, [])

  /**
   * Run a sensitive action. On STEP_UP_REQUIRED, prompt for a TOTP code and
   * retry once. STEP_UP_TOTP_NOT_ENABLED / admin-api-key errors are surfaced
   * to the caller (they cannot be resolved by entering a code). If the user
   * cancels the prompt, a StepUpCancelledError is thrown so callers can
   * distinguish "user changed their mind" from real failures.
   */
  const run = useCallback(
    async <T,>(action: () => Promise<T>): Promise<T> => {
      try {
        return await action()
      } catch (err) {
        if (isStepUpBlocked(err)) throw err
        if (!isStepUpRequired(err)) throw err
        const ok = await prompt()
        if (!ok) throw new StepUpCancelledError()
        // Retry once now that the session holds a step-up grant.
        return await action()
      }
    },
    [prompt]
  )

  return { visible, run, onVerified, onCancel }
}

/** Six-cell TOTP entry dialog bound to a step-up controller. */
export function TotpStepUpDialog({ controller }: { controller: StepUpController }) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)

  const [verifying, setVerifying] = useState(false)
  const [code, setCode] = useState<string[]>(['', '', '', '', '', ''])
  const inputRefs = useRef<Array<HTMLInputElement | null>>([])
  const joined = code.join('')

  useEffect(() => {
    if (controller.visible) {
      setCode(['', '', '', '', '', ''])
      inputRefs.current[0]?.focus()
    }
  }, [controller.visible])

  const submit = async (otp: string) => {
    setVerifying(true)
    try {
      await totpAPI.stepUp(otp)
      setVerifying(false)
      setCode(['', '', '', '', '', ''])
      controller.onVerified()
    } catch (err) {
      setVerifying(false)
      showError((err as { message?: string })?.message || t('stepUp.verifyFailed'))
      setCode(['', '', '', '', '', ''])
      inputRefs.current[0]?.focus()
    }
  }

  useEffect(() => {
    if (joined.length === 6 && !verifying) void submit(joined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [joined, verifying])

  const handleCodeInput = (event: React.ChangeEvent<HTMLInputElement>, index: number) => {
    const value = event.target.value.replace(/\D/g, '')
    if (!value) {
      setCode((prev) => prev.map((entry, i) => (i === index ? '' : entry)))
      return
    }
    setCode((prev) => {
      const next = [...prev]
      for (let i = 0; i < value.length && index + i < 6; i += 1) {
        next[index + i] = value.charAt(i)
      }
      return next
    })
    const nextIndex = Math.min(index + value.length, 5)
    inputRefs.current[nextIndex]?.focus()
  }

  const handleKeydown = (event: React.KeyboardEvent<HTMLInputElement>, index: number) => {
    if (event.key === 'Backspace' && !code[index] && index > 0) {
      inputRefs.current[index - 1]?.focus()
    }
    if (event.key === 'ArrowLeft' && index > 0) inputRefs.current[index - 1]?.focus()
    if (event.key === 'ArrowRight' && index < 5) inputRefs.current[index + 1]?.focus()
  }

  const handlePaste = (event: React.ClipboardEvent<HTMLInputElement>) => {
    event.preventDefault()
    const digits = event.clipboardData.getData('text').replace(/\D/g, '').slice(0, 6)
    if (!digits) return
    const next = ['', '', '', '', '', '']
    for (let i = 0; i < digits.length; i += 1) next[i] = digits.charAt(i)
    setCode(next)
    inputRefs.current[Math.min(digits.length, 5)]?.focus()
  }

  if (!controller.visible) return null

  return (
    <div className="fixed inset-0 z-[60] overflow-y-auto">
      <div className="flex min-h-full items-center justify-center p-4">
        <div className="fixed inset-0 transition-opacity surface-overlay" onClick={controller.onCancel} />

        <div className="relative w-full max-w-md transform rounded-xl p-6 shadow-xl transition-all surface-panel">
          <div className="mb-6 text-center">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-primary-100/[0.15] dark:bg-primary-900/30">
              <svg className="h-6 w-6 text-primary-600 dark:text-primary-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                <path strokeLinecap="round" strokeLinejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
              </svg>
            </div>
            <h3 className="mt-4 text-xl font-semibold text-gray-900 dark:text-white">{t('stepUp.title')}</h3>
            <p className="mt-2 text-sm surface-muted">{t('stepUp.hint')}</p>
          </div>

          <div className="mb-6">
            <div className="flex justify-center gap-1.5 sm:gap-2">
              {Array.from({ length: 6 }).map((_, index) => (
                <input
                  key={index}
                  ref={(element) => {
                    inputRefs.current[index] = element
                  }}
                  type="text"
                  maxLength={1}
                  inputMode="numeric"
                  pattern="[0-9]"
                  autoComplete="off"
                  disabled={verifying}
                  value={code[index]}
                  onChange={(event) => handleCodeInput(event, index)}
                  onKeyDown={(event) => handleKeydown(event, index)}
                  onPaste={handlePaste}
                  className="h-12 min-w-0 max-w-10 flex-1 rounded-lg border border-gray-300 text-center text-lg font-semibold focus:border-primary-500 focus:ring-primary-500 dark:border-dark-600 dark:surface-inset"
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

          <button type="button" className="btn btn-secondary w-full" disabled={verifying} onClick={controller.onCancel}>
            {t('common.cancel')}
          </button>
        </div>
      </div>
    </div>
  )
}

/**
 * 检测是否支持 Clipboard API（需要安全上下文：HTTPS/localhost）
 */
function isClipboardSupported(): boolean {
  return !!(navigator.clipboard && window.isSecureContext)
}

/**
 * 降级方案：使用 textarea + execCommand
 * 使用 textarea 而非 input，以正确处理多行文本
 */
function fallbackCopy(text: string): boolean {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', 'true')
  textarea.style.cssText = 'position:fixed;left:0;top:0;width:1px;height:1px;opacity:0;pointer-events:none'
  document.body.appendChild(textarea)
  textarea.focus({ preventScroll: true })
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)
  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    document.body.removeChild(textarea)
  }
}

export function useClipboard() {
  const { t } = useTranslation()
  const showSuccess = useAppStore((state) => state.showSuccess)
  const showError = useAppStore((state) => state.showError)
  const [copied, setCopied] = useState(false)

  const copyToClipboard = useCallback(
    async (text: string, successMessage?: string): Promise<boolean> => {
      if (!text) return false

      let success = false
      if (isClipboardSupported()) {
        try {
          await navigator.clipboard.writeText(text)
          success = true
        } catch {
          success = fallbackCopy(text)
        }
      } else {
        success = fallbackCopy(text)
      }

      if (success) {
        setCopied(true)
        showSuccess(successMessage || t('common.copiedToClipboard'))
        setTimeout(() => setCopied(false), 2000)
      } else {
        showError(t('common.copyFailed'))
      }

      return success
    },
    [showError, showSuccess]
  )

  return { copied, copyToClipboard }
}
