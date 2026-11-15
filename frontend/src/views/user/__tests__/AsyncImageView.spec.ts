import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import AsyncImageView from '../AsyncImageView.vue'
import DateTimePicker from '@/components/common/DateTimePicker.vue'
import { installAppStyles } from '@/__tests__/appStyles'
import { setAdminSupportContext } from '@/utils/adminSupportContext'

enableAutoUnmount(afterEach)

const {
  keysList,
  deleteUserImageTask,
  listUserImageTasks,
  getUserImageTaskFilters,
  getUserImageTask,
  downloadUserImageTaskZip,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  keysList: vi.fn(),
  deleteUserImageTask: vi.fn(),
  listUserImageTasks: vi.fn(),
  getUserImageTaskFilters: vi.fn(),
  getUserImageTask: vi.fn(),
  downloadUserImageTaskZip: vi.fn(),
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
  downloadUserImageTaskZip,
  getUserImageTask,
  listUserImageTasks,
  getUserImageTaskFilters,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const { default: messages } = await import('@/i18n/locales/en/asyncImage')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key.startsWith('asyncImage.status.')) {
          const status = key.slice('asyncImage.status.'.length) as keyof typeof messages.asyncImage.status
          return messages.asyncImage.status[status] || key
        }
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
  template: '<div class="data-table"><div v-for="row in data" :key="row.task_id" class="data-row" :data-task-id="row.task_id"><slot name="cell-status" :row="row" /><slot name="cell-actions" :row="row" /></div><slot v-if="data.length === 0" name="empty" /></div>',
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
    props: { canSubmit: true },
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
    setAdminSupportContext(null)
    keysList.mockReset()
    deleteUserImageTask.mockReset()
    getUserImageTask.mockReset()
    downloadUserImageTaskZip.mockReset()
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
    getUserImageTaskFilters.mockResolvedValue({ platforms: ['grok', 'openai', 'unknown'], models: ['retired-image-model', 'gpt-image-1'] })
    getUserImageTask.mockImplementation(async (id: string) => id === failedTask.task_id ? failedTask : completedTask)
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [failedTask, completedTask], has_more: false })
  })

  afterEach(() => {
    setAdminSupportContext(null)
    document.body.innerHTML = ''
    vi.clearAllMocks()
    vi.useRealTimers()
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

  it('does not restart polling when an in-flight poll completes after unmount', async () => {
    // Leaving the panel ends its requests, even when the old scope's HTTP read
    // finishes after the login/support target has changed.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const processing = { ...completedTask, status: 'processing' }
    let finishPoll!: (task: typeof processing) => void
    const pendingPoll = new Promise<typeof processing>(resolve => { finishPoll = resolve })
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [processing], has_more: false })
    getUserImageTask.mockResolvedValueOnce(processing).mockReturnValueOnce(pendingPoll)
    const wrapper = mountView()
    await flushPromises()
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    setAdminSupportContext({ actorId: 1, userId: 42 })
    finishPoll(processing)
    await flushPromises()
    vi.advanceTimersByTime(9000)
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledTimes(2)
    expect(listUserImageTasks).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['keys', 'filters'])('does not continue initial loading after unmount while %s are pending', async dependency => {
    let finish!: (value: unknown) => void
    const pending = new Promise<unknown>(resolve => { finish = resolve })
    if (dependency === 'keys') keysList.mockReturnValueOnce(pending)
    else getUserImageTaskFilters.mockReturnValueOnce(pending)
    const wrapper = mountView()
    await flushPromises()
    expect(listUserImageTasks).not.toHaveBeenCalled()
    wrapper.unmount()
    setAdminSupportContext({ actorId: 1, userId: 42 })
    finish(dependency === 'keys'
      ? { items: [{ id: 9 }], pages: 2 }
      : { platforms: ['openai'], models: [] })
    await flushPromises()
    expect(keysList).toHaveBeenCalledTimes(1)
    expect(listUserImageTasks).not.toHaveBeenCalled()
    expect(getUserImageTask).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it('discards a pending poll when a newer task list replaces its generation', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const processing = { ...completedTask, status: 'processing' }
    let finishPoll!: (task: typeof processing) => void
    getUserImageTask.mockReturnValue(new Promise<typeof processing>(resolve => { finishPoll = resolve }))
    listUserImageTasks
      .mockResolvedValueOnce({ object: 'list', data: [processing], has_more: false })
      .mockResolvedValue({ object: 'list', data: [completedTask], has_more: false })
    const wrapper = mountView()
    await flushPromises()
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledTimes(1)
    await wrapper.get('button[title="asyncImage.actions.refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-task-id="imgtask_completed"] .badge').text()).toBe('Completed')
    finishPoll(processing)
    await flushPromises()
    expect(wrapper.get('[data-task-id="imgtask_completed"] .badge').text()).toBe('Completed')
    vi.advanceTimersByTime(6000)
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  // Proposal R1/R2: switching new submissions off must leave history readable.
  it('disables creation while retaining history and failed-task management', async () => {
    const wrapper = mountView()
    await wrapper.setProps({ canSubmit: false })
    await flushPromises()

    const create = wrapper.findAll('button').find(button => button.text().includes('asyncImage.actions.create'))!
    expect(create.attributes('disabled')).toBeDefined()
    await create.trigger('click')
    expect(wrapper.find('.base-dialog').exists()).toBe(false)
    expect(wrapper.find('[data-testid="view-task-imgtask_failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="delete-task-imgtask_failed"]').exists()).toBe(true)
  })

  it('offers the real Composite image key in the creation form', async () => {
    keysList.mockResolvedValue({ items: [{ id: 1475, user_id: 7, key: 'sk-composite-test', name: 'images', status: 'active', group: { platform: 'composite', status: 'active', allow_image_generation: true } }] })
    const wrapper = mountView()
    await flushPromises()
    await clickButtonByText(wrapper, 'asyncImage.actions.create')
    await flushPromises()
    expect(wrapper.get('.base-dialog').text()).not.toContain('asyncImage.create.noKeys')
    expect(wrapper.get('.base-dialog').text()).toContain('images')
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

  it('offers unknown platforms and retired models from user history', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="async-image-platform-filter"]').trigger('click')
    const unknown = Array.from(document.querySelectorAll('[role="option"]'))
      .find(option => option.textContent?.includes('asyncImage.filters.unknownPlatform'))
    expect(unknown).toBeTruthy()
    ;(unknown as HTMLElement).click()
    await flushPromises()
    expect(listUserImageTasks).toHaveBeenLastCalledWith(expect.objectContaining({ platform: 'unknown', offset: 0 }))

    await wrapper.get('[data-testid="async-image-model-filter"]').trigger('click')
    const retired = Array.from(document.querySelectorAll('[role="option"]'))
      .find(option => option.textContent?.includes('retired-image-model'))
    expect(retired).toBeTruthy()
    ;(retired as HTMLElement).click()
    await flushPromises()
    expect(listUserImageTasks).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'retired-image-model', offset: 0 }))
  })

  it('sends absolute start-inclusive/end-exclusive timestamps and resets the page', async () => {
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [completedTask], has_more: true })
    const wrapper = mountView()
    await flushPromises()
    await clickButtonByText(wrapper, 'asyncImage.list.next')
    await flushPromises()
    expect(listUserImageTasks).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 20 }))
    const [start, end] = wrapper.findAllComponents(DateTimePicker)
    start.vm.$emit('update:modelValue', '2026-01-12T01:00')
    end.vm.$emit('update:modelValue', '2026-01-13T02:00')
    await flushPromises()
    end.vm.$emit('change', new Event('change'))
    await flushPromises()
    expect(listUserImageTasks).toHaveBeenLastCalledWith(expect.objectContaining({
      since: new Date(2026, 0, 12, 1).getTime() / 1000,
      until: new Date(2026, 0, 13, 2).getTime() / 1000,
      offset: 0,
    }))
  })

  it('shows uncertain execution separately from the unchanged processing status', async () => {
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [{ ...failedTask, status: 'processing', execution_status: 'unconfirmed' }], has_more: false })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-task-id="imgtask_failed"] .badge').text()).toBe('Processing')
    expect(wrapper.text()).toContain('asyncImage.detail.unconfirmed')
  })

  it.each(['unarchived', 'incomplete', 'storage_unavailable', 'object_missing'])('shows %s without replaying saved URLs or changing completion', async readStatus => {
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [{ ...completedTask, read_status: readStatus, result: { data: [{ url: 'https://expired.example/image.png' }] } }], has_more: false })
    const wrapper = mountView(true)
    await flushPromises()
    expect(wrapper.findAll('.badge').map(badge => badge.text())).toContain('Completed')
    expect(wrapper.text()).toContain('asyncImage.readStatus.' + readStatus)
    expect(wrapper.find('img').exists()).toBe(false)
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
    const detail = {
      ...completedTask,
      requested_images: 3,
      actual_images: 1,
      result: { data: [{ url: 'https://images.example.test/one.png' }] },
    }
    listUserImageTasks.mockResolvedValue({
      object: 'list',
      data: [detail],
      has_more: false,
    })
    getUserImageTask.mockResolvedValue(detail)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="view-task-imgtask_completed"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('asyncImage.detail.requestedImages')
    expect(wrapper.text()).toContain('asyncImage.detail.actualImages')
    expect(wrapper.text()).toContain('asyncImage.detail.countMismatch')
  })

  it('fetches fresh authorized detail instead of showing an expired URL from the list', async () => {
    const stale = 'https://expired.example/image.png'
    const fresh = 'https://fresh.example/image.png'
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [{ ...completedTask, result: { data: [{ url: stale }] } }], has_more: false })
    getUserImageTask.mockResolvedValue({ ...completedTask, archive_status: 'archived', read_status: 'readable', result: { data: [{ url: fresh }] } })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="view-task-imgtask_completed"]').trigger('click')
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledWith(completedTask.task_id)
    expect(wrapper.findAll('.base-dialog img').map(image => image.attributes('src'))).toEqual([fresh])
  })

  // Proposal R4: deleting the creating key preserves user history and labels it.
  it('labels a deleted creating key in history and task details', async () => {
    const task = { ...completedTask, api_key_deleted: true }
    keysList.mockResolvedValue({ items: [], pages: 1 })
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [task], has_more: false })
    getUserImageTask.mockResolvedValue(task)
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-task-id="imgtask_completed"]').text()).toContain('asyncImage.detail.deletedKey')
    await wrapper.get('[data-testid="view-task-imgtask_completed"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.base-dialog').text()).toContain('asyncImage.detail.deletedKey')
    expect(getUserImageTask).toHaveBeenCalledWith(completedTask.task_id)
  })

  it('keeps assistance history, filters and downloads available without target API keys while blocking mutations', async () => {
    setAdminSupportContext({ actorId: 1, userId: 42 })
    const task = { ...completedTask, api_key_deleted: true, read_status: 'readable', result: { data: [{ url: 'https://stored.example/image.png' }] } }
    listUserImageTasks.mockResolvedValue({ object: 'list', data: [task, failedTask], has_more: false })
    getUserImageTask.mockResolvedValue(task)
    downloadUserImageTaskZip.mockResolvedValue(new Blob(['zip']))
    const wrapper = mountView()
    await flushPromises()

    expect(keysList).not.toHaveBeenCalled()
    expect(getUserImageTaskFilters).toHaveBeenCalled()
    expect(listUserImageTasks).toHaveBeenCalledWith(expect.objectContaining({ offset: 0 }))
    expect(wrapper.find('[data-testid="async-image-api-key-filter"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="async-image-platform-filter"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="async-image-model-filter"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="delete-task-imgtask_failed"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="delete-task-imgtask_failed"]').trigger('click')
    expect(deleteUserImageTask).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="view-task-imgtask_completed"]').trigger('click')
    await flushPromises()
    expect(getUserImageTask).toHaveBeenCalledWith(completedTask.task_id)
    await clickButtonByText(wrapper, 'asyncImage.actions.downloadAll')
    await flushPromises()
    expect(downloadUserImageTaskZip).toHaveBeenCalledWith(completedTask.task_id)
    const create = wrapper.findAll('button').find(button => button.text().includes('asyncImage.actions.create'))!
    expect(create.attributes('disabled')).toBeDefined()
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
