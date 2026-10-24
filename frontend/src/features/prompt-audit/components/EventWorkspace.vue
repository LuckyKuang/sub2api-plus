<template>
  <section aria-labelledby="prompt-events-title" class="event-workspace workspace-body workspace-clip pt-4">
    <div class="event-heading workspace-fixed flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h2 id="prompt-events-title" class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.events.title') }}</h2>
        <p class="mt-1 text-sm surface-muted">{{ t('admin.promptAudit.events.description') }}</p>
      </div>
      <div class="event-actions flex flex-wrap gap-2">
        <button type="submit" :form="filterFormId" class="btn btn-primary btn-sm" data-test="event-search">{{ t('common.search') }}</button>
        <button type="button" class="btn btn-ghost btn-sm" data-test="event-reset" @click="resetFilters">{{ t('common.reset') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="selectedIds.length === 0" @click="$emit('batch-delete')">
          {{ t('admin.promptAudit.events.deleteSelected', { count: selectedIds.length }) }}
        </button>
        <button type="button" class="btn btn-danger btn-sm" data-test="filter-delete" @click="$emit('preview-delete')">
          {{ t('admin.promptAudit.events.deleteByFilter') }}
        </button>
      </div>
    </div>

    <form :id="filterFormId" class="event-filters workspace-fixed grid gap-3 sm:grid-cols-2 lg:grid-cols-6" @submit.prevent="applyFilters">
      <label class="text-xs surface-muted">
        <span>{{ t('admin.promptAudit.events.decision') }}</span>
        <Select
          v-model="localFilters.decision"
          class="mt-1 w-full"
          :aria-label="t('admin.promptAudit.events.decision')"
          @change="filtersChanged"
          :searchable="false"
          :options="[{ value: '', label: t('common.all') }, { value: 'pass', label: t('admin.promptAudit.decisions.pass') }, { value: 'flag', label: t('admin.promptAudit.decisions.flag') }, { value: 'critical', label: t('admin.promptAudit.decisions.critical') }]"
        />
      </label>
      <label class="text-xs surface-muted">
        <span>{{ t('admin.promptAudit.events.risk') }}</span>
        <Select
          v-model="localFilters.risk_level"
          class="mt-1 w-full"
          :aria-label="t('admin.promptAudit.events.risk')"
          @change="filtersChanged"
          :searchable="false"
          :options="[{ value: '', label: t('common.all') }, { value: 'low', label: t('admin.promptAudit.riskLevels.low') }, { value: 'medium', label: t('admin.promptAudit.riskLevels.medium') }, { value: 'high', label: t('admin.promptAudit.riskLevels.high') }, { value: 'critical', label: t('admin.promptAudit.riskLevels.critical') }]"
        />
      </label>
      <FilterInput v-model="localFilters.endpoint" :label="t('admin.promptAudit.events.endpoint')" @change="filtersChanged" />
      <FilterInput v-model="localFilters.client_ip" :label="t('admin.promptAudit.events.clientIp')" @change="filtersChanged" />
      <FilterInput v-model="localFilters.group_id" :label="t('admin.promptAudit.events.groupId')" type="number" @change="filtersChanged" />
      <FilterInput v-model="localFilters.user_id" :label="t('admin.promptAudit.events.userId')" type="number" @change="filtersChanged" />
      <FilterInput v-model="localFilters.api_key_id" :label="t('admin.promptAudit.events.apiKeyId')" type="number" @change="filtersChanged" />
      <FilterInput v-model="localFilters.request_id" :label="t('admin.promptAudit.events.requestId')" @change="filtersChanged" />
      <FilterInput v-model="localFilters.prompt_hash" :label="t('admin.promptAudit.events.promptHash')" @change="filtersChanged" />
      <FilterInput v-model="localFilters.keyword" :label="t('admin.promptAudit.events.keyword')" @change="filtersChanged" />
      <label class="text-xs surface-muted">
        <span>{{ t('admin.promptAudit.events.startAt') }}</span>
        <DateTimePicker v-model="localFilters.start_at" type="datetime-local" class="input mt-1 w-full" :aria-label="t('admin.promptAudit.events.startAt')" @change="filtersChanged" />
      </label>
      <label class="text-xs surface-muted">
        <span>{{ t('admin.promptAudit.events.endAt') }}</span>
        <DateTimePicker v-model="localFilters.end_at" type="datetime-local" class="input mt-1 w-full" :aria-label="t('admin.promptAudit.events.endAt')" @change="filtersChanged" />
      </label>
    </form>
    <div v-if="error" role="alert" class="workspace-fixed rounded-lg bg-red-50/[0.15] px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
    <div class="record-table-scroll workspace-table-viewport overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60" data-test="event-table-scroll">
      <table class="record-table min-w-[1420px] w-full text-left text-sm">
        <thead class="surface-inset text-xs uppercase tracking-wide surface-muted">
          <tr>
            <th class="w-10 px-3 py-3"><input type="checkbox" :checked="allSelected" :aria-label="t('admin.promptAudit.events.selectAll')" @change="toggleAll" /></th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.time') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.clientIp') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.identity') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.group') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.route') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.result') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.durations') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.preview') }}</th>
            <th class="px-3 py-3 text-right font-medium">{{ t('admin.promptAudit.common.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-if="loading"><td colspan="10" class="px-4 py-12 text-center surface-muted" aria-busy="true">{{ t('common.loading') }}</td></tr>
          <tr v-else-if="events.length === 0"><td colspan="10" class="px-4 py-12 text-center surface-muted">{{ t('admin.promptAudit.events.empty') }}</td></tr>
          <tr v-for="event in events" v-else :key="event.id" :data-test="`event-${event.id}`" class="align-top hover:surface-hover">
            <td class="px-3 py-3"><input type="checkbox" :checked="selectedIds.includes(event.id)" :aria-label="t('admin.promptAudit.events.selectEvent', { id: event.id })" @change="toggleOne(event.id)" /></td>
            <td class="whitespace-nowrap px-3 py-3 text-xs surface-muted">{{ formatDate(event.created_at) }}</td>
            <td class="px-3 py-3">
              <button
                v-if="event.snapshot.client_ip"
                type="button"
                class="font-mono text-xs text-primary-600 hover:underline dark:text-primary-300 surface-control"
                :title="t('admin.promptAudit.events.filterByIp', { ip: event.snapshot.client_ip })"
                :aria-label="t('admin.promptAudit.events.filterByIp', { ip: event.snapshot.client_ip })"
                @click="filterByIP(event.snapshot.client_ip)"
              >
                {{ event.snapshot.client_ip }}
              </button>
              <span v-else>—</span>
            </td>
            <td class="px-3 py-3">
              <CopyLine :label="t('admin.promptAudit.events.user')" :value="event.snapshot.username" />
              <CopyLine :label="t('admin.promptAudit.events.email')" :value="event.snapshot.user_email" />
              <CopyLine :label="t('admin.promptAudit.events.apiKey')" :value="event.snapshot.api_key_name" />
            </td>
            <td class="px-3 py-3 surface-muted">{{ event.snapshot.group_name || '—' }}</td>
            <td class="px-3 py-3">
              <p class="font-medium text-gray-900 dark:text-white">{{ event.snapshot.endpoint }}</p>
              <p class="mt-1 text-xs surface-muted">{{ event.snapshot.model }} · {{ event.snapshot.protocol }} · {{ event.snapshot.stage || 'http' }}</p>
            </td>
            <td class="px-3 py-3">
              <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="decisionClass(event.decision)">{{ formatDecisionRisk(event.decision, event.risk_level) }}</span>
              <p class="mt-2 max-w-48 truncate text-xs surface-muted" :title="formatCategories(event.categories)">{{ formatCategories(event.categories) }}</p>
            </td>
            <td class="whitespace-nowrap px-3 py-3 text-xs surface-muted">
              <p>{{ t('admin.promptAudit.events.queueDelay') }} · {{ formatDuration(event.queue_delay_ms) }}</p>
              <p class="mt-1">{{ t('admin.promptAudit.events.auditLatency') }} · {{ formatDuration(event.latency_ms) }}</p>
              <p class="mt-1 surface-muted">{{ formatMode(event.execution_mode) }}</p>
            </td>
            <td class="max-w-xs px-3 py-3"><p class="line-clamp-2 break-words surface-muted">{{ event.snapshot.redacted_preview || '—' }}</p></td>
            <td class="whitespace-nowrap px-3 py-3 text-right">
              <button type="button" class="btn btn-ghost btn-sm" @click="$emit('view', event.id)">{{ t('common.view') }}</button>
              <button type="button" class="btn btn-ghost btn-sm text-red-600" @click="$emit('delete', event.id)">{{ t('common.delete') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination class="event-pagination workspace-fixed" :total="total" :page="page" :page-size="pageSize" @update:page="$emit('page', $event)" @update:page-size="$emit('page-size', $event)" />
  </section>
</template>

<script setup lang="ts">
import DateTimePicker from '@/components/common/DateTimePicker.vue'
import Select from '@/components/common/Select.vue'
import { computed, defineComponent, h, reactive, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from '@/components/common/Pagination.vue'
import type { PromptAuditEvent, PromptEventFilters } from '../types'
import { cloneData, emptyEventFilters, SCANNER_CATALOG } from '../viewModel'

const props = defineProps<{
  events: PromptAuditEvent[]; total: number; page: number; pageSize: number
  filters: PromptEventFilters; selectedIds: number[]; loading: boolean; error: string
}>()
const emit = defineEmits<{
  (event: 'filters-change', value: PromptEventFilters): void
  (event: 'search', value: PromptEventFilters): void
  (event: 'selection', value: number[]): void
  (event: 'page', value: number): void
  (event: 'page-size', value: number): void
  (event: 'view', id: number): void
  (event: 'delete', id: number): void
  (event: 'batch-delete'): void
  (event: 'preview-delete'): void
}>()
const { t, locale } = useI18n()
const filterFormId = `prompt-event-filters-${useId()}`
const localFilters = reactive<PromptEventFilters>(cloneData(props.filters))
watch(() => props.filters, (value) => Object.assign(localFilters, cloneData(value)), { deep: true })
const allSelected = computed(() => props.events.length > 0 && props.events.every((event) => props.selectedIds.includes(event.id)))

const FilterInput = defineComponent({
  props: { modelValue: { type: String, required: true }, label: { type: String, required: true }, type: { type: String, default: 'text' } },
  emits: ['update:modelValue', 'change'],
  setup(componentProps, { emit: componentEmit }) {
    return () => h('label', { class: 'text-xs surface-muted' }, [
      h('span', componentProps.label),
      h('input', {
        value: componentProps.modelValue, type: componentProps.type, class: 'input mt-1 w-full', 'aria-label': componentProps.label,
        onInput: (event: Event) => componentEmit('update:modelValue', (event.target as HTMLInputElement).value),
        onChange: () => componentEmit('change'),
      }),
    ])
  },
})

const CopyLine = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, default: '' } },
  setup(componentProps) {
    return () => h('div', { class: 'flex max-w-56 items-center gap-1 text-xs' }, [
      h('span', { class: 'w-16 flex-none surface-muted' }, componentProps.label),
      h('span', { class: 'min-w-0 flex-1 truncate text-gray-800 dark:text-dark-100' }, componentProps.value || '—'),
      componentProps.value ? h('button', {
        type: 'button', class: 'text-primary-600 hover:underline', 'aria-label': `${t('common.copy')} ${componentProps.label}`,
        onClick: () => navigator.clipboard?.writeText(componentProps.value),
      }, t('common.copy')) : null,
    ])
  },
})

function filtersChanged() {
  emit('filters-change', cloneData(localFilters))
}
function applyFilters() {
  const value = cloneData(localFilters)
  emit('filters-change', value)
  emit('search', value)
}
function resetFilters() {
  Object.assign(localFilters, emptyEventFilters())
  applyFilters()
}
function filterByIP(value: string) {
  localFilters.client_ip = value
  applyFilters()
}
function toggleOne(id: number) {
  const selected = new Set(props.selectedIds)
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  emit('selection', [...selected])
}
function toggleAll() {
  emit('selection', allSelected.value ? [] : props.events.map((event) => event.id))
}
function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}
function formatDuration(value?: number | null): string {
  if (value == null) return '—'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(value < 10000 ? 2 : 1)} s`
}
function formatMode(mode?: string): string {
  if (!mode) return '—'
  const key = `admin.promptAudit.mode.${mode}`
  const label = t(key)
  return label === key ? mode : label
}
function decisionClass(decision: string): string {
  if (decision === 'critical') return 'bg-red-100/[0.15] text-red-700 dark:bg-red-950/50 dark:text-red-300'
  if (decision === 'flag') return 'bg-amber-100/[0.15] text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
  return 'bg-emerald-100/[0.15] text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
}
const DECISIONS = new Set(['pass', 'flag', 'critical'])
const RISK_LEVELS = new Set(['low', 'medium', 'high', 'critical'])

function translateDecision(decision: string): string {
  return DECISIONS.has(decision) ? t(`admin.promptAudit.decisions.${decision}`) : decision
}
function translateRiskLevel(riskLevel: string): string {
  return RISK_LEVELS.has(riskLevel) ? t(`admin.promptAudit.riskLevels.${riskLevel}`) : riskLevel
}
function translateCategory(category: string): string {
  return SCANNER_CATALOG.some((scanner) => scanner.id === category)
    ? t(`admin.promptAudit.scanners.${category}`)
    : category
}
function formatDecisionRisk(decision: string, riskLevel: string): string {
  return `${translateDecision(decision)} · ${translateRiskLevel(riskLevel)}`
}
function formatCategories(categories: string[]): string {
  if (!categories.length) return '—'
  return categories.map(translateCategory).join(', ')
}
</script>

<style scoped>
@media (min-width: 1024px) {
  .event-workspace { padding-top: 12px; }
  .event-heading { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 4px 12px; }
  .event-heading > div:first-child { display: contents; }
  .event-heading h2 { grid-area: 1 / 1; }
  .event-heading p { grid-area: 2 / 1 / auto / -1; margin-top: 0; font-size: 12px; line-height: 18px; }
  .event-actions { grid-area: 1 / 2; gap: 6px; }
  .event-actions .btn { min-height: 32px; padding: 6px 10px; font-size: 12px; line-height: 18px; }
  .event-filters { gap: 8px; }
  .event-filters > :deep(label) { min-width: 0; line-height: 16px; }
  .event-filters :deep(.input), .event-filters :deep(.select-trigger) {
    height: 32px;
    min-height: 32px;
    min-width: 0;
    padding: 6px 8px;
    font-size: 12px;
    line-height: 18px;
  }
  .event-filters :deep(.date-time-trigger) { gap: 4px; font-size: 12px; line-height: 18px; }
  .event-pagination { padding: 8px 12px; }
  .event-pagination :deep(.pagination-desktop) { gap: 8px; }
  .event-pagination :deep(.pagination-button) { min-height: 32px; min-width: 32px; padding: 5px 8px; }
  .event-pagination :deep(.select-trigger), .event-pagination :deep(.input) { min-height: 32px; height: 32px; padding: 5px 8px; }
}
</style>
