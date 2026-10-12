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
