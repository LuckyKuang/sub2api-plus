import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ZhipuStartPlanCell from '../ZhipuStartPlanCell.vue'
import type { Account } from '@/types'

const { queryQuota } = vi.hoisted(() => ({ queryQuota: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { cnProviders: { queryQuota } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const account = (owner = 'owner-a') => ({ id: 771, platform: 'zhipu', type: 'oauth', oauth_credential_owner: owner,
  credentials: { account_mode: 'coding', plan_kind: 'start-plan', oauth_provider: 'zai' }, proxy_id: 91 } as Account)
const observation = (state: string, modelsKnown = true, models: string[] = []) => ({ success: true, persisted: true,
  start_plan: { state, models_known: modelsKnown, models, observed_at: Math.floor(Date.now() / 1000), server_time: 2000000000 } })

describe('ZhipuStartPlanCell', () => {
  const wrappers: ReturnType<typeof mount>[] = []
  beforeEach(() => { queryQuota.mockReset(); vi.useFakeTimers() })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.useRealTimers() })
  const render = (value = account()) => {
    const wrapper = mount(ZhipuStartPlanCell, { props: { account: value } }); wrappers.push(wrapper); return wrapper
  }
  it.each(['available', 'unavailable', 'pending', 'unknown'])('shows the independent %s state', async state => {
    queryQuota.mockResolvedValue(observation(state))
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[data-test="start-plan-state"]').text()).toBe(`admin.accounts.cnProviders.startPlan.${state}`)
    expect(queryQuota).toHaveBeenCalledWith(771)
  })
  it('distinguishes unknown model details from an explicit empty model set', async () => {
    queryQuota.mockResolvedValue(observation('available', false))
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[data-test="start-plan-models"]').text()).toContain('modelsUnknown')
    queryQuota.mockResolvedValue(observation('available', true))
    await wrapper.get('[data-test="start-plan-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="start-plan-models"]').text()).toContain('modelsEmpty')
    queryQuota.mockResolvedValue(observation('available', true, ['GLM-5.3']))
    await wrapper.get('[data-test="start-plan-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="start-plan-models"]').text()).toBe('GLM-5.3')
  })
  it('rejects a late same-ID reauthorization result and permits the current owner', async () => {
    let resolveOld!: (value: unknown) => void
    queryQuota.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce(observation('pending'))
    const wrapper = render(); await flushPromises()
    await wrapper.setProps({ account: account('owner-b') }); await flushPromises()
    expect(queryQuota).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="start-plan-state"]').text()).toContain('.pending')
    resolveOld(observation('available', true, ['old-owner-model'])); await flushPromises()
    expect(wrapper.text()).not.toContain('old-owner-model')
    expect(wrapper.get('[data-test="start-plan-state"]').text()).toContain('.pending')
  })
  it('keeps a proven negative state after query failure and never promotes pending on time alone', async () => {
    const result = observation('pending')
    Object.assign(result.start_plan, { effective_at: 2000000001 })
    queryQuota.mockResolvedValue(result)
    const wrapper = render(); await flushPromises()
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.get('[data-test="start-plan-state"]').text()).toContain('.pending')
    queryQuota.mockRejectedValue(new Error('private-token-must-not-render'))
    await wrapper.get('[data-test="start-plan-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-test="start-plan-state"]').text()).toContain('.pending')
    expect(wrapper.get('[data-test="start-plan-error"]').text()).toContain('queryFailed')
    expect(wrapper.text()).not.toContain('private-token')
  })
  it('shows reauthorization for business login failure and ignores uncommitted observations', async () => {
    const rejected = observation('unavailable'); Object.assign(rejected.start_plan, { reason: 'auth_failed' })
    queryQuota.mockResolvedValue(rejected)
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[data-test="start-plan-reauthorize"]').text()).toContain('reauthorize')
    queryQuota.mockResolvedValue({ ...observation('available', true, ['uncommitted']), persisted: false })
    await wrapper.get('[data-test="start-plan-probe"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('uncommitted')
    expect(wrapper.get('[data-test="start-plan-state"]').text()).toContain('.unavailable')
  })
  it.each(['off-peak', 'individual-coding-plan', 'team-coding-plan'])('does not probe or render %s as Start', async plan => {
    const value = account(); value.credentials!.plan_kind = plan
    const wrapper = render(value); await flushPromises()
    expect(wrapper.find('[data-test="zhipu-start-plan"]').exists()).toBe(false)
    expect(queryQuota).not.toHaveBeenCalled()
  })
})
