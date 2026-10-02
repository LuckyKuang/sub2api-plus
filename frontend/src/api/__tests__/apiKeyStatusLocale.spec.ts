import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

// Keep this cross-module contract test outside check:i18n: the frontend Docker
// build stage contains only frontend sources and legal documents.
const here = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(here, '../../../../backend/internal/service/api_key.go'), 'utf8')
const statuses = [...source.matchAll(/\bStatusAPIKey\w+\s*=\s*"([\w]+)"/g)].map(match => match[1])

describe('backend API key status translations', () => {
  it.each([{ locale: 'en', messages: en }, { locale: 'zh', messages: zh }])(
    'translates all backend states in $locale', ({ messages }) => {
      expect(statuses.length).toBeGreaterThan(0)
      const labels = messages.keys.status as Record<string, string>
      const missing = statuses.filter(status => !labels[status]?.trim())
      expect(missing).toEqual([])
    }
  )
})
