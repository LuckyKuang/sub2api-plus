<template>
  <button
    ref="triggerRef"
    type="button"
    class="table-action table-action-neutral"
    :class="{ 'table-action-active': open }"
    aria-haspopup="menu"
    :aria-expanded="open"
    @click="toggle"
  >
    <Icon name="more" size="sm" />
    <span class="text-xs">{{ label || t('common.more') }}</span>
  </button>
  <Teleport to="body">
    <div
      v-if="open"
      ref="menuRef"
      role="menu"
      class="action-menu-content surface-panel"
      :style="{ top: `${position.top}px`, left: `${position.left}px` }"
      @keydown="onMenuKeydown"
    >
      <template v-for="item in visibleItems" :key="item.key">
        <button
          v-if="item.mutates"
          v-support-readonly
          type="button"
          role="menuitem"
          class="action-menu-item"
          :class="item.tone ? `action-menu-item--${item.tone}` : undefined"
          :disabled="item.disabled"
          :title="item.title"
          @click="choose(item)"
        >
          <Icon v-if="item.icon" :name="item.icon" size="sm" />
          <span>{{ item.label }}</span>
        </button>
        <button
          v-else
          type="button"
          role="menuitem"
          class="action-menu-item"
          :class="item.tone ? `action-menu-item--${item.tone}` : undefined"
          :disabled="item.disabled"
          :title="item.title"
          @click="choose(item)"
        >
          <Icon v-if="item.icon" :name="item.icon" size="sm" />
          <span>{{ item.label }}</span>
        </button>
      </template>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { supportReadonly as vSupportReadonly } from '@/directives/supportReadonly'

export interface ActionMenuItem {
  key: string
  label: string
  icon?: InstanceType<typeof Icon>['$props']['name']
  /** danger and warning tint the label; neutral items take the muted ink. */
  tone?: 'danger' | 'warning'
  disabled?: boolean
  /** Tooltip, typically explaining why the action is disabled. */
  title?: string
  hidden?: boolean
  /** Mutating actions are locked while an administrator views as the user. */
  mutates?: boolean
}

const props = defineProps<{ items: ActionMenuItem[]; label?: string }>()
const emit = defineEmits<{ select: [key: string] }>()
const { t } = useI18n()

const VIEWPORT_PADDING = 8
const GAP = 4

const open = ref(false)
const triggerRef = ref<HTMLButtonElement | null>(null)
const menuRef = ref<HTMLDivElement | null>(null)
const position = ref({ top: 0, left: 0 })
const visibleItems = computed(() => props.items.filter(item => !item.hidden))

function menuButtons(): HTMLButtonElement[] {
  return Array.from(menuRef.value?.querySelectorAll<HTMLButtonElement>('.action-menu-item:not(:disabled)') ?? [])
}

function place() {
  const trigger = triggerRef.value
  const menu = menuRef.value
  if (!trigger || !menu) return
  const anchor = trigger.getBoundingClientRect()
  const width = menu.offsetWidth
  const height = menu.offsetHeight
  const maxLeft = window.innerWidth - width - VIEWPORT_PADDING
  const left = Math.max(VIEWPORT_PADDING, Math.min(anchor.right - width, maxLeft))
  const below = anchor.bottom + GAP
  const fitsBelow = below + height <= window.innerHeight - VIEWPORT_PADDING
  const top = fitsBelow ? below : Math.max(VIEWPORT_PADDING, anchor.top - GAP - height)
  position.value = { top, left }
}

function onPointerDown(event: Event) {
  const target = event.target as Node
  if (menuRef.value?.contains(target) || triggerRef.value?.contains(target)) return
  close()
}

function onViewportChange() {
  close()
}

function listen(active: boolean) {
  const method = active ? 'addEventListener' : 'removeEventListener'
  document[method]('pointerdown', onPointerDown, true)
  window[method]('resize', onViewportChange)
  window[method]('scroll', onViewportChange, true)
}

async function show() {
  open.value = true
  listen(true)
  await nextTick()
  place()
  menuButtons()[0]?.focus()
}

function close(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  listen(false)
  if (restoreFocus) triggerRef.value?.focus()
}

function toggle() {
  if (open.value) close()
  else void show()
}

function choose(item: ActionMenuItem) {
  close(true)
  emit('select', item.key)
}

function onMenuKeydown(event: KeyboardEvent) {
  const buttons = menuButtons()
  const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
  if (event.key === 'Escape') {
    event.preventDefault()
    close(true)
  } else if (event.key === 'Tab') {
    close()
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!buttons.length) return
    const step = event.key === 'ArrowDown' ? 1 : -1
    buttons[(index + step + buttons.length) % buttons.length].focus()
  } else if (event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    buttons[event.key === 'Home' ? 0 : buttons.length - 1]?.focus()
  }
}

onBeforeUnmount(() => listen(false))
</script>
