<template>
  <section class="rounded-xl border border-amber-200 bg-amber-50/90 p-4 shadow-sm dark:border-amber-800/70 dark:bg-amber-950/30" role="status">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <span class="mt-0.5 flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300">
          <Icon name="eye" size="md" />
        </span>
        <div class="min-w-0">
          <p class="font-semibold text-amber-950 dark:text-amber-100">{{ t('admin.support.bannerTitle') }}</p>
          <p class="mt-0.5 break-words text-sm text-amber-800 dark:text-amber-200">
            {{ t('admin.support.bannerDescription', { account: targetLabel, id: target.id }) }}
          </p>
        </div>
      </div>
      <span class="inline-flex flex-shrink-0 items-center rounded-full border border-amber-300 bg-white/70 px-3 py-1 text-xs font-semibold text-amber-800 dark:border-amber-700 dark:bg-dark-900/50 dark:text-amber-200">
        {{ t('admin.support.readOnly') }}
      </span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AdminSupportUser } from '@/api/admin/supportView'

const props = defineProps<{ target: AdminSupportUser }>()
const { t } = useI18n()
const targetLabel = computed(() => props.target.username?.trim() || props.target.email)
</script>
