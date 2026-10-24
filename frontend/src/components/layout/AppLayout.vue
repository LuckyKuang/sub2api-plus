<template>
  <div ref="layoutRef" class="app-layout" :class="{ 'sidebar-is-collapsed': sidebarCollapsed, 'app-layout-workspace': workspace }">
    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div class="app-content" :inert="appStore.mobileOpen || undefined">
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="app-main">
        <div class="app-page">
          <AdminSupportBanner v-if="supportUser" :target="supportUser" class="support-banner-spacing" />
          <slot />
        </div>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted, ref } from 'vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AdminSupportBanner from '@/components/admin/support/AdminSupportBanner.vue'
import { useUserView } from '@/composables/useUserView'
import { useTransparentTableClipping } from '@/composables/useTransparentTableClipping'
import { adminSupportContext } from '@/utils/adminSupportContext'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

withDefaults(defineProps<{ workspace?: boolean }>(), { workspace: false })

const appStore = useAppStore()
const layoutRef = ref<HTMLElement | null>(null)
useTransparentTableClipping(layoutRef, () => {
  const transparency = (appStore.consoleAppearancePreview ?? appStore.consoleAppearance).card_transparency
  return typeof transparency === 'number' && Number.isFinite(transparency) && transparency > 0
})
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const view = useUserView()
const supportUser = computed(() => adminSupportContext.value ? view.user : null)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: !adminSupportContext.value
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>
