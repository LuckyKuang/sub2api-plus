<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-center justify-between gap-3 text-xs surface-muted">
        <span v-if="stats">{{ t('inspection.timezone', { zone: stats.timezone || 'UTC' }) }}</span>
        <RefreshControl :state="overviewRefresh" />
        <button v-if="!stats && overviewRefresh.error.value" class="btn btn-secondary" @click="refreshAll">{{ t('inspection.retry') }}</button>
      </div>
      <div v-if="loading && !stats" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <template v-else-if="stats">
        <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :frozen-balance="user?.frozen_balance" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
        <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="changeCharts" @granularityChange="changeCharts" @refresh="refreshAll" />
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <div class="min-w-0 lg:col-span-2"><UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" :start-date="startDate" :end-date="endDate" /></div>
          <div class="lg:col-span-1"><UserDashboardQuickActions /></div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'; import { useUserView as useAuthStore } from '@/composables/useUserView'; import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'; import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'; import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'; import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'
import { useI18n } from 'vue-i18n'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import RefreshControl from '@/components/common/RefreshControl.vue'
import { useColumnPreferenceKey } from '@/composables/useColumnPreferenceKey'
import { supportRequestGeneration } from '@/utils/adminSupportContext'
const { t } = useI18n()

const authStore = useAuthStore(); const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null); const loading = ref(false); const loadingUsage = ref(false); const loadingCharts = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const modelStats = ref<ModelStat[]>([]); const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)

const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000))); const endDate = ref(formatDateLocalInput(new Date())); const granularity = ref('day')

let disposed = false
let chartSeq = 0
let recentSeq = 0
let chartUpdatedAt = 0
let forceCharts = true
const scope = supportRequestGeneration()
const current = () => !disposed && scope === supportRequestGeneration()

const loadStats = async () => {
  loading.value = true
  try {
    const data = await usageAPI.getDashboardStats()
    if (current()) stats.value = data
    await authStore.refreshUser()
  } finally { if (current()) loading.value = false }
}
const loadCharts = async () => {
  const seq = ++chartSeq
  loadingCharts.value = true
  try {
    const params = { start_date: startDate.value, end_date: endDate.value }
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({ ...params, granularity: granularity.value as 'day' | 'hour' }),
      usageAPI.getDashboardModels(params),
    ])
    if (!current() || seq !== chartSeq) return
    trendData.value = trend.trend || []
    modelStats.value = models.models || []
    chartUpdatedAt = Date.now()
  } finally { if (current() && seq === chartSeq) loadingCharts.value = false }
}
const loadRecent = async () => {
  const seq = ++recentSeq
  loadingUsage.value = true
  try {
    const data = await usageAPI.getByDateRange(startDate.value, endDate.value)
    if (current() && seq === recentSeq) recentUsage.value = data.items.slice(0, 5)
  } finally { if (current() && seq === recentSeq) loadingUsage.value = false }
}
const loadPlatformQuotas = async () => {
  const data = await getMyPlatformQuotas()
  if (current()) platformQuotas.value = data.platform_quotas ?? []
}
const overviewRefresh = useAutoRefresh({
  storageKey: useColumnPreferenceKey('user-overview-refresh'), intervals: [30, 60, 120], defaultInterval: 30, defaultEnabled: true,
  shouldPause: () => loadingCharts.value,
  onRefresh: async () => {
    const includeCharts = forceCharts || Date.now() - chartUpdatedAt >= 60000
    forceCharts = false
    const results = await Promise.allSettled([loadStats(), loadRecent(), loadPlatformQuotas(), ...(includeCharts ? [loadCharts()] : [])])
    const failure = results.find(result => result.status === 'rejected')
    if (failure?.status === 'rejected') throw failure.reason
  },
})
const refreshAll = () => { forceCharts = true; return overviewRefresh.refresh() }
const changeCharts = async () => {
  try { await Promise.all([loadCharts(), loadRecent()]) }
  catch (error) { overviewRefresh.error.value = error }
}
onMounted(() => { void refreshAll() })
onBeforeUnmount(() => { disposed = true; chartSeq++; recentSeq++ })
</script>
