import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { paymentAPI } from '@/api/payment'

export default function RedeemShopView() {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const [error, setError] = useState(false)

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const { data } = await paymentAPI.getCheckoutInfo()
        if (disposed) return
        if (!data.redeem_purchase_enabled || data.redeem_purchase_mode !== 'iframe' || !data.redeem_purchase_url) {
          setError(true)
          return
        }
        // Use the saved URL verbatim. Never attach site tokens, identity or page URL.
        setUrl(data.redeem_purchase_url)
      } catch {
        if (!disposed) setError(true)
      }
    })()
    return () => {
      disposed = true
    }
  }, [])

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/purchase" className="btn btn-secondary">
          {t('featureExtensions.backPurchase')}
        </Link>
        <Link to="/redeem" className="btn btn-secondary">
          {t('featureExtensions.redeemLink')}
        </Link>
        {url && (
          <a href={url} target="_blank" rel="noopener noreferrer" className="btn btn-primary">
            {t('featureExtensions.openNewTab')}
          </a>
        )}
      </div>
      {error ? (
        <p role="alert" className="text-red-600">
          {t('featureExtensions.purchaseUnavailable')}
        </p>
      ) : !url ? (
        <p>{t('common.loading')}</p>
      ) : (
        <iframe
          src={url}
          title={t('featureExtensions.purchaseTitle')}
          referrerPolicy="no-referrer"
          sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-popups-to-escape-sandbox"
          className="workspace-shell h-[75vh] w-full rounded-xl"
        />
      )}
    </div>
  )
}
