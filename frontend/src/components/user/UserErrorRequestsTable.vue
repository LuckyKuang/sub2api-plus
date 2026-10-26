<template>
  <DetailWorkspace :workspace="workspace" :show="showDetail" :title="t('usage.errors.detail.title')" @close="showDetail = false">
    <div class="error-compact flex min-h-0 flex-1 flex-col" :style="{ '--error-table-width': compactTableWidth + 'px' }">
    <div class="card flex min-h-0 flex-1 flex-col overflow-hidden" :class="{ 'workspace-shell': workspace }">
      <IpGeoBatchToolbar :class="{ 'workspace-fixed': workspace }" :ips="rows.map((r) => r.client_ip)" @failed="emit('ipGeoBatchFailed')" />

      <DataTable
        :class="{ 'workspace-table-viewport': workspace }"
        :columns="columns"
        :data="rows"
        :loading="loading"
        :active-row-key="showDetail ? selectedId : undefined"
        clickable-rows
        server-side-sort
        default-sort-key="created_at"
        default-sort-order="desc"
        @sort="onSort"
        @rowClick="(row) => openDetail(row.id)"
      >
        <template #cell-model="{ row }">
          <span v-if="row.model" class="text-sm font-medium text-gray-900 dark:text-white">{{ row.model }}</span>
          <span v-else class="text-sm surface-muted">-</span>
        </template>

        <template #cell-key_name="{ row }">
          <div class="text-sm">
            <span class="block max-w-[160px] truncate text-gray-900 dark:text-white" :title="row.key_name">{{ row.key_name || '-' }}</span>
            <span
              v-if="row.key_deleted"
              class="ml-1 inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100/[0.15] text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30"
            >{{ t('usage.errors.keyDeleted') }}</span>
            <span v-if="!columns.some(column => column.key === 'group')" class="block max-w-[160px] truncate text-xs surface-muted" :title="row.group_name">{{ row.group_name || '—' }}</span>
          </div>
        </template>

        <template #cell-endpoint="{ row }">
          <div class="max-w-[320px] text-xs">
            <div class="break-all surface-muted">
              <span class="font-medium surface-muted">{{ t('usage.inbound') }}:</span>
              <span class="ml-1">{{ row.inbound_endpoint?.trim() || '-' }}</span>
            </div>
          </div>
        </template>

        <template #cell-status="{ row }">
          <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="statusClass(row.status_code)">
            {{ row.status_code || '-' }}
          </span>
        </template>

        <template #cell-category="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ t('usage.errors.categories.' + row.category) }}</span>
        </template>

        <template #cell-message="{ row }">
          <span
            v-if="row.message"
            class="block max-w-[280px] truncate text-sm surface-muted"
            :title="row.message"
          >{{ row.message }}</span>
          <span v-else class="text-sm surface-muted">-</span>
        </template>

        <template #cell-group="{ row }">
          <span
            v-if="row.group_name"
            class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-indigo-100/[0.15] text-indigo-800 dark:bg-indigo-900/25 dark:text-indigo-200"
          >{{ row.group_name }}</span>
          <span v-else class="text-sm surface-muted">-</span>
        </template>

        <template #cell-type="{ row }">
          <span
            v-if="requestTypeBadge(row)"
            class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium"
            :class="requestTypeBadge(row)!.className"
          >{{ requestTypeBadge(row)!.label }}</span>
          <span v-else class="text-sm surface-muted">-</span>
        </template>

        <template #cell-platform="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ row.platform || '-' }}</span>
        </template>

        <template #cell-client_ip="{ row }">
          <div @click.stop>
            <div v-if="row.client_ip">
              <span class="text-sm font-mono surface-muted">{{ row.client_ip }}</span>
              <IpGeoCell :ip="row.client_ip" />
            </div>
            <span v-else class="text-sm surface-muted">-</span>
          </div>
        </template>

        <template #cell-created_at="{ row }">
          <button type="button" class="text-left text-sm text-primary-600 underline decoration-dotted surface-control" :aria-label="t('inspection.view')" @click.stop="openDetail(row.id)">{{ formatDateTime(row.created_at) }}</button>
        </template>

        <template #cell-user_agent="{ row }">
          <span
            v-if="row.user_agent"
            class="block max-w-[320px] truncate text-sm surface-muted"
            :title="row.user_agent"
          >{{ row.user_agent }}</span>
          <span v-else class="text-sm surface-muted">-</span>
        </template>

        <template #empty><EmptyState :message="t('usage.errors.empty')" /></template>
      </DataTable>
    </div>

    <div class="flex-shrink-0">
      <Pagination
        v-if="total > 0"
        :page="page"
        :page-size="pageSize"
        :total="total"
        @update:page="$emit('update:page', $event)"
        @update:pageSize="$emit('update:pageSize', $event)"
      />
    </div>

    </div>
    <template #detail><UserErrorDetailModal embedded v-model:show="showDetail" :error-id="selectedId" /></template>
  </DetailWorkspace>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DataTable from '@/components/common/DataTable.vue'
import DetailWorkspace from '@/components/common/DetailWorkspace.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import UserErrorDetailModal from '@/components/user/UserErrorDetailModal.vue'
import IpGeoCell from '@/components/common/IpGeoCell.vue'
import IpGeoBatchToolbar from '@/components/common/IpGeoBatchToolbar.vue'
import { formatDateTime } from '@/utils/format'
import {
  mapErrorSortKey,
  numericRequestTypeKind,
  requestTypeBadgeClass,
  requestTypeLabelKey,
  statusCodeBadgeClass,
} from '@/utils/errorBadges'
import type { UserErrorRequest } from '@/types'
import type { Column } from '@/components/common/types'

const props = defineProps<{
  rows: UserErrorRequest[]
  total: number
  loading: boolean
  page: number
  pageSize: number
  workspace?: boolean
  /** 列设置:仅显示这些 key 的列;不传则全显(key 须与 allColumns 一致) */
  visibleColumnKeys?: string[]
}>()

const emit = defineEmits<{
  (e: 'update:page', v: number): void
  (e: 'update:pageSize', v: number): void
  (e: 'ipGeoBatchFailed'): void
  (e: 'sort', sortBy: string, sortOrder: 'asc' | 'desc'): void
}>()

function onSort(key: string, order: 'asc' | 'desc') {
  emit('sort', mapErrorSortKey(key), order)
}

const { t } = useI18n()

const allColumns = computed<Column[]>(() => [
  { key: 'created_at', label: t('usage.errors.time'), sortable: true },
  { key: 'key_name', label: t('usage.errors.keyName') },
  { key: 'model', label: t('usage.errors.model'), sortable: true },
  { key: 'endpoint', label: t('usage.errors.endpoint') },
  { key: 'client_ip', label: 'IP' },
  { key: 'group', label: t('admin.usage.group') },
  { key: 'type', label: t('usage.type') },
  { key: 'platform', label: t('usage.errors.platform') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('usage.errors.status'), sortable: true },
  { key: 'message', label: t('usage.errors.message') },
  { key: 'user_agent', label: t('usage.userAgent') },
])

const columns = computed<Column[]>(() =>
  props.visibleColumnKeys
    ? allColumns.value.filter((c) => props.visibleColumnKeys!.includes(c.key))
    : allColumns.value
)
const compactWidths: Record<string, number> = { created_at: 124, key_name: 140, model: 160, category: 100, status: 90, message: 240 }
const compactTableWidth = computed(() => columns.value.reduce((width, column) => width + (compactWidths[column.key] ?? 160), 0))

function requestTypeBadge(row: UserErrorRequest): { label: string; className: string } | null {
  const kind = numericRequestTypeKind(row.request_type, row.stream)
  if (!kind) return null
  return { label: t(requestTypeLabelKey(kind)), className: requestTypeBadgeClass(kind) }
}

const showDetail = ref(false)
const selectedId = ref<number | null>(null)

function openDetail(id: number) {
  selectedId.value = id
  showDetail.value = true
}

const statusClass = statusCodeBadgeClass
</script>

<style scoped>
.error-compact :deep(table) { table-layout: fixed; min-width: var(--error-table-width); }
.error-compact :deep(th), .error-compact :deep(td) { width: 160px; padding-inline: 10px; white-space: normal; overflow-wrap: anywhere; }
.error-compact :deep([data-field="created_at"]) { width: 124px; }
.error-compact :deep([data-field="key_name"]) { width: 140px; }
.error-compact :deep([data-field="category"]) { width: 100px; }
.error-compact :deep([data-field="status"]) { width: 90px; }
.error-compact :deep([data-field="message"]) { width: 240px; }
.error-compact :deep(td [class*="max-w-"]) { max-width: 100%; }
@media (max-width: 767px) { .error-compact :deep([data-field]) { width: auto; padding-inline: 0; } }
</style>
