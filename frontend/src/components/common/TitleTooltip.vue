<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue'

// Delegate legacy title attributes so dynamically rendered tables and menus use
// the same card as the rest of the application, without changing their DOM.
const id = `title-tooltip-${useId()}`
const panel = ref<HTMLElement | null>(null)
const content = ref('')
const visible = ref(false)
const position = ref({ left: '0px', top: '0px' })
let owner: Element | null = null
let originalTitle: string | null = null
let addedLabel = false
let labelValue = ''
let hovered = false
let focused = false
let dismissed = false
let observer: MutationObserver | undefined
let removalObserver: MutationObserver | undefined

function clearDescription() {
  if (!owner) return
  const descriptions = (owner.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(value => value && value !== id)
  if (descriptions.length) owner.setAttribute('aria-describedby', descriptions.join(' '))
  else owner.removeAttribute('aria-describedby')
}

function release() {
  observer?.disconnect()
  removalObserver?.disconnect()
  if (owner) {
    if (owner.getAttribute('title') === '') {
      if (originalTitle === null) owner.removeAttribute('title')
      else owner.setAttribute('title', originalTitle)
    }
    clearDescription()
    if (addedLabel && owner.getAttribute('aria-label') === labelValue) owner.removeAttribute('aria-label')
  }
  owner = null
  addedLabel = false
  hovered = focused = dismissed = false
  visible.value = false
}

function updatePosition() {
  if (!owner?.isConnected) return release()
  if (!visible.value || !panel.value) return
  const anchor = owner.getBoundingClientRect()
  if (anchor.bottom <= 0 || anchor.top >= innerHeight || anchor.right <= 0 || anchor.left >= innerWidth) return release()
  const bounds = panel.value.getBoundingClientRect()
  const inset = 12
  const left = Math.max(inset, Math.min(anchor.left + (anchor.width - bounds.width) / 2, innerWidth - bounds.width - inset))
  const top = anchor.top >= bounds.height + inset + 8
    ? anchor.top - bounds.height - 8
    : Math.min(anchor.bottom + 8, innerHeight - bounds.height - inset)
  position.value = { left: `${left}px`, top: `${Math.max(inset, top)}px` }
}

function describe() {
  if (!owner || dismissed) return
  visible.value = true
  const descriptions = new Set((owner.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean))
  descriptions.add(id)
  owner.setAttribute('aria-describedby', [...descriptions].join(' '))
  nextTick(updatePosition)
}

function activate(target: EventTarget | null, mode: 'hover' | 'focus') {
  if (!(target instanceof Element)) return
  if (target.closest('[role="tooltip"], .help-tooltip-trigger')) {
    release()
    return
  }
  const candidate = target.closest('[title]')
  if (owner?.contains(target) && (!candidate || candidate === owner || !candidate.getAttribute('title')?.trim())) {
    if (mode === 'hover') hovered = true
    else focused = true
    describe()
    return
  }
  if (!candidate) return
  const title = candidate.getAttribute('title') ?? ''
  if (!title.trim()) return
  release()
  owner = candidate
  originalTitle = title
  content.value = title
  candidate.setAttribute('title', '')
  // Keep title-only icon buttons named while the native tooltip is suppressed.
  const accessibleContent = candidate.cloneNode(true) as Element
  accessibleContent.querySelectorAll('[aria-hidden="true"], [hidden]').forEach(element => element.remove())
  const hasLabel = 'labels' in candidate && !!(candidate as HTMLInputElement).labels?.length
  addedLabel = candidate.matches('button, a, input, [role="button"]') && !candidate.hasAttribute('aria-label') && !candidate.hasAttribute('aria-labelledby') && !hasLabel && !accessibleContent.textContent?.trim()
  if (addedLabel) {
    labelValue = title
    candidate.setAttribute('aria-label', title)
  }
  hovered = mode === 'hover'
  focused = mode === 'focus'
  observer?.observe(candidate, { attributes: true, attributeFilter: ['title'] })
  removalObserver?.observe(document.body, { childList: true, subtree: true })
  describe()
}

function onOver(event: PointerEvent) {
  if (event.pointerType !== 'touch') activate(event.target, 'hover')
}
function onOut(event: PointerEvent) {
  if (!owner || (event.relatedTarget instanceof Node && owner.contains(event.relatedTarget))) return
  hovered = false
  if (!focused) release()
}
function onFocus(event: FocusEvent) { activate(event.target, 'focus') }
function onBlur(event: FocusEvent) {
  if (!owner || (event.relatedTarget instanceof Node && owner.contains(event.relatedTarget))) return
  focused = false
  if (!hovered) release()
}
function dismiss() {
  dismissed = true
  visible.value = false
  clearDescription()
}
function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') dismiss()
}

onMounted(() => {
  observer = new MutationObserver(() => {
    if (!owner) return
    originalTitle = owner.getAttribute('title')
    content.value = originalTitle ?? ''
    observer?.disconnect()
    owner.setAttribute('title', '')
    observer?.observe(owner, { attributes: true, attributeFilter: ['title'] })
    if (addedLabel && originalTitle) {
      labelValue = originalTitle
      owner.setAttribute('aria-label', originalTitle)
    }
    if (!originalTitle?.trim()) dismiss()
    else if (!dismissed) nextTick(updatePosition)
  })
  removalObserver = new MutationObserver(() => { if (owner && !owner.isConnected) release() })
  document.addEventListener('pointerover', onOver, true)
  document.addEventListener('pointerout', onOut, true)
  document.addEventListener('focusin', onFocus, true)
  document.addEventListener('focusout', onBlur, true)
  document.addEventListener('keydown', onKey)
  document.addEventListener('click', dismiss, true)
  window.addEventListener('scroll', updatePosition, true)
  window.addEventListener('resize', updatePosition)
})
onBeforeUnmount(() => {
  release()
  document.removeEventListener('pointerover', onOver, true)
  document.removeEventListener('pointerout', onOut, true)
  document.removeEventListener('focusin', onFocus, true)
  document.removeEventListener('focusout', onBlur, true)
  document.removeEventListener('keydown', onKey)
  document.removeEventListener('click', dismiss, true)
  window.removeEventListener('scroll', updatePosition, true)
  window.removeEventListener('resize', updatePosition)
})
</script>

<template>
  <Teleport to="body">
    <div v-if="visible" :id="id" ref="panel" role="tooltip" class="title-tooltip surface-panel" :style="position">{{ content }}</div>
  </Teleport>
</template>

<style scoped>
.title-tooltip {
  position: fixed;
  z-index: 100000030;
  pointer-events: none;
  width: max-content;
  max-width: min(360px, calc(100vw - 24px));
  max-height: calc(100dvh - 24px);
  overflow: hidden;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  border: 1px solid var(--surface-edge);
  padding: 12px 16px;
  font-size: 12px;
  line-height: 1.5;
}
</style>
