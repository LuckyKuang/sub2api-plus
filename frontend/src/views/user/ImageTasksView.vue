<template>
  <AppLayout appearance="user" workspace>
    <div class="image-tasks-shell">
      <div v-if="showSwitcher" class="mode-switcher" data-testid="image-task-mode-switcher">
        <div class="mode-switcher-track">
          <button
            v-for="card in modeCards"
            :key="card.key"
            type="button"
            class="mode-card surface-control"
            :class="{ 'mode-card-active': mode === card.key }"
            :aria-pressed="mode === card.key"
            :data-testid="'image-task-mode-' + card.key"
            @click="selectMode(card.key)"
          >
            <span class="mode-card-icon"><Icon :name="card.icon" size="md" /></span>
            <span class="mode-card-body">
              <span class="mode-card-title">{{ card.title }}</span>
              <span class="mode-card-hint">{{ card.hint }}</span>
            </span>
          </button>
        </div>
      </div>

      <div v-if="!loading && !anyAvailable" class="card workspace-shell p-8" data-testid="image-tasks-unavailable">
        <div class="mx-auto max-w-lg text-center">
          <Icon name="sparkles" size="xl" class="mx-auto mb-4 h-12 w-12 surface-muted" />
          <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('imageTasks.unavailable.title') }}</p>
          <p class="mt-2 text-sm surface-muted">{{ t('imageTasks.unavailable.hint') }}</p>
          <ul class="mt-4 space-y-1 text-sm surface-muted">
            <li>{{ t('imageTasks.unavailable.ordinary', { reason: reasonLabel(modes?.ordinary?.reason) }) }}</li>
            <li>{{ t('imageTasks.unavailable.batch', { reason: reasonLabel(modes?.batch?.reason) }) }}</li>
          </ul>
        </div>
      </div>

      <KeepAlive v-else>
        <component :is="activePanel" :key="mode" embedded />
      </KeepAlive>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import AsyncImageView from '@/views/user/AsyncImageView.vue'
import BatchImageGuideView from '@/views/user/BatchImageGuideView.vue'
import { useImageTaskModes } from '@/composables/useImageTaskModes'

type Mode = 'ordinary' | 'batch'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { modes, loading, ordinaryAvailable, batchAvailable, anyAvailable, refreshImageTaskModes } = useImageTaskModes()

const mode = computed<Mode>(() => {
  const requested = route.query.mode
  if (requested === 'batch' && batchAvailable.value) return 'batch'
  if (requested === 'ordinary' && ordinaryAvailable.value) return 'ordinary'
  if (ordinaryAvailable.value) return 'ordinary'
  return 'batch'
})

const showSwitcher = computed(() => ordinaryAvailable.value && batchAvailable.value)
const activePanel = computed(() => mode.value === 'ordinary' ? AsyncImageView : BatchImageGuideView)

const modeCards = computed(() => [
  {
    key: 'ordinary' as Mode,
    icon: 'sparkles' as const,
    title: t('imageTasks.modes.ordinary.title'),
    hint: t('imageTasks.modes.ordinary.hint'),
  },
  {
    key: 'batch' as Mode,
    icon: 'grid' as const,
    title: t('imageTasks.modes.batch.title'),
    hint: t('imageTasks.modes.batch.hint'),
  },
])

function reasonLabel(reason?: string) {
  if (!reason) return t('imageTasks.reasons.ready')
  const key = 'imageTasks.reasons.' + reason
  const translated = t(key)
  return translated === key ? reason : translated
}

function selectMode(next: Mode) {
  if (next === mode.value) return
  void router.replace({ path: '/image-tasks', query: { mode: next } })
}

onMounted(async () => {
  await refreshImageTaskModes(true)
})

// A direct link may request a mode that is currently switched off; normalize the
// query so the visible page and the URL agree.
watch([ordinaryAvailable, batchAvailable], () => {
  const requested = route.query.mode
  if (requested && requested !== mode.value) {
    void router.replace({ path: '/image-tasks', query: { mode: mode.value } })
  }
})
</script>

<style scoped>
.image-tasks-shell {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: var(--shell-zone-gap, 16px);
  min-height: 0;
  min-width: 0;
}

.mode-switcher {
  display: flex;
  justify-content: center;
  flex-shrink: 0;
}

.mode-switcher-track {
  display: inline-flex;
  gap: 8px;
  padding: 6px;
  border: 1px solid var(--surface-border, rgba(148, 163, 184, 0.25));
  border-radius: var(--surface-radius-panel, 12px);
  background: var(--surface-field, rgba(148, 163, 184, 0.06));
}

.mode-card {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 200px;
  padding: 8px 16px;
  border: 1px solid transparent;
  border-radius: var(--surface-radius-item, 8px);
  text-align: left;
  transition: background-color 0.15s ease, border-color 0.15s ease;
}

.mode-card:hover {
  background: var(--surface-hover, rgba(148, 163, 184, 0.12));
}

.mode-card-active {
  background: var(--surface-card, #fff);
  border-color: var(--surface-border, rgba(148, 163, 184, 0.35));
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
}

.mode-card-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: rgba(99, 102, 241, 0.12);
  color: rgb(99, 102, 241);
}

.mode-card-body {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.mode-card-title {
  font-size: 0.875rem;
  font-weight: 600;
}

.mode-card-hint {
  font-size: 0.75rem;
  color: var(--surface-muted);
}
</style>
