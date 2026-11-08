import { useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { OAuthLoginStart } from '@/api/auth'
import { resolveAffiliateReferralCode, storeOAuthAffiliateCode } from '@/utils/oauthAffiliate'

export interface OAuthSectionProps {
  disabled?: boolean
  affCode?: string
  promoCode?: string
  showDivider?: boolean
  onStart?: (request: OAuthLoginStart) => void
}

export function LinuxDoOAuthSection({
  disabled = false,
  affCode = '',
  promoCode = '',
  showDivider = true,
  onStart
}: OAuthSectionProps) {
  const { t } = useTranslation()
  const location = useLocation()

  function startLogin(): void {
    const params = new URLSearchParams(location.search)
    const redirectTo = params.get('redirect') || '/dashboard'
    storeOAuthAffiliateCode(
      resolveAffiliateReferralCode(affCode, params.get('aff'), params.get('aff_code'))
    )
    const requestParams: Record<string, string> = { redirect: redirectTo }
    const promo = promoCode?.trim()
    if (promo) {
      requestParams.promo_code = promo
    }
    onStart?.({ provider: 'linuxdo', params: requestParams })
  }

  return (
    <div className="space-y-4">
      <button type="button" disabled={disabled} className="btn btn-secondary w-full" onClick={startLogin}>
        <svg
          className="icon mr-2"
          viewBox="0 0 16 16"
          version="1.1"
          xmlns="http://www.w3.org/2000/svg"
          width="1em"
          height="1em"
          style={{ color: 'rgb(233, 84, 32)', width: 20, height: 20 }}
          aria-hidden="true"
        >
          <g id="linuxdo_icon" data-name="linuxdo_icon">
            <path
              d="m7.44,0s.09,0,.13,0c.09,0,.19,0,.28,0,.14,0,.29,0,.43,0,.09,0,.18,0,.27,0q.12,0,.25,0t.26.08c.15.03.29.06.44.08,1.97.38,3.78,1.47,4.95,3.11.04.06.09.12.13.18.67.96,1.15,2.11,1.3,3.28q0,.19.09.26c0,.15,0,.29,0,.44,0,.04,0,.09,0,.13,0,.09,0,.19,0,.28,0,.14,0,.29,0,.43,0,.09,0,.18,0,.27,0,.08,0,.17,0,.25q0,.19-.08.26c-.03.15-.06.29-.08.44-.38,1.97-1.47,3.78-3.11,4.95-.06.04-.12.09-.18.13-.96.67-2.11,1.15-3.28,1.3q-.19,0-.26.09c-.15,0-.29,0-.44,0-.04,0-.09,0-.13,0-.09,0-.19,0-.28,0-.14,0-.29,0-.43,0-.09,0-.18,0-.27,0-.08,0-.17,0-.25,0q-.19,0-.26-.08c-.15-.03-.29-.06-.44-.08-1.97-.38-3.78-1.47-4.95-3.11q-.07-.09-.13-.18c-.67-.96-1.15-2.11-1.3-3.28q0-.19-.09-.26c0-.15,0-.29,0-.44,0-.04,0-.09,0-.13,0-.09,0-.19,0-.28,0-.14,0-.29,0-.43,0-.09,0-.18,0-.27,0-.08,0-.17,0-.25q0-.19.08-.26c.03-.15.06-.29.08-.44.38-1.97,1.47-3.78,3.11-4.95.06-.04.12-.09.18-.13C4.42.73,5.57.26,6.74.1,7,.07,7.15,0,7.44,0Z"
              fill="#EFEFEF"
            />
            <path
              d="m1.27,11.33h13.45c-.94,1.89-2.51,3.21-4.51,3.88-1.99.59-3.96.37-5.8-.57-1.25-.7-2.67-1.9-3.14-3.3Z"
              fill="#FEB005"
            />
            <path
              d="m12.54,1.99c.87.7,1.82,1.59,2.18,2.68H1.27c.87-1.74,2.33-3.13,4.2-3.78,2.44-.79,5-.47,7.07,1.1Z"
              fill="#1D1D1F"
            />
          </g>
        </svg>
        {t('auth.linuxdo.signIn')}
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

export function DingTalkOAuthSection({
  disabled = false,
  affCode = '',
  showDivider = true,
  onStart
}: OAuthSectionProps) {
  const { t } = useTranslation()
  const location = useLocation()

  function startLogin(): void {
    const params = new URLSearchParams(location.search)
    const redirectTo = params.get('redirect') || '/dashboard'
    storeOAuthAffiliateCode(
      resolveAffiliateReferralCode(affCode, params.get('aff'), params.get('aff_code'))
    )
    onStart?.({ provider: 'dingtalk', params: { redirect: redirectTo } })
  }

  return (
    <div className="space-y-4">
      <button type="button" disabled={disabled} className="btn btn-secondary w-full" onClick={startLogin}>
        <svg
          className="icon mr-2"
          viewBox="0 0 24 24"
          xmlns="http://www.w3.org/2000/svg"
          width="20"
          height="20"
          aria-hidden="true"
          style={{ flexShrink: 0 }}
        >
          <circle cx="12" cy="12" r="12" fill="#1677FF" />
          <text
            x="12"
            y="17"
            fontFamily="sans-serif"
            fontSize="13"
            fontWeight="bold"
            fill="white"
            textAnchor="middle"
          >
            D
          </text>
        </svg>
        {t('auth.dingtalk.signIn')}
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

export default LinuxDoOAuthSection
