/**
 * Payment Store
 * Manages payment configuration, current order state, and subscription plans
 */

import { create } from 'zustand'
import { paymentAPI } from '@/api/payment'
import type { PaymentConfig, PaymentOrder, SubscriptionPlan, CreateOrderRequest, CreateOrderResult } from '@/types/payment'
import { onLocaleRefresh } from './localeRefresh'

interface PaymentState {
  /** Payment configuration from backend */
  config: PaymentConfig | null
  /** Currently active order (for payment flow) */
  currentOrder: PaymentOrder | null
  /** Available subscription plans */
  plans: SubscriptionPlan[]
  configLoading: boolean
  configLoaded: boolean
  fetchConfig: (force?: boolean) => Promise<PaymentConfig | null>
  fetchPlans: () => Promise<SubscriptionPlan[]>
  createOrder: (params: CreateOrderRequest) => Promise<CreateOrderResult>
  pollOrderStatus: (orderId: number) => Promise<PaymentOrder | null>
  clearCurrentOrder: () => void
}

export const usePaymentStore = create<PaymentState>()((set, get) => {
  let configPromise: Promise<PaymentConfig | null> | null = null
  let plansRequest = 0
  let plansLoading = false

  /** Fetch payment configuration */
  async function fetchConfig(force = false): Promise<PaymentConfig | null> {
    if (get().configLoaded && !force) return get().config
    if (configPromise) return configPromise

    configPromise = loadConfig()
    return configPromise
  }

  async function loadConfig(): Promise<PaymentConfig | null> {
    set({ configLoading: true })
    try {
      const response = await paymentAPI.getConfig()
      set({ config: response.data, configLoaded: true })
      return get().config
    } catch (error: unknown) {
      console.error('[payment] Failed to fetch config:', error)
      return null
    } finally {
      set({ configLoading: false })
      configPromise = null
    }
  }

  /** Fetch available subscription plans */
  async function fetchPlans(): Promise<SubscriptionPlan[]> {
    const request = ++plansRequest
    plansLoading = true
    try {
      const response = await paymentAPI.getPlans()
      if (request !== plansRequest) return get().plans
      // Backend returns features as newline-separated string; parse to array
      set({
        plans: (response.data || []).map((p: Omit<SubscriptionPlan, 'features'> & { features: string | string[] }) => ({
          ...p,
          features: typeof p.features === 'string'
            ? p.features.split('\n').map((f: string) => f.trim()).filter(Boolean)
            : (p.features || []),
        }))
      })
      return get().plans
    } catch (error: unknown) {
      console.error('[payment] Failed to fetch plans:', error)
      return []
    } finally {
      if (request === plansRequest) plansLoading = false
    }
  }

  onLocaleRefresh(() => (get().plans.length || plansLoading ? fetchPlans() : undefined))

  /** Create a new order and set it as current */
  async function createOrder(params: CreateOrderRequest) {
    const response = await paymentAPI.createOrder(params)
    return response.data
  }

  /** Poll order status by ID (read-only, no upstream check) */
  async function pollOrderStatus(orderId: number): Promise<PaymentOrder | null> {
    try {
      const response = await paymentAPI.getOrder(orderId)
      const order = response.data
      if (get().currentOrder?.id === orderId) {
        set({ currentOrder: order })
      }
      return order
    } catch (error: unknown) {
      console.error('[payment] Failed to poll order status:', error)
      return null
    }
  }

  /** Clear current order state */
  function clearCurrentOrder() {
    set({ currentOrder: null })
  }

  return {
    config: null,
    currentOrder: null,
    plans: [],
    configLoading: false,
    configLoaded: false,
    fetchConfig,
    fetchPlans,
    createOrder,
    pollOrderStatus,
    clearCurrentOrder
  }
})
