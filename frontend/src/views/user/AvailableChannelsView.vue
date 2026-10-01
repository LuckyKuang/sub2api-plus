<template>
  <AppLayout>
    <div class="catalog-page min-w-0 space-y-5">
      <div class="flex items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('availableChannels.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('availableChannels.description') }}</p>
        </div>
        <button ref="refreshButton" class="btn btn-secondary" :disabled="loading" :aria-label="t('common.refresh')" @click="loadCatalog">
          <Icon name="refresh" size="md" :class="{ 'animate-spin': loading }" />
        </button>
      </div>
      <div class="space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-wrap items-center gap-2" role="group" :aria-label="t('modelPlaza.filters.platformLabel')">
          <span class="mr-2 text-xs text-gray-500 dark:text-dark-400">{{ t('modelPlaza.filters.platformLabel') }}</span>
          <button class="catalog-filter" :class="{ selected: !filters.platform }" :aria-pressed="!filters.platform" @click="selectPlatform('')">{{ t('modelPlaza.filters.all') }} <span class="opacity-60">{{ models.length }}</span></button>
          <button v-for="platform in platforms" :key="platform" class="catalog-filter" :class="{ selected: filters.platform === platform }" :aria-pressed="filters.platform === platform" @click="selectPlatform(platform)">
            <PlatformIcon :platform="platform" size="sm" />{{ platform }} <span class="opacity-60">{{ platformCount(platform) }}</span>
          </button>
        </div>
        <div class="flex flex-wrap items-center gap-3 border-t border-gray-100 pt-3 dark:border-dark-800">
          <label class="relative w-full min-w-0 max-w-sm">
            <span class="sr-only">{{ t('availableChannels.catalog.searchPlaceholder') }}</span>
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="filters.search" class="input w-full pl-10 pr-10" type="search" :placeholder="t('availableChannels.catalog.searchPlaceholder')" />
          </label>
          <label class="relative">
            <span class="sr-only">{{ t('availableChannels.catalog.sort') }}</span>
            <select v-model="filters.sort" class="input appearance-none pl-3 pr-9">
              <option value="name">{{ t('availableChannels.catalog.sortName') }}</option>
              <option value="price">{{ t('availableChannels.catalog.sortPrice') }}</option>
            </select>
            <Icon name="chevronDown" size="sm" class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 dark:text-dark-400" />
          </label>
          <button class="btn btn-secondary" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
        </div>
      </div>
      <div v-if="catalog.user_rate_status === 'unavailable'" role="status" class="flex flex-wrap items-center gap-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('availableChannels.catalog.ratesUnavailable') }}<button class="underline" @click="loadCatalog">{{ t('availableChannels.catalog.retry') }}</button>
      </div>
      <p v-if="!loading && !error" class="text-xs text-gray-500 dark:text-dark-400" aria-live="polite">{{ t('availableChannels.catalog.modelCount', { count: visible.length }) }}</p>
      <div v-if="loading" class="catalog-grid" aria-busy="true" :aria-label="t('common.loading')">
        <div v-for="i in 12" :key="i" class="h-52 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-800" />
      </div>
      <div v-else-if="error" role="alert" class="py-12 text-center">
        <p class="text-red-600 dark:text-red-400">{{ error }}</p><button class="btn btn-secondary mt-4" @click="loadCatalog">{{ t('availableChannels.catalog.retry') }}</button>
      </div>
      <div v-else-if="!visible.length" class="py-12 text-center text-gray-500 dark:text-dark-400">
        <p>{{ models.length ? t('availableChannels.catalog.noMatches') : t('availableChannels.noModels') }}</p>
        <button v-if="models.length" class="btn btn-secondary mt-4" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
      </div>
      <div v-else class="catalog-grid">
        <CatalogModelCard v-for="model in visible" :key="model.key" :model="model" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @details="openDetails" />
      </div>
      <CatalogPriceDetails :model="selected" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @close="selectedKey = null" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import CatalogModelCard from '@/components/channels/CatalogModelCard.vue'
import CatalogPriceDetails from '@/components/channels/CatalogPriceDetails.vue'
import { catalogModels, defaultCatalogFilters, normalizeCatalogFilters, visibleModels, type CatalogModel } from '@/components/channels/catalog'
import userChannelsAPI, { type ChannelCatalog } from '@/api/channels'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const auth = useAuthStore()
const emptyCatalog = (): ChannelCatalog => ({ groups: [], user_rate_status: 'not_requested' })
const catalog = ref<ChannelCatalog>(emptyCatalog())
const filters = reactive(defaultCatalogFilters())
const loading = ref(false)
const error = ref('')
const selectedKey = ref<string | null>(null)
const refreshButton = ref<HTMLButtonElement>()
let requestId = 0
let controller: AbortController | undefined
const models = computed(() => catalogModels(catalog.value.groups))
const platforms = computed(() => [...new Set(models.value.map(m => m.platform))].sort())
const visible = computed(() => visibleModels(models.value, filters))
const selected = computed(() => models.value.find(m => m.key === selectedKey.value) || null)
function platformCount(platform: string) { return models.value.filter(m => m.platform === platform).length }
function selectPlatform(platform: string) {
  filters.platform = platform
  normalizeCatalogFilters(models.value, filters)
}
function reset() { Object.assign(filters, defaultCatalogFilters()) }
function openDetails(model: CatalogModel) { selectedKey.value = model.key }
async function loadCatalog() {
  controller?.abort()
  const id = ++requestId
  const user = auth.user?.id
  if (user == null) return
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    const result = await userChannelsAPI.getCatalog({ signal: controller.signal })
    if (id !== requestId || auth.user?.id !== user) return
    catalog.value = result
    normalizeCatalogFilters(models.value, filters)
  } catch (err) {
    if (id !== requestId || auth.user?.id !== user) return
    catalog.value = emptyCatalog()
    error.value = extractApiErrorMessage(err, t('availableChannels.catalog.loadFailed'))
  } finally {
    if (id === requestId) {
      loading.value = false
      if (selectedKey.value && !selected.value) {
        selectedKey.value = null
        await nextTick()
        refreshButton.value?.focus()
      }
    }
  }
}
watch(() => auth.user?.id, () => {
  ++requestId
  controller?.abort()
  catalog.value = emptyCatalog()
  selectedKey.value = null
  loading.value = false
  error.value = ''
  reset()
  void loadCatalog()
}, { immediate: true })
onUnmounted(() => { ++requestId; controller?.abort() })
</script>

<style scoped>
.catalog-page { container-type: inline-size; }
.catalog-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; }
.catalog-filter { @apply inline-flex items-center gap-1.5 rounded-lg border border-transparent px-3 py-1.5 text-xs text-gray-600 transition-colors hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-dark-300 dark:hover:bg-dark-800; }
.catalog-filter.selected { @apply border-primary-200 bg-primary-50 text-primary-700 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-300; }
@container (min-width: 452px) { .catalog-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@container (min-width: 684px) { .catalog-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@container (min-width: 916px) { .catalog-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
@container (min-width: 1148px) { .catalog-grid { grid-template-columns: repeat(5, minmax(0, 1fr)); } }
@container (min-width: 1380px) { .catalog-grid { grid-template-columns: repeat(6, minmax(0, 1fr)); } }
</style>
