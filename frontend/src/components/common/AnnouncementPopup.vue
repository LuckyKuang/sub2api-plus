<template>
  <Teleport to="body">
    <BaseDialog
      :show="!!displayedAnnouncement"
      :title="displayedAnnouncement?.title || ''"
      width="wide"
      :z-index="120"
      :show-close-button="true"
      @close="handleDismiss"
    >
      <p class="mb-4 flex items-center gap-1.5 text-sm surface-muted">
        <Icon name="clock" size="sm" />
        <time>{{ displayedAnnouncement ? formatRelativeWithDateTime(displayedAnnouncement.created_at) : '' }}</time>
      </p>
      <div class="markdown-body prose prose-sm max-w-none dark:prose-invert" v-html="renderedContent"></div>

      <template #footer>
        <button
          @click="handleDismiss"
          data-testid="announcement-popup-dismiss"
          class="btn btn-primary"
        >
          <span class="flex items-center gap-2">
            <Icon v-if="preview" name="x" size="sm" />
            <Icon v-else name="check" size="sm" />
            {{ preview ? t('common.close') : t('announcements.markRead') }}
          </span>
        </button>
      </template>
    </BaseDialog>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { Announcement, UserAnnouncement } from '@/types'
import '@/styles/announcement-markdown.css'

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>

const props = withDefaults(defineProps<{
  announcement?: PreviewAnnouncement | null
  preview?: boolean
}>(), {
  announcement: null,
  preview: false,
})

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const announcementStore = useAnnouncementStore()
const displayedAnnouncement = computed(() => (
  props.preview ? props.announcement : announcementStore.currentPopup
))

marked.setOptions({
  breaks: true,
  gfm: true,
})

const renderedContent = computed(() => {
  const content = displayedAnnouncement.value?.content
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
})

function handleDismiss() {
  if (props.preview) {
    emit('close')
    return
  }
  announcementStore.dismissPopup()
}
</script>
