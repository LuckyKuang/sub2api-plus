import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { CatalogGroup, CatalogOffer, UserSupportedModelPricing } from '@/api/channels'
import { cn } from '@/lib/utils'
import CatalogPriceTable from './CatalogPriceTable'
import CatalogRateBadge from './CatalogRateBadge'
import { catalogMoney, effectivePriceRate, rateSource } from './catalogQuote'

export interface CatalogQuoteDetailsProps {
  offer: CatalogOffer
  group?: CatalogGroup
  rateUnavailable: boolean
  className?: string
}

export function CatalogQuoteDetails({ offer, group, rateUnavailable, className }: CatalogQuoteDetailsProps) {
  const { t } = useTranslation()
  const [openTiers, setOpenTiers] = useState<Set<string>>(new Set())

  useEffect(() => { setOpenTiers(new Set()) }, [offer])

  function toggleTier(name: string) {
    setOpenTiers(previous => {
      const next = new Set(previous)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const rate = effectivePriceRate(group, offer)
  const source = rateSource(group, offer)
  const independent = source === 'media'
  const official: UserSupportedModelPricing | null = offer.official_pricing ? {
    ...offer.official_pricing, billing_mode: 'token', image_input_price: null, image_output_price: null, per_request_price: null, intervals: offer.official_pricing.intervals || []
  } : null

  return (
    <section className={cn('catalog-quote min-w-0 space-y-4 rounded-xl border border-gray-200 p-4 text-sm dark:border-dark-700 surface-muted', className)}>
      <div>
        <div className="catalog-quote-header flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
          {group && (
            <h3 className="min-w-0 break-words text-sm font-semibold [overflow-wrap:anywhere]">
              {group.name}
              {group.is_exclusive && <span className="whitespace-nowrap text-purple-700 dark:text-purple-300">{' ' + t('availableChannels.exclusive')}</span>}
            </h3>
          )}
          <span className="min-w-0 break-words text-xs [overflow-wrap:anywhere] surface-muted">{offer.source.name}</span>
          {group && !group.is_exclusive && (
            <span className="rounded surface-inset px-1.5 py-0.5 text-xs font-medium leading-4 surface-muted">{t('availableChannels.public')}</span>
          )}
          {group?.subscription_type === 'subscription' && (
            <span className="text-xs surface-muted">{t('modelPlaza.badges.subscription')}</span>
          )}
          {group && <CatalogRateBadge group={group} offer={offer} />}
        </div>
        {offer.source.description && <p className="mt-2 break-words">{offer.source.description}</p>}
      </div>
      <p className="rounded-lg surface-inset p-3 text-xs leading-6">
        {t('availableChannels.catalog.quoteExplanation', { rate })}
        {t(`availableChannels.catalog.units.${offer.billing_unit}`)}
        {rateUnavailable && <span className="block text-amber-600 dark:text-amber-400">{t('availableChannels.catalog.ratesUnavailable')}</span>}
        {independent && <span className="block">{t('availableChannels.catalog.independentRate')}</span>}
      </p>
      {offer.rate_quote && (
        <p className="text-xs surface-muted">
          {offer.rate_quote.rate_calculation === 'override_v1'
            ? t('billingRates.legacyFactorBreakdown', { base: offer.rate_quote.base_multiplier, policy: offer.rate_quote.policy_multiplier, peak: offer.rate_quote.peak_multiplier })
            : t('billingRates.factorBreakdown', { base: offer.rate_quote.base_multiplier, platform: offer.rate_quote.platform_multiplier, model: offer.rate_quote.model_multiplier, peak: offer.rate_quote.peak_multiplier })}
          {' · '}
          {t('billingRates.asOf', { time: offer.rate_quote.as_of })}
        </p>
      )}
      {offer.price_time_multiplier != null && offer.price_time_multiplier !== 1 && (
        <p className="text-xs surface-muted">{t('billingRates.priceTimeFactor', { factor: offer.price_time_multiplier })}</p>
      )}
      {offer.pricing ? (
        <>
          <CatalogPriceTable pricing={offer.pricing} rate={rate} token={offer.billing_unit === 'token'} />
          {offer.pricing.intervals.length > 0 && (
            <p className="text-xs surface-muted">{t('availableChannels.catalog.wholeRequest')}</p>
          )}
          {offer.media_tiers?.length ? (
            <div className="flex flex-wrap gap-3">
              {offer.media_tiers.map(tier => (
                <div key={tier.label} className="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700">
                  {tier.label} <strong className="ml-2 font-mono">{catalogMoney(tier.price, rate)}</strong><span className="ml-1 text-xs">{t(`availableChannels.catalog.units.${tier.unit}`)}</span>
                </div>
              ))}
            </div>
          ) : null}
          {offer.billing_unit === 'second' && <p className="text-xs">{t('availableChannels.catalog.videoSeconds')}</p>}
          {offer.service_tier_pricing?.length ? (
            <div className="space-y-3">
              <h4 className="font-medium">{t('availableChannels.catalog.serviceTiers')}</h4>
              <p className="text-xs surface-muted">{t('availableChannels.catalog.serviceTierNote')}</p>
              {offer.service_tier_pricing.map(tier => (
                <details key={tier.name} open={openTiers.has(tier.name)} className="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
                  <summary className="cursor-pointer font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" onClick={(event) => { event.preventDefault(); toggleTier(tier.name) }}>
                    {t(`availableChannels.catalog.tiers.${tier.name}`)}
                  </summary>
                  {openTiers.has(tier.name) && <CatalogPriceTable pricing={tier.pricing} rate={rate} />}
                </details>
              ))}
            </div>
          ) : null}
          {Object.keys(offer.pricing.reasoning_effort_multipliers || {}).length > 0 && (
            <div>
              <h4 className="mb-2 font-medium">{t('availableChannels.catalog.reasoning')}</h4>
              {Object.entries(offer.pricing.reasoning_effort_multipliers || {}).map(([effort, multiplier]) => (
                <p key={effort}>{effort} · {multiplier}×</p>
              ))}
            </div>
          )}
        </>
      ) : (
        <p>{t(`availableChannels.catalog.reasons.${offer.price_reason || 'pricing_unavailable'}`)}</p>
      )}
      {group?.peak_rate_enabled && (
        <div className="rounded-lg bg-amber-50/[0.15] p-3 text-xs dark:bg-amber-900/20">
          {t('availableChannels.catalog.peak', { start: group.peak_start, end: group.peak_end, timezone: group.peak_timezone, multiplier: group.peak_rate_multiplier })}
        </div>
      )}
      {offer.time_pricing && (
        <div className="space-y-1 text-xs">
          <h4 className="font-medium">{t('availableChannels.catalog.timeRules')} · {offer.time_pricing.timezone}</h4>
          {offer.time_pricing.weekdays_only && <p>{t('availableChannels.catalog.weekdays')}</p>}
          {offer.time_pricing.periods.map(period => (
            <p key={period.start_time}>[{period.start_time}, {period.end_time}) · {period.multiplier}×</p>
          ))}
        </div>
      )}
      {official && (
        <section className="space-y-3">
          <h4 className="font-medium">{t('availableChannels.catalog.official')}</h4>
          <p className="text-xs surface-muted">{t('availableChannels.catalog.officialNote')}</p>
          <CatalogPriceTable pricing={official} />
        </section>
      )}
    </section>
  )
}

export default CatalogQuoteDetails
