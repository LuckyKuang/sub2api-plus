<template>
  <div class="app-layout" :class="{ 'sidebar-is-collapsed': sidebarCollapsed, 'app-layout-workspace': workspace, 'app-layout-user': appearance === 'user', 'app-layout-admin': appearance === 'admin' }">
    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div class="app-content" :inert="appStore.mobileOpen || supportTransitioning || undefined" :aria-busy="supportTransitioning || undefined">
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

    <!-- Covers teleported dialogs until the selected account is resolved. -->
    <div
      v-if="supportTransitioning"
      class="support-transition-blocker fixed inset-0 z-[120] cursor-wait"
      aria-hidden="true"
    ></div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onBeforeUnmount, onMounted, provide, watch } from 'vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AdminSupportBanner from '@/components/admin/support/AdminSupportBanner.vue'
import { useUserView } from '@/composables/useUserView'
import { usePortalArea } from '@/composables/usePortalArea'
import { adminSupportContext, supportTransitioning } from '@/utils/adminSupportContext'
import { useProductAppearance } from '@/composables/useProductAppearance'
import { USER_TABLE_LAYOUT_KEY } from '@/composables/useUserTableLayout'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const props = withDefaults(defineProps<{ workspace?: boolean; appearance?: 'user' | 'admin' }>(), {
  workspace: false,
  appearance: 'admin'
})
useProductAppearance()
provide(USER_TABLE_LAYOUT_KEY, computed(() => props.appearance === 'user'))

const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const view = useUserView()
const supportUser = computed(() => adminSupportContext.value ? view.user : null)
const portalArea = usePortalArea()
const adminGuide = computed(() => portalArea.value === 'admin' && authStore.isAdmin)

const { replayTour } = useOnboardingTour({
  storageKey: adminGuide.value ? 'admin_guide' : 'user_guide',
  variant: adminGuide.value ? 'admin' : 'user',
  autoStart: !adminSupportContext.value
})

const onboardingStore = useOnboardingStore()

// Inert only covers this layout; dialogs teleported to body and keyboard
// submission stay reachable. Block them globally for the short switch window.
let releaseTransitionGuard: (() => void) | null = null
watch(supportTransitioning, (active) => {
  if (typeof document === 'undefined') return
  if (!active) {
    releaseTransitionGuard?.()
    releaseTransitionGuard = null
    return
  }
  if (releaseTransitionGuard) return
  const blockEvent = (event: Event) => {
    event.preventDefault()
    event.stopImmediatePropagation()
  }
  const blockKeyboard = (event: KeyboardEvent) => {
    if (event.key === 'Enter' || event.key === ' ') blockEvent(event)
  }
  document.addEventListener('submit', blockEvent, true)
  document.addEventListener('click', blockEvent, true)
  document.addEventListener('keydown', blockKeyboard, true)
  releaseTransitionGuard = () => {
    document.removeEventListener('submit', blockEvent, true)
    document.removeEventListener('click', blockEvent, true)
    document.removeEventListener('keydown', blockKeyboard, true)
  }
}, { immediate: true })

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

onBeforeUnmount(() => {
  releaseTransitionGuard?.()
  releaseTransitionGuard = null
})

defineExpose({ replayTour })
</script>
