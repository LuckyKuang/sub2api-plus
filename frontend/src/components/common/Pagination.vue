<template>
  <div
    :class="[
      'pagination-container min-w-0',
      embedded ? 'pagination-embedded' : 'pagination-card card px-4 py-3 sm:px-6'
    ]"
  >
    <!-- Narrow containers keep numbered pages, with a tighter window -->
    <nav class="pagination-mobile" aria-label="Pagination">
      <button
        @click="goToPage(page - 1)"
        :disabled="disabled || page <= 1"
        class="pagination-button"
        :aria-label="t('pagination.previous')"
      >
        <Icon name="chevronLeft" size="md" />
      </button>
      <button
        v-for="(pageNum, index) in compactPages"
        :key="`m-${pageNum}-${index}`"
        @click="typeof pageNum === 'number' && goToPage(pageNum)"
        :disabled="typeof pageNum !== 'number' || (disabled && pageNum !== page)"
        :class="[
          'pagination-button',
          pageNum === page && 'pagination-button--current',
          typeof pageNum !== 'number' && 'pagination-button--gap'
        ]"
        :aria-label="
          typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined
        "
        :aria-current="pageNum === page ? 'page' : undefined"
      >
        {{ pageNum }}
      </button>
      <button
        @click="goToPage(page + 1)"
        :disabled="disabled || page >= totalPages"
        class="pagination-button"
        :aria-label="t('pagination.next')"
      >
        <Icon name="chevronRight" size="md" />
      </button>
    </nav>

    <div class="pagination-desktop">
      <!-- Desktop pagination info -->
      <div class="pagination-info flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
        <p class="whitespace-nowrap text-sm surface-muted">
          <slot name="info">
            <template v-if="totalKnown">
              {{ t('pagination.showing') }}
              <span class="font-medium">{{ fromItem }}</span>
              {{ t('pagination.to') }}
              <span class="font-medium">{{ toItem }}</span>
              {{ t('pagination.of') }}
              <span class="font-medium">{{ total }}</span>
              {{ t('pagination.results') }}
            </template>
            <template v-else>{{ t('pagination.pageOf', { page, total: totalPages }) }}</template>
          </slot>
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
              :disabled="disabled"
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
        class="pagination-pages"
        aria-label="Pagination"
      >
        <!-- Previous button -->
        <button
          @click="goToPage(page - 1)"
          :disabled="disabled || page <= 1"
          class="pagination-button"
          :aria-label="t('pagination.previous')"
        >
          <Icon name="chevronLeft" size="md" />
        </button>

        <!-- Page numbers -->
        <button
          v-for="(pageNum, index) in visiblePages"
          :key="`${pageNum}-${index}`"
          @click="typeof pageNum === 'number' && goToPage(pageNum)"
          :disabled="typeof pageNum !== 'number' || (disabled && pageNum !== page)"
          :class="[
            'pagination-button',
            pageNum === page && 'pagination-button--current',
            typeof pageNum !== 'number' && 'pagination-button--gap'
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
          :disabled="disabled || page >= totalPages"
          class="pagination-button"
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
  /** Total rows; `null` when the source only reports `hasMore` (cursor-style lists). */
  total: number | null
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
  /** Whether a page after `page` exists; only read when `total` is `null`. */
  hasMore?: boolean
  /** Render without the card shell, for pagers inside a card or dialog. */
  embedded?: boolean
  /** Lock navigation while the owner is loading or submitting. */
  disabled?: boolean
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false,
  hasMore: false,
  embedded: false,
  disabled: false
})

const emit = defineEmits<Emits>()

const totalKnown = computed(() => typeof props.total === 'number')

// Unknown totals expose every page reached so far plus the next one.
const totalPages = computed(() => {
  if (typeof props.total !== 'number') return Math.max(1, props.page + (props.hasMore ? 1 : 0))
  return Math.max(1, Math.ceil(props.total / props.pageSize))
})

const fromItem = computed(() => {
  if (!props.total) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const total = props.total ?? 0
  const to = props.page * props.pageSize
  return to > total ? total : to
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

// First, last, and `siblings` pages either side of the current one, with
// "..." standing in for each skipped run.
function pageWindow(maxVisible: number, siblings: number): (number | string)[] {
  const total = totalPages.value
  if (total <= maxVisible) return Array.from({ length: total }, (_, i) => i + 1)
  const pages: (number | string)[] = [1]
  const start = Math.max(2, props.page - siblings)
  const end = Math.min(total - 1, props.page + siblings)
  if (start > 2) pages.push('...')
  for (let i = start; i <= end; i++) pages.push(i)
  if (end < total - 1) pages.push('...')
  pages.push(total)
  return pages
}

const visiblePages = computed(() => pageWindow(7, 2))
const compactPages = computed(() => pageWindow(5, 1))

const goToPage = (newPage: number) => {
  if (props.disabled) return
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (props.disabled || value === null || typeof value === 'boolean') return
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
  .pagination-mobile { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 4px; }
}

.page-size-select :deep(.select-trigger) {
  min-height: var(--control-height-sm);
  padding-block: 5px;
}
</style>
