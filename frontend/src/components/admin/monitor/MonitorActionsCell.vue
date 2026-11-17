<template>
  <div class="table-actions">
    <span :title="runTitle" :class="running || probesEnabled === false ? 'cursor-not-allowed' : ''">
      <button
        data-testid="monitor-run"
        :title="runTitle"
        @click="$emit('run', row)"
        :disabled="running || probesEnabled === false"
        class="table-action disabled:pointer-events-none"
      >
        <Icon name="refresh" size="sm" :class="running ? 'animate-spin' : ''" />
        <span class="text-xs">{{ t(running ? 'admin.channelMonitor.running' : 'admin.channelMonitor.runNow') }}</span>
      </button>
    </span>
    <button
      @click="$emit('edit', row)"
      class="table-action"
    >
      <Icon name="edit" size="sm" />
      <span class="text-xs">{{ t('common.edit') }}</span>
    </button>
    <ActionMenu :items="menuItems" @select="handleSelect" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import ActionMenu, { type ActionMenuItem } from '@/components/common/ActionMenu.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  row: ChannelMonitor
  running: boolean
  probesEnabled?: boolean
  duplicating: boolean
}>()

const emit = defineEmits<{
  (e: 'run', row: ChannelMonitor): void
  (e: 'duplicate', row: ChannelMonitor): void
  (e: 'edit', row: ChannelMonitor): void
  (e: 'delete', row: ChannelMonitor): void
}>()

const { t } = useI18n()
const runTitle = computed(() => {
  if (props.running) return t('admin.channelMonitor.running')
  if (props.probesEnabled === false) return t('admin.channelMonitor.probesDisabledHint')
  return t('admin.channelMonitor.runNow')
})

const duplicateTitle = computed(() => {
  if (props.row.api_key_decrypt_failed) return t('admin.channelMonitor.duplicateKeyUnavailable')
  if (props.duplicating) return t('admin.channelMonitor.duplicating')
  return t('admin.channelMonitor.duplicate')
})

// Run now and Edit stay visible; the rest live in one "more" menu.
const menuItems = computed<ActionMenuItem[]>(() => [
  {
    key: 'duplicate',
    label: props.duplicating ? t('admin.channelMonitor.duplicating') : t('admin.channelMonitor.duplicate'),
    icon: 'copy',
    disabled: props.duplicating || Boolean(props.row.api_key_decrypt_failed),
    title: duplicateTitle.value,
  },
  { key: 'delete', label: t('common.delete'), icon: 'trash', tone: 'danger' },
])

function handleSelect(action: string) {
  if (action === 'duplicate') emit('duplicate', props.row)
  else if (action === 'delete') emit('delete', props.row)
}
</script>
