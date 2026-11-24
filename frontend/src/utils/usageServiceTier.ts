type Translate = (key: string) => string

// NULL/undefined means historical or unrecorded; empty means captured absence.
export function formatRequestedServiceTier(tier: string | null | undefined, t: Translate): string {
  if (tier == null) return t('admin.usage.serviceTierUnrecorded')
  const value = tier.trim()
  if (!value) return t('admin.usage.serviceTierUnspecified')
  if (['priority', 'fast'].includes(value.toLowerCase())) return 'Fast'
  return value
}

export function formatResponseServiceTier(tier: string | null | undefined, t: Translate): string {
  if (tier == null) return t('admin.usage.serviceTierUnrecorded')
  return tier.trim() || t('admin.usage.serviceTierNotReturned')
}

// NULL/undefined/empty/unrecognized billing tiers are omitted from cost
// details: history and provider-specific values cannot prove a tier.
export function formatBillingServiceTier(tier: string | null | undefined, t: Translate): string | null {
  switch (tier?.trim().toLowerCase()) {
    case 'priority':
    case 'fast':
      return t('usage.billingServiceTierFast')
    case 'default':
    case 'standard':
    case 'auto':
    case 'scale':
      return t('usage.billingServiceTierStandard')
    case 'flex':
      return t('usage.billingServiceTierFlex')
    case 'ultrafast':
      return t('usage.billingServiceTierUltrafast')
    default:
      return null
  }
}
