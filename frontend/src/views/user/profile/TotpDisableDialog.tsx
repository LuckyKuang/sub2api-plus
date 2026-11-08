import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { totpAPI } from '@/api/totp'
import { Modal, toast } from '@/components/ui'
import { extractApiErrorMessage } from '@/utils/apiError'

export interface TotpDisableDialogProps {
  open: boolean
  onClose?: () => void
  onSuccess?: () => void
}

export function TotpDisableDialog({ open, onClose, onSuccess }: TotpDisableDialogProps) {
  const { t } = useTranslation()
  const [methodLoading, setMethodLoading] = useState(true)
  const [verificationMethod, setVerificationMethod] = useState<'email' | 'password'>('password')
  const [loading, setLoading] = useState(false)
  const [sendingCode, setSendingCode] = useState(false)
  const [codeCooldown, setCodeCooldown] = useState(0)
  const [form, setForm] = useState({ emailCode: '', password: '' })
  const cooldownTimer = useRef<ReturnType<typeof setInterval> | null>(null)

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

  const canSubmit = verificationMethod === 'email' ? form.emailCode.length === 6 : form.password.length > 0

  async function handleSendCode() {
    setSendingCode(true)
    try {
      await totpAPI.sendVerifyCode()
      toast.success(t('profile.totp.codeSent'))
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

  async function handleDisable() {
    if (!canSubmit) return
    setLoading(true)
    try {
      const request =
        verificationMethod === 'email' ? { email_code: form.emailCode } : { password: form.password }
      await totpAPI.disable(request)
      toast.success(t('profile.totp.disableSuccess'))
      onSuccess?.()
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('profile.totp.disableFailed')))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title={t('profile.totp.disableTitle')} size="sm" showClose={false}>
      <div className="mb-6">
        <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-red-100/[0.15] dark:bg-red-900/30">
          <svg
            className="h-6 w-6 text-red-600 dark:text-red-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z"
            />
          </svg>
        </div>
        <h3 className="mt-4 text-center text-xl font-semibold text-gray-900 dark:text-white">
          {t('profile.totp.disableTitle')}
        </h3>
        <p className="mt-2 text-center text-sm surface-muted">{t('profile.totp.disableWarning')}</p>
      </div>

      {methodLoading ? (
        <div className="flex items-center justify-center py-8">
          <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-500" />
        </div>
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void handleDisable()
          }}
          className="space-y-4"
        >
          {verificationMethod === 'email' ? (
            <div>
              <label className="input-label">{t('profile.totp.emailCode')}</label>
              <div className="flex gap-2">
                <input
                  value={form.emailCode}
                  onChange={(event) => setForm({ ...form, emailCode: event.target.value })}
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
                  {codeCooldown > 0 ? `${codeCooldown}s` : sendingCode ? t('common.sending') : t('profile.totp.sendCode')}
                </button>
              </div>
            </div>
          ) : (
            <div>
              <label htmlFor="totp-disable-password" className="input-label">
                {t('profile.currentPassword')}
              </label>
              <input
                id="totp-disable-password"
                value={form.password}
                onChange={(event) => setForm({ ...form, password: event.target.value })}
                type="password"
                autoComplete="current-password"
                className="input"
                placeholder={t('profile.totp.enterPassword')}
              />
            </div>
          )}

          <div className="flex justify-end gap-3 pt-4">
            <button type="button" className="btn btn-secondary" onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="submit" className="btn btn-danger" disabled={loading || !canSubmit}>
              {loading ? t('common.processing') : t('profile.totp.confirmDisable')}
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}

export default TotpDisableDialog
