import { useTranslation } from 'react-i18next'
import type { CatalogGroup } from '@/api/channels'
import type { SaleModelCatalog } from '@/types/billingRate'
import { formatBillingRate } from '@/utils/billingRate'
import { platformLabel } from '@/utils/platformColors'
import { PlanModelCatalog } from './PlanModelCatalog'

export interface GroupRateSummaryProps {
  group?: CatalogGroup
  loading?: boolean
  failed?: boolean
  compact?: boolean
  className?: string
  onRetry?: () => void
}

export function GroupRateSummary({ group, loading = false, failed = false, compact = false, className, onRetry }: GroupRateSummaryProps) {
  const { t } = useTranslation()
  const summary = group?.rate_summary

  const modelCatalog: SaleModelCatalog = {
    policy_version: summary?.policy_version ?? 0,
    as_of: summary?.as_of ?? '',
    models: group?.models ?? [],
    platforms: [],
    rate_min: null,
    rate_max: null
  }

  function range(min: number | null, max: number | null): string {
    if (min == null || max == null) return t('billingRates.unknown')
    return min === max ? formatBillingRate(min) : `${formatBillingRate(min)}–${formatBillingRate(max)}`
  }

  return (
    <section className={`min-w-0 space-y-2 text-xs ${className ?? ''}`} aria-live="polite">
      {loading ? (
        <p className="surface-muted">{t('common.loading')}</p>
      ) : failed || !summary || summary.status === 'unavailable' ? (
        <div className="flex flex-wrap items-center gap-2 text-amber-700 dark:text-amber-300">
          <span>{t('billingRates.unknown')}</span>
          {!compact && (
            <button type="button" className="btn btn-secondary btn-sm" onClick={onRetry}>
              {t('billingRates.retry')}
            </button>
          )}
        </div>
      ) : summary.status === 'empty' ? (
        <p className="surface-muted">{t('billingRates.noModels')}</p>
      ) : (
        <>
          {summary.entries.map((entry) => (
            <div
              key={`${entry.platform}:${entry.billing_unit}`}
              data-rate-entry={`${entry.platform}:${entry.billing_unit}`}
              className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1"
            >
              <strong>{platformLabel(entry.platform)}</strong>
              <span className="surface-muted">{t(`billingRates.units.${entry.billing_unit}`, entry.billing_unit)}</span>
              <span>
                {t('billingRates.platformDefault')}{' '}
                {entry.default_quote ? formatBillingRate(entry.default_quote.effective_multiplier) : t('billingRates.unknown')}
              </span>
              {(entry.has_model_exceptions || !entry.default_quote) && (
                <span className="surface-muted">
                  {t('billingRates.modelRange')} {range(entry.rate_min, entry.rate_max)}
                </span>
              )}
            </div>
          ))}
          {!compact && (
            <>
              <p className="surface-muted">{t('billingRates.keyQuoteNote')}</p>
              <PlanModelCatalog catalog={modelCatalog} />
            </>
          )}
        </>
      )}
    </section>
  )
}

export default GroupRateSummary
