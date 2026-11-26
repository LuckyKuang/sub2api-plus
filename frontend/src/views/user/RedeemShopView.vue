<template>
  <AppLayout appearance="user">
    <div class="space-y-4">
      <div class="card flex flex-wrap items-center gap-3 p-3">
        <RouterLink to="/purchase" class="btn btn-secondary">{{ t('featureExtensions.backPurchase') }}</RouterLink>
        <RouterLink to="/redeem" class="btn btn-secondary">{{ t('featureExtensions.redeemLink') }}</RouterLink>
        <a v-if="url" :href="url" target="_blank" rel="noopener noreferrer" class="btn btn-primary">{{ t('featureExtensions.openNewTab') }}</a>
      </div>
      <div v-if="error" role="alert" class="card p-8 text-center text-sm text-red-600 dark:text-red-400">{{ t('featureExtensions.purchaseUnavailable') }}</div>
      <div v-else-if="!url" class="card p-8 text-center text-sm surface-muted">{{ t('common.loading') }}</div>
      <iframe v-else :src="url" :title="t('featureExtensions.purchaseTitle')" referrerpolicy="no-referrer" sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-popups-to-escape-sandbox" class="card workspace-shell h-[75vh] w-full" />
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { paymentAPI } from '@/api/payment'
const { t } = useI18n()
const url = ref('')
const error = ref(false)
onMounted(async () => {
  try {
    const { data } = await paymentAPI.getCheckoutInfo()
    if (!data.redeem_purchase_enabled || data.redeem_purchase_mode !== 'iframe' || !data.redeem_purchase_url) { error.value = true; return }
    // Use the saved URL verbatim. Never attach site tokens, identity or page URL.
    url.value = data.redeem_purchase_url
  } catch { error.value = true }
})
</script>
