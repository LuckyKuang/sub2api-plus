<template>
  <!-- 后台内嵌形态:?embedded=1 且已登录,套完整后台布局 -->
  <AppLayout v-if="isEmbedded" appearance="user">
    <ModelPlazaContent :response="data" :loading="loading" :error="loadFailed" embedded />
  </AppLayout>

  <!-- 独立形态:自带导航条(logo/站名 + 登录/回后台) -->
  <div v-else class="flex min-h-screen flex-col gap-[var(--shell-gap)] bg-canvas p-[var(--shell-gutter)] dark:surface-inset">
    <PlazaNavBar />
    <main class="mx-auto w-full min-w-0 max-w-7xl">
      <ModelPlazaContent :response="data" :loading="loading" :error="loadFailed" />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useLocaleRefresh } from '@/utils/localeRefresh'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import PlazaNavBar from '@/components/modelPlaza/PlazaNavBar.vue'
import ModelPlazaContent from '@/components/modelPlaza/ModelPlazaContent.vue'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

// embedded=1 但未登录(如转发的链接)自动降级为独立形态。
const isEmbedded = computed(() => route.query.embedded === '1' && authStore.isAuthenticated)

const data = ref<ModelPlazaResponse | null>(null)
const loading = ref(true)
const loadFailed = ref(false)

let requestVersion = 0
async function loadPlaza() {
  const version = ++requestVersion
  loading.value = true
  loadFailed.value = false
  try {
    const response = await getModelPlaza()
    if (version === requestVersion) data.value = response
  } catch {
    if (version === requestVersion) loadFailed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}
useLocaleRefresh(loadPlaza)
onMounted(() => {
  void appStore.fetchPublicSettings()
  void loadPlaza()
})
</script>
