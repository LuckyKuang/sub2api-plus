<template>
  <AppLayout v-if="isEmbedded">
    <DocsBody
      :sections="sections"
      :active-id="activeSection.id"
      :title="notFound ? t('docs.notFoundTitle') : activeSection.title[locale]"
      :description="notFound ? t('docs.notFoundDesc') : activeSection.description[locale]"
      :markdown="markdown"
      :variables="variables"
      :contact-info="contactInfo"
      :external-doc-url="externalDocUrl"
      :prev="prevSection"
      :next="nextSection"
      :loading="loading"
      :error="error"
      :anonymous="isAnonymous"
      :not-found="notFound"
      :locale="locale"
      @retry="loadMarkdown"
    />
  </AppLayout>

  <div v-else class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <DocsNavBar />
    <DocsBody
      :sections="sections"
      :active-id="activeSection.id"
      :title="notFound ? t('docs.notFoundTitle') : activeSection.title[locale]"
      :description="notFound ? t('docs.notFoundDesc') : activeSection.description[locale]"
      :markdown="markdown"
      :variables="variables"
      :contact-info="contactInfo"
      :external-doc-url="externalDocUrl"
      :prev="prevSection"
      :next="nextSection"
      :loading="loading"
      :error="error"
      :anonymous="isAnonymous"
      :not-found="notFound"
      :locale="locale"
      @retry="loadMarkdown"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import DocsBody from '@/components/docs/DocsBody.vue'
import DocsNavBar from '@/components/docs/DocsNavBar.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import {
  defaultDocsSection,
  docsSectionIndex,
  docsSections,
  findDocsSection,
  loadDocsMarkdown,
  normalizeDocsLocale,
  type DocsLocale,
} from '@/docs/manifest'
import { sanitizeUrl } from '@/utils/url'

const { t, locale: i18nLocale } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

const sections = docsSections
const locale = computed<DocsLocale>(() => normalizeDocsLocale(i18nLocale.value))
const markdown = ref('')
const loading = ref(true)
const error = ref('')
let requestVersion = 0

const requestedSection = computed(() => {
  const value = route.params.section
  return Array.isArray(value) ? value[0] : value
})
const notFound = computed(() => Boolean(requestedSection.value) && !findDocsSection(requestedSection.value))
const activeSection = computed(() => findDocsSection(requestedSection.value) ?? defaultDocsSection)
const activeIndex = computed(() => docsSectionIndex(activeSection.value.id))
const prevSection = computed(() => docsSections[activeIndex.value - 1])
const nextSection = computed(() => docsSections[activeIndex.value + 1])

const isEmbedded = computed(() => route.query.embedded === '1' && authStore.isAuthenticated)
const isAnonymous = computed(() => !authStore.isAuthenticated)
const isPreview = computed(() => route.query.preview === '1' && authStore.isAdmin)
const docsEnabled = computed(() => appStore.cachedPublicSettings?.docs_enabled !== false)
const canView = computed(() => {
  if (isPreview.value) return true
  if (!docsEnabled.value) return false
  const requireAuth = appStore.cachedPublicSettings?.docs_require_auth !== false
  return !requireAuth || authStore.isAuthenticated
})

const settings = computed(() => appStore.cachedPublicSettings)
const siteName = computed(() => settings.value?.site_name || appStore.siteName || 'Sub2API Plus')
const contactInfo = computed(() => settings.value?.contact_info || appStore.contactInfo || '')
const externalDocUrl = computed(() => sanitizeUrl(settings.value?.doc_url || appStore.docUrl || ''))

function trimTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, '')
}

function ensureV1(value: string): string {
  return /\/v1$/i.test(value) ? value : `${value}/v1`
}

function absoluteAppUrl(path: string): string {
  return new URL(router.resolve(path).href, window.location.origin).toString()
}

function resolveBaseUrl(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return window.location.origin
  try {
    return trimTrailingSlashes(new URL(trimmed, window.location.origin).toString())
  } catch {
    return window.location.origin
  }
}

const baseUrl = computed(() =>
  resolveBaseUrl(settings.value?.api_base_url || appStore.apiBaseUrl || ''),
)
const openaiBaseUrl = computed(() => ensureV1(baseUrl.value))
const host = computed(() => {
  try {
    return new URL(baseUrl.value).hostname
  } catch {
    return window.location.hostname
  }
})
const variables = computed<Record<string, string>>(() => ({
  baseUrl: baseUrl.value,
  openaiBaseUrl: openaiBaseUrl.value,
  host: host.value,
  siteName: siteName.value,
  keysUrl: absoluteAppUrl('/keys'),
  modelPlazaUrl: absoluteAppUrl('/model-plaza'),
}))

function redirectAwayFromDocs() {
  if (!docsEnabled.value) {
    void router.replace(
      authStore.isAuthenticated
        ? authStore.isAdmin
          ? '/admin/dashboard'
          : '/dashboard'
        : '/home',
    )
    return
  }
  if (!authStore.isAuthenticated) {
    void router.replace({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  void router.replace(authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
}

async function ensureSettings() {
  if (appStore.publicSettingsLoaded) return
  try {
    await appStore.fetchPublicSettings()
  } catch {
    // Keep the page usable; the route guard performs the same check.
  }
}

async function loadMarkdown() {
  const version = ++requestVersion
  loading.value = true
  error.value = ''
  if (notFound.value) {
    loading.value = false
    markdown.value = ''
    return
  }
  try {
    const content = await loadDocsMarkdown(locale.value, activeSection.value.id)
    if (version !== requestVersion) return
    markdown.value = content
  } catch (loadError) {
    if (version !== requestVersion) return
    console.error('Failed to load documentation', loadError)
    error.value = t('docs.loadFailed')
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

watch(
  [requestedSection, locale],
  () => {
    void loadMarkdown()
  },
  { immediate: true },
)

watch(
  [() => activeSection.value, locale, siteName],
  () => {
    if (notFound.value) {
      document.title = `${t('docs.notFoundTitle')} - ${siteName.value}`
      return
    }
    document.title = `${activeSection.value.title[locale.value]} - ${siteName.value}`
  },
  { immediate: true },
)

watch(
  [canView, () => appStore.publicSettingsLoaded],
  ([allowed, loaded]) => {
    if (loaded && !allowed) {
      redirectAwayFromDocs()
    }
  },
  { immediate: true },
)

onMounted(() => {
  void ensureSettings()
})
</script>
