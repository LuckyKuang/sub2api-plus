import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Select } from './ui/Select'
import { setLocale, availableLocales } from '@/i18n'

export interface LocaleSwitcherProps {
  className?: string
}

export function LocaleSwitcher({ className }: LocaleSwitcherProps) {
  const { i18n } = useTranslation()
  const [switching, setSwitching] = useState(false)
  const currentLocaleCode = i18n.language
  const currentLocale = availableLocales.find((item) => item.code === currentLocaleCode)

  const selectLocale = async (code: string) => {
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
      className={className}
      triggerClass="py-1.5 text-xs"
      ariaLabel={currentLocale?.name}
      onValueChange={(value) => void selectLocale(String(value))}
      searchable={false}
      options={availableLocales.map((locale) => ({
        value: locale.code,
        label: locale.flag + ' ' + locale.name
      }))}
    />
  )
}
