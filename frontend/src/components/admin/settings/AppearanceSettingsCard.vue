<template>
  <section class="card workspace-shell" :aria-labelledby="`${id}-title`" :aria-busy="loading || saving || processing">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 :id="`${id}-title`" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.appearance.title') }}</h2>
      <p class="mt-1 text-sm surface-muted">{{ t('admin.settings.appearance.description') }}</p>
    </div>
    <div class="space-y-5 p-6">
      <p v-if="loading" class="text-sm surface-muted">{{ t('common.loading') }}</p>
      <template v-else-if="loaded">
        <div class="grid gap-4 sm:grid-cols-2">
          <div class="appearance-control-card">
            <div class="mb-3 flex items-center justify-between gap-3">
              <label :for="`${id}-accent`" class="text-sm font-medium">{{ t('admin.settings.appearance.accent') }}</label>
              <button type="button" class="btn btn-secondary btn-sm" data-testid="appearance-reset-accent" :disabled="busy" @click="setAccent(DEFAULT_CONSOLE_ACCENT)">{{ t('admin.settings.appearance.restore') }}</button>
            </div>
            <div class="flex flex-wrap items-center gap-3">
              <input :id="`${id}-accent`" class="appearance-color-input" type="color" :value="draft.accent_color" :disabled="busy" :aria-label="t('admin.settings.appearance.chooseAccent')" @input="setAccent(($event.target as HTMLInputElement).value)">
              <input :id="`${id}-accent-hex`" class="input appearance-hex-input" type="text" :value="accentText" :disabled="busy" maxlength="7" spellcheck="false" autocapitalize="characters" :aria-label="t('admin.settings.appearance.accentHex')" :aria-invalid="!validAccent" :aria-describedby="`${id}-accent-hint${validAccent ? '' : ` ${id}-accent-error`}`" @input="editAccent" @keydown.enter.prevent>
            </div>
            <div class="mt-3 flex flex-wrap gap-2">
              <button v-for="preset in accentPresets" :key="preset.color" type="button" class="appearance-accent-swatch" :style="{ backgroundColor: preset.color }" :disabled="busy" :aria-label="t(`admin.settings.appearance.${preset.label}`)" :title="t(`admin.settings.appearance.${preset.label}`)" :aria-pressed="draft.accent_color === preset.color" @click="setAccent(preset.color)">
                <span v-if="draft.accent_color === preset.color" aria-hidden="true">✓</span>
              </button>
            </div>
            <p :id="`${id}-accent-hint`" class="mt-3 text-xs surface-muted">{{ t('admin.settings.appearance.accentHint') }}</p>
            <p v-if="!validAccent" :id="`${id}-accent-error`" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ t('admin.settings.appearance.accentInvalid') }}</p>
          </div>
          <div class="appearance-control-card">
            <div class="mb-3 flex items-center justify-between gap-3">
              <label :for="`${id}-transparency`" class="text-sm font-medium">{{ t('admin.settings.appearance.transparency') }}</label>
              <button type="button" class="btn btn-secondary btn-sm" data-testid="appearance-reset-transparency" :disabled="busy" @click="setTransparency(0)">{{ t('admin.settings.appearance.restore') }}</button>
            </div>
            <div class="flex items-center gap-3">
              <input :id="`${id}-transparency`" class="min-w-0 flex-1" type="range" min="0" max="100" step="1" :value="transparency" :disabled="busy" :aria-describedby="`${id}-transparency-hint`" @input="editTransparency">
              <div class="flex shrink-0 items-center gap-1">
                <input :id="`${id}-transparency-number`" class="input appearance-percent-input" type="number" min="0" max="100" step="1" :value="transparency" :disabled="busy" :aria-label="t('admin.settings.appearance.transparency')" @input="editTransparency" @keydown.enter.prevent>
                <span class="text-sm surface-muted">%</span>
              </div>
            </div>
            <div class="mt-2 flex justify-between gap-3 text-xs surface-muted"><span>{{ t('admin.settings.appearance.opaque') }}</span><span>{{ t('admin.settings.appearance.transparent') }}</span></div>
            <p :id="`${id}-transparency-hint`" class="mt-3 text-xs surface-muted">{{ t('admin.settings.appearance.transparencyHint') }}</p>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <input :id="`${id}-file`" ref="fileInput" class="sr-only" tabindex="-1" type="file" accept="image/jpeg,image/png,image/webp" :disabled="busy" :aria-label="t('admin.settings.appearance.upload')" @change="upload">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="fileInput?.click()">{{ processing ? t('admin.settings.appearance.processing') : t('admin.settings.appearance.upload') }}</button>
          <button type="button" class="btn btn-secondary" data-testid="appearance-reset-background" :disabled="busy" @click="restoreDefaults">{{ t('admin.settings.appearance.restore') }}</button>
        </div>
        <p class="text-xs surface-muted">{{ t('admin.settings.appearance.uploadHint') }}</p>
        <div class="grid gap-4 sm:grid-cols-2">
          <div v-for="theme in previewThemes" :key="theme.key" class="appearance-preview" :class="`appearance-preview-${theme.key}`">
            <img v-if="previewImageURL" :key="previewImageURL" :src="previewImageURL" alt="" class="absolute inset-0 h-full w-full object-cover" @error="($event.target as HTMLImageElement).style.visibility = 'hidden'">
            <div v-if="draft.background_url" class="absolute inset-0" :style="{ backgroundColor: `rgb(${theme.maskColor} / ${draft[theme.maskKey]}%)` }" />
            <div class="appearance-sample-card" :style="{ backgroundColor: `rgb(${theme.cardColor} / ${100 - transparency}%)` }">
              <span class="text-xs opacity-75">{{ t(`admin.settings.appearance.${theme.key}`) }}</span>
              <p class="mt-2 font-semibold">{{ t('admin.settings.appearance.sample') }}</p>
              <div class="mt-3 h-1.5 w-20 rounded-full" :style="{ backgroundColor: draft.accent_color }" />
            </div>
          </div>
        </div>
        <div class="grid gap-5 sm:grid-cols-2">
          <div v-for="theme in previewThemes" :key="theme.maskKey">
            <div class="mb-2 flex justify-between gap-2 text-sm font-medium surface-muted">
              <label :for="`${id}-${theme.maskKey}`">{{ t(`admin.settings.appearance.${theme.maskKey}`) }}</label>
              <output :for="`${id}-${theme.maskKey}`">{{ draft[theme.maskKey] }}%</output>
            </div>
            <input :id="`${id}-${theme.maskKey}`" v-model.number="draft[theme.maskKey]" class="w-full" type="range" min="0" max="100" step="1" :disabled="busy || !draft.background_url" @input="previewing = true">
          </div>
        </div>
        <p v-if="previewing" role="status" class="text-sm text-primary-700 dark:text-primary-300">{{ t('admin.settings.appearance.previewNotice') }}</p>
        <div class="flex flex-wrap gap-3">
          <button type="button" class="btn btn-primary" :disabled="busy || !dirty || !validAccent" @click="save">{{ saving ? t('common.saving') : t('admin.settings.appearance.save') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="previewing = !previewing">{{ t(previewing ? 'admin.settings.appearance.stopPreview' : 'admin.settings.appearance.preview') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="busy || !dirty" @click="discard">{{ t('admin.settings.appearance.discard') }}</button>
        </div>
      </template>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <button v-if="!loading && !loaded" type="button" class="btn btn-secondary" @click="load">{{ t('admin.settings.appearance.retry') }}</button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { getConsoleAppearance, saveConsoleAppearance } from '@/api/admin/appearance'
import { defaultConsoleAppearance, type ConsoleAppearance, type ConsoleAppearanceUpdate } from '@/types/appearance'
import { BackgroundImageError, prepareConsoleBackground } from '@/utils/consoleBackground'
import { resolveConsoleBackgroundURL } from '@/utils/consoleAppearance'
import { DEFAULT_CONSOLE_ACCENT, normalizeConsoleAccent } from '@/utils/consoleAccent'

const props = defineProps<{ active: boolean }>()
const id = 'console-appearance'
const { t } = useI18n()
const appStore = useAppStore()
const fileInput = ref<HTMLInputElement | null>(null)
const saved = ref<ConsoleAppearance>(defaultConsoleAppearance())
const draft = ref<ConsoleAppearance>(defaultConsoleAppearance())
const accentText = ref(DEFAULT_CONSOLE_ACCENT.toUpperCase())
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const processing = ref(false)
const previewing = ref(false)
const error = ref('')
let backgroundImage: string | undefined
let draftURL = ''
let disposed = false
let previewRevision = 0
const busy = computed(() => loading.value || saving.value || processing.value)
const previewImageURL = computed(() => resolveConsoleBackgroundURL(draft.value.background_url))
const validAccent = computed(() => /^#[0-9a-f]{6}$/i.test(accentText.value))
const dirty = computed(() => !validAccent.value || JSON.stringify(draft.value) !== JSON.stringify(saved.value))
const transparency = computed(() => draft.value.card_transparency ?? 0)
const accentPresets = [
  { color: DEFAULT_CONSOLE_ACCENT, label: 'accentGreen' },
  { color: '#3b82f6', label: 'accentBlue' },
  { color: '#6366f1', label: 'accentIndigo' },
  { color: '#a855f7', label: 'accentPurple' },
  { color: '#ec4899', label: 'accentPink' },
  { color: '#f59e0b', label: 'accentAmber' }
] as const
const previewThemes = [
  { key: 'light', maskKey: 'light_mask', maskColor: '245 248 247', cardColor: '248 250 249' },
  { key: 'dark', maskKey: 'dark_mask', maskColor: '15 23 28', cardColor: '38 44 45' }
] as const

function normalizeAppearance(value: ConsoleAppearance): ConsoleAppearance {
  return { ...value, accent_color: normalizeConsoleAccent(value.accent_color), card_transparency: value.card_transparency ?? 0 }
}

function setAccent(value: string) {
  draft.value.accent_color = normalizeConsoleAccent(value)
  accentText.value = draft.value.accent_color.toUpperCase()
  previewing.value = true
}

function editAccent(event: Event) {
  accentText.value = (event.target as HTMLInputElement).value.trim()
  if (validAccent.value) {
    draft.value.accent_color = normalizeConsoleAccent(accentText.value)
    previewing.value = true
  }
}

function setTransparency(value: string | number) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return
  draft.value.card_transparency = Math.max(0, Math.min(100, Math.round(parsed)))
  previewing.value = true
}

function editTransparency(event: Event) {
  const input = event.target as HTMLInputElement
  setTransparency(input.value)
  input.value = String(transparency.value)
}

function releaseDraftImage() {
  if (draftURL) URL.revokeObjectURL(draftURL)
  draftURL = ''
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await getConsoleAppearance()
    if (disposed) return
    saved.value = normalizeAppearance(result)
    draft.value = { ...saved.value }
    accentText.value = draft.value.accent_color.toUpperCase()
    loaded.value = true
  } catch { if (!disposed) error.value = t('admin.settings.appearance.loadFailed') }
  finally { loading.value = false }
}

async function upload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const uploadPreviewRevision = previewRevision
  processing.value = true
  error.value = ''
  try {
    const result = await prepareConsoleBackground(file)
    if (disposed) return
    releaseDraftImage()
    draftURL = URL.createObjectURL(result.blob)
    backgroundImage = result.dataURL
    draft.value.background_url = draftURL
    previewing.value = props.active && uploadPreviewRevision === previewRevision
  } catch (failure) {
    if (!disposed) error.value = t('admin.settings.appearance.' + (failure instanceof BackgroundImageError ? `imageError_${failure.reason}` : 'imageError_decode'))
  } finally { processing.value = false; input.value = '' }
}

function restoreDefaults() {
  releaseDraftImage()
  draft.value = { ...draft.value, background_url: '', light_mask: 30, dark_mask: 48 }
  backgroundImage = saved.value.background_url ? '' : undefined
  previewing.value = true
  error.value = ''
}

function discard() {
  previewing.value = false
  draft.value = { ...saved.value }
  accentText.value = draft.value.accent_color.toUpperCase()
  backgroundImage = undefined
  releaseDraftImage()
  error.value = ''
}

async function save() {
  if (busy.value || !dirty.value || !validAccent.value) return
  saving.value = true
  error.value = ''
  const update: ConsoleAppearanceUpdate = {
    light_mask: draft.value.light_mask,
    dark_mask: draft.value.dark_mask,
    accent_color: draft.value.accent_color,
    card_transparency: draft.value.card_transparency,
    ...(backgroundImage === undefined ? {} : { background_image: backgroundImage })
  }
  try {
    const result = normalizeAppearance(await saveConsoleAppearance(update))
    appStore.applyConsoleAppearance(result)
    if (disposed) return
    saved.value = { ...result }
    draft.value = { ...result }
    accentText.value = draft.value.accent_color.toUpperCase()
    backgroundImage = undefined
    previewing.value = false
    releaseDraftImage()
    appStore.showSuccess(t('admin.settings.appearance.saved'))
  } catch { if (!disposed) error.value = t('admin.settings.appearance.saveFailed') }
  finally { saving.value = false }
}

watch(() => props.active, active => { if (!active) { previewing.value = false; previewRevision++ } })
watch([previewing, draft, () => props.active], () => {
  appStore.consoleAppearancePreview = previewing.value && props.active ? { ...draft.value } : null
}, { deep: true })
onMounted(load)
onBeforeUnmount(() => { disposed = true; appStore.consoleAppearancePreview = null; releaseDraftImage() })
</script>

<style scoped>
.appearance-preview { position: relative; display: grid; min-height: 180px; place-items: center; overflow: hidden; border-radius: 16px; border: 1px solid rgb(148 163 184 / 25%); }
.appearance-preview-light { color: #16292b; background: radial-gradient(ellipse at 8% 10%, rgb(20 184 166 / 13%), transparent 55%), #e9efee; }
.appearance-preview-dark { color: #edf6f5; background: radial-gradient(ellipse at 8% 10%, rgb(20 184 166 / 10%), transparent 55%), #191f21; }
.appearance-sample-card { position: relative; width: 76%; padding: 18px; border-radius: 16px; border: 1px solid rgb(148 163 184 / 25%); }
.appearance-control-card { min-width: 0; padding: 20px; border: 1px solid var(--surface-edge); border-radius: 16px; background: var(--surface-field); }
.appearance-color-input { width: 48px; height: 42px; padding: 4px; border: 1px solid var(--surface-edge); border-radius: 10px; background: var(--surface-field); cursor: pointer; }
.appearance-hex-input { width: 136px; font-family: ui-monospace, monospace; text-transform: uppercase; }
.appearance-percent-input { width: 76px; text-align: right; }
.appearance-accent-swatch { display: grid; width: 28px; height: 28px; place-items: center; border: 1px solid var(--surface-edge); border-radius: 8px; color: white; text-shadow: 0 1px 3px rgb(0 0 0 / 50%); }
.appearance-accent-swatch[aria-pressed="true"] { outline: 2px solid var(--surface-text); outline-offset: 2px; }
.appearance-accent-swatch:focus-visible { outline: 2px solid var(--surface-text); outline-offset: 2px; }
</style>
