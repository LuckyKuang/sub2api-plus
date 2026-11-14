<template>
  <div class="space-y-6">
    <div class="card workspace-shell p-6">
      <div class="mb-4">
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.imageTasks.switches.title') }}</h3>
        <p class="mt-1 text-sm surface-muted">{{ t('admin.imageTasks.switches.description') }}</p>
      </div>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <label class="flex items-start justify-between gap-4 rounded border border-gray-200 p-4 surface-inset dark:border-dark-600">
          <span class="min-w-0">
            <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.imageTasks.switches.ordinary') }}</span>
            <span class="mt-1 block text-xs surface-muted">{{ t('admin.imageTasks.switches.ordinaryHint') }}</span>
          </span>
          <Toggle
            :model-value="ordinaryEnabled"
            :disabled="switchSaving.ordinary"
            data-testid="image-task-ordinary-toggle"
            :aria-label="t('admin.imageTasks.switches.ordinary')"
            @update:model-value="value => persistSwitch('ordinary', value)"
          />
        </label>
        <label class="flex items-start justify-between gap-4 rounded border border-gray-200 p-4 surface-inset dark:border-dark-600">
          <span class="min-w-0">
            <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.imageTasks.switches.batch') }}</span>
            <span class="mt-1 block text-xs surface-muted">{{ t('admin.imageTasks.switches.batchHint') }}</span>
          </span>
          <Toggle
            :model-value="batchEnabled"
            :disabled="switchSaving.batch"
            data-testid="image-task-batch-toggle"
            :aria-label="t('admin.imageTasks.switches.batch')"
            @update:model-value="value => persistSwitch('batch', value)"
          />
        </label>
      </div>
      <p class="mt-3 text-xs surface-muted">{{ t('admin.imageTasks.switches.historyHint') }}</p>
    </div>

    <div class="card workspace-shell p-6">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.imageTasks.storage.title') }}</h3>
          <p class="mt-1 text-sm surface-muted">{{ t('admin.imageTasks.storage.description') }}</p>
        </div>
      </div>

      <label class="inline-flex items-center gap-2 text-sm surface-muted">
        <input v-model="storage.reuse_backup_s3" type="checkbox" />
        <span>{{ t('admin.imageTasks.storage.reuseBackupS3') }}</span>
      </label>

      <div class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
        <div>
          <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.bucket') }}</label>
          <input v-model="storage.bucket" class="input w-full" :placeholder="storage.reuse_backup_s3 ? t('admin.imageTasks.storage.bucketInherited') : ''" />
        </div>
        <div>
          <div class="mb-1 flex min-h-6 items-center justify-between gap-3">
            <label class="block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.prefix') }}</label>
            <div class="flex items-center gap-2">
              <span class="text-xs surface-muted">{{ t('admin.imageTasks.storage.appendDatePath') }}</span>
              <Toggle v-model="storage.append_date_path" data-testid="image-task-storage-date-path" :aria-label="t('admin.imageTasks.storage.appendDatePath')" />
            </div>
          </div>
          <input v-model="storage.prefix" class="input w-full" placeholder="images/" />
          <p class="mt-1 text-xs surface-muted">{{ prefixPreview }}</p>
        </div>

        <template v-if="!storage.reuse_backup_s3">
          <div>
            <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.endpoint') }}</label>
            <input v-model="storage.endpoint" class="input w-full" placeholder="https://<account_id>.r2.cloudflarestorage.com" />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.region') }}</label>
            <input v-model="storage.region" class="input w-full" placeholder="auto" />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.accessKeyId') }}</label>
            <input v-model="storage.access_key_id" class="input w-full" />
          </div>
          <div>
            <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.secretAccessKey') }}</label>
            <input
              v-model="storage.secret_access_key"
              type="password"
              class="input w-full"
              :placeholder="secretConfigured ? t('admin.imageTasks.storage.secretConfigured') : ''"
            />
          </div>
          <label class="inline-flex items-center gap-2 text-sm md:col-span-2 surface-muted">
            <input v-model="storage.force_path_style" type="checkbox" />
            <span>{{ t('admin.imageTasks.storage.forcePathStyle') }}</span>
          </label>
        </template>

        <div>
          <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.publicBaseUrl') }}</label>
          <input v-model="storage.public_base_url" class="input w-full" :placeholder="t('admin.imageTasks.storage.publicBaseUrlPlaceholder')" />
        </div>
        <div>
          <label class="mb-1 block text-xs font-medium surface-muted">{{ t('admin.imageTasks.storage.presignExpiryHours') }}</label>
          <input v-model.number="storage.presign_expiry_hours" type="number" min="1" class="input w-full" />
        </div>
      </div>


      <div class="mt-4 flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="testing" @click="testConnection">
          {{ testing ? t('common.loading') : t('admin.imageTasks.storage.testConnection') }}
        </button>
        <button type="button" class="btn btn-primary btn-sm" data-testid="save-image-task-settings" :disabled="saving" @click="save">
          {{ saving ? t('common.loading') : t('common.save') }}
        </button>
      </div>
    </div>

    <TotpStepUpDialog :controller="stepUp" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import Toggle from '@/components/common/Toggle.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { useImageTaskModes } from '@/composables/useImageTaskModes'
import { getImageTaskSettings, testImageTaskStorage, updateImageTaskSettings, type ImageStorageSettingsPayload } from '@/api/imageTasks'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()

const { refreshImageTaskModes } = useImageTaskModes()

const ordinaryEnabled = ref(false)
const batchEnabled = ref(false)
const secretConfigured = ref(false)
const saving = ref(false)
const testing = ref(false)
const switchSaving = reactive({ ordinary: false, batch: false })

interface StorageForm {
  reuse_backup_s3: boolean
  bucket: string
  prefix: string
  append_date_path: boolean
  public_base_url: string
  presign_expiry_hours: number
  endpoint: string
  region: string
  access_key_id: string
  secret_access_key: string
  force_path_style: boolean
}

const storage = reactive<StorageForm>({
  reuse_backup_s3: false,
  bucket: '',
  prefix: 'images/',
  append_date_path: false,
  public_base_url: '',
  presign_expiry_hours: 24,
  endpoint: '',
  region: 'auto',
  access_key_id: '',
  secret_access_key: '',
  force_path_style: false,
})

const prefixPreview = computed(() => {
  const base = (storage.prefix || '').trim().replace(/\/+$/, '') || 'images'
  return base + '/' + (storage.append_date_path ? 'yyyy/MM/dd/' : '')
})

function applySettings(next: { ordinary_enabled: boolean; batch_enabled: boolean; storage: ImageStorageSettingsPayload; storage_secret_configured: boolean }) {
  ordinaryEnabled.value = next.ordinary_enabled
  batchEnabled.value = next.batch_enabled
  secretConfigured.value = next.storage_secret_configured
  Object.assign(storage, {
    reuse_backup_s3: next.storage.reuse_backup_s3 ?? false,
    bucket: next.storage.bucket ?? '',
    prefix: next.storage.prefix || 'images/',
    append_date_path: next.storage.append_date_path ?? false,
    public_base_url: next.storage.public_base_url ?? '',
    presign_expiry_hours: next.storage.presign_expiry_hours ?? 24,
    endpoint: next.storage.endpoint ?? '',
    region: next.storage.region || 'auto',
    access_key_id: next.storage.access_key_id ?? '',
    secret_access_key: '',
    force_path_style: next.storage.force_path_style ?? false,
  })
}

async function load() {
  try {
    applySettings(await getImageTaskSettings())
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('errors.networkError')))
  }
}

function reportError(error: unknown) {
  if (isStepUpBlocked(error)) {
    appStore.showError(
      stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
        ? t('stepUp.adminApiKeyForbidden')
        : t('stepUp.notEnabled')
    )
    return
  }
  appStore.showError(extractApiErrorMessage(error, t('errors.networkError')))
}

// Each switch persists on its own request: a rejected ordinary switch (for
// example, an incomplete storage target) must never roll back the batch switch.
async function persistSwitch(mode: 'ordinary' | 'batch', value: boolean) {
  if (switchSaving[mode]) return
  const target = mode === 'ordinary' ? ordinaryEnabled : batchEnabled
  const previous = target.value
  target.value = value
  switchSaving[mode] = true
  try {
    const next = await stepUp.run(() => updateImageTaskSettings(
      mode === 'ordinary' ? { ordinary_enabled: value } : { batch_enabled: value },
    ))
    ordinaryEnabled.value = next.ordinary_enabled
    batchEnabled.value = next.batch_enabled
    // The console menu reads the same capability endpoint; refresh it so the
    // entry appears without a full page reload.
    void refreshImageTaskModes(true)
    appStore.showSuccess(t('admin.imageTasks.saved'))
  } catch (error) {
    target.value = previous
    if (isStepUpCancelled(error)) return
    if (extractApiErrorCode(error) === 'IMAGE_STORAGE_INCOMPLETE') {
      appStore.showError(t('admin.imageTasks.switches.storageRequired'))
      return
    }
    reportError(error)
  } finally {
    switchSaving[mode] = false
  }
}

async function save() {
  saving.value = true
  try {
    const next = await stepUp.run(() => updateImageTaskSettings({
      ordinary_enabled: ordinaryEnabled.value,
      batch_enabled: batchEnabled.value,
      storage: { ...storage },
    }))
    applySettings(next)
    void refreshImageTaskModes(true)
    appStore.showSuccess(t('admin.imageTasks.saved'))
  } catch (error) {
    if (isStepUpCancelled(error)) return
    reportError(error)
  } finally {
    saving.value = false
  }
}

async function testConnection() {
  testing.value = true
  try {
    const result = await testImageTaskStorage({ ...storage })
    if (result.ok) {
      appStore.showSuccess(result.message || t('admin.imageTasks.storage.testSuccess'))
    } else {
      appStore.showError(result.message || t('admin.imageTasks.storage.testFailed'))
    }
  } catch (error) {
    reportError(error)
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>
