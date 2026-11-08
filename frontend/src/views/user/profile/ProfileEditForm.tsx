import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { userAPI } from '@/api/user'
import { toast } from '@/components/ui'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useUserView } from '../useUserView'

export interface ProfileEditFormProps {
  initialUsername: string
  embedded?: boolean
}

export function ProfileEditForm({ initialUsername, embedded = false }: ProfileEditFormProps) {
  const { t } = useTranslation()
  const authStore = useUserView()
  const [username, setUsername] = useState(initialUsername)
  const [loading, setLoading] = useState(false)
  const savedUsername = useRef(initialUsername)

  useEffect(() => {
    if (username === savedUsername.current) setUsername(initialUsername)
    savedUsername.current = initialUsername
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialUsername])

  const handleUpdateProfile = async () => {
    if (!username.trim()) {
      toast.error(t('profile.usernameRequired'))
      return
    }

    setLoading(true)
    const submittedUsername = username
    try {
      const updatedUser = await userAPI.updateProfile({ username: submittedUsername })
      if (username === submittedUsername) setUsername(updatedUser.username)
      savedUsername.current = updatedUser.username
      if (authStore.user) authStore.user = updatedUser
      toast.success(t('profile.updateSuccess'))
    } catch (error: unknown) {
      toast.error(extractApiErrorMessage(error, t('profile.updateFailed')))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className={embedded ? 'space-y-4' : 'card'}>
      {!embedded && (
        <div className="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 className="text-lg font-medium text-gray-900 dark:text-white">{t('profile.editProfile')}</h2>
        </div>
      )}
      <div className={embedded ? '' : 'px-6 py-6'}>
        <form
          onSubmit={(event) => {
            event.preventDefault()
            void handleUpdateProfile()
          }}
          className="space-y-4"
        >
          {embedded && (
            <p className="text-sm font-semibold text-gray-900 dark:text-white">{t('profile.editProfile')}</p>
          )}
          <div>
            <label htmlFor="username" className="input-label">
              {t('profile.username')}
            </label>
            <input
              id="username"
              value={username}
              type="text"
              className="input"
              placeholder={t('profile.enterUsername')}
              onChange={(event) => setUsername(event.target.value)}
            />
          </div>

          <div className="flex justify-end pt-4">
            <button type="submit" disabled={loading} className="btn btn-primary">
              {loading ? t('profile.updating') : t('profile.updateProfile')}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default ProfileEditForm
