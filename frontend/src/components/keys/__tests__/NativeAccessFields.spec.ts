import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import NativeAccessFields from '../NativeAccessFields.vue'
import Select from '@/components/common/Select.vue'
const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('@/api/client', () => ({ apiClient: api }))
const conversion = ['/v1/responses', '/v1/messages']
const models = [{ name: 'gpt-6.1-sol', platform: 'openai', endpoints: ['/v1/responses'], conversion_endpoints: conversion }, { name: 'claude-opus-5-5', platform: 'anthropic', endpoints: ['/v1/messages'], conversion_endpoints: conversion }]
beforeEach(() => api.get.mockResolvedValue({ data: { endpoints: conversion, conversion_endpoints: conversion, models } }))
function mountFields(client = '', endpoint = '', enabled = false) {
 return mount(defineComponent({ components: { NativeAccessFields }, setup: () => ({ enabled: ref(enabled), client: ref(client), endpoint: ref(endpoint) }), template: '<NativeAccessFields v-model:conversion-enabled="enabled" v-model:client="client" v-model:endpoint="endpoint" :group-id="1" base-url="https://example.test" api-key="test-key" />' }), { global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] } })
}
// User requirement: native access is the default; choosing a unified interface
// is an explicit, reversible server policy, not an automatically filled hint.
describe('optional Composite endpoint configuration', () => {
 it('loads the aggregate key catalog without exposing conversion controls', async () => {
  api.get.mockClear()
  api.get.mockResolvedValueOnce({ data: { endpoints: ['/v1/responses'], conversion_endpoints: [], models: [{ name: 'gpt-6.1-sol', platform: 'openai', endpoints: ['/v1/responses'], conversion_endpoints: [], group_ids: [7, 12] }] } })
  const page = mount(NativeAccessFields, { props: { aggregateKeyId: 44, groupId: null, client: '', endpoint: '', editable: false }, global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] } })
  await flushPromises()
  expect(api.get).toHaveBeenCalledWith('/keys/44/native-access', expect.anything())
  expect(page.find('input[type="checkbox"]').exists()).toBe(false)
  expect(page.findAllComponents(Select)).toHaveLength(0)
  expect(page.get('details').text()).toContain('gpt-6.1-sol')
  expect(page.get('[data-testid="aggregate-native-groups"]').text()).toContain('X-Composite-Group-ID: 7')
  expect(page.get('[data-testid="aggregate-native-groups"]').text()).toContain('X-Composite-Group-ID: 12')
  page.unmount()
 })

 it('leaves new and unconfigured edited keys native without auto-filling defaults', async () => {
  const page = mountFields(); await flushPromises()
  expect(page.vm.client).toBe(''); expect(page.vm.endpoint).toBe('')
  expect(page.findAllComponents(Select)).toHaveLength(0)
  expect(page.get('[data-testid="composite-native-mode"]').exists()).toBe(true)
  expect(page.get('details').text()).toContain('https://example.test/v1/messages')
  expect(page.getComponent(NativeAccessFields).vm.validate()).toBe(true)
  page.unmount()
 })
 it('converts all models through the explicitly selected endpoint and can clear it', async () => {
  const page = mountFields(); await flushPromises()
  await page.get('input[type=checkbox]').setValue(true); await flushPromises()
  expect(page.vm.endpoint).toBe('/v1/responses')
  expect(page.get('ul').text()).toContain('gpt-6.1-sol'); expect(page.get('ul').text()).toContain('claude-opus-5-5')
  const fields = page.getComponent(NativeAccessFields)
  fields.findAllComponents(Select)[0]!.vm.$emit('update:modelValue', 'claude'); await flushPromises()
  expect(page.vm.endpoint).toBe('/v1/messages')
  expect(page.get('pre').text()).toContain('"ANTHROPIC_BASE_URL": "https://example.test"')
  expect(page.get('dl').text()).toContain('https://example.test/v1/messages')
  expect(fields.vm.validate()).toBe(true)
  await page.get('input[type=checkbox]').setValue(false); await flushPromises()
  expect(page.vm.client).toBe(''); expect(page.vm.endpoint).toBe(''); expect(fields.vm.validate()).toBe(true)
  page.unmount()
 })
 it('keeps previously saved display preferences native until explicitly enabled', async () => {
  const page = mountFields('codex', '/v1/responses'); await flushPromises()
  expect(page.get('input[type=checkbox]').element).toHaveProperty('checked', false)
  expect(page.findAllComponents(Select)).toHaveLength(0)
  expect(page.getComponent(NativeAccessFields).vm.validate()).toBe(true)
  page.unmount()
 })
 it('restores an edited configuration without changing it after loading capabilities', async () => {
  const page = mountFields('codex', '/v1/responses', true); await flushPromises()
  expect(page.get('input[type=checkbox]').element).toHaveProperty('checked', true)
  expect(page.get('pre').text()).toContain('wire_api = "responses"')
  expect(page.vm.client).toBe('codex'); expect(page.vm.endpoint).toBe('/v1/responses'); page.unmount()
 })
 it('allows native creation without models or metadata but rejects an enabled unsupported conversion', async () => {
  api.get.mockResolvedValue({ data: { endpoints: [], conversion_endpoints: [], models: [] } })
  const page = mountFields(); await flushPromises()
  const fields = page.getComponent(NativeAccessFields)
  expect(fields.vm.validate()).toBe(true)
  await page.get('input[type=checkbox]').setValue(true); await flushPromises()
  expect(fields.vm.validate()).toBe(false)
  await page.get('input[type=checkbox]').setValue(false); expect(fields.vm.validate()).toBe(true); page.unmount()
 })
})
