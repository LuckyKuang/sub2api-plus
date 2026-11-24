import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar version badge visibility', () => {
  it('mounts VersionBadge only for confirmed admins', () => {
    expect(componentSource).toContain('<VersionBadge v-if="isAdmin" />')
    expect(componentSource).not.toContain(':version="siteVersion"')
    expect(componentSource).not.toContain('const siteVersion')
  })
})

describe('AppSidebar collapsible groups', () => {
  it('lets the user collapse a group even while a child route is active', () => {
    // The expand state must come from the user's override first, falling back
    // to the active-route heuristic only when the user has not clicked yet.
    expect(componentSource).toContain('const groupExpandOverrides = ref<Map<string, boolean>>(new Map())')
    expect(componentSource).not.toContain('expandedGroups.value.has(item.path) || isGroupActive(item)')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar admin console and user portal separation', () => {
  it('renders the admin console without personal menus and with a user portal entry', () => {
    expect(componentSource).toContain("const isAdmin = computed(() => authStore.isAdmin && portalArea.value === 'admin')")
    expect(componentSource).toContain("const isAdminUserPortal = computed(() => authStore.isAdmin && portalArea.value === 'user')")
    expect(componentSource).not.toContain('personalNavItems')
    expect(componentSource).not.toContain('isAdminSupportMode')
    expect(componentSource).not.toContain('parseAdminSupportTargetId(route.params.user_id)')
    expect(componentSource).toContain('id="sidebar-user-portal"')
    expect(componentSource).toContain("t('nav.userPortal')")
    expect(componentSource).not.toContain("label: t('nav.myAccount')")
  })

  it('mounts the account selector only in the administrator user portal and keeps target navigation scoped', () => {
    expect(componentSource).toContain('<AdminSupportUserSelector')
    expect(componentSource).toContain('v-if="isAdminUserPortal"')
    expect(componentSource).toContain('finalizeNav(buildSelfNavItems())')
    expect(componentSource).toContain('supportPathForPersonalPath(id, item.path)')
    expect(componentSource).toContain('adminSupportContext')
  })

  it('places the portal switch directly above the theme toggle', () => {
    const portalEntry = componentSource.indexOf('id="sidebar-user-portal"')
    const backToAdmin = componentSource.indexOf('id="sidebar-back-to-admin"')
    const themeToggle = componentSource.indexOf('<!-- Theme Toggle -->')
    expect(portalEntry).toBeGreaterThan(-1)
    expect(backToAdmin).toBeGreaterThan(portalEntry)
    expect(themeToggle).toBeGreaterThan(backToAdmin)
  })

  it('clears assistance when entering the user portal and returns through the admin console', () => {
    expect(componentSource).toContain('setAdminSupportContext(null)')
    expect(componentSource).toContain('router.push(USER_PORTAL_HOME)')
    expect(componentSource).toContain('router.push(ADMIN_PORTAL_HOME)')
    expect(componentSource).toContain('beginSupportTransition')
    expect(componentSource).toContain('endSupportTransition')
  })
})

describe('AppSidebar subscription feature flag', () => {
  it('gates the My Subscriptions entry behind the subscription public-settings flag', () => {
    expect(componentSource).toContain('const flagSubscription = makeSidebarFlag(FeatureFlags.subscription)')
    expect(componentSource).toMatch(/path: '\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('also hides the admin Subscription Management entry on recharge-only sites', () => {
    expect(componentSource).toMatch(/path: '\/admin\/subscriptions'[^\n]*featureFlag: flagSubscription/)
  })

  it('derives the purchase entry label from the site billing mode', () => {
    expect(componentSource).toContain("import { resolveSiteBillingMode } from '@/utils/siteBillingMode'")
    expect(componentSource).toMatch(/case 'recharge_only':\s*return t\('nav\.recharge'\)/)
    expect(componentSource).toMatch(/case 'subscription_only':\s*return t\('nav\.subscribe'\)/)
    expect(componentSource).toMatch(/path: '\/purchase'[^\n]*label: purchaseNavLabel\.value/)
  })
})

describe('AppSidebar MCP entry', () => {
  it('uses the MCP logo and shows the entry only when the deployment enables MCP', () => {
    expect(componentSource).toContain("render: () => h(Icon, { name: 'mcp' })")
    expect(componentSource).toContain('const flagMCP = makeSidebarFlag(FeatureFlags.mcp)')
    expect(componentSource).toMatch(/path: '\/settings\/mcp'[^\n]*icon: MCPIcon, featureFlag: flagMCP/)
    expect(componentSource).not.toMatch(/filtered\.push\(\{ path: '\/keys'/)
    expect(componentSource).not.toMatch(/if \(flagMCP\(\)\) filtered\.push/)
    expect(componentSource).not.toMatch(/path: '\/settings\/mcp'[^\n]*PluginIcon/)
  })
})
