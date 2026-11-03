import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it, afterEach } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'

import TablePageLayout from '../TablePageLayout.vue'

enableAutoUnmount(afterEach)

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../TablePageLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

const mountLayout = () =>
  mount(TablePageLayout, {
    slots: {
      actions: '<button type="button">actions</button>',
      filters: '<input type="text" aria-label="filters" />',
      table: '<table><tbody><tr><td>row</td></tr></tbody></table>',
      pagination: '<nav aria-label="pagination" />',
    },
  })

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
  // Asserted on the rendered tree, not on the source text, so a stylesheet or
  // template edit that drops a zone class fails here.
  it('emits the canonical zone classes alongside its own section classes', () => {
    const wrapper = mountLayout()
    const classes = (selector: string) =>
      Array.from(wrapper.element.querySelectorAll(selector)).map(element =>
        Array.from(element.classList).sort().join(' ')
      )

    expect(wrapper.classes()).toContain('table-page-layout')
    expect(wrapper.classes()).toContain('workspace-page')

    // Two fixed zones (actions, filters) plus the pagination zone, each with the
    // canonical workspace-fixed class and its own section class.
    expect(classes('.layout-section-fixed')).toEqual([
      'layout-section-fixed workspace-fixed',
      'layout-section-fixed workspace-fixed',
      'layout-section-fixed table-page-pagination workspace-fixed',
    ])
    expect(classes('.table-page-pagination')).toEqual([
      'layout-section-fixed table-page-pagination workspace-fixed',
    ])

    // The scrolling zone keeps its own class and the canonical workspace-scroll.
    expect(classes('.layout-section-scrollable')).toEqual([
      'layout-section-scrollable workspace-scroll',
    ])
    // The table card is the workspace shell, so workspace.css can size and clip
    // it without knowing which layout component produced it.
    expect(classes('.workspace-shell')).toEqual([
      'card table-scroll-container workspace-shell workspace-table-card',
    ])

    // Every slot is rendered inside its zone, so the zones are not decorative.
    expect(wrapper.find('.layout-section-fixed button').text()).toBe('actions')
    expect(wrapper.find('.workspace-shell td').text()).toBe('row')
    expect(wrapper.find('.table-page-pagination nav').exists()).toBe(true)
  })

  it('omits the fixed zones whose slots are empty', () => {
    const wrapper = mount(TablePageLayout, {
      slots: { table: '<table><tbody><tr><td>row</td></tr></tbody></table>' },
    })
    expect(wrapper.find('.layout-section-fixed').exists()).toBe(false)
    expect(wrapper.find('.table-page-pagination').exists()).toBe(false)
    expect(wrapper.find('.layout-section-scrollable').exists()).toBe(true)
  })
})
