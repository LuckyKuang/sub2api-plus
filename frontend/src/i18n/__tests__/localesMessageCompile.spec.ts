import { describe, expect, it } from 'vitest'
import { baseCompile } from '@intlify/message-compiler'

import en from '../locales/en'
import zh from '../locales/zh'

// vue-i18n 在运行时才编译消息：文案里未转义的花括号（如内嵌 JSON 示例
// "{\"user-agent\": ...}"）会在渲染时抛 "Invalid token in placeholder"，
// 直接炸掉整个组件树，且构建期完全无感。本测试把全部文案预编译一遍，
// 将该类问题固化为显式失败。字面量花括号请用 {'{'} / {'}'} 转义，
// 或将语言中立的示例文本（如 JSON）移出 i18n。
function collectCompileErrors(node: unknown, path: string, out: string[]): void {
  if (typeof node === 'string') {
    baseCompile(node, {
      onError: (err) => {
        out.push(`${path}: ${err.message}`)
      }
    })
    return
  }
  if (Array.isArray(node)) {
    node.forEach((item, index) => collectCompileErrors(item, `${path}[${index}]`, out))
    return
  }
  if (node && typeof node === 'object') {
    for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
      collectCompileErrors(value, path ? `${path}.${key}` : key, out)
    }
  }
}

function parameters(message: string): string[] {
  const values = new Set<string>()
  function visit(node: unknown): void {
    if (!node || typeof node !== 'object') return
    const ast = node as Record<string, unknown>
    if (ast.type === 4) values.add(`named:${ast.key}`)
    if (ast.type === 5) values.add(`list:${ast.index}`)
    Object.values(ast).forEach(value => Array.isArray(value) ? value.forEach(visit) : visit(value))
  }
  visit(baseCompile(message).ast)
  return [...values].sort()
}

function parameterMismatches(english: unknown, chinese: unknown, path = ''): string[] {
  if (typeof english === 'string' && typeof chinese === 'string') {
    return JSON.stringify(parameters(english)) === JSON.stringify(parameters(chinese)) ? [] : [path]
  }
  if (!english || typeof english !== 'object' || !chinese || typeof chinese !== 'object') return []
  return Object.entries(english).flatMap(([key, value]) =>
    parameterMismatches(value, (chinese as Record<string, unknown>)[key], path ? `${path}.${key}` : key)
  )
}

describe('locale messages compile', () => {
  it('uses the same interpolation parameters in English and Chinese', () => {
    expect(parameterMismatches(en, zh)).toEqual([])
  })
  it.each([
    ['zh', zh],
    ['en', en]
  ] as const)('%s messages all compile without placeholder errors', (locale, messages) => {
    const errors: string[] = []
    collectCompileErrors(messages, locale, errors)
    expect(errors).toEqual([])
  })
})
