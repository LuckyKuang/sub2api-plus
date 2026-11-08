import { describe, expect, it } from 'vitest'
import i18next from 'i18next'
import { initReactI18next } from 'react-i18next'

import en from '../locales/en'
import zh from '../locales/zh'

// 运行时才暴露的插值问题（占位符不闭合、英文中文参数集合不一致）会让组件渲染出
// 裸 key 或半句话，构建期完全无感。这里把全部文案过一遍 i18next 的插值器，
// 把这类问题固化为显式失败。
const PLACEHOLDER = /\{\{\s*([^{}]*?)\s*\}\}/g

function placeholders(message: string): string[] {
  const names: string[] = []
  let unbalanced = 0
  for (const character of message) {
    if (character === '{') unbalanced += 1
    if (character === '}') unbalanced -= 1
    if (unbalanced < 0) break
  }
  if (unbalanced !== 0) {
    throw new Error(`unbalanced braces: ${message}`)
  }
  for (const match of message.matchAll(PLACEHOLDER)) {
    names.push(match[1])
  }
  return names.sort()
}

function collect(node: unknown, path: string, out: Array<{ path: string; message: string }>): void {
  if (typeof node === 'string') {
    out.push({ path, message: node })
    return
  }
  if (Array.isArray(node)) {
    node.forEach((item, index) => collect(item, `${path}[${index}]`, out))
    return
  }
  if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
      collect(value, path ? `${path}.${key}` : key, out)
    }
  }
}

function leaves(messages: Record<string, unknown>): Array<{ path: string; message: string }> {
  const out: Array<{ path: string; message: string }> = []
  collect(messages, '', out)
  return out
}

function parameterMismatches(english: unknown, chinese: unknown, path = ''): string[] {
  if (typeof english === 'string' && typeof chinese === 'string') {
    return JSON.stringify(placeholders(english)) === JSON.stringify(placeholders(chinese)) ? [] : [path]
  }
  if (!english || typeof english !== 'object' || !chinese || typeof chinese !== 'object') return []
  return Object.entries(english).flatMap(([key, value]) =>
    parameterMismatches(value, (chinese as Record<string, unknown>)[key], path ? `${path}.${key}` : key)
  )
}

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

describe('locale messages compile', () => {
  it('uses the same interpolation parameters in English and Chinese', () => {
    expect(parameterMismatches(en, zh)).toEqual([])
  })

  it.each([
    ['zh', zh],
    ['en', en]
  ] as const)('%s messages all have well-formed placeholders', (_locale, messages) => {
    const malformed: string[] = []
    for (const { path, message } of leaves(messages)) {
      try {
        placeholders(message)
      } catch (error) {
        malformed.push(`${path}: ${(error as Error).message}`)
      }
    }
    expect(malformed).toEqual([])
  })

  it.each([
    ['zh', zh],
    ['en', en]
  ] as const)('%s messages all resolve through the i18next interpolator', async (locale, messages) => {
    const instance = await createInstance(locale, messages)
    const unresolved: string[] = []
    for (const { path, message } of leaves(messages)) {
      const rendered = instance.t(path, { defaultValue: message, count: 1, page: 1 })
      if (rendered === path) unresolved.push(path)
    }
    expect(unresolved).toEqual([])
  })
})
