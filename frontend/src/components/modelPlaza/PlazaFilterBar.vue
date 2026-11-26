<template>
  <div class="card p-3 sm:p-4">
    <div class="filter-bar">
      <div class="filter-field filter-field--search relative">
        <Icon
          name="search"
          size="sm"
          aria-hidden="true"
          class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 surface-muted"
        />
        <input
          ref="searchInput"
          :value="search"
          type="text"
          role="searchbox"
          enterkeyhint="search"
          :aria-label="t('modelPlaza.filters.searchPlaceholder')"
          :placeholder="t('modelPlaza.filters.searchPlaceholder')"
          class="input pl-9"
          :class="{ 'pr-8': search }"
          @input="$emit('update:search', ($event.target as HTMLInputElement).value)"
        />
        <button
          v-if="search"
          type="button"
          class="absolute inset-y-0 right-0 flex items-center justify-center px-2.5 surface-muted"
          :aria-label="t('common.clear')"
          @click="clearSearch"
        >
          <Icon name="x" size="xs" aria-hidden="true" />
        </button>
      </div>

      <div class="filter-pair lg:flex-1">
        <div class="filter-field">
          <Select
            :model-value="groupId"
            :options="groupOptions"
            :aria-label="t('modelPlaza.filters.groupLabel')"
            :clearable="groupId !== 'all'"
            @update:model-value="$emit('update:groupId', typeof $event === 'number' ? $event : 'all')"
          />
        </div>

        <div class="filter-actions">
          <button
            type="button"
            class="btn btn-secondary"
            :aria-expanded="expanded"
            :aria-controls="advancedFiltersId"
            @click="expanded = !expanded"
          >
            <Icon name="filter" size="sm" aria-hidden="true" />
            {{ t('common.filter') }}
          </button>
          <div
            role="group"
            class="segmented-control h-[var(--control-height)]"
            :aria-label="t('modelPlaza.filters.view')"
          >
            <button
              v-for="mode in modes"
              :key="mode.value"
              type="button"
              class="segmented-option"
              :aria-pressed="view === mode.value"
              :aria-label="t(mode.label)"
              :title="t(mode.label)"
              @click="$emit('update:view', mode.value)"
            >
              <Icon :name="mode.icon" size="sm" aria-hidden="true" />
              <span class="hidden sm:inline">{{ t(mode.label) }}</span>
            </button>
          </div>
        </div>
      </div>
    </div>

    <div
      v-show="expanded"
      :id="advancedFiltersId"
      class="filter-bar mt-3 border-t border-[var(--surface-edge)] pt-3"
    >
      <div class="filter-field">
        <label :for="platformId" class="mb-1 block text-xs font-medium surface-muted">
          {{ t('modelPlaza.filters.platformLabel') }}
        </label>
        <Select
          :id="platformId"
          :model-value="platform"
          :options="platformOptions"
          :aria-label="t('modelPlaza.filters.platformLabel')"
          @update:model-value="$emit('update:platform', typeof $event === 'string' ? $event : 'all')"
        />
      </div>
      <div class="filter-field">
        <label :for="rateId" class="mb-1 block text-xs font-medium surface-muted">
          {{ t('modelPlaza.filters.rateLabel') }}
        </label>
        <Select
          :id="rateId"
          :model-value="rate"
          :options="rateOptions"
          :aria-label="t('modelPlaza.filters.rateLabel')"
          @update:model-value="$emit('update:rate', typeof $event === 'number' ? $event : 'all')"
        />
      </div>
    </div>

    <div v-if="platform !== 'all' || rate !== 'all'" class="mt-3 flex flex-wrap gap-2">
      <button
        v-if="platform !== 'all'"
        type="button"
        class="btn btn-secondary btn-xs"
        :aria-label="t('modelPlaza.filters.remove', { filter: platformLabel(platform) })"
        @click="$emit('update:platform', 'all')"
      >
        {{ platformLabel(platform) }}
        <Icon name="x" size="xs" aria-hidden="true" />
      </button>
      <button
        v-if="rate !== 'all'"
        type="button"
        class="btn btn-secondary btn-xs"
        :aria-label="t('modelPlaza.filters.remove', { filter: rate + '×' })"
        @click="$emit('update:rate', 'all')"
      >
        {{ rate }}×
        <Icon name="x" size="xs" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { platformLabel } from '@/utils/platformColors'

const props = defineProps<{
  platforms: string[]
  groups: Array<{ id: number; name: string; platform: string; rate: number }>
  rates: number[]
  platform: string
  groupId: number | 'all'
  rate: number | 'all'
  search: string
  view: 'cards' | 'table'
}>()

const emit = defineEmits<{
  'update:platform': [value: string]
  'update:groupId': [value: number | 'all']
  'update:rate': [value: number | 'all']
  'update:search': [value: string]
  'update:view': [value: 'cards' | 'table']
}>()

const { t } = useI18n()
const searchInput = ref<HTMLInputElement | null>(null)
const expanded = ref(false)
const advancedFiltersId = useId()
const platformId = useId()
const rateId = useId()

const modes = [
  { value: 'cards', label: 'modelPlaza.filters.cards', icon: 'grid' },
  { value: 'table', label: 'modelPlaza.filters.table', icon: 'menu' }
] as const

const groupOptions = computed(() => [
  { value: 'all', label: t('modelPlaza.filters.allGroups') },
  ...props.groups.map(g => ({
    value: g.id,
    label: g.name,
    disabled: (props.platform !== 'all' && g.platform !== props.platform)
      || (props.rate !== 'all' && g.rate !== props.rate)
  }))
])

const platformOptions = computed(() => [
  { value: 'all', label: t('modelPlaza.filters.all') },
  ...props.platforms.map(p => ({
    value: p,
    label: platformLabel(p),
    disabled: !props.groups.some(g => g.platform === p
      && (props.groupId === 'all' || g.id === props.groupId)
      && (props.rate === 'all' || g.rate === props.rate))
  }))
])

const rateOptions = computed(() => [
  { value: 'all', label: t('modelPlaza.filters.all') },
  ...props.rates.map(r => ({
    value: r,
    label: r + '×',
    disabled: !props.groups.some(g => g.rate === r
      && (props.groupId === 'all' || g.id === props.groupId)
      && (props.platform === 'all' || g.platform === props.platform))
  }))
])

function clearSearch() {
  emit('update:search', '')
  searchInput.value?.focus()
}
</script>
