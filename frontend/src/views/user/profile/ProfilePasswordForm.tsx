import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { userAPI } from '@/api/user'
import { toast } from '@/components/ui'
import { extractApiErrorMessage } from '@/utils/apiError'
import { cn } from '@/lib/utils'

export interface ProfilePasswordFormProps {
  embedded?: boolean
  className?: string
}

export function ProfilePasswordForm({ embedded = false, className }: ProfilePasswordFormProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [form, setForm] = useState({ old_password: '', new_password: '', confirm_password: '' })

  const handleChangePassword = async () => {
    if (form.new_password !== form.confirm_password) {
      toast.error(t('profile.passwordsNotMatch'))
      return
    }

    if (form.new_password.length < 8) {
      toast.error(t('profile.passwordTooShort'))
      return
    }

    setLoading(true)
    try {
      await userAPI.changePassword(form.old_password, form.new_password)
      setForm({ old_password: '', new_password: '', confirm_password: '' })
      toast.success(t('profile.passwordChangeSuccess'))
    } catch (error: unknown) {
      toast.error(extractApiErrorMessage(error, t('profile.passwordChangeFailed')))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className={cn(embedded ? 'space-y-4' : 'card', className)}>
      {!embedded && (
        <div className="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 className="text-lg font-medium text-gray-900 dark:text-white">{t('profile.changePassword')}</h2>
        </div>
      )}
      <div className={embedded ? '' : 'px-6 py-6'}>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void handleChangePassword()
          }}
          className="space-y-4"
        >
          {embedded && (
            <p className="text-sm font-semibold text-gray-900 dark:text-white">{t('profile.changePassword')}</p>
          )}
          <div>
            <label htmlFor="old_password" className="input-label">
              {t('profile.currentPassword')}
            </label>
            <input
              id="old_password"
              value={form.old_password}
              onChange={(event) => setForm({ ...form, old_password: event.target.value })}
              type="password"
              required
              autoComplete="current-password"
              className="input"
            />
          </div>

          <div>
            <label htmlFor="new_password" className="input-label">
              {t('profile.newPassword')}
            </label>
            <input
              id="new_password"
              value={form.new_password}
              onChange={(event) => setForm({ ...form, new_password: event.target.value })}
              type="password"
              required
              autoComplete="new-password"
              className="input"
            />
            <p className="input-hint">{t('profile.passwordHint')}</p>
          </div>

          <div>
            <label htmlFor="confirm_password" className="input-label">
              {t('profile.confirmNewPassword')}
            </label>
            <input
              id="confirm_password"
              value={form.confirm_password}
              onChange={(event) => setForm({ ...form, confirm_password: event.target.value })}
              type="password"
              required
              autoComplete="new-password"
              className="input"
            />
          </div>

          <div className="flex justify-end pt-4">
            <button type="submit" disabled={loading} className="btn btn-primary">
              {loading ? t('profile.changingPassword') : t('profile.changePasswordButton')}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default ProfilePasswordForm
