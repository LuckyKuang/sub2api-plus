<template>
  <div
    class="pagination-card pagination-container card min-w-0 px-4 py-3 sm:px-6"
  >
    <div class="pagination-mobile">
      <!-- Mobile pagination -->
      <button
        @click="goToPage(page - 1)"
        :disabled="page === 1"
        class="pagination-button relative inline-flex items-center border border-gray-300 px-4 py-2 text-sm font-medium hover:surface-hover disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 surface-control surface-muted"
      >
        {{ t('pagination.previous') }}
      </button>
      <span class="pagination-page-label text-sm surface-muted">
        {{ t('pagination.pageOf', { page, total: totalPages }) }}
      </span>
      <button
        @click="goToPage(page + 1)"
        :disabled="page === totalPages"
        class="pagination-button relative inline-flex items-center border border-gray-300 px-4 py-2 text-sm font-medium hover:surface-hover disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 surface-control surface-muted"
      >
        {{ t('pagination.next') }}
      </button>
    </div>

    <div class="pagination-desktop">
      <!-- Desktop pagination info -->
      <div class="pagination-info flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
        <p class="whitespace-nowrap text-sm surface-muted">
          {{ t('pagination.showing') }}
          <span class="font-medium">{{ fromItem }}</span>
          {{ t('pagination.to') }}
          <span class="font-medium">{{ toItem }}</span>
          {{ t('pagination.of') }}
          <span class="font-medium">{{ total }}</span>
          {{ t('pagination.results') }}
        </p>

        <!-- Page size selector -->
        <div v-if="showPageSizeSelector" class="flex shrink-0 items-center space-x-2 whitespace-nowrap">
          <span class="text-sm surface-muted"
            >{{ t('pagination.perPage') }}:</span
          >
          <div class="page-size-select w-24">
            <Select
              :model-value="pageSize"
              :options="pageSizeSelectOptions"
              @update:model-value="handlePageSizeChange"
            />
          </div>
        </div>

        <div v-if="showJump" class="flex shrink-0 items-center space-x-2 whitespace-nowrap">
          <span class="text-sm surface-muted">{{ t('pagination.jumpTo') }}</span>
          <input
            v-model="jumpPage"
            type="number"
            min="1"
            :max="totalPages"
            class="input w-20 text-sm"
            :placeholder="t('pagination.jumpPlaceholder')"
            @keyup.enter="submitJump"
          />
          <button type="button" class="btn btn-ghost btn-sm" @click="submitJump">
            {{ t('pagination.jumpAction') }}
          </button>
        </div>
      </div>

      <!-- Desktop pagination buttons -->
      <nav
        class="relative z-0 inline-flex flex-wrap items-center justify-end gap-1.5 self-end"
        aria-label="Pagination"
      >
        <!-- Previous button -->
        <button
          @click="goToPage(page - 1)"
          :disabled="page === 1"
          class="pagination-button relative inline-flex items-center border border-gray-300 px-2 py-2 text-sm font-medium hover:surface-hover disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 surface-control surface-muted"
          :aria-label="t('pagination.previous')"
        >
          <Icon name="chevronLeft" size="md" />
        </button>

        <!-- Page numbers -->
        <button
          v-for="(pageNum, index) in visiblePages"
          :key="`${pageNum}-${index}`"
          @click="typeof pageNum === 'number' && goToPage(pageNum)"
          :disabled="typeof pageNum !== 'number'"
          :class="[
            'pagination-button relative inline-flex items-center border px-3 py-2 text-sm font-medium',
            pageNum === page
              ? 'z-10 border-primary-500 bg-primary-50/[0.15] text-primary-600 dark:bg-primary-900/30 dark:text-primary-400'
              : 'border-gray-300 surface-inset text-gray-700 hover:surface-hover dark:border-dark-600 dark:surface-inset dark:text-gray-300 dark:hover:surface-hover',
            typeof pageNum !== 'number' && 'cursor-default'
          ]"
          :aria-label="
            typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined
          "
          :aria-current="pageNum === page ? 'page' : undefined"
        >
          {{ pageNum }}
        </button>

        <!-- Next button -->
        <button
          @click="goToPage(page + 1)"
          :disabled="page === totalPages"
          class="pagination-button relative inline-flex items-center border border-gray-300 px-2 py-2 text-sm font-medium hover:surface-hover disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 surface-control surface-muted"
          :aria-label="t('pagination.next')"
        >
          <Icon name="chevronRight" size="md" />
        </button>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from './Select.vue'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()

interface Props {
  total: number
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false
})

const emit = defineEmits<Emits>()

const totalPages = computed(() => Math.ceil(props.total / props.pageSize))

const fromItem = computed(() => {
  if (props.total === 0) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const to = props.page * props.pageSize
  return to > props.total ? props.total : to
})

const pageSizeSelectOptions = computed(() => {
  const options = Array.from(
    new Set([
      ...getConfiguredTablePageSizeOptions(),
      normalizeTablePageSize(props.pageSize)
    ])
  ).sort((a, b) => a - b)

  return options.map((size) => ({
    value: size,
    label: String(size)
  }))
})

const jumpPage = ref('')

const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const maxVisible = 7
  const total = totalPages.value

  if (total <= maxVisible) {
    // Show all pages if total is small
    for (let i = 1; i <= total; i++) {
      pages.push(i)
    }
  } else {
    // Always show first page
    pages.push(1)

    const start = Math.max(2, props.page - 2)
    const end = Math.min(total - 1, props.page + 2)

    // Add ellipsis before if needed
    if (start > 2) {
      pages.push('...')
    }

    // Add middle pages
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }

    // Add ellipsis after if needed
    if (end < total - 1) {
      pages.push('...')
    }

    // Always show last page
    pages.push(total)
  }

  return pages
})

const goToPage = (newPage: number) => {
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (value === null || typeof value === 'boolean') return
  const newPageSize = normalizeTablePageSize(typeof value === 'string' ? parseInt(value, 10) : value)
  setPersistedPageSize(newPageSize)
  emit('update:pageSize', newPageSize)
}

const submitJump = () => {
  const value = String(jumpPage.value).trim()
  if (!value) return
  const pageNum = Number.parseInt(value, 10)
  if (Number.isNaN(pageNum)) return
  const nextPage = Math.min(Math.max(pageNum, 1), totalPages.value)
  jumpPage.value = ''
  goToPage(nextPage)
}
</script>

<style scoped>
.pagination-container {
  container-name: pagination;
  container-type: inline-size;
}
.pagination-mobile { display: none; }
.pagination-desktop { display: flex; min-width: 0; flex-direction: column; gap: 12px; }
@container pagination (min-width: 760px) {
  .pagination-desktop { flex-direction: row; align-items: center; justify-content: space-between; }
}
@container pagination (max-width: 540px) {
  .pagination-desktop { display: none; }
  .pagination-mobile { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .pagination-page-label { white-space: nowrap; font-size: 12px; }
  .pagination-mobile .pagination-button { padding-inline: 12px; white-space: nowrap; }
}
@container pagination (max-width: 360px) {
  .pagination-mobile { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .pagination-page-label { grid-column: 1 / -1; grid-row: 1; text-align: center; }
}

.pagination-button {
  @apply min-h-9 min-w-9 justify-center transition-colors;
  @apply focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 focus-visible:ring-offset-2;
  border-radius: var(--surface-radius-item);
}

.page-size-select :deep(.select-trigger) {
  @apply px-3 py-1.5 text-sm;
}
</style>
