import { describe, expect, it } from 'vitest'
import { formatBillingServiceTier, formatRequestedServiceTier, formatResponseServiceTier } from '@/utils/usageServiceTier'

const t = (key: string) => ({
  'admin.usage.serviceTierUnrecorded': '未记录',
  'admin.usage.serviceTierUnspecified': '未指定',
  'admin.usage.serviceTierNotReturned': '未返回',
  'usage.billingServiceTierFast': 'Fast',
  'usage.billingServiceTierStandard': 'Standard',
  'usage.billingServiceTierFlex': 'Flex',
  'usage.billingServiceTierUltrafast': 'Ultra Fast',
})[key] ?? key

describe('admin service tier diagnostics', () => {
  // Requirement: request Fast and response default must remain distinct.
  it.each(['priority', 'fast', ' PRIORITY '])('labels requested %s as Fast', tier => {
    expect(formatRequestedServiceTier(tier, t)).toBe('Fast')
  })
  it.each(['default', 'priority', 'fast', 'auto', 'provider-tier'])('preserves response %s', tier => {
    expect(formatResponseServiceTier(tier, t)).toBe(tier)
  })
  it('distinguishes historical records from newly captured absence', () => {
    expect(formatRequestedServiceTier(null, t)).toBe('未记录')
    expect(formatResponseServiceTier(undefined, t)).toBe('未记录')
    expect(formatRequestedServiceTier('', t)).toBe('未指定')
    expect(formatResponseServiceTier('', t)).toBe('未返回')
    expect(formatRequestedServiceTier('flex', t)).toBe('flex')
  })

  // Requirement: the billable tier is a separate, normalized fact. Missing or
  // unknown values are omitted rather than inferred as Standard.
  it.each([
    ['priority', 'Fast'],
    [' FAST ', 'Fast'],
    ['default', 'Standard'],
    ['standard', 'Standard'],
    ['auto', 'Standard'],
    ['scale', 'Standard'],
    ['flex', 'Flex'],
    ['ultrafast', 'Ultra Fast'],
  ])('labels billing tier %s as %s', (tier, label) => {
    expect(formatBillingServiceTier(tier, t)).toBe(label)
  })

  it.each([null, undefined, '', '   ', 'provider-tier'])('omits billing tier %s', tier => {
    expect(formatBillingServiceTier(tier, t)).toBeNull()
  })
})
