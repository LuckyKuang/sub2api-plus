<template>
  <AppLayout workspace>
    <div class="catalog-page workspace-page min-w-0 space-y-5">
      <div class="card workspace-fixed flex items-start justify-between gap-3 p-4" data-testid="catalog-heading">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('availableChannels.title') }}</h1>
          <p class="mt-1 text-sm surface-muted">{{ t('availableChannels.description') }}</p>
        </div>
        <button ref="refreshButton" class="btn btn-secondary" :disabled="loading" :aria-label="t('common.refresh')" @click="loadCatalog">
          <Icon name="refresh" size="md" :class="{ 'animate-spin': loading }" />
        </button>
      </div>
      <div class="surface-card workspace-fixed space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700" data-testid="catalog-filters">
        <div class="catalog-platforms flex flex-wrap items-center gap-2" role="group" :aria-label="t('modelPlaza.filters.platformLabel')">
          <span class="mr-2 text-xs surface-muted">{{ t('modelPlaza.filters.platformLabel') }}</span>
          <button class="catalog-filter surface-control" :class="{ selected: !filters.platform }" :aria-pressed="!filters.platform" @click="selectPlatform('')">{{ t('modelPlaza.filters.all') }} <span class="opacity-60">{{ models.length }}</span></button>
          <button v-for="platform in platforms" :key="platform" class="catalog-filter surface-control" :class="{ selected: filters.platform === platform }" :aria-pressed="filters.platform === platform" @click="selectPlatform(platform)">
            <PlatformIcon :platform="platform" size="sm" />{{ platform }} <span class="opacity-60">{{ platformCount(platform) }}</span>
          </button>
        </div>
        <div class="flex flex-wrap items-center gap-3 border-t border-gray-100 pt-3 dark:border-dark-800">
          <label class="catalog-search relative w-full min-w-0 max-w-sm">
            <span class="sr-only">{{ t('availableChannels.catalog.searchPlaceholder') }}</span>
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="filters.search" class="input w-full pl-10 pr-10" type="search" :placeholder="t('availableChannels.catalog.searchPlaceholder')" />
          </label>
          <label class="relative">
            <span class="sr-only">{{ t('availableChannels.catalog.sort') }}</span>
            <Select
              v-model="filters.sort"
              :searchable="false"
              :options="[{ value: 'name', label: t('availableChannels.catalog.sortName') }, { value: 'price', label: t('availableChannels.catalog.sortPrice') }]"
            />
          </label>
          <button class="btn btn-secondary" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
        </div>
      </div>
      <div v-if="catalog.user_rate_status === 'unavailable'" role="status" class="workspace-fixed flex flex-wrap items-center gap-3 rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('availableChannels.catalog.ratesUnavailable') }}<button class="underline surface-control" @click="loadCatalog">{{ t('availableChannels.catalog.retry') }}</button>
      </div>
      <p v-if="!loading && !error" class="workspace-fixed text-xs surface-muted" aria-live="polite">{{ t('availableChannels.catalog.modelCount', { count: visible.length }) }}</p>
      <section class="workspace-shell workspace-scroll" :aria-label="t('availableChannels.title')" data-testid="catalog-results">
        <div v-if="loading" class="catalog-grid" aria-busy="true" :aria-label="t('common.loading')">
          <div v-for="i in 12" :key="i" class="glass-surface h-52 animate-pulse" />
        </div>
        <div v-else-if="error" role="alert" class="card py-12 text-center">
          <p class="text-red-600 dark:text-red-400">{{ error }}</p><button class="btn btn-secondary mt-4" @click="loadCatalog">{{ t('availableChannels.catalog.retry') }}</button>
        </div>
        <div v-else-if="!visible.length" class="card py-12 text-center surface-muted">
          <p>{{ models.length ? t('availableChannels.catalog.noMatches') : t('availableChannels.noModels') }}</p>
          <button v-if="models.length" class="btn btn-secondary mt-4" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
        </div>
        <div v-else class="catalog-grid">
          <CatalogModelCard v-for="model in visible" :key="model.key" :model="model" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @details="openDetails" />
        </div>
      </section>
      <CatalogPriceDetails :model="selected" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @close="selectedKey = null" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import Select from '@/components/common/Select.vue'
import { useLocaleRefresh } from '@/utils/localeRefresh'
import { computed, nextTick, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import CatalogModelCard from '@/components/channels/CatalogModelCard.vue'
import CatalogPriceDetails from '@/components/channels/CatalogPriceDetails.vue'
import { catalogModels, defaultCatalogFilters, normalizeCatalogFilters, visibleModels, type CatalogModel } from '@/components/channels/catalog'
import userChannelsAPI, { type ChannelCatalog } from '@/api/channels'
import { useUserView as useAuthStore } from '@/composables/useUserView'
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
useLocaleRefresh(loadCatalog)
</script>

<style scoped>
.catalog-page { container-type: inline-size; }
.catalog-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 20px; }
.catalog-filter { @apply inline-flex items-center gap-1.5 rounded-lg border border-transparent px-3 py-1.5 text-xs text-gray-600 transition-colors focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-dark-300; }
.catalog-filter:hover { background-color: var(--surface-hover); }
.catalog-filter.selected { @apply border-primary-200 bg-primary-50/[0.15] text-primary-700 dark:border-primary-800 dark:bg-primary-900/30 dark:text-primary-300; }
@container (min-width: 460px) { .catalog-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@container (min-width: 700px) { .catalog-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@container (min-width: 940px) { .catalog-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
@media (min-width: 1024px) {
  .catalog-search { flex: 1 1 200px; width: auto; }
}
@media (min-width: 1024px) and (max-height: 600px) {
  .catalog-platforms { flex-wrap: nowrap; overflow-x: auto; scrollbar-width: thin; }
  .catalog-platforms > * { flex-shrink: 0; }
}
</style>
