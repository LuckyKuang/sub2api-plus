<template>
  <DetailWorkspace :show="selectedId != null" :title="t('inspection.title')" @close="close">
    <slot :select="select" :selected-id="selectedId" />
    <template #detail>
      <div class="mb-4 flex items-center justify-between gap-2">
        <button type="button" class="btn btn-secondary" :disabled="index <= 0" @click="select(rows[index - 1])">{{ t('inspection.previous') }}</button>
        <button type="button" class="btn btn-secondary" :disabled="index < 0 || index >= rows.length - 1" @click="select(rows[index + 1])">{{ t('inspection.next') }}</button>
      </div>
      <p v-if="index < 0 && detail" class="mb-4 text-xs text-amber-600">{{ t('inspection.notInResults') }}</p>
      <p v-if="loading" role="status" class="py-6 text-sm text-gray-500">{{ t('inspection.updating') }}</p>
      <div v-else-if="failed" role="alert" class="py-6 text-sm">
        <p>{{ t('inspection.loadFailed') }}</p><button type="button" class="btn btn-secondary mt-3" @click="load">{{ t('inspection.retry') }}</button>
      </div>
      <UsageRecordDetail v-else-if="detail" :row="detail" :admin="mode === 'admin'" />
    </template>
  </DetailWorkspace>
</template>

<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { AdminUsageLog } from '@/types'
import { usageAPI } from '@/api/usage'
import { adminUsageAPI } from '@/api/admin/usage'
import { adminSupportContext } from '@/utils/adminSupportContext'
import DetailWorkspace from '@/components/common/DetailWorkspace.vue'
import UsageRecordDetail from './UsageRecordDetail.vue'
const props = withDefaults(defineProps<{ rows: AdminUsageLog[]; mode: 'user' | 'admin'; active?: boolean }>(), { active: true })
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const selectedId = computed(() => {
  if (!props.active) return null
  const value = Number(route.query.record)
  return Number.isSafeInteger(value) && value > 0 ? value : null
})
const index = computed(() => props.rows.findIndex(row => row.id === selectedId.value))
const detail = ref<AdminUsageLog | null>(null)
const loading = ref(false)
const failed = ref(false)
let controller: AbortController | undefined
let generation = 0
let origin: HTMLElement | null = null
let openedFrom: string | null = null
async function select(row: AdminUsageLog) {
  if (selectedId.value == null) {
    origin = document.activeElement as HTMLElement
    openedFrom = route.fullPath
    await router.push({ query: { ...route.query, record: String(row.id) }, hash: route.hash })
  } else await router.replace({ query: { ...route.query, record: String(row.id) }, hash: route.hash })
}
async function close() {
  const query = { ...route.query }
  delete query.record
  const list = { path: route.path, query, hash: route.hash }
  // Going back is safe only while the original list location is unchanged.
  // Filters changed while reading must survive closing the detail.
  const canGoBack = openedFrom === router.resolve(list).fullPath
  openedFrom = null
  if (canGoBack) router.back()
  else await router.replace(list)
  await nextTick()
  if (origin?.isConnected) origin.focus({ preventScroll: true })
}
async function load() {
  const seq = ++generation
  controller?.abort()
  detail.value = null
  failed.value = false
  const id = selectedId.value
  if (id == null) { loading.value = false; return }
  loading.value = true
  controller = new AbortController()
  try {
    const data: AdminUsageLog = await (props.mode === 'admin' ? adminUsageAPI.getById(id, controller.signal) : usageAPI.getById(id, controller.signal))
    if (seq === generation) {
      const listed = props.rows.find(row => row.id === data.id)
      detail.value = { ...data, api_key: data.api_key ?? listed?.api_key, group: data.group ?? listed?.group,
        account: data.account ?? listed?.account, user: data.user ?? listed?.user }
    }
  } catch {
    if (seq === generation && !controller.signal.aborted) failed.value = true
  } finally { if (seq === generation) loading.value = false }
}
watch([selectedId, () => route.path, () => adminSupportContext.value?.userId], load, { immediate: true })
watch(selectedId, id => { if (id == null) openedFrom = null })
watch(() => props.rows, rows => {
  const updated = rows.find(row => row.id === selectedId.value)
  if (updated && detail.value?.id === updated.id && !loading.value) detail.value = updated
})
onBeforeUnmount(() => { generation++; controller?.abort() })
</script>
