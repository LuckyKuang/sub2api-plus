<template>
  <Teleport to="body">
    <div v-if="lines.length" ref="tooltip" class="matrix-floating-tooltip surface-panel" role="tooltip">
      <span
        v-for="(line, index) in lines"
        :key="index"
        class="matrix-floating-tooltip-line"
        :class="{ 'matrix-floating-tooltip-title': index === 0 }"
      >{{ line }}</span>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref } from 'vue'

const tooltip = ref<HTMLElement | null>(null)
const lines = ref<string[]>([])
let frame = 0
let point = { x: 0, y: 0 }
let alive = true

function position() {
  frame = 0
  const element = tooltip.value
  if (!element) return
  const { width, height } = element.getBoundingClientRect()
  const left = Math.max(12, Math.min(window.innerWidth - width - 12, point.x - width / 2))
  const above = point.y - height - 12
  const top = Math.max(12, Math.min(window.innerHeight - height - 12, above >= 12 ? above : point.y + 12))
  // Pointer coordinates never enter the parent component's reactive render graph.
  element.style.left = `${left}px`
  element.style.top = `${top}px`
}
function schedulePosition() {
  if (!frame) frame = requestAnimationFrame(position)
}
function move(event: MouseEvent) {
  point = { x: event.clientX, y: event.clientY }
  if (lines.value.length) schedulePosition()
}
async function show(event: MouseEvent | FocusEvent, content: string[]) {
  if ('clientX' in event) point = { x: event.clientX, y: event.clientY }
  else {
    const rect = (event.target as HTMLElement | null)?.getBoundingClientRect()
    if (!rect) return
    point = { x: rect.left + rect.width / 2, y: rect.top }
  }
  lines.value = content
  await nextTick()
  if (alive && lines.value.length) schedulePosition()
}
function hide() {
  lines.value = []
  if (frame) cancelAnimationFrame(frame)
  frame = 0
}
onBeforeUnmount(() => { alive = false; hide() })
defineExpose({ show, move, hide })
</script>

<style scoped>
.matrix-floating-tooltip {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  min-width: 11.5rem;
  max-width: min(18rem, calc(100vw - 1.5rem));
  max-height: calc(100vh - 1.5rem);
  overflow: hidden;
  border-radius: var(--surface-radius-panel);
  border: 1px solid rgb(229 231 235);
  background: var(--surface-panel);
  padding: 0.5rem 0.625rem;
  box-shadow: 0 18px 40px -12px rgb(0 0 0 / 0.28);
  overflow-wrap: anywhere;
}
:global(.dark) .matrix-floating-tooltip {
  border-color: rgb(55 65 81);
  color: rgb(229 231 235);
}
.matrix-floating-tooltip-line {
  display: block;
  font-size: 11px;
  line-height: 1.45;
  color: rgb(75 85 99);
}
:global(.dark) .matrix-floating-tooltip-line { color: rgb(209 213 219); }
.matrix-floating-tooltip-title {
  margin-bottom: 0.2rem;
  font-weight: 600;
  color: rgb(17 24 39);
}
:global(.dark) .matrix-floating-tooltip-title { color: rgb(243 244 246); }
</style>
