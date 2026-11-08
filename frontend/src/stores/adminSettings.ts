import { create } from 'zustand'
import { adminAPI } from '@/api'
import type { CustomMenuItem } from '@/types'

interface AdminSettingsState {
  loaded: boolean
  loading: boolean
  opsMonitoringEnabled: boolean
  opsRealtimeMonitoringEnabled: boolean
  opsQueryModeDefault: string
  paymentEnabled: boolean
  customMenuItems: CustomMenuItem[]
  fetch: (force?: boolean) => Promise<void>
  setOpsMonitoringEnabledLocal: (value: boolean) => void
  setOpsRealtimeMonitoringEnabledLocal: (value: boolean) => void
  setPaymentEnabledLocal: (value: boolean) => void
  setOpsQueryModeDefaultLocal: (value: string) => void
}

const readCachedBool = (key: string, defaultValue: boolean): boolean => {
  try {
    const raw = localStorage.getItem(key)
    if (raw === 'true') return true
    if (raw === 'false') return false
  } catch {
    // ignore localStorage failures
  }
  return defaultValue
}

const writeCachedBool = (key: string, value: boolean) => {
  try {
    localStorage.setItem(key, value ? 'true' : 'false')
  } catch {
    // ignore localStorage failures
  }
}

const readCachedString = (key: string, defaultValue: string): string => {
  try {
    const raw = localStorage.getItem(key)
    if (typeof raw === 'string' && raw.length > 0) return raw
  } catch {
    // ignore localStorage failures
  }
  return defaultValue
}

const writeCachedString = (key: string, value: string) => {
  try {
    localStorage.setItem(key, value)
  } catch {
    // ignore localStorage failures
  }
}

// Default open, but honor cached value to reduce UI flicker on first paint.
const opsMonitoringEnabled = readCachedBool('ops_monitoring_enabled_cached', true)
const opsRealtimeMonitoringEnabled = readCachedBool('ops_realtime_monitoring_enabled_cached', true)
const opsQueryModeDefault = readCachedString('ops_query_mode_default_cached', 'auto')
const paymentEnabled = readCachedBool('payment_enabled_cached', false)

// Keep UI consistent if we learn that ops is disabled via feature-gated 404s.
// (event is dispatched from the axios interceptor)
let eventHandlerCleanup: (() => void) | null = null

function initializeEventListeners() {
  if (eventHandlerCleanup) return

  try {
    const handler = () => {
      useAdminSettingsStore.getState().setOpsMonitoringEnabledLocal(false)
    }
    window.addEventListener('ops-monitoring-disabled', handler)
    eventHandlerCleanup = () => {
      window.removeEventListener('ops-monitoring-disabled', handler)
    }
  } catch {
    // ignore window access failures (SSR)
  }
}

if (typeof window !== 'undefined') {
  initializeEventListeners()
}

export const useAdminSettingsStore = create<AdminSettingsState>()((set, get) => {
  async function fetch(force = false): Promise<void> {
    if (get().loaded && !force) return
    if (get().loading) return

    set({ loading: true })
    try {
      const [settings, paymentConfigResp] = await Promise.all([
        adminAPI.settings.getSettings(),
        adminAPI.payment.getConfig()
      ])

      const nextOpsMonitoringEnabled = settings.ops_monitoring_enabled ?? true
      writeCachedBool('ops_monitoring_enabled_cached', nextOpsMonitoringEnabled)
      set({ opsMonitoringEnabled: nextOpsMonitoringEnabled })

      const nextOpsRealtimeMonitoringEnabled = settings.ops_realtime_monitoring_enabled ?? true
      writeCachedBool('ops_realtime_monitoring_enabled_cached', nextOpsRealtimeMonitoringEnabled)
      set({ opsRealtimeMonitoringEnabled: nextOpsRealtimeMonitoringEnabled })

      const nextOpsQueryModeDefault = settings.ops_query_mode_default || 'auto'
      writeCachedString('ops_query_mode_default_cached', nextOpsQueryModeDefault)
      set({ opsQueryModeDefault: nextOpsQueryModeDefault })

      set({ customMenuItems: Array.isArray(settings.custom_menu_items) ? settings.custom_menu_items : [] })

      const nextPaymentEnabled = paymentConfigResp.data?.enabled ?? false
      writeCachedBool('payment_enabled_cached', nextPaymentEnabled)
      set({ paymentEnabled: nextPaymentEnabled })

      set({ loaded: true })
    } catch (err) {
      // Keep cached/default value: do not "flip" the UI based on a transient fetch failure.
      set({ loaded: true })
      console.error('[adminSettings] Failed to fetch settings:', err)
    } finally {
      set({ loading: false })
    }
  }

  function setOpsMonitoringEnabledLocal(value: boolean) {
    set({ opsMonitoringEnabled: value })
    writeCachedBool('ops_monitoring_enabled_cached', value)
    set({ loaded: true })
  }

  function setOpsRealtimeMonitoringEnabledLocal(value: boolean) {
    set({ opsRealtimeMonitoringEnabled: value })
    writeCachedBool('ops_realtime_monitoring_enabled_cached', value)
    set({ loaded: true })
  }

  function setPaymentEnabledLocal(value: boolean) {
    set({ paymentEnabled: value })
    writeCachedBool('payment_enabled_cached', value)
    set({ loaded: true })
  }

  function setOpsQueryModeDefaultLocal(value: string) {
    set({ opsQueryModeDefault: value || 'auto' })
    writeCachedString('ops_query_mode_default_cached', get().opsQueryModeDefault)
    set({ loaded: true })
  }

  return {
    loaded: false,
    loading: false,
    opsMonitoringEnabled,
    opsRealtimeMonitoringEnabled,
    opsQueryModeDefault,
    paymentEnabled,
    customMenuItems: [],
    fetch,
    setOpsMonitoringEnabledLocal,
    setOpsRealtimeMonitoringEnabledLocal,
    setPaymentEnabledLocal,
    setOpsQueryModeDefaultLocal
  }
})
