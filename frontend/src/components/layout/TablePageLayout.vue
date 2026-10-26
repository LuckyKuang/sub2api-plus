<template>
  <div ref="layoutRef" class="table-page-layout" :class="{ 'mobile-mode': isMobile }" :style="{ '--table-page-height': availableHeight }">
    <!-- 固定区域：操作按钮 -->
    <div v-if="$slots.actions" class="layout-section-fixed">
      <slot name="actions" />
    </div>

    <!-- 固定区域：搜索和过滤器 -->
    <div v-if="$slots.filters" class="layout-section-fixed">
      <slot name="filters" />
    </div>

    <!-- 滚动区域：表格 -->
    <div class="layout-section-scrollable">
      <div class="card table-scroll-container workspace-shell">
        <slot name="table" />
      </div>
    </div>

    <!-- 固定区域：分页器 -->
    <div v-if="$slots.pagination" class="layout-section-fixed table-page-pagination">
      <slot name="pagination" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const isMobile = ref(false)
const layoutRef = ref<HTMLElement | null>(null)
const availableHeight = ref('auto')
let observer: ResizeObserver | undefined

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
  window.addEventListener('resize', checkMobile)
  if (typeof ResizeObserver !== 'undefined' && layoutRef.value?.parentElement) {
    observer = new ResizeObserver(checkMobile)
    observer.observe(layoutRef.value.parentElement)
  }
})

onUnmounted(() => {
  window.removeEventListener('resize', checkMobile)
  observer?.disconnect()
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
