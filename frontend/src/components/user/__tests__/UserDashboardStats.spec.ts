import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import type { UserDashboardStats } from '@/api/usage'
import UserDashboardStatsComponent from '../dashboard/UserDashboardStats.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const stats: UserDashboardStats = {
  total_api_keys: 1,
  active_api_keys: 1,
  total_requests: 1,
  total_input_tokens: 100,
  total_output_tokens: 900,
  total_cache_creation_tokens: 50,
  total_cache_read_tokens: 50,
  total_tokens: 1100,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 1,
  today_input_tokens: 100,
  today_output_tokens: 900,
  today_cache_creation_tokens: 50,
  today_cache_read_tokens: 50,
  today_tokens: 1100,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 1,
  rpm: 1,
  tpm: 1100,
}

describe('UserDashboardStats', () => {
  it('shows token shares without prompt cache hit rate', () => {
    const wrapper = mount(UserDashboardStatsComponent, {
      props: {
        stats,
        balance: 0,
        isSimple: true,
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    const text = wrapper.text()
    expect(text).not.toContain('Cache Hit Rate')
    expect(text).toContain('dashboard.input: 100')
    expect(text).toContain('dashboard.output: 900')
    expect(text).toContain('dashboard.cache: 100')
    expect(text).toContain('(9.1%)')
    expect(text).toContain('(81.8%)')
  })
})
