import { describe, expect, it } from 'vitest'
import i18next from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from '../locales/en'
import zh from '../locales/zh'

async function createInstance(locale: 'en' | 'zh', messages: Record<string, unknown>) {
  const instance = i18next.createInstance()
  await instance.use(initReactI18next).init({
    lng: locale,
    fallbackLng: locale,
    defaultNS: 'translation',
    resources: { [locale]: { translation: messages } },
    interpolation: { escapeValue: false },
    initImmediate: false,
    react: { useSuspense: false }
  })
  return instance
}

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
  ] as const)('registers the account editor path in %s', async (locale, messages) => {
    const instance = await createInstance(locale, messages)
    expect(instance.exists('admin.accounts.accountSchedulingThresholdOverride')).toBe(true)
  })

  it('does not nest account editor labels under account status', () => {
    const enStatus = en.admin.accounts.status as Record<string, unknown>
    const zhStatus = zh.admin.accounts.status as Record<string, unknown>

    expect(enStatus.accountSchedulingThresholdOverride).toBeUndefined()
    expect(zhStatus.accountSchedulingThresholdOverride).toBeUndefined()
  })
})
