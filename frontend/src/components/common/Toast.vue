<template>
  <Teleport to="body">
    <div
      class="toast-stack pointer-events-none fixed z-[9999]"
      aria-live="polite"
      aria-atomic="true"
    >
      <TransitionGroup
        enter-active-class="transition ease-out duration-300"
        enter-from-class="opacity-0 translate-x-full"
        enter-to-class="opacity-100 translate-x-0"
        leave-active-class="transition ease-in duration-200"
        leave-from-class="opacity-100 translate-x-0"
        leave-to-class="opacity-0 translate-x-full"
      >
        <div
          v-for="toast in toasts"
          :key="toast.id"
          class="toast-card pointer-events-auto"
          :class="{ 'toast-card-timed': toast.duration }"
          :data-toast-id="toast.id"
          :data-toast-type="toast.type"
        >
          <div class="toast-body">
            <!-- Icon -->
            <div class="toast-icon" aria-hidden="true">
              <Icon
                :name="getToastIconName(toast.type)"
                size="md"
                aria-hidden="true"
              />
            </div>

            <!-- Content -->
            <div class="toast-copy">
              <p v-if="toast.title" class="toast-title">
                {{ toast.title }}
              </p>
              <p class="toast-message">
                {{ toast.message }}
              </p>
            </div>

            <!-- Close button -->
            <button
              type="button"
              @click="removeToast(toast.id)"
              class="toast-close"
              :aria-label="t('common.close')"
            >
              <Icon name="x" size="sm" aria-hidden="true" />
            </button>
          </div>

          <!-- Progress bar -->
          <div v-if="toast.duration" class="toast-progress-track" aria-hidden="true">
            <div
              class="toast-progress"
              :style="{ animationDuration: `${toast.duration}ms` }"
            ></div>
          </div>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()
const { t } = useI18n()

const toasts = computed(() => appStore.toasts)

const getToastIconName = (type: string): 'checkCircle' | 'xCircle' | 'exclamationTriangle' | 'infoCircle' => {
  switch (type) {
    case 'success':
      return 'checkCircle'
    case 'error':
      return 'xCircle'
    case 'warning':
      return 'exclamationTriangle'
    case 'info':
    default:
      return 'infoCircle'
  }
}

const removeToast = (id: string) => {
  appStore.hideToast(id)
}
</script>

<style scoped>
.toast-stack {
  top: var(--shell-inset-top, var(--shell-gutter, 12px));
  right: max(var(--shell-gutter, 12px), env(safe-area-inset-right));
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: min(376px, calc(100vw - 24px));
  max-width: calc(100vw - max(var(--shell-gutter, 12px), env(safe-area-inset-left)) - max(var(--shell-gutter, 12px), env(safe-area-inset-right)));
  max-height: calc(100dvh - var(--shell-inset-top, 12px) - var(--shell-inset-bottom, 12px));
  overflow-x: hidden;
  overflow-y: auto;
}

.toast-card {
  --toast-accent: 59 130 246;
  position: relative;
  flex-shrink: 0;
  width: 100%;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--surface-edge);
  border-radius: 20px;
  background-color: var(--surface-panel);
  background-image: linear-gradient(135deg, var(--surface-glint), transparent 65%);
  color: var(--surface-text);
  box-shadow: var(--surface-shadow);
}
.toast-card[data-toast-type='success'] { --toast-accent: 34 197 94; }
.toast-card[data-toast-type='error'] { --toast-accent: 239 68 68; }
.toast-card[data-toast-type='warning'] { --toast-accent: 245 158 11; }

.toast-body {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
}
.toast-card-timed .toast-body { padding-bottom: 28px; }
.toast-icon {
  display: grid;
  flex: 0 0 36px;
  width: 36px;
  height: 36px;
  place-items: center;
  border-radius: 12px;
  background-color: rgb(var(--toast-accent) / 14%);
  color: rgb(var(--toast-accent));
}
.toast-copy { min-width: 0; flex: 1; }
.toast-title, .toast-message {
  margin: 0;
  color: inherit;
  font-size: 14px;
  line-height: 1.5;
  overflow-wrap: anywhere;
  white-space: pre-line;
}
.toast-title { font-weight: 600; }
.toast-title + .toast-message { margin-top: 4px; }
.toast-close {
  display: grid;
  flex: 0 0 28px;
  width: 28px;
  height: 28px;
  margin: -4px -4px 0 0;
  place-items: center;
  border: 0;
  border-radius: 10px;
  background-color: transparent;
  color: var(--surface-muted);
  transition: background-color 150ms ease, color 150ms ease;
}
.toast-close:hover { background-color: var(--surface-hover); color: var(--surface-text); }
.toast-close:focus-visible { outline: 2px solid rgb(var(--console-accent-500, 20 184 166) / 65%); outline-offset: 2px; }

.toast-progress-track {
  position: absolute;
  right: 16px;
  bottom: 12px;
  left: 16px;
  height: 3px;
  overflow: hidden;
  border-radius: 9999px;
  background-color: rgb(var(--toast-accent) / 14%);
}
.toast-progress {
  height: 100%;
  width: 100%;
  border-radius: inherit;
  background-color: rgb(var(--toast-accent));
  animation-name: toast-progress-shrink;
  animation-timing-function: linear;
  animation-fill-mode: forwards;
}

@media (max-width: 639px) {
  .toast-progress-track { bottom: 9px; }
}

@media (prefers-reduced-motion: reduce) {
  .toast-card, .toast-close { transition: none !important; }
  .toast-progress { animation: none; }
}

@keyframes toast-progress-shrink {
  from {
    width: 100%;
  }
  to {
    width: 0%;
  }
}
</style>
