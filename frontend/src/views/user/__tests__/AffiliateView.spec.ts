import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { UserAffiliateDetail } from '@/types'

import AffiliateView from '../AffiliateView.vue'

const { copyToClipboard, getAffiliateDetail, transferAffiliateQuota, refreshUser, showError, showSuccess } = vi.hoisted(() => ({
  copyToClipboard: vi.fn(),
  getAffiliateDetail: vi.fn(),
  transferAffiliateQuota: vi.fn(),
  refreshUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/user', () => ({
  default: {
    getAffiliateDetail,
    transferAffiliateQuota,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

enableAutoUnmount(afterEach)

function mountView() {
  return mount(AffiliateView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } } })
}

function detailWithInvitees(count: number): UserAffiliateDetail {
  return {
    user_id: 1, aff_code: 'INVITE', inviter_id: null, aff_count: count, aff_quota: 12.5,
    aff_frozen_quota: 0, aff_history_quota: 30, effective_rebate_rate_percent: 10,
    invitees: Array.from({ length: count }, (_, index) => ({
      user_id: index + 1, email: `invitee-${index + 1}@example.test`, username: `User ${index + 1}`,
      total_rebate: index + 1, created_at: '2026-10-01T12:00:00Z',
    })),
  }
}

describe('AffiliateView', () => {
  const affiliateCode = 'affiliate-code-that-is-long-enough-to-overflow-a-mobile-viewport'

  beforeEach(() => {
    vi.clearAllMocks()
    copyToClipboard.mockResolvedValue(true)
    transferAffiliateQuota.mockResolvedValue({ transferred_quota: 12.5, balance: 30 })
    refreshUser.mockResolvedValue({ balance: 30 })
    getAffiliateDetail.mockResolvedValue({
      user_id: 1,
      aff_code: affiliateCode,
      inviter_id: null,
      aff_count: 0,
      aff_quota: 0,
      aff_frozen_quota: 0,
      aff_history_quota: 0,
      effective_rebate_rate_percent: 10,
      invitees: [],
    })
  })

  it('stacks long values and copy controls on mobile while retaining desktop rows', async () => {
    const wrapper = mount(AffiliateView, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          Icon: true,
        },
      },
    })

    await flushPromises()

    const values = wrapper.findAll('code')
    expect(values).toHaveLength(2)
    for (const value of values) {
      expect(value.classes()).toEqual(expect.arrayContaining([
        'min-w-0',
        'break-all',
        'sm:flex-1',
        'sm:truncate',
      ]))
      expect(Array.from(value.element.parentElement?.classList ?? [])).toEqual(expect.arrayContaining([
        'flex-col',
        'items-stretch',
        'sm:flex-row',
        'sm:items-center',
      ]))
    }

    const copyButtons = wrapper.findAll('button').filter((button) =>
      ['affiliate.copyCode', 'affiliate.copyLink'].includes(button.text()),
    )
    expect(copyButtons).toHaveLength(2)
    for (const button of copyButtons) {
      expect(button.classes()).toEqual(expect.arrayContaining([
        'w-full',
        'sm:w-auto',
        'sm:shrink-0',
      ]))
    }

    await copyButtons[0].trigger('click')
    await copyButtons[1].trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenNthCalledWith(1, affiliateCode, 'affiliate.codeCopied')
    expect(copyToClipboard).toHaveBeenNthCalledWith(
      2,
      `${window.location.origin}/register?aff=${encodeURIComponent(affiliateCode)}`,
      'affiliate.linkCopied',
    )
  })

  // Accepted browser review: invited users show ten actual records per page;
  // paging never initiates a transfer, refetch, or other instance mutation.
  it('pages 32 real invited users in groups of ten with a two-row final page', async () => {
    getAffiliateDetail.mockResolvedValue(detailWithInvitees(32))
    const wrapper = mountView()
    await flushPromises()
    const rows = () => wrapper.findAll('tbody tr').map(row => row.findAll('td')[0].text())
    const pager = wrapper.get('nav')
    const previous = () => pager.findAll('button').find(button => button.text() === 'pagination.previous')!
    const next = () => pager.findAll('button').find(button => button.text() === 'pagination.next')!
    expect(rows()).toEqual([
      'invitee-1@example.test', 'invitee-2@example.test', 'invitee-3@example.test', 'invitee-4@example.test', 'invitee-5@example.test',
      'invitee-6@example.test', 'invitee-7@example.test', 'invitee-8@example.test', 'invitee-9@example.test', 'invitee-10@example.test',
    ])
    expect(previous().attributes('disabled')).toBeDefined()
    await next().trigger('click')
    expect(rows()).toEqual([
      'invitee-11@example.test', 'invitee-12@example.test', 'invitee-13@example.test', 'invitee-14@example.test', 'invitee-15@example.test',
      'invitee-16@example.test', 'invitee-17@example.test', 'invitee-18@example.test', 'invitee-19@example.test', 'invitee-20@example.test',
    ])
    await pager.findAll('button').find(button => button.text() === '4')!.trigger('click')
    expect(rows()).toEqual(['invitee-31@example.test', 'invitee-32@example.test'])
    expect(next().attributes('disabled')).toBeDefined()
    expect(pager.find('[aria-current="page"]').text()).toBe('4')
    await previous().trigger('click')
    expect(rows()[0]).toBe('invitee-21@example.test')
    expect(rows()).toHaveLength(10)
    expect(getAffiliateDetail).toHaveBeenCalledOnce()
    expect(transferAffiliateQuota).not.toHaveBeenCalled()
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it.each([10, 11])('keeps the next-page boundary correct for %i invited users', async count => {
    getAffiliateDetail.mockResolvedValue(detailWithInvitees(count))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(10)
    const next = wrapper.get('nav').findAll('button').find(button => button.text() === 'pagination.next')!
    if (count === 10) {
      expect(next.attributes('disabled')).toBeDefined()
    } else {
      expect(next.attributes('disabled')).toBeUndefined()
      await next.trigger('click')
      expect(wrapper.findAll('tbody tr')).toHaveLength(1)
      expect(wrapper.get('tbody tr').text()).toContain('invitee-11@example.test')
      expect(next.attributes('disabled')).toBeDefined()
    }
    expect(transferAffiliateQuota).not.toHaveBeenCalled()
  })

  it('retains all four guidance steps and disables navigation for no invited users', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findAll('.affiliate-guide-card')).toHaveLength(4)
    expect(wrapper.text()).toContain('affiliate.tips.line4')
    expect(wrapper.text()).toContain('affiliate.invitees.empty')
    const pager = wrapper.get('nav')
    expect(pager.find('[aria-current="page"]').text()).toBe('1')
    for (const text of ['pagination.previous', 'pagination.next']) {
      expect(pager.findAll('button').find(button => button.text() === text)!.attributes('disabled')).toBeDefined()
    }
    expect(transferAffiliateQuota).not.toHaveBeenCalled()
  })

  it('keeps transfer functional and clamps pagination when refreshed records shrink', async () => {
    getAffiliateDetail.mockResolvedValueOnce(detailWithInvitees(32)).mockResolvedValueOnce({ ...detailWithInvitees(2), aff_quota: 0 })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('nav').findAll('button').find(button => button.text() === '4')!.trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    await wrapper.get('.affiliate-transfer button').trigger('click')
    await flushPromises()
    expect(transferAffiliateQuota).toHaveBeenCalledOnce()
    expect(refreshUser).toHaveBeenCalledOnce()
    expect(showSuccess).toHaveBeenCalledWith('affiliate.transfer.success')
    expect(wrapper.get('nav [aria-current="page"]').text()).toBe('1')
    expect(wrapper.findAll('tbody tr').map(row => row.findAll('td')[0].text())).toEqual(['invitee-1@example.test', 'invitee-2@example.test'])
    expect(wrapper.get('.affiliate-transfer button').attributes('disabled')).toBeDefined()
  })

  it('preserves the current page and permits retry after transfer fails', async () => {
    getAffiliateDetail.mockResolvedValue(detailWithInvitees(32))
    transferAffiliateQuota.mockRejectedValue(new Error('Transfer unavailable'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('nav').findAll('button').find(button => button.text() === '4')!.trigger('click')
    await wrapper.get('.affiliate-transfer button').trigger('click')
    await flushPromises()
    expect(wrapper.get('nav [aria-current="page"]').text()).toBe('4')
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(getAffiliateDetail).toHaveBeenCalledOnce()
    expect(refreshUser).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledOnce()
    expect(wrapper.get('.affiliate-transfer button').attributes('disabled')).toBeUndefined()
  })
})
