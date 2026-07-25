import { describe, expect, it } from 'vitest'
import { normalizeDisplayVersion, toDockerImageTag } from '@/utils/version'

describe('normalizeDisplayVersion', () => {
  it.each([
    ['0.1.164+custom.001', '0.1.164+custom.001'],
    ['v0.1.164+custom.001', '0.1.164+custom.001'],
    ['', ''],
    [undefined, '']
  ])('normalizes %p', (input, expected) => {
    expect(normalizeDisplayVersion(input)).toBe(expected)
  })
})

describe('toDockerImageTag', () => {
  it('converts custom build metadata into an OCI-compatible tag', () => {
    expect(toDockerImageTag('v0.1.164+custom.001')).toBe('0.1.164-custom.001')
  })
})
