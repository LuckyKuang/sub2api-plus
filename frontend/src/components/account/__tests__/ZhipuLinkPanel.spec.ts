import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ZhipuLinkPanel from '../ZhipuLinkPanel.vue'
import { createZhipuAccountFromLink, exchangeZhipuLink, getZhipuOAuthCapabilities, pollZhipuLink, startZhipuLink } from '@/api/admin/zhipu'

vi.mock('vue-i18n', async (original) => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

vi.mock('@/api/admin/zhipu', () => ({
  getZhipuOAuthCapabilities: vi.fn(),
  startZhipuLink: vi.fn(),
  pollZhipuLink: vi.fn(),
  exchangeZhipuLink: vi.fn(),
  createZhipuAccountFromLink: vi.fn()
}))

const capabilities = {
  enabled: true,
  providers: ['bigmodel', 'zai'] as const,
  plan_kinds: ['individual-coding-plan', 'team-coding-plan', 'start-plan', 'off-peak'] as const,
  supported_plan_kinds: ['individual-coding-plan', 'team-coding-plan', 'start-plan', 'off-peak'] as const,
  handshake_url: 'https://zcode.z.ai/api/v1'
}

const session = {
  session_id: 's-1',
  provider: 'bigmodel' as const,
  authorize_url: 'https://bigmodel.cn/login?appId=zcode&state=st-1',
  expires_at: '2026-01-01T00:10:00Z',
  interval_seconds: 2
}

const ready = {
  provider: 'bigmodel' as const,
  access_token: 'bm-token',
  zcode_jwt_token: 'zcode-jwt',
  user: { user_id: 'u-1', user_name: 'Ada' }
}

async function mountPanel() {
  const wrapper = mount(ZhipuLinkPanel, { props: { proxyId: 5 } })
  await flushPromises()
  return wrapper
}

describe('ZhipuLinkPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.mocked(getZhipuOAuthCapabilities).mockResolvedValue({ ...capabilities })
    vi.mocked(startZhipuLink).mockResolvedValue(session)
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: true })
  })

  it('renders the estates and plan kinds from the server capabilities', async () => {
    const wrapper = await mountPanel()
    const providers = wrapper.get('[data-testid="zhipu-link-provider"]').findAll('option')
    expect(providers.map(option => option.element.value)).toEqual(['bigmodel', 'zai'])
    const plans = wrapper.get('[data-testid="zhipu-link-plan"]').findAll('option')
    expect(plans.map(option => option.element.value)).toEqual([
      'individual-coding-plan', 'team-coding-plan', 'start-plan', 'off-peak'
    ])
    expect(wrapper.text()).toContain('admin.accounts.oauth.zhipu.plans.off-peak')
    wrapper.unmount()
  })

  it('fails closed when the deployment has no link configured', async () => {
    vi.mocked(getZhipuOAuthCapabilities).mockResolvedValue({ ...capabilities, enabled: false })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('admin.accounts.oauth.zhipu.unavailable')
    expect(wrapper.find('[data-testid="zhipu-link-start"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('starts a link, exposes the authorization URL and polls until ready', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()

    expect(startZhipuLink).toHaveBeenCalledWith({ provider: 'bigmodel', proxy_id: 5 })
    expect(wrapper.get('[data-testid="zhipu-link-open"]').attributes('href')).toBe(session.authorize_url)
    expect(wrapper.find('[data-testid="zhipu-link-polling"]').exists()).toBe(true)

    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(wrapper.find('[data-testid="zhipu-link-ready"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="zhipu-link-create"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('creates the account with the session token material and reports success', async () => {
    vi.mocked(createZhipuAccountFromLink).mockResolvedValue({})
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()

    await wrapper.get('[data-testid="zhipu-link-create"]').trigger('click')
    await flushPromises()

    expect(createZhipuAccountFromLink).toHaveBeenCalledWith({
      plan_kind: 'individual-coding-plan',
      concurrency: 3,
      priority: 0,
      proxy_id: 5,
      session_id: 's-1',
      provider: 'bigmodel',
      access_token: 'bm-token',
      zcode_jwt_token: 'zcode-jwt'
    })
    expect(wrapper.emitted('created')).toHaveLength(1)
    wrapper.unmount()
  })

  it('asks for the team scope only for a team plan', async () => {
    const wrapper = await mountPanel()
    expect(wrapper.find('[data-testid="zhipu-link-team-org"]').exists()).toBe(false)
    await wrapper.get('[data-testid="zhipu-link-plan"]').setValue('team-coding-plan')
    expect(wrapper.find('[data-testid="zhipu-link-team-org"]').exists()).toBe(true)

    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    vi.mocked(pollZhipuLink).mockResolvedValue({ pending: false, ready })
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    await wrapper.get('[data-testid="zhipu-link-team-org"]').setValue('org-t')
    await wrapper.get('[data-testid="zhipu-link-team-project"]').setValue('proj-t')
    vi.mocked(createZhipuAccountFromLink).mockResolvedValue({})
    await wrapper.get('[data-testid="zhipu-link-create"]').trigger('click')
    await flushPromises()

    expect(createZhipuAccountFromLink).toHaveBeenCalledWith(expect.objectContaining({
      plan_kind: 'team-coding-plan',
      team_organization: 'org-t',
      team_project: 'proj-t'
    }))
    wrapper.unmount()
  })

  it('falls back to a pasted callback URL when the browser cannot return', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text().includes('showFallback'))!.trigger('click')
    await wrapper.get('[data-testid="zhipu-link-callback"]').setValue('https://zcode.z.ai/app/oauth/login?code=c&state=s')
    vi.mocked(exchangeZhipuLink).mockResolvedValue({ pending: false, ready })
    await wrapper.findAll('button').find(button => button.text().includes('fallbackSubmit'))!.trigger('click')
    await flushPromises()

    expect(exchangeZhipuLink).toHaveBeenCalledWith('s-1', 'https://zcode.z.ai/app/oauth/login?code=c&state=s', 5)
    expect(wrapper.find('[data-testid="zhipu-link-ready"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('surfaces a server rejection to the operator', async () => {
    vi.mocked(startZhipuLink).mockRejectedValue({ response: { data: { message: 'plan "off-peak" cannot be linked yet' } } })
    const wrapper = await mountPanel()
    await wrapper.get('[data-testid="zhipu-link-start"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="zhipu-link-error"]').text()).toBe('plan "off-peak" cannot be linked yet')
    wrapper.unmount()
  })
})
