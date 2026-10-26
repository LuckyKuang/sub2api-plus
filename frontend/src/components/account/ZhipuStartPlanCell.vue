<template>
  <div v-if="visible" data-test="zhipu-start-plan" class="min-w-[180px] space-y-1 text-xs">
    <div class="flex flex-wrap items-center gap-1.5">
      <span>{{ t('admin.accounts.cnProviders.startPlan.title') }}</span>
      <span data-test="start-plan-state" class="surface-muted">{{ t(`admin.accounts.cnProviders.startPlan.${state}`) }}</span>
      <button
        type="button"
        data-test="start-plan-probe"
        class="rounded px-1.5 py-0.5 text-[10px] text-blue-600 disabled:opacity-50 dark:text-blue-400 surface-control"
        :disabled="loading"
        @click="probe"
      >{{ t('admin.accounts.cnProviders.probe') }}</button>
    </div>
    <div v-if="state === 'available'" data-test="start-plan-models" class="surface-muted">
      {{ !data?.models_known ? t('admin.accounts.cnProviders.startPlan.modelsUnknown') : data.models.length ? data.models.join(', ') : t('admin.accounts.cnProviders.startPlan.modelsEmpty') }}
    </div>
    <div v-if="data?.reason === 'auth_failed' || data?.reason === 'reauthorization_required'" data-test="start-plan-reauthorize" class="text-amber-600 dark:text-amber-400">
      {{ t('admin.accounts.cnProviders.startPlan.reauthorize') }}
    </div>
    <div v-if="state === 'pending' && data?.effective_at" data-test="start-plan-effective" class="surface-muted">
      {{ t('admin.accounts.cnProviders.startPlan.effectiveAt', { time: formatTime(data.effective_at) }) }}
    </div>
    <div v-if="data?.expires_at" data-test="start-plan-expiry" class="surface-muted">
      {{ t('admin.accounts.cnProviders.startPlan.expiresAt', { time: formatTime(data.expires_at) }) }}
    </div>
    <div v-if="failed" data-test="start-plan-error" class="text-[10px] text-red-600 dark:text-red-400">
      {{ t('admin.accounts.cnProviders.startPlan.queryFailed') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { ZhipuStartPlanEntitlement } from '@/api/admin/cnProviders'
import type { Account } from '@/types'

const props = defineProps<{ account: Account }>()
const { t } = useI18n()
const visible = computed(() => props.account.platform === 'zhipu' && props.account.type === 'oauth' &&
  props.account.credentials?.plan_kind === 'start-plan' &&
  ['bigmodel', 'zai'].includes(String(props.account.credentials?.oauth_provider)))
const data = ref<ZhipuStartPlanEntitlement | null>(null)
const loading = ref(false)
const failed = ref(false)
const now = ref(Date.now())
const timer = setInterval(() => { now.value = Date.now() }, 1000)
let generation = 0
// The admin API supplies an opaque grant revision because credentials are
// redacted. Same-ID relinks and route edits invalidate replies.
const ownerKey = computed(() => JSON.stringify([
  props.account.id, props.account.platform, props.account.type, props.account.oauth_credential_owner,
  props.account.credentials, props.account.proxy_id, props.account.proxy
]))
const state = computed(() => {
  if (!data.value) return 'unknown'
  const age = Math.max(0, Math.floor(now.value / 1000) - data.value.observed_at)
  const remoteNow = data.value.server_time + age
  if (data.value.expires_at && remoteNow >= data.value.expires_at) return 'unavailable'
  if (age > 15 * 60) return 'unknown'
  // Reaching an effective date cannot promote a cached pending result.
  return data.value.state
})
const formatTime = (seconds: number) => new Date(seconds * 1000).toLocaleString()
const probe = async () => {
  if (!visible.value || loading.value) return
  const requestGeneration = generation
  const requestOwner = ownerKey.value
  loading.value = true
  failed.value = false
  try {
    const result = await adminAPI.cnProviders.queryQuota(props.account.id)
    if (requestGeneration !== generation || requestOwner !== ownerKey.value) return
    if (result.success && result.persisted && result.start_plan) data.value = result.start_plan
    else failed.value = true
  } catch {
    if (requestGeneration === generation && requestOwner === ownerKey.value) failed.value = true
  } finally {
    if (requestGeneration === generation && requestOwner === ownerKey.value) loading.value = false
  }
}
watch(ownerKey, () => {
  generation++
  data.value = null
  failed.value = false
  loading.value = false
  void probe()
}, { immediate: true })
onBeforeUnmount(() => { generation++; clearInterval(timer) })
</script>
