import { describe, expect, it } from 'vitest'
import type { MonitorModelCatalogItem, MonitorPlatformConfig } from '@/api/channelMonitorV2'
import { applyModelAction, displayMode, modelCategories, normalizePlatform, parseModelIds } from '../monitorModelSettings'

const platform = (): MonitorPlatformConfig => ({ platform: 'custom', enabled: true, models: ['Alpha'], model_display_mode: 'selected', excluded_models: ['hidden'] })
const observed = (model: string, sources: MonitorModelCatalogItem['sources'] = ['success']): MonitorModelCatalogItem => ({ platform: 'custom', model, success_requests: 1, error_requests: 0, sources })

describe('monitor model rules', () => {
  it('trims and deduplicates opaque IDs without collapsing case or allowing the derived Other sentinel', () => {
    expect(parseModelIds('gpt-render\nNOVA\r\nNOVA')).toEqual(['NOVA', 'gpt-render'])
    expect(parseModelIds(' Alpha,Alpha，alpha\n  beta  \r\n__other__\n')).toEqual(['Alpha', 'alpha', 'beta'])
  })

  it('uses actual success and error observations for Other, never a saved zero-traffic candidate difference', () => {
    const catalog = [observed('Alpha'), observed('alpha', ['error']), observed('hidden'), observed('zero-traffic', ['configured']), { ...observed('foreign'), platform: 'elsewhere' }]
    expect(modelCategories(platform(), catalog)).toEqual({ named: ['Alpha'], other: ['alpha'], excluded: ['hidden'] })
    expect(modelCategories({ ...platform(), model_display_mode: 'all' }, catalog)).toEqual({ named: ['Alpha', 'alpha'], other: [], excluded: ['hidden'] })
  })

  it('keeps an explicit empty selected list as all traffic in Other after moving the final named model', () => {
    const moved = applyModelAction(platform(), 'other', ['Alpha'])
    expect(moved).toEqual({ platform: 'custom', enabled: true, models: [], model_display_mode: 'selected', excluded_models: ['hidden'] })
    expect(modelCategories(moved, [observed('Alpha')])).toEqual({ named: [], other: ['Alpha'], excluded: ['hidden'] })
    expect(displayMode({ platform: 'legacy', enabled: true, models: [] })).toBe('all')
    expect(displayMode({ platform: 'legacy', enabled: true, models: ['Alpha'] })).toBe('selected')
  })

  it('gives exclusion priority and restores according to the current mode', () => {
    expect(normalizePlatform({ ...platform(), models: ['hidden', 'Alpha', 'Alpha'] }).models).toEqual(['Alpha'])
    const excluded = applyModelAction(platform(), 'exclude', ['Alpha'])
    expect(modelCategories(excluded, [observed('Alpha'), observed('hidden')])).toEqual({ named: [], other: [], excluded: ['Alpha', 'hidden'] })
    const restored = applyModelAction(excluded, 'restore', ['Alpha'])
    expect(modelCategories(restored, [observed('Alpha')])).toEqual({ named: [], other: ['Alpha'], excluded: ['hidden'] })
    expect(modelCategories({ ...restored, model_display_mode: 'all' }, [observed('Alpha')]).named).toEqual(['Alpha'])
    const named = applyModelAction(excluded, 'name', ['Alpha'])
    expect(named.models).toEqual(['Alpha'])
    expect(named.excluded_models).toEqual(['hidden'])
  })
})
