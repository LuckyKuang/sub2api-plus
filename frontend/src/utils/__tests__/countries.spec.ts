import { describe, expect, it } from 'vitest'
import { COUNTRY_CODES, getCountryOptions } from '@/utils/countries'

describe('COUNTRY_CODES', () => {
  it('contains all officially assigned ISO 3166-1 alpha-2 codes (249)', () => {
    expect(COUNTRY_CODES.length).toBe(249)
    expect(new Set(COUNTRY_CODES).size).toBe(249)
    for (const code of COUNTRY_CODES) {
      expect(code).toMatch(/^[A-Z]{2}$/)
    }
  })
})

describe('getCountryOptions', () => {
  it('localizes names for zh locale', () => {
    const options = getCountryOptions('zh-CN')
    const cn = options.find((o) => o.value === 'CN')
    expect(cn?.label).toBe(`CN · ${new Intl.DisplayNames(['zh-CN'], { type: 'region' }).of('CN')}`)
  })

  it('localizes names for en locale', () => {
    const options = getCountryOptions('en')
    const us = options.find((o) => o.value === 'US')
    expect(us?.label).toBe(`US · ${new Intl.DisplayNames(['en'], { type: 'region' }).of('US')}`)
  })

  it('returns every code exactly once and sorted by label', () => {
    const options = getCountryOptions('zh-CN')
    expect(options.map((o) => o.value)).toHaveLength(COUNTRY_CODES.length)
    const labels = options.map((o) => o.label)
    const sorted = [...labels].sort((a, b) => a.localeCompare(b, 'zh-CN'))
    expect(labels).toEqual(sorted)
  })

  it('falls back to raw code when DisplayNames is unavailable', () => {
    const options = getCountryOptions('zz-ZZ')
    const ad = options.find((o) => o.value === 'AD')
    expect(ad).toBeDefined()
    expect(ad!.label.startsWith('AD')).toBe(true)
  })
})
