<template>
  <div class="mt-3 space-y-2 rounded-lg surface-inset p-3">
    <p class="text-xs surface-muted">{{ t('billingRates.editor.sharedPriceHint') }}</p>
    <div class="flex flex-wrap items-center gap-2">
      <Select v-model="selectedGroup" :options="options" :placeholder="t('billingRates.editor.selectBoundGroup')" class="min-w-0 flex-1" searchable />
      <RouterLink v-if="selectedGroup" :to="{ path: '/admin/groups', query: { edit_group_id: selectedGroup } }" target="_blank" rel="noopener" class="btn btn-secondary btn-sm">{{ t('billingRates.editor.openGroupRates') }}</RouterLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
const props = defineProps<{ groups: Array<{ id: number; name: string }> }>()
const { t } = useI18n()
const selectedGroup = ref<number | null>(null)
const options = computed(() => props.groups.map(group => ({ value: group.id, label: group.name })))
watch(() => props.groups, groups => {
  if (!groups.some(group => group.id === selectedGroup.value)) selectedGroup.value = groups.length === 1 ? groups[0]!.id : null
}, { immediate: true })
</script>
