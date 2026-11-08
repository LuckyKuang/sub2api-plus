import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAppStore } from '@/stores/app'
import type { LoginAgreementDocument } from '@/types'

const LOGIN_AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'

export interface LoginAgreementSettings {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}

export interface LoginAgreementState {
  enabled: boolean
  mode: 'modal' | 'checkbox'
  updatedAt: string
  documents: LoginAgreementDocument[]
  accepted: boolean
  modalVisible: boolean
  gateActive: boolean
  applySettings: (settings: LoginAgreementSettings) => void
  accept: () => void
  reject: (rejectedWarningKey: string) => void
  openModal: () => void
}

function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }
  try {
    const raw = localStorage.getItem(LOGIN_AGREEMENT_STORAGE_KEY)
    if (!raw) {
      return false
    }
    const parsed = JSON.parse(raw) as { revision?: string }
    return parsed.revision === revision
  } catch {
    return false
  }
}

/**
 * Shared login-agreement consent gate for the sign-in and registration forms.
 * A missing revision fails closed so a changed document list always re-prompts.
 */
export function useLoginAgreement(): LoginAgreementState {
  const { t } = useTranslation()
  const [enabled, setEnabled] = useState(false)
  const [mode, setMode] = useState<'modal' | 'checkbox'>('modal')
  const [updatedAt, setUpdatedAt] = useState('')
  const [revision, setRevision] = useState('')
  const [documents, setDocuments] = useState<LoginAgreementDocument[]>([])
  const [accepted, setAccepted] = useState(false)
  const [modalVisible, setModalVisible] = useState(false)

  const applySettings = useCallback((settings: LoginAgreementSettings) => {
    const filtered = Array.isArray(settings.login_agreement_documents)
      ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
      : []
    const nextEnabled = settings.login_agreement_enabled === true && filtered.length > 0
    const nextMode = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
    const nextUpdatedAt = settings.login_agreement_updated_at || ''
    const nextRevision =
      settings.login_agreement_revision ||
      `${nextUpdatedAt}:${filtered.map((doc) => `${doc.id}:${doc.title}`).join('|')}`
    const nextAccepted = !nextEnabled || hasAcceptedLoginAgreement(nextRevision)

    setDocuments(filtered)
    setEnabled(nextEnabled)
    setMode(nextMode as 'modal' | 'checkbox')
    setUpdatedAt(nextUpdatedAt)
    setRevision(nextRevision)
    setAccepted(nextAccepted)
    setModalVisible(nextEnabled && !nextAccepted && nextMode !== 'checkbox')
  }, [])

  const accept = useCallback(() => {
    if (revision) {
      localStorage.setItem(
        LOGIN_AGREEMENT_STORAGE_KEY,
        JSON.stringify({
          revision,
          accepted_at: new Date().toISOString()
        })
      )
    }
    setAccepted(true)
    setModalVisible(false)
  }, [revision])

  const reject = useCallback(
    (rejectedWarningKey: string) => {
      localStorage.removeItem(LOGIN_AGREEMENT_STORAGE_KEY)
      setAccepted(false)
      setModalVisible(false)
      useAppStore.getState().showWarning(t(rejectedWarningKey))
    },
    [t]
  )

  const openModal = useCallback(() => setModalVisible(true), [])

  const gateActive = useMemo(() => enabled && !accepted, [enabled, accepted])

  return {
    enabled,
    mode,
    updatedAt,
    documents,
    accepted,
    modalVisible,
    gateActive,
    applySettings,
    accept,
    reject,
    openModal
  }
}

export default useLoginAgreement
