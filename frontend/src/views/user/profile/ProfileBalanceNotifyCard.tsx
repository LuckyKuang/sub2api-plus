import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { userAPI } from '@/api/user'
import { toast } from '@/components/ui'
import { extractApiErrorMessage } from '@/utils/apiError'
import { cn } from '@/lib/utils'
import { useUserView } from '../useUserView'
import type { NotifyEmailEntry } from '@/types'

export interface ProfileBalanceNotifyCardProps {
  enabled: boolean
  threshold: number | null
  extraEmails: NotifyEmailEntry[]
  systemDefaultThreshold: number
  userEmail: string
  className?: string
}

const maxTotalEmails = 3

interface PendingEmail {
  email: string
  codeSent: boolean
  code: string
  sending: boolean
  verifying: boolean
  countdown: number
  timer: ReturnType<typeof setInterval> | null
}

export function ProfileBalanceNotifyCard({
  enabled,
  threshold,
  extraEmails,
  systemDefaultThreshold,
  userEmail,
  className
}: ProfileBalanceNotifyCardProps) {
  const { t } = useTranslation()
  const authStore = useUserView()
  const [notifyEnabled, setNotifyEnabled] = useState(enabled)
  const [customThreshold, setCustomThreshold] = useState<number | null>(threshold)
  const [emailEntries, setEmailEntries] = useState<NotifyEmailEntry[]>([...extraEmails])
  const [pendingEmails, setPendingEmails] = useState<PendingEmail[]>([])
  const [newEmail, setNewEmail] = useState('')
  const [savingThreshold, setSavingThreshold] = useState(false)
  const [verifyingEmail, setVerifyingEmail] = useState('')
  const [verifyCode, setVerifyCode] = useState('')
  const [verifyingSaved, setVerifyingSaved] = useState(false)
  const [sendingSavedCode, setSendingSavedCode] = useState(false)
  const [verifyCountdown, setVerifyCountdown] = useState(0)
  const disposed = useRef(false)
  const verifyTimer = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    disposed.current = false
    return () => {
      disposed.current = true
      for (const pe of pendingEmails) {
        if (pe.timer) clearInterval(pe.timer)
      }
      if (verifyTimer.current) clearInterval(verifyTimer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    setNotifyEnabled(enabled)
  }, [enabled])

  useEffect(() => {
    setCustomThreshold(threshold)
  }, [threshold])

  useEffect(() => {
    setEmailEntries([...extraEmails])
  }, [extraEmails])

  // When list is empty on mount, pre-fill the add input with user's email
  useEffect(() => {
    if (emailEntries.length === 0 && userEmail) setNewEmail(userEmail)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const canAddMore = emailEntries.length + pendingEmails.length < maxTotalEmails

  const handleToggle = async () => {
    try {
      const updated = await userAPI.updateProfile({ balance_notify_enabled: notifyEnabled })
      authStore.user = updated
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
      setNotifyEnabled(!notifyEnabled)
    }
  }

  const handleThresholdUpdate = async () => {
    setSavingThreshold(true)
    try {
      const nextThreshold = customThreshold && customThreshold > 0 ? customThreshold : 0
      const updated = await userAPI.updateProfile({ balance_notify_threshold: nextThreshold })
      authStore.user = updated
      toast.success(t('common.saved'))
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    } finally {
      setSavingThreshold(false)
    }
  }

  async function handleEmailToggle(entry: NotifyEmailEntry) {
    const newDisabled = !entry.disabled
    try {
      const updated = await userAPI.toggleNotifyEmail(entry.email, newDisabled)
      authStore.user = updated
      setEmailEntries([...updated.balance_notify_extra_emails])
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    }
  }

  function addPendingEmail() {
    const email = newEmail.trim()
    if (!email) return
    // Check duplicates
    const isDuplicate =
      emailEntries.some((e) => e.email.toLowerCase() === email.toLowerCase()) ||
      pendingEmails.some((p) => p.email.toLowerCase() === email.toLowerCase())
    if (isDuplicate) {
      toast.error(t('profile.balanceNotify.emailDuplicate'))
      return
    }
    setPendingEmails([
      ...pendingEmails,
      { email, codeSent: false, code: '', sending: false, verifying: false, countdown: 0, timer: null }
    ])
    setNewEmail('')
  }

  async function sendCodeFor(idx: number) {
    const pe = pendingEmails[idx]
    if (!pe) return
    pe.sending = true
    setPendingEmails([...pendingEmails])
    try {
      await userAPI.sendNotifyEmailCode(pe.email)
      if (disposed.current || !pendingEmails.includes(pe)) return
      pe.codeSent = true
      pe.countdown = 60
      pe.timer = setInterval(() => {
        pe.countdown -= 1
        if (pe.countdown <= 0 && pe.timer) {
          clearInterval(pe.timer)
          pe.timer = null
        }
        setPendingEmails([...pendingEmails])
      }, 1000)
      toast.success(t('profile.balanceNotify.codeSent'))
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    } finally {
      pe.sending = false
      setPendingEmails([...pendingEmails])
    }
  }

  async function verifyPending(idx: number) {
    const pe = pendingEmails[idx]
    if (!pe || !pe.code || pe.code.length !== 6) return
    pe.verifying = true
    setPendingEmails([...pendingEmails])
    try {
      await userAPI.verifyNotifyEmail(pe.email, pe.code)
      if (pe.timer) clearInterval(pe.timer)
      setPendingEmails(pendingEmails.filter((entry) => entry !== pe))
      toast.success(t('profile.balanceNotify.verifySuccess'))
      const updated = await userAPI.getProfile()
      authStore.user = updated
      setEmailEntries([...updated.balance_notify_extra_emails])
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    } finally {
      pe.verifying = false
      setPendingEmails([...pendingEmails])
    }
  }

  const handleRemoveEmail = async (email: string) => {
    try {
      await userAPI.removeNotifyEmail(email)
      toast.success(t('profile.balanceNotify.removeSuccess'))
      const updated = await userAPI.getProfile()
      authStore.user = updated
      setEmailEntries([...updated.balance_notify_extra_emails])
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    }
  }

  async function sendCodeForSaved(email: string) {
    setSendingSavedCode(true)
    try {
      await userAPI.sendNotifyEmailCode(email)
      if (disposed.current) return
      setVerifyingEmail(email)
      setVerifyCode('')
      setVerifyCountdown(60)
      if (verifyTimer.current) clearInterval(verifyTimer.current)
      verifyTimer.current = setInterval(() => {
        setVerifyCountdown((value) => {
          if (value <= 1 && verifyTimer.current) {
            clearInterval(verifyTimer.current)
            verifyTimer.current = null
            return 0
          }
          return value - 1
        })
      }, 1000)
      toast.success(t('profile.balanceNotify.codeSent'))
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    } finally {
      setSendingSavedCode(false)
    }
  }

  async function verifySavedEmail(email: string) {
    if (!verifyCode || verifyCode.length !== 6) return
    setVerifyingSaved(true)
    try {
      await userAPI.verifyNotifyEmail(email, verifyCode)
      setVerifyingEmail('')
      setVerifyCode('')
      if (verifyTimer.current) {
        clearInterval(verifyTimer.current)
        verifyTimer.current = null
      }
      toast.success(t('profile.balanceNotify.verifySuccess'))
      const updated = await userAPI.getProfile()
      authStore.user = updated
      setEmailEntries([...updated.balance_notify_extra_emails])
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    } finally {
      setVerifyingSaved(false)
    }
  }

  return (
    <div className={cn('card', className)}>
      <div className="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
        <h2 className="text-lg font-medium text-gray-900 dark:text-white">{t('profile.balanceNotify.title')}</h2>
        <p className="mt-1 text-sm surface-muted">{t('profile.balanceNotify.description')}</p>
      </div>
      <div className="space-y-6 px-6 py-6">
        <div className="flex items-center justify-between">
          <label className="input-label mb-0">{t('profile.balanceNotify.enabled')}</label>
          <button
            type="button"
            role="switch"
            aria-checked={notifyEnabled}
            aria-label={t('profile.balanceNotify.enabled')}
            className="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-primary-500/50"
            style={{ backgroundColor: notifyEnabled ? 'rgb(var(--console-accent-500, 20 184 166))' : undefined }}
            onClick={() => void handleToggle()}
          >
            <span
              className={`inline-block h-5 w-5 transform rounded-full bg-white shadow-sm transition-transform duration-200 ${
                notifyEnabled ? 'translate-x-[22px]' : 'translate-x-0.5'
              }`}
            />
          </button>
        </div>

        {notifyEnabled && (
          <>
            <div>
              <label className="input-label">
                {t('profile.balanceNotify.threshold')}
                <span className="ml-2 text-xs surface-muted">{t('profile.balanceNotify.thresholdHint')}</span>
              </label>
              <div className="flex items-center gap-2">
                <span className="surface-muted">$</span>
                <input
                  value={customThreshold ?? ''}
                  onChange={(event) => setCustomThreshold(event.target.value === '' ? null : Number(event.target.value))}
                  type="number"
                  min="0"
                  step="0.01"
                  className="input flex-1"
                  placeholder={
                    systemDefaultThreshold > 0
                      ? `${t('profile.balanceNotify.systemDefault')} $${systemDefaultThreshold}`
                      : t('profile.balanceNotify.thresholdPlaceholder')
                  }
                />
                <button
                  onClick={() => void handleThresholdUpdate()}
                  disabled={savingThreshold}
                  className="btn btn-primary btn-sm whitespace-nowrap"
                >
                  {savingThreshold ? t('common.saving') : t('common.save')}
                </button>
              </div>
            </div>

            <div>
              <label className="input-label">{t('profile.balanceNotify.extraEmails')}</label>
              <p className="mb-2 text-xs text-yellow-600 dark:text-yellow-400">
                {t('profile.balanceNotify.extraEmailsHint')}
              </p>

              {emailEntries.length > 0 && (
                <div className="mb-3 space-y-2">
                  {emailEntries.map((entry, idx) => (
                    <div
                      key={idx}
                      className="surface-inset flex flex-col items-stretch justify-between gap-2 rounded-lg px-3 py-2 sm:flex-row sm:items-center"
                    >
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <button
                          type="button"
                          role="switch"
                          aria-checked={!entry.disabled}
                          aria-label={entry.email}
                          className="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors"
                          style={{
                            backgroundColor: !entry.disabled
                              ? 'rgb(var(--console-accent-500, 20 184 166))'
                              : undefined
                          }}
                          onClick={() => void handleEmailToggle(entry)}
                        >
                          <span
                            className={`inline-block h-4 w-4 transform rounded-full bg-white shadow-sm transition-transform ${
                              !entry.disabled ? 'translate-x-[18px]' : 'translate-x-0.5'
                            }`}
                          />
                        </button>
                        <span className="truncate text-sm surface-muted">{entry.email}</span>
                      </div>
                      <div className="flex flex-wrap items-center gap-2 sm:shrink-0">
                        {!entry.verified ? (
                          verifyingEmail === entry.email ? (
                            <>
                              <input
                                value={verifyCode}
                                onChange={(event) => setVerifyCode(event.target.value)}
                                type="text"
                                maxLength={6}
                                className="w-20 rounded border border-gray-300 px-2 py-1 text-xs dark:border-dark-500 dark:surface-inset"
                                placeholder={t('profile.balanceNotify.codePlaceholder')}
                              />
                              <button
                                onClick={() => void verifySavedEmail(entry.email)}
                                disabled={!verifyCode || verifyCode.length !== 6 || verifyingSaved}
                                className="surface-control text-xs text-primary-600 hover:text-primary-700"
                              >
                                {t('profile.balanceNotify.verify')}
                              </button>
                              {verifyCountdown > 0 ? (
                                <span className="text-xs surface-muted">{verifyCountdown}s</span>
                              ) : (
                                <button
                                  onClick={() => void sendCodeForSaved(entry.email)}
                                  disabled={sendingSavedCode}
                                  className="surface-control surface-muted text-xs hover:text-gray-700"
                                >
                                  {t('profile.balanceNotify.resend')}
                                </button>
                              )}
                              <button
                                onClick={() => setVerifyingEmail('')}
                                className="surface-control surface-muted text-xs hover:text-gray-600"
                              >
                                {t('common.cancel')}
                              </button>
                            </>
                          ) : (
                            <>
                              <button
                                onClick={() => void sendCodeForSaved(entry.email)}
                                disabled={sendingSavedCode}
                                className="surface-control text-xs text-primary-600 hover:text-primary-700"
                              >
                                {t('profile.balanceNotify.verify')}
                              </button>
                              <span className="text-xs text-yellow-500">
                                {t('profile.balanceNotify.unverified')}
                              </span>
                            </>
                          )
                        ) : (
                          <span className="text-xs text-green-500">{t('profile.balanceNotify.verified')}</span>
                        )}
                        <button
                          onClick={() => void handleRemoveEmail(entry.email)}
                          className="surface-control text-xs text-red-500 hover:text-red-700"
                        >
                          {t('profile.balanceNotify.removeEmail')}
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {pendingEmails.length > 0 && (
                <div className="mb-3 space-y-2">
                  {pendingEmails.map((pe, idx) => (
                    <div
                      key={pe.email}
                      data-testid="pending-notify-email"
                      className="flex flex-col items-stretch gap-2 rounded-lg border border-yellow-200 bg-yellow-50/[0.15] px-3 py-2 dark:border-yellow-800 dark:bg-yellow-900/10 sm:flex-row sm:items-center"
                    >
                      <span className="min-w-0 flex-1 break-all text-sm surface-muted">{pe.email}</span>
                      {!pe.codeSent ? (
                        <div className="flex flex-wrap items-center gap-1">
                          <button
                            onClick={() => void sendCodeFor(idx)}
                            disabled={pe.sending}
                            className="surface-control text-xs text-primary-600 hover:text-primary-700"
                          >
                            {t('profile.balanceNotify.sendCode')}
                          </button>
                          <button
                            onClick={() => setPendingEmails(pendingEmails.filter((entry) => entry !== pe))}
                            className="surface-control ml-1 text-xs text-red-500 hover:text-red-700"
                          >
                            {t('profile.balanceNotify.removeEmail')}
                          </button>
                        </div>
                      ) : (
                        <div className="flex flex-wrap items-center gap-1">
                          <input
                            value={pe.code}
                            onChange={(event) => {
                              pe.code = event.target.value
                              setPendingEmails([...pendingEmails])
                            }}
                            type="text"
                            maxLength={6}
                            className="w-20 rounded border border-gray-300 px-2 py-1 text-xs dark:border-dark-500 dark:surface-inset"
                            placeholder={t('profile.balanceNotify.codePlaceholder')}
                          />
                          <button
                            onClick={() => void verifyPending(idx)}
                            disabled={!pe.code || pe.code.length !== 6 || pe.verifying}
                            className="surface-control text-xs text-primary-600 hover:text-primary-700"
                          >
                            {t('profile.balanceNotify.verify')}
                          </button>
                          {pe.countdown > 0 ? (
                            <span className="text-xs surface-muted">{pe.countdown}s</span>
                          ) : (
                            <button
                              onClick={() => void sendCodeFor(idx)}
                              disabled={pe.sending}
                              className="surface-control surface-muted text-xs hover:text-gray-700"
                            >
                              {t('profile.balanceNotify.resend')}
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}

              {canAddMore ? (
                <div className="flex gap-2">
                  <input
                    value={newEmail}
                    onChange={(event) => setNewEmail(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') addPendingEmail()
                    }}
                    type="email"
                    className="input flex-1"
                    placeholder={t('profile.balanceNotify.emailPlaceholder')}
                  />
                  <button onClick={addPendingEmail} disabled={!newEmail} className="btn btn-secondary whitespace-nowrap">
                    {t('common.add')}
                  </button>
                </div>
              ) : (
                <p className="text-xs surface-muted">{t('profile.balanceNotify.maxEmailsReached')}</p>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

export default ProfileBalanceNotifyCard
