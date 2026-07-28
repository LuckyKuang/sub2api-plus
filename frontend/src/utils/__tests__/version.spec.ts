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
  it.each([
    ['v0.1.166+custom.004', 'v0.1.166-custom.004'],
    ['0.1.166+custom.004', 'v0.1.166-custom.004'],
    ['', ''],
    [undefined, '']
  ])('converts %p into an OCI-compatible release tag', (input, expected) => {
    expect(toDockerImageTag(input)).toBe(expected)
  })
})
