<template>
  <div class="space-y-4" data-testid="usage-overview-panel">
    <div v-if="error" role="alert" class="card flex flex-wrap items-center justify-between gap-3 p-4 text-sm text-red-600 dark:text-red-400">
      <span>{{ error }}</span>
      <button type="button" class="btn btn-secondary btn-sm" @click="load(true)">{{ t('inspection.retry') }}</button>
    </div>
    <UsageStatsCards :stats="stats" />
    <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <GroupDistributionChart
        v-model:metric="groupMetric"
        :group-stats="groupStats"
        :loading="loading"
        :show-metric-toggle="true"
        :start-date="startDate"
        :end-date="endDate"
      />
      <EndpointDistributionChart
        v-model:source="endpointSource"
        v-model:metric="endpointMetric"
        :endpoint-stats="inboundEndpointStats"
        :upstream-endpoint-stats="upstreamEndpointStats"
        :endpoint-path-stats="endpointPathStats"
        :loading="loading"
        :show-source-toggle="true"
        :show-metric-toggle="true"
        :title="t('usage.endpointDistribution')"
        :start-date="startDate"
        :end-date="endDate"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'
import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import { adminAPI } from '@/api/admin'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type { EndpointStat, GroupStat } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'

type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'

// The dashboard owns the date range: this panel only reads usage totals and the
// group/endpoint distributions for it, so the usage page does not need a second
// charts workspace.
const props = defineProps<{
  startDate: string
  endDate: string
}>()

const { t } = useI18n()
const stats = ref<AdminUsageStatsResponse | null>(null)
const groupStats = ref<GroupStat[]>([])
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])
const groupMetric = ref<DistributionMetric>('tokens')
const endpointMetric = ref<DistributionMetric>('tokens')
const endpointSource = ref<EndpointSource>('inbound')
const loading = ref(false)
const error = ref('')
let loadSeq = 0

async function load(force = false) {
  const seq = ++loadSeq
  loading.value = true
  error.value = ''
  try {
    const [usageStats, snapshot] = await Promise.all([
      adminAPI.usage.getStats({
        start_date: props.startDate,
        end_date: props.endDate,
        ...(force ? { nocache: 1 } : {}),
      }),
      adminAPI.dashboard.getSnapshotV2({
        start_date: props.startDate,
        end_date: props.endDate,
        granularity: 'hour',
        include_stats: false,
        include_trend: false,
        include_model_stats: false,
        include_group_stats: true,
        include_users_trend: false,
      }),
    ])
    if (seq !== loadSeq) return
    stats.value = usageStats
    groupStats.value = snapshot.groups || []
    inboundEndpointStats.value = usageStats.endpoints || []
    upstreamEndpointStats.value = usageStats.upstream_endpoints || []
    endpointPathStats.value = usageStats.endpoint_paths || []
  } catch (err) {
    if (seq !== loadSeq) return
    error.value = extractApiErrorMessage(err, t('usage.overviewLoadFailed'))
    stats.value = null
    groupStats.value = []
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

watch(() => [props.startDate, props.endDate], () => { void load() }, { immediate: true })

defineExpose({ reload: () => load(true) })
</script>
