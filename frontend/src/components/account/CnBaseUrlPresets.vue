<template>
  <div v-if="platform === 'stepfun'" class="space-y-2">
    <label class="input-label">{{ t('admin.accounts.oauth.domestic.region') }}</label>
    <Select :model-value="stepRegion" :options="stepRegionOptions" :searchable="false" @update:model-value="selectStepRegion" />
  </div>
  <div v-else class="flex flex-wrap gap-2">
    <button
      v-for="preset in presets"
      :key="preset.mode + ':' + preset.protocol + ':' + preset.url"
      type="button"
      data-testid="cn-base-url-preset"
      :class="[
        'rounded-lg px-3 py-1 text-xs transition-colors',
        isActive(preset)
          ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
          : 'bg-gray-100 text-gray-700 hover:bg-primary-50 hover:text-primary-700 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-primary-900/30 dark:hover:text-primary-400'
      ]"
      @click="emit('select', preset)"
    >
      {{ preset.label }} ({{ displayUrl(preset.url) }})
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { CN_BASE_URL_PRESETS, type CnBaseUrlPreset } from './credentialsBuilder'

// 国产供应商快捷端点：点击把预设地址（及对应账号类型/协议）回填到调用方。
// 与 Grok 预设一致，仅作快速填充，输入框仍接受任意第三方转发地址。
// 传入 protocol 时只显示该协议档的预设（协议 × 账号类型正交分档）。
const props = defineProps<{
  platform: 'kimi' | 'zhipu' | 'deepseek' | 'minimax' | 'stepfun'
  /** 当前已选账号类型，用于过滤和高亮匹配的预设 */
  mode?: 'payg' | 'coding'
  /** 当前已选 API 协议，用于过滤和高亮匹配的预设 */
  protocol?: 'adaptive' | 'chat_completions' | 'anthropic' | 'responses'
  /** 当前输入框中的 base url，用于高亮完全匹配项 */
  currentUrl?: string
}>()

const emit = defineEmits<{
  (e: 'select', preset: CnBaseUrlPreset): void
}>()

const presets = computed(() => {
  const all = CN_BASE_URL_PRESETS[props.platform] ?? []
  // 只按协议过滤：同协议下 payg/coding 两档都展示，点击即同时切换账号类型。
  if (props.protocol == null) return all
  return all.filter(p => p.protocol === props.protocol)
})

const isActive = (preset: CnBaseUrlPreset) =>
  (props.mode != null && preset.mode === props.mode) || preset.url === props.currentUrl

const displayUrl = (url: string) => url.replace(/^https?:\/\//i, '')
const { t } = useI18n()
const stepRegion = computed(() => props.currentUrl?.startsWith('https://api.stepfun.ai/') ? 'global' : 'cn')
const stepRegionOptions = computed(() => [
  { value: 'cn', label: t('admin.accounts.oauth.domestic.cn') },
  { value: 'global', label: t('admin.accounts.oauth.domestic.international') }
])
function selectStepRegion(region: string | number | boolean | null | undefined) {
  const host = region === 'global' ? 'https://api.stepfun.ai/' : 'https://api.stepfun.com/'
  const preset = CN_BASE_URL_PRESETS.stepfun.find(p => p.mode === (props.mode || 'payg') && p.url.startsWith(host))
  if (preset) emit('select', preset)
}
</script>
