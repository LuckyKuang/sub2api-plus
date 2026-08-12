import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../VersionBadge.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('VersionBadge dropdown width', () => {
  it('uses a fixed 20rem width for long custom version strings', () => {
    expect(componentSource).toContain('mt-2 w-80 overflow-hidden')
    expect(componentSource).not.toContain("'w-64'")
  })
})
