<template>
  <div>
    <!-- 铃铛按钮 -->
    <button
      @click="openModal"
      class="relative flex h-9 w-9 items-center justify-center rounded-lg transition-colors hover:surface-hover surface-control surface-muted"
      :class="{ 'text-primary-600 dark:text-primary-400': unreadCount > 0 }"
      :aria-label="t('announcements.title')"
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
      <BaseDialog
        :show="isModalOpen"
        :title="t('announcements.title')"
        width="normal"
        :z-index="100"
        @close="closeModal"
      >
        <template #header-actions>
          <button
            v-if="unreadCount > 0"
            v-support-readonly
            @click="markAllAsRead"
            :disabled="loading"
            class="btn btn-secondary text-xs"
          >
            {{ t('announcements.markAllRead') }}
          </button>
        </template>

        <p v-if="unreadCount > 0" class="mb-4 text-sm surface-muted">
          <span class="font-medium">{{ unreadCount }}</span>
          {{ t('announcements.unread') }}
        </p>

        <!-- Loading -->
        <div v-if="loading" class="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>

        <!-- Announcements List -->
        <div v-else-if="announcements.length > 0" class="-mx-6 -my-4">
          <button
            v-for="item in announcements"
            :key="item.id"
            type="button"
            class="group relative flex w-full items-center gap-4 border-b border-gray-100 px-6 py-4 text-left transition-colors last:border-b-0 hover:surface-hover dark:border-dark-700"
            :class="{ 'surface-soft': !item.read_at }"
            style="min-height: 72px"
            data-testid="announcement-row"
            @click="openDetail(item)"
          >
            <!-- Status Indicator -->
            <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-[var(--surface-radius-item)] surface-inset surface-muted">
              <Icon v-if="!item.read_at" name="infoCircle" size="sm" />
              <Icon v-else name="check" size="sm" />
            </div>

            <!-- Content -->
            <div class="flex min-w-0 flex-1 items-center justify-between gap-4">
              <div class="min-w-0 flex-1">
                <h3 class="truncate text-sm font-medium text-gray-900 dark:text-white">
                  {{ item.title }}
                </h3>
                <div class="mt-1 flex items-center gap-2">
                  <time class="text-xs surface-muted">
                    {{ formatRelativeTime(item.created_at) }}
                  </time>
                  <span
                    v-if="!item.read_at"
                    class="inline-flex items-center gap-1 rounded-md surface-inset px-1.5 py-0.5 text-xs font-medium surface-muted"
                  >
                    <span class="console-data-mark h-1.5 w-1.5 rounded-full bg-rose-500"></span>
                    {{ t('announcements.unread') }}
                  </span>
                </div>
              </div>

              <!-- Arrow -->
              <Icon name="chevronRight" size="sm" class="flex-shrink-0 surface-muted" />
            </div>
          </button>
        </div>

        <!-- Empty State -->
        <div v-else class="flex flex-col items-center justify-center py-16">
          <div class="mb-4 flex h-16 w-16 items-center justify-center rounded-[var(--surface-radius-panel)] surface-inset surface-muted">
            <Icon name="inbox" size="lg" />
          </div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('announcements.empty') }}</p>
          <p class="mt-1 text-xs surface-muted">{{ t('announcements.emptyDescription') }}</p>
        </div>
      </BaseDialog>
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
import { ref, computed } from 'vue'
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

// Local modal state
const isModalOpen = ref(false)
const detailModalOpen = ref(false)
const selectedAnnouncement = ref<UserAnnouncement | null>(null)

// Methods
function renderMarkdown(content: string): string {
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
}

function openModal() {
  isModalOpen.value = true
}

function closeModal() {
  isModalOpen.value = false
}

function openDetail(announcement: UserAnnouncement) {
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
