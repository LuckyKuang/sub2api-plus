import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { isWeChatWebOAuthEnabled } from '@/api/auth'
import { useAppStore } from '@/stores/app'
import type { PublicSettings } from '@/types'
import { Icon } from './icons'
import { useUserView } from './useUserView'
import { ProfileBalanceNotifyCard } from './profile/ProfileBalanceNotifyCard'
import { ProfileInfoCard } from './profile/ProfileInfoCard'
import { ProfilePasskeyCard } from './profile/ProfilePasskeyCard'
import { ProfilePasswordForm } from './profile/ProfilePasswordForm'
import { ProfileTotpCard } from './profile/ProfileTotpCard'

export default function ProfileView() {
  const { t } = useTranslation()
  const authStore = useUserView()
  const user = authStore.user

  const [contactInfo, setContactInfo] = useState('')
  const [balanceLowNotifyEnabled, setBalanceLowNotifyEnabled] = useState(false)
  const [systemDefaultThreshold, setSystemDefaultThreshold] = useState(0)
  const [linuxdoOAuthEnabled, setLinuxdoOAuthEnabled] = useState(false)
  const [dingtalkOAuthEnabled, setDingtalkOAuthEnabled] = useState(false)
  const [wechatOAuthEnabled, setWechatOAuthEnabled] = useState(false)
  const [wechatOAuthOpenEnabled, setWechatOAuthOpenEnabled] = useState<boolean | undefined>(undefined)
  const [wechatOAuthMPEnabled, setWechatOAuthMPEnabled] = useState<boolean | undefined>(undefined)
  const [oidcOAuthEnabled, setOidcOAuthEnabled] = useState(false)
  const [oidcOAuthProviderName, setOidcOAuthProviderName] = useState('OIDC')
  const [passkeyEnabled, setPasskeyEnabled] = useState(false)

  useEffect(() => {
    const profileRefresh = authStore.refreshUser().catch((error) => {
      console.error('Failed to refresh profile:', error)
    })

    const settingsLoad = useAppStore
      .getState()
      .fetchPublicSettings()
      .then((settings: PublicSettings | null) => {
        if (!settings) return
        setContactInfo(settings.contact_info || '')
        setBalanceLowNotifyEnabled(settings.balance_low_notify_enabled ?? false)
        setSystemDefaultThreshold(settings.balance_low_notify_threshold ?? 0)
        setLinuxdoOAuthEnabled(settings.linuxdo_oauth_enabled ?? false)
        setDingtalkOAuthEnabled(settings.dingtalk_oauth_enabled ?? false)
        setWechatOAuthEnabled(isWeChatWebOAuthEnabled(settings))
        setWechatOAuthOpenEnabled(
          typeof settings.wechat_oauth_open_enabled === 'boolean' ? settings.wechat_oauth_open_enabled : undefined
        )
        setWechatOAuthMPEnabled(
          typeof settings.wechat_oauth_mp_enabled === 'boolean' ? settings.wechat_oauth_mp_enabled : undefined
        )
        setOidcOAuthEnabled(settings.oidc_oauth_enabled ?? false)
        setOidcOAuthProviderName(settings.oidc_oauth_provider_name || 'OIDC')
        setPasskeyEnabled(settings.passkey_enabled === true)
      })
      .catch((error: unknown) => {
        console.error('Failed to load settings:', error)
      })

    void Promise.all([profileRefresh, settingsLoad])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div data-testid="profile-shell" className="space-y-6">
      <ProfileInfoCard
        user={user}
        linuxdoEnabled={linuxdoOAuthEnabled}
        dingtalkEnabled={dingtalkOAuthEnabled}
        oidcEnabled={oidcOAuthEnabled}
        oidcProviderName={oidcOAuthProviderName}
        wechatEnabled={wechatOAuthEnabled}
        wechatOpenEnabled={wechatOAuthOpenEnabled}
        wechatMpEnabled={wechatOAuthMPEnabled}
      />

      {contactInfo && (
        <div className="card workspace-shell bg-primary-50/[0.15] p-6 dark:bg-primary-900/20">
          <div className="flex items-center gap-4">
            <div className="shrink-0 rounded-xl bg-primary-100/[0.15] p-3 text-primary-600">
              <Icon name="chat" size="lg" />
            </div>
            <div className="min-w-0">
              <h3 className="font-semibold text-primary-800 dark:text-primary-200">{t('common.contactSupport')}</h3>
              <p className="break-words text-sm font-medium [overflow-wrap:anywhere]">{contactInfo}</p>
            </div>
          </div>
        </div>
      )}

      <ProfilePasswordForm className="workspace-shell" />

      {user && balanceLowNotifyEnabled && (
        <ProfileBalanceNotifyCard
          className="workspace-shell"
          enabled={user.balance_notify_enabled ?? true}
          threshold={user.balance_notify_threshold}
          extraEmails={user.balance_notify_extra_emails ?? []}
          systemDefaultThreshold={systemDefaultThreshold}
          userEmail={user.email}
        />
      )}

      <ProfileTotpCard className="workspace-shell" />
      <ProfilePasskeyCard className="workspace-shell" enabled={passkeyEnabled} />
    </div>
  )
}
