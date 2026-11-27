import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import QualityInspectionView from '../QualityInspectionView.vue'
import * as api from '@/api/admin/qualityInspection'
import type { QualityPlan, QualityRun } from '@/api/admin/qualityInspection'

vi.mock('@/api/admin/qualityInspection')
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
const dialog = { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>' }
const question = { id: 'numeric', title: 'Calculation', kind: 'number' as const, prompt: '17 * 19', instructions: '', expected: '323', choices: [], unit: '', rounds: 1, enabled: true }
const plan = (id: number): QualityPlan => ({ id, name: `Plan ${id}`, group_id: 1, group_name: 'Group one', platform: 'openai', model: 'gpt-5.1-codex', endpoint: 'any', effort: 'medium', enabled: true, schedule_enabled: false, interval_minutes: 60, pause_start: '22:00', pause_end: '09:00', timezone: 'Asia/Shanghai', max_accounts: 1, max_saved_rounds: 20, quarantine_enabled: false, quarantine_cooldown_minutes: 60, quarantine_fail_rounds: 2, revision: 1, questions: [{ ...question }], estimated_requests: 1, updated_at: '' })
const run = (id: number): QualityRun => ({ id, plan_id: 1, trigger: 'manual', status: 'completed', model: 'gpt-5.1-codex', effort: 'medium', request_count: 1, message: '', stats: { verdict: 'pass' }, resolved_platform: 'openai', resolved_upstream_model: '', resolved_endpoint: '', resolved_source: '', started_at: null, finished_at: null })
let wrapper: VueWrapper
function render(locale = 'en') {
  wrapper = mount(QualityInspectionView, { attachTo: document.body, global: { plugins: [createPinia(), createI18n({ legacy: false, locale, messages: { en, zh }, warnHtmlMessage: false })], stubs: { BaseDialog: dialog, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
  return wrapper
}
function button(text: string) { return wrapper.findAll('button').find(b => b.text() === text)! }
async function action(key: string) {
  wrapper.findComponent({ name: 'ActionMenu' }).vm.$emit('select', key)
  await flushPromises()
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(api.listQualityPlans).mockResolvedValue({ plans: [plan(1)], questions: [question], settings: { enabled: true, quarantine_enabled: false, quarantine_cooldown_minutes: 60, max_requests_per_round: 30, stability_rounds: 5 } })
  vi.mocked(api.listQualityGroupOptions).mockResolvedValue([{ id: 1, name: 'Group one', platform: 'openai' }])
  vi.mocked(api.listQualityRuns).mockResolvedValue([])
  vi.mocked(api.getQualityRun).mockResolvedValue({ run: run(1), results: [] })
})
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

// CONTRIBUTING: shared console controls, localized labels, numbered pagination;
// MODEL_QUALITY_INSPECTION: billable manual runs require confirmation.
describe('quality inspection admin workflow', () => {
  it('localizes controls and explains the implemented feature limits', async () => {
    render(); await flushPromises()
    expect(wrapper.text()).toContain('Model quality inspection')
    expect(wrapper.text()).toContain('does not change account scheduling')
    expect(wrapper.text()).not.toContain('新建计划')
    expect(wrapper.find('select').exists()).toBe(false)
  })
  it('creates with the loaded question bank and preserves explicit plan fields', async () => {
    render(); await flushPromises(); await button('New plan').trigger('click')
    const dialog = wrapper.find('[role="dialog"]')
    await dialog.find('input[name="plan-name"]').setValue('Weekday inspection')
    await dialog.find('input[name="plan-model"]').setValue('gpt-5.1-codex')
    await button('Save').trigger('click'); await flushPromises()
    expect(api.createQualityPlan).toHaveBeenCalledWith(expect.objectContaining({ name: 'Weekday inspection', model: 'gpt-5.1-codex', group_id: 1, questions: [question], schedule_enabled: false }))
    expect(api.listQualityPlans).toHaveBeenCalledTimes(2) // initial + successful save, no duplicate bank load
  })
  it('does not issue a billable run when confirmation is cancelled', async () => {
    render(); await flushPromises(); await action('run')
    expect(wrapper.find('[role="dialog"]').text()).toContain('billable requests')
    expect(api.runQualityPlan).not.toHaveBeenCalled()
    await button('Cancel').trigger('click'); await flushPromises()
    expect(api.runQualityPlan).not.toHaveBeenCalled()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
  it('runs only after confirmation and reports its history', async () => {
    render(); await flushPromises(); await action('run')
    await button('Confirm').trigger('click'); await flushPromises()
    expect(api.runQualityPlan).toHaveBeenCalledExactlyOnceWith(1, true)
    expect(api.listQualityRuns).toHaveBeenCalledWith(1)
  })
  it('requires explicit confirmation before deleting a plan', async () => {
    render('zh'); await flushPromises(); await action('delete')
    expect(api.deleteQualityPlan).not.toHaveBeenCalled()
    expect(wrapper.find('[role="dialog"]').text()).toContain('Plan 1')
    await button('确认').trigger('click'); await flushPromises()
    expect(api.deleteQualityPlan).toHaveBeenCalledExactlyOnceWith(1)
  })
  it('paginates plans with numbered shared pagination', async () => {
    const data = await vi.mocked(api.listQualityPlans).getMockImplementation()!()
    vi.mocked(api.listQualityPlans).mockResolvedValue({ ...data, plans: Array.from({ length: 12 }, (_, i) => plan(i + 1)) })
    render(); await flushPromises()
    expect(wrapper.findAll('table tbody tr')).toHaveLength(10)
    wrapper.findComponent({ name: 'Pagination' }).vm.$emit('update:page', 2); await flushPromises()
    expect(wrapper.findAll('table tbody tr')).toHaveLength(2)
    expect(wrapper.find('table tbody').text()).toContain('Plan 11')
  })
  it('ignores an older round response after a new round is selected', async () => {
    vi.mocked(api.listQualityRuns).mockResolvedValue([run(1), run(2)])
    let resolveOld!: (value: Awaited<ReturnType<typeof api.getQualityRun>>) => void
    vi.mocked(api.getQualityRun).mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    render(); await flushPromises(); await button('History').trigger('click'); await flushPromises()
    const roundButtons = wrapper.findAll('button').filter(b => b.text().includes('Manual'))
    await roundButtons[1].trigger('click'); await flushPromises()
    resolveOld({ run: run(1), results: [{ id: 1, question_id: 'old-response', answer: 'stale' } as api.QualityResult] }); await flushPromises()
    expect(wrapper.text()).not.toContain('stale')
  })
})
