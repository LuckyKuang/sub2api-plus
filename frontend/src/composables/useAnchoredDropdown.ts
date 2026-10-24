import { nextTick, onBeforeUnmount, onMounted, ref, watch, type CSSProperties, type Ref } from 'vue'

/** Keep an existing dropdown aligned when its trigger lives in a scrolling toolbar. */
export function useAnchoredDropdown(anchor: Ref<HTMLElement | null>, active: () => boolean) {
  const style = ref<CSSProperties>({})
  const update = () => {
    if (!active() || !anchor.value) return
    const rect = anchor.value.getBoundingClientRect()
    const gutter = 12
    const gap = 6
    const width = Math.min(Math.max(rect.width, 240), window.innerWidth - gutter * 2)
    const below = Math.max(0, window.innerHeight - rect.bottom - gap - gutter)
    const above = Math.max(0, rect.top - gap - gutter)
    const upward = below < 160 && above > below
    style.value = {
      position: 'fixed', margin: 0, width: `${width}px`, right: 'auto',
      left: `${Math.max(gutter, Math.min(rect.left, window.innerWidth - width - gutter))}px`,
      ...(upward ? { bottom: `${window.innerHeight - rect.top + gap}px` } : { top: `${rect.bottom + gap}px` }),
      maxHeight: `${Math.min(240, upward ? above : below)}px`,
    }
  }
  watch(active, async () => { await nextTick(); update() })
  onMounted(() => {
    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, { capture: true, passive: true })
  })
  onBeforeUnmount(() => {
    window.removeEventListener('resize', update)
    window.removeEventListener('scroll', update, true)
  })
  return style
}
