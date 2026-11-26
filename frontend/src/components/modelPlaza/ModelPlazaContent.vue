<template>
  <div class="flex min-w-0 flex-col gap-[var(--shell-gap)]">
    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <div v-if="!embedded" class="card px-5 py-5 sm:px-6">
      <h1 class="text-2xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('modelPlaza.title') }}</h1>
      <p class="mt-1.5 text-sm surface-muted">{{ t('modelPlaza.description') }}</p>
    </div>

    <!-- 未登录提示 -->
    <p
      v-if="!isAuthenticated"
      class="card flex items-center gap-1.5 px-5 py-3 text-xs surface-muted"
      :class="{ 'workspace-shell': embedded }"
    >
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <!-- 加载/错误/空 -->
    <div v-if="loading" class="card flex min-h-[240px] items-center justify-center" :class="{ 'workspace-shell': embedded }">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error"
      class="card border-red-200 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:text-red-300"
      :class="{ 'workspace-shell': embedded }"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <PlazaFilterBar
        :class="{ 'workspace-shell': embedded }"
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        :view="view"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
        @update:view="view = $event"
      />

      <details v-if="descriptionHtml" class="plaza-notes">
        <summary>{{ t('modelPlaza.catalog.pricingNotes') }}</summary>
        <div class="plaza-description mt-2 text-sm" v-html="descriptionHtml"></div>
      </details>

      <section v-if="cards.length" :aria-label="t('modelPlaza.title')" class="space-y-3">
        <h2 class="flex flex-wrap items-baseline gap-x-2" aria-live="polite" aria-atomic="true">
          <span class="text-base font-semibold">{{ t('modelPlaza.catalog.models', cards.length) }}</span>
          <span class="text-sm font-normal surface-muted">{{ t('modelPlaza.catalog.groups', filteredGroups.length) }}</span>
        </h2>
        <div v-if="view === 'cards'" class="plaza-grid">
          <PlazaModelCard
            v-for="card in cards"
            :key="card.key"
            :group="card.group"
            :model="card.model"
            :group-count="card.groupCount"
            @open="openModel(card.group, card.model)"
          />
        </div>
        <div v-else class="flex flex-col gap-[var(--shell-gap)]">
          <PlazaGroupSection
            v-for="group in filteredGroups"
            :key="group.id"
            :group="group"
            @open="openModel(group, $event)"
          />
        </div>
      </section>
      <div
        v-else
        class="card flex flex-col items-center gap-3 px-5 py-10 text-center text-sm surface-muted"
        :class="{ 'workspace-shell': embedded }"
      >
        <Icon name="search" size="lg" aria-hidden="true" />
        <p role="status">{{ filtered ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}</p>
        <button v-if="filtered" type="button" class="btn btn-secondary" @click="clearFilters">
          {{ t('modelPlaza.catalog.reset') }}
        </button>
      </div>
    </template>
    <PlazaModelDetail
      v-if="activeOffer && !error"
      :show="true"
      :group="activeOffer.group"
      :model="activeOffer.model"
      :offers="selectedOffers"
      @close="selection = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import PlazaModelCard from './PlazaModelCard.vue'
import PlazaModelDetail from './PlazaModelDetail.vue'
import type { PlazaOffer } from './pricing'
import type { ModelPlazaGroup, ModelPlazaResponse, PlazaModel } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')
const view = ref<'cards' | 'table'>('cards')
const selection = ref<{ key: string; groupId: number } | null>(null)

const filtered = computed(() => !!searchQuery.value.trim() || selectedGroupId.value !== 'all'
  || selectedPlatform.value !== 'all' || selectedRate.value !== 'all')

function clearFilters() {
  searchQuery.value = ''
  selectedGroupId.value = 'all'
  selectedPlatform.value = 'all'
  selectedRate.value = 'all'
}

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

const groups = computed(() => [...(props.response?.groups ?? [])].sort(
  (a, b) => effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name) || a.id - b.id
))

const platforms = computed(() =>
  [...new Set(groups.value.map((g) => g.platform).filter(Boolean))].sort()
)

const groupOptions = computed(() =>
  groups.value.map((g) => ({
    id: g.id,
    name: g.name,
    platform: g.platform,
    rate: effectiveRate(g)
  }))
)

/** 全量生效倍率;当前组合下不可用的项由 FilterBar 置灰而非隐藏。 */
const rates = computed(() =>
  [...new Set(groups.value.map(effectiveRate))].sort((a, b) => a - b)
)

const matchingGroups = computed(() => groups.value.filter(g =>
  (selectedPlatform.value === 'all' || g.platform === selectedPlatform.value) &&
  (selectedGroupId.value === 'all' || g.id === selectedGroupId.value) &&
  (selectedRate.value === 'all' || effectiveRate(g) === selectedRate.value)
))

const query = computed(() => searchQuery.value.trim().toLowerCase())
const filteredGroups = computed(() => matchingGroups.value
  .map(group => query.value ? { ...group, models: group.models.filter(m => matchesSearch(m)) } : group)
  .filter(group => group.models.length > 0))

function matchesSearch(model: PlazaModel) {
  return model.name.toLowerCase().includes(query.value)
}

function modelKey(model: PlazaModel) {
  return JSON.stringify([model.platform, model.name])
}

const offersByModel = computed(() => {
  const offers = new Map<string, PlazaOffer[]>()
  for (const group of groups.value) {
    for (const model of group.models) {
      const key = modelKey(model)
      const existing = offers.get(key)
      if (existing) existing.push({ group, model })
      else offers.set(key, [{ group, model }])
    }
  }
  return offers
})

const cards = computed(() => {
  const allowed = new Set(matchingGroups.value.map(group => group.id))
  const result = []
  for (const [key, offers] of offersByModel.value) {
    const first = offers.find(offer => allowed.has(offer.group.id))
    if (first && matchesSearch(first.model)) result.push({ ...first, key, groupCount: offers.length })
  }
  return result
})

function openModel(group: ModelPlazaGroup, model: PlazaModel) {
  selection.value = { key: modelKey(model), groupId: group.id }
}

const selectedOffers = computed(() => selection.value ? offersByModel.value.get(selection.value.key) ?? [] : [])
const activeOffer = computed(() => selectedOffers.value.find(offer => offer.group.id === selection.value?.groupId))

// A refresh can remove a group, change its effective rate or replace its translated label.
watch(groups, (list) => {
  const group = list.find(g => g.id === selectedGroupId.value)
  if (!group) selectedGroupId.value = 'all'
  const available = group ? [group] : list
  if (!available.some(g => g.platform === selectedPlatform.value)) selectedPlatform.value = 'all'
  if (!available.some(g => (selectedPlatform.value === 'all' || g.platform === selectedPlatform.value)
    && effectiveRate(g) === selectedRate.value)) selectedRate.value = 'all'
  if (!activeOffer.value) selection.value = null
})
</script>

<style scoped>
.plaza-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, max(19rem, calc((100% - 24px) / 3))), 1fr));
  gap: 12px;
}

.plaza-notes {
  color: var(--surface-muted);
  font-size: .875rem;
}

.plaza-notes summary {
  width: fit-content;
  cursor: pointer;
}

.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5; color: var(--surface-muted);
}

.plaza-description :deep(code) {
  @apply rounded px-1.5 py-0.5 font-mono text-xs;
  background-color: var(--surface-soft);
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
