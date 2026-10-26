<script setup lang="ts">
import { onBeforeUnmount, onMounted, onUpdated, ref, useId, useTemplateRef, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  content?: string
  trigger?: 'hover' | 'click'
  widthClass?: string
}>(), {
  trigger: 'hover',
  widthClass: 'w-64',
})

const show = ref(false)
const tooltipId = `help-tooltip-${useId()}`
const triggerRef = useTemplateRef<HTMLElement>('trigger')
const tooltipRef = useTemplateRef<HTMLElement>('tooltip')
const tooltipStyle = ref({ top: '0px', left: '0px' })
const placement = ref<'above' | 'below'>('above')
const hasInteractiveTrigger = ref(false)
let describedElement: HTMLElement | null = null
let observer: ResizeObserver | undefined

const interactiveSelector = 'a[href], button, input, select, textarea, summary, [tabindex]'

function syncTriggerFocus() {
  hasInteractiveTrigger.value = !!triggerRef.value?.querySelector(interactiveSelector)
}

function clearDescription() {
  if (!describedElement) return
  const ids = (describedElement.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(id => id && id !== tooltipId)
  if (ids.length) describedElement.setAttribute('aria-describedby', ids.join(' '))
  else describedElement.removeAttribute('aria-describedby')
  describedElement = null
}

function openTooltip() {
  clearDescription()
  describedElement = triggerRef.value?.querySelector<HTMLElement>(interactiveSelector) ?? triggerRef.value
  if (describedElement) {
    const ids = (describedElement.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean)
    describedElement.setAttribute('aria-describedby', [...ids, tooltipId].join(' '))
  }
  show.value = true
  nextTick(updatePosition)
}

function closeTooltip() {
  show.value = false
  clearDescription()
}

function onEnter() {
  if (props.trigger !== 'hover') return
  openTooltip()
}

function isInside(container: HTMLElement | null, target: EventTarget | null): boolean {
  return target instanceof Node && !!container?.contains(target)
}

// 悬停模式下指针在触发图标与提示框之间往返时保持打开，便于选中提示里的文字。
function onLeave(event: MouseEvent) {
  if (props.trigger !== 'hover') return
  if (isInside(tooltipRef.value, event.relatedTarget)) return
  if (isInside(triggerRef.value, document.activeElement) || isInside(tooltipRef.value, document.activeElement)) return
  closeTooltip()
}

function onTooltipLeave(event: MouseEvent) {
  if (props.trigger !== 'hover') return
  if (isInside(triggerRef.value, event.relatedTarget)) return
  if (isInside(triggerRef.value, document.activeElement) || isInside(tooltipRef.value, document.activeElement)) return
  closeTooltip()
}

function onFocusIn() {
  if (props.trigger === 'hover') openTooltip()
}

function onFocusOut(event: FocusEvent) {
  if (props.trigger !== 'hover') return
  if (isInside(triggerRef.value, event.relatedTarget) || isInside(tooltipRef.value, event.relatedTarget)) return
  closeTooltip()
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.target !== triggerRef.value || !['Enter', ' '].includes(event.key)) return
  event.preventDefault()
  if (props.trigger === 'click' && show.value) closeTooltip()
  else openTooltip()
}

function onClick(event: MouseEvent) {
  if (props.trigger !== 'click') return
  event.stopPropagation()
  if (show.value) {
    closeTooltip()
    return
  }
  openTooltip()
}

function onDocumentClick(event: MouseEvent) {
  if (props.trigger !== 'click' || !show.value) return
  const target = event.target as Node | null
  if (!target) return
  if (triggerRef.value?.contains(target) || tooltipRef.value?.contains(target)) return
  closeTooltip()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (show.value && event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeTooltip()
  }
}

function onViewportChange() {
  if (!show.value) return
  updatePosition()
}

function updatePosition() {
  const el = triggerRef.value
  const panel = tooltipRef.value
  if (!show.value || !el || !panel) return
  const rect = el.getBoundingClientRect()
  const panelRect = panel.getBoundingClientRect()
  const gutter = 12
  const gap = 8
  const spaceAbove = rect.top - gap - gutter
  const spaceBelow = window.innerHeight - rect.bottom - gap - gutter
  placement.value = panelRect.height <= spaceAbove || spaceAbove > spaceBelow ? 'above' : 'below'
  const top = placement.value === 'above' ? rect.top - gap - panelRect.height : rect.bottom + gap
  const left = rect.left + rect.width / 2 - panelRect.width / 2
  tooltipStyle.value = {
    top: `${Math.max(gutter, Math.min(top, window.innerHeight - gutter - panelRect.height))}px`,
    left: `${Math.max(gutter, Math.min(left, window.innerWidth - gutter - panelRect.width))}px`,
  }
}

onMounted(() => {
  syncTriggerFocus()
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onDocumentKeydown, true)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(onViewportChange)
    if (triggerRef.value) observer.observe(triggerRef.value)
    if (tooltipRef.value) observer.observe(tooltipRef.value)
  }
})

onUpdated(syncTriggerFocus)

onBeforeUnmount(() => {
  clearDescription()
  observer?.disconnect()
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onDocumentKeydown, true)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div
    ref="trigger"
    title=""
    class="help-tooltip-trigger group relative ml-1 inline-flex items-center rounded-full align-middle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50"
    :tabindex="hasInteractiveTrigger ? undefined : 0"
    :role="!hasInteractiveTrigger ? 'button' : undefined"
    :aria-label="!hasInteractiveTrigger ? t('common.info') : undefined"
    :aria-expanded="!hasInteractiveTrigger ? show : undefined"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
    @keydown="onTriggerKeydown"
    @click="onClick"
  >
    <!-- Trigger Icon -->
    <slot name="trigger">
      <svg
        class="h-4 w-4 cursor-help text-gray-400 transition-colors hover:text-primary-600 dark:text-gray-500 dark:hover:text-primary-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    </slot>

    <!-- Teleport to body to escape modal overflow clipping -->
    <Teleport to="body">
      <!-- before: 伪元素向下延伸一段透明区域，盖住提示框与触发图标之间的空隙，让指针能连续移入提示框。 -->
      <div
        ref="tooltip"
        v-show="show"
        :id="tooltipId"
        role="tooltip"
        :data-placement="placement"
        :class="[
          'help-tooltip-panel fixed z-[99999] surface-panel p-3 text-xs leading-relaxed shadow-xl ring-1 ring-white/10 selection:bg-primary-200 selection:text-gray-900 dark:selection:bg-primary-200 dark:selection:text-gray-900',
          props.widthClass,
        ]"
        :style="tooltipStyle"
        @mouseleave="onTooltipLeave"
        @focusout="onFocusOut"
      >
        <button
          v-if="props.trigger === 'click'"
          type="button"
          class="absolute right-1.5 top-1.5 rounded-lg p-1 transition-colors hover:surface-hover surface-control surface-muted"
          aria-label="Close"
          @click.stop="closeTooltip"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
        <div class="help-tooltip-content" :class="{ 'pr-5': props.trigger === 'click' }"><slot>{{ content }}</slot></div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* Keep the hover bridge between the trigger and the solid tooltip panel. */
.help-tooltip-panel {
  border-radius: var(--surface-radius-panel, 16px);
  max-width: calc(100vw - 24px);
}
.help-tooltip-content {
  max-height: calc(100dvh - 48px);
  overflow-y: auto;
  overflow-wrap: anywhere;
}
.help-tooltip-panel::before {
  content: '';
  position: absolute;
  inset: 100% 0 auto;
  height: 0.75rem;
}
.help-tooltip-panel[data-placement='below']::before {
  inset: auto 0 100%;
}
</style>
