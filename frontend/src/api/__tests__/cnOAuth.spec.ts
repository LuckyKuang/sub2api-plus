import { beforeEach, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { cnOAuthModels } from '../admin/cnOAuth'

vi.mock('../client', () => ({ apiClient: { post: vi.fn() } }))
beforeEach(() => vi.mocked(apiClient.post).mockReset())

it('discovers StepFun models with only the opaque OAuth session handle', async () => {
  vi.mocked(apiClient.post).mockResolvedValue({ data: { models: ['step-3.7-flash'] } })
  expect(await cnOAuthModels('ready-session')).toEqual({ models: ['step-3.7-flash'] })
  expect(apiClient.post).toHaveBeenCalledWith('/admin/cn/oauth/stepfun/models', { session_id: 'ready-session' })
})
