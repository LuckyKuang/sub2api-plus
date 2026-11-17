<template>
  <!-- Purchase cards open the summary in a dialog that points to the full
       list on 可用渠道; other hosts (already inside a dialog or checkout
       panel) keep the inline disclosure. -->
  <div v-if="catalog && dialog">
    <button type="button" class="btn btn-secondary w-full justify-between text-xs" @click="open = true">
      <span>{{ t('billingRates.models') }} · {{ catalog.models.length }}</span>
      <Icon name="chevronRight" size="sm" />
    </button>
    <BaseDialog :show="open" :title="t('billingRates.models')" width="wide" @close="open = false">
      <div class="space-y-3 text-xs">
        <p class="rounded-lg surface-inset px-3 py-2 leading-5 surface-muted">{{ t('billingRates.catalogDialogHint') }}</p>
        <PlanModelCatalogList :catalog="catalog" />
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary" @click="open = false">{{ t('common.close') }}</button>
          <button type="button" class="btn btn-primary" @click="goToChannels">{{ t('billingRates.viewAvailableChannels') }}</button>
        </div>
      </template>
    </BaseDialog>
  </div>
  <details v-else-if="catalog" class="space-y-3 rounded-lg surface-inset p-3 text-xs">
    <summary class="cursor-pointer font-medium">{{ t('billingRates.models') }} · {{ catalog.models.length }}</summary>
    <PlanModelCatalogList :catalog="catalog" />
  </details>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { SaleModelCatalog } from '@/types/billingRate'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import PlanModelCatalogList from './PlanModelCatalogList.vue'

defineProps<{ catalog?: SaleModelCatalog; dialog?: boolean }>()
const { t } = useI18n()
const router = useRouter()
const open = ref(false)
function goToChannels() {
  open.value = false
  void router.push('/available-channels')
}
</script>
