import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'

// Use the real application stylesheet/configuration, not a mock of utility
// classes. jsdom can verify computed declarations, but not pixel geometry.
export async function installAppStyles(): Promise<() => void> {
  const from = resolve(process.cwd(), 'src/style.css')
  const config = resolve(process.cwd(), 'tailwind.config.js')
  const result = await postcss([tailwindcss({ config })]).process(
    readFileSync(from, 'utf8'), { from },
  )
  const style = document.createElement('style')
  style.textContent = result.css
  document.head.appendChild(style)
  return () => style.remove()
}

export function cssPixels(value: string): number {
  if (value === '' || value === '0') return 0
  if (value.endsWith('px')) return Number.parseFloat(value)
  if (value.endsWith('rem')) {
    const rootSize = Number.parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    return Number.parseFloat(value) * rootSize
  }
  throw new Error(`Unsupported CSS length: ${value}`)
}
