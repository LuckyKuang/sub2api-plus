import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'

import en from '../locales/en'
import zh from '../locales/zh'

describe('account scheduling threshold locale paths', () => {
  it('defines the account editor labels at admin.accounts', () => {
    expect(en.admin.accounts.accountSchedulingThresholdOverride).toBe(
      'Account Auto-Pause Threshold Override'
    )
    expect(en.admin.accounts.accountSchedulingThresholdOverrideHint).toBeTruthy()
    expect(en.admin.accounts.accountSchedulingThresholdOverrideValue).toBeTruthy()
    expect(en.admin.accounts.accountSchedulingThresholdOverrideDisabledHint).toBeTruthy()

    expect(zh.admin.accounts.accountSchedulingThresholdOverride).toBe('账号自动停调阈值覆盖')
    expect(zh.admin.accounts.accountSchedulingThresholdOverrideHint).toBeTruthy()
    expect(zh.admin.accounts.accountSchedulingThresholdOverrideValue).toBeTruthy()
    expect(zh.admin.accounts.accountSchedulingThresholdOverrideDisabledHint).toBeTruthy()
  })

  it.each([
    ['en', en],
    ['zh', zh]
  ] as const)('registers the account editor path in %s', (locale, messages) => {
    const i18n = createI18n({
      legacy: false,
      locale,
      messages: { [locale]: messages }
    })

    expect(i18n.global.te('admin.accounts.accountSchedulingThresholdOverride')).toBe(true)
  })

  it('does not nest account editor labels under account status', () => {
    const enStatus = en.admin.accounts.status as Record<string, unknown>
    const zhStatus = zh.admin.accounts.status as Record<string, unknown>

    expect(enStatus.accountSchedulingThresholdOverride).toBeUndefined()
    expect(zhStatus.accountSchedulingThresholdOverride).toBeUndefined()
  })
})
