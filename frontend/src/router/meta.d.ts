/**
 * Type definitions for Vue Router meta fields
 * Extends the RouteMeta interface with custom properties
 */

import 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    /**
     * Whether this route requires authentication
     * @default true
     */
    requiresAuth?: boolean

    /**
     * Whether this route requires admin role
     * @default false
     */
    requiresAdmin?: boolean

    /**
     * Page title for this route
     */
    title?: string

    /**
     * Optional breadcrumb items for navigation
     */
    breadcrumbs?: Array<{
      label: string
      to?: string
    }>

    /**
     * Icon name for this route (for sidebar navigation)
     */
    icon?: string

    /**
     * Whether to hide this route from navigation menu
     * @default false
     */
    hideInMenu?: boolean

    /**
     * Whether this route requires internal payment system to be enabled
     * @default false
     */
    requiresPayment?: boolean

    /**
     * 是否要求风控中心功能开关已启用
     * @default false
     */
    requiresRiskControl?: boolean

    /**
     * 是否要求全局 IP 访问控制功能总开关已启用
     * @default false
     */
    requiresIpAccessControl?: boolean

    /**
     * Whether this route requires async image access. Administrators always
     * have access; users need an eligible image-generation API key.
     * @default false
     */
    requiresAsyncImageAccess?: boolean

    /** Read-only administrator support resource for an explicit target user. */
    adminSupportResource?:
      | 'overview'
      | 'api-keys'
      | 'async-images'
      | 'usage'
      | 'channels'
      | 'channel-status'
      | 'subscriptions'
      | 'orders'
      | 'profile'

    /**
     * i18n key for the page title
     */
    titleKey?: string

    /**
     * i18n key for the page description
     */
    descriptionKey?: string
  }
}
