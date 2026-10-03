<template>
  <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <div class="flex items-start justify-between gap-3">
      <div>
        <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.oauth.zhipu.title') }}</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.oauth.zhipu.desc') }}</p>
      </div>
      <span
        v-if="polling"
        data-testid="zhipu-link-polling"
        class="shrink-0 rounded bg-amber-50 px-2 py-0.5 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
      >
        {{ t('admin.accounts.oauth.zhipu.waiting') }}
      </span>
      <span
        v-else-if="ready"
        data-testid="zhipu-link-ready"
        class="shrink-0 rounded bg-emerald-50 px-2 py-0.5 text-xs text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
      >
        {{ ready.user.user_name || ready.user.user_email || ready.user.user_id }}
      </span>
    </div>

    <p v-if="!capabilities?.enabled" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.oauth.zhipu.unavailable') }}
    </p>

    <template v-else>
      <div class="mt-3 grid gap-3 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.providerLabel') }}</label>
          <select v-model="provider" class="input" :disabled="!!session" data-testid="zhipu-link-provider">
            <option v-for="item in capabilities.providers" :key="item" :value="item">
              {{ t(`admin.accounts.oauth.zhipu.providers.${item}`) }}
            </option>
          </select>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.planLabel') }}</label>
          <select v-model="planKind" class="input" :disabled="!!session && !ready" data-testid="zhipu-link-plan">
            <option
              v-for="item in capabilities.plan_kinds"
              :key="item"
              :value="item"
              :disabled="!capabilities.supported_plan_kinds.includes(item)"
            >
              {{ t(`admin.accounts.oauth.zhipu.plans.${item}`) }}
              <template v-if="!capabilities.supported_plan_kinds.includes(item)">&nbsp;({{ t('admin.accounts.oauth.zhipu.unsupported') }})</template>
            </option>
          </select>
        </div>
      </div>

      <div v-if="needsTeamScope" class="mt-3 grid gap-3 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.teamOrganization') }}</label>
          <input v-model="teamOrganization" type="text" class="input" data-testid="zhipu-link-team-org" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.teamProject') }}</label>
          <input v-model="teamProject" type="text" class="input" data-testid="zhipu-link-team-project" />
        </div>
        <p class="input-hint sm:col-span-2">{{ t('admin.accounts.oauth.zhipu.teamScopeHint') }}</p>
      </div>

      <div v-if="session" class="mt-3 space-y-2">
        <div class="flex flex-wrap items-center gap-2">
          <a
            :href="session.authorize_url"
            target="_blank"
            rel="noopener noreferrer"
            class="btn btn-secondary btn-sm"
            data-testid="zhipu-link-open"
          >
            {{ t('admin.accounts.oauth.zhipu.openAuthorize') }}
          </a>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="pollNow">
            {{ t('admin.accounts.oauth.zhipu.checkNow') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="cancel">
            {{ t('admin.accounts.oauth.zhipu.cancel') }}
          </button>
        </div>
        <p class="break-all rounded bg-gray-50 p-2 font-mono text-[11px] leading-relaxed text-gray-600 dark:bg-dark-700 dark:text-gray-300">
          {{ session.authorize_url }}
        </p>
        <p class="input-hint">{{ t('admin.accounts.oauth.zhipu.browserHint') }}</p>
      </div>

      <div v-if="!ready" class="mt-3 space-y-2">
        <button type="button" class="text-xs text-indigo-600 hover:underline dark:text-indigo-400" @click="showFallback = !showFallback">
          {{ showFallback ? t('admin.accounts.oauth.zhipu.hideFallback') : t('admin.accounts.oauth.zhipu.showFallback') }}
        </button>
        <div v-if="showFallback" class="space-y-2">
          <textarea
            v-model="callback"
            rows="2"
            class="input font-mono text-xs"
            :placeholder="t('admin.accounts.oauth.zhipu.fallbackPlaceholder')"
            data-testid="zhipu-link-callback"
          />
          <button type="button" class="btn btn-secondary btn-sm" :disabled="loading || !session" @click="submitCallback">
            {{ t('admin.accounts.oauth.zhipu.fallbackSubmit') }}
          </button>
        </div>
      </div>

      <div class="mt-3 grid gap-3 sm:grid-cols-3">
        <div class="sm:col-span-3">
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.name') }}</label>
          <input v-model="name" type="text" class="input" :placeholder="t('admin.accounts.oauth.zhipu.namePlaceholder')" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.concurrency') }}</label>
          <input v-model.number="concurrency" type="number" min="1" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.oauth.zhipu.priority') }}</label>
          <input v-model.number="priority" type="number" class="input" />
        </div>
      </div>

      <div class="mt-3 flex flex-wrap items-center gap-3">
        <button
          v-if="!session"
          type="button"
          class="btn btn-primary"
          :disabled="loading"
          data-testid="zhipu-link-start"
          @click="start"
        >
          {{ loading ? t('admin.accounts.oauth.zhipu.starting') : t('admin.accounts.oauth.zhipu.start') }}
        </button>
        <button
          v-else
          type="button"
          class="btn btn-primary"
          :disabled="loading || !ready"
          data-testid="zhipu-link-create"
          @click="create"
        >
          {{ t('admin.accounts.oauth.zhipu.create') }}
        </button>
        <span v-if="ready" class="text-xs text-emerald-600 dark:text-emerald-400">
          {{ t('admin.accounts.oauth.zhipu.authorized') }}
        </span>
      </div>

      <p v-if="error" role="alert" class="mt-2 text-xs text-red-600 dark:text-red-400" data-testid="zhipu-link-error">
        {{ error }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useZhipuOAuth, zhipuPlanNeedsTeamScope } from '@/composables/useZhipuOAuth'
import type { ZhipuPlanKind, ZhipuProvider } from '@/api/admin/zhipu'

const props = defineProps<{ proxyId?: number }>()
const emit = defineEmits<{ created: [] }>()

const { t } = useI18n()
const {
  capabilities,
  session,
  ready,
  loading,
  polling,
  error,
  loadCapabilities,
  startLink,
  pollOnce,
  exchangeLink,
  createAccount,
  stopPolling
} = useZhipuOAuth()

const provider = ref<ZhipuProvider>('bigmodel')
const planKind = ref<ZhipuPlanKind>('individual-coding-plan')
const teamOrganization = ref('')
const teamProject = ref('')
const name = ref('')
const concurrency = ref(3)
const priority = ref(0)
const callback = ref('')
const showFallback = ref(false)

const needsTeamScope = computed(() => zhipuPlanNeedsTeamScope(planKind.value))

onMounted(loadCapabilities)

async function start() {
  // startLink schedules the poll loop from the platform's own interval.
  await startLink(provider.value, props.proxyId)
}

async function pollNow() {
  await pollOnce()
}

async function submitCallback() {
  await exchangeLink(callback.value, props.proxyId)
}

function cancel() {
  stopPolling()
}

async function create() {
  // A team plan without its scope is rejected by the server with a specific
  // message, so the panel does not duplicate that validation.
  const ok = await createAccount({
    plan_kind: planKind.value,
    ...(needsTeamScope.value
      ? { team_organization: teamOrganization.value.trim(), team_project: teamProject.value.trim() }
      : {}),
    ...(name.value.trim() ? { name: name.value.trim() } : {}),
    concurrency: concurrency.value,
    priority: priority.value,
    ...(props.proxyId ? { proxy_id: props.proxyId } : {})
  })
  if (ok) {
    emit('created')
  }
}
</script>
