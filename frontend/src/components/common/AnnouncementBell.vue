<template>
  <div class="relative">
    <!-- 铃铛按钮：面板像用户菜单一样贴着按钮展开 -->
    <button
      ref="buttonRef"
      type="button"
      @click="togglePanel"
      class="relative flex h-9 w-9 items-center justify-center rounded-lg transition-colors hover:surface-hover surface-control surface-muted"
      :class="{ 'text-primary-600 dark:text-primary-400': unreadCount > 0 }"
      :aria-label="t('announcements.title')"
      :aria-expanded="isOpen"
      aria-controls="announcement-panel"
    >
      <Icon name="bell" size="md" />
      <!-- 未读标记 -->
      <span
        v-if="unreadCount > 0"
        class="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-rose-500"
      ></span>
    </button>

    <!-- 公告列表 -->
    <Teleport to="body">
      <transition name="dropdown">
        <div
          v-if="isOpen"
          id="announcement-panel"
          ref="panelRef"
          class="dropdown announcement-dropdown"
          :style="panelStyle"
          role="dialog"
          tabindex="-1"
          :aria-label="t('announcements.title')"
          @keydown.esc.stop="closePanelAndFocus"
        >
          <div class="announcement-dropdown-header">
            <div class="min-w-0">
              <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('announcements.title') }}</p>
              <p v-if="unreadCount > 0" class="mt-0.5 text-xs surface-muted">
                <span class="font-medium">{{ unreadCount }}</span>
                {{ t('announcements.unread') }}
              </p>
            </div>
            <button
              v-if="unreadCount > 0"
              v-support-readonly
              type="button"
              @click="markAllAsRead"
              :disabled="loading"
              class="btn btn-ghost btn-sm shrink-0"
            >
              {{ t('announcements.markAllRead') }}
            </button>
          </div>

          <!-- Loading -->
          <div v-if="loading" class="flex items-center justify-center py-12">
            <LoadingSpinner />
          </div>

          <!-- Announcements List -->
          <div v-else-if="announcements.length > 0" class="announcement-dropdown-list">
            <button
              v-for="item in announcements"
              :key="item.id"
              type="button"
              class="announcement-dropdown-item"
              :class="{ 'is-unread': !item.read_at }"
              data-testid="announcement-row"
              @click="openDetail(item)"
            >
              <span class="announcement-dropdown-icon surface-inset surface-muted">
                <Icon v-if="!item.read_at" name="infoCircle" size="sm" />
                <Icon v-else name="check" size="sm" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm text-gray-900 dark:text-white" :class="item.read_at ? 'font-normal' : 'font-semibold'">
                  {{ item.title }}
                </span>
                <span class="mt-0.5 flex items-center gap-2 text-xs surface-muted">
                  <time>{{ formatRelativeTime(item.created_at) }}</time>
                  <span v-if="!item.read_at" class="inline-flex items-center gap-1">
                    <span class="console-data-mark h-1.5 w-1.5 rounded-full bg-rose-500"></span>
                    {{ t('announcements.unread') }}
                  </span>
                </span>
              </span>
              <Icon name="chevronRight" size="sm" class="flex-shrink-0 surface-muted" />
            </button>
          </div>

          <!-- Empty State -->
          <div v-else class="px-4 py-6">
            <EmptyState
              size="sm"
              :title="t('announcements.empty')"
              :description="t('announcements.emptyDescription')"
            />
          </div>
        </div>
      </transition>
    </Teleport>

    <!-- 公告详情 -->
    <Teleport to="body">
      <BaseDialog
        :show="detailModalOpen && !!selectedAnnouncement"
        :title="selectedAnnouncement?.title || ''"
        width="wide"
        :z-index="110"
        @close="closeDetail"
      >
        <p class="mb-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm surface-muted">
          <span class="flex items-center gap-1.5">
            <Icon name="clock" size="sm" />
            <time>{{ selectedAnnouncement ? formatRelativeWithDateTime(selectedAnnouncement.created_at) : '' }}</time>
          </span>
          <span class="flex items-center gap-1.5">
            <Icon name="eye" size="sm" />
            {{ selectedAnnouncement?.read_at ? t('announcements.read') : t('announcements.unread') }}
          </span>
        </p>
        <div
          class="markdown-body prose prose-sm max-w-none dark:prose-invert"
          v-html="selectedAnnouncement ? renderMarkdown(selectedAnnouncement.content) : ''"
        ></div>

        <template #footer>
          <span class="mr-auto text-xs surface-muted">
            {{ selectedAnnouncement?.read_at ? t('announcements.readStatus') : t('announcements.markReadHint') }}
          </span>
          <button @click="closeDetail" class="btn btn-secondary">
            {{ t('common.close') }}
          </button>
          <button
            v-if="selectedAnnouncement && !selectedAnnouncement.read_at"
            @click="markAsReadAndClose(selectedAnnouncement.id)"
            class="btn btn-primary"
          >
            <span class="flex items-center gap-2">
              <Icon name="check" size="sm" />
              {{ t('announcements.markRead') }}
            </span>
          </button>
        </template>
      </BaseDialog>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { supportReadonly as vSupportReadonly } from '@/directives/supportReadonly'
import { ref, computed, nextTick, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAppStore } from '@/stores/app'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeTime, formatRelativeWithDateTime } from '@/utils/format'
import type { UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import '@/styles/announcement-markdown.css'

const { t } = useI18n()
const appStore = useAppStore()
const announcementStore = useAnnouncementStore()

// Configure marked
marked.setOptions({
  breaks: true,
  gfm: true,
})

// Use store state (storeToRefs for reactivity)
const { announcements, loading } = storeToRefs(announcementStore)
const unreadCount = computed(() => announcementStore.unreadCount)

// Local panel state
const isOpen = ref(false)
const buttonRef = ref<HTMLButtonElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
const panelStyle = ref<Record<string, string>>({})
const detailModalOpen = ref(false)
const selectedAnnouncement = ref<UserAnnouncement | null>(null)

// Methods
function renderMarkdown(content: string): string {
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
}

const PANEL_GAP = 8
const PANEL_EDGE = 12
const PANEL_MAX_WIDTH = 380

// The list panel is teleported (the header's backdrop filter would otherwise
// become the containing block), so anchor it to the bell by hand: right edges
// aligned like the user menu, clamped inside the viewport on narrow screens.
function positionPanel() {
  const rect = buttonRef.value?.getBoundingClientRect()
  if (!rect) return
  const viewportWidth = window.innerWidth || document.documentElement.clientWidth
  const width = Math.min(PANEL_MAX_WIDTH, viewportWidth - PANEL_EDGE * 2)
  const left = Math.min(Math.max(rect.right - width, PANEL_EDGE), viewportWidth - width - PANEL_EDGE)
  panelStyle.value = {
    position: 'fixed',
    top: `${Math.round(rect.bottom + PANEL_GAP)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
    maxHeight: `calc(100dvh - ${Math.round(rect.bottom + PANEL_GAP + PANEL_EDGE)}px)`
  }
}

function handleOutsideClick(event: MouseEvent) {
  const target = event.target as Node | null
  if (!target) return
  if (buttonRef.value?.contains(target) || panelRef.value?.contains(target)) return
  closePanel()
}

function openPanel() {
  positionPanel()
  isOpen.value = true
  window.addEventListener('resize', positionPanel)
  // Capture phase: registered while the opening click is still bubbling, so
  // that click cannot close the panel it just opened.
  document.addEventListener('click', handleOutsideClick, true)
  void nextTick(() => panelRef.value?.focus({ preventScroll: true }))
}

function closePanel() {
  if (!isOpen.value) return
  isOpen.value = false
  window.removeEventListener('resize', positionPanel)
  document.removeEventListener('click', handleOutsideClick, true)
}

function closePanelAndFocus() {
  closePanel()
  buttonRef.value?.focus()
}

function togglePanel() {
  if (isOpen.value) closePanel()
  else openPanel()
}

onBeforeUnmount(closePanel)

function openDetail(announcement: UserAnnouncement) {
  closePanel()
  selectedAnnouncement.value = announcement
  detailModalOpen.value = true
  if (!announcement.read_at) {
    markAsRead(announcement.id)
  }
}

function closeDetail() {
  detailModalOpen.value = false
  selectedAnnouncement.value = null
}

async function markAsRead(id: number) {
  try {
    const marked = await announcementStore.markAsRead(id)
    if (!marked) appStore.showError(t('common.unknownError'))
    return marked
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
    return false
  }
}

async function markAsReadAndClose(id: number) {
  if (!await markAsRead(id)) return
  appStore.showSuccess(t('announcements.markedAsRead'))
  closeDetail()
}

async function markAllAsRead() {
  try {
    await announcementStore.markAllAsRead()
    appStore.showSuccess(t('announcements.allMarkedAsRead'))
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
  }
}
</script>
