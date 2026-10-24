<template>
  <section class="card" :aria-labelledby="`${id}-title`" :aria-busy="loading || saving || processing">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 :id="`${id}-title`" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.appearance.title') }}</h2>
      <p class="mt-1 text-sm surface-muted">{{ t('admin.settings.appearance.description') }}</p>
    </div>
    <div class="space-y-5 p-6">
      <p v-if="loading" class="text-sm surface-muted">{{ t('common.loading') }}</p>
      <template v-else-if="loaded">
        <div class="flex flex-wrap items-center gap-3">
          <input :id="`${id}-file`" ref="fileInput" class="sr-only" tabindex="-1" type="file" accept="image/jpeg,image/png,image/webp" :disabled="busy" :aria-label="t('admin.settings.appearance.upload')" @change="upload">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="fileInput?.click()">{{ processing ? t('admin.settings.appearance.processing') : t('admin.settings.appearance.upload') }}</button>
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="restoreDefaults">{{ t('admin.settings.appearance.restore') }}</button>
        </div>
        <p class="text-xs surface-muted">{{ t('admin.settings.appearance.uploadHint') }}</p>
        <div class="grid gap-4 sm:grid-cols-2">
          <div v-for="theme in previewThemes" :key="theme.key" class="appearance-preview" :class="`appearance-preview-${theme.key}`">
            <img v-if="previewImageURL" :key="previewImageURL" :src="previewImageURL" alt="" class="absolute inset-0 h-full w-full object-cover" @error="($event.target as HTMLImageElement).style.visibility = 'hidden'">
            <div v-if="draft.background_url" class="absolute inset-0" :style="{ backgroundColor: `rgb(${theme.maskColor} / ${draft[theme.maskKey]}%)` }" />
            <div class="appearance-sample-card" :style="{ backgroundColor: `rgb(${theme.cardColor})` }">
              <span class="text-xs opacity-75">{{ t(`admin.settings.appearance.${theme.key}`) }}</span>
              <p class="mt-2 font-semibold">{{ t('admin.settings.appearance.sample') }}</p>
              <div class="mt-3 h-1.5 w-20 rounded-full bg-teal-500/70" />
            </div>
          </div>
        </div>
        <div class="grid gap-5 sm:grid-cols-2">
          <div v-for="theme in previewThemes" :key="theme.maskKey">
            <div class="mb-2 flex justify-between gap-2 text-sm font-medium surface-muted">
              <label :for="`${id}-${theme.maskKey}`">{{ t(`admin.settings.appearance.${theme.maskKey}`) }}</label>
              <output :for="`${id}-${theme.maskKey}`">{{ draft[theme.maskKey] }}%</output>
            </div>
            <input :id="`${id}-${theme.maskKey}`" v-model.number="draft[theme.maskKey]" class="w-full accent-teal-500" type="range" min="0" max="100" step="1" :disabled="busy || !draft.background_url" @input="previewing = true">
          </div>
        </div>
        <p v-if="previewing" role="status" class="text-sm text-teal-700 dark:text-teal-300">{{ t('admin.settings.appearance.previewNotice') }}</p>
        <div class="flex flex-wrap gap-3">
          <button type="button" class="btn btn-primary" :disabled="busy || !dirty" @click="save">{{ saving ? t('common.saving') : t('admin.settings.appearance.save') }}</button>
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

const props = defineProps<{ active: boolean }>()
const id = 'console-appearance'
const { t } = useI18n()
const appStore = useAppStore()
const fileInput = ref<HTMLInputElement | null>(null)
const saved = ref<ConsoleAppearance>(defaultConsoleAppearance())
const draft = ref<ConsoleAppearance>(defaultConsoleAppearance())
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const processing = ref(false)
const previewing = ref(false)
const error = ref('')
let backgroundImage: string | undefined
let draftURL = ''
let disposed = false
const busy = computed(() => loading.value || saving.value || processing.value)
const previewImageURL = computed(() => resolveConsoleBackgroundURL(draft.value.background_url))
const dirty = computed(() => JSON.stringify(draft.value) !== JSON.stringify(saved.value))
const previewThemes = [
  { key: 'light', maskKey: 'light_mask', maskColor: '245 248 247', cardColor: '248 250 249' },
  { key: 'dark', maskKey: 'dark_mask', maskColor: '15 23 28', cardColor: '38 44 45' }
] as const

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
    saved.value = { ...result }
    draft.value = { ...result }
    loaded.value = true
  } catch { if (!disposed) error.value = t('admin.settings.appearance.loadFailed') }
  finally { loading.value = false }
}

async function upload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  processing.value = true
  error.value = ''
  try {
    const result = await prepareConsoleBackground(file)
    if (disposed) return
    releaseDraftImage()
    draftURL = URL.createObjectURL(result.blob)
    backgroundImage = result.dataURL
    draft.value.background_url = draftURL
    previewing.value = true
  } catch (failure) {
    if (!disposed) error.value = t('admin.settings.appearance.' + (failure instanceof BackgroundImageError ? `imageError_${failure.reason}` : 'imageError_decode'))
  } finally { processing.value = false; input.value = '' }
}

function restoreDefaults() {
  releaseDraftImage()
  draft.value = { ...defaultConsoleAppearance(), card_transparency: saved.value.card_transparency }
  backgroundImage = saved.value.background_url ? '' : undefined
  previewing.value = true
  error.value = ''
}

function discard() {
  previewing.value = false
  draft.value = { ...saved.value }
  backgroundImage = undefined
  releaseDraftImage()
  error.value = ''
}

async function save() {
  if (busy.value || !dirty.value) return
  saving.value = true
  error.value = ''
  const update: ConsoleAppearanceUpdate = {
    light_mask: draft.value.light_mask,
    dark_mask: draft.value.dark_mask,
    card_transparency: draft.value.card_transparency,
    ...(backgroundImage === undefined ? {} : { background_image: backgroundImage })
  }
  try {
    const result = await saveConsoleAppearance(update)
    appStore.applyConsoleAppearance(result)
    if (disposed) return
    saved.value = { ...result }
    draft.value = { ...result }
    backgroundImage = undefined
    previewing.value = false
    releaseDraftImage()
    appStore.showSuccess(t('admin.settings.appearance.saved'))
  } catch { if (!disposed) error.value = t('admin.settings.appearance.saveFailed') }
  finally { saving.value = false }
}

watch(() => props.active, active => { if (!active) previewing.value = false })
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
</style>
