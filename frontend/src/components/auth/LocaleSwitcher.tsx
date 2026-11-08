import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { availableLocales, setLocale } from '@/i18n'
import { Select, type SelectOption } from '@/components/ui'

export function LocaleSwitcher() {
  const { i18n } = useTranslation()
  const [switching, setSwitching] = useState(false)

  const currentLocaleCode = i18n.resolvedLanguage || i18n.language
  const currentLocale = availableLocales.find((item) => item.code === currentLocaleCode)

  const options: SelectOption[] = availableLocales.map((locale) => ({
    value: locale.code,
    label: `${locale.flag} ${locale.name}`
  }))

  async function selectLocale(code: string) {
    if (switching || code === currentLocaleCode) {
      return
    }
    setSwitching(true)
    try {
      await setLocale(code)
    } finally {
      setSwitching(false)
    }
  }

  return (
    <Select
      value={currentLocaleCode}
      disabled={switching}
      className="w-auto max-w-32"
      triggerClass="py-1.5 text-xs"
      ariaLabel={currentLocale?.name}
      searchable={false}
      options={options}
      onValueChange={(value) => void selectLocale(String(value))}
    />
  )
}

export default LocaleSwitcher
