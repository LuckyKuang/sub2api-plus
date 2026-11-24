import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import { apiClient } from '@/api'
import RedeemView from '../RedeemView.vue'

const { refreshUser, fetchActiveSubscriptions, showError, showWarning, showSuccess } = vi.hoisted(() => ({
  refreshUser: vi.fn(),
  fetchActiveSubscriptions: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { balance: 0, concurrency: 2 }, refreshUser }),
}))
vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({ fetchActiveSubscriptions }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  fallbackWarn: false,
  missingWarn: false,
  messages: { zh, en },
})

const apiAdapter = (handler: (config: InternalAxiosRequestConfig) => unknown): AxiosAdapter =>
  (async (config: InternalAxiosRequestConfig) => handler(config)) as AxiosAdapter

function success(config: InternalAxiosRequestConfig, data: unknown) {
  return { status: 200, data: { code: 0, data }, headers: {}, config, statusText: 'OK' }
}

type RedeemFailure = { status: number; reason?: string } | 'timeout'

function installAdapter(failure: RedeemFailure) {
  apiClient.defaults.adapter = apiAdapter((config) => {
    const url = String(config.url || '')
    if (url === '/redeem' && config.method === 'post') {
      if (failure === 'timeout') {
        return Promise.reject({
          code: 'ECONNABORTED',
          message: 'timeout of 30000ms exceeded',
          config,
        })
      }
      const data = {
        code: failure.status,
        reason: failure.reason,
        message: 'backend redemption failure',
      }
      return Promise.reject({
        code: 'ERR_BAD_REQUEST',
        message: 'Request failed with status code ' + failure.status,
        config,
        response: { status: failure.status, data, headers: {}, config, statusText: '' },
      })
    }
    if (url === '/redeem/history') {
      return success(config, { items: [], total: 0, page: 1, page_size: 20, pages: 0 })
    }
    if (url === '/settings/public') {
      return success(config, {})
    }
    return Promise.reject(new Error(`unexpected request ${config.method} ${url}`))
  })
}

async function submitCode(code = ' REDEEM-CODE ') {
  const wrapper = mount(RedeemView, {
    global: {
      plugins: [i18n],
      stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true },
    },
  })
  await flushPromises()
  await wrapper.get('input#code').setValue(code)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  return wrapper
}

const genericFailure = '兑换失败，请检查兑换码后重试'

describe('RedeemView redemption failures through the API client', () => {
  let originalAdapter: typeof apiClient.defaults.adapter

  beforeEach(() => {
    vi.clearAllMocks()
    originalAdapter = apiClient.defaults.adapter
    refreshUser.mockResolvedValue({ balance: 10, concurrency: 2 })
    fetchActiveSubscriptions.mockResolvedValue([])
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
    vi.restoreAllMocks()
  })

  it.each([
    { status: 404, reason: 'REDEEM_CODE_NOT_FOUND', message: '兑换码不存在' },
    { status: 409, reason: 'REDEEM_CODE_USED', message: '兑换码已被使用' },
    { status: 409, reason: 'REDEEM_CODE_EXPIRED', message: '兑换码已过期' },
    { status: 409, reason: 'REDEEM_CODE_LOCKED', message: '兑换码正在处理中，请稍后重试' },
    { status: 429, reason: 'REDEEM_RATE_LIMITED', message: '尝试次数过多，请稍后再试' },
  ])('shows the localized $reason reason', async ({ status, reason, message }) => {
    installAdapter({ status, reason })
    const wrapper = await submitCode()

    expect(wrapper.text()).toContain(message)
    expect(wrapper.text()).not.toContain(genericFailure)
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showWarning).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('兑换失败')
    expect((wrapper.get('input#code').element as HTMLInputElement).value).toBe(' REDEEM-CODE ')
    wrapper.unmount()
  })

  it('distinguishes processing and expired responses that share HTTP 409', async () => {
    installAdapter({ status: 409, reason: 'REDEEM_CODE_LOCKED' })
    const processing = await submitCode()
    const processingText = processing.text()
    expect(processingText).toContain('兑换码正在处理中，请稍后重试')
    expect(processingText).not.toContain('兑换码已过期')
    processing.unmount()

    installAdapter({ status: 409, reason: 'REDEEM_CODE_EXPIRED' })
    const expired = await submitCode()
    const expiredText = expired.text()
    expect(expiredText).toContain('兑换码已过期')
    expect(expiredText).not.toContain('兑换码正在处理中，请稍后重试')
    expired.unmount()
  })

  it.each([
    ['network interruption', { code: 'ERR_NETWORK', message: 'Network Error' }],
    ['timeout', { code: 'ECONNABORTED', message: 'timeout of 30000ms exceeded' }],
  ])('warns that the result is unknown after a %s', async (_name, rejection) => {
    apiClient.defaults.adapter = apiAdapter((config) => {
      const url = String(config.url || '')
      if (url === '/redeem' && config.method === 'post') {
        return Promise.reject({ ...rejection, config })
      }
      if (url === '/redeem/history') {
        return success(config, { items: [], total: 0, page: 1, page_size: 20, pages: 0 })
      }
      if (url === '/settings/public') return success(config, {})
      return Promise.reject(new Error(`unexpected request ${config.method} ${url}`))
    })
    const wrapper = await submitCode()

    expect(wrapper.text()).toContain('暂时无法确认兑换结果，请先查看兑换记录')
    expect(wrapper.text()).not.toContain('Network error')
    expect(wrapper.text()).not.toContain(genericFailure)
    expect(wrapper.text()).not.toContain('兑换成功')
    expect(showSuccess).not.toHaveBeenCalled()
    expect((wrapper.get('input#code').element as HTMLInputElement).value).toBe(' REDEEM-CODE ')
    wrapper.unmount()
  })
})
