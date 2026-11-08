import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '@/components/ui'
import { getPublicSettings, sendPendingOAuthVerifyCode } from '@/api/auth'
import { CaptchaChallenge, type CaptchaChallengeHandle, type ActionCaptchaResult } from './CaptchaChallenge'

export interface PendingOAuthCreateAccountPayload {
  email: string
  password: string
  verifyCode: string
  turnstileToken?: string
  tencentCaptchaTicket?: string
  tencentCaptchaRandstr?: string
  invitationCode?: string
}

export interface PendingOAuthCreateAccountFormProps {
  initialEmail: string
  testIdPrefix: string
  isSubmitting: boolean
  errorMessage?: string
  onSubmit?: (payload: PendingOAuthCreateAccountPayload) => void
  onSwitchToBind?: (email: string) => void
}

export function PendingOAuthCreateAccountForm({
  initialEmail,
  testIdPrefix,
  isSubmitting,
  errorMessage = '',
  onSubmit,
  onSwitchToBind
}: PendingOAuthCreateAccountFormProps) {
  const { t } = useTranslation()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [verifyCode, setVerifyCode] = useState('')
  const [invitationCode, setInvitationCode] = useState('')
  const [isSendingCode, setIsSendingCode] = useState(false)
  const [sendCodeError, setSendCodeError] = useState('')
  const [sendCodeSuccess, setSendCodeSuccess] = useState(false)
  const [countdown, setCountdown] = useState(0)

  const [invitationCodeEnabled, setInvitationCodeEnabled] = useState(false)
  const [emailVerifyEnabled, setEmailVerifyEnabled] = useState(true)
  const [turnstileEnabled, setTurnstileEnabled] = useState(false)
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('')
  const [tencentCaptchaEnabled, setTencentCaptchaEnabled] = useState(false)
  const [tencentCaptchaAppId, setTencentCaptchaAppId] = useState('')
  const [tencentCaptchaRegion, setTencentCaptchaRegion] = useState('cn')
  const [aliyunCaptchaEnabled, setAliyunCaptchaEnabled] = useState(false)
  const [aliyunCaptchaSceneId, setAliyunCaptchaSceneId] = useState('')
  const [aliyunCaptchaPrefix, setAliyunCaptchaPrefix] = useState('')
  const [aliyunCaptchaRegion, setAliyunCaptchaRegion] = useState('cn')

  const [turnstileToken, setTurnstileToken] = useState('')
  const [tencentCaptchaRandstr, setTencentCaptchaRandstr] = useState('')
  const captchaRef = useRef<CaptchaChallengeHandle | null>(null)

  const aliyunCaptchaReady =
    aliyunCaptchaEnabled && Boolean(aliyunCaptchaSceneId) && Boolean(aliyunCaptchaPrefix)
  // 动作触发式验证码（腾讯/阿里云）：发送验证码、提交时弹窗验证
  const actionCaptchaEnabled =
    (tencentCaptchaEnabled && Boolean(tencentCaptchaAppId)) || aliyunCaptchaReady
  const captchaEnabled =
    (turnstileEnabled && Boolean(turnstileSiteKey)) || actionCaptchaEnabled

  const countdownTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const disposedRef = useRef(false)

  useEffect(() => {
    setEmail(initialEmail || '')
  }, [initialEmail])

  useEffect(() => {
    if (sendCodeError) {
      toast.error(sendCodeError)
    }
  }, [sendCodeError])

  useEffect(() => {
    if (!errorMessage) return
    toast.error(errorMessage)
    if (captchaEnabled) {
      resetTurnstile()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [errorMessage])

  useEffect(() => {
    disposedRef.current = false
    return () => {
      disposedRef.current = true
      if (countdownTimerRef.current) {
        clearInterval(countdownTimerRef.current)
        countdownTimerRef.current = null
      }
    }
  }, [])

  useEffect(() => {
    if (countdown <= 0) return
    const timer = setInterval(() => {
      setCountdown((previous) => {
        if (previous <= 1) {
          clearInterval(timer)
          return 0
        }
        return previous - 1
      })
    }, 1000)
    countdownTimerRef.current = timer
    return () => {
      clearInterval(timer)
      countdownTimerRef.current = null
    }
  }, [countdown])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const settings = await getPublicSettings()
        if (cancelled) return
        setInvitationCodeEnabled(settings.invitation_code_enabled === true)
        setEmailVerifyEnabled(settings.email_verify_enabled !== false)
        setTurnstileEnabled(settings.turnstile_enabled === true)
        setTurnstileSiteKey(settings.turnstile_site_key || '')
        setTencentCaptchaEnabled(settings.tencent_captcha_enabled === true)
        setTencentCaptchaAppId(settings.tencent_captcha_app_id || '')
        setTencentCaptchaRegion(settings.tencent_captcha_region || 'cn')
        setAliyunCaptchaEnabled(settings.aliyun_captcha_enabled === true)
        setAliyunCaptchaSceneId(settings.aliyun_captcha_scene_id || '')
        setAliyunCaptchaPrefix(settings.aliyun_captcha_prefix || '')
        setAliyunCaptchaRegion(settings.aliyun_captcha_region || 'cn')
      } catch {
        if (cancelled) return
        setInvitationCodeEnabled(false)
        setEmailVerifyEnabled(true)
        setTurnstileEnabled(false)
        setTurnstileSiteKey('')
        setTencentCaptchaEnabled(false)
        setTencentCaptchaAppId('')
        setTencentCaptchaRegion('cn')
        setAliyunCaptchaEnabled(false)
        setAliyunCaptchaSceneId('')
        setAliyunCaptchaPrefix('')
        setAliyunCaptchaRegion('cn')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  function resetTurnstile() {
    setTurnstileToken('')
    setTencentCaptchaRandstr('')
    captchaRef.current?.reset()
  }

  function onTurnstileVerify(token: string, randstr = '') {
    setTurnstileToken(token)
    setTencentCaptchaRandstr(randstr)
    setSendCodeError('')
  }

  function onTurnstileExpire() {
    setTurnstileToken('')
    setTencentCaptchaRandstr('')
    setSendCodeError(t('auth.turnstileExpired'))
  }

  function onTurnstileError() {
    setTurnstileToken('')
    setTencentCaptchaRandstr('')
    setSendCodeError(t('auth.turnstileFailed'))
  }

  async function acquireActionProof(): Promise<boolean> {
    if (!actionCaptchaEnabled) return true

    const proof = await captchaRef.current?.verifyAction()
    if (!proof) return false

    setTurnstileToken(proof.token)
    setTencentCaptchaRandstr(proof.randstr)
    return true
  }

  function getRequestErrorMessage(error: unknown, fallback: string): string {
    const err = error as { message?: string; response?: { data?: { detail?: string; message?: string } } }
    return err.response?.data?.detail || err.response?.data?.message || err.message || fallback
  }

  async function handleSendCode() {
    const trimmedEmail = email.trim()
    if (!trimmedEmail) {
      return
    }

    if (turnstileEnabled && !turnstileToken) {
      setSendCodeError(t('auth.completeVerification'))
      return
    }

    if (!(await acquireActionProof())) {
      return
    }

    setIsSendingCode(true)
    setSendCodeError('')
    setSendCodeSuccess(false)

    try {
      const response = await sendPendingOAuthVerifyCode({
        email: trimmedEmail,
        turnstile_token:
          turnstileEnabled || aliyunCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_ticket: tencentCaptchaEnabled ? turnstileToken : undefined,
        tencent_captcha_randstr: tencentCaptchaEnabled ? tencentCaptchaRandstr : undefined
      })
      if (disposedRef.current) return
      setSendCodeSuccess(true)
      setCountdown(response.countdown)
    } catch (error: unknown) {
      setSendCodeError(getRequestErrorMessage(error, t('auth.sendCodeFailed')))
    } finally {
      if (captchaEnabled) {
        resetTurnstile()
      }
      setIsSendingCode(false)
    }
  }

  async function handleSubmit() {
    const trimmedEmail = email.trim()
    if (!trimmedEmail || password.length < 6) {
      return
    }

    // Turnstile 票据一次性：发送验证码已消耗上一枚，reset 后要等新票据回调。
    // 缺票时不能提交——create-account 端点会校验验证码，空 token 直接被判失败。
    // 表单的隐式提交（输入框回车）绕得过按钮的 disabled，所以这里必须再挡一次。
    if (turnstileEnabled && !turnstileToken) {
      setSendCodeError(t('auth.completeVerification'))
      return
    }

    if (!(await acquireActionProof())) {
      return
    }

    onSubmit?.({
      email: trimmedEmail,
      password,
      verifyCode: emailVerifyEnabled ? verifyCode.trim() : '',
      ...((turnstileEnabled || aliyunCaptchaEnabled) && turnstileToken
        ? { turnstileToken }
        : {}),
      ...(tencentCaptchaEnabled && turnstileToken
        ? {
            tencentCaptchaTicket: turnstileToken,
            tencentCaptchaRandstr
          }
        : {}),
      invitationCode: invitationCode.trim() || undefined
    })

    if (actionCaptchaEnabled) {
      resetTurnstile()
    }
  }

  function emitSwitchToBind() {
    onSwitchToBind?.(email.trim())
  }

  return (
    <form
      className="space-y-3"
      onSubmit={(event) => {
        event.preventDefault()
        void handleSubmit()
      }}
    >
      <input
        value={email}
        data-testid={`${testIdPrefix}-create-account-email`}
        type="email"
        className="input w-full"
        placeholder={t('auth.emailPlaceholder')}
        disabled={isSubmitting || isSendingCode}
        onChange={(event) => setEmail(event.target.value)}
      />
      <input
        value={password}
        data-testid={`${testIdPrefix}-create-account-password`}
        type="password"
        className="input w-full"
        placeholder={t('auth.passwordPlaceholder')}
        disabled={isSubmitting}
        onChange={(event) => setPassword(event.target.value)}
      />
      {captchaEnabled && (
        <div className="space-y-2">
          <CaptchaChallenge
            ref={captchaRef}
            turnstileEnabled={turnstileEnabled}
            turnstileSiteKey={turnstileSiteKey}
            tencentEnabled={tencentCaptchaEnabled}
            tencentAppId={tencentCaptchaAppId}
            tencentRegion={tencentCaptchaRegion}
            aliyunEnabled={aliyunCaptchaEnabled}
            aliyunSceneId={aliyunCaptchaSceneId}
            aliyunPrefix={aliyunCaptchaPrefix}
            aliyunRegion={aliyunCaptchaRegion}
            onVerify={onTurnstileVerify}
            onExpire={onTurnstileExpire}
            onError={onTurnstileError}
          />
        </div>
      )}
      {emailVerifyEnabled && (
        <div className="flex gap-3">
          <input
            value={verifyCode}
            data-testid={`${testIdPrefix}-create-account-verify-code`}
            type="text"
            inputMode="numeric"
            maxLength={6}
            className="input min-w-0 flex-1"
            placeholder="123456"
            disabled={isSubmitting}
            onChange={(event) => setVerifyCode(event.target.value)}
          />
          <button
            data-testid={`${testIdPrefix}-create-account-send-code`}
            type="button"
            className="btn btn-secondary shrink-0"
            disabled={
              isSubmitting ||
              isSendingCode ||
              countdown > 0 ||
              !email.trim() ||
              (turnstileEnabled && !turnstileToken)
            }
            onClick={() => void handleSendCode()}
          >
            {isSendingCode
              ? t('auth.sendingCode')
              : countdown > 0
                ? t('auth.resendCountdown', { countdown })
                : t('auth.sendCode')}
          </button>
        </div>
      )}
      {emailVerifyEnabled && sendCodeSuccess ? (
        <p className="text-sm text-green-600 dark:text-green-400">{t('auth.codeSentSuccess')}</p>
      ) : emailVerifyEnabled ? (
        <p className="text-xs surface-muted">{t('auth.verificationCodeHint')}</p>
      ) : null}
      {invitationCodeEnabled && (
        <input
          value={invitationCode}
          data-testid={`${testIdPrefix}-create-account-invitation-code`}
          type="text"
          className="input w-full"
          placeholder={t('auth.invitationCodePlaceholder')}
          disabled={isSubmitting}
          onChange={(event) => setInvitationCode(event.target.value)}
        />
      )}
      <button
        data-testid={`${testIdPrefix}-create-account-submit`}
        type="button"
        className="btn btn-primary w-full"
        disabled={
          isSubmitting ||
          !email.trim() ||
          password.length < 6 ||
          (invitationCodeEnabled && !invitationCode.trim()) ||
          (turnstileEnabled && !turnstileToken)
        }
        onClick={() => void handleSubmit()}
      >
        {isSubmitting ? t('common.processing') : t('auth.createAccount')}
      </button>
      <button type="button" className="btn btn-secondary w-full" disabled={isSubmitting} onClick={emitSwitchToBind}>
        {t('auth.alreadyHaveAccount')}
      </button>
    </form>
  )
}

export default PendingOAuthCreateAccountForm
