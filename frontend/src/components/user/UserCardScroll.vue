<template>
  <div class="user-card-scroll-frame">
    <div
      ref="viewport"
      class="user-card-scroll-viewport"
      :style="{ '--user-card-scrollbar': `${scrollbarWidth}px` }"
      :aria-label="label"
      :role="label ? 'region' : undefined"
      tabindex="0"
    >
      <div class="user-card-scroll-content"><slot /></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{ label?: string; resetKey?: string | number }>()
const viewport = ref<HTMLElement | null>(null)
const scrollbarWidth = ref(0)
let resize: ResizeObserver | undefined

function measureScrollbar() {
  const element = viewport.value
  if (element) scrollbarWidth.value = Math.max(0, element.offsetWidth - element.clientWidth)
}

watch(() => props.resetKey, () => {
  if (viewport.value) viewport.value.scrollTop = 0
})

onMounted(() => {
  measureScrollbar()
  resize = new ResizeObserver(measureScrollbar)
  if (viewport.value) resize.observe(viewport.value)
})
onBeforeUnmount(() => resize?.disconnect())
</script>

<style scoped>
.user-card-scroll-frame { min-width: 0; border-radius: 16px; }
.user-card-scroll-content { display: flex; flex-direction: column; gap: 16px; min-width: 0; }
.user-card-scroll-content > :deep(*) { flex-shrink: 0; margin-block: 0; }
@media (min-width: 1024px) {
  .user-card-scroll-frame { display: flex; flex: 1 1 0%; min-height: 0; }
  .user-card-scroll-viewport {
    flex: none;
    width: calc(100% + var(--user-card-scrollbar, 0px));
    height: 100%;
    min-height: 0;
    overflow: auto;
    overscroll-behavior: contain;
    scrollbar-gutter: stable;
    border-radius: 16px;
  }
}
</style>
