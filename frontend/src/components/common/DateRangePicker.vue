<template>
  <div class="relative" ref="containerRef">
    <button
      type="button"
      @click="toggle"
      :aria-expanded="isOpen"
      :aria-controls="isOpen ? panelId : undefined"
      aria-haspopup="dialog"
      :class="['date-picker-trigger surface-control', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Teleport to="body">
      <Transition name="date-picker-dropdown">
        <div v-if="isOpen" :id="panelId" ref="dropdownRef" class="date-picker-dropdown surface-panel" :style="dropdownStyle" role="dialog" :aria-label="t('dates.selectDateRange')" tabindex="-1" @click.stop @mousedown.stop>
          <!-- Quick presets -->
          <div class="date-picker-presets">
            <button
              v-for="preset in presets"
              :key="preset.value"
              type="button"
              @click="selectPreset(preset)"
              :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
            >
              {{ t(preset.labelKey) }}
            </button>
          </div>

          <div class="date-picker-divider"></div>

          <!-- Custom date range inputs -->
          <div class="date-picker-custom">
            <div class="date-picker-field">
              <label class="date-picker-label">{{ t('dates.startDate') }}</label>
              <DateTimePicker :aria-label="t('dates.startDate')"
                type="date"
                v-model="localStartDate"
                :max="localEndDate || tomorrow"
                class="date-picker-input"
                @change="onDateChange"
              />
            </div>
            <div class="date-picker-separator">
              <Icon name="arrowRight" size="sm" class="text-gray-400" />
            </div>
            <div class="date-picker-field">
              <label class="date-picker-label">{{ t('dates.endDate') }}</label>
              <DateTimePicker :aria-label="t('dates.endDate')"
                type="date"
                v-model="localEndDate"
                :min="localStartDate"
                :max="tomorrow"
                class="date-picker-input"
                @change="onDateChange"
              />
            </div>
          </div>

          <!-- Apply button -->
          <div class="date-picker-actions">
            <button type="button" @click="apply" class="date-picker-apply surface-control">
              {{ t('dates.apply') }}
            </button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import DateTimePicker from './DateTimePicker.vue'
import { ref, computed, watch, nextTick, onMounted, onUnmounted, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useDialogFocus } from '@/composables/useDialogFocus'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => { start: string; end: string }
}

interface Props {
  startDate: string
  endDate: string
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (e: 'change', range: { startDate: string; endDate: string; preset: string | null }): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t, locale } = useI18n()

const panelId = `date-range-${useId()}`
const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownStyle = ref<Record<string, string>>({})
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
const activePreset = ref<string | null>('last24Hours')
const restoreFocus = ref(true)

function closeDropdown() {
  restoreFocus.value = true
  isOpen.value = false
}

useDialogFocus(() => isOpen.value, dropdownRef, closeDropdown, () => true, {
  lockScroll: false,
  restoreFocus: () => restoreFocus.value,
})

const today = computed(() => {
  // Use local timezone to avoid UTC timezone issues
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
})

// Tomorrow's date - used for max date to handle timezone differences
// When user is in a timezone behind the server, "today" on server might be "tomorrow" locally
const tomorrow = computed(() => {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return formatDateToString(d)
})

// Helper function to format date to YYYY-MM-DD using local timezone
const formatDateToString = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const presets: DatePreset[] = [
  {
    labelKey: 'dates.today',
    value: 'today',
    getRange: () => {
      const t = today.value
      return { start: t, end: t }
    }
  },
  {
    labelKey: 'dates.yesterday',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const yesterday = formatDateToString(d)
      return { start: yesterday, end: yesterday }
    }
  },
  {
    labelKey: 'dates.last24Hours',
    value: 'last24Hours',
    getRange: () => {
      const end = new Date()
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      return {
        start: formatDateToString(start),
        end: formatDateToString(end)
      }
    }
  },
  {
    labelKey: 'dates.last7Days',
    value: '7days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 6)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last14Days',
    value: '14days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 13)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last30Days',
    value: '30days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 29)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.thisMonth',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1))
      return { start, end: today.value }
    }
  },
  {
    labelKey: 'dates.lastMonth',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1))
      const end = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0))
      return { start, end }
    }
  }
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const preset = presets.find((p) => p.value === activePreset.value)
    if (preset) return t(preset.labelKey)
  }

  if (localStartDate.value && localEndDate.value) {
    if (localStartDate.value === localEndDate.value) {
      return formatDate(localStartDate.value)
    }
    return `${formatDate(localStartDate.value)} - ${formatDate(localEndDate.value)}`
  }

  return t('dates.selectDateRange')
})

const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr + 'T00:00:00')
  const dateLocale = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return date.toLocaleDateString(dateLocale, { month: 'short', day: 'numeric' })
}

const isPresetActive = (preset: DatePreset): boolean => {
  return activePreset.value === preset.value
}

const selectPreset = (preset: DatePreset) => {
  const range = preset.getRange()
  localStartDate.value = range.start
  localEndDate.value = range.end
  activePreset.value = preset.value
}

const onDateChange = () => {
  // Check if current dates match any preset
  activePreset.value = null
  for (const preset of presets) {
    const range = preset.getRange()
    if (range.start === localStartDate.value && range.end === localEndDate.value) {
      activePreset.value = preset.value
      break
    }
  }
}

const toggle = () => {
  restoreFocus.value = true
  isOpen.value = !isOpen.value
}

function positionDropdown() {
  if (!isOpen.value || !containerRef.value || !dropdownRef.value) return
  const anchor = containerRef.value.getBoundingClientRect()
  const dropdown = dropdownRef.value
  const viewport = window.visualViewport
  const offsetLeft = viewport?.offsetLeft ?? 0
  const offsetTop = viewport?.offsetTop ?? 0
  const width = viewport?.width ?? window.innerWidth
  const height = viewport?.height ?? window.innerHeight
  const margin = 12
  const gap = 8
  const maxWidth = Math.max(0, width - margin * 2)
  // Measure after updating the width constraint: a menu previously narrowed
  // by the keyboard or zoom can grow again when the visible viewport expands.
  dropdown.style.maxWidth = `${maxWidth}px`
  const panel = dropdown.getBoundingClientRect()
  const viewportTop = offsetTop + margin
  const viewportBottom = Math.max(viewportTop, offsetTop + height - margin)
  const below = Math.max(viewportTop, Math.min(anchor.bottom + gap, viewportBottom))
  const above = Math.max(viewportTop, Math.min(anchor.top - gap, viewportBottom))
  const spaceBelow = viewportBottom - below
  const spaceAbove = above - viewportTop
  // The scroll height retains the full menu size after a smaller viewport has
  // constrained it, so scrolling/resizing can still choose the better side.
  const fullHeight = Math.max(panel.height, dropdown.scrollHeight + dropdown.offsetHeight - dropdown.clientHeight)
  const openAbove = fullHeight > spaceBelow && spaceAbove > spaceBelow
  const maxHeight = openAbove ? spaceAbove : spaceBelow
  const top = openAbove ? above - Math.min(fullHeight, maxHeight) : below
  const left = Math.max(offsetLeft + margin, Math.min(anchor.left, offsetLeft + width - panel.width - margin))
  dropdownStyle.value = {
    left: `${left}px`,
    top: `${top}px`,
    maxWidth: `${maxWidth}px`,
    maxHeight: `${maxHeight}px`,
  }
}

watch(isOpen, async (open) => {
  if (!open) return
  await nextTick()
  positionDropdown()
})

const apply = () => {
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    preset: activePreset.value
  })
  closeDropdown()
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as Node | null
  if (target && !containerRef.value?.contains(target) && !dropdownRef.value?.contains(target)) {
    restoreFocus.value = false
    isOpen.value = false
  }
}

// Sync local state with props
watch(
  () => props.startDate,
  (val) => {
    localStartDate.value = val
    onDateChange()
  }
)

watch(
  () => props.endDate,
  (val) => {
    localEndDate.value = val
    onDateChange()
  }
)

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  window.addEventListener('resize', positionDropdown)
  window.addEventListener('scroll', positionDropdown, true)
  window.visualViewport?.addEventListener('resize', positionDropdown)
  window.visualViewport?.addEventListener('scroll', positionDropdown)
  // Initialize active preset detection
  onDateChange()
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  window.removeEventListener('resize', positionDropdown)
  window.removeEventListener('scroll', positionDropdown, true)
  window.visualViewport?.removeEventListener('resize', positionDropdown)
  window.visualViewport?.removeEventListener('scroll', positionDropdown)
})
</script>

<style scoped>
.date-picker-trigger {
  @apply flex items-center gap-2;
  @apply min-w-0 text-sm;
  padding: 6.5px 12px;
  border-radius: var(--surface-radius-control);
  background-color: var(--surface-field);
  @apply border border-gray-200 dark:border-dark-600;
  color: var(--surface-muted);
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.date-picker-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.date-picker-icon {
  color: var(--surface-muted);
}

.date-picker-value {
  @apply min-w-0 font-medium;
  overflow-wrap: anywhere;
}

.date-picker-chevron {
  color: var(--surface-muted);
}

.date-picker-dropdown {
  position: fixed;
  z-index: 100000020;
  background-color: var(--surface-panel);
  border-radius: var(--surface-radius-panel);
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  width: 320px;
  max-width: calc(100vw - 24px);
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply px-3 py-1.5 text-xs font-medium;
  border-radius: var(--surface-radius-item);
  color: var(--surface-muted);

  @apply transition-colors duration-150;
}

.date-picker-preset-active {
  @apply bg-primary-100/[0.15] dark:bg-primary-900/30;
  @apply text-primary-700 dark:text-primary-300;
}

.date-picker-divider {
  @apply border-t border-gray-100 dark:border-dark-700;
}

.date-picker-custom {
  @apply flex items-end gap-2 p-3;
}

.date-picker-field {
  @apply min-w-0 flex-1;
}

.date-picker-label {
  @apply mb-1 block text-xs font-medium;
  color: var(--surface-muted);
}

.date-picker-input {
  @apply w-full px-2 py-1.5 text-sm;
  border-radius: var(--surface-radius-item);
  background-color: var(--surface-field);
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
}

.date-picker-separator {
  @apply flex items-center justify-center pb-1;
}

.date-picker-actions {
  @apply flex justify-end p-2 pt-0;
}

.date-picker-apply {
  @apply px-4 py-1.5 text-sm font-medium;
  border-radius: var(--surface-radius-item);
  @apply bg-primary-600 text-white;
  @apply hover:bg-primary-700;
  @apply transition-colors duration-150;
}

/* Dropdown animation */
.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition: all 0.2s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
