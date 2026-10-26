<template>
  <Select
    :model-value="currentLocaleCode"
    :disabled="switching"
    class="w-auto max-w-32"
    trigger-class="py-1.5 text-xs"
    :aria-label="currentLocale?.name"
    @update:model-value="selectLocale(String($event))"
    :searchable="false"
    :options="availableLocales.map(locale => ({ value: locale.code, label: locale.flag + ' ' + locale.name }))"
  />
</template>

<script setup lang="ts">
import Select from '@/components/common/Select.vue'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale, availableLocales } from '@/i18n'

const { locale } = useI18n()
const switching = ref(false)
const currentLocaleCode = computed(() => locale.value)
const currentLocale = computed(() => availableLocales.find(item => item.code === currentLocaleCode.value))

async function selectLocale(code: string) {
  if (switching.value || code === currentLocaleCode.value) {
    return
  }
  switching.value = true
  try {
    await setLocale(code)
  } finally {
    switching.value = false
  }
}

</script>
