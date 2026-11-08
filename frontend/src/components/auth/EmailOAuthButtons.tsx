import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { GitHubMark, GoogleMark } from './OAuthMarks'
import type { OAuthLoginStart } from '@/api/auth'
import { resolveAffiliateReferralCode, storeOAuthAffiliateCode } from '@/utils/oauthAffiliate'

type EmailOAuthProvider = 'github' | 'google'
const EMAIL_OAUTH_PENDING_PROVIDER_KEY = 'email_oauth_pending_provider'

export interface EmailOAuthButtonsProps {
  disabled?: boolean
  affCode?: string
  promoCode?: string
  githubEnabled?: boolean
  googleEnabled?: boolean
  showDivider?: boolean
  onStart?: (request: OAuthLoginStart) => void
}

export function EmailOAuthButtons({
  disabled = false,
  affCode = '',
  promoCode = '',
  githubEnabled = false,
  googleEnabled = false,
  showDivider = true,
  onStart
}: EmailOAuthButtonsProps) {
  const { t } = useTranslation()
  const location = useLocation()

  const visibleProviders: EmailOAuthProvider[] = []
  if (githubEnabled) visibleProviders.push('github')
  if (googleEnabled) visibleProviders.push('google')

  const hasProviders = visibleProviders.length > 0
  const hasMultipleProviders = visibleProviders.length > 1

  if (!hasProviders) return null

  const providerLabel = (provider: EmailOAuthProvider): string => {
    const name = provider === 'github' ? 'GitHub' : 'Google'
    return hasMultipleProviders ? name : t('auth.emailOAuth.signIn', { providerName: name })
  }

  function startLogin(provider: EmailOAuthProvider): void {
    const params = new URLSearchParams(location.search)
    const redirectTo = params.get('redirect') || '/dashboard'
    const affiliateCode = resolveAffiliateReferralCode(
      affCode,
      params.get('aff'),
      params.get('aff_code')
    )
    storeOAuthAffiliateCode(affiliateCode)
    window.sessionStorage.setItem(EMAIL_OAUTH_PENDING_PROVIDER_KEY, provider)
    const requestParams: Record<string, string> = { redirect: redirectTo }
    if (affiliateCode) {
      requestParams.aff_code = affiliateCode
    }
    const promo = promoCode?.trim()
    if (promo) {
      requestParams.promo_code = promo
    }
    onStart?.({ provider, params: requestParams })
  }

  return (
    <div className="space-y-4">
      {showDivider && (
        <div className="flex items-center gap-3">
          <div className="h-px flex-1 surface-inset" />
          <span className="text-xs surface-muted">{t('auth.oauthOrContinue')}</span>
          <div className="h-px flex-1 surface-inset" />
        </div>
      )}

      <div className={cn('grid', 'grid-cols-1', 'gap-3', hasMultipleProviders && 'sm:grid-cols-2')}>
        {visibleProviders.map((provider) => (
          <button
            key={provider}
            type="button"
            disabled={disabled}
            className="btn btn-secondary h-12 w-full justify-center gap-2"
            onClick={() => startLogin(provider)}
          >
            {provider === 'github' ? (
              <GitHubMark className="h-5 w-5 text-gray-800 dark:text-gray-100" />
            ) : (
              <GoogleMark className="h-5 w-5" />
            )}
            <span className="font-medium">{providerLabel(provider)}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

export default EmailOAuthButtons
