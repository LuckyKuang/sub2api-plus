<template>
  <section class="mt-6 rounded-lg border border-[var(--border-color)] p-4 sm:p-5" data-testid="codex-error-messages-panel">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0 flex-1">
        <h3 class="text-sm font-semibold">
          {{ t('admin.settings.gatewayForwarding.codexErrorMessages.title') }}
        </h3>
        <p class="mt-1 max-w-3xl text-xs surface-muted">
          {{ t('admin.settings.gatewayForwarding.codexErrorMessages.description') }}
        </p>
        <p class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs surface-muted">
          <span class="badge" data-testid="codex-error-messages-mode">
            {{ customEnabled ? t('admin.settings.gatewayForwarding.codexErrorMessages.customOn') : t('admin.settings.gatewayForwarding.codexErrorMessages.customOff') }}
          </span>
          <span>{{ t('admin.settings.gatewayForwarding.codexErrorMessages.customCount', { count: customCount }) }}</span>
        </p>
      </div>
      <button
        type="button"
        class="btn btn-secondary shrink-0"
        data-testid="codex-error-messages-open"
        @click="openDialog"
      >
        {{ t('admin.settings.gatewayForwarding.codexErrorMessages.configure') }}
      </button>
    </div>

    <BaseDialog
      :show="dialogOpen"
      :title="t('admin.settings.gatewayForwarding.codexErrorMessages.title')"
      width="extra-wide"
      :close-on-click-outside="false"
      @close="closeDialog"
    >
      <div class="space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <p class="min-w-0 flex-1 text-xs surface-muted">
            {{ t('admin.settings.gatewayForwarding.codexErrorMessages.description') }}
          </p>
          <div class="flex shrink-0 items-center gap-2">
            <span class="text-xs surface-muted">
              {{ t('admin.settings.gatewayForwarding.codexErrorMessages.customEnabled') }}
            </span>
            <Toggle
              id="codex-error-messages-custom-enabled"
              v-model="customEnabled"
              data-testid="codex-error-messages-custom-enabled"
              @update:model-value="modeTouched = true"
            />
          </div>
        </div>

        <div class="filter-bar">
          <div class="filter-field">
            <Select
              v-model="groupFilter"
              class="w-full"
              :options="groupOptions"
              data-testid="codex-error-messages-group"
            />
          </div>
          <div class="filter-field">
            <input
              v-model="search"
              type="search"
              class="input w-full"
              :placeholder="t('admin.settings.gatewayForwarding.codexErrorMessages.searchPlaceholder')"
              data-testid="codex-error-messages-search"
            />
          </div>
          <label class="filter-pair text-xs surface-muted">
            <input v-model="onlyCustom" type="checkbox" class="h-4 w-4" />
            {{ t('admin.settings.gatewayForwarding.codexErrorMessages.onlyCustom') }}
          </label>
          <span class="text-xs surface-muted">
            {{ t('admin.settings.gatewayForwarding.codexErrorMessages.customCount', { count: customCount }) }}
          </span>
        </div>

        <p v-if="conflict" class="flex flex-wrap items-center gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs text-amber-600 dark:text-amber-200" data-testid="codex-error-messages-conflict">
          <span>{{ t('admin.settings.gatewayForwarding.codexErrorMessages.conflict') }}</span>
          <button type="button" class="table-action" data-testid="codex-error-messages-conflict-reload" @click="load">
            {{ t('admin.settings.gatewayForwarding.codexErrorMessages.conflictReload') }}
          </button>
        </p>

        <div class="record-table-scroll">
          <table class="record-table w-full text-left text-xs">
            <thead class="surface-muted">
              <tr>
                <th class="px-2 py-2 font-medium">{{ t('admin.settings.gatewayForwarding.codexErrorMessages.reason') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.settings.gatewayForwarding.codexErrorMessages.key') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.settings.gatewayForwarding.codexErrorMessages.code') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.settings.gatewayForwarding.codexErrorMessages.effective') }}</th>
                <th class="px-2 py-2 font-medium">{{ t('admin.settings.gatewayForwarding.codexErrorMessages.source') }}</th>
                <th class="px-2 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              <template v-for="item in filteredItems" :key="item.key">
                <tr class="record-row-parent border-t border-[var(--border-color)] align-top">
                  <td class="px-2 py-2">{{ reasonLabel(item) }}</td>
                  <td class="px-2 py-2 font-mono text-[11px]">{{ item.key }}</td>
                  <td class="px-2 py-2 font-mono text-[11px]">{{ item.public_code || '—' }}</td>
                  <td class="px-2 py-2">
                    <div>{{ effectiveMessage(item) }}</div>
                    <div v-if="draftOverrides[item.key]" class="mt-1 text-[11px] surface-muted">
                      {{ t('admin.settings.gatewayForwarding.codexErrorMessages.customDraft') }}
                    </div>
                  </td>
                  <td class="px-2 py-2">
                    <span class="badge">
                      {{ sourceLabel(item) }}
                    </span>
                  </td>
                  <td class="px-2 py-2 text-right">
                    <button type="button" class="table-action" @click="startEditing(item)">
                      {{ t('admin.settings.gatewayForwarding.codexErrorMessages.edit') }}
                    </button>
                    <button
                      v-if="draftOverrides[item.key]"
                      type="button"
                      class="table-action ml-2"
                      @click="restoreOne(item.key)"
                    >
                      {{ t('admin.settings.gatewayForwarding.codexErrorMessages.restore') }}
                    </button>
                  </td>
                </tr>
                <tr v-if="editingKey === item.key" class="record-row-leaf border-t border-[var(--border-color)]">
                  <td colspan="6" class="px-2 py-3">
                    <div class="grid gap-3 lg:grid-cols-2">
                      <div>
                        <div class="mb-1 text-[11px] font-medium surface-muted">
                          {{ t('admin.settings.gatewayForwarding.codexErrorMessages.defaultText') }}
                        </div>
                        <p class="rounded-lg border border-[var(--border-color)] p-2">
                          {{ item.default_message }}
                        </p>
                      </div>
                      <div>
                        <div class="mb-1 text-[11px] font-medium surface-muted">
                          {{ t('admin.settings.gatewayForwarding.codexErrorMessages.customText') }}
                        </div>
                        <textarea
                          v-model="editorText"
                          class="input min-h-20 w-full"
                          :placeholder="t('admin.settings.gatewayForwarding.codexErrorMessages.customPlaceholder')"
                          data-testid="codex-error-messages-editor"
                          @input="editorError = ''"
                        />
                        <p class="mt-1 flex flex-wrap items-center justify-between gap-2 text-[11px] surface-muted">
                          <span>{{ t('admin.settings.gatewayForwarding.codexErrorMessages.editorHint') }}</span>
                          <span :class="editorError ? 'text-rose-400' : ''" data-testid="codex-error-messages-counter">
                            {{ t('admin.settings.gatewayForwarding.codexErrorMessages.counter', { count: editorCodePoints }) }}
                          </span>
                        </p>
                        <p v-if="editorError" class="mt-1 text-[11px] text-rose-400">
                          {{ editorError }}
                        </p>
                      </div>
                    </div>
                    <div class="mt-3 flex flex-wrap justify-end gap-2">
                      <button type="button" class="btn btn-secondary" @click="cancelEditing">
                        {{ t('admin.settings.gatewayForwarding.codexErrorMessages.cancel') }}
                      </button>
                      <button type="button" class="btn btn-primary" @click="applyEditing(item)">
                        {{ t('admin.settings.gatewayForwarding.codexErrorMessages.apply') }}
                      </button>
                    </div>
                  </td>
                </tr>
              </template>
              <tr v-if="filteredItems.length === 0">
                <td colspan="6" class="px-2 py-6 text-center surface-muted">
                  {{ t('admin.settings.gatewayForwarding.codexErrorMessages.empty') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <template #footer>
        <div class="flex w-full flex-wrap items-center justify-between gap-3">
          <span class="text-xs surface-muted">{{ errorText }}</span>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-secondary" :disabled="saving" @click="resetDraft">
              {{ t('admin.settings.gatewayForwarding.codexErrorMessages.discard') }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="saving || !customCount" @click="restoreAll">
              {{ t('admin.settings.gatewayForwarding.codexErrorMessages.restoreAll') }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="saving" @click="load">
              {{ t('admin.settings.gatewayForwarding.codexErrorMessages.reload') }}
            </button>
            <button type="button" class="btn btn-primary" :disabled="saving || !dirty" data-testid="codex-error-messages-save" @click="save">
              {{ saveButtonLabel }}
            </button>
          </div>
        </div>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getCodexErrorMessages,
  patchCodexErrorMessages,
  type CodexPublicErrorItem,
  type CodexPublicErrorSnapshot,
} from '@/api/admin/settings'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'

const { t } = useI18n()

const snapshot = ref<CodexPublicErrorSnapshot | null>(null)
const baselineOverrides = ref<Record<string, string | null>>({})
const draftOverrides = ref<Record<string, string | null>>({})
const customEnabled = ref(false)
const baselineCustomEnabled = ref(false)
const modeTouched = ref(false)
const loading = ref(false)
const saving = ref(false)
const conflict = ref(false)
const errorText = ref('')
const search = ref('')
const groupFilter = ref('all')
const onlyCustom = ref(false)
const editingKey = ref('')
const editorText = ref('')
const editorError = ref('')
const dialogOpen = ref(false)

const groupOptions = computed(() => [
  { value: 'all', label: t('admin.settings.gatewayForwarding.codexErrorMessages.allGroups') },
  { value: 'request', label: t('admin.settings.gatewayForwarding.codexErrorMessages.groupRequest') },
  { value: 'service', label: t('admin.settings.gatewayForwarding.codexErrorMessages.groupService') },
  { value: 'transport', label: t('admin.settings.gatewayForwarding.codexErrorMessages.groupTransport') },
  { value: 'media', label: t('admin.settings.gatewayForwarding.codexErrorMessages.groupMedia') },
])

const items = computed<CodexPublicErrorItem[]>(() => snapshot.value?.items ?? [])

function reasonLabel(item: CodexPublicErrorItem) {
  const path = `admin.settings.gatewayForwarding.codexErrorMessages.reasons.${item.key}`
  const label = t(path)
  return label === path ? item.key : label
}

const filteredItems = computed(() => {
  const needle = search.value.trim().toLowerCase()
  return items.value.filter((item) => {
    if (groupFilter.value !== 'all' && item.group !== groupFilter.value) return false
    if (onlyCustom.value && !draftOverrides.value[item.key]) return false
    if (!needle) return true
    return (
      item.key.toLowerCase().includes(needle) ||
      (item.public_code || '').toLowerCase().includes(needle) ||
      reasonLabel(item).toLowerCase().includes(needle) ||
      item.default_message.toLowerCase().includes(needle) ||
      effectiveMessage(item).toLowerCase().includes(needle)
    )
  })
})

const customCount = computed(() => Object.values(draftOverrides.value).filter(Boolean).length)

const dirty = computed(() => {
  if (customEnabled.value !== baselineCustomEnabled.value) return true
  const keys = new Set([...Object.keys(baselineOverrides.value), ...Object.keys(draftOverrides.value)])
  for (const key of keys) {
    if ((baselineOverrides.value[key] || null) !== (draftOverrides.value[key] || null)) return true
  }
  return false
})

const saveEnablesCustom = computed(() => {
  if (modeTouched.value || baselineCustomEnabled.value) return false
  const keys = new Set([...Object.keys(baselineOverrides.value), ...Object.keys(draftOverrides.value)])
  for (const key of keys) {
    const before = baselineOverrides.value[key] || null
    const after = draftOverrides.value[key] || null
    if (after && after !== before) return true
  }
  return false
})

const saveButtonLabel = computed(() => {
  if (saving.value) return t('admin.settings.gatewayForwarding.codexErrorMessages.saving')
  if (saveEnablesCustom.value) return t('admin.settings.gatewayForwarding.codexErrorMessages.saveAndEnable')
  return t('admin.settings.gatewayForwarding.codexErrorMessages.save')
})

function effectiveMessage(item: CodexPublicErrorItem) {
  const custom = draftOverrides.value[item.key]
  return customEnabled.value && custom ? custom : item.default_message
}

function sourceLabel(item: CodexPublicErrorItem) {
  const draft = draftOverrides.value[item.key]
  if (draft) {
    return customEnabled.value
      ? t('admin.settings.gatewayForwarding.codexErrorMessages.sourceCustom')
      : t('admin.settings.gatewayForwarding.codexErrorMessages.sourceCustomInactive')
  }
  return t('admin.settings.gatewayForwarding.codexErrorMessages.sourceDefault')
}

function applySnapshot(next: CodexPublicErrorSnapshot) {
  snapshot.value = next
  baselineCustomEnabled.value = next.custom_enabled
  customEnabled.value = next.custom_enabled
  modeTouched.value = false
  const overrides: Record<string, string | null> = {}
  for (const item of next.items) {
    if (item.custom_message) overrides[item.key] = item.custom_message
  }
  baselineOverrides.value = { ...overrides }
  draftOverrides.value = { ...overrides }
  conflict.value = false
  editorError.value = ''
}

async function load() {
  loading.value = true
  errorText.value = ''
  try {
    applySnapshot(await getCodexErrorMessages())
  } catch {
    errorText.value = t('admin.settings.gatewayForwarding.codexErrorMessages.loadFailed')
  } finally {
    loading.value = false
  }
}

const editorCodePoints = computed(() => Array.from(editorText.value).length)

function startEditing(item: CodexPublicErrorItem) {
  editingKey.value = item.key
  editorText.value = draftOverrides.value[item.key] || ''
  editorError.value = ''
}

function cancelEditing() {
  editingKey.value = ''
  editorText.value = ''
  editorError.value = ''
}

function applyEditing(item: CodexPublicErrorItem) {
  const value = editorText.value.trim()
  if ([...value].length > 500) {
    editorError.value = t('admin.settings.gatewayForwarding.codexErrorMessages.limitExceeded')
    return
  }
  if (Array.from(value).some((character) => {
    const code = character.codePointAt(0) ?? 0
    return code < 0x20 || code === 0x7f
  })) {
    editorError.value = t('admin.settings.gatewayForwarding.codexErrorMessages.singleLineRequired')
    return
  }
  if (!value) {
    delete draftOverrides.value[item.key]
  } else {
    draftOverrides.value[item.key] = value
  }
  cancelEditing()
}

function restoreOne(key: string) {
  delete draftOverrides.value[key]
  if (editingKey.value === key) cancelEditing()
}

function restoreAll() {
  const next: Record<string, string | null> = {}
  for (const item of items.value) {
    if (draftOverrides.value[item.key]) next[item.key] = null
  }
  draftOverrides.value = next
}

function resetDraft() {
  draftOverrides.value = { ...baselineOverrides.value }
  customEnabled.value = baselineCustomEnabled.value
  modeTouched.value = false
  conflict.value = false
  cancelEditing()
}

async function save() {
  if (!snapshot.value || !dirty.value) return
  saving.value = true
  errorText.value = ''
  conflict.value = false
  const changes: Record<string, string | null> = {}
  const keys = new Set([...Object.keys(baselineOverrides.value), ...Object.keys(draftOverrides.value)])
  for (const key of keys) {
    const before = baselineOverrides.value[key] || null
    const after = draftOverrides.value[key] || null
    if (before !== after) changes[key] = after
  }
  try {
    applySnapshot(await patchCodexErrorMessages({
      revision: snapshot.value.revision,
      custom_enabled: saveEnablesCustom.value ? true : customEnabled.value,
      changes,
    }))
  } catch (error: any) {
    if (error?.status === 409) {
      conflict.value = true
    } else {
      errorText.value = t('admin.settings.gatewayForwarding.codexErrorMessages.saveFailed')
    }
  } finally {
    saving.value = false
  }
}

function openDialog() {
  dialogOpen.value = true
}

function closeDialog() {
  dialogOpen.value = false
}

onMounted(load)
</script>
