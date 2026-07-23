import { afterEach, describe, expect, it, vi } from 'vitest'
import { AsyncImageDownloadValidationError, downloadAsyncImageZip, listAsyncImageModels, preferredAsyncImageModel, submitAsyncImageEdit } from '../asyncImage'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('async image model API', () => {
  it('loads, normalizes, and deduplicates models with the selected API key', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        data: [{ id: 'gpt-image-1' }, { id: 'gpt-image-2' }, { id: 'gpt-image-2' }, 'custom-image-model', { id: '  ' }],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await expect(listAsyncImageModels('sk-selected-key')).resolves.toEqual([
      'gpt-image-1', 'gpt-image-2', 'custom-image-model',
    ])
    expect(fetchMock).toHaveBeenCalledWith('http://localhost:3000/v1/models', {
      headers: { Authorization: 'Bearer sk-selected-key' },
    })
  })

  it('prefers gpt-image-2 and otherwise falls back to the first returned model', () => {
    expect(preferredAsyncImageModel(['gpt-image-1', 'gpt-image-2'])).toBe('gpt-image-2')
    expect(preferredAsyncImageModel(['custom-image-model'])).toBe('custom-image-model')
    expect(preferredAsyncImageModel([])).toBe('')
  })

	it('downloads a ZIP for the task through the API-key-authenticated endpoint', async () => {
		const zip = new Blob([new Uint8Array([0x50, 0x4b, 0x03, 0x04, 0x00])], { type: 'application/zip' })
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: new Headers({ 'content-type': 'application/zip' }),
			blob: async () => zip,
		})
		vi.stubGlobal('fetch', fetchMock)

		await expect(downloadAsyncImageZip('sk-selected-key', 'imgtask_abc')).resolves.toMatchObject({ type: 'application/zip' })
		expect(fetchMock).toHaveBeenCalledWith('http://localhost:3000/v1/images/tasks/imgtask_abc/download', {
			headers: { Authorization: 'Bearer sk-selected-key' },
		})
	})

	it('rejects a successful HTTP response that is not actually a ZIP archive', async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			headers: new Headers({ 'content-type': 'application/zip' }),
			blob: async () => new Blob(['not a zip'], { type: 'application/zip' }),
		})
		vi.stubGlobal('fetch', fetchMock)

		await expect(downloadAsyncImageZip('sk-selected-key', 'imgtask_broken')).rejects.toBeInstanceOf(AsyncImageDownloadValidationError)
	})

  it('submits edits as multipart data without overriding the browser boundary', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ task_id: 'imgtask_edit' }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const image = new File(['image'], 'source.png', { type: 'image/png' })
    const mask = new File(['mask'], 'mask.png', { type: 'image/png' })

    await submitAsyncImageEdit('sk-selected-key', {
      model: 'gpt-image-2',
      prompt: 'replace the background',
      size: '1536x1024',
      quality: 'high',
      n: 2,
      images: [image],
      mask,
    })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, options] = fetchMock.mock.calls[0]
    expect(url).toBe('http://localhost:3000/v1/images/edits/async')
    expect(options).toMatchObject({ method: 'POST', headers: { Authorization: 'Bearer sk-selected-key' } })
    expect(options.headers).not.toHaveProperty('Content-Type')
    expect(options.body).toBeInstanceOf(FormData)
    const form = options.body as FormData
    expect(form.get('model')).toBe('gpt-image-2')
    expect(form.get('prompt')).toBe('replace the background')
    expect(form.get('n')).toBe('2')
    const uploadedImages = form.getAll('image[]') as File[]
    expect(uploadedImages).toHaveLength(1)
    expect(uploadedImages[0]).toMatchObject({ name: 'source.png', type: 'image/png' })
    expect(form.get('mask')).toMatchObject({ name: 'mask.png', type: 'image/png' })
  })
})
