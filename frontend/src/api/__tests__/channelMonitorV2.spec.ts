import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient } from '../client'
import { getMatrix, getModelCatalog, repeatedArrayParamsSerializer, updateConfig, type MonitorConfig } from '../channelMonitorV2'

afterEach(() => vi.restoreAllMocks())

describe('channel monitor V2 query serialization', () => {
  it('uses repeated keys without bracket suffixes for array filters', () => {
    const query = repeatedArrayParamsSerializer({
      range: '90m',
      platform: ['openai', 'grok'],
      group_id: [1, 2],
      model: undefined,
      group_by: 'platform_group_model',
    })

    expect(query).toBe('range=90m&platform=openai&platform=grok&group_id=1&group_id=2&group_by=platform_group_model')
    expect(query).not.toContain('%5B%5D')
  })

  it('sends the matrix grouping with the shared filters', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      data: { coverage: {}, group_by: 'platform_group', items: [] },
    })

    await getMatrix({ range: '24h', platforms: ['openai'], groupIds: [7], models: [] }, 'platform_group', true)

    expect(get).toHaveBeenCalledWith('/admin/channel-monitor-v2/matrix', expect.objectContaining({
      params: {
        range: '24h',
        platform: ['openai'],
        group_id: [7],
        model: undefined,
        group_by: 'platform_group',
      },
    }))
  })

  it('requests the uncollapsed admin catalog with the chosen platform, groups, and cancellation signal', async () => {
    const catalog = { config_version: 9, range: '30d', start: '', end: '', items: [] }
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({ data: catalog })
    const controller = new AbortController()
    expect(await getModelCatalog('custom-platform', [7, 42], controller.signal)).toEqual(catalog)
    expect(get).toHaveBeenCalledWith('/admin/channel-monitor-v2/model-catalog', expect.objectContaining({
      params: { range: '30d', platform: ['custom-platform'], group_id: [7, 42] },
      signal: controller.signal,
    }))
    const config = get.mock.calls[0]![1]!
    expect((config.paramsSerializer as { serialize: typeof repeatedArrayParamsSerializer }).serialize(config.params)).toBe('range=30d&platform=custom-platform&group_id=7&group_id=42')
  })

  it('sends the exact full versioned config including explicit empty selected mode and exclusions', async () => {
    const config: MonitorConfig = {
      version: 9, enabled: true, refresh_interval_seconds: 300,
      platforms: [{ platform: 'custom', enabled: false, models: [], model_display_mode: 'selected', excluded_models: ['Alpha', 'alpha'] }],
      group_ids: [42], ignored_error_categories: [],
      health_thresholds: { minimum_sample: 50, warning_error_rate: .05, critical_error_rate: .2, target_ttft_ms: 3000, warning_ttft_ms: 3000, critical_ttft_ms: 10000, warning_cache_rate: .85, critical_cache_rate: .6, error_weight: .6, ttft_weight: .2, cache_weight: .2 },
    }
    const saved = { ...config, version: 10 }
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: saved })
    expect(await updateConfig(config)).toEqual(saved)
    expect(put).toHaveBeenCalledWith('/admin/channel-monitor-v2/config', config)
  })
})
