import { describe, expect, it } from 'vitest'
import en from '../locales/en'
import zh from '../locales/zh'

describe('admin service tier locale keys', () => {
  it('provides separate request, response, historical and absent labels', () => {
    expect(zh.admin.usage).toMatchObject({
      serviceTier: '服务档位', serviceTierRequest: '请求', serviceTierResponse: '响应',
      serviceTierUnrecorded: '未记录', serviceTierUnspecified: '未指定', serviceTierNotReturned: '未返回',
    })
    expect(en.admin.usage).toMatchObject({
      serviceTier: 'Service tier', serviceTierRequest: 'Request', serviceTierResponse: 'Response',
      serviceTierUnrecorded: 'Not recorded', serviceTierUnspecified: 'Unspecified', serviceTierNotReturned: 'Not returned',
    })
  })
})
