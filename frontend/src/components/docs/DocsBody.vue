<template>
  <div class="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
    <div class="mb-4 lg:hidden">
      <Select
        v-model="mobileSection"
        :options="mobileOptions"
        :searchable="false"
        :aria-label="t('docs.mobileNav')"
        @update:model-value="onMobileSectionChange"
      />
    </div>

    <div class="grid gap-8 lg:grid-cols-[220px_minmax(0,1fr)] xl:grid-cols-[220px_minmax(0,1fr)_200px]">
      <aside class="hidden lg:block">
        <nav class="sticky top-24" :aria-label="t('docs.sidebarLabel')">
          <div v-for="group in groups" :key="group.id" class="mb-6">
            <p class="mb-2 px-3 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">
              {{ group.title[locale] }}
            </p>
            <RouterLink
              v-for="section in sectionsByGroup(group.id)"
              :key="section.id"
              :to="docsSectionHref(section.id)"
              class="mb-1 block rounded-lg px-3 py-2 text-sm transition-colors"
              :class="section.id === activeId
                ? 'bg-primary-50 font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
                : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-800 dark:hover:text-white'"
            >
              {{ section.title[locale] }}
            </RouterLink>
          </div>
        </nav>
      </aside>

      <main class="min-w-0">
        <div
          v-if="anonymous"
          class="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200"
          role="status"
        >
          <div>
            <p class="font-medium">{{ t('docs.anonymousTitle') }}</p>
            <p class="mt-0.5 text-amber-800/80 dark:text-amber-200/80">{{ t('docs.anonymousDesc') }}</p>
          </div>
          <RouterLink
            :to="{ path: '/login', query: { redirect: `/docs/${activeId}` } }"
            class="shrink-0 rounded-lg bg-amber-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-amber-700"
          >
            {{ t('docs.login') }}
          </RouterLink>
        </div>

        <div class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm sm:p-8 dark:border-dark-700 dark:bg-dark-900">
          <header class="mb-6 border-b border-gray-100 pb-5 dark:border-dark-700">
            <h1 class="text-2xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-3xl">{{ title }}</h1>
            <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ description }}</p>
          </header>

          <div
            v-if="showUseKeyHint && !notFound"
            class="mb-5 flex flex-col gap-3 rounded-xl border border-primary-200 bg-primary-50/80 px-4 py-4 sm:flex-row sm:items-center sm:justify-between dark:border-primary-900/50 dark:bg-primary-950/30"
          >
            <div class="min-w-0">
              <p class="text-sm font-semibold text-primary-900 dark:text-primary-100">{{ t('docs.useKeyTitle') }}</p>
              <p class="mt-0.5 text-xs leading-5 text-primary-800/80 dark:text-primary-200/80">{{ t('docs.useKeyDesc') }}</p>
            </div>
            <RouterLink
              to="/keys"
              class="inline-flex shrink-0 items-center justify-center rounded-lg bg-primary-600 px-3.5 py-2 text-xs font-semibold text-white shadow-sm transition-colors hover:bg-primary-700"
            >
              {{ t('docs.useKeyCta') }}
              <span class="ml-1" aria-hidden="true">→</span>
            </RouterLink>
          </div>

          <div
            v-if="hasOsContent && !notFound"
            class="mb-5 rounded-xl border-2 border-primary-300 bg-white p-4 shadow-sm dark:border-primary-800 dark:bg-dark-900"
          >
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div class="min-w-0">
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('docs.os.title') }}</p>
                <p class="mt-0.5 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('docs.os.hint') }}</p>
              </div>
              <div
                class="grid shrink-0 grid-cols-1 gap-1 rounded-lg bg-gray-100 p-1 sm:grid-cols-3 dark:bg-dark-800"
                role="group"
                :aria-label="t('docs.os.label')"
              >
                <button
                  v-for="option in osOptions"
                  :key="option.id"
                  type="button"
                  class="whitespace-nowrap rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                  :class="selectedOS === option.id
                    ? 'bg-primary-600 text-white shadow-sm'
                    : 'text-gray-600 hover:bg-white hover:text-gray-900 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white'"
                  :aria-pressed="selectedOS === option.id"
                  @click="selectOS(option.id)"
                >
                  {{ t(option.labelKey) }}
                </button>
              </div>
            </div>
          </div>

          <div v-if="notFound" class="py-12 text-center">
            <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('docs.notFoundTitle') }}</p>
            <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('docs.notFoundDesc') }}</p>
            <RouterLink to="/docs" class="btn btn-secondary mt-4">{{ t('docs.backToDocs') }}</RouterLink>
          </div>

          <div v-else-if="loading" class="flex items-center justify-center py-16" role="status">
            <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
            <span class="sr-only">{{ t('docs.loading') }}</span>
          </div>

          <div v-else-if="error" class="py-12 text-center">
            <p class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
            <button type="button" class="btn btn-secondary mt-4" @click="emit('retry')">
              {{ t('docs.retry') }}
            </button>
          </div>

          <Transition v-else name="docs-os-swap" mode="out-in">
            <DocMarkdown
              :key="selectedOS"
              :source="markdown"
              :variables="variables"
              :selected-os="selectedOS"
              @toc="onToc"
            />
          </Transition>

          <nav v-if="!loading && !error && !notFound" class="mt-8 flex flex-col gap-3 border-t border-gray-100 pt-5 sm:flex-row sm:justify-between dark:border-dark-700">
            <RouterLink
              v-if="prev"
              :to="prev ? docsSectionHref(prev.id) : ''"
              class="group flex items-center gap-2 text-sm text-gray-600 hover:text-primary-600 dark:text-dark-300 dark:hover:text-primary-400"
            >
              <Icon name="arrowLeft" size="sm" />
              <span>
                <span class="block text-xs text-gray-400 dark:text-dark-500">{{ t('docs.prev') }}</span>
                {{ prev?.title[locale] }}
              </span>
            </RouterLink>
            <span v-else />
            <RouterLink
              v-if="next"
              :to="next ? docsSectionHref(next.id) : ''"
              class="group flex items-center justify-end gap-2 text-right text-sm text-gray-600 hover:text-primary-600 dark:text-dark-300 dark:hover:text-primary-400"
            >
              <span>
                <span class="block text-xs text-gray-400 dark:text-dark-500">{{ t('docs.next') }}</span>
                {{ next?.title[locale] }}
              </span>
              <Icon name="arrowRight" size="sm" />
            </RouterLink>
          </nav>

          <footer v-if="contactInfo || externalDocUrl" class="mt-8 border-t border-gray-100 pt-5 text-sm dark:border-dark-700">
            <div v-if="contactInfo">
              <p class="font-medium text-gray-900 dark:text-white">{{ t('docs.contactTitle') }}</p>
              <p class="mt-1 text-gray-500 dark:text-dark-400">{{ t('docs.contactHint') }}</p>
              <div class="mt-2 whitespace-pre-line break-words text-gray-700 dark:text-dark-300" v-html="contactHtml"></div>
            </div>
            <a
              v-if="externalDocUrl"
              :href="externalDocUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="mt-3 inline-flex items-center gap-1.5 text-primary-600 hover:underline dark:text-primary-400"
            >
              <Icon name="externalLink" size="sm" />
              {{ t('docs.moreDocs') }}
            </a>
          </footer>
        </div>
      </main>

      <aside v-if="toc.length" class="hidden xl:block">
        <nav class="sticky top-24" :aria-label="t('docs.toc')">
          <p class="mb-2 px-3 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">
            {{ t('docs.toc') }}
          </p>
          <button
            v-for="item in toc"
            :key="item.id"
            type="button"
            class="block w-full truncate rounded px-3 py-1.5 text-left text-xs transition-colors"
            :class="[
              item.level > 2 ? 'pl-5' : '',
              item.id === activeHeading
                ? 'font-medium text-primary-700 dark:text-primary-300'
                : 'text-gray-500 hover:text-gray-900 dark:text-dark-400 dark:hover:text-white',
            ]"
            @click="scrollToHeading(item.id)"
          >
            {{ item.text }}
          </button>
        </nav>
      </aside>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import {
  docsGroups,
  docsSectionHref,
  docsSections,
  type DocsGroupId,
  type DocsLocale,
  type DocsSection,
} from '@/docs/manifest'
import { hasOsCodeBlocks, renderContactInfo, type DocsOS, type DocsTocItem } from '@/docs/render'
import DocMarkdown from './DocMarkdown.vue'

const props = defineProps<{
  sections: DocsSection[]
  activeId: string
  title: string
  description: string
  markdown: string
  variables: Record<string, string>
  contactInfo: string
  externalDocUrl: string
  prev?: DocsSection
  next?: DocsSection
  loading: boolean
  error: string
  anonymous: boolean
  notFound?: boolean
  showUseKeyHint?: boolean
  locale: DocsLocale
}>()

const emit = defineEmits<{
  retry: []
}>()

const { t } = useI18n()
const router = useRouter()
const groups = docsGroups
const OS_STORAGE_KEY = 'sub2api.docs.os'
const osOptions: { id: DocsOS; labelKey: string }[] = [
  { id: 'unix', labelKey: 'docs.os.unix' },
  { id: 'cmd', labelKey: 'docs.os.cmd' },
  { id: 'powershell', labelKey: 'docs.os.powershell' },
]

function detectDocsOS(): DocsOS {
  try {
    const stored = window.localStorage.getItem(OS_STORAGE_KEY)
    if (stored === 'unix' || stored === 'cmd' || stored === 'powershell') return stored
  } catch {
    // localStorage may be unavailable in hardened browsers.
  }
  return typeof navigator !== 'undefined' && /Windows/i.test(navigator.userAgent)
    ? 'powershell'
    : 'unix'
}

const selectedOS = ref<DocsOS>(detectDocsOS())
const hasOsContent = computed(() => hasOsCodeBlocks(props.markdown))

function selectOS(os: DocsOS) {
  selectedOS.value = os
  try {
    window.localStorage.setItem(OS_STORAGE_KEY, os)
  } catch {
    // The selection still applies for the current page.
  }
}
const toc = ref<DocsTocItem[]>([])
const activeHeading = ref('')
const mobileSection = ref(props.activeId)

const mobileOptions = computed(() =>
  docsSections.map(section => ({
    value: section.id,
    label: `${groupTitle(section.group)} · ${section.title[props.locale]}`,
  })),
)
const contactHtml = computed(() => renderContactInfo(props.contactInfo))

watch(
  () => props.activeId,
  (id) => {
    mobileSection.value = id
    toc.value = []
    activeHeading.value = ''
  },
)

function sectionsByGroup(group: DocsGroupId): DocsSection[] {
  return props.sections.filter(section => section.group === group)
}

function groupTitle(group: DocsGroupId): string {
  return groups.find(item => item.id === group)?.title[props.locale] ?? group
}

function onMobileSectionChange(value: unknown) {
  const id = String(value ?? '')
  if (id && id !== props.activeId) {
    void router.push(docsSectionHref(id))
  }
}

function onToc(items: DocsTocItem[]) {
  toc.value = items
  void nextTick(updateActiveHeading)
}

function scrollToHeading(id: string) {
  const element = document.getElementById(id)
  if (!element) return
  element.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeHeading.value = id
}

let scrollRaf = 0
function updateActiveHeading() {
  if (scrollRaf) return
  scrollRaf = requestAnimationFrame(() => {
    scrollRaf = 0
    if (!toc.value.length) return
    let current = toc.value[0]?.id ?? ''
    for (const item of toc.value) {
      const element = document.getElementById(item.id)
      if (!element) continue
      if (element.getBoundingClientRect().top <= 140) {
        current = item.id
      }
    }
    activeHeading.value = current
  })
}

onMounted(() => window.addEventListener('scroll', updateActiveHeading, { passive: true }))
onUnmounted(() => {
  window.removeEventListener('scroll', updateActiveHeading)
  if (scrollRaf) cancelAnimationFrame(scrollRaf)
})
</script>

<style scoped>
.docs-os-swap-enter-active,
.docs-os-swap-leave-active {
  transition: opacity 160ms ease, transform 160ms ease;
}

.docs-os-swap-enter-from {
  opacity: 0;
  transform: translateY(4px);
}

.docs-os-swap-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .docs-os-swap-enter-active,
  .docs-os-swap-leave-active {
    transition: none;
  }
}
</style>
