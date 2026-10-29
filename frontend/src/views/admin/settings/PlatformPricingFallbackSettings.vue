<template>
  <section class="card workspace-shell" data-testid="platform-pricing-fallback-settings">
    <div class="px-6 py-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0 flex-1">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.settings.pricingFallback.title') }}
          </h2>
          <p class="mt-1 text-sm surface-muted">
            {{ t('admin.settings.pricingFallback.description') }}
          </p>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm shrink-0"
          :disabled="disabled"
          data-testid="pricing-fallback-restore"
          @click="emit('update:modelValue', {})"
        >
          {{ t('admin.settings.pricingFallback.restore') }}
        </button>
      </div>
      <p class="mt-3 text-sm surface-muted" data-testid="pricing-fallback-precedence">
        {{ t('admin.settings.pricingFallback.precedence') }}
      </p>
    </div>

    <div class="space-y-4 px-6 pb-6">
      <p class="text-xs surface-muted" id="pricing-fallback-model-hint">
        {{ t('admin.settings.pricingFallback.modelHint') }}
      </p>
      <p
        v-if="Object.keys(modelValue).length === 0"
        class="text-sm surface-muted"
        data-testid="pricing-fallback-defaults-pending"
        role="status"
      >
        {{ t('admin.settings.pricingFallback.defaultsPending') }}
      </p>
      <div class="grid min-w-0 gap-4 md:grid-cols-2">
        <div
          v-for="platform in platforms"
          :key="platform.id"
          class="min-w-0"
          :data-pricing-fallback-platform="platform.id"
        >
          <label
            :for="`pricing-fallback-model-${platform.id}`"
            class="mb-2 block text-sm font-medium"
          >
            {{ platform.display_name }}
          </label>
          <Select
            :id="`pricing-fallback-model-${platform.id}`"
            :model-value="modelValue[platform.id] ?? null"
            :options="modelOptions(platform.id)"
            :placeholder="t(modelValue[platform.id] === ''
              ? 'admin.settings.pricingFallback.disabledPlaceholder'
              : 'admin.settings.pricingFallback.inherit')"
            :search-placeholder="t('admin.settings.pricingFallback.searchPlaceholder')"
            :creatable-prefix="t('admin.settings.pricingFallback.useModel')"
            :aria-label="t('admin.settings.pricingFallback.modelLabel', { platform: platform.display_name })"
            aria-describedby="pricing-fallback-model-hint"
            :disabled="disabled"
            searchable
            creatable
            clearable
            @update:model-value="setModel(platform.id, $event)"
          />
          <p
            v-if="modelValue[platform.id] === ''"
            class="mt-1.5 text-xs surface-muted"
            data-testid="pricing-fallback-disabled"
          >
            {{ t('admin.settings.pricingFallback.disabled') }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { listPlatforms } from '@/constants/platformCatalog'
import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import type { PricingFallbackModels } from '@/api/admin/settings'

const props = withDefaults(defineProps<{
  modelValue: PricingFallbackModels
  disabled?: boolean
}>(), { disabled: false })

const emit = defineEmits<{
  'update:modelValue': [value: PricingFallbackModels]
}>()

const { t } = useI18n()
const platforms = computed(() => listPlatforms())

function modelOptions(platform: string): SelectOption[] {
  const saved = props.modelValue[platform]
  const models = new Set([
    ...(saved ? [saved] : []),
    ...getModelsByPlatform(platform),
  ])
  return [
    { value: '', label: t('admin.settings.pricingFallback.disabledPlaceholder') },
    ...[...models].map(model => ({ value: model, label: model })),
  ]
}

function setModel(platform: string, value: SelectOption['value']): void {
  // Keep an explicit empty string: deleting the key would restore its default.
  emit('update:modelValue', {
    ...props.modelValue,
    [platform]: typeof value === 'string' ? value.trim() : '',
  })
}
</script>
