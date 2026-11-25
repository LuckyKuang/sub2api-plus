import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const routerSource = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts'),
  'utf8'
)

describe('administrator assistance guard', () => {
  it('resolves invalid and self targets without falling back to a writable personal page', () => {
    expect(routerSource).toContain('resolveAdminSupportRouteDecision')
    expect(routerSource).toContain("if (decision.kind === 'self')")
    expect(routerSource).toContain("if (decision.kind === 'safe-landing')")
    expect(routerSource).not.toContain('next(selfPathForSupportResource(supportResource))')
  })

  it('treats browser history navigation as an explicit destination decision', () => {
    expect(routerSource).toContain("window.addEventListener('popstate'")
    expect(routerSource).toContain('requestSupportExit()')
    expect(routerSource).toContain('consumeSupportExit()')
    expect(routerSource).toContain('supportExitRequested')
  })
})
