<template>
  <div ref="root" :class="['date-time-picker input', $attrs.class, { 'date-time-picker-disabled': disabled }]" :style="$attrs.style">
    <!-- Keep native value, constraint validation and form submission semantics.
         This proxy never opens a browser-owned picker. -->
    <input
      ref="valueInput"
      class="date-time-value-proxy"
      :type="type"
      :value="modelValue ?? ''"
      :name="name"
      :min="min"
      :max="max"
      :step="step"
      :required="required"
      :disabled="disabled"
      :readonly="readonly"
      tabindex="-1"
      aria-hidden="true"
      @invalid.prevent="onInvalid"
      @click.prevent.stop="open"
      @focus="trigger?.focus()"
    />
    <button
      v-bind="{ ...$attrs, class: undefined, style: undefined }"
      :id="id"
      ref="trigger"
      type="button"
      :disabled="disabled"
      :aria-readonly="readonly || undefined"
      :aria-required="required || undefined"
      :aria-expanded="opened"
      :aria-controls="opened ? panelId : undefined"
      aria-haspopup="dialog"
      :aria-label="String($attrs['aria-label'] || placeholderText)"
      :class="['date-time-trigger', { 'date-time-trigger-empty': !modelValue }]"
      @click="toggle"
      @keydown.down.prevent="open"
    >
      <span class="date-time-trigger-value">{{ displayValue || placeholderText }}</span>
      <Icon :name="type === 'time' ? 'clock' : 'calendar'" size="sm" class="shrink-0 surface-muted" />
    </button>
    <Teleport to="body">
      <div
        v-if="opened"
        :id="panelId"
        ref="panel"
        class="date-time-panel surface-panel"
        :style="panelStyle"
        role="dialog"
        :aria-label="placeholderText"
        tabindex="-1"
        @click.stop
        @mousedown.stop
      >
        <div v-if="type !== 'time'" class="date-time-calendar">
          <div class="date-time-month-nav">
            <button type="button" class="date-time-icon-button" :aria-label="t('dates.previousMonth')" :disabled="!canMoveMonth(-1)" @click="moveMonth(-1)">
              <Icon name="chevronLeft" size="sm" />
            </button>
            <span class="date-time-month-label" aria-live="polite">{{ monthLabel }}</span>
            <button type="button" class="date-time-icon-button" :aria-label="t('dates.nextMonth')" :disabled="!canMoveMonth(1)" @click="moveMonth(1)">
              <Icon name="chevronRight" size="sm" />
            </button>
          </div>
          <div class="date-time-weekdays" aria-hidden="true"><span v-for="day in weekdays" :key="day">{{ day }}</span></div>
          <div class="date-time-days" role="grid" :aria-label="monthLabel">
            <div v-for="(week, index) in calendarWeeks" :key="index" class="date-time-week" role="row">
            <button
              v-for="day in week"
              :key="day.value"
              type="button"
              role="gridcell"
              :data-date="day.value"
              :aria-label="day.label"
              :aria-selected="day.value === draftDate"
              :aria-current="day.value === todayValue ? 'date' : undefined"
              :disabled="day.disabled"
              :tabindex="day.value === focusedDate ? 0 : -1"
              :class="['date-time-day', { 'date-time-day-outside': day.outside, 'date-time-day-selected': day.value === draftDate, 'date-time-day-today': day.value === todayValue }]"
              @click="selectDay(day.value)"
              @keydown="onDayKey($event, day.value)"
            >{{ day.number }}</button>
            </div>
          </div>
        </div>
        <div v-if="type !== 'date'" class="date-time-clock" :aria-label="t('dates.time')">
          <div v-for="part in timeParts" :key="part.key" class="date-time-clock-part">
            <label :for="`${panelId}-${part.key}`">{{ part.label }}</label>
            <div class="date-time-stepper">
              <button type="button" :aria-label="t('dates.decrease', { unit: part.label })" @click="adjustTime(part.key, -1)"><svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path stroke-linecap="round" d="M5 12h14" /></svg></button>
              <input
                :id="`${panelId}-${part.key}`"
                type="number"
                :inputmode="part.key === 'seconds' && fractionalSeconds ? 'decimal' : 'numeric'"
                min="0"
                :max="part.max"
                :step="part.key === 'seconds' && (step === 'any' || Number(step) > 0 && Number(step) < 60) ? step : 1"
                :value="timeValue(part.key)"
                :aria-label="part.label"
                @input="setTime(part.key, $event)"
                @blur="normalizeTime(part.key)"
              />
              <button type="button" :aria-label="t('dates.increase', { unit: part.label })" @click="adjustTime(part.key, 1)"><Icon name="plus" size="sm" /></button>
            </div>
          </div>
        </div>
        <p v-if="!draftValid" class="date-time-validation" role="status">{{ t('dates.outOfRange') }}</p>
        <div class="date-time-actions">
          <button type="button" class="btn btn-ghost btn-sm date-time-clear" @click="commit('')">{{ t('common.clear') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" @click="close">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary btn-sm" :disabled="!draftValid" @click="apply">{{ t('dates.apply') }}</button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import type { CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useDialogFocus } from '@/composables/useDialogFocus'

defineOptions({ inheritAttrs: false })
type TimePart = 'hours' | 'minutes' | 'seconds'
const props = withDefaults(defineProps<{
  modelValue?: string | null
  type?: 'date' | 'datetime-local' | 'time'
  min?: string
  max?: string
  step?: string | number
  disabled?: boolean
  readonly?: boolean
  required?: boolean
  name?: string
  id?: string
  placeholder?: string
}>(), { type: 'datetime-local', disabled: false, readonly: false, required: false })
const emit = defineEmits<{
  'update:modelValue': [value: string]
  input: [event: Event]
  change: [event: Event]
}>()
const { t, locale } = useI18n()
const panelId = `date-time-${useId()}`
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const valueInput = ref<HTMLInputElement | null>(null)
const opened = ref(false)
const restoreFocus = ref(true)
const draftDate = ref('')
const focusedDate = ref('')
const hours = ref('00')
const minutes = ref('00')
const seconds = ref('00')
const monthYear = ref(new Date().getFullYear())
const monthIndex = ref(new Date().getMonth())
const panelStyle = ref<CSSProperties>({})
function pad(value: number) {
  const [integer, decimals] = String(value).split('.')
  return `${integer!.padStart(2, '0')}${decimals === undefined ? '' : `.${decimals}`}`
}
function dateValue(date: Date) { return `${String(date.getFullYear()).padStart(4, '0')}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` }
function localDate(value: string): Date {
  const [year, month, day] = value.split('-').map(Number)
  const result = new Date(0)
  result.setFullYear(year, month - 1, day)
  result.setHours(12, 0, 0, 0)
  return result
}
const todayValue = computed(() => dateValue(new Date()))
const placeholderText = computed(() => props.placeholder || t(props.type === 'time' ? 'dates.selectTime' : props.type === 'date' ? 'dates.selectDate' : 'dates.selectDateTime'))
const displayValue = computed(() => String(props.modelValue ?? '').replace('T', ' '))
const includeSeconds = computed(() => props.type !== 'date' && (props.step === 'any' || (Number(props.step) > 0 && Number(props.step) < 60) || [props.modelValue, props.min, props.max].some(value => /:\d{2}:\d{2}/.test(`T${value ?? ''}`))))
const fractionalSeconds = computed(() => props.step === 'any' || Number(props.step) > 0 && Number(props.step) < 1)
const timeParts = computed(() => [
  { key: 'hours' as const, label: t('dates.hours'), max: 23 },
  { key: 'minutes' as const, label: t('dates.minutes'), max: 59 },
  ...(includeSeconds.value ? [{ key: 'seconds' as const, label: t('dates.seconds'), max: fractionalSeconds.value ? undefined : 59 }] : []),
])
const monthDate = computed(() => { const d = new Date(0); d.setFullYear(monthYear.value, monthIndex.value, 1); return d })
const monthLabel = computed(() => new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long' }).format(monthDate.value))
const weekdays = computed(() => Array.from({ length: 7 }, (_, i) => new Intl.DateTimeFormat(locale.value, { weekday: 'short' }).format(new Date(2026, 5, 8 + i))))
function dateDisabled(value: string) {
  return Boolean((props.min && compareDates(value, props.min) < 0) || (props.max && compareDates(value, props.max) > 0))
}
function compareDates(left: string, right: string) {
  const a = left.split('T')[0]!.split('-').map(Number)
  const b = right.split('T')[0]!.split('-').map(Number)
  for (let index = 0; index < 3; index++) { const difference = a[index]! - b[index]!; if (difference) return difference }
  return 0
}
const calendarDays = computed(() => {
  const first = localDate(dateValue(monthDate.value))
  first.setDate(1 - (first.getDay() + 6) % 7)
  return Array.from({ length: 42 }, (_, index) => {
    const day = new Date(first)
    day.setDate(first.getDate() + index)
    const value = dateValue(day)
    return { value, number: day.getDate(), outside: day.getMonth() !== monthIndex.value, disabled: day.getFullYear() < 1 || dateDisabled(value), label: new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long', day: 'numeric' }).format(day) }
  })
})
const calendarWeeks = computed(() => Array.from({ length: 6 }, (_, index) => calendarDays.value.slice(index * 7, index * 7 + 7)))
const draftValue = computed(() => {
  if (props.type === 'date') return draftDate.value
  const clock = `${pad(Number(hours.value))}:${pad(Number(minutes.value))}${includeSeconds.value ? `:${pad(Number(seconds.value))}` : ''}`
  return props.type === 'time' ? clock : `${draftDate.value}T${clock}`
})
function millisecondValue(value: string) {
  return value.replace(/\.(\d{1,2})$/, (_, digits: string) => `.${digits.padEnd(3, '0')}`)
}
// Let the browser enforce the original input's calendar, range and step rules,
// without displaying its platform-specific popup or converting to UTC.
const draftValid = computed(() => {
  if (props.type !== 'date' && timeParts.value.some(part => {
    const pattern = part.key === 'seconds' && fractionalSeconds.value ? /^\d+(?:\.\d+)?$/ : /^\d+$/
    const value = Number(timeValue(part.key))
    return !pattern.test(timeValue(part.key)) || value < 0 || value >= (part.key === 'hours' ? 24 : 60)
  })) return false
  const input = document.createElement('input')
  input.type = props.type
  // Compare fractional values in full milliseconds so equivalent .9 and .900
  // times use the same numeric range and step semantics in DOM implementations.
  if (props.min) input.min = millisecondValue(props.min)
  if (props.max) input.max = millisecondValue(props.max)
  if (props.step !== undefined) input.step = String(props.step)
  input.value = millisecondValue(draftValue.value)
  return Boolean(input.value) && input.validity.valid
})
function timeValue(part: TimePart) { return ({ hours, minutes, seconds })[part].value }
function setTime(part: TimePart, event: Event) { ({ hours, minutes, seconds })[part].value = (event.target as HTMLInputElement).value }
function normalizeTime(part: TimePart) { const target = ({ hours, minutes, seconds })[part]; if (target.value !== '') target.value = pad(Number(target.value)) }
function adjustTime(part: TimePart, delta: number) {
  const target = ({ hours, minutes, seconds })[part]
  const limit = part === 'hours' ? 24 : 60
  const amount = part === 'seconds' && Number(props.step) > 0 && Number(props.step) < 60 ? Number(props.step) : 1
  // Native time values have millisecond precision; integer arithmetic avoids
  // introducing binary floating-point digits when a fractional second wraps.
  const units = limit * 1000
  const next = Math.round(Number(target.value || 0) * 1000) + delta * Math.round(amount * 1000)
  target.value = pad(((next % units + units) % units) / 1000)
}
function canMoveMonth(delta: number) {
  const date = new Date(monthDate.value)
  date.setMonth(date.getMonth() + delta)
  if (date.getFullYear() < 1) return false
  const first = dateValue(date)
  date.setMonth(date.getMonth() + 1, 0)
  return !(props.min && compareDates(dateValue(date), props.min) < 0) && !(props.max && compareDates(first, props.max) > 0)
}
function moveMonth(delta: number) {
  if (!canMoveMonth(delta)) return
  const date = new Date(monthDate.value)
  date.setMonth(date.getMonth() + delta)
  monthYear.value = date.getFullYear()
  monthIndex.value = date.getMonth()
  focusedDate.value = dateValue(date)
}
function selectDay(value: string) {
  draftDate.value = value
  focusedDate.value = value
  const date = localDate(value)
  monthYear.value = date.getFullYear()
  monthIndex.value = date.getMonth()
}
async function onDayKey(event: KeyboardEvent, value: string) {
  const offsets: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
  const date = localDate(value)
  if (event.key in offsets) date.setDate(date.getDate() + offsets[event.key]!)
  else if (event.key === 'Home') date.setDate(date.getDate() - (date.getDay() + 6) % 7)
  else if (event.key === 'End') date.setDate(date.getDate() + 6 - (date.getDay() + 6) % 7)
  else if (event.key === 'PageUp' || event.key === 'PageDown') {
    const day = date.getDate()
    date.setDate(1)
    date.setMonth(date.getMonth() + (event.key === 'PageUp' ? -1 : 1))
    const lastDay = new Date(date)
    lastDay.setMonth(lastDay.getMonth() + 1, 0)
    date.setDate(Math.min(day, lastDay.getDate()))
  }
  else return
  event.preventDefault()
  const next = dateValue(date)
  if (date.getFullYear() < 1 || dateDisabled(next)) return
  focusedDate.value = next
  monthYear.value = date.getFullYear()
  monthIndex.value = date.getMonth()
  await nextTick()
  panel.value?.querySelector<HTMLButtonElement>(`[data-date="${next}"]`)?.focus()
}
function initializeDraft() {
  const value = props.modelValue ?? ''
  const datePart = props.type === 'time' ? '' : value.split('T')[0]
  const clock = props.type === 'time' ? value : value.split('T')[1] || ''
  const match = /^(\d{2}):(\d{2})(?::(\d{2})(\.\d+)?)?$/.exec(clock)
  hours.value = match?.[1] ?? '00'
  minutes.value = match?.[2] ?? '00'
  seconds.value = `${match?.[3] ?? '00'}${match?.[4] ?? ''}`
  let date = datePart && /^\d{4,}-\d{2}-\d{2}$/.test(datePart) ? datePart : todayValue.value
  if (props.type !== 'time' && dateDisabled(date)) date = (props.min || props.max)?.split('T')[0] || date
  draftDate.value = date
  focusedDate.value = date
  const current = localDate(date)
  monthYear.value = current.getFullYear()
  monthIndex.value = current.getMonth()
}
function editable() { return !props.disabled && !props.readonly && !trigger.value?.disabled && !valueInput.value?.readOnly && !valueInput.value?.disabled }
function toggle() { if (opened.value) close(); else void open() }
async function open() {
  if (!editable() || opened.value) return
  initializeDraft()
  restoreFocus.value = true
  opened.value = true
  await nextTick()
  positionPanel()
}
function close() { restoreFocus.value = true; opened.value = false }
function commit(value: string) {
  if (!editable()) { close(); return }
  const changed = value !== String(props.modelValue ?? '')
  close()
  if (!changed) return
  if (valueInput.value) valueInput.value.value = value
  emit('update:modelValue', value)
  for (const type of ['input', 'change'] as const) {
    const event = new Event(type, { bubbles: true })
    valueInput.value?.dispatchEvent(event)
    if (type === 'input') emit('input', event)
    else emit('change', event)
  }
}
function apply() { if (draftValid.value) commit(draftValue.value) }
function onInvalid() { void open(); trigger.value?.focus() }
function positionPanel() {
  if (!opened.value || !panel.value || !trigger.value) return
  const anchor = trigger.value.getBoundingClientRect()
  const popup = panel.value.getBoundingClientRect()
  const viewport = window.visualViewport
  const offsetLeft = viewport?.offsetLeft ?? 0
  const offsetTop = viewport?.offsetTop ?? 0
  const width = viewport?.width ?? window.innerWidth
  const height = viewport?.height ?? window.innerHeight
  const margin = 12
  const left = Math.max(offsetLeft + margin, Math.min(anchor.left, offsetLeft + width - popup.width - margin))
  const bottom = offsetTop + height - margin
  const top = anchor.bottom + 8 + popup.height <= bottom ? anchor.bottom + 8 : Math.max(offsetTop + margin, Math.min(anchor.top - popup.height - 8, bottom - popup.height))
  panelStyle.value = { left: `${left}px`, top: `${top}px`, maxWidth: `${Math.max(0, width - margin * 2)}px`, maxHeight: `${Math.max(0, height - margin * 2)}px` }
}
function onOutside(event: MouseEvent) {
  const target = event.target as Node | null
  if (!opened.value || (target && (root.value?.contains(target) || panel.value?.contains(target)))) return
  restoreFocus.value = false
  opened.value = false
}
useDialogFocus(() => opened.value, panel, close, () => true, { lockScroll: false, restoreFocus: () => restoreFocus.value })
watch(() => [props.disabled, props.readonly], ([disabled, readonly]) => { if (disabled || readonly) close() })
watch(() => [draftDate.value, includeSeconds.value], async () => { await nextTick(); positionPanel() })
onMounted(() => {
  document.addEventListener('click', onOutside, true)
  window.addEventListener('resize', positionPanel)
  window.addEventListener('scroll', positionPanel, true)
  window.visualViewport?.addEventListener('resize', positionPanel)
  window.visualViewport?.addEventListener('scroll', positionPanel)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onOutside, true)
  window.removeEventListener('resize', positionPanel)
  window.removeEventListener('scroll', positionPanel, true)
  window.visualViewport?.removeEventListener('resize', positionPanel)
  window.visualViewport?.removeEventListener('scroll', positionPanel)
})
</script>

<style scoped>
.date-time-picker { position: relative; min-width: 0; }
.date-time-picker:focus-within { border-color: rgb(var(--console-accent-500, 20 184 166)); box-shadow: 0 0 0 2px rgb(var(--console-accent-500, 20 184 166) / 30%); }
.date-time-picker-disabled { opacity: 0.6; cursor: not-allowed; }
.date-time-value-proxy { position: absolute; width: 1px; height: 1px; opacity: 0; pointer-events: none; overflow: hidden; }
.date-time-trigger { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 0.5rem; text-align: left; min-width: 0; border-radius: inherit; outline: none; }
.date-time-trigger-value { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.date-time-trigger-empty { color: var(--surface-muted); }
.date-time-trigger[aria-readonly=true] { cursor: default; }
.date-time-panel.surface-panel { position: fixed; z-index: 100000030; width: 352px; max-width: calc(100vw - 24px); overflow: auto; overscroll-behavior: contain; padding: 16px; border: 1px solid var(--surface-edge); border-radius: 20px; box-shadow: var(--surface-shadow); }
.date-time-month-nav { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 10px; }
.date-time-month-label { font-size: 14px; font-weight: 600; }
.date-time-icon-button { display: flex; align-items: center; justify-content: center; width: 32px; height: 32px; border-radius: var(--surface-radius-item); }
.date-time-weekdays, .date-time-days { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: 4px; }
.date-time-week { display: contents; }
.date-time-weekdays { margin-bottom: 6px; text-align: center; font-size: 11px; color: var(--surface-muted); }
.date-time-day { min-width: 0; height: 36px; border: 1px solid transparent; border-radius: var(--surface-radius-item); font-size: 13px; font-variant-numeric: tabular-nums; }
.date-time-day-outside { color: var(--surface-muted); opacity: 0.65; }
.date-time-day-today { border-color: var(--surface-edge); }
.date-time-day-selected { color: var(--console-accent-readable, var(--surface-text)); background: rgb(var(--console-accent-500, 20 184 166)); border-color: rgb(var(--console-accent-500, 20 184 166)); opacity: 1; }
.date-time-panel button:not(:disabled):hover { background-color: var(--surface-hover); }
.date-time-panel .date-time-day-selected:not(:disabled):hover { background-color: rgb(var(--console-accent-400, 45 212 191)); }
.date-time-panel button:disabled { cursor: not-allowed; opacity: 0.35; }
.date-time-panel :is(button, input):focus-visible { outline: 2px solid rgb(var(--console-accent-500, 20 184 166) / 60%); outline-offset: 1px; }
.date-time-clock { display: flex; flex-wrap: wrap; justify-content: center; gap: 10px; margin-top: 12px; padding: 12px; border: 1px solid var(--surface-edge); border-radius: 14px; background: var(--surface-soft); }
.date-time-clock:first-child { margin-top: 0; }
.date-time-clock-part { flex: 1 0 90px; min-width: 0; text-align: center; }
.date-time-clock-part label { display: block; margin-bottom: 6px; font-size: 11px; color: var(--surface-muted); }
.date-time-stepper { display: flex; align-items: center; gap: 2px; padding: 3px; border: 1px solid var(--surface-edge); border-radius: var(--surface-radius-control); background: var(--surface-field); }
.date-time-stepper button { display: flex; flex: none; align-items: center; justify-content: center; width: 24px; height: 30px; border-radius: 8px; }
.date-time-stepper input { width: 100%; min-width: 0; border: 0; padding: 4px 0; background: transparent; color: var(--surface-text); text-align: center; font-size: 16px; font-variant-numeric: tabular-nums; }
.date-time-actions { display: flex; justify-content: flex-end; align-items: center; gap: 6px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--surface-edge); }
.date-time-actions .btn { padding-inline: 10px; }
.date-time-clear { margin-right: auto; }
.date-time-validation { margin-top: 8px; font-size: 12px; color: #dc2626; }
.dark .date-time-validation { color: #fca5a5; }
</style>
