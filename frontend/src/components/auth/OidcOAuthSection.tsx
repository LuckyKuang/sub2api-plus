import { useMemo } from 'react'
import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { OAuthLoginStart } from '@/api/auth'
import { resolveAffiliateReferralCode, storeOAuthAffiliateCode } from '@/utils/oauthAffiliate'

export interface OidcOAuthSectionProps {
  disabled?: boolean
  affCode?: string
  providerName?: string
  showDivider?: boolean
  onStart?: (request: OAuthLoginStart) => void
}

export function OidcOAuthSection({
  disabled = false,
  affCode = '',
  providerName = 'OIDC',
  showDivider = true,
  onStart
}: OidcOAuthSectionProps) {
  const { t } = useTranslation()
  const location = useLocation()

  const normalizedProviderName = useMemo(() => {
    const name = providerName?.trim()
    return name || 'OIDC'
  }, [providerName])

  const providerInitial = normalizedProviderName.charAt(0).toUpperCase() || 'O'

  function startLogin(): void {
    const params = new URLSearchParams(location.search)
    const redirectTo = params.get('redirect') || '/dashboard'
    storeOAuthAffiliateCode(
      resolveAffiliateReferralCode(affCode, params.get('aff'), params.get('aff_code'))
    )
    onStart?.({ provider: 'oidc', params: { redirect: redirectTo } })
  }

  return (
    <div className="space-y-4">
      <button type="button" disabled={disabled} className="btn btn-secondary w-full" onClick={startLogin}>
        <span className="mr-2 inline-flex h-5 w-5 items-center justify-center rounded-full bg-primary-100/[0.15] text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
          {providerInitial}
        </span>
        {t('auth.oidc.signIn', { providerName: normalizedProviderName })}
      </button>

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

export default OidcOAuthSection
