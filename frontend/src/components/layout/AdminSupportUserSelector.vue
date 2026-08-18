<template>
  <Select
    :model-value="selectedUserId"
    :options="accountOptions"
    value-key="value"
    label-key="label"
    :searchable="true"
    :search-placeholder="t('admin.support.selectorSearch')"
    :empty-text="selectorEmptyText"
    :aria-label="t('admin.support.selectorLabel')"
    :class="['admin-support-selector', { 'admin-support-selector-collapsed': collapsed }]"
    @update:model-value="selectAccount"
  >
    <template #selected="{ option }">
      <span class="flex min-w-0 items-center gap-2">
        <span class="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
          {{ optionInitial(option as AccountOption | undefined) }}
        </span>
        <span v-if="!collapsed" class="min-w-0 text-left">
          <span class="block truncate text-xs font-semibold text-gray-800 dark:text-gray-100">
            {{ (option as AccountOption | undefined)?.label ?? t('admin.support.selectorLabel') }}
          </span>
          <span class="block truncate text-[10px] text-gray-500 dark:text-gray-400">
            {{ selectedUserId === actorUserId ? t('admin.support.selfAccount') : t('admin.support.readOnlyTarget') }}
          </span>
        </span>
      </span>
    </template>

    <template #option="{ option, selected }">
      <div class="flex min-w-0 flex-1 items-center gap-2">
        <span class="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md bg-gray-100 text-xs font-semibold text-gray-700 dark:bg-dark-700 dark:text-gray-200">
          {{ optionInitial(option as AccountOption) }}
        </span>
        <span class="min-w-0 flex-1">
          <span class="flex items-center gap-1.5">
            <span class="truncate text-sm font-medium">{{ (option as AccountOption).label }}</span>
            <span v-if="(option as AccountOption).value === actorUserId" class="rounded bg-primary-50 px-1.5 py-0.5 text-[10px] text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
              {{ t('admin.support.self') }}
            </span>
          </span>
          <span class="block truncate text-xs text-gray-500 dark:text-gray-400">
            {{ (option as AccountOption).email }} · #{{ (option as AccountOption).value }}
          </span>
        </span>
        <Icon v-if="selected" name="check" size="sm" class="text-primary-500" />
      </div>
    </template>
  </Select>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAdminSupportViewStore, useAuthStore } from '@/stores'
import { accountSelectionDestination, parseAdminSupportTargetId } from '@/utils/adminSupport'

interface Props {
  collapsed?: boolean
}

interface AccountOption {
  [key: string]: unknown
  value: number
  label: string
  email: string
  username: string
  role: 'admin' | 'user'
  status: 'active' | 'disabled'
}

withDefaults(defineProps<Props>(), { collapsed: false })

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const supportStore = useAdminSupportViewStore()

const actorUserId = computed(() => authStore.user?.id ?? 0)
const accountsLoading = computed(() => supportStore.accountsLoading)
const selectorEmptyText = computed(() => {
  if (accountsLoading.value) return t('common.loading')
  if (supportStore.accountsError) return t('admin.support.selectorLoadFailed')
  return t('admin.support.selectorEmpty')
})
const selectedUserId = computed(() => {
  return parseAdminSupportTargetId(route.params.user_id) ?? actorUserId.value
})

const accountOptions = computed<AccountOption[]>(() => {
  const options = new Map<number, AccountOption>()
  const add = (user: {
    id: number
    email: string
    username?: string
    role: 'admin' | 'user'
    status: 'active' | 'disabled'
  }) => {
    const username = user.username?.trim() ?? ''
    options.set(user.id, {
      value: user.id,
      label: username || user.email,
      description: `${user.email} #${user.id}`,
      email: user.email,
      username,
      role: user.role,
      status: user.status
    })
  }

  if (authStore.user) add(authStore.user)
  for (const account of supportStore.accounts) add(account)
  if (supportStore.target) add(supportStore.target)

  return [...options.values()].sort((a, b) => {
    if (a.value === actorUserId.value) return -1
    if (b.value === actorUserId.value) return 1
    return a.label.localeCompare(b.label)
  })
})

function optionInitial(option?: AccountOption): string {
  const label = option?.username || option?.email || '?'
  return label.slice(0, 1).toUpperCase()
}

function selectAccount(value: string | number | boolean | null): void {
  const targetUserId = Number(value)
  if (!Number.isSafeInteger(targetUserId) || targetUserId <= 0 || actorUserId.value <= 0) return
  const destination = accountSelectionDestination(route.path, actorUserId.value, targetUserId)
  if (destination && destination !== route.path) void router.push(destination)
}

onMounted(() => {
  void supportStore.loadAccounts().catch(() => undefined)
})
</script>

<style scoped>
.admin-support-selector {
  width: 100%;
}

.admin-support-selector :deep(.select-trigger) {
  min-height: 42px;
  padding: 0.45rem 0.55rem;
  border-color: rgb(229 231 235 / 0.9);
  background: rgb(249 250 251 / 0.75);
}

.dark .admin-support-selector :deep(.select-trigger) {
  border-color: rgb(55 65 81 / 0.9);
  background: rgb(31 41 55 / 0.55);
}

.admin-support-selector-collapsed :deep(.select-trigger) {
  min-height: 40px;
  justify-content: center;
  padding: 0.4rem;
}

.admin-support-selector-collapsed :deep(.select-icon) {
  display: none;
}
</style>
