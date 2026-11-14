import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ImageTaskSettings from '../ImageTaskSettings.vue'

const { getSettings, updateSettings, testStorage, showSuccess, showError } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  testStorage: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/imageTasks', () => ({
  getImageTaskSettings: getSettings,
  updateImageTaskSettings: updateSettings,
  testImageTaskStorage: testStorage,
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('@/composables/useImageTaskModes', () => ({
  useImageTaskModes: () => ({ refreshImageTaskModes: vi.fn() }),
}))

// Step-up is a separate contract with its own coverage; here it only has to run
// the guarded call.
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (fn: () => Promise<unknown>) => fn() }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const toggleStub = {
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<button class="toggle" :data-checked="String(modelValue)" @click="$emit(\'update:modelValue\', !modelValue)" />',
}

function mountPanel() {
  return mount(ImageTaskSettings, {
    global: {
      stubs: { Toggle: toggleStub, TotpStepUpDialog: true },
    },
  })
}

describe('ImageTaskSettings', () => {
  beforeEach(() => {
    getSettings.mockReset()
    updateSettings.mockReset()
    testStorage.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    getSettings.mockResolvedValue({
      ordinary_enabled: true,
      batch_enabled: false,
      storage_secret_configured: true,
      storage: {
        reuse_backup_s3: true,
        bucket: '',
        prefix: 'images/',
        append_date_path: false,
        public_base_url: '',
        presign_expiry_hours: 24,
        endpoint: '',
        region: 'auto',
        access_key_id: '',
        force_path_style: false,
      },
    })
    const persisted = { ordinary_enabled: true, batch_enabled: false }
    updateSettings.mockImplementation(async (payload: { ordinary_enabled?: boolean; batch_enabled?: boolean }) => {
      if (payload.ordinary_enabled !== undefined) persisted.ordinary_enabled = payload.ordinary_enabled
      if (payload.batch_enabled !== undefined) persisted.batch_enabled = payload.batch_enabled
      return {
        ...persisted,
        storage_secret_configured: true,
        storage: { reuse_backup_s3: true, prefix: 'images/' },
      }
    })
  })

  it('persists each switch immediately and still saves the storage target', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.get('[data-testid="image-task-ordinary-toggle"]').attributes('data-checked')).toBe('true')
    expect(wrapper.get('[data-testid="image-task-batch-toggle"]').attributes('data-checked')).toBe('false')

    await wrapper.get('[data-testid="image-task-ordinary-toggle"]').trigger('click')
    await wrapper.get('[data-testid="image-task-batch-toggle"]').trigger('click')
    await flushPromises()

    // Each switch persists on its own, before the storage save button is used.
    expect(updateSettings).toHaveBeenNthCalledWith(1, { ordinary_enabled: false })
    expect(updateSettings).toHaveBeenNthCalledWith(2, { batch_enabled: true })
    expect(wrapper.get('[data-testid="image-task-ordinary-toggle"]').attributes('data-checked')).toBe('false')
    expect(wrapper.get('[data-testid="image-task-batch-toggle"]').attributes('data-checked')).toBe('true')

    await wrapper.get('[data-testid="save-image-task-settings"]').trigger('click')
    await flushPromises()

    expect(updateSettings).toHaveBeenLastCalledWith(expect.objectContaining({
      ordinary_enabled: false,
      batch_enabled: true,
      storage: expect.objectContaining({ reuse_backup_s3: true }),
    }))
    expect(showSuccess).toHaveBeenCalledWith('admin.imageTasks.saved')
  })

  it('reverts only the rejected switch and explains the missing storage target', async () => {
    updateSettings.mockRejectedValueOnce({ code: 'IMAGE_STORAGE_INCOMPLETE', message: 'incomplete' })
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-testid="image-task-ordinary-toggle"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="image-task-ordinary-toggle"]').attributes('data-checked')).toBe('true')
    expect(wrapper.get('[data-testid="image-task-batch-toggle"]').attributes('data-checked')).toBe('false')
    expect(showError).toHaveBeenCalledWith('admin.imageTasks.switches.storageRequired')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('reports a rejected target change instead of pretending it saved', async () => {
    updateSettings.mockRejectedValue(new Error('existing image tasks still reference this storage target'))
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-testid="save-image-task-settings"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
  })
})
