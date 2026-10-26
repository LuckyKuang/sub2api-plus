<template>
  <tr v-if="loading">
    <td :colspan="columnCount" data-record-edges="b l r bl br" class="py-3">
      <div class="flex justify-center"><LoadingSpinner /></div>
    </td>
  </tr>
  <tr v-else-if="items.length === 0">
    <td :colspan="columnCount" data-record-edges="b l r bl br" class="py-2 text-center text-xs surface-muted">
      {{ t('admin.dashboard.noDataAvailable') }}
    </td>
  </tr>
  <template v-else>
    <tr
      v-for="(user, index) in items"
      :key="user.user_id"
      class="text-xs"
    >
      <td :data-record-edges="index === items.length - 1 ? 'b l bl' : 'l'" class="py-1 pl-6 surface-muted">
        <span class="block max-w-[120px] truncate" :title="user.email">{{ user.email || `User #${user.user_id}` }}</span>
      </td>
      <td :data-record-edges="index === items.length - 1 ? 'b' : ''" class="py-1 text-right surface-muted">
        {{ user.requests.toLocaleString() }}
      </td>
      <td :data-record-edges="index === items.length - 1 ? 'b' : ''" class="py-1 text-right surface-muted">
        {{ formatTokens(user.total_tokens) }} <span v-if="tokenShare(user.total_tokens)" class="surface-muted">({{ tokenShare(user.total_tokens) }})</span>
      </td>
      <td :data-record-edges="index === items.length - 1 ? 'b' : ''" class="py-1 text-right text-green-600 dark:text-green-400">
        ${{ formatCost(user.actual_cost) }}
      </td>
      <td v-if="showAccountCost" :data-record-edges="index === items.length - 1 ? 'b' : ''" class="py-1 text-right text-orange-500 dark:text-orange-400">
        ${{ formatCost(user.account_cost) }}
      </td>
      <td :data-record-edges="index === items.length - 1 ? 'b r br' : 'r'" class="py-1 text-right surface-muted">
        ${{ formatCost(user.cost) }}
      </td>
    </tr>
  </template>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { UserBreakdownItem } from '@/types'
import { formatTokenShare } from '@/utils/tokenShare'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  items: UserBreakdownItem[]
  loading?: boolean
  showAccountCost?: boolean
  totalTokens?: number
}>(), {
  loading: false,
  showAccountCost: true,
  totalTokens: 0,
})

const showAccountCost = computed(() => props.showAccountCost)
const columnCount = computed(() => showAccountCost.value ? 6 : 5)
const tokenShare = (tokens: number | null | undefined): string | null =>
  formatTokenShare(tokens, props.totalTokens)

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)}B`
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toLocaleString()
}

const formatCost = (value: number | undefined | null): string => {
  if (value == null) return '0.0000'
  if (value >= 1000) return (value / 1000).toFixed(2) + 'K'
  if (value >= 1) return value.toFixed(2)
  if (value >= 0.01) return value.toFixed(3)
  return value.toFixed(4)
}
</script>
