<template>
  <component :is="page" v-if="target && !loading" :key="`${userId}:${resource}:${route.params.id || ''}`" />
  <AppLayout v-else>
    <div class="card flex min-h-64 flex-col items-center justify-center gap-4 p-6">
      <LoadingSpinner v-if="loading" />
      <template v-else>
        <p class="text-red-600 dark:text-red-400">{{ error || t('admin.support.loadFailed') }}</p>
        <button class="btn btn-secondary" @click="load">{{ t('admin.support.retry') }}</button>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { useAdminSupportViewStore } from '@/stores/adminSupportView'
import { parseAdminSupportTargetId, type AdminSupportResource } from '@/utils/adminSupport'

const pages = {
  overview: defineAsyncComponent(() => import('@/views/user/DashboardView.vue')),
  'api-keys': defineAsyncComponent(() => import('@/views/user/KeysView.vue')),
  'async-images': defineAsyncComponent(() => import('@/views/user/AsyncImageView.vue')),
  'batch-images': defineAsyncComponent(() => import('@/views/user/BatchImageGuideView.vue')),
  usage: defineAsyncComponent(() => import('@/views/user/UsageView.vue')),
  channels: defineAsyncComponent(() => import('@/views/user/AvailableChannelsView.vue')),
  'channel-status': defineAsyncComponent(() => import('@/views/user/ChannelStatusView.vue')),
  subscriptions: defineAsyncComponent(() => import('@/views/user/SubscriptionsView.vue')),
  orders: defineAsyncComponent(() => import('@/views/user/UserOrdersView.vue')),
  profile: defineAsyncComponent(() => import('@/views/user/ProfileView.vue')),
  purchase: defineAsyncComponent(() => import('@/views/user/PaymentView.vue')),
  redeem: defineAsyncComponent(() => import('@/views/user/RedeemView.vue')),
  affiliate: defineAsyncComponent(() => import('@/views/user/AffiliateView.vue')),
  custom: defineAsyncComponent(() => import('@/views/user/CustomPageView.vue'))
}
const route = useRoute()
const { t } = useI18n()
const store = useAdminSupportViewStore()
const loading = ref(false)
const error = ref('')
let sequence = 0
const userId = computed(() => parseAdminSupportTargetId(route.params.user_id))
const resource = computed(() => route.meta.adminSupportResource as AdminSupportResource)
const target = computed(() => store.target?.id === userId.value ? store.target : null)
const page = computed(() => pages[resource.value])
async function load() {
  const id = userId.value
  if (!id || target.value) return
  const request = ++sequence
  loading.value = true
  error.value = ''
  try { await store.loadTarget(id) }
  catch (err) { if (request === sequence) error.value = (err as Error).message || t('admin.support.loadFailed') }
  finally { if (request === sequence) loading.value = false }
}
watch(userId, () => { sequence++; loading.value = false; error.value = ''; void load() }, { immediate: true })
</script>
