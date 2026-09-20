import { describe, expect, it } from 'vitest'
import { getTimezoneOptions } from '@/utils/timezones'

describe('getTimezoneOptions', () => {
  it('returns options with value/label shape', () => {
    const options = getTimezoneOptions()
    expect(options.length).toBeGreaterThan(0)
    for (const opt of options) {
      expect(typeof opt.value).toBe('string')
      expect(opt.label).toContain(opt.value)
    }
  })

  it('labels carry UTC offset prefix', () => {
    const options = getTimezoneOptions()
    const la = options.find((o) => o.value === 'America/Los_Angeles')
    expect(la).toBeDefined()
    expect(la!.label).toMatch(/^\(UTC[+-]\d{2}:\d{2}\) America\/Los_Angeles$/)
  })

  it('contains common zones and is sorted by offset then name', () => {
    const options = getTimezoneOptions()
    const values = options.map((o) => o.value)
    for (const tz of ['UTC', 'Asia/Shanghai', 'America/Los_Angeles', 'Europe/London']) {
      expect(values).toContain(tz)
    }
    const offsets = options.map((o) => {
      const m = o.label.match(/UTC([+-])(\d{2}):(\d{2})/)
      if (!m) return 0
      const minutes = Number(m[2]) * 60 + Number(m[3])
      return m[1] === '-' ? -minutes : minutes
    })
    for (let i = 1; i < offsets.length; i++) {
      expect(offsets[i]).toBeGreaterThanOrEqual(offsets[i - 1])
    }
  })
})
