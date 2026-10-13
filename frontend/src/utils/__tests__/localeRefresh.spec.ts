import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { notifyLocaleChanged, useLocaleRefresh } from '../localeRefresh'

describe('translated read models', () => {
  it('refreshes registered reads and preserves a draft without remounting', async () => {
    const draft = ref('unsaved group name')
    const groupName = ref('Original')
    const read = vi.fn(async () => { groupName.value = 'Translated' })
    const scope = effectScope()
    scope.run(() => useLocaleRefresh(read))
    notifyLocaleChanged()
    await flushPromises()
    expect(read).toHaveBeenCalledTimes(1)
    expect(groupName.value).toBe('Translated')
    expect(draft.value).toBe('unsaved group name')
    scope.stop()
    notifyLocaleChanged()
    await flushPromises()
    expect(read).toHaveBeenCalledTimes(1)
  })
})
