import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import NativeAccessFields from '../NativeAccessFields.vue'
import Select from '@/components/common/Select.vue'
const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('@/api/client', () => ({ apiClient: api }))
const models = [{ name: 'gpt-6.1-sol', platform: 'openai', endpoints: ['/v1/responses'] }, { name: 'claude-opus-5-5', platform: 'anthropic', endpoints: ['/v1/messages'] }]
beforeEach(() => api.get.mockResolvedValue({ data: { endpoints: ['/v1/messages', '/v1/responses'], models } }))
function mountFields() {
 return mount(defineComponent({ components: { NativeAccessFields }, setup: () => ({ client: ref('codex'), endpoint: ref('/v1/responses') }), template: '<NativeAccessFields v-model:client="client" v-model:endpoint="endpoint" :group-id="1" base-url="https://example.test" api-key="test-key" />' }), { global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] } })
}
describe('Composite native access examples', () => {
 it('only advertises models native to the chosen endpoint and generates the correct client base URL', async () => {
  const page = mountFields(); await flushPromises()
  expect(page.get('ul').text()).toContain('gpt-6.1-sol'); expect(page.get('ul').text()).not.toContain('claude-opus-5-5')
  expect(page.get('pre').text()).toContain('wire_api = "responses"'); expect(page.get('dl').text()).toContain('https://example.test/v1')
  const fields = page.getComponent(NativeAccessFields)
  fields.findAllComponents(Select)[0]!.vm.$emit('update:modelValue', 'claude'); await flushPromises()
  expect(page.get('ul').text()).toContain('claude-opus-5-5'); expect(page.get('ul').text()).not.toContain('gpt-6.1-sol')
  expect(page.get('pre').text()).toContain('"ANTHROPIC_BASE_URL": "https://example.test"'); expect(page.get('dl').text()).toContain('https://example.test/v1/messages')
  expect(fields.vm.validate()).toBe(true); page.unmount()
 })
})
