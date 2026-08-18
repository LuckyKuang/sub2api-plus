import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const directory = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(resolve(directory, '../AdminSupportView.vue'), 'utf8')
const apiSource = readFileSync(resolve(directory, '../../../../api/admin/supportView.ts'), 'utf8')

describe('administrator support view safety boundary', () => {
  it('does not import owner mutation, credential-copy, or export capabilities', () => {
    for (const forbidden of [
      'useClipboard',
      'file-saver',
      'deleteAsyncImageTask',
      'submitAsyncImage',
      'cancelOrder',
      'requestRefund',
      'updateSubscription',
    ]) {
      expect(viewSource).not.toContain(forbidden)
    }
  })

  it('keeps the support client GET-only', () => {
    expect(apiSource).toContain('apiClient.get')
    expect(apiSource).not.toContain('apiClient.post')
    expect(apiSource).not.toContain('apiClient.put')
    expect(apiSource).not.toContain('apiClient.patch')
    expect(apiSource).not.toContain('apiClient.delete')
  })

  it('guards async results against stale target navigation', () => {
    expect(viewSource).toContain('isCurrentReadRequest')
    expect(viewSource).toContain('request.userId')
    expect(viewSource).not.toContain('Number(route.params.user_id)')
  })
})
