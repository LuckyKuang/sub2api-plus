import { useEffect, useMemo } from 'react'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { resolveWeChatOAuthStart, type OAuthLoginStart } from '@/api/auth'
import { useAppStore } from '@/stores/app'
import { resolveAffiliateReferralCode, storeOAuthAffiliateCode } from '@/utils/oauthAffiliate'

export interface WechatOAuthSectionProps {
  disabled?: boolean
  affCode?: string
  showDivider?: boolean
  onStart?: (request: OAuthLoginStart) => void
}

export function WechatOAuthSection({
  disabled = false,
  affCode = '',
  showDivider = true,
  onStart
}: WechatOAuthSectionProps) {
  const { t, i18n } = useTranslation()
  const cachedPublicSettings = useAppStore((state) => state.cachedPublicSettings)
  const publicSettingsLoaded = useAppStore((state) => state.publicSettingsLoaded)
  const fetchPublicSettings = useAppStore((state) => state.fetchPublicSettings)
  const location = useLocation()
  const providerName = useMemo(() => t('auth.wechatProviderName'), [t])

  // The cached settings are populated by the app shell; refetch when they are missing.
  useEffect(() => {
    if (!cachedPublicSettings && !publicSettingsLoaded) {
      void fetchPublicSettings()
    }
  }, [cachedPublicSettings, publicSettingsLoaded, fetchPublicSettings])

  const localizeWeChatHint = (zh: string, en: string): string =>
    i18n.language.startsWith('zh') ? zh : en

  const resolvedStart = useMemo(
    () => resolveWeChatOAuthStart(cachedPublicSettings),
    [cachedPublicSettings]
  )

  const buttonDisabled = disabled || resolvedStart.mode === null

  const disabledHint = (() => {
    if (disabled) {
      return ''
    }
    switch (resolvedStart.unavailableReason) {
      case 'external_browser_required':
        return t('auth.oauthFlow.wechatSystemBrowserOnly')
      case 'wechat_browser_required':
        return t('auth.oauthFlow.wechatBrowserOnly')
      case 'native_app_required':
        return localizeWeChatHint(
          '当前仅配置微信移动应用登录，需要在原生 App 中通过微信 SDK 发起授权。',
          'This site only has WeChat mobile app login configured. Continue from the native app through the WeChat SDK.'
        )
      case 'not_configured':
        return t('auth.oauthFlow.wechatNotConfigured')
      default:
        return ''
    }
  })()

  function startLogin(): void {
    if (buttonDisabled || !resolvedStart.mode) {
      return
    }
    const params = new URLSearchParams(location.search)
    const redirectTo = params.get('redirect') || '/dashboard'
    storeOAuthAffiliateCode(
      resolveAffiliateReferralCode(affCode, params.get('aff'), params.get('aff_code'))
    )
    const mode = resolvedStart.mode
    onStart?.({ provider: 'wechat', params: { mode, redirect: redirectTo } })
  }

  return (
    <div className="space-y-4">
      <button type="button" disabled={buttonDisabled} className="btn btn-secondary w-full" onClick={startLogin}>
        <span className="mr-2 inline-flex h-5 w-5 items-center justify-center rounded-full bg-green-100/[0.15] text-xs font-semibold text-green-700 dark:bg-green-900/30 dark:text-green-300">
          W
        </span>
        {t('auth.oidc.signIn', { providerName })}
      </button>

      {disabledHint && (
        <p data-testid="wechat-oauth-hint" className="text-sm text-amber-600 dark:text-amber-400">
          {disabledHint}
        </p>
      )}

      {showDivider && (
        <div className="flex items-center gap-3">
          <div className="h-px flex-1 surface-inset" />
          <span className="text-xs surface-muted">{t('auth.oauthOrContinue')}</span>
          <div className="h-px flex-1 surface-inset" />
        </div>
      )}
    </div>
  )
}

export default WechatOAuthSection
