import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import SetupWizardView from '../SetupWizardView.vue'
import { install, testDatabase, testRedis } from '@/api/setup'

vi.mock('@/api/client', () => ({ buildGatewayUrl: (path: string) => path }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/setup', () => ({
  testDatabase: vi.fn().mockResolvedValue({}),
  testRedis: vi.fn().mockResolvedValue({}),
  install: vi.fn().mockRejectedValue(new Error('Installation not started in this test'))
}))

enableAutoUnmount(afterEach)
afterEach(() => vi.clearAllMocks())

async function openAdminStep() {
  const wrapper = mount(SetupWizardView)
  const button = (key: string) => {
    const found = wrapper.findAll('button').find((item) => item.text().trim() === key)
    expect(found, key).toBeDefined()
    return found!
  }
  await button('setup.status.testConnection').trigger('click')
  await flushPromises()
  expect(testDatabase).toHaveBeenCalledOnce()
  await button('common.next').trigger('click')
  await button('setup.status.testConnection').trigger('click')
  await flushPromises()
  expect(testRedis).toHaveBeenCalledOnce()
  await button('common.next').trigger('click')
  return { wrapper, button }
}

describe('setup administrator credentials', () => {
  it.each([
    ['seven ASCII bytes', 'a'.repeat(7), false],
    ['eight ASCII bytes', 'a'.repeat(8), true],
    ['72 ASCII bytes', 'a'.repeat(72), true],
    ['73 ASCII bytes', 'a'.repeat(73), false],
    ['six Chinese bytes', '中'.repeat(2), false],
    ['nine Chinese bytes', '中'.repeat(3), true],
    ['72 Chinese bytes', '中'.repeat(24), true],
    ['75 Chinese bytes', '中'.repeat(25), false],
    ['eight emoji bytes', '😀'.repeat(2), true],
    ['72 emoji bytes', '😀'.repeat(18), true],
    ['76 emoji bytes', '😀'.repeat(19), false]
  ])('uses the backend byte limits for %s', async (_name, password, accepted) => {
    const { wrapper, button } = await openAdminStep()
    expect((wrapper.get('#setup-admin-email').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('#setup-admin-password').element as HTMLInputElement).value).toBe('')
    await wrapper.get('#setup-admin-email').setValue('owner@example.com')
    await wrapper.get('#setup-admin-password').setValue(password)
    await wrapper.get('#setup-admin-confirm-password').setValue(password)

    expect((button('common.next').element as HTMLButtonElement).disabled).toBe(!accepted)
    expect(wrapper.find('#setup-password-error').exists()).toBe(!accepted)
    if (accepted) {
      await button('common.next').trigger('click')
      await button('setup.status.completeInstallation').trigger('click')
      await flushPromises()
      expect(install).toHaveBeenCalledWith(expect.objectContaining({
        admin: { email: 'owner@example.com', password }
      }))
    } else {
      await button('common.next').trigger('click')
      expect(wrapper.find('#setup-admin-email').exists()).toBe(true)
      expect(install).not.toHaveBeenCalled()
    }
  })

  it('requires a matching confirmation and a nonblank email', async () => {
    const { wrapper, button } = await openAdminStep()
    await wrapper.get('#setup-admin-email').setValue('owner@example.com')
    await wrapper.get('#setup-admin-password').setValue('valid-password')
    await wrapper.get('#setup-admin-confirm-password').setValue('different-password')
    expect((button('common.next').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.text()).toContain('setup.admin.passwordMismatch')
    await wrapper.get('#setup-admin-confirm-password').setValue('valid-password')
    expect((button('common.next').element as HTMLButtonElement).disabled).toBe(false)
    await wrapper.get('#setup-admin-email').setValue('   ')
    expect((button('common.next').element as HTMLButtonElement).disabled).toBe(true)
    expect(install).not.toHaveBeenCalled()
  })
})
