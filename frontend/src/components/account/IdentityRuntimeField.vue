<template>
  <Select
    v-if="kind"
    :model-value="modelValue || ''"
    :options="options"
    :aria-label="name"
    :searchable="true"
    @update:model-value="emit('update:modelValue', String($event ?? ''))"
  />
  <input v-else :value="modelValue" class="input font-mono text-sm" :placeholder="fallback" :aria-label="name" @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { getTimezoneOptions } from '@/utils/timezones'

const props = defineProps<{ name: string; modelValue?: string; fallback?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t, locale } = useI18n()
const kind = computed(() => props.name === 'X-Client-Language' ? 'language' : ['X-Client-Timezone', 'timezone'].includes(props.name) ? 'timezone' : '')
const languages = ['zh-CN', 'zh-TW', 'zh-HK', 'en-US', 'en-GB', 'ja-JP', 'ko-KR', 'de-DE', 'fr-FR', 'es-ES', 'it-IT', 'pt-BR', 'ru-RU', 'nl-NL', 'ar-SA', 'hi-IN', 'id-ID', 'tr-TR', 'vi-VN', 'th-TH']
const options = computed(() => {
  let values
  if (kind.value === 'timezone') values = [...getTimezoneOptions()]
  else {
    const names = new Intl.DisplayNames([locale?.value || 'en'], { type: 'language' })
    values = languages.map(value => ({ value, label: `${names.of(value)} (${value})` }))
  }
  // Keep a valid previously configured language/IANA alias visible even when
  // it is absent from the browser's current catalog. Never erase it on load.
  if (props.modelValue && !values.some(option => option.value === props.modelValue)) values.push({ value: props.modelValue, label: props.modelValue })
  return [{ value: '', label: `${t('admin.settings.outboundIdentity.inherit')} (${props.fallback || t('admin.settings.outboundIdentity.effectiveGlobal')})` }, ...values]
})
</script>
