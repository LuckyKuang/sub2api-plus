<template>
  <AppLayout appearance="user" workspace>
    <div class="catalog-page workspace-page min-w-0">
      <h1 class="sr-only">{{ t('availableChannels.title') }}</h1>

      <div class="catalog-toolbar-card workspace-fixed" data-testid="catalog-filters">
        <div
          v-if="categoryOptions.length > 1"
          class="segmented-control self-start"
          role="tablist"
          :aria-label="t('availableChannels.catalog.category')"
        >
          <button
            v-for="item in categoryOptions"
            :key="item.value"
            type="button"
            class="segmented-option"
            role="tab"
            :aria-selected="category === item.value"
            @click="selectCategory(item.value)"
          >
            {{ t(`availableChannels.catalog.${item.value}Platforms`) }}
          </button>
        </div>

        <div class="catalog-toolbar-row">
          <label class="catalog-search-field">
            <span class="sr-only">{{ t('availableChannels.catalog.searchPlaceholder') }}</span>
            <Icon name="search" size="sm" class="catalog-search-icon" aria-hidden="true" />
            <input
              :value="search"
              class="input catalog-search-input w-full"
              type="search"
              :placeholder="t('availableChannels.catalog.searchPlaceholder')"
              @input="onSearchInput(($event.target as HTMLInputElement).value)"
            />
          </label>

          <button
            type="button"
            class="btn btn-secondary catalog-reset-btn"
            :class="{ 'is-idle': !hasActiveFilters }"
            @click="reset"
          >
            {{ t('availableChannels.catalog.reset') }}
          </button>

          <div class="catalog-toolbar-right">
            <div class="catalog-meta-pill" aria-live="polite" :aria-busy="detailLoading">
              <template v-if="!loading && !error">
                <strong class="catalog-meta-count">{{ t('availableChannels.catalog.modelCount', { count: modelsTotal }) }}</strong>
                <span class="catalog-meta-dot" aria-hidden="true" />
              </template>
              <span>{{ t(catalog.user_rate_status === 'unavailable' ? 'availableChannels.catalog.referenceRange' : 'availableChannels.catalog.effectiveRange') }}</span>
              <template v-if="detailLoading">
                <span class="catalog-meta-dot" aria-hidden="true" />
                <span>{{ t('availableChannels.catalog.detailsLoading') }}</span>
              </template>
            </div>

            <div class="segmented-control" :aria-label="t('availableChannels.catalog.viewSwitchLabel')">
              <button
                type="button"
                class="segmented-option"
                :aria-pressed="activeLayout === 'list'"
                @click="setLayout('list')"
              >
                <svg class="h-3.5 w-3.5 shrink-0" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true">
                  <path d="M2.5 4h11M2.5 8h11M2.5 12h11" />
                </svg>
                <span>{{ t('availableChannels.catalog.viewList') }}</span>
              </button>
              <button
                type="button"
                class="segmented-option"
                :aria-pressed="activeLayout === 'card'"
                @click="setLayout('card')"
              >
                <svg class="h-3.5 w-3.5 shrink-0" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" aria-hidden="true">
                  <rect x="2.25" y="2.25" width="4.75" height="4.75" rx="1" />
                  <rect x="9" y="2.25" width="4.75" height="4.75" rx="1" />
                  <rect x="2.25" y="9" width="4.75" height="4.75" rx="1" />
                  <rect x="9" y="9" width="4.75" height="4.75" rx="1" />
                </svg>
                <span>{{ t('availableChannels.catalog.viewCard') }}</span>
              </button>
            </div>

            <button
              ref="refreshButton"
              type="button"
              class="catalog-refresh-btn"
              :disabled="loading"
              :aria-label="t('common.refresh')"
              :title="t('common.refresh')"
              @click="loadCatalog(true)"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            </button>
          </div>
        </div>

        <div class="catalog-platforms">
          <span class="catalog-platforms-label">{{ t('modelPlaza.filters.platformLabel') }}</span>
          <div class="catalog-platform-chips" role="group" :aria-label="t('modelPlaza.filters.platformLabel')">
            <button
              type="button"
              class="catalog-platform-chip"
              :aria-pressed="!platform"
              @click="selectPlatform('')"
            >
              <span>{{ t('modelPlaza.filters.all') }}</span>
              <span class="catalog-chip-count">{{ platformTotal }}</span>
            </button>
            <button
              v-for="item in platformOptions"
              :key="item.platform"
              type="button"
              class="catalog-platform-chip"
              :aria-pressed="platform === item.platform"
              @click="selectPlatform(item.platform)"
            >
              <PlatformIcon :platform="item.platform" size="sm" />
              <span>{{ item.platform }}</span>
              <span class="catalog-chip-count">{{ item.count }}</span>
            </button>
          </div>
        </div>
      </div>

      <div
        v-if="catalog.user_rate_status === 'unavailable'"
        role="status"
        class="workspace-fixed flex flex-wrap items-center gap-3 rounded-lg bg-amber-50/[0.15] p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
      >
        {{ t('availableChannels.catalog.ratesUnavailable') }}
        <button type="button" class="btn btn-ghost btn-xs underline" @click="loadCatalog(true)">{{ t('availableChannels.catalog.retry') }}</button>
      </div>

      <section class="workspace-shell workspace-scroll" :aria-label="t('availableChannels.title')" data-testid="catalog-results">
        <div
          v-if="loading"
          :class="activeLayout === 'list' ? 'catalog-skeleton-list' : 'catalog-grid'"
          aria-busy="true"
          :aria-label="t('common.loading')"
        >
          <div
            v-for="i in PAGE_SIZE"
            :key="i"
            class="animate-pulse"
            :class="activeLayout === 'list' ? 'catalog-skeleton-row' : 'catalog-skeleton-card'"
          />
        </div>
        <div v-else-if="error" role="alert" class="card py-12 text-center">
          <p class="text-red-600 dark:text-red-400">{{ error }}</p>
          <button type="button" class="btn btn-secondary mt-4" @click="loadCatalog(true)">{{ t('availableChannels.catalog.retry') }}</button>
        </div>
        <div v-else-if="!models.length" class="card py-12 text-center surface-muted">
          <p>{{ modelsTotal ? t('availableChannels.catalog.noMatches') : t('availableChannels.noModels') }}</p>
          <button v-if="modelsTotal || search || platform" type="button" class="btn btn-secondary mt-4" @click="reset">{{ t('availableChannels.catalog.reset') }}</button>
        </div>
        <CatalogModelList
          v-else-if="activeLayout === 'list'"
          :models="models"
          :rate-unavailable="catalog.user_rate_status === 'unavailable'"
          @details="openDetails"
        />
        <div v-else class="catalog-grid">
          <CatalogModelCard
            v-for="model in models"
            :key="model.key"
            :model="model"
            :rate-unavailable="catalog.user_rate_status === 'unavailable'"
            @details="openDetails"
          />
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

      <CatalogPriceDetails
        :model="selectedModel"
        :rate-unavailable="catalog.user_rate_status === 'unavailable'"
        @close="closeDetails"
      />
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
import { useAvailableChannelsLayoutSession } from '@/composables/useAvailableChannelsLayout'
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
const layoutSession = useAvailableChannelsLayoutSession()
const activeLayout = computed<'list' | 'card'>(() => {
  const entry = layoutSession.sessionLayout.value
  if (entry && entry.adminDefault === app.availableChannelsLayout) return entry.mode
  return app.availableChannelsLayout
})
const hasActiveFilters = computed(() => Boolean(search.value.trim() || platform.value))

watch(() => app.availableChannelsLayout, () => {
  layoutSession.reset()
})

function setLayout(mode: 'list' | 'card') {
  layoutSession.choose(mode, app.availableChannelsLayout)
}

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
.catalog-page {
  container-type: inline-size;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.catalog-toolbar-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--surface-radius);
  background-color: var(--surface-fill);
  background-image: linear-gradient(135deg, var(--surface-glint), transparent 65%);
  border: 1px solid var(--surface-edge);
  box-shadow: var(--surface-shadow);
}

.catalog-toolbar-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.catalog-search-field {
  position: relative;
  width: min(300px, 100%);
  flex: 1 1 200px;
  max-width: 320px;
  min-width: 0;
}

.catalog-search-icon {
  position: absolute;
  left: 10px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--surface-muted);
  pointer-events: none;
}

.catalog-search-input {
  height: var(--control-height);
  padding-left: 32px;
  padding-right: 10px;
  font-size: 13px;
  border-radius: var(--surface-radius-control);
}

.catalog-reset-btn {
  height: var(--control-height);
  padding-inline: 12px;
  font-size: 12.5px;
  border-radius: var(--surface-radius-control);
  white-space: nowrap;
  transition: opacity 150ms ease, background-color 150ms ease, border-color 150ms ease, color 150ms ease;
}

.catalog-reset-btn.is-idle {
  opacity: 0.62;
}

.catalog-reset-btn.is-idle:hover,
.catalog-reset-btn.is-idle:focus-visible {
  opacity: 1;
}

.catalog-toolbar-right {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
}

.catalog-meta-pill {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: var(--control-height);
  padding: 0 12px;
  border-radius: var(--surface-radius-control);
  background: var(--record-card-fill);
  border: 1px solid color-mix(in srgb, var(--surface-edge) 70%, transparent);
  font-size: 12px;
  color: var(--surface-muted);
  white-space: nowrap;
}

.catalog-meta-count {
  font-weight: 600;
  color: var(--surface-text);
  font-variant-numeric: tabular-nums;
}

.catalog-meta-dot {
  width: 3px;
  height: 3px;
  border-radius: 9999px;
  background: currentColor;
  opacity: 0.45;
  flex: none;
}

.catalog-view-switch {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: var(--control-height);
  padding: 3px;
  border-radius: var(--surface-radius-control);
  background: var(--record-card-fill);
  border: 1px solid var(--surface-edge);
}

.catalog-view-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 9px;
  border-radius: var(--surface-radius-tag);
  border: 1px solid transparent;
  background: transparent;
  color: var(--surface-muted);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease, color 150ms ease, box-shadow 150ms ease;
}

.catalog-view-btn:hover {
  color: var(--surface-text);
}

.catalog-view-btn[aria-checked='true'] {
  background: var(--surface-field);
  border-color: var(--surface-edge);
  color: var(--surface-text);
  font-weight: 600;
  box-shadow: 0 1px 2px rgb(0 0 0 / 10%);
}

.catalog-view-btn:focus-visible,
.catalog-refresh-btn:focus-visible,
.catalog-platform-chip:focus-visible {
  outline: 2px solid rgb(var(--console-accent-500, 20 184 166) / 75%);
  outline-offset: 2px;
}

.catalog-refresh-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--control-height);
  height: var(--control-height);
  flex: none;
  border-radius: var(--surface-radius-control);
  background: var(--surface-field);
  border: 1px solid var(--surface-edge);
  color: var(--surface-text);
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease, color 150ms ease;
}

.catalog-refresh-btn:hover:not(:disabled) {
  background: var(--surface-hover);
  border-color: var(--console-control-hover-edge);
}

.catalog-refresh-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.catalog-platforms {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid color-mix(in srgb, var(--surface-edge) 55%, transparent);
}

.catalog-platforms-label {
  flex: none;
  padding-top: 4px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--surface-muted);
  letter-spacing: 0.02em;
  white-space: nowrap;
}

.catalog-platform-chips {
  display: flex;
  flex: 1 1 0%;
  flex-wrap: wrap;
  gap: 5px;
  min-width: 0;
}

.catalog-platform-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 26px;
  padding: 3px 8px;
  border-radius: var(--surface-radius-control);
  background: var(--record-card-fill);
  border: 1px solid color-mix(in srgb, var(--surface-edge) 75%, transparent);
  color: var(--surface-muted);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition: background-color 150ms ease, border-color 150ms ease, color 150ms ease;
}

.catalog-platform-chip:hover {
  background: var(--surface-hover);
  border-color: var(--console-control-hover-edge);
  color: var(--surface-text);
}

.catalog-platform-chip[aria-pressed='true'] {
  background: rgb(var(--console-accent-500, 20 184 166) / 16%);
  border-color: rgb(var(--console-accent-500, 20 184 166) / 55%);
  color: var(--console-accent-readable, var(--surface-text));
  font-weight: 600;
}

.catalog-chip-count {
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  opacity: 0.72;
  padding: 0 4px;
  border-radius: var(--surface-radius-xs);
  background: color-mix(in srgb, currentColor 10%, transparent);
}

.catalog-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
}

.catalog-skeleton-list {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 10px 12px 12px;
  border-radius: var(--surface-radius);
  background-color: var(--surface-fill);
  border: 1px solid var(--surface-edge);
}

.catalog-skeleton-row {
  height: 48px;
  border-radius: var(--surface-radius-item);
  background: var(--record-card-fill);
}

.catalog-skeleton-card {
  height: 168px;
  border-radius: var(--surface-radius-card);
  background: var(--surface-fill);
  border: 1px solid var(--surface-edge);
}

@container (min-width: 560px) {
  .catalog-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@container (min-width: 860px) {
  .catalog-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@container (min-width: 1160px) {
  .catalog-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@container (max-width: 640px) {
  .catalog-toolbar-card {
    padding: 10px;
  }

  .catalog-search-field {
    flex: 1 1 100%;
    max-width: none;
  }

  .catalog-toolbar-right {
    margin-left: 0;
    width: 100%;
    justify-content: space-between;
  }

  .catalog-meta-pill {
    height: auto;
    min-height: var(--control-height);
    padding: 6px 10px;
    white-space: normal;
    flex-wrap: wrap;
    max-width: 100%;
  }

  .catalog-platforms {
    flex-direction: column;
    gap: 6px;
  }

  .catalog-platforms-label {
    padding-top: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .catalog-reset-btn,
  .catalog-view-btn,
  .catalog-refresh-btn,
  .catalog-platform-chip {
    transition: none;
  }
}
</style>
