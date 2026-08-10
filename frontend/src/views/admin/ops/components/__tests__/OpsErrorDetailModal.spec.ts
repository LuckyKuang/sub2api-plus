import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OpsErrorDetailModal from '../OpsErrorDetailModal.vue'
import type { OpsErrorDetail } from '@/api/admin/ops'

const {
  getRequestErrorDetail,
  getUpstreamErrorDetail,
  listRequestErrorUpstreamErrors,
} = vi.hoisted(() => ({
  getRequestErrorDetail: vi.fn(),
  getUpstreamErrorDetail: vi.fn(),
  listRequestErrorUpstreamErrors: vi.fn(),
}))

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getRequestErrorDetail,
    getUpstreamErrorDetail,
    listRequestErrorUpstreamErrors,
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
}))

vi.mock('@/utils/format', () => ({
  formatDateTime: (value: string) => value,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function errorDetail(overrides: Partial<OpsErrorDetail> = {}): OpsErrorDetail {
  return {
    id: 41,
    created_at: '2026-08-10T12:00:00Z',
    phase: 'routing',
    type: 'api_error',
    error_owner: 'platform',
    error_source: 'gateway',
    severity: 'error',
    status_code: 503,
    platform: 'openai',
    model: 'gpt-5.2',
    resolved: false,
    client_request_id: 'client-41',
    request_id: 'request-41',
    message: 'Service temporarily unavailable',
    user_email: '',
    account_name: '',
    group_name: '',
    error_body: '',
    is_business_limited: true,
    ...overrides,
  }
}

describe('OpsErrorDetailModal routing diagnostics', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listRequestErrorUpstreamErrors.mockResolvedValue({ items: [] })
  })

  it('renders the persisted sanitized routing diagnostic object', async () => {
    getRequestErrorDetail.mockResolvedValue(errorDetail({
      routing_diagnostics: {
        selection_decision: 'no_available_account',
        selection_layer: 'load_balance',
        candidate_pool: 3,
        filtered_candidates: { runtime_blocked: 3 },
        outbound_identity_source: 'global',
      },
    }))
    const wrapper = mount(OpsErrorDetailModal, {
      props: { show: true, errorId: 41, errorType: 'request' },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /></div>' },
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.ops.errorDetail.routingDiagnostics')
    expect(wrapper.text()).toContain('no_available_account')
    expect(wrapper.text()).toContain('runtime_blocked')
    expect(wrapper.text()).toContain('global')
  })
})
