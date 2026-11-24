import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AdminSupportUserSelector.vue')
const source = readFileSync(componentPath, 'utf8')

describe('administrator support account selector', () => {
  it('defaults to the authenticated administrator and navigates without replacing auth state', () => {
    expect(source).toContain('authStore.user?.id')
    expect(source).toContain('accountSelectionDestination')
    expect(source).toContain('router.push(destination)')
    expect(source).not.toContain('authStore.user =')
    expect(source).not.toContain('authStore.token =')
    expect(source).not.toContain('localStorage')
    expect(source).not.toContain('sessionStorage')
    expect(source).toContain('description: `${user.email} #${user.id}`')
    expect(source).toContain('loadAccounts().catch(() => undefined)')
  })

  it('locks the old page during a target switch instead of clearing the context early', () => {
    expect(source).toContain('beginSupportTransition()')
    expect(source).toContain('endSupportTransition()')
    expect(source).toContain('requestSupportExit()')
    expect(source).toContain('clearSupportExit()')
    expect(source).not.toContain('setAdminSupportContext(null)')
    expect(source).toContain('returningToSelf')
  })

  it('identifies the selected account by id so equal display names stay distinct', () => {
    expect(source).toContain('· #{{ selectedUserId }}')
  })
})
