import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../TablePageLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('TablePageLayout responsive table scrolling', () => {
  it('does not disable the table horizontal scroll container in mobile mode', () => {
    const tableWrapperBlocks = Array.from(
      componentSource.matchAll(/([^{}]*:deep\(\.table-wrapper\)[^{}]*)\{([^{}]*)\}/g)
    )

    expect(tableWrapperBlocks.length).toBeGreaterThan(0)

    const baseBlock = tableWrapperBlocks.find(([selector]) => !selector.includes('.mobile-mode'))
    const mobileBlocks = tableWrapperBlocks.filter(([selector]) => selector.includes('.mobile-mode'))

    expect(baseBlock?.[2]).toContain('overflow-x-auto')
    expect(mobileBlocks.every(([, , declarations]) => !declarations.includes('overflow-visible'))).toBe(
      true
    )
  })

  // Both page-shell systems are one contract: the fixed sections and the
  // scrolling section carry the canonical zone classes, so a page built with
  // TablePageLayout behaves exactly like one built with workspace-* directly.
  it('emits the canonical zone classes alongside its own section classes', () => {
    expect(componentSource).toContain('class="table-page-layout workspace-page"')
    expect(componentSource.match(/class="layout-section-fixed workspace-fixed"/g)).toHaveLength(2)
    expect(componentSource).toContain('class="layout-section-scrollable workspace-scroll"')
    expect(componentSource).toContain('card table-scroll-container workspace-shell workspace-table-card')
    expect(componentSource).toContain('layout-section-fixed table-page-pagination workspace-fixed')
  })
})
