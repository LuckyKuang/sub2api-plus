import { lazy } from 'react'
import { Navigate } from 'react-router-dom'
import type { IndexRouteObject, NonIndexRouteObject } from 'react-router-dom'

/**
 * Route metadata mirrors the previous Vue Router table so titles, guards and
 * admin-support scoping keep identical semantics.
 */
export interface RouteMeta {
  requiresAuth?: boolean
  requiresAdmin?: boolean
  requiresPayment?: boolean
  title?: string
  titleKey?: string
  descriptionKey?: string
  adminSupportResource?: string
  hideSidebar?: boolean
  requiresRiskControl?: boolean
  requiresIpAccessControl?: boolean
  requiresSubscription?: boolean
  requiresAsyncImageAccess?: boolean
  /**
   * Console shell chrome for this route. The Vue views declared it themselves by
   * wrapping in `<AppLayout appearance="user">`; here the single shell layout
   * reads it off the matched route. Omitted means the admin console.
   */
  appearance?: 'user' | 'admin'
  /** Adds `app-layout-workspace`, which pins the main region to its own scroll. */
  workspace?: boolean
}

const view = (loader: () => Promise<{ default: React.ComponentType }>) => lazy(loader)

/** Route shape carrying our own metadata alongside react-router's. */
export type AppRoute = (IndexRouteObject | NonIndexRouteObject) & { handle?: RouteMeta }

// ==================== Setup ====================
export const SetupWizardView = view(() => import('@/views/setup/SetupWizardView'))

// ==================== Public ====================
export const HomeView = view(() => import('@/views/HomeView'))
export const LoginView = view(() => import('@/views/auth/LoginView'))
export const RegisterView = view(() => import('@/views/auth/RegisterView'))
export const EmailVerifyView = view(() => import('@/views/auth/EmailVerifyView'))
export const ForgotPasswordView = view(() => import('@/views/auth/ForgotPasswordView'))
export const ResetPasswordView = view(() => import('@/views/auth/ResetPasswordView'))
export const KeyUsageView = view(() => import('@/views/KeyUsageView'))
export const ModelPlazaView = view(() => import('@/views/ModelPlazaView'))
export const NotFoundView = view(() => import('@/views/NotFoundView'))
export const LegalDocumentView = view(() => import('@/views/public/LegalDocumentView'))

// ==================== Auth callbacks ====================
export const LinuxDoCallbackView = view(() => import('@/views/auth/LinuxDoCallbackView'))
export const WechatCallbackView = view(() => import('@/views/auth/WechatCallbackView'))
export const WechatPaymentCallbackView = view(() => import('@/views/auth/WechatPaymentCallbackView'))
export const DingTalkCallbackView = view(() => import('@/views/auth/DingTalkCallbackView'))
export const DingTalkEmailCompletionView = view(() => import('@/views/auth/DingTalkEmailCompletionView'))
export const OidcCallbackView = view(() => import('@/views/auth/OidcCallbackView'))
export const MCPOAuthAuthorizeView = view(() => import('@/views/auth/MCPOAuthAuthorizeView'))

// ==================== User ====================
export const DashboardView = view(() => import('@/views/user/DashboardView'))
export const KeysView = view(() => import('@/views/user/KeysView'))
export const UsageView = view(() => import('@/views/user/UsageView'))
export const RedeemView = view(() => import('@/views/user/RedeemView'))
export const AffiliateView = view(() => import('@/views/user/AffiliateView'))
export const AvailableChannelsView = view(() => import('@/views/user/AvailableChannelsView'))
export const MCPView = view(() => import('@/views/user/MCPView'))
export const ProfileView = view(() => import('@/views/user/ProfileView'))
export const SubscriptionsView = view(() => import('@/views/user/SubscriptionsView'))
export const UserOrdersView = view(() => import('@/views/user/UserOrdersView'))
export const PaymentView = view(() => import('@/views/user/PaymentView'))
export const RedeemShopView = view(() => import('@/views/user/RedeemShopView'))
export const PaymentQRCodeView = view(() => import('@/views/user/PaymentQRCodeView'))
export const PaymentResultView = view(() => import('@/views/user/PaymentResultView'))
export const StripePaymentView = view(() => import('@/views/user/StripePaymentView'))
export const StripePopupView = view(() => import('@/views/user/StripePopupView'))
export const AirwallexPaymentView = view(() => import('@/views/user/AirwallexPaymentView'))
export const CustomPageView = view(() => import('@/views/user/CustomPageView'))
export const ChannelStatusView = view(() => import('@/views/user/ChannelStatusView'))
export const BatchImageView = view(() => import('@/views/user/BatchImageGuideView'))
export const AsyncImageView = view(() => import('@/views/user/AsyncImageView'))

// ==================== Admin ====================
export const AdminDashboardView = view(() => import('@/views/admin/DashboardView'))
export const OpsDashboard = view(() => import('@/views/admin/ops/OpsDashboard'))
export const AuditLogView = view(() => import('@/views/admin/AuditLogView'))
export const IPAccessControlView = view(() => import('@/views/admin/IPAccessControlView'))
export const UsersView = view(() => import('@/views/admin/UsersView'))
export const GroupsView = view(() => import('@/views/admin/GroupsView'))
export const ChannelsView = view(() => import('@/views/admin/ChannelsView'))
export const ChannelMonitorView = view(() => import('@/views/admin/ChannelMonitorView'))
export const AdminSubscriptionsView = view(() => import('@/views/admin/SubscriptionsView'))
export const AccountsView = view(() => import('@/views/admin/AccountsView'))
export const PluginsView = view(() => import('@/views/admin/PluginsView'))
export const AnnouncementsView = view(() => import('@/views/admin/AnnouncementsView'))
export const ProxiesView = view(() => import('@/views/admin/ProxiesView'))
export const AdminRedeemView = view(() => import('@/views/admin/RedeemView'))
export const PromoCodesView = view(() => import('@/views/admin/PromoCodesView'))
export const SettingsView = view(() => import('@/views/admin/SettingsView'))
export const RiskControlView = view(() => import('@/views/admin/RiskControlView'))
export const PromptAuditView = view(() => import('@/features/prompt-audit/PromptAuditView'))
export const AdminUsageView = view(() => import('@/views/admin/UsageView'))
export const AdminAffiliateInvitesView = view(() => import('@/views/admin/affiliates/AdminAffiliateInvitesView'))
export const AdminAffiliateRebatesView = view(() => import('@/views/admin/affiliates/AdminAffiliateRebatesView'))
export const AdminAffiliateTransfersView = view(() => import('@/views/admin/affiliates/AdminAffiliateTransfersView'))
export const AdminPaymentDashboardView = view(() => import('@/views/admin/orders/AdminPaymentDashboardView'))
export const AdminOrdersView = view(() => import('@/views/admin/orders/AdminOrdersView'))
export const AdminPaymentPlansView = view(() => import('@/views/admin/orders/AdminPaymentPlansView'))
export const AdminSupportView = view(() => import('@/views/admin/support/AdminSupportView'))

const meta = (values: RouteMeta): RouteMeta => values

export const routes: AppRoute[] = [
  { path: '/setup', element: <SetupWizardView />, handle: meta({ requiresAuth: false, title: 'Setup' }) },
  { path: '/home', element: <HomeView />, handle: meta({ requiresAuth: false, title: 'Home' }) },
  {
    path: '/login',
    element: <LoginView />,
    handle: meta({ requiresAuth: false, title: 'Login', titleKey: 'home.login' })
  },
  {
    path: '/register',
    element: <RegisterView />,
    handle: meta({ requiresAuth: false, title: 'Register', titleKey: 'auth.createAccount' })
  },
  {
    path: '/email-verify',
    element: <EmailVerifyView />,
    handle: meta({ requiresAuth: false, title: 'Verify Email' })
  },
  {
    path: '/auth/linuxdo/callback',
    element: <LinuxDoCallbackView />,
    handle: meta({ requiresAuth: false, titleKey: 'auth.linuxdoCallbackPageTitle' })
  },
  {
    path: '/auth/wechat/callback',
    element: <WechatCallbackView />,
    handle: meta({ requiresAuth: false, titleKey: 'auth.wechatCallbackPageTitle' })
  },
  {
    path: '/auth/wechat/payment/callback',
    element: <WechatPaymentCallbackView />,
    handle: meta({ requiresAuth: false, titleKey: 'auth.wechatPaymentCallbackPageTitle' })
  },
  {
    path: '/auth/dingtalk/callback',
    element: <DingTalkCallbackView />,
    handle: meta({ requiresAuth: false, titleKey: 'auth.dingtalkCallbackPageTitle' })
  },
  {
    path: '/auth/dingtalk/email-completion',
    element: <DingTalkEmailCompletionView />,
    handle: meta({ requiresAuth: false, title: 'DingTalk Email Completion' })
  },
  {
    path: '/auth/oidc/callback',
    element: <OidcCallbackView />,
    handle: meta({ requiresAuth: false, titleKey: 'auth.oidcCallbackPageTitle' })
  },
  {
    path: '/forgot-password',
    element: <ForgotPasswordView />,
    handle: meta({ requiresAuth: false, titleKey: 'auth.forgotPasswordTitle' })
  },
  {
    path: '/reset-password',
    element: <ResetPasswordView />,
    handle: meta({ requiresAuth: false, title: 'Reset Password' })
  },
  {
    path: '/key-usage',
    element: <KeyUsageView />,
    handle: meta({ requiresAuth: false, title: 'Key Usage' })
  },
  {
    path: '/legal/:documentId',
    element: <LegalDocumentView />,
    handle: meta({ requiresAuth: false, title: 'Legal Document' })
  },
  {
    path: '/model-plaza',
    element: <ModelPlazaView />,
    handle: meta({ requiresAuth: false, titleKey: 'modelPlaza.title' })
  },
  { path: '/', element: <Navigate to="/home" replace /> },

  // ==================== User Routes ====================
  {
    path: '/dashboard',
    element: <DashboardView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'dashboard.title', appearance: 'user', workspace: true})
  },
  {
    path: '/keys',
    element: <KeysView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'keys.title', appearance: 'user', workspace: true})
  },
  {
    path: '/batch-image',
    element: <BatchImageView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'batchImage.title', appearance: 'user', workspace: true})
  },
  {
    path: '/async-image',
    element: <AsyncImageView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'asyncImage.title', appearance: 'user', workspace: true})
  },
  {
    path: '/usage',
    element: <UsageView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'usage.title', appearance: 'user', workspace: true})
  },
  {
    path: '/redeem',
    element: <RedeemView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'redeem.title', appearance: 'user', workspace: true})
  },
  {
    path: '/affiliate',
    element: <AffiliateView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'affiliate.title', appearance: 'user', workspace: true})
  },
  {
    path: '/available-channels',
    element: <AvailableChannelsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'availableChannels.title', appearance: 'user', workspace: true})
  },
  {
    path: '/settings/mcp',
    element: <MCPView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'mcp.title' })
  },
  {
    path: '/oauth/mcp/authorize',
    element: <MCPOAuthAuthorizeView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'mcp.consentTitle' })
  },
  {
    path: '/profile',
    element: <ProfileView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'profile.title', appearance: 'user'})
  },
  {
    path: '/subscriptions',
    element: <SubscriptionsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'userSubscriptions.title', appearance: 'user', workspace: true})
  },
  {
    path: '/purchase/redeem-shop',
    element: <RedeemShopView />,
    handle: meta({ requiresAuth: true, requiresPayment: true, titleKey: 'featureExtensions.purchaseTitle', appearance: 'user'})
  },
  {
    path: '/purchase',
    element: <PaymentView />,
    handle: meta({ requiresAuth: true, requiresPayment: true, titleKey: 'nav.buySubscription', appearance: 'user', workspace: true})
  },
  {
    path: '/orders',
    element: <UserOrdersView />,
    handle: meta({ requiresAuth: true, requiresPayment: true, titleKey: 'nav.myOrders', appearance: 'user', workspace: true})
  },
  {
    path: '/payment/qrcode',
    element: <PaymentQRCodeView />,
    handle: meta({ requiresAuth: true, requiresPayment: true, titleKey: 'payment.qr.scanToPay', appearance: 'user'})
  },
  {
    path: '/payment/result',
    element: <PaymentResultView />,
    handle: meta({ requiresAuth: false, requiresPayment: false, titleKey: 'payment.result.success' })
  },
  {
    path: '/payment/stripe',
    element: <StripePaymentView />,
    handle: meta({ requiresAuth: false, requiresPayment: false, titleKey: 'payment.stripePay' })
  },
  {
    path: '/payment/airwallex',
    element: <AirwallexPaymentView />,
    handle: meta({ requiresAuth: false, requiresPayment: false, titleKey: 'payment.airwallexPay', appearance: 'user'})
  },
  {
    path: '/payment/stripe-popup',
    element: <StripePopupView />,
    handle: meta({ requiresAuth: false, requiresPayment: false, title: 'Payment' })
  },
  {
    path: '/custom/:id',
    element: <CustomPageView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'customPage.title', appearance: 'user'})
  },
  {
    path: '/monitor',
    element: <ChannelStatusView />,
    handle: meta({ requiresAuth: true, requiresAdmin: false, titleKey: 'nav.channelStatus', appearance: 'user', workspace: true})
  },

  // ==================== Admin Routes ====================
  { path: '/admin', element: <Navigate to="/admin/dashboard" replace /> },
  {
    path: '/admin/support/users/:user_id/:resource',
    element: <AdminSupportView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, adminSupportResource: 'custom' })
  },
  {
    path: '/admin/dashboard',
    element: <AdminDashboardView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.dashboard.title' })
  },
  {
    path: '/admin/ops',
    element: <OpsDashboard />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.ops.title' })
  },
  {
    path: '/admin/audit-logs',
    element: <AuditLogView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.audit.title' })
  },
  {
    path: '/admin/ip-access-control',
    element: <IPAccessControlView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.ipAccessControl.title' })
  },
  {
    path: '/admin/users',
    element: <UsersView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.users.title' })
  },
  {
    path: '/admin/groups',
    element: <GroupsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.groups.title' })
  },
  {
    path: '/admin/channels/pricing',
    element: <ChannelsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.channels.title' })
  },
  {
    path: '/admin/channels/monitor',
    element: <ChannelMonitorView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.channelMonitor.title' })
  },
  {
    path: '/admin/subscriptions',
    element: <AdminSubscriptionsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.subscriptions.title' })
  },
  {
    path: '/admin/accounts',
    element: <AccountsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.accounts.title' })
  },
  {
    path: '/admin/plugins',
    element: <PluginsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.plugins.title' })
  },
  {
    path: '/admin/announcements',
    element: <AnnouncementsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.announcements.title' })
  },
  {
    path: '/admin/proxies',
    element: <ProxiesView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.proxies.title' })
  },
  {
    path: '/admin/redeem',
    element: <AdminRedeemView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.redeem.title' })
  },
  {
    path: '/admin/promo-codes',
    element: <PromoCodesView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.promo.title' })
  },
  {
    path: '/admin/settings',
    element: <SettingsView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.settings.title' })
  },
  {
    path: '/admin/risk-control',
    element: <RiskControlView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.riskControl.title' })
  },
  {
    path: '/admin/prompt-audit',
    element: <PromptAuditView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.promptAudit.title' })
  },
  {
    path: '/admin/usage',
    element: <AdminUsageView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'admin.usage.title' })
  },
  {
    path: '/admin/affiliates/invites',
    element: <AdminAffiliateInvitesView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'nav.affiliateInviteRecords' })
  },
  {
    path: '/admin/affiliates/rebates',
    element: <AdminAffiliateRebatesView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'nav.affiliateRebateRecords' })
  },
  {
    path: '/admin/affiliates/transfers',
    element: <AdminAffiliateTransfersView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, titleKey: 'nav.affiliateTransferRecords' })
  },
  {
    path: '/admin/orders/dashboard',
    element: <AdminPaymentDashboardView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, requiresPayment: true, titleKey: 'nav.paymentDashboard' })
  },
  {
    path: '/admin/orders',
    element: <AdminOrdersView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, requiresPayment: true, titleKey: 'nav.orderManagement' })
  },
  {
    path: '/admin/orders/plans',
    element: <AdminPaymentPlansView />,
    handle: meta({ requiresAuth: true, requiresAdmin: true, requiresPayment: true, titleKey: 'nav.paymentPlans' })
  },

  // ==================== 404 ====================
  { path: '*', element: <NotFoundView />, handle: meta({ title: '404 Not Found' }) }
]
