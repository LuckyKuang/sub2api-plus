<template>
  <select :value="currentLocaleCode" :disabled="switching" class="input w-auto max-w-32 py-1.5 text-xs" :aria-label="currentLocale?.name" @change="selectLocale(($event.target as HTMLSelectElement).value)">
    <option v-for="locale in availableLocales" :key="locale.code" :value="locale.code">{{ locale.flag }} {{ locale.name }}</option>
  </select>
</template>

<script setup lang="ts">
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
