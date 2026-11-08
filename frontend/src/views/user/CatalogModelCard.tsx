import { useTranslation } from 'react-i18next'
import { type CatalogModel } from './catalog'
import { catalogRange } from './catalogQuote'
import { rateSource } from './catalogQuote'
import { Icon } from './icons'
import { PlatformIcon } from './PlatformIcon'
import { useClipboard } from './useClipboard'
import { platformTextClass } from '@/utils/platformColors'

export interface CatalogModelCardProps {
  model: CatalogModel
  rateUnavailable: boolean
  onDetails?: (model: CatalogModel) => void
}

export function CatalogModelCard({ model, rateUnavailable, onDetails }: CatalogModelCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()

  return (
    <article className="glass-surface catalog-model-card flex min-w-0 flex-col border p-4 transition-shadow hover:shadow-md">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className={`inline-flex min-w-0 items-center gap-1.5 ${platformTextClass(model.platform)}`}>
          <PlatformIcon platform={model.platform} size="sm" />
          {model.platform}
        </span>
        {model.prices.length > 1 && <span className="surface-muted">{t('availableChannels.catalog.multipleUnits')}</span>}
      </div>
      <div className="mb-4 mt-3 flex items-start gap-2">
        <h3 className="min-w-0 flex-1 break-words font-mono text-sm font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">
          {model.name}
        </h3>
        <button
          className="surface-control surface-muted shrink-0 rounded p-1 hover:text-primary-500 focus-visible:ring-2 focus-visible:ring-primary-500"
          aria-label={t('availableChannels.catalog.copyModel', { model: model.name })}
          onClick={() => void copyToClipboard(model.name)}
        >
          <Icon name="copy" size="sm" />
        </button>
      </div>
      <div className="space-y-3">
        {model.prices.map((price) => (
          <div key={price.unit} className="space-y-1">
            <p className="text-[11px] surface-muted">{t(`availableChannels.catalog.units.${price.unit}`)}</p>
            {(price.input || price.output) && (
              <div className={`grid gap-2 ${price.unit === 'token' ? 'grid-cols-2' : 'grid-cols-1'}`}>
                <div>
                  <p className="text-xs surface-muted">
                    {t(price.unit === 'token' ? 'availableChannels.pricing.inputPrice' : 'availableChannels.catalog.unitPrice')}
                  </p>
                  <p className="break-words font-mono text-base font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                    {catalogRange(price.input, price.unit)}
                  </p>
                </div>
                {price.unit === 'token' && (
                  <div>
                    <p className="text-xs surface-muted">{t('availableChannels.pricing.outputPrice')}</p>
                    <p className="break-words font-mono text-base font-semibold tabular-nums text-gray-900 dark:text-gray-100">
                      {catalogRange(price.output, price.unit)}
                    </p>
                  </div>
                )}
              </div>
            )}
            {price.incomplete && (
              <p className="text-xs surface-muted">
                {t(
                  price.input || price.output
                    ? 'availableChannels.catalog.partialUnknown'
                    : 'availableChannels.catalog.reasons.pricing_unavailable'
                )}
              </p>
            )}
          </div>
        ))}
      </div>
      <p className="mt-2 text-[11px] surface-muted">
        {t(rateUnavailable ? 'availableChannels.catalog.referenceRange' : 'availableChannels.catalog.effectiveRange')}
      </p>
      <div className="mb-3 mt-2 flex flex-wrap gap-1.5 text-[11px] font-medium">
        {model.quotes.some((q) => rateSource(q.group, q.offer) === 'personal') && (
          <span className="rounded-md bg-emerald-100/[0.15] px-2 py-1 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300">
            {t('availableChannels.catalog.personalRate')}
          </span>
        )}
        {model.tiered !== 'none' && (
          <span className="rounded-md bg-blue-50/[0.15] px-2 py-1 text-blue-700 ring-1 ring-inset ring-blue-200 dark:bg-blue-900/30 dark:text-blue-300 dark:ring-blue-800">
            {t(model.tiered === 'all' ? 'availableChannels.pricing.intervals' : 'availableChannels.catalog.someTiered')}
          </span>
        )}
        {model.dynamic !== 'none' && (
          <span className="rounded-md bg-amber-50/[0.15] px-2 py-1 text-amber-800 ring-1 ring-inset ring-amber-200 dark:bg-amber-900/30 dark:text-amber-300 dark:ring-amber-800">
            {t(model.dynamic === 'all' ? 'availableChannels.catalog.dynamicBilling' : 'availableChannels.catalog.someDynamic')}
          </span>
        )}
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 border-t border-gray-100 pt-3 text-xs dark:border-dark-800">
        <span className="surface-muted">
          {t('availableChannels.catalog.quoteCount', { count: model.quotes.length })}
          {model.quotes.some((q) => q.group.is_exclusive) && (
            <span
              className="whitespace-nowrap font-medium text-purple-700 dark:text-purple-300"
              title={t('availableChannels.catalog.exclusiveQuote')}
            >
              {' ' + t('availableChannels.exclusive')}
            </span>
          )}
        </span>
        <button
          className="surface-control shrink-0 rounded font-medium text-primary-600 focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-400"
          aria-label={t('availableChannels.catalog.detailsFor', { model: model.name })}
          onClick={() => onDetails?.(model)}
        >
          {t('availableChannels.catalog.details')} <span aria-hidden="true">→</span>
        </button>
      </div>
    </article>
  )
}

export default CatalogModelCard
