<template>
  <div ref="layoutRef" class="table-page-layout workspace-page" :class="{ 'mobile-mode': isMobile }" :style="{ '--table-page-height': availableHeight }">
    <!-- 固定区域：操作按钮 -->
    <div v-if="$slots.actions" class="layout-section-fixed workspace-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区域：搜索和过滤器 -->
    <div v-if="$slots.filters" ref="filtersSectionRef" class="layout-section-fixed workspace-fixed">
      <button
        v-if="shouldCollapseMobileFilters"
        type="button"
        class="mobile-filters-toggle"
        :aria-expanded="mobileFiltersExpanded"
        @click="mobileFiltersExpanded = !mobileFiltersExpanded"
      >
        <span class="flex items-center gap-2 font-medium">
          <Icon name="filter" size="sm" />
          <span>{{ filtersLabel }}</span>
          <span v-if="activeFilterCount > 0" class="badge badge-primary">{{ activeFilterCount }}</span>
        </span>
        <span class="flex items-center gap-1 text-xs surface-muted">
          <span>{{ mobileFiltersExpanded ? hideFiltersLabel : showFiltersLabel }}</span>
          <Icon
            name="chevronDown"
            size="sm"
            class="transition-transform duration-200"
            :class="{ 'rotate-180': mobileFiltersExpanded }"
          />
        </span>
      </button>
      <div
        ref="filtersBodyRef"
        :class="{ 'mt-3': shouldCollapseMobileFilters && mobileFiltersExpanded }"
        v-show="!shouldCollapseMobileFilters || mobileFiltersExpanded"
      >
        <slot name="filters" />
      </div>
    </div>

    <!-- 滚动区域：表格 -->
    <div class="layout-section-scrollable workspace-scroll">
      <div class="card table-scroll-container workspace-shell workspace-table-card">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区域：分页器 -->
    <div v-if="$slots.pagination" class="layout-section-fixed table-page-pagination workspace-fixed">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, onUpdated, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

let translate: ((key: string) => string) | null = null
try {
  const { t } = useI18n()
  translate = (key: string) => t(key)
} catch {
  translate = null
}

const filtersLabel = computed(() => translate ? translate('common.filters') : 'Filters')
const showFiltersLabel = computed(() => translate ? translate('common.showFilters') : 'Show filters')
const hideFiltersLabel = computed(() => translate ? translate('common.hideFilters') : 'Hide filters')

const isMobile = ref(false)
const layoutRef = ref<HTMLElement | null>(null)
const filtersSectionRef = ref<HTMLElement | null>(null)
const filtersBodyRef = ref<HTMLElement | null>(null)
const availableHeight = ref('auto')
const filterFieldCount = ref(0)
const activeFilterCount = ref(0)
const mobileFiltersExpanded = ref(false)
let observer: ResizeObserver | undefined
let filterMutationObserver: MutationObserver | undefined

const shouldCollapseMobileFilters = computed(() => isMobile.value && filterFieldCount.value >= 3)

const inspectFilters = () => {
  const container = filtersBodyRef.value
  if (!container) {
    filterFieldCount.value = 0
    activeFilterCount.value = 0
    return
  }
  const fields = container.querySelectorAll('.filter-field')
  filterFieldCount.value = fields.length
  let active = 0
  fields.forEach((field) => {
    const input = field.querySelector<HTMLInputElement>('input')
    if (input && input.value.trim().length > 0) {
      active += 1
      return
    }
    const clearBtn = field.querySelector('[aria-label="Clear"], [data-clear], .select-clear')
    if (clearBtn) active += 1
  })
  activeFilterCount.value = active
}

const checkMobile = () => {
  isMobile.value = window.innerWidth < 1024
  if (!layoutRef.value || isMobile.value) { availableHeight.value = 'auto'; return }
  const top = layoutRef.value.getBoundingClientRect().top + window.scrollY
  const main = layoutRef.value.closest('main')
  const shell = layoutRef.value.closest('.app-layout')
  const padding = shell ? parseFloat(getComputedStyle(shell).paddingBottom) : main ? parseFloat(getComputedStyle(main).paddingBottom) : 24
  availableHeight.value = `${Math.max(320, window.innerHeight - top - padding)}px`
}

onMounted(() => {
  checkMobile()
  nextTick(inspectFilters)
  window.addEventListener('resize', checkMobile)
  if (typeof ResizeObserver !== 'undefined' && layoutRef.value?.parentElement) {
    observer = new ResizeObserver(checkMobile)
    observer.observe(layoutRef.value.parentElement)
  }
  if (typeof MutationObserver !== 'undefined' && filtersBodyRef.value) {
    filterMutationObserver = new MutationObserver(inspectFilters)
    filterMutationObserver.observe(filtersBodyRef.value, { childList: true, subtree: true, attributes: true })
  }
})

onUpdated(() => {
  inspectFilters()
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
  observer?.disconnect()
  filterMutationObserver?.disconnect()
})
</script>

<style scoped>
/* 桌面端：Flexbox 布局 */
.table-page-layout {
  @apply flex flex-col;
  gap: var(--shell-gap, 24px);
  height: var(--table-page-height, auto);
}

.layout-section-fixed {
  @apply flex-shrink-0;
}

.mobile-filters-toggle {
  display: flex;
  width: 100%;
  min-height: var(--control-height);
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 10px;
  border: 1px solid var(--surface-edge);
  border-radius: var(--surface-radius-control);
  background-color: var(--surface-soft);
  color: var(--surface-text);
  font-size: 13px;
  transition: background-color 150ms, border-color 150ms;
}

.mobile-filters-toggle:hover {
  background-color: var(--surface-hover);
  border-color: var(--console-control-hover-edge);
}

.table-page-pagination { container-name: table-page-pagination; container-type: inline-size; }

.layout-section-scrollable {
  @apply flex-1 min-h-0 flex flex-col;
}

/* 表格滚动容器 - 增强版表体滚动方案 */
.table-scroll-container {
  @apply flex flex-col overflow-hidden h-full;
  border-radius: var(--surface-radius);
}

.table-scroll-container :deep(.table-wrapper) {
  border-radius: inherit;
  @apply flex-1 overflow-x-auto overflow-y-auto;
  /* 确保横向滚动条显示在最底部 */
  scrollbar-gutter: stable;
}

.table-scroll-container :deep(table) {
  @apply w-full;
  min-width: max-content; /* 关键：确保表格宽度根据内容撑开，从而触发横向滚动 */
  display: table; /* 使用标准 table 布局以支持 sticky 列 */
}

.table-scroll-container :deep(th) {
  @apply px-5 py-4 text-left text-sm font-medium; color: var(--surface-muted);
}

.table-scroll-container :deep(td) {
  @apply px-5 py-4 text-sm; color: var(--surface-muted);
}

/* 移动端：恢复正常滚动 */
.table-page-layout.mobile-mode .table-scroll-container {
  @apply h-auto overflow-visible border-none shadow-none bg-transparent;
}

.table-page-layout.mobile-mode .layout-section-scrollable {
  @apply flex-none min-h-fit;
}

.table-page-layout.mobile-mode .table-scroll-container :deep(table) {
  @apply flex-none;
  display: table;
  min-width: 100%;
}
</style>
