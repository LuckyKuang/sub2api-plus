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
        <div class="flex flex-col items-center gap-4 sm:flex-row sm:gap-6">
          <div class="h-48 w-48 shrink-0">
            <Doughnut v-if="modelData" :data="modelData" :options="doughnutOptions" />
            <div v-else class="flex h-full items-center justify-center text-sm surface-muted">{{ t('dashboard.noDataAvailable') }}</div>
          </div>
          <div class="record-table-scroll max-h-48 w-full min-w-0 flex-1 overflow-auto">
            <table class="record-table record-table-compact w-full text-xs">
              <thead>
                <tr class="surface-muted">
                  <th class="pb-2 text-left">{{ t('dashboard.model') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.requests') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.tokens') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.actual') }}</th>
                  <th class="pb-2 text-right">{{ t('dashboard.standard') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in models" :key="model.model" class="border-t border-gray-100 dark:border-dark-700">
                  <td class="max-w-[100px] truncate py-1.5 font-medium text-gray-900 dark:text-white" :title="model.model">{{ model.model }}</td>
                  <td class="py-1.5 text-right surface-muted">{{ formatNumber(model.requests) }}</td>
                  <td class="py-1.5 text-right surface-muted">{{ formatTokens(model.total_tokens) }} <span v-if="modelTokenShare(model.total_tokens)" class="surface-muted">({{ modelTokenShare(model.total_tokens) }})</span></td>
                  <td class="py-1.5 text-right text-green-600 dark:text-green-400">${{ formatCost(model.actual_cost) }}</td>
                  <td class="py-1.5 text-right surface-muted">${{ formatCost(model.cost) }}</td>
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
.record-table-scroll { --record-table-max-height: 12rem; max-height: var(--record-table-max-height); }
@media (min-width: 1024px) {
  .user-chart-grid { display: grid; gap: 12px; }
  .user-model-chart { display: flex; min-height: 0; flex-direction: column; padding: 14px; }
  .user-model-chart > h3 { flex: none; margin-bottom: 12px; }
  .user-model-chart > div:not(.absolute) { min-height: 0; flex: 1; gap: 10px; }
  .user-model-chart .h-48.w-48 { width: clamp(110px, 9vw, 178px); height: clamp(110px, 9vw, 178px); flex: none; }
  .user-model-chart .record-table-scroll { --record-table-max-height: 100%; min-height: 0; align-self: stretch; }
}
</style>
