import type { MonitorModelCatalogItem, MonitorPlatformConfig } from '@/api/channelMonitorV2'

export type ModelCategory = 'named' | 'other' | 'excluded'
export type ModelAction = 'name' | 'other' | 'exclude' | 'restore'

/** Model IDs are opaque and case-sensitive; only surrounding whitespace is removed. */
export function normalizeModelIds(values: string[]): string[] {
  return [...new Set(values.map(value => value.trim()).filter(value => value && value !== '__other__'))].sort()
}

export function parseModelIds(value: string): string[] {
  return normalizeModelIds(value.split(/[,，\r\n]+/))
}

export function displayMode(platform: MonitorPlatformConfig): 'all' | 'selected' {
  return platform.model_display_mode ?? (platform.models.length ? 'selected' : 'all')
}

export function normalizePlatform(platform: MonitorPlatformConfig): MonitorPlatformConfig {
  const excluded = normalizeModelIds(platform.excluded_models ?? [])
  return {
    ...platform,
    model_display_mode: displayMode(platform),
    models: normalizeModelIds(platform.models).filter(model => !excluded.includes(model)),
    excluded_models: excluded,
  }
}

export function modelCategories(platform: MonitorPlatformConfig, catalog: MonitorModelCatalogItem[]) {
  const excluded = normalizeModelIds(platform.excluded_models ?? [])
  const observed = normalizeModelIds(catalog.filter(item => item.platform === platform.platform &&
    (item.sources.includes('success') || item.sources.includes('error'))).map(item => item.model))
  const named = normalizeModelIds(platform.models).filter(model => !excluded.includes(model))
  return {
    named: displayMode(platform) === 'all'
      ? normalizeModelIds([...observed, ...named]).filter(model => !excluded.includes(model))
      : named,
    other: displayMode(platform) === 'selected'
      ? observed.filter(model => !named.includes(model) && !excluded.includes(model))
      : [],
    excluded,
  }
}

export function applyModelAction(platform: MonitorPlatformConfig, action: ModelAction, ids: string[]): MonitorPlatformConfig {
  const next = normalizePlatform(platform)
  const values = normalizeModelIds(ids)
  if (action === 'exclude') {
    next.excluded_models = normalizeModelIds([...(next.excluded_models ?? []), ...values])
    next.models = next.models.filter(model => !values.includes(model))
  } else if (action === 'name') {
    next.model_display_mode = 'selected'
    next.models = normalizeModelIds([...next.models, ...values])
    next.excluded_models = next.excluded_models?.filter(model => !values.includes(model))
  } else if (action === 'other') {
    next.model_display_mode = 'selected'
    next.models = next.models.filter(model => !values.includes(model))
  } else {
    // Restore follows the current display rule: all -> individual, selected -> Other.
    next.excluded_models = next.excluded_models?.filter(model => !values.includes(model))
  }
  return next
}
