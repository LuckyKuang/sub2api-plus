<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="show"
        class="modal-overlay"
        :style="zIndexStyle"
        :aria-labelledby="dialogId"
        role="dialog"
        aria-modal="true"
        @click.self="handleClose"
      >
        <!-- Modal panel -->
        <div ref="dialogRef" tabindex="-1" :class="['modal-content', placement === 'right' ? 'detail-dialog' : widthClasses]" @click.stop>
          <!-- Header -->
          <div class="modal-header">
            <h3 :id="dialogId" ref="titleRef" class="modal-title" :tabindex="titleOverflows ? 0 : undefined">
              {{ title }}
            </h3>
            <button
              v-if="showCloseButton"
              @click="emit('close')"
              class="-mr-2 shrink-0 rounded-xl p-2 transition-colors hover:surface-hover hover:text-gray-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/30 focus-visible:ring-offset-2 dark:hover:text-dark-300 dark:focus-visible:ring-offset-dark-900 surface-control surface-muted"
              aria-label="Close modal"
            >
              <Icon name="x" size="md" />
            </button>
          </div>

          <!-- Body -->
          <div ref="modalBodyRef" class="modal-body">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="$slots.footer" class="modal-footer">
            <slot name="footer"></slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
let dialogIdCounter = 0
</script>

<script setup lang="ts">
import { computed, watch, ref, nextTick } from 'vue'
import { useResizeObserver } from '@vueuse/core'
import { useDialogFocus } from '@/composables/useDialogFocus'
import Icon from '@/components/icons/Icon.vue'

// 生成唯一ID以避免多个对话框时ID冲突
const dialogId = `modal-title-${++dialogIdCounter}`

// 焦点管理
const dialogRef = ref<HTMLElement | null>(null)
const modalBodyRef = ref<HTMLElement | null>(null)
const titleRef = ref<HTMLElement | null>(null)
const titleOverflows = ref(false)

function updateTitleOverflow() {
  const title = titleRef.value
  titleOverflows.value = !!title && title.scrollHeight > title.clientHeight + 1
}

useResizeObserver(titleRef, updateTitleOverflow)

type DialogWidth = 'narrow' | 'normal' | 'wide' | 'extra-wide' | 'full'

interface Props {
  show: boolean
  title: string
  width?: DialogWidth
  placement?: 'center' | 'right'
  closeOnEscape?: boolean
  closeOnClickOutside?: boolean
  showCloseButton?: boolean
  zIndex?: number
}

interface Emits {
  (e: 'close'): void
}

const props = withDefaults(defineProps<Props>(), {
  width: 'normal',
  closeOnEscape: true,
  closeOnClickOutside: false,
  showCloseButton: true,
  zIndex: 50
})

const emit = defineEmits<Emits>()
useDialogFocus(() => props.show, dialogRef, () => emit('close'), () => props.closeOnEscape)

// Custom z-index style (overrides the default z-50 from CSS)
const zIndexStyle = computed(() => {
  return props.zIndex !== 50 ? { zIndex: props.zIndex } : undefined
})

const widthClasses = computed(() => {
  // Width guidance: narrow=confirm/short prompts, normal=standard forms,
  // wide=multi-section forms or rich content, extra-wide=analytics/tables,
  // full=full-screen or very dense layouts.
  const widths: Record<DialogWidth, string> = {
    narrow: 'max-w-md',
    normal: 'max-w-lg',
    wide: 'w-full sm:max-w-2xl md:max-w-3xl lg:max-w-4xl',
    'extra-wide': 'w-full sm:max-w-3xl md:max-w-4xl lg:max-w-5xl xl:max-w-6xl',
    full: 'w-full sm:max-w-4xl md:max-w-5xl lg:max-w-6xl xl:max-w-7xl'
  }
  return widths[props.width]
})

const handleClose = () => {
  if (props.closeOnClickOutside) {
    emit('close')
  }
}

watch(() => props.show, async show => {
  if (!show) return
  await nextTick()
  if (modalBodyRef.value) modalBodyRef.value.scrollTop = 0
}, { immediate: true })

watch(() => [props.show, props.title], async () => {
  await nextTick()
  updateTitleOverflow()
}, { immediate: true })
</script>

<style scoped>
/* Exceptionally long titles scroll on their own so the close button, form
   body and footer retain usable space even in short windows. */
.modal-title {
  max-height: 30dvh;
  overflow-y: auto;
  overscroll-behavior: contain;
}
</style>
