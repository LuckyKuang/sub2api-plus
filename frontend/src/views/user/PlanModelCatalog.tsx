import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { SaleModelCatalog } from '@/types/billingRate'
import { formatBillingRate } from '@/utils/billingRate'
import { platformLabel } from '@/utils/platformColors'
import CatalogQuoteDetails from './CatalogQuoteDetails'

export interface PlanModelCatalogProps {
  catalog?: SaleModelCatalog
}

export function PlanModelCatalog({ catalog }: PlanModelCatalogProps) {
  const { t } = useTranslation()
  const [openModels, setOpenModels] = useState<Set<string>>(new Set())

  useEffect(() => { setOpenModels(new Set()) }, [catalog])

  function toggleModel(key: string) {
    setOpenModels(previous => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  if (!catalog) return null

  return (
    <details className="space-y-3 rounded-lg surface-inset p-3 text-xs">
      <summary className="cursor-pointer font-medium">{t('billingRates.models')} · {catalog.models.length}</summary>
      <p className="leading-5 surface-muted">{t('billingRates.saleNote')}</p>
      {catalog.models.length === 0 && <p className="surface-muted">{t('billingRates.noModels')}</p>}
      {catalog.models.map(model => (
        <details
          key={model.offer_key}
          open={openModels.has(model.offer_key)}
          className="border-t border-gray-200 py-2 dark:border-dark-600"
        >
          <summary
            className="flex min-w-0 cursor-pointer flex-wrap items-start justify-between gap-2"
            onClick={(event) => { event.preventDefault(); toggleModel(model.offer_key) }}
          >
            <div className="min-w-0 flex-1">
              <span className="font-medium">{platformLabel(model.platform)}</span>
              <div className="break-words font-mono [overflow-wrap:anywhere]">{model.name}</div>
              <span className="surface-muted">{t(`billingRates.units.${model.billing_unit}`, model.billing_unit)}</span>
            </div>
            <div className="text-right">
              <strong>{model.rate_quote ? formatBillingRate(model.rate_quote.effective_multiplier) : t('billingRates.unknown')}</strong>
              {model.rate_quote && <p className="mt-1 surface-muted">{t(`billingRates.source.${model.rate_quote.source}`)}</p>}
            </div>
          </summary>
          {openModels.has(model.offer_key) && (
            <CatalogQuoteDetails className="mt-3" offer={model} rateUnavailable={!model.rate_quote} />
          )}
        </details>
      ))}
      {catalog.as_of && <p className="surface-muted">{t('billingRates.asOf', { time: catalog.as_of })}</p>}
    </details>
  )
}

export default PlanModelCatalog
