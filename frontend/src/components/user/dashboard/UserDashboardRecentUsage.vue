<template>
  <div class="card">
    <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('dashboard.recentUsage') }}</h2>
      <span class="badge badge-gray">{{ startDate && endDate ? `${startDate} – ${endDate}` : t('dashboard.last7Days') }}</span>
    </div>
    <div class="p-6">
      <div v-if="loading && !data.length" class="flex items-center justify-center py-12">
        <LoadingSpinner size="lg" />
      </div>
      <div v-else-if="data.length === 0" class="py-8">
        <EmptyState :title="t('dashboard.noUsageRecords')" :description="t('dashboard.startUsingApi')" />
      </div>
      <div v-else class="space-y-3">
        <div v-for="log in data" :key="log.id" class="flex flex-wrap items-center justify-between gap-3 rounded-xl surface-inset p-4 transition-colors hover:surface-hover">
          <div class="flex min-w-0 items-center gap-4">
            <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-100/[0.15] dark:bg-primary-900/30">
              <Icon name="beaker" size="md" class="text-primary-600 dark:text-primary-400" />
            </div>
            <div class="min-w-0">
              <p class="truncate text-sm font-medium text-gray-900 dark:text-white" :title="log.model">{{ log.model }}</p>
              <p class="text-xs surface-muted">{{ formatDateTime(log.created_at) }}</p>
            </div>
          </div>
          <div class="text-right">
            <p class="text-sm font-semibold">
              <span class="text-green-600 dark:text-green-400" :title="t('dashboard.actual')">{{ usageMoney(log.actual_cost) }}</span>
              <span class="font-normal surface-muted" :title="t('dashboard.standard')"> / {{ usageMoney(log.total_cost) }}</span>
            </p>
            <p class="text-xs surface-muted">
              {{ requestTokenTotal(log).toLocaleString() }} Token
              <span v-if="requestTokenShare(log, log.input_tokens)">&middot; {{ t('dashboard.input') }} {{ requestTokenShare(log, log.input_tokens) }}</span>
              <span v-if="requestTokenShare(log, log.output_tokens)">&middot; {{ t('dashboard.output') }} {{ requestTokenShare(log, log.output_tokens) }}</span>
              <span v-if="requestTokenShare(log, requestCacheTokens(log))">&middot; {{ t('dashboard.cache') }} {{ requestTokenShare(log, requestCacheTokens(log)) }}</span>
            </p>
          </div>
        </div>

        <router-link to="/usage" class="flex items-center justify-center gap-2 py-3 text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300">
          {{ t('dashboard.viewAllUsage') }}
          <Icon name="arrowRight" size="sm" />
        </router-link>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import { usageMoney } from '@/utils/usageInspection'
import type { UsageLog } from '@/types'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'

defineProps<{
  data: UsageLog[]
  loading: boolean
  startDate?: string
  endDate?: string
}>()
const { t } = useI18n()

function requestCacheTokens(log: UsageLog): number {
  return sumTokenBuckets(log.cache_creation_tokens, log.cache_read_tokens)
}

function requestTokenTotal(log: UsageLog): number {
  return sumTokenBuckets(log.input_tokens, log.output_tokens, log.cache_creation_tokens, log.cache_read_tokens)
}

function requestTokenShare(log: UsageLog, tokens: number | null | undefined): string | null {
  return formatTokenShare(tokens, requestTokenTotal(log))
}
</script>
