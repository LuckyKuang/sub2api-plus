import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import AsyncImageView from '../AsyncImageView.vue'
import { installAppStyles } from '@/__tests__/appStyles'

enableAutoUnmount(afterEach)

const {
  keysList,
  deleteUserImageTask,
  listUserImageTasks,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  keysList: vi.fn(),
  deleteUserImageTask: vi.fn(),
  listUserImageTasks: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api', () => ({
  keysAPI: { list: keysList },
}))

vi.mock('@/api/asyncImage', () => ({
  AsyncImageDownloadValidationError: class AsyncImageDownloadValidationError extends Error {},
  listAsyncImageModels: vi.fn().mockResolvedValue([]),
  preferredAsyncImageModel: vi.fn().mockReturnValue(''),
  saveAsyncImageBlob: vi.fn(),
  submitAsyncImageEdit: vi.fn(),
  submitAsyncImageGeneration: vi.fn(),
}))

// History is user-scoped: the console never passes an API key to read or delete.
vi.mock('@/api/imageTasks', () => ({
  deleteUserImageTask,
  downloadUserImageTaskZip: vi.fn(),
  getUserImageTask: vi.fn(),
  listUserImageTasks,
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
        if (params?.taskId) return key + ':' + String(params.taskId)
        if (params?.page) return key + ':' + String(params.page)
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
  template: '<div class="data-table"><div v-for="row in data" :key="row.task_id" class="data-row" :data-task-id="row.task_id"><slot name="cell-actions" :row="row" /></div><slot v-if="data.length === 0" name="empty" /></div>',
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

function mountView(realTable = false) {
  return mount(AsyncImageView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: appLayoutStub,
        TablePageLayout: pageLayoutStub,
        DataTable: realTable ? false : dataTableStub,
        BaseDialog: baseDialogStub,
        Icon: iconStub,
      },
    },
  })
}

async function clickButtonByText(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find(button => button.text() === text)
  expect(button, 'Expected button: ' + text).toBeDefined()
  await button!.trigger('click')
}

describe('AsyncImageView task management', () => {
  let removeStyles: () => void
  beforeAll(async () => { removeStyles = await installAppStyles() }, 30_000)
  afterAll(() => removeStyles?.())

  beforeEach(() => {
    keysList.mockReset()
    deleteUserImageTask.mockReset()
    listUserImageTasks.mockReset()
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
    deleteUserImageTask.mockResolvedValue(undefined)
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [failedTask, completedTask], has_more: false })
  })

  afterEach(() => {
    document.body.innerHTML = ''
    vi.clearAllMocks()
  })

  it('uses matching shared Select triggers for the platform and status filters', async () => {
    const wrapper = mountView()
    await flushPromises()

    const platformFilter = wrapper.get('[data-testid="async-image-platform-filter"]')
    const statusFilter = wrapper.get('[data-testid="async-image-status-filter"]')

    expect(platformFilter.element.tagName).toBe('BUTTON')
    expect(statusFilter.element.tagName).toBe('BUTTON')
    expect(platformFilter.classes()).toEqual(statusFilter.classes())
    expect(platformFilter.find('[role="listbox"]').exists()).toBe(false)
    expect(platformFilter.attributes('aria-label')).toBe('asyncImage.filters.allPlatforms')
    expect(statusFilter.attributes('aria-label')).toBe('asyncImage.filters.allStatuses')
  })

  it('loads every key of the signed-in user without an API key filter', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listUserImageTasks).toHaveBeenCalledWith(expect.objectContaining({ offset: 0, platform: '', model: '' }))
    expect(wrapper.find('[data-testid="async-image-api-key-filter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="view-task-imgtask_failed"]').exists()).toBe(true)
  })

  it('sends the platform filter to the user-scoped list request', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="async-image-platform-filter"]').trigger('click')
    const grokOption = Array.from(document.querySelectorAll('[role="option"]'))
      .find(option => option.textContent?.includes('Grok'))
    expect(grokOption).toBeTruthy()
    ;(grokOption as HTMLElement).click()
    await flushPromises()

    expect(listUserImageTasks).toHaveBeenLastCalledWith(expect.objectContaining({ platform: 'grok', offset: 0 }))
  })

  it.each([1, 2, 3])('centers the actual result thumbnails for %i images under their heading', async count => {
    const urls = Array.from({ length: count }, (_, index) => 'https://images.example.test/' + index + '.png')
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [{ ...completedTask, result: { data: urls.map(url => ({ url })) } }], has_more: false })
    const wrapper = mountView(true)
    await flushPromises()
    const table = wrapper.get('table')
    const headings = table.findAll('thead th')
    const index = headings.findIndex(cell => cell.text() === 'asyncImage.columns.result')
    expect(index).toBeGreaterThanOrEqual(0)
    const cell = table.findAll('tbody tr')[0].findAll('td')[index]
    expect(cell.findAll('img').map(image => image.attributes('src'))).toEqual(urls)
    const gallery = cell.get('img').element.parentElement!.parentElement!
    expect(getComputedStyle(cell.element).textAlign).toBe('center')
    expect(getComputedStyle(gallery).display).toBe('inline-grid')
    const columns = getComputedStyle(gallery).gridTemplateColumns
    expect(columns).toBe(count === 1 ? 'repeat(1, minmax(0, 1fr))' : 'repeat(2, minmax(0, 1fr))')
    expect(getComputedStyle(headings[index].get('div').element).justifyContent).toBe('center')
  })

  it('keeps history visible when the user has no key eligible for new submissions', async () => {
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

    expect(listUserImageTasks).toHaveBeenCalledWith(expect.objectContaining({ offset: 0 }))
    expect(wrapper.find('[data-testid="view-task-imgtask_failed"]').exists()).toBe(true)

    await clickButtonByText(wrapper, 'asyncImage.actions.create')
    await vi.waitFor(() => expect(wrapper.find('.base-dialog').exists()).toBe(true))
    expect(wrapper.text()).toContain('asyncImage.create.noKeys')
  })

  it('shows delete only for failed rows and supports cancel', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="delete-task-imgtask_completed"]').exists()).toBe(false)

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    expect(document.body.textContent).toContain('asyncImage.delete.confirm:imgtask_failed')
    await clickButtonByText(wrapper, 'common.cancel')
    await flushPromises()

    expect(deleteUserImageTask).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
  })

  it('shows requested and actual image counts when the upstream returns fewer images', async () => {
    listUserImageTasks.mockResolvedValue({
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

  it('deletes a failed task by its own id and shows pending state only on that row', async () => {
    let finishDelete!: () => void
    deleteUserImageTask.mockReturnValue(new Promise<void>((resolve) => { finishDelete = resolve }))
    listUserImageTasks
      .mockResolvedValueOnce({ object: 'list', data: [failedTask, completedTask], has_more: false })
      .mockResolvedValue({ object: 'list', data: [completedTask], has_more: false })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="view-task-imgtask_failed"]').trigger('click')
    expect(wrapper.text()).toContain('asyncImage.detail.taskId')
    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    await clickButtonByText(wrapper, 'common.delete')
    await flushPromises()

    expect(deleteUserImageTask).toHaveBeenCalledWith('imgtask_failed')
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
    deleteUserImageTask.mockRejectedValue(Object.assign(new Error('not allowed'), { code: 'IMAGE_TASK_DELETE_NOT_ALLOWED' }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    await clickButtonByText(wrapper, 'common.delete')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('asyncImage.errors.deleteNotAllowed')
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
    expect(listUserImageTasks).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="async-image-status-filter"]').text()).toContain('asyncImage.filters.allStatuses')
  })

  it('falls back to the preceding page when deletion empties a later page', async () => {
    listUserImageTasks
      .mockResolvedValueOnce({ object: 'list', data: [completedTask], has_more: true })
      .mockResolvedValueOnce({ object: 'list', data: [failedTask], has_more: false })
      .mockResolvedValueOnce({ object: 'list', data: [], has_more: false })
      .mockResolvedValueOnce({ object: 'list', data: [completedTask], has_more: false })
    const wrapper = mountView()
    await flushPromises()

    await clickButtonByText(wrapper, 'asyncImage.list.next')
    await flushPromises()
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)

    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    await clickButtonByText(wrapper, 'common.delete')
    await flushPromises()

    expect(listUserImageTasks).toHaveBeenCalledTimes(4)
    expect(listUserImageTasks.mock.calls.map(call => call[0]?.offset)).toEqual([0, 20, 20, 0])
    expect(wrapper.text()).toContain('asyncImage.list.page:1')
  })
})
