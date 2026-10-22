<template>
  <div class="space-y-6 text-sm">
    <section v-for="section in sections" :key="section.title">
      <h3 class="mb-3 font-semibold text-gray-900 dark:text-white">{{ section.title }}</h3>
      <dl class="space-y-3">
        <div v-for="field in section.fields" :key="field.label" class="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-3">
          <dt class="text-gray-500 dark:text-dark-400">{{ field.label }}</dt>
          <dd class="min-w-0 break-words text-right tabular-nums [overflow-wrap:anywhere]">
            <button v-if="field.copy && field.value !== '—'" type="button" class="text-primary-600 dark:text-primary-400" :title="copied === field.label ? t('inspection.copied') : t('inspection.copy')" @click="copy(field)">{{ field.value }}</button>
            <span v-else>{{ field.value }}</span>
          </dd>
        </div>
      </dl>
    </section>
    <p v-if="tpsNote(row)" class="rounded-lg bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-900 dark:text-gray-300">{{ t(tpsNote(row)!) }}</p>
    <p class="text-xs text-gray-500">{{ t('usage.latencyTpsHint') }}</p>
    <span role="status" class="sr-only">{{ copied ? t('inspection.copied') : '' }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminUsageLog } from '@/types'
import { formatDateTime } from '@/utils/format'
import { averageTps, firstByteMs, strictFirstTokenMs, formatTpsNumber, tpsNote } from '@/utils/usageTiming'
import { usageCompletion, usageMoney, usageMilliseconds } from '@/utils/usageInspection'
import { getBillingModeLabel, getDisplayBillingMode } from '@/utils/billingMode'
const props = defineProps<{ row: AdminUsageLog; admin?: boolean }>()
const { t } = useI18n()
type Field = { label: string; value: string; copy?: boolean }
const copied = ref('')
async function copy(field: Field) {
  try { await navigator.clipboard.writeText(field.value); copied.value = field.label } catch { copied.value = '' }
}
const sections = computed(() => {
  const r = props.row
  const f = (key: string, value: unknown, copy = false): Field => ({ label: t(key), value: value == null || value === '' ? '—' : String(value), copy })
  const fields = (pairs: [string, unknown][]) => pairs.map(([key, value]) => f(`inspection.${key}`, value))
  const money = (pairs: [string, number | null | undefined][]) => fields(pairs.map(([key, value]) => [key, usageMoney(value)]))
  const rate = averageTps(r)
  const result = [
    { title: t('inspection.summary'), fields: [
      f('usage.time', formatDateTime(r.created_at)), f('usage.model', r.model),
      f('inspection.status', t(`inspection.${usageCompletion(r)}`)), f('usage.apiKeyFilter', r.api_key?.name ?? (r.api_key_id ? `#${r.api_key_id}` : null)),
      f('admin.usage.group', r.group?.name ?? (r.group_id ? `#${r.group_id}` : null)), f('usage.reasoningEffort', r.reasoning_effort),
      f('usage.inbound', r.inbound_endpoint), f('usage.type', r.request_type),
      f('admin.usage.billingMode', getBillingModeLabel(getDisplayBillingMode(r), t)),
    ] },
    { title: t('inspection.performance'), fields: [
      f('usage.latencyFirstByte', usageMilliseconds(firstByteMs(r))),
      f('usage.latencyFirstToken', usageMilliseconds(strictFirstTokenMs(r))),
      f('usage.latencyLastToken', usageMilliseconds(strictFirstTokenMs(r) == null ? null : r.last_token_ms)),
      f('usage.latencyFirstOutputKind', r.first_output_kind),
      f('usage.latencyDuration', usageMilliseconds(r.duration_ms)),
      f('usage.latencyTps', rate == null ? '—' : `${formatTpsNumber(rate)} tok/s`),
    ] },
    { title: t('inspection.usage'), fields: fields([
      ['input', r.input_tokens], ['output', r.output_tokens], ['cacheRead', r.cache_read_tokens], ['cacheWrite', r.cache_creation_tokens],
      ['cache5m', r.cache_creation_5m_tokens], ['cache1h', r.cache_creation_1h_tokens],
      ['imageInput', r.image_input_tokens], ['imageOutput', r.image_output_tokens], ['audioOutput', r.audio_output_tokens],
      ['images', r.image_count], ['imageSize', r.image_output_size ?? r.image_size], ['usageSource', r.usage_source],
    ]) },
    { title: t('inspection.billing'), fields: [
      ...money([['actualCost', r.actual_cost], ['standardCost', r.total_cost], ['inputCost', r.input_cost], ['outputCost', r.output_cost],
        ['cacheReadCost', r.cache_read_cost], ['cacheWriteCost', r.cache_creation_cost], ['imageInputCost', r.image_input_cost], ['imageOutputCost', r.image_output_cost]]),
      ...fields([['multiplier', r.rate_multiplier], ['billingSource', t(r.subscription_id != null ? 'inspection.subscription' : 'inspection.balance')]]),
    ] },
    { title: t('inspection.client'), fields: [f('inspection.requestId', r.request_id, true), f('inspection.sessionId', r.session_id, true), f('inspection.ip', r.ip_address, true), f('inspection.userAgent', r.user_agent, true)] },
  ]
  if (props.admin) result.push({ title: t('inspection.upstream'), fields: [
    f('admin.usage.user', r.user?.email ?? `#${r.user_id}`),
    f('admin.usage.account', r.account?.name ?? `#${r.account_id}`), f('usage.upstream', r.upstream_endpoint),
    ...fields([['mapping', r.model_mapping_chain ?? r.upstream_model], ['responseModel', r.upstream_response_model], ['requestedTier', r.requested_service_tier], ['responseTier', r.response_service_tier], ['accountMultiplier', r.account_rate_multiplier]]),
    f('inspection.upstreamRequestId', r.upstream_request_id, true),
    ...money([['accountCost', r.account_stats_cost != null || r.total_cost != null ? (r.account_stats_cost ?? r.total_cost) * (r.account_rate_multiplier ?? 1) : null]]),
  ] })
  return result
})
</script>
