<template>
  <AppLayout appearance="user" workspace>
    <div class="catalog-page workspace-page min-w-0 space-y-5">
      <div class="card workspace-fixed flex items-start justify-between gap-3 p-4" data-testid="catalog-heading">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">{{ t('availableChannels.title') }}</h1>
          <p class="mt-1 text-sm surface-muted">{{ t('availableChannels.description') }}</p>
        </div>
        <button ref="refreshButton" class="btn btn-secondary" :disabled="loading" :aria-label="t('common.refresh')" @click="loadCatalog(true)">
          <Icon name="refresh" size="md" :class="{ 'animate-spin': loading }" />
        </button>
      </div>
      <div class="surface-card workspace-fixed space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700" data-testid="catalog-filters">
        <div v-if="categoryOptions.length > 1" class="segmented-control" role="tablist" :aria-label="t('availableChannels.catalog.category')">
          <button v-for="item in categoryOptions" :key="item.value" type="button" class="segmented-option" role="tab" :aria-selected="category === item.value" @click="selectCategory(item.value)">{{ t(`availableChannels.catalog.${item.value}Platforms`) }}</button>
        </div>
        <div class="catalog-platforms flex flex-wrap items-center gap-2">
          <span class="mr-2 text-xs surface-muted">{{ t('modelPlaza.filters.platformLabel') }}</span>
          <div class="segmented-control" role="group" :aria-label="t('modelPlaza.filters.platformLabel')">
            <button type="button" class="segmented-option" :aria-pressed="!platform" @click="selectPlatform('')">{{ t('modelPlaza.filters.all') }} <span class="opacity-60">{{ platformTotal }}</span></button>
            <button v-for="item in platformOptions" :key="item.platform" type="button" class="segmented-option" :aria-pressed="platform === item.platform" @click="selectPlatform(item.platform)">
              <PlatformIcon :platform="item.platform" size="sm" />{{ item.platform }} <span class="opacity-60">{{ item.count }}</span>
            </button>
          </div>
        </div>
        <div class="filter-bar border-t border-gray-100 pt-3 dark:border-dark-800">
          <label class="filter-field filter-field--search relative">
            <span class="sr-only">{{ t('availableChannels.catalog.searchPlaceholder') }}</span>
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input :value="search" class="input w-full pl-10 pr-10" type="search" :placeholder="t('availableChannels.catalog.searchPlaceholder')" @input="onSearchInput(($event.target as HTMLInputElement).value)" />
          </label>
          <button class="btn btn-secondary" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
        </div>
      </div>
      <div v-if="catalog.user_rate_status === 'unavailable'" role="status" class="workspace-fixed flex flex-wrap items-center gap-3 rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('availableChannels.catalog.ratesUnavailable') }}<button class="underline surface-control" @click="loadCatalog(true)">{{ t('availableChannels.catalog.retry') }}</button>
      </div>
      <p v-if="!loading && !error" class="workspace-fixed text-xs surface-muted" aria-live="polite" :aria-busy="detailLoading">
        {{ t('availableChannels.catalog.modelCount', { count: modelsTotal }) }}<template v-if="detailLoading"> · {{ t('availableChannels.catalog.detailsLoading') }}</template>
      </p>
      <section class="workspace-shell workspace-scroll" :aria-label="t('availableChannels.title')" data-testid="catalog-results">
        <div v-if="loading" :class="app.availableChannelsLayout === 'list' ? 'space-y-3' : 'catalog-grid'" aria-busy="true" :aria-label="t('common.loading')">
          <div v-for="i in PAGE_SIZE" :key="i" class="glass-surface animate-pulse" :class="app.availableChannelsLayout === 'list' ? 'h-16' : 'h-52'" />
        </div>
        <div v-else-if="error" role="alert" class="card py-12 text-center">
          <p class="text-red-600 dark:text-red-400">{{ error }}</p><button class="btn btn-secondary mt-4" @click="loadCatalog(true)">{{ t('availableChannels.catalog.retry') }}</button>
        </div>
        <div v-else-if="!models.length" class="card py-12 text-center surface-muted">
          <p>{{ modelsTotal ? t('availableChannels.catalog.noMatches') : t('availableChannels.noModels') }}</p>
          <button v-if="modelsTotal || search || platform" class="btn btn-secondary mt-4" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
        </div>
        <CatalogModelList v-else-if="app.availableChannelsLayout === 'list'" :models="models" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @details="openDetails" />
        <div v-else class="catalog-grid">
          <CatalogModelCard v-for="model in models" :key="model.key" :model="model" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @details="openDetails" />
        </div>
      </section>
      <Pagination
        v-if="modelsTotal > PAGE_SIZE"
        class="workspace-fixed"
        :page="page"
        :total="modelsTotal"
        :page-size="PAGE_SIZE"
        :show-page-size-selector="false"
        @update:page="onPageChange"
      />
      <CatalogPriceDetails :model="selectedModel" :rate-unavailable="catalog.user_rate_status === 'unavailable'" @close="closeDetails" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import Pagination from '@/components/common/Pagination.vue'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import CatalogModelCard from '@/components/channels/CatalogModelCard.vue'
import CatalogModelList from '@/components/channels/CatalogModelList.vue'
import { useAppStore } from '@/stores/app'
import CatalogPriceDetails from '@/components/channels/CatalogPriceDetails.vue'
import { catalogModels, type CatalogModel } from '@/components/channels/catalog'
import userChannelsAPI, { type ChannelCatalog } from '@/api/channels'
import { useUserView } from '@/composables/useUserView'
import { useAuthStore } from '@/stores/auth'
import { getLocale } from '@/i18n'
import { adminSupportContext } from '@/utils/adminSupportContext'
import { availableChannelCatalogGeneration, cacheAvailableChannelCatalog, invalidateAvailableChannelCatalog, readAvailableChannelCatalog } from '@/utils/availableChannelCatalogCache'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const userView = useUserView()
const PAGE_SIZE = 9
const SEARCH_DEBOUNCE_MS = 300

const catalogIdentity = computed(() => {
  const actorId = auth.user?.id
  const userId = userView.user?.id
  const support = adminSupportContext.value
  if (!auth.isAuthenticated || actorId == null || userId == null || (support && support.actorId !== actorId)) return null
  return JSON.stringify([availableChannelCatalogGeneration(), actorId, support ? userId : null])
})

// Re-reads follow the actor/support identity and the API locale, so a locale
// switch re-queries instead of showing the previous language's group names.
const catalogKey = computed(() => catalogIdentity.value === null ? null : JSON.stringify([catalogIdentity.value, getLocale()]))
const emptyCatalog = (): ChannelCatalog => ({ groups: [], user_rate_status: 'not_requested' })
const catalog = ref<ChannelCatalog>(emptyCatalog())
const category = ref<'group' | 'composite'>('group')
const platform = ref('')
const search = ref('')
const page = ref(1)
const loading = ref(false)
const error = ref('')
const selectedKey = ref<string | null>(null)
const selectedModel = ref<CatalogModel | null>(null)
const detailLoading = ref(false)
const refreshButton = ref<HTMLButtonElement>()
let requestId = 0
let controller: AbortController | undefined
let detailController: AbortController | undefined
let searchTimer: ReturnType<typeof setTimeout> | undefined

// The server picks the page in this same order; sorting the page locally keeps
// the rendered order identical even if a response arrives unsorted.
const models = computed(() => catalogModels(catalog.value.groups)
  .sort((a, b) => a.name.localeCompare(b.name) || a.platform.localeCompare(b.platform) || a.key.localeCompare(b.key)))
const modelsTotal = computed(() => catalog.value.models_total ?? models.value.length)
const categoryOptions = computed(() => {
  const counts = catalog.value.categories ?? {}
  return (['group', 'composite'] as const)
    .filter(item => (counts[item] ?? 0) > 0 || item === category.value)
    .map(item => ({ value: item, count: counts[item] ?? 0 }))
})
const platformOptions = computed(() => Object.entries(catalog.value.platforms ?? {})
  .map(([name, count]) => ({ platform: name, count }))
  .sort((a, b) => a.platform.localeCompare(b.platform)))
const platformTotal = computed(() => platformOptions.value.reduce((sum, item) => sum + item.count, 0))

const cachedKey = () => {
  if (catalogIdentity.value === null) return null
  return JSON.stringify([catalogIdentity.value, getLocale(), category.value, platform.value, search.value.trim(), page.value])
}

async function loadCatalog(force = false) {
  controller?.abort()
  const id = ++requestId
  const key = cachedKey()
  if (key === null) return
  error.value = ''
  if (force) invalidateAvailableChannelCatalog(key)
  const cached = force ? undefined : readAvailableChannelCatalog(key)
  const currentController = new AbortController()
  controller = currentController
  loading.value = !cached
  let handedOff = false
  try {
    const result = cached ?? await userChannelsAPI.getCatalog({
      page: page.value,
      pageSize: PAGE_SIZE,
      category: category.value,
      platform: platform.value || undefined,
      search: search.value.trim() || undefined,
      signal: currentController.signal,
    })
    if (id !== requestId || cachedKey() !== key || currentController.signal.aborted) return
    catalog.value = result
    if (!cached) cacheAvailableChannelCatalog(key, result)
    // A platform that the new result no longer offers would show an empty list.
    if (platform.value && !(catalog.value.platforms ?? {})[platform.value] && modelsTotal.value > 0) {
      handedOff = true
      platform.value = ''
      page.value = 1
      void loadCatalog()
      return
    }
    if (page.value > 1 && !models.value.length && modelsTotal.value > 0) {
      handedOff = true
      page.value = Math.max(1, Math.ceil(modelsTotal.value / PAGE_SIZE))
      void loadCatalog()
    }
  } catch (err) {
    if (id !== requestId || currentController.signal.aborted) return
    invalidateAvailableChannelCatalog(key)
    catalog.value = emptyCatalog()
    error.value = extractApiErrorMessage(err, t('availableChannels.catalog.loadFailed'))
  } finally {
    if (id === requestId && !handedOff) {
      loading.value = false
      // Keep an open detail dialog in step with the list it came from: re-read
      // it for the new locale/prices, or close it when the model disappeared.
      if (selectedKey.value) {
        const current = models.value.find(model => model.key === selectedKey.value)
        if (current) void loadDetail(current)
        else closeDetails()
      }
    }
  }
}

function selectCategory(value: 'group' | 'composite') {
  if (category.value === value) return
  category.value = value
  platform.value = ''
  page.value = 1
  void loadCatalog()
}

function selectPlatform(value: string) {
  platform.value = value
  page.value = 1
  void loadCatalog()
}

function onSearchInput(value: string) {
  search.value = value
  if (searchTimer) clearTimeout(searchTimer)
  // Typing re-queries the server; wait for a pause so each keystroke is not a request.
  searchTimer = setTimeout(() => {
    page.value = 1
    void loadCatalog()
  }, SEARCH_DEBOUNCE_MS)
}

function onPageChange(value: number) {
  page.value = value
  void loadCatalog()
}

function reset() {
  if (searchTimer) clearTimeout(searchTimer)
  platform.value = ''
  search.value = ''
  page.value = 1
  void loadCatalog()
}

function closeDetails() {
  detailController?.abort()
  detailLoading.value = false
  selectedKey.value = null
  selectedModel.value = null
}

function openDetails(model: CatalogModel) {
  selectedKey.value = model.key
  selectedModel.value = null
  void loadDetail(model)
}

// Details are a separate query for this one card: the list never carries the
// per-group quote tables.
async function loadDetail(model: CatalogModel) {
  detailController?.abort()
  const currentController = new AbortController()
  detailController = currentController
  detailLoading.value = true
  try {
    const result = await userChannelsAPI.getCatalogDetail(
      { category: model.category ?? 'group', platform: model.platform, name: model.name },
      { signal: currentController.signal },
    )
    if (currentController.signal.aborted || selectedKey.value !== model.key) return
    selectedModel.value = catalogModels(result.groups).find(item => item.key === model.key) ?? null
    if (!selectedModel.value) app.showError(t('availableChannels.catalog.loadFailed'))
  } catch (err) {
    if (currentController.signal.aborted) return
    app.showError(extractApiErrorMessage(err, t('availableChannels.catalog.loadFailed')))
  } finally {
    if (!currentController.signal.aborted) detailLoading.value = false
  }
}

let previousIdentity: string | null | undefined
watch(catalogKey, () => {
  ++requestId
  controller?.abort()
  if (catalogIdentity.value !== previousIdentity) {
    catalog.value = emptyCatalog()
    closeDetails()
    category.value = 'group'
    platform.value = ''
    search.value = ''
    page.value = 1
  }
  previousIdentity = catalogIdentity.value
  loading.value = false
  error.value = ''
  void loadCatalog()
}, { immediate: true, flush: 'sync' })

onUnmounted(() => {
  ++requestId
  controller?.abort()
  detailController?.abort()
  if (searchTimer) clearTimeout(searchTimer)
})
</script>

<style scoped>
.catalog-page { container-type: inline-size; }
.catalog-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 20px; }
.catalog-platforms { align-items: flex-start; flex-wrap: nowrap; }
.catalog-platforms > span { flex: none; padding-top: 10px; }
.catalog-platforms .segmented-control { flex: 1 1 0%; justify-content: flex-start; gap: 5px; }
.catalog-platforms .segmented-option { flex: none; min-height: 30px; padding: 5px 12px; white-space: nowrap; }
.catalog-grid :deep(.catalog-model-card) { min-height: 300px; padding: 22px; border-radius: var(--surface-radius); }
.catalog-grid :deep(.catalog-model-card h3) { font-size: 16px; }
.catalog-grid :deep(.catalog-model-card .font-mono.text-base) { font-size: 20px; }
@container (min-width: 460px) { .catalog-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@container (min-width: 700px) { .catalog-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
</style>
