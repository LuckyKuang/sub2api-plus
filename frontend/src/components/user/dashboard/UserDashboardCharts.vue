<template>
  <div :class="workspace ? 'workspace-body user-dashboard-charts' : 'space-y-6'">
    <!-- Date Range Filter -->
    <div v-if="showToolbar !== false" class="card p-4">
      <div class="flex flex-wrap items-center gap-4">
        <div class="flex items-center gap-2">
          <span class="text-sm font-medium surface-muted">{{ t('dashboard.timeRange') }}:</span>
          <DateRangePicker :start-date="startDate" :end-date="endDate" @update:startDate="$emit('update:startDate', $event)" @update:endDate="$emit('update:endDate', $event)" @change="$emit('dateRangeChange', $event)" />
        </div>
        <button @click="$emit('refresh')" :disabled="loading" class="btn btn-secondary">
          {{ t('common.refresh') }}
        </button>
        <div class="ml-auto flex items-center gap-2">
          <span class="text-sm font-medium surface-muted">{{ t('dashboard.granularity') }}:</span>
          <div class="w-28">
            <Select :model-value="granularity" :options="[{value:'day', label:t('dashboard.day')}, {value:'hour', label:t('dashboard.hour')}]" @update:model-value="$emit('update:granularity', $event)" @change="$emit('granularityChange')" />
          </div>
        </div>
      </div>
    </div>

    <!-- Charts Grid -->
    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2" :class="{ 'workspace-body user-chart-grid': workspace }">
      <!-- Model Distribution Chart -->
      <div class="card relative overflow-hidden p-4" :class="{ 'user-model-chart': workspace }">
        <div v-if="loading && !models.length" class="absolute inset-0 z-10 flex items-center justify-center surface-inset">
          <LoadingSpinner size="md" />
        </div>
        <h3 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.modelDistribution') }}</h3>
        <div class="model-distribution-body flex flex-col items-center gap-4">
          <div class="model-distribution-doughnut shrink-0">
            <Doughnut v-if="modelData" :data="modelData" :options="doughnutOptions" />
            <div v-else class="flex h-full items-center justify-center text-sm surface-muted">{{ t('dashboard.noDataAvailable') }}</div>
          </div>
          <div class="model-distribution-records record-table-scroll w-full min-w-0">
            <table class="model-distribution-table record-table record-table-compact w-full text-xs">
              <thead>
                <tr class="surface-muted">
                  <th class="pb-2 text-left">{{ t('dashboard.model') }}</th>
                  <th class="pb-2 text-left">{{ t('dashboard.requests') }}</th>
                  <th class="pb-2 text-left">{{ t('dashboard.tokens') }}</th>
                  <th class="pb-2 text-left">{{ t('dashboard.actual') }}</th>
                  <th class="pb-2 text-left">{{ t('dashboard.standard') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in models" :key="model.model" class="border-t border-gray-100 dark:border-dark-700">
                  <td class="py-1.5 font-medium text-gray-900 dark:text-white" :title="model.model">{{ model.model }}</td>
                  <td class="py-1.5 text-left surface-muted" :data-label="t('dashboard.requests')">{{ formatNumber(model.requests) }}</td>
                  <td class="py-1.5 text-left surface-muted" :data-label="t('dashboard.tokens')">{{ formatTokens(model.total_tokens) }} <span v-if="modelTokenShare(model.total_tokens)" class="surface-muted">({{ modelTokenShare(model.total_tokens) }})</span></td>
                  <td class="py-1.5 text-left text-green-600 dark:text-green-400" :data-label="t('dashboard.actual')">${{ formatCost(model.actual_cost) }}</td>
                  <td class="py-1.5 text-left surface-muted" :data-label="t('dashboard.standard')">${{ formatCost(model.cost) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- Token Usage Trend Chart -->
      <TokenUsageTrend :workspace="workspace" :trend-data="trend" :loading="loading" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useDocumentDarkMode } from '@/composables/useDocumentDarkMode'
import { chartTooltipStyle } from '@/utils/chartTooltip'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Select from '@/components/common/Select.vue'
import { Doughnut } from 'vue-chartjs'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { TrendDataPoint, ModelStat } from '@/types'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber, formatTokensK as formatTokens } from '@/utils/format'
import { formatTokenShare } from '@/utils/tokenShare'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler } from 'chart.js'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, ArcElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{ workspace?: boolean, showToolbar?: boolean, loading: boolean, startDate: string, endDate: string, granularity: string, trend: TrendDataPoint[], models: ModelStat[] }>()
defineEmits(['update:startDate', 'update:endDate', 'update:granularity', 'dateRangeChange', 'granularityChange', 'refresh'])
const { t } = useI18n()
const isDarkMode = useDocumentDarkMode()

const modelData = computed(() => !props.models?.length ? null : {
  labels: props.models.map((m: ModelStat) => m.model),
  datasets: [{
    data: props.models.map((m: ModelStat) => m.total_tokens),
    backgroundColor: ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16']
  }]
})

const modelTokenTotal = computed(() =>
  props.models.reduce((sum, model) => sum + Number(model.total_tokens || 0), 0)
)

const modelTokenShare = (tokens: number | null | undefined): string | null =>
  formatTokenShare(tokens, modelTokenTotal.value)

const doughnutOptions = computed(() => ({
  animation: false as const,
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: {
      ...chartTooltipStyle(isDarkMode.value),
      callbacks: {
        label: (context: any) => {
          const total = context.dataset.data.reduce((sum: number, value: number) => sum + value, 0)
          return `${context.label}: ${formatTokens(context.parsed)} tokens (${formatTokenShare(context.parsed, total) ?? '0.0%'})`
        }
      }
    }
  }
}))
</script>

<style scoped>
.user-dashboard-charts { display: block; height: auto; flex: none; container-type: inline-size; }
.user-chart-grid { display: grid; grid-template-columns: minmax(0, 1fr); align-items: stretch; gap: 16px !important; height: auto; flex: none; }
.user-chart-grid > .card { min-width: 0; min-height: 360px; padding: 20px; }
.user-model-chart { display: flex; flex-direction: column; container-type: inline-size; }
.model-distribution-body { flex: 1; min-width: 0; }
.model-distribution-doughnut { width: 154px; height: 154px; }
.user-dashboard-charts .user-model-chart .model-distribution-records { max-height: none; overflow: visible; }
.model-distribution-table { table-layout: auto; }
.model-distribution-table :is(th, td) { text-align: left; white-space: nowrap; }
.user-chart-grid :deep(.chart-workspace) { display: flex; flex-direction: column; }
.user-chart-grid :deep(.chart-workspace > .h-48) { height: 280px; min-height: 280px; flex: 1; }
@container (min-width: 1480px) {
  .user-chart-grid { grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr); }
}
@container (min-width: 780px) {
  .model-distribution-body { flex-direction: row; }
  .user-dashboard-charts .user-model-chart .model-distribution-records { width: 0; flex: 1 1 0%; }
}
/* Compact records retain the full, single-line model name without requiring
   sideways navigation when all five columns cannot fit beside each other. */
@container (max-width: 580px) {
  .model-distribution-table, .model-distribution-table tbody { display: block; }
  .model-distribution-table thead { display: none; }
  .model-distribution-table tbody tr { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .model-distribution-table td:first-child { grid-column: 1 / -1; }
  .model-distribution-table td[data-label]::before { display: block; margin-bottom: 4px; color: var(--surface-muted); content: attr(data-label); }
  .model-distribution-table td:nth-child(3) > span { display: block; }
}
</style>
