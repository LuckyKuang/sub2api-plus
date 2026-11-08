import { useMemo } from 'react'
import {
  BarChart3,
  Bell,
  CreditCard,
  FileText,
  Folder,
  Gift,
  Globe,
  KeyRound,
  LayoutDashboard,
  ListOrdered,
  Plug,
  Receipt,
  Server,
  Settings,
  Shield,
  Signal,
  Ticket,
  Users,
  Wallet
} from 'lucide-react'
import type { PublicSettings } from '@/types'

export interface NavItem {
  path: string
  label: string
  icon?: React.ReactNode
  iconSvg?: string
  children?: NavItem[]
  hideInSimpleMode?: boolean
}

export interface NavFlags {
  asyncImage: boolean
  batchImage: boolean
  opsMonitoring: boolean
  adminPayment: boolean
}

export interface NavContext {
  t: (key: string) => string
  settings: Partial<PublicSettings> | null | undefined
  isAdmin: boolean
  isSimpleMode: boolean
  purchaseNavLabel: string
  customMenuItems: Array<{ id: string; label: string; icon_svg?: string }>
  channelMonitorMode: 'v2' | 'v3'
  supportUserId?: number | null
  flags: NavFlags
}

export const navIcons = {
  dashboard: <LayoutDashboard className="h-[18px] w-[18px]" />,
  chart: <BarChart3 className="h-[18px] w-[18px]" />,
  key: <KeyRound className="h-[18px] w-[18px]" />,
  batchImage: <FileText className="h-[18px] w-[18px]" />,
  channel: <Globe className="h-[18px] w-[18px]" />,
  signal: <Signal className="h-[18px] w-[18px]" />,
  creditCard: <CreditCard className="h-[18px] w-[18px]" />,
  recharge: <Wallet className="h-[18px] w-[18px]" />,
  orders: <ListOrdered className="h-[18px] w-[18px]" />,
  gift: <Gift className="h-[18px] w-[18px]" />,
  users: <Users className="h-[18px] w-[18px]" />,
  user: <Users className="h-[18px] w-[18px]" />,
  folder: <Folder className="h-[18px] w-[18px]" />,
  plugin: <Plug className="h-[18px] w-[18px]" />,
  bell: <Bell className="h-[18px] w-[18px]" />,
  server: <Server className="h-[18px] w-[18px]" />,
  shield: <Shield className="h-[18px] w-[18px]" />,
  ticket: <Ticket className="h-[18px] w-[18px]" />,
  receipt: <Receipt className="h-[18px] w-[18px]" />,
  settings: <Settings className="h-[18px] w-[18px]" />
}

/** Opt-out: visible until the backend explicitly sends `false`. */
function enabledByDefault(settings: NavContext['settings'], key: keyof PublicSettings): boolean {
  return settings?.[key] !== false
}

/** Opt-in: hidden until the backend explicitly sends `true`. */
function disabledByDefault(settings: NavContext['settings'], key: keyof PublicSettings): boolean {
  return settings?.[key] === true
}

function userNavItems(ctx: NavContext): NavItem[] {
  const { t, settings, flags } = ctx
  return [
    { path: '/dashboard', label: t('nav.dashboard'), icon: navIcons.dashboard },
    { path: '/keys', label: t('nav.apiKeys'), icon: navIcons.key },
    ...(flags.asyncImage
      ? [{ path: '/async-image', label: t('nav.asyncImage'), icon: navIcons.batchImage, hideInSimpleMode: true }]
      : []),
    ...(flags.batchImage
      ? [{ path: '/batch-image', label: t('nav.batchImage'), icon: navIcons.batchImage, hideInSimpleMode: true }]
      : []),
    { path: '/usage', label: t('nav.usage'), icon: navIcons.chart, hideInSimpleMode: true },
    ...(disabledByDefault(settings, 'available_channels_enabled')
      ? [
          {
            path: '/available-channels',
            label: t('nav.availableChannels'),
            icon: navIcons.channel,
            hideInSimpleMode: true
          }
        ]
      : []),
    ...(enabledByDefault(settings, 'channel_monitor_enabled')
      ? [
          {
            path: '/monitor',
            label: t(ctx.channelMonitorMode === 'v3' ? 'channelMonitorV3.title' : 'nav.channelStatus'),
            icon: navIcons.signal
          }
        ]
      : []),
    ...(enabledByDefault(settings, 'subscription_enabled')
      ? [
          {
            path: '/subscriptions',
            label: t('nav.mySubscriptions'),
            icon: navIcons.creditCard,
            hideInSimpleMode: true
          }
        ]
      : []),
    ...(enabledByDefault(settings, 'payment_enabled')
      ? [
          { path: '/purchase', label: ctx.purchaseNavLabel, icon: navIcons.recharge, hideInSimpleMode: true },
          { path: '/orders', label: t('nav.myOrders'), icon: navIcons.orders, hideInSimpleMode: true }
        ]
      : []),
    { path: '/redeem', label: t('nav.redeem'), icon: navIcons.gift, hideInSimpleMode: true },
    ...(disabledByDefault(settings, 'affiliate_enabled')
      ? [{ path: '/affiliate', label: t('nav.affiliate'), icon: navIcons.users, hideInSimpleMode: true }]
      : []),
    { path: '/profile', label: t('nav.profile'), icon: navIcons.user },
    ...(disabledByDefault(settings, 'mcp_enabled')
      ? [{ path: '/settings/mcp', label: t('mcp.title'), icon: navIcons.plugin }]
      : []),
    ...ctx.customMenuItems.map((item) => ({
      path: `/custom/${item.id}`,
      label: item.label,
      icon: null,
      iconSvg: item.icon_svg
    }))
  ]
}

function adminNavItems(ctx: NavContext): NavItem[] {
  const { t, settings, flags } = ctx
  return [
    { path: '/admin/dashboard', label: t('nav.dashboard'), icon: navIcons.dashboard },
    ...(flags.opsMonitoring ? [{ path: '/admin/ops', label: t('nav.ops'), icon: navIcons.chart }] : []),
    { path: '/admin/users', label: t('nav.users'), icon: navIcons.users, hideInSimpleMode: true },
    { path: '/admin/groups', label: t('nav.groups'), icon: navIcons.folder },
    {
      path: '/admin/channels',
      label: t('nav.channels'),
      icon: navIcons.channel,
      children: [
        { path: '/admin/channels/pricing', label: t('nav.channelPricing'), icon: navIcons.ticket },
        ...(enabledByDefault(settings, 'channel_monitor_enabled')
          ? [{ path: '/admin/channels/monitor', label: t('nav.channelMonitor'), icon: navIcons.signal }]
          : [])
      ]
    },
    ...(enabledByDefault(settings, 'subscription_enabled')
      ? [
          {
            path: '/admin/subscriptions',
            label: t('nav.subscriptions'),
            icon: navIcons.creditCard,
            hideInSimpleMode: true
          }
        ]
      : []),
    { path: '/admin/accounts', label: t('nav.accounts'), icon: navIcons.channel },
    ...(disabledByDefault(settings, 'plugin_management_enabled')
      ? [{ path: '/admin/plugins', label: t('nav.plugins'), icon: navIcons.plugin }]
      : []),
    { path: '/admin/announcements', label: t('nav.announcements'), icon: navIcons.bell },
    { path: '/admin/proxies', label: t('nav.proxies'), icon: navIcons.server },
    {
      path: '/admin/security-audit',
      label: t('nav.securityAudit'),
      icon: navIcons.shield,
      children: [
        ...(disabledByDefault(settings, 'risk_control_enabled')
          ? [
              { path: '/admin/risk-control', label: t('nav.contentModeration'), icon: navIcons.shield },
              { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: navIcons.shield }
            ]
          : []),
        ...(disabledByDefault(settings, 'global_ip_access_control_enabled')
          ? [{ path: '/admin/ip-access-control', label: t('nav.ipAccessControl'), icon: navIcons.shield }]
          : [])
      ]
    },
    { path: '/admin/redeem', label: t('nav.redeemCodes'), icon: navIcons.ticket, hideInSimpleMode: true },
    { path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: navIcons.gift, hideInSimpleMode: true },
    ...(disabledByDefault(settings, 'affiliate_enabled')
      ? [
          {
            path: '/admin/affiliates',
            label: t('nav.affiliates'),
            icon: navIcons.users,
            hideInSimpleMode: true,
            children: [
              { path: '/admin/affiliates/invites', label: t('nav.affiliateInviteRecords'), icon: navIcons.users },
              { path: '/admin/affiliates/rebates', label: t('nav.affiliateRebateRecords'), icon: navIcons.orders },
              {
                path: '/admin/affiliates/transfers',
                label: t('nav.affiliateTransferRecords'),
                icon: navIcons.creditCard
              }
            ]
          }
        ]
      : []),
    ...(flags.adminPayment
      ? [
          {
            path: '/admin/orders',
            label: t('nav.orders'),
            icon: navIcons.orders,
            hideInSimpleMode: true,
            children: [
              { path: '/admin/orders/dashboard', label: t('nav.paymentDashboard'), icon: navIcons.chart },
              { path: '/admin/orders', label: t('nav.orderManagement'), icon: navIcons.orders },
              { path: '/admin/orders/plans', label: t('nav.paymentPlans'), icon: navIcons.creditCard }
            ]
          }
        ]
      : []),
    { path: '/admin/usage', label: t('nav.usage'), icon: navIcons.chart },
    { path: '/admin/audit-logs', label: t('nav.auditLogs'), icon: navIcons.shield, hideInSimpleMode: true }
  ]
}

/** The console root menu: the console's own entry plus settings and custom pages. */
export function consoleNavItems(ctx: NavContext): NavItem[] {
  const { t, settings } = ctx
  const items: NavItem[] = [{ path: '/keys', label: t('nav.apiKeys'), icon: navIcons.key }]
  if (disabledByDefault(settings, 'mcp_enabled')) {
    items.push({ path: '/settings/mcp', label: t('mcp.title'), icon: navIcons.plugin })
  }
  if (ctx.isAdmin) {
    items.push({ path: '/admin/settings', label: t('nav.settings'), icon: navIcons.settings })
  }
  for (const item of ctx.customMenuItems) {
    items.push({ path: `/custom/${item.id}`, label: item.label, icon: null, iconSvg: item.icon_svg })
  }
  return items
}

export function navSections(ctx: NavContext): Array<{ items: NavItem[] }> {
  const userItems = userNavItems(ctx)
    .filter((item) => !(ctx.isSimpleMode && item.hideInSimpleMode))
    .map((item) => scopeSupportPath(item, ctx.supportUserId))

  const sections: Array<{ items: NavItem[] }> = [
    { items: consoleNavItems(ctx).map((item) => scopeSupportPath(item, ctx.supportUserId)) }
  ]

  if (ctx.isAdmin) {
    sections.push({
      items: adminNavItems(ctx).filter((item) => !(ctx.isSimpleMode && item.hideInSimpleMode))
    })
  }

  sections.push({ items: userItems })
  return sections
}

const SELF_PATHS: Record<string, string> = {
  '/dashboard': '/dashboard',
  '/keys': '/keys',
  '/usage': '/usage',
  '/redeem': '/redeem',
  '/orders': '/orders',
  '/subscriptions': '/subscriptions',
  '/available-channels': '/available-channels',
  '/profile': '/profile',
  '/affiliate': '/affiliate',
  '/monitor': '/monitor',
  '/settings/mcp': '/settings/mcp'
}

function scopeSupportPath(item: NavItem, supportUserId?: number | null): NavItem {
  if (!supportUserId) return item
  const selfPath = SELF_PATHS[item.path]
  if (!selfPath) return item
  return { ...item, path: `/admin/support/users/${supportUserId}${selfPath}` }
}

/** Stable helper so layout components can memoise the nav context cheaply. */
export function useNavContext(build: () => NavContext, deps: unknown[]): NavContext {
  // eslint-disable-next-line react-hooks/exhaustive-deps
  return useMemo(build, deps)
}
