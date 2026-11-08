import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Icon } from '@/components/auth/Icon'
import { useAppStore } from '@/stores/app'
import { resetPassword } from '@/api/auth'
import { cn } from '@/lib/utils'
import '@/views/authViews.css'

export default function ResetPasswordView() {
  const { t } = useTranslation()
  const appStore = useAppStore()
  const [searchParams] = useSearchParams()

  const [isLoading, setIsLoading] = useState(false)
  const [isSuccess, setIsSuccess] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)

  // URL parameters
  const [email, setEmail] = useState('')
  const [token, setToken] = useState('')

  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  const [errors, setErrors] = useState({ password: '', confirmPassword: '' })

  const validationToastMessage = errors.password || errors.confirmPassword || ''

  // Validation messages surface as toasts, matching the Vue watcher.
  useEffect(() => {
    if (validationToastMessage) {
      appStore.showError(validationToastMessage)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [validationToastMessage])

  // Check if the reset link is valid (has email and token)
  const isInvalidLink = !email || !token

  useEffect(() => {
    // Get email and token from URL query parameters
    const queryEmail = searchParams.get('email') || ''
    const queryToken = searchParams.get('token') || ''
    setEmail(queryEmail)
    setToken(queryToken)

    if (!queryEmail || !queryToken) {
      appStore.showError(t('auth.invalidResetLink'))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ==================== Validation ====================

  function validateForm(): boolean {
    setErrors({ password: '', confirmPassword: '' })

    let isValid = true

    // Password validation
    if (!password) {
      setErrors((previous) => ({ ...previous, password: t('auth.passwordRequired') }))
      isValid = false
    } else if (password.length < 6) {
      setErrors((previous) => ({ ...previous, password: t('auth.passwordMinLength') }))
      isValid = false
    }

    // Confirm password validation
    if (!confirmPassword) {
      setErrors((previous) => ({ ...previous, confirmPassword: t('auth.confirmPasswordRequired') }))
      isValid = false
    } else if (password !== confirmPassword) {
      setErrors((previous) => ({ ...previous, confirmPassword: t('auth.passwordsDoNotMatch') }))
      isValid = false
    }

    return isValid
  }

  // ==================== Form Handlers ====================

  async function handleSubmit(event: React.FormEvent): Promise<void> {
    event.preventDefault()

    if (!validateForm()) {
      return
    }

    setIsLoading(true)

    try {
      await resetPassword({
        email,
        token,
        new_password: password
      })

      setIsSuccess(true)
      appStore.showSuccess(t('auth.passwordResetSuccess'))
    } catch (error: unknown) {
      const err = error as { message?: string; response?: { data?: { detail?: string; code?: string } } }

      // Check for invalid/expired token error
      if (err.response?.data?.code === 'INVALID_RESET_TOKEN') {
        appStore.showError(t('auth.invalidOrExpiredToken'))
      } else if (err.response?.data?.detail) {
        appStore.showError(err.response.data.detail)
      } else if (err.message) {
        appStore.showError(err.message)
      } else {
        appStore.showError(t('auth.resetPasswordFailed'))
      }
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <>
      <div className="space-y-6">
        {/* Title */}
        <div className="text-center">
          <h2 className="text-2xl font-bold text-gray-900 dark:text-white">{t('auth.resetPasswordTitle')}</h2>
          <p className="mt-2 text-sm surface-muted">{t('auth.resetPasswordHint')}</p>
        </div>

        {/* Invalid Link State */}
        {isInvalidLink && (
          <div className="space-y-6">
            <div className="rounded-xl border border-amber-200 bg-amber-50/[0.15] p-6 dark:border-amber-800/50 dark:bg-amber-900/20">
              <div className="flex flex-col items-center gap-4 text-center">
                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-amber-100/[0.15] dark:bg-amber-800/50">
                  <Icon name="exclamationCircle" size="lg" className="text-amber-600 dark:text-amber-400" />
                </div>
                <div>
                  <h3 className="text-lg font-semibold text-amber-800 dark:text-amber-200">
                    {t('auth.invalidResetLink')}
                  </h3>
                  <p className="mt-2 text-sm text-amber-700 dark:text-amber-300">{t('auth.invalidResetLinkHint')}</p>
                </div>
              </div>
            </div>

            <div className="text-center">
              <Link
                to="/forgot-password"
                className="inline-flex items-center gap-2 font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
              >
                {t('auth.requestNewResetLink')}
              </Link>
            </div>
          </div>
        )}

        {/* Success State */}
        {!isInvalidLink && isSuccess && (
          <div className="space-y-6">
            <div className="rounded-xl border border-green-200 bg-green-50/[0.15] p-6 dark:border-green-800/50 dark:bg-green-900/20">
              <div className="flex flex-col items-center gap-4 text-center">
                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-green-100/[0.15] dark:bg-green-800/50">
                  <Icon name="checkCircle" size="lg" className="text-green-600 dark:text-green-400" />
                </div>
                <div>
                  <h3 className="text-lg font-semibold text-green-800 dark:text-green-200">
                    {t('auth.passwordResetSuccess')}
                  </h3>
                  <p className="mt-2 text-sm text-green-700 dark:text-green-300">{t('auth.passwordResetSuccessHint')}</p>
                </div>
              </div>
            </div>

            <div className="text-center">
              <Link to="/login" className="btn btn-primary inline-flex items-center gap-2">
                <Icon name="login" size="md" />
                {t('auth.signIn')}
              </Link>
            </div>
          </div>
        )}

        {/* Form State */}
        {!isInvalidLink && !isSuccess && (
          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Email (readonly) */}
            <div>
              <label htmlFor="email" className="input-label">
                {t('auth.emailLabel')}
              </label>
              <div className="relative">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                  <Icon name="mail" size="md" className="surface-muted" />
                </div>
                <input id="email" value={email} type="email" readOnly disabled className="input pl-11 surface-inset" />
              </div>
            </div>

            {/* New Password Input */}
            <div>
              <label htmlFor="password" className="input-label">
                {t('auth.newPassword')}
              </label>
              <div className="relative">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                  <Icon name="lock" size="md" className="surface-muted" />
                </div>
                <input
                  id="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  type={showPassword ? 'text' : 'password'}
                  required
                  autoComplete="new-password"
                  disabled={isLoading}
                  className={cn('input pl-11 pr-11', errors.password && 'input-error')}
                  placeholder={t('auth.newPasswordPlaceholder')}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((value) => !value)}
                  className="absolute inset-y-0 right-0 flex items-center pr-3.5 transition-colors hover:text-gray-600 dark:hover:text-dark-300 surface-control surface-muted"
                >
                  <Icon name={showPassword ? 'eyeOff' : 'eye'} size="md" />
                </button>
              </div>
            </div>

            {/* Confirm Password Input */}
            <div>
              <label htmlFor="confirmPassword" className="input-label">
                {t('auth.confirmPassword')}
              </label>
              <div className="relative">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                  <Icon name="lock" size="md" className="surface-muted" />
                </div>
                <input
                  id="confirmPassword"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  type={showConfirmPassword ? 'text' : 'password'}
                  required
                  autoComplete="new-password"
                  disabled={isLoading}
                  className={cn('input pl-11 pr-11', errors.confirmPassword && 'input-error')}
                  placeholder={t('auth.confirmPasswordPlaceholder')}
                />
                <button
                  type="button"
                  onClick={() => setShowConfirmPassword((value) => !value)}
                  className="absolute inset-y-0 right-0 flex items-center pr-3.5 transition-colors hover:text-gray-600 dark:hover:text-dark-300 surface-control surface-muted"
                >
                  <Icon name={showConfirmPassword ? 'eyeOff' : 'eye'} size="md" />
                </button>
              </div>
            </div>

            {/* Submit Button */}
            <button type="submit" disabled={isLoading} className="btn btn-primary w-full">
              {isLoading ? (
                <svg className="-ml-1 mr-2 h-4 w-4 animate-spin text-white" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  />
                </svg>
              ) : (
                <Icon name="checkCircle" size="md" className="mr-2" />
              )}
              {isLoading ? t('auth.resettingPassword') : t('auth.resetPassword')}
            </button>
          </form>
        )}
      </div>

      {/* Footer */}
      <div className="mt-6 border-t border-gray-100 pt-4 text-center text-sm surface-inset dark:border-dark-800">
        <p className="surface-muted">
          {t('auth.rememberedPassword')}
          <Link
            to="/login"
            className="font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
          >
            {t('auth.signIn')}
          </Link>
        </p>
      </div>
    </>
  )
}
