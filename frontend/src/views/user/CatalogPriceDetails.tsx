import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { BaseDialog } from './BaseDialog'
import { Icon } from './icons'
import CatalogQuoteDetails from './CatalogQuoteDetails'
import CatalogRateBadge from './CatalogRateBadge'
import { quotePrice, type CatalogModel } from './catalog'
import { catalogMoney } from './catalogQuote'
import { useClipboard } from './useClipboard'

export interface CatalogPriceDetailsProps {
  model: CatalogModel | null
  rateUnavailable: boolean
  onClose?: () => void
}

export function CatalogPriceDetails({ model, rateUnavailable, onClose }: CatalogPriceDetailsProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useClipboard()
  const [openQuotes, setOpenQuotes] = useState<Set<string>>(new Set())

  const quoteKey = (quote: CatalogModel['quotes'][number]) => `${quote.group.id}:${quote.offer.offer_key}`
  const toggleQuote = (key: string) => {
    setOpenQuotes((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <BaseDialog
      show={!!model}
      title={model?.name || t('availableChannels.catalog.details')}
      width="wide"
      closeOnClickOutside
      onClose={onClose}
    >
      {model && (
        <div key={model.key} className="min-w-0 space-y-5">
          <div>
            <div className="flex min-w-0 flex-wrap items-start gap-3">
              <h3 className="min-w-0 flex-1 break-words font-mono text-lg font-semibold text-gray-900 [overflow-wrap:anywhere] dark:text-gray-100">
                {model.name}
              </h3>
              <button
                type="button"
                className="catalog-copy-model btn btn-secondary shrink-0 text-xs"
                aria-label={t('availableChannels.catalog.copyModel', { model: model.name })}
                onClick={() => void copyToClipboard(model.name)}
              >
                <Icon name="copy" size="sm" />
                {t('common.copy')} ID
              </button>
            </div>
            <p className="mt-1 text-sm surface-muted">
              {model.platform} · {t('availableChannels.catalog.groupQuotes', { count: model.quotes.length })}
            </p>
          </div>
          <p className="surface-inset rounded-lg p-3 text-sm surface-muted">
            {t('availableChannels.catalog.billingGroupNote')}
          </p>
          {rateUnavailable && (
            <p className="text-sm text-amber-700 dark:text-amber-300">{t('availableChannels.catalog.ratesUnavailable')}</p>
          )}
          <div
            className="record-table-scroll record-table-dialog-scroll max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700"
            tabIndex={0}
            aria-label={t('availableChannels.catalog.quoteComparison')}
          >
            <table className="record-table catalog-comparison w-full text-left text-xs surface-muted">
              <thead className="surface-inset surface-muted">
                <tr>
                  <th className="px-3 py-2">{t('modelPlaza.filters.groupLabel')}</th>
                  <th className="px-3 py-2">{t('availableChannels.catalog.effectiveRateLabel')}</th>
                  <th className="px-3 py-2">{t('availableChannels.catalog.baseInputOrUnit')}</th>
                  <th className="px-3 py-2">{t('availableChannels.pricing.outputPrice')}</th>
                  <th className="px-3 py-2">{t('availableChannels.catalog.billingUnit')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 dark:divide-dark-700">
                {model.quotes.map((quote) => (
                  <tr key={quote.offer.offer_key}>
                    <th className="min-w-28 break-words px-3 py-2 font-medium">
                      {quote.group.name}
                      {quote.group.is_exclusive && (
                        <span className="whitespace-nowrap text-purple-700 dark:text-purple-300">
                          {' ' + t('availableChannels.exclusive')}
                        </span>
                      )}
                    </th>
                    <td className="min-w-28 px-3 py-2">
                      <CatalogRateBadge group={quote.group} offer={quote.offer} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 font-mono">
                      {catalogMoney(quotePrice(quote, false), 1, quote.offer.billing_unit === 'token')}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 font-mono">
                      {quote.offer.billing_unit === 'token'
                        ? catalogMoney(quotePrice(quote, true), 1, true)
                        : '—'}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2">
                      {t(`availableChannels.catalog.units.${quote.offer.billing_unit}`)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {model.quotes.map((quote) => {
            const key = quoteKey(quote)
            return (
              <details
                key={key}
                className="catalog-quote-disclosure min-w-0"
                open={openQuotes.has(key)}
              >
                <summary
                  className="cursor-pointer rounded-xl border border-gray-200 p-4 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-700"
                  onClick={(event) => {
                    event.preventDefault()
                    toggleQuote(key)
                  }}
                >
                  <span className="font-semibold">{quote.group.name}</span>
                  <span className="ml-2 surface-muted">{t('availableChannels.catalog.quoteDetails')}</span>
                </summary>
                {openQuotes.has(key) && (
                  <CatalogQuoteDetails
                    className="mt-3"
                    group={quote.group}
                    offer={quote.offer}
                    rateUnavailable={rateUnavailable}
                  />
                )}
              </details>
            )
          })}
        </div>
      )}
    </BaseDialog>
  )
}

export default CatalogPriceDetails
