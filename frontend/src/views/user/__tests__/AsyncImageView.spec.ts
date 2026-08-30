import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AsyncImageView from '../AsyncImageView.vue'

const {
  keysList,
  deleteAsyncImageTask,
  listAsyncImageTasks,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  keysList: vi.fn(),
  deleteAsyncImageTask: vi.fn(),
  listAsyncImageTasks: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api', () => ({
  keysAPI: { list: keysList },
}))

vi.mock('@/api/asyncImage', () => ({
  AsyncImageDownloadValidationError: class AsyncImageDownloadValidationError extends Error {},
  deleteAsyncImageTask,
  downloadAsyncImageZip: vi.fn(),
  getAsyncImageTask: vi.fn(),
  listAsyncImageModels: vi.fn().mockResolvedValue([]),
  listAsyncImageTasks,
  preferredAsyncImageModel: vi.fn().mockReturnValue(''),
  saveAsyncImageBlob: vi.fn(),
  submitAsyncImageEdit: vi.fn(),
  submitAsyncImageGeneration: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (params?.taskId) return `${key}:${params.taskId}`
        if (params?.page) return `${key}:${params.page}`
        return key
      },
    }),
  }
})

const pageLayoutStub = {
  template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>',
}
const appLayoutStub = { template: '<main><slot /></main>' }
const baseDialogStub = {
  props: ['show'],
  template: '<section v-if="show" class="base-dialog"><slot /><slot name="footer" /></section>',
}
const dataTableStub = {
  props: ['data'],
  template: `
    <div class="data-table">
      <div v-for="row in data" :key="row.task_id" class="data-row" :data-task-id="row.task_id">
        <slot name="cell-actions" :row="row" />
      </div>
      <slot v-if="data.length === 0" name="empty" />
    </div>
  `,
}
const iconStub = {
  props: ['name'],
  template: '<span class="icon" :data-icon="name" />',
}

const failedTask = {
  id: 'imgtask_failed',
  task_id: 'imgtask_failed',
  object: 'image.generation.task',
  status: 'failed',
  created_at: 1,
  expires_at: 2,
  error: { message: 'upstream failed' },
}

const completedTask = {
  ...failedTask,
  id: 'imgtask_completed',
  task_id: 'imgtask_completed',
  status: 'completed',
  error: undefined,
}

const otherKeyTask = {
  ...completedTask,
  id: 'imgtask_other_key',
  task_id: 'imgtask_other_key',
}

function mountView() {
  return mount(AsyncImageView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: appLayoutStub,
        TablePageLayout: pageLayoutStub,
        DataTable: dataTableStub,
        BaseDialog: baseDialogStub,
        Icon: iconStub,
      },
    },
  })
}

function findButtonByText(text: string) {
  return Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.trim() === text)
}

describe('AsyncImageView task management', () => {
  beforeEach(() => {
    keysList.mockReset()
    deleteAsyncImageTask.mockReset()
    listAsyncImageTasks.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    keysList.mockResolvedValue({
      items: [{
        id: 9,
        user_id: 7,
        key: 'sk-selected-key',
        name: 'A very long selected API key name that must truncate',
        status: 'active',
        group: { platform: 'openai', allow_image_generation: true },
      }],
    })
    deleteAsyncImageTask.mockResolvedValue(undefined)
    listAsyncImageTasks.mockResolvedValue({ object: 'list', data: [failedTask, completedTask], has_more: false })
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('uses matching shared Select triggers for both top filters', async () => {
    const wrapper = mountView()
    await flushPromises()

    const apiKeyFilter = wrapper.get('[data-testid="async-image-api-key-filter"]')
    const statusFilter = wrapper.get('[data-testid="async-image-status-filter"]')
    const apiKeyTrigger = apiKeyFilter.get('.select-trigger')
    const statusTrigger = statusFilter.get('.select-trigger')

    expect(apiKeyTrigger.element.tagName).toBe('BUTTON')
    expect(statusTrigger.element.tagName).toBe('BUTTON')
    expect(apiKeyTrigger.classes()).toEqual(statusTrigger.classes())
    expect(apiKeyTrigger.find('.select-value').classes()).toContain('select-value')
    expect(statusTrigger.find('.select-icon').exists()).toBe(true)
    expect(apiKeyTrigger.attributes('aria-label')).toBe('asyncImage.filters.apiKey')
    expect(statusTrigger.attributes('aria-label')).toBe('asyncImage.filters.allStatuses')
  })

  it('keeps exhausted keys selectable for history management but not task creation', async () => {
    keysList.mockResolvedValue({
      items: [{
        id: 11,
        user_id: 7,
        key: 'sk-exhausted-key',
        name: 'Exhausted image key',
        status: 'quota_exhausted',
        group: { platform: 'openai', allow_image_generation: true },
      }],
    })
    const wrapper = mountView()
    await flushPromises()

    expect(listAsyncImageTasks).toHaveBeenCalledWith('sk-exhausted-key', expect.objectContaining({ offset: 0 }))
    expect(wrapper.get('[data-testid="async-image-api-key-filter"] .select-trigger').text()).toContain('Exhausted image key')

    findButtonByText('asyncImage.actions.create')?.click()
    await flushPromises()
    expect(wrapper.text()).toContain('asyncImage.create.noKeys')
  })

  it('keeps a key selectable for history after it moves to another platform', async () => {
    keysList.mockResolvedValue({
      items: [{
        id: 12,
        user_id: 7,
        key: 'sk-reassigned-key',
        name: 'Reassigned key',
        status: 'active',
        group: { platform: 'anthropic', allow_image_generation: false },
      }],
    })
    const wrapper = mountView()
    await flushPromises()

    expect(listAsyncImageTasks).toHaveBeenCalledWith('sk-reassigned-key', expect.objectContaining({ offset: 0 }))
    expect(wrapper.get('[data-testid="async-image-api-key-filter"] .select-trigger').text()).toContain('Reassigned key')

    findButtonByText('asyncImage.actions.create')?.click()
    await flushPromises()
    expect(wrapper.text()).toContain('asyncImage.create.noKeys')
  })

  it('shows delete only for failed rows and supports cancel', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="delete-task-imgtask_completed"]').exists()).toBe(false)

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    expect(document.body.textContent).toContain('asyncImage.delete.confirm:imgtask_failed')
    findButtonByText('common.cancel')?.click()
    await flushPromises()

    expect(deleteAsyncImageTask).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
  })

  it('shows requested and actual image counts when the upstream returns fewer images', async () => {
    listAsyncImageTasks.mockResolvedValue({
      object: 'list',
      data: [{
        ...completedTask,
        requested_images: 3,
        actual_images: 1,
        result: { data: [{ url: 'https://images.example.test/one.png' }] },
      }],
      has_more: false,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="view-task-imgtask_completed"]').trigger('click')
    expect(wrapper.text()).toContain('asyncImage.detail.requestedImages')
    expect(wrapper.text()).toContain('asyncImage.detail.actualImages')
    expect(wrapper.text()).toContain('asyncImage.detail.countMismatch')
  })

  it('uses the selected API key and shows pending state only on the deleted row', async () => {
    let finishDelete!: () => void
    deleteAsyncImageTask.mockReturnValue(new Promise<void>((resolve) => { finishDelete = resolve }))
    listAsyncImageTasks
      .mockResolvedValueOnce({ object: 'list', data: [failedTask, completedTask], has_more: false })
      .mockResolvedValue({ object: 'list', data: [completedTask], has_more: false })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="view-task-imgtask_failed"]').trigger('click')
    expect(wrapper.text()).toContain('asyncImage.detail.taskId')
    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    findButtonByText('common.delete')?.click()
    await flushPromises()

    expect(deleteAsyncImageTask).toHaveBeenCalledWith('sk-selected-key', 'imgtask_failed')
    const deleteButton = wrapper.get('[data-testid="delete-task-imgtask_failed"]')
    expect(deleteButton.attributes('disabled')).toBeDefined()
    expect(deleteButton.get('[data-icon="refresh"]').classes()).toContain('animate-spin')

    finishDelete()
    await flushPromises()
    expect(showSuccess).toHaveBeenCalledWith('asyncImage.delete.success')
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('asyncImage.detail.taskId')
  })

  it('keeps the failed row and filters when deletion is rejected', async () => {
    deleteAsyncImageTask.mockRejectedValue(Object.assign(new Error('not allowed'), { code: 'IMAGE_TASK_DELETE_NOT_ALLOWED' }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    findButtonByText('common.delete')?.click()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('asyncImage.errors.deleteNotAllowed')
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
    expect(listAsyncImageTasks).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="async-image-status-filter"] .select-trigger').text()).toContain('asyncImage.filters.allStatuses')
  })

  it('falls back to the preceding page when deletion empties a later page', async () => {
    listAsyncImageTasks
      .mockResolvedValueOnce({ object: 'list', data: [completedTask], has_more: true })
      .mockResolvedValueOnce({ object: 'list', data: [failedTask], has_more: false })
      .mockResolvedValueOnce({ object: 'list', data: [], has_more: false })
      .mockResolvedValueOnce({ object: 'list', data: [completedTask], has_more: false })
    const wrapper = mountView()
    await flushPromises()

    findButtonByText('asyncImage.list.next')?.click()
    await flushPromises()
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    findButtonByText('common.delete')?.click()
    await flushPromises()

    expect(listAsyncImageTasks).toHaveBeenCalledTimes(4)
    expect(listAsyncImageTasks.mock.calls.map(call => call[1]?.offset)).toEqual([0, 20, 20, 0])
    expect(wrapper.text()).toContain('asyncImage.list.page:1')
  })

  it('does not let a stale post-delete refresh overwrite a newly selected API key', async () => {
    let finishDeletedKeyRefresh!: (value: { object: string; data: typeof failedTask[]; has_more: boolean }) => void
    const deletedKeyRefresh = new Promise<{ object: string; data: typeof failedTask[]; has_more: boolean }>((resolve) => {
      finishDeletedKeyRefresh = resolve
    })
    keysList.mockResolvedValue({
      items: [
        {
          id: 9,
          user_id: 7,
          key: 'sk-selected-key',
          name: 'Primary image key',
          status: 'active',
          group: { platform: 'openai', allow_image_generation: true },
        },
        {
          id: 10,
          user_id: 7,
          key: 'sk-other-key',
          name: 'Secondary image key',
          status: 'active',
          group: { platform: 'openai', allow_image_generation: true },
        },
      ],
    })
    listAsyncImageTasks
      .mockResolvedValueOnce({ object: 'list', data: [failedTask], has_more: false })
      .mockReturnValueOnce(deletedKeyRefresh)
      .mockResolvedValueOnce({ object: 'list', data: [otherKeyTask], has_more: false })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    findButtonByText('common.delete')?.click()
    await flushPromises()

    await wrapper.get('[data-testid="async-image-api-key-filter"] .select-trigger').trigger('click')
    const otherKeyOption = Array.from(document.body.querySelectorAll<HTMLElement>('[role="option"]'))
      .find(option => option.textContent?.includes('Secondary image key'))
    expect(otherKeyOption).toBeDefined()
    otherKeyOption?.click()
    await flushPromises()

    expect(listAsyncImageTasks).toHaveBeenLastCalledWith('sk-other-key', expect.objectContaining({ offset: 0 }))
    expect(wrapper.find('[data-testid="view-task-imgtask_other_key"]').exists()).toBe(true)

    finishDeletedKeyRefresh({ object: 'list', data: [failedTask], has_more: false })
    await flushPromises()
    expect(wrapper.find('[data-testid="view-task-imgtask_other_key"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="view-task-imgtask_failed"]').exists()).toBe(false)
  })
})
