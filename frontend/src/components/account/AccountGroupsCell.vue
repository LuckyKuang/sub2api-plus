<template>
  <div v-if="groups && groups.length > 0" class="relative w-56">
    <div data-test="group-summary" class="flex max-h-14 flex-wrap gap-1 overflow-hidden">
      <GroupBadge
        v-for="group in displayGroups"
        :key="group.id"
        :name="group.name"
        :platform="group.platform"
        :subscription-type="group.subscription_type"
        :rate-multiplier="group.rate_multiplier"
        :show-rate="false"
        :title="group.name"
        class="max-w-24"
      />
      <button
        v-if="hiddenCount > 0"
        ref="moreButtonRef"
        type="button"
        class="inline-flex cursor-pointer items-center whitespace-nowrap rounded-md bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500"
        :aria-expanded="showPopover"
        :aria-label="t('admin.accounts.groupCountTotal', { count: groups.length })"
        @click.stop="showPopover = !showPopover"
      >
        +{{ hiddenCount }}
      </button>
    </div>

    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-150 ease-out"
        enter-from-class="scale-95 opacity-0"
        enter-to-class="scale-100 opacity-100"
        leave-active-class="transition duration-100 ease-in"
        leave-from-class="scale-100 opacity-100"
        leave-to-class="scale-95 opacity-0"
      >
        <div
          v-if="showPopover"
          ref="popoverRef"
          class="fixed z-50 min-w-48 max-w-96 rounded-lg border border-gray-200 bg-white p-3 shadow-lg dark:border-dark-600 dark:bg-dark-800"
          :style="popoverStyle"
          role="dialog"
        >
          <div class="mb-2 flex items-center justify-between gap-3">
            <span class="text-xs font-medium text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.groupCountTotal', { count: groups.length }) }}
            </span>
            <button
              type="button"
              class="rounded p-0.5 text-gray-400 hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-300"
              :aria-label="t('common.close')"
              @click="showPopover = false"
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
          <div class="flex max-h-64 flex-wrap gap-1.5 overflow-y-auto">
            <GroupBadge
              v-for="group in groups"
              :key="group.id"
              :name="group.name"
              :platform="group.platform"
              :subscription-type="group.subscription_type"
              :rate-multiplier="group.rate_multiplier"
              :show-rate="false"
              :title="group.name"
              class="max-w-full"
            />
          </div>
        </div>
      </Transition>
    </Teleport>

    <div v-if="showPopover" class="fixed inset-0 z-40" @click="showPopover = false" />
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Group } from '@/types'

interface Props {
  groups: Group[] | null | undefined
  maxDisplay?: number
}

const props = withDefaults(defineProps<Props>(), {
  maxDisplay: 4
})

const { t } = useI18n()
const moreButtonRef = ref<HTMLElement | null>(null)
const popoverRef = ref<HTMLElement | null>(null)
const showPopover = ref(false)

const displayGroups = computed(() => {
  if (!props.groups) return []
  if (props.groups.length <= props.maxDisplay) return props.groups
  return props.groups.slice(0, props.maxDisplay - 1)
})

const hiddenCount = computed(() => {
  if (!props.groups || props.groups.length <= props.maxDisplay) return 0
  return props.groups.length - (props.maxDisplay - 1)
})

const popoverStyle = computed(() => {
  if (!moreButtonRef.value) return {}
  const rect = moreButtonRef.value.getBoundingClientRect()
  const viewportHeight = window.innerHeight
  const viewportWidth = window.innerWidth
  let top = rect.bottom + 8
  let left = rect.left

  if (top + 280 > viewportHeight) top = Math.max(8, rect.top - 280)
  if (left + 384 > viewportWidth) left = Math.max(8, viewportWidth - 392)

  return { top: `${top}px`, left: `${left}px` }
})

const handleKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape') showPopover.value = false
}

onMounted(() => window.addEventListener('keydown', handleKeydown))
onUnmounted(() => window.removeEventListener('keydown', handleKeydown))
</script>
