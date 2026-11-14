import { apiClient } from './client'
import { assertAsyncImageZipResponse, type AsyncImageTask } from './asyncImage'

export type ImageTaskMode = 'ordinary' | 'batch'

export interface ImageTaskModeState {
  enabled: boolean
  can_submit: boolean
  can_view: boolean
  reason?: string
}

export interface ImageTaskModes {
  ordinary: ImageTaskModeState
  batch: ImageTaskModeState
}

export interface ImageTaskListOptions {
  status?: string
  platform?: string
  model?: string
  since?: number
  until?: number
  limit?: number
  offset?: number
}

export interface ImageTaskListResponse {
  object: string
  data: AsyncImageTask[]
  has_more: boolean
}

export interface ImageStorageSettingsPayload {
  enabled?: boolean
  reuse_backup_s3?: boolean
  bucket?: string
  prefix?: string
  append_date_path?: boolean
  public_base_url?: string
  presign_expiry_hours?: number
  max_download_bytes?: number
  endpoint?: string
  region?: string
  access_key_id?: string
  secret_access_key?: string
  force_path_style?: boolean
}

export interface ImageTaskSettings {
  ordinary_enabled: boolean
  batch_enabled: boolean
  storage: ImageStorageSettingsPayload
  storage_secret_configured: boolean
}

// The console reads every key of the signed-in user, so history survives a
// deleted or rotated API key.
export async function listUserImageTasks(options: ImageTaskListOptions = {}): Promise<ImageTaskListResponse> {
  const params = new URLSearchParams()
  params.set('limit', String(options.limit || 20))
  params.set('offset', String(options.offset || 0))
  if (options.status) params.set('status', options.status)
  if (options.platform) params.set('platform', options.platform)
  if (options.model) params.set('model', options.model)
  if (options.since) params.set('since', String(options.since))
  if (options.until) params.set('until', String(options.until))
  const { data } = await apiClient.get<ImageTaskListResponse>(`/image-tasks?` + params.toString())
  return data
}

export async function getUserImageTask(taskID: string): Promise<AsyncImageTask> {
  const { data } = await apiClient.get<AsyncImageTask>(`/image-tasks/` + encodeURIComponent(taskID))
  return data
}

export async function deleteUserImageTask(taskID: string): Promise<void> {
  await apiClient.delete(`/image-tasks/` + encodeURIComponent(taskID))
}

export async function downloadUserImageTaskZip(taskID: string): Promise<Blob> {
  const response = await apiClient.get<Blob>(`/image-tasks/` + encodeURIComponent(taskID) + '/download', {
    responseType: 'blob',
  })
  const contentType = String(response.headers['content-type'] || '')
  return assertAsyncImageZipResponse(contentType, response.data)
}

export async function getImageTaskModes(): Promise<ImageTaskModes> {
  const { data } = await apiClient.get<ImageTaskModes>('/image-tasks/modes')
  return data
}

export async function getImageTaskSettings(): Promise<ImageTaskSettings> {
  const { data } = await apiClient.get<ImageTaskSettings>('/admin/image-tasks/settings')
  return data
}

export async function updateImageTaskSettings(payload: {
  ordinary_enabled?: boolean
  batch_enabled?: boolean
  storage?: ImageStorageSettingsPayload
}): Promise<ImageTaskSettings> {
  const { data } = await apiClient.put<ImageTaskSettings>('/admin/image-tasks/settings', payload)
  return data
}

export async function testImageTaskStorage(
  storage: ImageStorageSettingsPayload,
): Promise<{ ok: boolean; message: string }> {
  const { data } = await apiClient.post<{ ok: boolean; message: string }>('/admin/image-tasks/settings/test', storage)
  return data
}
