<template>
  <div class="relative" ref="dropdownRef">
    <button
      @click="showDropdown = !showDropdown"
      class="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-2.5 py-1.5 text-xs font-medium shadow-sm transition-colors hover:surface-hover dark:border-dark-600 surface-control surface-muted"
      :title="t('common.autoRefresh.title')"
    >
      <svg
        class="h-3.5 w-3.5"
        :class="fetching ? 'animate-spin' : ''"
        xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
      >
        <path fill-rule="evenodd" d="M15.312 11.424a5.5 5.5 0 01-9.201 2.466l-.312-.311h2.433a.75.75 0 000-1.5H4.598a.75.75 0 00-.75.75v3.634a.75.75 0 001.5 0v-2.033l.312.312a7 7 0 0011.712-3.138.75.75 0 00-1.449-.39zm-10.624-2.848a5.5 5.5 0 019.201-2.466l.312.311H11.768a.75.75 0 000 1.5h3.634a.75.75 0 00.75-.75V3.537a.75.75 0 00-1.5 0v2.034l-.312-.312A7 7 0 002.628 8.397a.75.75 0 001.449.39z" clip-rule="evenodd" />
      </svg>
      <span>
        {{ fetching ? t('inspection.updating') : paused && enabled ? t('inspection.paused') : enabled
          ? t('common.autoRefresh.countdown', { seconds: countdown })
          : intervalOnly ? t('common.autoRefresh.seconds', { n: intervalSeconds }) : t('common.autoRefresh.title')
        }}
      </span>
    </button>

    <div
      v-if="showDropdown"
      class="absolute right-0 z-20 mt-1 w-44 rounded-lg border border-gray-200 shadow-lg dark:border-dark-600 surface-panel"
    >
      <div class="p-1.5">
        <button
          v-if="!intervalOnly"
          @click="$emit('update:enabled', !enabled)"
          class="flex w-full items-center justify-between rounded-md px-3 py-1.5 text-sm hover:surface-hover surface-control surface-muted"
        >
          <span>{{ t('common.autoRefresh.enable') }}</span>
          <svg v-if="enabled" class="h-4 w-4 text-primary-500" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z" clip-rule="evenodd" />
          </svg>
        </button>
        <div v-if="!intervalOnly" class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
        <button
          v-for="sec in intervals"
          :key="sec"
          @click="$emit('update:interval', sec)"
          class="flex w-full items-center justify-between rounded-md px-3 py-1.5 text-sm hover:surface-hover surface-control surface-muted"
        >
          <span>{{ t('common.autoRefresh.seconds', { n: sec }) }}</span>
          <svg v-if="intervalSeconds === sec" class="h-4 w-4 text-primary-500" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z" clip-rule="evenodd" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{
  enabled: boolean
  fetching?: boolean
  paused?: boolean
  intervalSeconds: number
  countdown: number
  intervals: readonly number[]
  intervalOnly?: boolean
}>()

defineEmits<{
  (e: 'update:enabled', value: boolean): void
  (e: 'update:interval', value: number): void
}>()

const { t } = useI18n()
const showDropdown = ref(false)
const dropdownRef = ref<HTMLElement | null>(null)

function handleClickOutside(event: MouseEvent) {
  if (dropdownRef.value && !dropdownRef.value.contains(event.target as Node)) {
    showDropdown.value = false
  }
}

onMounted(() => document.addEventListener('click', handleClickOutside))
onBeforeUnmount(() => document.removeEventListener('click', handleClickOutside))
</script>
