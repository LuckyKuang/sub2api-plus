<template>
  <section class="card workspace-shell monitor-platform-manager" :aria-label="t('channelMonitorV2.settings.platformsTitle')">
    <header class="manager-heading">
      <div>
        <h3 class="text-sm font-semibold">{{ t('channelMonitorV2.settings.platformsTitle') }}</h3>
        <p class="mt-1 text-xs surface-muted">{{ t('channelMonitorV2.settings.platformsHint') }}</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <span class="badge badge-primary">{{ t('channelMonitorV2.settings.platformsEnabled', { enabled: modelValue.filter(item => item.enabled).length, total: modelValue.length }) }}</span>
        <span class="badge badge-gray">{{ t('channelMonitorV2.settings.excludedCount', { count: excludedCount }) }}</span>
      </div>
    </header>
    <div class="manager-workspace">
      <aside class="manager-rail">
        <div class="mb-3 flex items-center justify-between gap-2">
          <strong class="text-xs">{{ t('channelMonitorV2.settings.platformList') }}</strong>
          <button v-if="availablePlatforms.length" type="button" class="btn btn-ghost btn-sm" :disabled="disabled" :aria-expanded="addingPlatform" :aria-label="t('channelMonitorV2.settings.addPlatform')" data-testid="open-add-platform" @click="addingPlatform = !addingPlatform"><Icon name="plus" size="sm" /></button>
        </div>
        <div v-if="addingPlatform && availablePlatforms.length" class="mb-3 space-y-2 rounded-xl border border-gray-200 p-2 dark:border-dark-600">
          <Select v-model="newPlatform" :searchable="true" :options="availablePlatforms" :disabled="disabled" :aria-label="t('channelMonitorV2.settings.selectPlatform')" :placeholder="t('channelMonitorV2.settings.selectPlatform')" />
          <button type="button" class="btn btn-secondary btn-sm w-full" :disabled="disabled || !newPlatform" data-testid="add-platform" @click="addPlatform">{{ t('channelMonitorV2.settings.addPlatform') }}</button>
        </div>
        <input v-model="platformQuery" class="input" type="search" :placeholder="t('channelMonitorV2.settings.searchPlatforms')" :aria-label="t('channelMonitorV2.settings.searchPlatforms')" />
        <div class="manager-platform-list mt-3" role="group" :aria-label="t('channelMonitorV2.settings.platformList')">
          <button v-for="platform in filteredPlatforms" :key="platform.platform" type="button" class="manager-platform" :class="{ active: activeId === platform.platform }" :aria-pressed="activeId === platform.platform" :data-platform="platform.platform" @click="activeId = platform.platform">
            <span class="manager-avatar">{{ platformLabel(platform.platform).slice(0, 2).toUpperCase() }}</span>
            <span class="min-w-0 flex-1 text-left">
              <strong class="block truncate text-sm">{{ platformLabel(platform.platform) }}</strong>
              <small class="mt-1 flex items-center gap-1.5 surface-muted"><span class="manager-dot console-data-mark" :class="platform.enabled ? 'bg-emerald-500' : 'bg-gray-400'" />{{ t(platform.enabled ? 'channelMonitorV2.settings.platformEnabled' : 'channelMonitorV2.settings.platformPaused') }}</small>
            </span>
            <span class="text-xs surface-muted">{{ displayMode(platform) === 'all' ? t('channelMonitorV2.settings.allShort') : platform.models.length }}</span>
          </button>
          <p v-if="!filteredPlatforms.length" class="p-3 text-xs surface-muted">{{ t('channelMonitorV2.settings.noPlatforms') }}</p>
        </div>
      </aside>
      <section v-if="active" class="manager-main" :aria-label="platformLabel(active.platform)">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h4 class="flex flex-wrap items-center gap-2 text-lg font-semibold">{{ platformLabel(active.platform) }} <span class="badge badge-gray text-xs font-normal">{{ active.platform }}</span></h4>
            <p class="mt-1 text-xs surface-muted">{{ t(displayMode(active) === 'all' ? 'channelMonitorV2.settings.allModelsSummary' : 'channelMonitorV2.settings.selectedModelsSummary', { count: active.models.length }) }}</p>
          </div>
          <div class="flex items-center gap-3">
            <Toggle :model-value="active.enabled" :disabled="disabled" :aria-label="t('channelMonitorV2.settings.enablePlatform', { platform: platformLabel(active.platform) })" @update:model-value="updateActive({ ...active, enabled: $event })" />
            <button type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" :disabled="disabled" data-testid="remove-platform" @click="removePlatform">{{ t('channelMonitorV2.settings.removePlatform') }}</button>
          </div>
        </div>
        <div class="tabs mt-5 w-fit max-w-full" role="group" :aria-label="t('channelMonitorV2.settings.displayMode')">
          <button v-for="mode in ['all', 'selected'] as const" :key="mode" type="button" class="tab" :class="displayMode(active) === mode && 'tab-active'" :aria-pressed="displayMode(active) === mode" :disabled="disabled" :data-mode="mode" @click="setMode(mode)">{{ t(mode === 'all' ? 'channelMonitorV2.settings.badgeAllModels' : 'channelMonitorV2.settings.selectedAndOther') }}</button>
        </div>
        <p class="mt-3 text-xs leading-relaxed surface-muted">{{ t(displayMode(active) === 'all' ? 'channelMonitorV2.settings.allRule' : 'channelMonitorV2.settings.selectedRule') }}</p>
        <div class="manager-category-tabs mt-5" role="group" :aria-label="t('channelMonitorV2.settings.modelCategories')">
          <button v-for="category in categories" :key="category" type="button" :aria-pressed="tab === category" :data-category="category" @click="tab = category">
            {{ categoryLabel(category) }} <span class="badge badge-gray">{{ lists[category].length }}</span>
          </button>
        </div>
        <p v-if="tab !== 'named'" class="manager-note" :class="{ excluded: tab === 'excluded' }">{{ t(tab === 'excluded' ? 'channelMonitorV2.settings.excludedHint' : 'channelMonitorV2.settings.otherHint') }}</p>
        <div v-if="catalogError" class="manager-note excluded flex flex-wrap items-center justify-between gap-2" role="alert">
          <span>{{ t('channelMonitorV2.settings.catalogFailed') }} {{ catalogError }}</span>
          <button type="button" class="btn btn-ghost btn-sm" @click="loadCatalog">{{ t('channelMonitorV2.settings.retry') }}</button>
        </div>
        <div class="mt-4 flex items-center gap-2">
          <input v-model="query" class="input min-w-0 flex-1" type="search" :placeholder="t('channelMonitorV2.settings.searchModels')" :aria-label="t('channelMonitorV2.settings.searchModels')" />
          <button type="button" class="btn btn-primary shrink-0" :disabled="disabled || (displayMode(active) === 'all' && tab !== 'excluded')" data-testid="open-model-picker" @click="openPicker"><Icon name="plus" size="sm" />{{ t(tab === 'excluded' ? 'channelMonitorV2.settings.addExclusion' : 'channelMonitorV2.settings.addModels') }}</button>
        </div>
        <div v-if="selection.length" class="manager-batch mt-3" role="group" :aria-label="t('channelMonitorV2.settings.batchActions')">
          <span class="text-xs">{{ t('channelMonitorV2.settings.selectedCount', { count: selection.length }) }}</span>
          <button v-if="tab === 'named' && displayMode(active) === 'selected'" type="button" class="btn btn-secondary btn-sm" :disabled="disabled" data-batch="other" @click="act('other', selection)">{{ t('channelMonitorV2.settings.moveToOther') }}</button>
          <button v-if="tab === 'other'" type="button" class="btn btn-secondary btn-sm" :disabled="disabled" data-batch="name" @click="act('name', selection)">{{ t('channelMonitorV2.settings.showIndividually') }}</button>
          <button v-if="tab !== 'excluded'" type="button" class="btn btn-secondary btn-sm text-red-600" :disabled="disabled" data-batch="exclude" @click="act('exclude', selection)">{{ t('channelMonitorV2.settings.excludeModels') }}</button>
          <button v-else type="button" class="btn btn-secondary btn-sm" :disabled="disabled" data-batch="restore" @click="act('restore', selection)">{{ t('channelMonitorV2.settings.restoreModels') }}</button>
          <button type="button" class="btn btn-ghost btn-sm" @click="selection = []">{{ t('channelMonitorV2.settings.clearSelection') }}</button>
        </div>
        <div class="my-3 flex flex-wrap items-center justify-between gap-2 text-xs surface-muted">
          <label class="flex cursor-pointer items-center gap-2"><input type="checkbox" class="rounded" :checked="allVisibleSelected" :disabled="disabled || !visibleModels.length" data-testid="select-visible-models" @change="selectVisible(($event.target as HTMLInputElement).checked)" />{{ t('channelMonitorV2.settings.selectResults') }}</label>
          <span>{{ catalogLoading ? t('channelMonitorV2.settings.loading') : t('channelMonitorV2.settings.resultCount', { count: visibleModels.length }) }}</span>
        </div>
        <div class="manager-model-container">
          <div class="manager-models" role="group" :aria-label="categoryLabel(tab)" :aria-busy="catalogLoading">
            <div v-for="model in visibleModels" :key="model" class="manager-model" :class="{ selected: selection.includes(model) }" :data-model="model">
              <input type="checkbox" class="rounded" :checked="selection.includes(model)" :disabled="disabled" :aria-label="t('channelMonitorV2.settings.selectModel', { model })" @change="toggleModel(model)" />
              <span class="min-w-0 flex-1 break-all text-sm font-medium">{{ model }}</span>
              <div class="flex shrink-0 gap-1">
                <button v-if="tab === 'other'" type="button" class="manager-icon" :disabled="disabled" :aria-label="t('channelMonitorV2.settings.showModel', { model })" :title="t('channelMonitorV2.settings.modelActionTooltips.name')" data-action="name" @click="act('name', [model])"><Icon name="plus" size="sm" /></button>
                <button v-if="tab === 'named' && displayMode(active) === 'selected'" type="button" class="manager-icon" :disabled="disabled" :aria-label="t('channelMonitorV2.settings.moveModel', { model })" :title="t('channelMonitorV2.settings.modelActionTooltips.other')" data-action="other" @click="act('other', [model])"><Icon name="arrowRight" size="sm" /></button>
                <button v-if="tab !== 'excluded'" type="button" class="manager-icon text-red-600 dark:text-red-400" :disabled="disabled" :aria-label="t('channelMonitorV2.settings.excludeModel', { model })" :title="t('channelMonitorV2.settings.modelActionTooltips.exclude')" data-action="exclude" @click="act('exclude', [model])"><Icon name="eyeOff" size="sm" /></button>
                <button v-else type="button" class="btn btn-ghost btn-sm" :disabled="disabled" :title="t('channelMonitorV2.settings.modelActionTooltips.restore')" data-action="restore" @click="act('restore', [model])">{{ t('channelMonitorV2.settings.restoreModels') }}</button>
              </div>
            </div>
            <p v-if="!visibleModels.length" class="manager-empty text-sm surface-muted">{{ t(catalogLoading ? 'channelMonitorV2.settings.loading' : catalogError && tab !== 'excluded' ? 'channelMonitorV2.settings.catalogUnavailable' : query ? 'channelMonitorV2.settings.noModelResults' : tab === 'other' ? 'channelMonitorV2.settings.noOtherModels' : tab === 'excluded' ? 'channelMonitorV2.settings.noExcludedModels' : 'channelMonitorV2.settings.noNamedModels') }}</p>
          </div>
        </div>
        <p class="mt-3 text-xs surface-muted">{{ t('channelMonitorV2.settings.modelActionHint') }}</p>
      </section>
      <div v-else class="manager-empty text-sm surface-muted">{{ t('channelMonitorV2.settings.noPlatforms') }}</div>
    </div>
    <div v-if="undoSnapshot" class="manager-undo" role="status">
      <span>{{ t('channelMonitorV2.settings.platformChangesPending') }}</span>
      <button type="button" class="btn btn-ghost btn-sm" :disabled="disabled" data-testid="undo-model-change" @click="undo">{{ t('channelMonitorV2.settings.undo') }}</button>
    </div>
    <MonitorModelPicker :open="pickerOpen" :anchor="pickerAnchor" :title="t(pickerExclude ? 'channelMonitorV2.settings.addExclusion' : 'channelMonitorV2.settings.addModels')" :candidates="candidates" :unavailable="pickerUnavailable" @close="pickerOpen = false" @apply="applyPicker" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getModelCatalog, type MonitorModelCatalogItem, type MonitorPlatformConfig } from '@/api/channelMonitorV2'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { platformLabel as getPlatformLabel } from '@/utils/platformColors'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import MonitorModelPicker from './MonitorModelPicker.vue'
import { applyModelAction, displayMode, modelCategories, normalizeModelIds, normalizePlatform, type ModelAction, type ModelCategory } from './monitorModelSettings'

const props = defineProps<{ modelValue: MonitorPlatformConfig[]; groupIds: number[]; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: MonitorPlatformConfig[]] }>()
const { t } = useI18n()
const activeId = ref(props.modelValue[0]?.platform ?? '')
const active = computed(() => props.modelValue.find(item => item.platform === activeId.value))
const platformLabel = getPlatformLabel
const platformQuery = ref('')
const query = ref('')
const tab = ref<ModelCategory>('named')
const categories: ModelCategory[] = ['named', 'other', 'excluded']
const selection = ref<string[]>([])
const addingPlatform = ref(false)
const newPlatform = ref('')
const catalog = ref<MonitorModelCatalogItem[]>([])
const catalogLoading = ref(false)
const catalogError = ref('')
const undoSnapshot = ref<MonitorPlatformConfig[] | null>(null)
const pickerOpen = ref(false)
const pickerExclude = ref(false)
const pickerAnchor = ref<HTMLElement | null>(null)
let catalogRevision = 0
let controller: AbortController | undefined
const availablePlatforms = computed(() => CONCRETE_PLATFORM_OPTIONS.filter(option => !props.modelValue.some(item => item.platform === option.value)))
const filteredPlatforms = computed(() => props.modelValue.filter(item => `${platformLabel(item.platform)} ${item.platform}`.toLowerCase().includes(platformQuery.value.trim().toLowerCase())))
const excludedCount = computed(() => props.modelValue.reduce((sum, platform) => sum + (platform.excluded_models?.length ?? 0), 0))
const lists = computed(() => active.value ? modelCategories(active.value, catalog.value) : { named: [], other: [], excluded: [] })
const visibleModels = computed(() => lists.value[tab.value].filter(model => model.toLowerCase().includes(query.value.trim().toLowerCase())))
const allVisibleSelected = computed(() => visibleModels.value.length > 0 && visibleModels.value.every(model => selection.value.includes(model)))
// Only platforms with an explicit existing preset are eligible; the generic helper falls back to Claude for unknown IDs.
const presetPlatforms = new Set(['openai', 'anthropic', 'claude', 'gemini', 'antigravity', 'zhipu', 'qwen', 'deepseek', 'mistral', 'meta', 'xai', 'grok', 'cohere', 'yi', 'moonshot', 'kimi', 'opencode_go', 'typesafe', 'doubao', 'stepfun', 'minimax', 'baidu', 'spark', 'hunyuan', 'perplexity'])
const candidates = computed(() => {
  const platform = active.value
  if (!platform) return []
  const entries = catalog.value.filter(item => item.platform === platform.platform)
  const observed = entries.filter(item => item.sources.includes('success') || item.sources.includes('error')).map(item => item.model)
  const known = entries.map(item => item.model)
  const defaults = presetPlatforms.has(platform.platform) ? getModelsByPlatform(platform.platform) : []
  return normalizeModelIds([...known, ...defaults, ...platform.models, ...(platform.excluded_models ?? [])]).map(model => ({
    model,
    source: observed.includes(model) ? 'observed' as const : platform.excluded_models?.includes(model) ? 'excluded' as const : platform.models.includes(model) || known.includes(model) ? 'configured' as const : 'preset' as const,
  }))
})
const pickerUnavailable = computed(() => !active.value ? [] : pickerExclude.value ? active.value.excluded_models ?? [] : active.value.models)
function categoryLabel(category: ModelCategory) {
  return t(category === 'named' ? displayMode(active.value!) === 'all' ? 'channelMonitorV2.settings.knownModels' : 'channelMonitorV2.settings.individualModels' : category === 'other' ? 'channelMonitorV2.otherModels' : 'channelMonitorV2.settings.excludedModels')
}
function clonePlatforms(value: MonitorPlatformConfig[]) { return value.map(platform => ({ ...platform, models: [...platform.models], excluded_models: [...(platform.excluded_models ?? [])] })) }
function commit(value: MonitorPlatformConfig[]) {
  if (props.disabled) return
  undoSnapshot.value = clonePlatforms(props.modelValue)
  emit('update:modelValue', value)
  selection.value = []
}
function updateActive(value: MonitorPlatformConfig) { commit(props.modelValue.map(platform => platform.platform === value.platform ? value : platform)) }
function setMode(mode: 'all' | 'selected') { if (active.value && displayMode(active.value) !== mode) updateActive({ ...active.value, model_display_mode: mode }) }
function addPlatform() {
  if (!availablePlatforms.value.some(option => option.value === newPlatform.value)) return
  const id = newPlatform.value
  commit([...props.modelValue, normalizePlatform({ platform: id, enabled: true, models: [], model_display_mode: 'all' })])
  activeId.value = id; newPlatform.value = ''; addingPlatform.value = false; platformQuery.value = ''
}
function removePlatform() { if (active.value) commit(props.modelValue.filter(item => item.platform !== activeId.value)) }
function act(action: ModelAction, models: string[]) { if (active.value) updateActive(applyModelAction(active.value, action, models)) }
function undo() { if (undoSnapshot.value && !props.disabled) { emit('update:modelValue', clonePlatforms(undoSnapshot.value)); undoSnapshot.value = null; selection.value = [] } }
function toggleModel(model: string) { selection.value = selection.value.includes(model) ? selection.value.filter(value => value !== model) : [...selection.value, model] }
function selectVisible(checked: boolean) { selection.value = checked ? normalizeModelIds([...selection.value, ...visibleModels.value]) : selection.value.filter(model => !visibleModels.value.includes(model)) }
function openPicker(event: MouseEvent) { pickerAnchor.value = event.currentTarget as HTMLElement; pickerExclude.value = tab.value === 'excluded'; pickerOpen.value = true }
function applyPicker(models: string[]) { act(pickerExclude.value ? 'exclude' : 'name', models); tab.value = pickerExclude.value ? 'excluded' : 'named' }
async function loadCatalog() {
  const revision = ++catalogRevision
  controller?.abort()
  selection.value = []
  pickerOpen.value = false
  catalog.value = []; catalogError.value = ''
  if (!activeId.value) { catalogLoading.value = false; return }
  controller = new AbortController()
  catalogLoading.value = true
  try {
    const response = await getModelCatalog(activeId.value, [...props.groupIds], controller.signal)
    if (revision === catalogRevision) catalog.value = response.items
  } catch (error) {
    if (revision === catalogRevision) catalogError.value = extractApiErrorMessage(error, t('channelMonitorV2.settings.catalogUnavailable'))
  } finally {
    if (revision === catalogRevision) catalogLoading.value = false
  }
}
watch(() => props.modelValue.map(item => item.platform), ids => { if (!ids.includes(activeId.value)) activeId.value = ids[0] ?? '' })
watch([activeId, () => props.groupIds.join(',')], loadCatalog, { immediate: true })
watch(activeId, () => { tab.value = 'named'; query.value = ''; selection.value = []; pickerOpen.value = false })
watch(tab, () => { query.value = ''; selection.value = [] })
watch(() => props.disabled, disabled => { if (disabled) pickerOpen.value = false })
onBeforeUnmount(() => { ++catalogRevision; controller?.abort() })
</script>

<style scoped>
.monitor-platform-manager { overflow: visible; }
.manager-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 14px; padding: 20px 22px; }
.manager-workspace { display: grid; grid-template-columns: 230px minmax(0, 1fr); }
.manager-rail { min-width: 0; padding: 18px 14px; border-right: 1px solid rgb(148 163 184 / 18%); }
.manager-platform-list { display: flex; max-height: 540px; flex-direction: column; gap: 7px; overflow-y: auto; }
.manager-platform { display: flex; width: 100%; align-items: center; gap: 10px; border: 1px solid transparent; border-radius: 13px; padding: 12px 10px; transition: background .15s; }
.manager-platform:hover, .manager-platform.active { background: rgb(var(--console-accent-500, 20 184 166) / 9%); }
.manager-platform.active { border-color: rgb(var(--console-accent-500, 20 184 166) / 32%); }
.manager-avatar { display: grid; width: 32px; height: 32px; flex-shrink: 0; place-items: center; border-radius: 10px; background: rgb(148 163 184 / 12%); font-size: 11px; font-weight: 700; }
.manager-dot { width: 6px; height: 6px; border-radius: 50%; }
.manager-main { min-width: 0; padding: 22px; }
.manager-category-tabs { display: flex; flex-wrap: wrap; gap: 7px; }
.manager-category-tabs button { display: flex; align-items: center; gap: 6px; border-radius: 10px; padding: 8px 10px; font-size: 13px; }
.manager-category-tabs button:hover, .manager-category-tabs button[aria-pressed='true'] { background: rgb(var(--console-accent-500, 20 184 166) / 10%); color: rgb(var(--console-accent-600, 13 148 136)); }
.manager-note { margin-top: 12px; border: 1px solid rgb(var(--console-accent-500, 20 184 166) / 18%); border-radius: 12px; padding: 10px 12px; background: rgb(var(--console-accent-500, 20 184 166) / 6%); font-size: 12px; line-height: 1.7; }
.manager-note.excluded { border-color: rgb(239 68 68 / 18%); background: rgb(239 68 68 / 5%); }
.manager-batch { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; border-radius: 12px; padding: 10px; background: rgb(var(--console-accent-500, 20 184 166) / 8%); }
.manager-model-container { container: v2models / inline-size; }
.manager-models { display: grid; grid-template-columns: minmax(0, 1fr); gap: 8px; max-height: 390px; overflow-y: auto; padding: 1px; }
.manager-model { display: flex; min-width: 0; align-items: center; gap: 12px; border: 1px solid rgb(148 163 184 / 19%); border-radius: 12px; padding: 12px; }
.manager-model:hover, .manager-model.selected { background: rgb(var(--console-accent-500, 20 184 166) / 5%); border-color: rgb(var(--console-accent-500, 20 184 166) / 28%); }
.manager-icon { display: grid; width: 30px; height: 30px; place-items: center; border-radius: 9px; }
.manager-icon:hover { background: rgb(148 163 184 / 12%); }
.manager-empty { grid-column: 1 / -1; padding: 42px 20px; text-align: center; line-height: 1.8; }
.manager-undo { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 22px; font-size: 12px; }
@container v2models (min-width: 940px) { .manager-models { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 1000px) { .manager-workspace { grid-template-columns: 190px minmax(0, 1fr); } }
@media (max-width: 700px) {
  .manager-workspace { grid-template-columns: minmax(0, 1fr); }
  .manager-rail { border-right: 0; border-bottom: 1px solid rgb(148 163 184 / 18%); }
  .manager-platform-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); max-height: 230px; }
  .manager-platform { gap: 7px; padding: 10px 7px; }
  .manager-main, .manager-heading { padding: 16px; }
  .manager-models { max-height: 450px; }
}
</style>
