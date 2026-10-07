<template>
  <div v-if="fields.length" class="space-y-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-800" data-testid="identity-environment-summary">
    <p class="text-sm font-medium">{{ t('admin.settings.outboundIdentity.environment') }} · Ubuntu 24.04 · x86_64</p>
    <p class="text-xs text-gray-500">{{ t('admin.settings.outboundIdentity.environmentGroupHint') }}</p>
    <dl class="grid gap-1 text-xs sm:grid-cols-[auto_1fr]">
      <template v-for="field in fields" :key="field.name">
        <dt class="font-mono text-gray-500">{{ field.name }}</dt>
        <dd class="break-all font-mono">{{ field.builtin }}</dd>
      </template>
    </dl>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { identityEnvironmentHeaders, type IdentityDeclaration } from '@/api/admin/outboundIdentity'
const props = defineProps<{ declarations: IdentityDeclaration[] }>()
const { t } = useI18n()
const fields = computed(() => props.declarations.filter(field => identityEnvironmentHeaders.includes(field.name)))
</script>
