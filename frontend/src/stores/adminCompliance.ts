import { create } from 'zustand'
import adminComplianceAPI, { type AdminComplianceStatus } from '@/api/admin/compliance'
import { getLocale } from '@/i18n'

const FALLBACK_ZH_PHRASE = '我已阅读、理解并同意 Sub2API Plus 部署与运营合规承诺'
const FALLBACK_EN_PHRASE = 'I have read, understood, and agree to the Sub2API Plus Deployment and Operation Compliance Commitment'

interface AdminComplianceState {
  status: AdminComplianceStatus | null
  loading: boolean
  submitting: boolean
  initialized: boolean
  required: () => boolean
  shouldShow: () => boolean
  expectedPhrase: () => string
  fetchStatus: () => Promise<AdminComplianceStatus>
  accept: (phrase: string) => Promise<AdminComplianceStatus>
  requireAcknowledgement: (partialStatus?: Partial<AdminComplianceStatus>) => void
  reset: () => void
}

export const useAdminComplianceStore = create<AdminComplianceState>()((set, get) => {
  let stateVersion = 0
  // Internal bookkeeping kept out of the public store surface, like the Vue ref it replaces.
  let forceVisible = false

  async function fetchStatus(): Promise<AdminComplianceStatus> {
    const version = stateVersion
    set({ loading: true })
    try {
      const nextStatus = await adminComplianceAPI.getStatus()
      if (version !== stateVersion) return nextStatus
      forceVisible = nextStatus.required
      set({
        status: nextStatus,
        initialized: true
      })
      return nextStatus
    } finally {
      if (version === stateVersion) set({ loading: false })
    }
  }

  async function accept(phrase: string): Promise<AdminComplianceStatus> {
    const version = stateVersion
    set({ submitting: true })
    try {
      const nextStatus = await adminComplianceAPI.accept({
        phrase,
        language: getLocale()
      })
      if (version !== stateVersion) return nextStatus
      forceVisible = nextStatus.required
      set({ status: nextStatus })
      return nextStatus
    } finally {
      if (version === stateVersion) set({ submitting: false })
    }
  }

  function requireAcknowledgement(partialStatus?: Partial<AdminComplianceStatus>): void {
    forceVisible = true
    set({
      status: {
        required: true,
        version: partialStatus?.version || get().status?.version || 'v2026.07.25-plus.1',
        document_path_zh: partialStatus?.document_path_zh || get().status?.document_path_zh || 'docs/legal/admin-compliance.zh.md',
        document_path_en: partialStatus?.document_path_en || get().status?.document_path_en || 'docs/legal/admin-compliance.en.md',
        document_url_zh: partialStatus?.document_url_zh || get().status?.document_url_zh || 'https://github.com/luckykuang/sub2api-plus/blob/main/docs/legal/admin-compliance.zh.md',
        document_url_en: partialStatus?.document_url_en || get().status?.document_url_en || 'https://github.com/luckykuang/sub2api-plus/blob/main/docs/legal/admin-compliance.en.md',
        ack_phrase_zh: partialStatus?.ack_phrase_zh || get().status?.ack_phrase_zh || FALLBACK_ZH_PHRASE,
        ack_phrase_en: partialStatus?.ack_phrase_en || get().status?.ack_phrase_en || FALLBACK_EN_PHRASE,
        acknowledgement: get().status?.acknowledgement
      },
      initialized: true
    })
  }

  function reset(): void {
    stateVersion++
    forceVisible = false
    set({
      status: null,
      loading: false,
      submitting: false,
      initialized: false
    })
  }

  return {
    status: null,
    loading: false,
    submitting: false,
    initialized: false,
    required: () => get().status?.required === true,
    shouldShow: () => (get().status?.required === true) || forceVisible,
    expectedPhrase: () => {
      if (getLocale() === 'zh') {
        return get().status?.ack_phrase_zh || FALLBACK_ZH_PHRASE
      }
      return get().status?.ack_phrase_en || FALLBACK_EN_PHRASE
    },
    fetchStatus,
    accept,
    requireAcknowledgement,
    reset
  }
})
