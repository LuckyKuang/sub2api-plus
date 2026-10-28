/**
 * Structure contracts: channel-monitor-v2 + studio shells must use project
 * design-system utility classes rather than isolated flat RGB skins.
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../../..')

function read(rel: string) {
  return readFileSync(resolve(root, rel), 'utf8')
}

describe('channel-monitor-v2 design system structure', () => {
  it('user ChannelStatus V2 shell uses page-header, card, btn, tabs utilities', () => {
    // Route wrapper may switch V1/V2; design chrome lives on the V2 implementation.
    const src = read('views/user/ChannelStatusV2View.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('page-title')
    expect(src).toContain('class="card')
    expect(src).toContain('btn btn-secondary')
    expect(src).toContain('class="tab')
    expect(src).toContain('tab-active')
    expect(src).toContain('badge badge-warning')
    // Compact single-row toolbar
    expect(src).toContain('monitor-toolbar')
    expect(src).toContain('clearFilters')
    expect(src).toContain('healthModeOptions')
    expect(src).toContain("'cache'")
    // Approved transparent layout: the fixed toolbar is outside the only
    // activity scrollport, so content cannot travel behind it.
    expect(src).toContain('<AppLayout workspace>')
    expect(src).toContain('channel-monitor-toolbar card workspace-shell workspace-fixed')
    expect(src).toContain('data-testid="channel-status-activity"')
    expect(src).not.toContain('top-[var(--shell-sticky-offset)]')
    expect(src).not.toContain('dark:!bg-dark-800')
    // Overview-first KPI strip before primary viz
    expect(src.indexOf('summaryAria')).toBeLessThan(src.indexOf('MonitorTrendChart'))
    // No page-level fixed min-width that forces viewport horizontal scroll
    expect(src).not.toMatch(/min-width:\s*980px/)
    expect(src).not.toMatch(/min-w-\[980px\]/)
    // Dense tables scroll internally
    expect(src).toContain('--record-table-max-height: max(0px, calc(min(52vh, 520px)')
    expect(src).toContain('max-height: var(--record-table-max-height)')
    expect(src).toContain('monitor-tab-viewport record-table-scroll table-container min-h-0 overflow-auto')
    // Trend view toggle (pulse matrix / line chart) + default platform/group dimension
    expect(src).toContain("trendView")
    expect(src).toContain("'platform_group'")
    expect(src).toContain('MonitorTrendChart')
  })

  it('keeps the main visualization borderless across matrix, line and loading states', () => {
    const view = read('views/user/ChannelStatusV2View.vue')
    expect(view).toMatch(/<MonitorTrendChart\b[^>]*class="workspace-shell"/s)
    expect(view).toMatch(/<div\s+v-else-if="loading"\s+class="card workspace-shell[^"]*"/s)
    expect(read('features/channel-monitor-v2/RelayPulseMatrix.vue')).toContain('class="card workspace-shell')
    // Reusing the chart elsewhere retains its ordinary independent card frame.
    expect(read('features/channel-monitor-v2/MonitorTrendChart.vue')).not.toContain('workspace-shell')
  })

  it('RelayPulseMatrix uses card chrome, matrix scroll, and hover tooltips (no click modal)', () => {
    const src = read('features/channel-monitor-v2/RelayPulseMatrix.vue')
    expect(src).toContain('class="card')
    expect(src).toContain('card-header')
    expect(src).toContain('card-body')
    expect(src).toContain('matrix-scroll')
    // Header/body are native sibling viewports; scroll handlers do not drive
    // a vertical mask that can lag behind compositor scrolling.
    expect(src).toContain('matrix-header-rail')
    expect(src).not.toContain('--matrix-scroll-top')
    expect(src).not.toContain('clip-path:')
    expect(src).not.toContain('paddingStart:')
    expect(src).not.toContain('scrollPaddingStart:')
    expect(src).not.toContain('matrix-row sticky')
    expect(src).toMatch(/max-h-\[min\(42vh/)
    expect(src).toContain('overflow-auto')
    expect(src).toContain('MatrixTooltip')
    expect(read('features/channel-monitor-v2/MatrixTooltip.vue')).toContain('role="tooltip"')
    expect(src).not.toContain('!rounded-3xl')
    expect(src).not.toContain('dark:!bg-dark-800')
    expect(src).not.toContain('modal-overlay')
    expect(src).not.toContain('modal-content')
  })

  it('MetricCell uses stat-card utility', () => {
    const src = read('features/channel-monitor-v2/MetricCell.vue')
    expect(src).toContain('stat-card')
    expect(src).toContain('stat-label')
    expect(src).toContain('stat-value')
    expect(src).not.toContain('!rounded-3xl')
  })

  it('MonitorTrendChart uses Ops chart shell tokens', () => {
    const src = read('features/channel-monitor-v2/MonitorTrendChart.vue')
    expect(src).toContain('class="card')
    expect(src).not.toContain('!rounded-3xl')
    expect(src).not.toContain('dark:!bg-dark-800')
    expect(src).toContain('EmptyState')
    expect(src).toContain('min-h-[360px]')
  })

  it('FilterMultiSelect uses rounded-xl input chrome and dropdown utility', () => {
    const src = read('features/channel-monitor-v2/FilterMultiSelect.vue')
    expect(src).toContain('rounded-xl')
    expect(src).toContain('dropdown')
    expect(src).toContain('dropdown-item')
  })

  it('MonitorSettingsPanel uses page-header, card, btn-primary, tabs', () => {
    const src = read('features/channel-monitor-v2/MonitorSettingsPanel.vue')
    expect(src).toContain('page-header')
    expect(src).toContain('btn btn-primary')
    expect(src).toContain('class="card')
    expect(src).toContain('tab-active')
    expect(src).toMatch(/max-h-\[min\(40vh/)
  })

  it('admin ChannelMonitorView uses shared accessible rounded workspace tabs', () => {
    const src = read('views/admin/ChannelMonitorView.vue')
    const tabs = read('components/common/WorkspaceTabs.vue')
    const styles = read('styles/workspace.css')
    expect(src).toContain('page-header')
    expect(src).toContain('page-title')
    expect(src).toContain('<WorkspaceTabs v-model="adminMonitorTab"')
    expect(tabs).toContain('card workspace-tabs workspace-fixed')
    expect(tabs).toContain('role="tablist"')
    expect(tabs).toContain('role="tab"')
    expect(tabs).toContain(':aria-selected="modelValue === tab.value"')
    expect(styles).toMatch(/\.workspace-tab\s*\{[^}]*border-radius:\s*9px/s)
    expect(styles).toContain('.workspace-tab[aria-selected="true"]')
    expect(src).toContain('MonitorSettingsPanel')
  })
})
