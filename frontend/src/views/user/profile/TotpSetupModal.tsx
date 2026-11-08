import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { QRCodeSVG } from 'qrcode.react'
import { totpAPI } from '@/api/totp'
import { toast } from '@/components/ui'
import { extractApiErrorMessage } from '@/utils/apiError'
import { Modal } from '@/components/ui'
import type { TotpSetupResponse } from '@/types'

export interface TotpSetupModalProps {
  open: boolean
  onClose?: () => void
  onSuccess?: () => void
}

// Step: 0 = verify identity, 1 = QR code, 2 = verify TOTP code
export function TotpSetupModal({ open, onClose, onSuccess }: TotpSetupModalProps) {
  const { t } = useTranslation()
  const [step, setStep] = useState(0)
  const [methodLoading, setMethodLoading] = useState(true)
  const [verificationMethod, setVerificationMethod] = useState<'email' | 'password'>('password')
  const [verifyForm, setVerifyForm] = useState({ emailCode: '', password: '' })
  const [sendingCode, setSendingCode] = useState(false)
  const [codeCooldown, setCodeCooldown] = useState(0)
  const cooldownTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const [setupLoading, setSetupLoading] = useState(false)
  const [setupData, setSetupData] = useState<TotpSetupResponse | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [code, setCode] = useState<string[]>(['', '', '', '', '', ''])
  const inputRefs = useRef<Array<HTMLInputElement | null>>([])

  useEffect(() => {
    let disposed = false
    setMethodLoading(true)
    void (async () => {
      try {
        const method = await totpAPI.getVerificationMethod()
        if (disposed) return
        setVerificationMethod(method.method)
      } catch (err: unknown) {
        toast.error(extractApiErrorMessage(err, t('common.error')))
        onClose?.()
      } finally {
        if (!disposed) setMethodLoading(false)
      }
    })()
    return () => {
      disposed = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    return () => {
      if (cooldownTimer.current) {
        clearInterval(cooldownTimer.current)
        cooldownTimer.current = null
      }
    }
  }, [])

  const stepDescription = (() => {
    switch (step) {
      case 0:
        return verificationMethod === 'email'
          ? t('profile.totp.verifyEmailFirst')
          : t('profile.totp.verifyPasswordFirst')
      case 1:
        return t('profile.totp.setupStep1')
      case 2:
        return t('profile.totp.setupStep2')
      default:
        return ''
    }
  })()

  const canProceedFromVerify =
    verificationMethod === 'email' ? verifyForm.emailCode.length === 6 : verifyForm.password.length > 0

  function handleCodeInput(event: React.ChangeEvent<HTMLInputElement>, index: number) {
    const value = event.target.value.replace(/[^0-9]/g, '')
    setCode((previous) => {
      const next = [...previous]
      next[index] = value
      return next
    })
    if (value && index < 5) {
      inputRefs.current[index + 1]?.focus()
    }
  }

  function handleKeydown(event: React.KeyboardEvent<HTMLInputElement>, index: number) {
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

  function handlePaste(event: React.ClipboardEvent<HTMLInputElement>) {
    event.preventDefault()
    const pastedData = event.clipboardData?.getData('text') || ''
    const digits = pastedData
      .replace(/[^0-9]/g, '')
      .slice(0, 6)
      .split('')

    setCode((previous) => {
      const next = ['', '', '', '', '', '']
      digits.forEach((digit, index) => {
        next[index] = digit
      })
      void previous
      return next
    })

    const focusIndex = Math.min(digits.length, 5)
    inputRefs.current[focusIndex]?.focus()
  }

  async function copySecret() {
    if (setupData) {
      try {
        await navigator.clipboard.writeText(setupData.secret)
        toast.success(t('common.copied'))
      } catch {
        toast.error(t('common.copyFailed'))
      }
    }
  }

  async function handleSendCode() {
    setSendingCode(true)
    try {
      await totpAPI.sendVerifyCode()
      toast.success(t('profile.totp.codeSent'))
      // Start cooldown
      setCodeCooldown(60)
      if (cooldownTimer.current) {
        clearInterval(cooldownTimer.current)
        cooldownTimer.current = null
      }
      cooldownTimer.current = setInterval(() => {
        setCodeCooldown((value) => {
          if (value <= 1) {
            if (cooldownTimer.current) {
              clearInterval(cooldownTimer.current)
              cooldownTimer.current = null
            }
            return 0
          }
          return value - 1
        })
      }, 1000)
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('profile.totp.sendCodeFailed')))
    } finally {
      setSendingCode(false)
    }
  }

  async function handleVerifyAndSetup() {
    setSetupLoading(true)
    try {
      const request =
        verificationMethod === 'email' ? { email_code: verifyForm.emailCode } : { password: verifyForm.password }
      const data = await totpAPI.initiateSetup(request)
      setSetupData(data)
      setStep(1)
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('profile.totp.setupFailed')))
    } finally {
      setSetupLoading(false)
    }
  }

  async function handleVerify() {
    const totpCode = code.join('')
    if (totpCode.length !== 6 || !setupData) return

    setVerifying(true)
    try {
      await totpAPI.enable({ totp_code: totpCode, setup_token: setupData.setup_token })
      toast.success(t('profile.totp.enableSuccess'))
      onSuccess?.()
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('profile.totp.verifyFailed')))
      setCode(['', '', '', '', '', ''])
      inputRefs.current[0]?.focus()
    } finally {
      setVerifying(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={t('profile.totp.setupTitle')} size="sm" showClose={false}>
      <div className="mb-6 text-center">
        <h3 className="text-xl font-semibold text-gray-900 dark:text-white">{t('profile.totp.setupTitle')}</h3>
        <p className="mt-2 text-sm surface-muted">{stepDescription}</p>
      </div>

      {step === 0 && (
        <div className="space-y-6">
          {methodLoading ? (
            <div className="flex items-center justify-center py-8">
              <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-500" />
            </div>
          ) : (
            <>
              {verificationMethod === 'email' ? (
                <div className="space-y-4">
                  <div>
                    <label className="input-label">{t('profile.totp.emailCode')}</label>
                    <div className="flex gap-2">
                      <input
                        value={verifyForm.emailCode}
                        onChange={(event) => setVerifyForm({ ...verifyForm, emailCode: event.target.value })}
                        type="text"
                        maxLength={6}
                        inputMode="numeric"
                        className="input flex-1"
                        placeholder={t('profile.totp.enterEmailCode')}
                      />
                      <button
                        type="button"
                        className="btn btn-secondary whitespace-nowrap"
                        disabled={sendingCode || codeCooldown > 0}
                        onClick={() => void handleSendCode()}
                      >
                        {codeCooldown > 0
                          ? `${codeCooldown}s`
                          : sendingCode
                            ? t('common.sending')
                            : t('profile.totp.sendCode')}
                      </button>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="space-y-4">
                  <div>
                    <label className="input-label">{t('profile.currentPassword')}</label>
                    <input
                      value={verifyForm.password}
                      onChange={(event) => setVerifyForm({ ...verifyForm, password: event.target.value })}
                      type="password"
                      autoComplete="current-password"
                      className="input"
                      placeholder={t('profile.totp.enterPassword')}
                    />
                  </div>
                </div>
              )}

              <div className="flex justify-end gap-3 pt-4">
                <button type="button" className="btn btn-secondary" onClick={onClose}>
                  {t('common.cancel')}
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={!canProceedFromVerify || setupLoading}
                  onClick={() => void handleVerifyAndSetup()}
                >
                  {setupLoading ? t('common.loading') : t('common.next')}
                </button>
              </div>
            </>
          )}
        </div>
      )}

      {step === 1 && (
        <div className="space-y-6">
          {setupData && (
            <>
              <div className="flex justify-center">
                <div className="surface-opaque max-w-full rounded-lg border border-gray-200 p-4 dark:border-dark-600">
                  <QRCodeSVG value={setupData.qr_code_url} size={192} level="M" />
                </div>
              </div>

              <div className="text-center">
                <p className="mb-2 text-sm surface-muted">{t('profile.totp.manualEntry')}</p>
                <div className="flex min-w-0 items-center justify-center gap-2">
                  <code className="surface-inset min-w-0 break-all rounded px-3 py-2 font-mono text-sm">
                    {setupData.secret}
                  </code>
                  <button
                    type="button"
                    className="surface-control surface-muted shrink-0 rounded p-1.5 hover:surface-hover"
                    onClick={() => void copySecret()}
                  >
                    <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="1.5">
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M15.666 3.888A2.25 2.25 0 0013.5 2.25h-3c-1.03 0-1.9.693-2.166 1.638m7.332 0c.055.194.084.4.084.612v0a.75.75 0 01-.75.75H9a.75.75 0 01-.75-.75v0c0-.212.03-.418.084-.612m7.332 0c.646.049 1.288.11 1.927.184 1.1.128 1.907 1.077 1.907 2.185V19.5a2.25 2.25 0 01-2.25 2.25H6.75A2.25 2.25 0 014.5 19.5V6.257c0-1.108.806-2.057 1.907-2.185a48.208 48.208 0 011.927-.184"
                      />
                    </svg>
                  </button>
                </div>
              </div>
            </>
          )}

          <div className="flex justify-end gap-3 pt-4">
            <button type="button" className="btn btn-secondary" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="button" className="btn btn-primary" disabled={!setupData} onClick={() => setStep(2)}>
              {t('common.next')}
            </button>
          </div>
        </div>
      )}

      {step === 2 && (
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void handleVerify()
          }}
          className="space-y-6"
        >
          <div className="mb-6">
            <label className="input-label mb-3 block text-center">{t('profile.totp.enterCode')}</label>
            <div className="flex justify-center gap-1.5 sm:gap-2">
              {Array.from({ length: 6 }, (_, index) => (
                <input
                  key={index}
                  ref={(el) => {
                    inputRefs.current[index] = el
                  }}
                  value={code[index]}
                  onChange={(event) => handleCodeInput(event, index)}
                  onKeyDown={(event) => handleKeydown(event, index)}
                  onPaste={handlePaste}
                  type="text"
                  maxLength={1}
                  inputMode="numeric"
                  pattern="[0-9]"
                  className="dark:surface-inset h-12 min-w-0 max-w-10 flex-1 rounded-lg border border-gray-300 text-center text-lg font-semibold focus:border-primary-500 focus:ring-primary-500 dark:border-dark-600"
                />
              ))}
            </div>
          </div>

          <div className="flex justify-end gap-3">
            <button type="button" className="btn btn-secondary" onClick={() => setStep(1)}>
              {t('common.back')}
            </button>
            <button type="submit" className="btn btn-primary" disabled={verifying || code.join('').length !== 6}>
              {verifying ? t('common.verifying') : t('profile.totp.verify')}
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}

export default TotpSetupModal
