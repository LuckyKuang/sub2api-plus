<template>
  <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="cn-oauth-panel">
    <p class="text-sm font-medium">{{ t('admin.accounts.oauth.domestic.title', { platform: label }) }}</p>
    <p class="input-hint">{{ t(platform === 'stepfun' ? 'admin.accounts.oauth.domestic.stepfunDescription' : 'admin.accounts.oauth.domestic.description') }}</p>
    <template v-if="!session">
      <label v-if="platform !== 'deepseek'" class="block">
        <span class="input-label">{{ t('admin.accounts.oauth.domestic.region') }}</span>
        <Select v-model="region" :options="regionOptions" :searchable="false" :disabled="busy || (platform === 'stepfun' && !!accountId)" />
      </label>
      <button type="button" class="btn btn-secondary" :disabled="busy" @click="startLogin">
        {{ t(accountId ? 'admin.accounts.oauth.domestic.reauthorize' : 'admin.accounts.oauth.domestic.start') }}
      </button>
    </template>
    <template v-else>
      <p v-if="session.user_code" class="font-mono text-lg" data-testid="cn-oauth-user-code">{{ session.user_code }}</p>
      <a v-if="session.status === 'pending' && !expired" :href="session.authorize_url" target="_blank" rel="noopener noreferrer" class="btn btn-secondary">
        {{ t('admin.accounts.oauth.domestic.open') }}
      </a>
      <template v-if="(platform === 'deepseek' || platform === 'stepfun') && session.status === 'pending' && !expired">
        <p class="input-hint">{{ t(platform === 'stepfun' ? 'admin.accounts.oauth.domestic.stepfunCallbackHint' : 'admin.accounts.oauth.domestic.callbackHint') }}</p>
        <textarea v-model="callback" class="input" rows="3" :aria-label="t('admin.accounts.oauth.domestic.callback')" :placeholder="t('admin.accounts.oauth.domestic.callback')" />
        <button type="button" class="btn btn-secondary" :disabled="busy || !callback.trim()" @click="advance(callback.trim())">{{ t('admin.accounts.oauth.domestic.exchange') }}</button>
      </template>
      <p v-if="session.status === 'pending' && !expired" role="status" class="input-hint">{{ t('admin.accounts.oauth.domestic.waiting') }}</p>
      <p v-if="expired" role="alert" class="text-sm text-red-600">{{ t('admin.accounts.oauth.domestic.expired') }}</p>
      <div class="flex gap-2">
        <button v-if="session.status === 'ready'" type="button" class="btn btn-primary" :disabled="busy || expired" @click="save">{{ t(accountId ? 'admin.accounts.oauth.domestic.save' : 'admin.accounts.oauth.domestic.create') }}</button>
        <button type="button" class="btn btn-secondary" @click="cancel">{{ t('common.cancel') }}</button>
      </div>
    </template>
    <p v-if="failed" role="alert" class="text-sm text-red-600">{{ t('admin.accounts.oauth.domestic.failed') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { useCNOAuth } from '@/composables/useCNOAuth'
import type { CNOAuthAccountInput, CNOAuthPlatform } from '@/api/admin/cnOAuth'

const props = defineProps<{ platform: CNOAuthPlatform; proxyId?: number; accountId?: number; accountInput?: CNOAuthAccountInput; initialRegion?: string }>()
const emit = defineEmits<{ completed: [id: number]; 'ready-session': [id: string | undefined] }>()
const { t } = useI18n()
const { session, busy, failed, expired, start, advance, complete, cancel } = useCNOAuth(props.platform)
const region = ref(props.initialRegion || 'cn')
const callback = ref('')
const label = computed(() => ({ deepseek: 'DeepSeek', kimi: 'Kimi', minimax: 'MiniMax', stepfun: 'StepFun' })[props.platform])
const regionOptions = computed(() => [
  { value: 'cn', label: t('admin.accounts.oauth.domestic.cn') },
  { value: 'global', label: t('admin.accounts.oauth.domestic.international') }
])
watch(() => props.proxyId, cancel)
watch([session, expired], () => { if (!session.value || expired.value || session.value.status !== 'pending') callback.value = '' })
watch([session, expired], () => {
  emit('ready-session', !expired.value && session.value?.status === 'ready' ? session.value.session_id : undefined)
})
async function startLogin() {
  callback.value = ''
  await start({ region: region.value, proxy_id: props.proxyId, account_id: props.accountId })
}
async function save() {
  const id = await complete(props.accountInput || {})
  if (id) emit('completed', id)
}
</script>
