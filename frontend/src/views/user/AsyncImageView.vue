<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col gap-3">
          <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
            <div class="grid w-full grid-cols-1 gap-3 sm:grid-cols-2 xl:w-auto xl:grid-cols-[260px_168px]">
              <Select
                v-model="selectedApiKeyId"
                :options="apiKeyFilterOptions"
                :disabled="loadingKeys"
                searchable="auto"
                class="w-full"
                data-testid="async-image-api-key-filter"
                :aria-label="t('asyncImage.filters.apiKey')"
                @change="selectApiKey"
              />
              <Select
                v-model="filters.status"
                :options="statusFilterOptions"
                :disabled="loadingTasks"
                :searchable="false"
                class="w-full"
                data-testid="async-image-status-filter"
                :aria-label="t('asyncImage.filters.allStatuses')"
                @change="applyFilters"
              />
            </div>
            <div class="flex flex-wrap items-center gap-2 xl:flex-shrink-0">
              <button type="button" class="btn btn-secondary" :disabled="loadingTasks" :title="t('asyncImage.actions.refresh')" @click="() => loadTasks()">
                <Icon name="refresh" size="md" :class="loadingTasks ? 'animate-spin' : ''" />
              </button>
              <button type="button" class="btn btn-primary" @click="openCreateDialog">
                <Icon name="plus" size="md" class="mr-2" />
                {{ t('asyncImage.actions.create') }}
              </button>
            </div>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.list.keyHint') }}</p>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="tasks" :loading="loadingTasks" row-key="task_id">
          <template #cell-prompt_preview="{ row }">
            <div class="max-w-[360px]">
              <p class="line-clamp-2 text-sm text-gray-800 dark:text-gray-100" :title="row.prompt_preview || ''">
                {{ row.prompt_preview || '-' }}
              </p>
              <p class="mt-1 truncate font-mono text-xs text-gray-400 dark:text-dark-500">{{ row.task_id }}</p>
            </div>
          </template>

          <template #cell-model="{ row }">
            <span class="block max-w-[160px] truncate text-sm text-gray-700 dark:text-gray-300" :title="row.model || ''">
              {{ row.model || '-' }}
            </span>
          </template>

          <template #cell-request_type="{ row }">
            <span class="badge" :class="requestTypeClass(row.request_type)">{{ requestTypeLabel(row.request_type) }}</span>
          </template>

          <template #cell-status="{ row }">
            <span class="badge" :class="statusClass(row.status)">{{ statusLabel(row.status) }}</span>
          </template>

          <template #cell-result="{ row }">
            <div v-if="taskImageUrls(row).length" class="grid w-[104px] grid-cols-2 gap-1">
              <div
                v-for="url in taskImageUrls(row)"
                :key="url"
                class="h-12 w-12 overflow-hidden rounded border border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-800"
              >
                <img :src="url" alt="" class="h-full w-full object-cover" loading="lazy" />
              </div>
            </div>
            <span v-else-if="row.status === 'failed'" class="text-xs text-red-600 dark:text-red-300">{{ row.error?.message || '-' }}</span>
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-created_at="{ row }">
            <span class="text-sm text-gray-600 dark:text-gray-300">{{ formatDate(row.created_at) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex justify-center gap-1">
              <button type="button" class="icon-button" :title="t('asyncImage.actions.view')" :data-testid="`view-task-${row.task_id}`" @click="selectedTask = row">
                <Icon name="eye" size="sm" />
              </button>
              <button
                v-if="row.status === 'failed'"
                type="button"
                class="icon-button icon-button-danger"
                :disabled="deletingTaskId === row.task_id"
                :title="t('asyncImage.actions.delete')"
                :data-testid="`delete-task-${row.task_id}`"
                @click="deleteTarget = row"
              >
                <Icon :name="deletingTaskId === row.task_id ? 'refresh' : 'trash'" size="sm" :class="deletingTaskId === row.task_id ? 'animate-spin' : ''" />
              </button>
            </div>
          </template>

          <template #empty>
            <div class="flex min-h-[260px] flex-col items-center justify-center px-6 py-10 text-center">
              <Icon name="sparkles" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('asyncImage.list.empty') }}</p>
              <p class="mt-1 max-w-md text-sm text-gray-500 dark:text-gray-400">{{ t('asyncImage.list.emptyHint') }}</p>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <div v-if="tasks.length || offset > 0" class="flex items-center justify-end gap-3 border-t border-gray-200 bg-white px-4 py-3 dark:border-dark-700 dark:bg-dark-800">
          <span class="mr-auto text-sm text-gray-500 dark:text-gray-400">{{ t('asyncImage.list.page', { page: currentPage }) }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="offset === 0 || loadingTasks" @click="previousPage">
            {{ t('asyncImage.list.previous') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="!hasMore || loadingTasks" @click="nextPage">
            {{ t('asyncImage.list.next') }}
          </button>
        </div>
      </template>
    </TablePageLayout>

    <BaseDialog :show="showCreateDialog" :title="t('asyncImage.create.title')" width="wide" @close="closeCreateDialog">
      <form class="space-y-5" @submit.prevent="submitTask">
        <div v-if="eligibleKeys.length === 0" class="rounded border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-200">
          {{ t('asyncImage.create.noKeys') }}
        </div>
        <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="sm:col-span-2">
            <span class="input-label">{{ t('asyncImage.create.mode') }}</span>
            <div class="mt-2 inline-flex overflow-hidden rounded border border-gray-200 dark:border-dark-600">
              <button
                type="button"
                class="h-9 px-4 text-sm font-medium transition-colors"
                :class="form.mode === 'generation' ? 'bg-primary-600 text-white' : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
                :disabled="submitting"
                @click="form.mode = 'generation'"
              >
                {{ t('asyncImage.create.generation') }}
              </button>
              <button
                type="button"
                class="h-9 border-l border-gray-200 px-4 text-sm font-medium transition-colors dark:border-dark-600"
                :class="form.mode === 'edit' ? 'bg-primary-600 text-white' : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
                :disabled="submitting"
                @click="form.mode = 'edit'"
              >
                {{ t('asyncImage.create.edit') }}
              </button>
            </div>
          </div>
          <label class="block">
            <span class="input-label">{{ t('asyncImage.create.apiKey') }}</span>
            <select v-model.number="form.apiKeyId" class="input w-full" :disabled="submitting" @change="loadFormModels">
              <option v-for="key in eligibleKeys" :key="key.id" :value="key.id">{{ key.name }}</option>
            </select>
          </label>
          <label class="block">
            <span class="input-label">{{ t('asyncImage.create.model') }}</span>
            <select v-model="form.model" class="input w-full" :disabled="loadingModels || availableModels.length === 0 || submitting" @change="onModelChanged">
              <option v-if="loadingModels" value="" disabled>{{ t('asyncImage.create.modelsLoading') }}</option>
              <option v-else-if="availableModels.length === 0" value="" disabled>{{ t('asyncImage.create.noModels') }}</option>
              <option v-for="model in availableModels" :key="model" :value="model">{{ model }}</option>
            </select>
          </label>
          <label class="block sm:col-span-2">
            <span class="input-label">{{ t('asyncImage.create.prompt') }}</span>
            <textarea v-model="form.prompt" class="input min-h-32 w-full resize-y" :placeholder="t('asyncImage.create.promptPlaceholder')" />
          </label>
          <div v-if="form.mode === 'edit'" class="space-y-4 sm:col-span-2">
            <div>
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <span class="input-label">{{ t('asyncImage.create.referenceImages') }}</span>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.create.referenceImagesHint') }}</p>
                </div>
                <label class="btn btn-secondary btn-sm cursor-pointer" :class="editImages.length >= MAX_EDIT_INPUT_IMAGES ? 'pointer-events-none opacity-60' : ''">
                  <Icon name="upload" size="sm" class="mr-1.5" />
                  {{ t('asyncImage.create.addReferenceImages') }}
                  <input
                    type="file"
                    accept="image/png,image/jpeg,image/webp"
                    multiple
                    class="hidden"
                    :disabled="submitting || editImages.length >= MAX_EDIT_INPUT_IMAGES"
                    @change="handleReferenceImageFiles"
                  />
                </label>
              </div>
              <div v-if="editImages.length" class="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
                <div v-for="(image, index) in editImages" :key="image.url" class="relative overflow-hidden rounded border border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-800">
                  <img :src="image.url" :alt="image.file.name" class="aspect-square h-full w-full object-cover" />
                  <button type="button" class="absolute right-1 top-1 inline-flex h-6 w-6 items-center justify-center rounded bg-black/60 text-white hover:bg-red-600" :title="t('asyncImage.create.removeReferenceImage')" :disabled="submitting" @click="removeReferenceImage(index)">
                    <Icon name="x" size="xs" />
                  </button>
                  <p class="truncate px-2 py-1.5 text-xs text-gray-700 dark:text-gray-200" :title="image.file.name">{{ image.file.name }}</p>
                </div>
              </div>
              <p v-else class="mt-3 text-sm text-amber-700 dark:text-amber-300">{{ t('asyncImage.create.referenceImageRequired') }}</p>
            </div>
            <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <span class="input-label">{{ t('asyncImage.create.maskImage') }}</span>
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.create.maskImageHint') }}</p>
                </div>
                <label class="btn btn-secondary btn-sm cursor-pointer" :class="!editImages.length || editMask ? 'pointer-events-none opacity-60' : ''">
                  <Icon name="upload" size="sm" class="mr-1.5" />
                  {{ t('asyncImage.create.addMaskImage') }}
                  <input
                    type="file"
                    accept="image/png,image/webp"
                    class="hidden"
                    :disabled="submitting || !editImages.length || !!editMask"
                    @change="handleMaskImageFile"
                  />
                </label>
              </div>
              <div v-if="editMask" class="mt-3 flex max-w-sm items-center gap-3 rounded border border-gray-200 bg-gray-50 p-2 dark:border-dark-600 dark:bg-dark-800">
                <img :src="editMask.url" :alt="editMask.file.name" class="h-14 w-14 rounded object-cover" />
                <p class="min-w-0 flex-1 truncate text-sm text-gray-700 dark:text-gray-200" :title="editMask.file.name">{{ editMask.file.name }}</p>
                <button type="button" class="icon-button" :title="t('asyncImage.create.removeMaskImage')" :disabled="submitting" @click="removeMaskImage">
                  <Icon name="x" size="xs" />
                </button>
              </div>
            </div>
          </div>
          <label class="block">
            <span class="input-label">{{ t('asyncImage.create.size') }}</span>
            <select v-model="form.size" class="input w-full">
              <option v-for="option in sizeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <div v-if="usingCustomSize" class="grid grid-cols-1 gap-4 sm:col-span-2 sm:grid-cols-2">
            <label class="block">
              <span class="input-label">{{ t('asyncImage.create.width') }}</span>
              <input v-model.number="form.customWidth" type="number" min="16" max="3840" step="16" class="input w-full" />
            </label>
            <label class="block">
              <span class="input-label">{{ t('asyncImage.create.height') }}</span>
              <input v-model.number="form.customHeight" type="number" min="16" max="3840" step="16" class="input w-full" />
            </label>
            <p v-if="customSizeError" class="sm:col-span-2 text-xs text-red-600 dark:text-red-300">{{ customSizeError }}</p>
            <p v-else-if="customSizeExperimental" class="sm:col-span-2 text-xs text-amber-700 dark:text-amber-300">{{ t('asyncImage.create.experimentalSizeHint') }}</p>
          </div>
          <label v-if="selectedModelIsGPTImage2" class="block">
            <span class="input-label">{{ t('asyncImage.create.quality') }}</span>
            <select v-model="form.quality" class="input w-full">
              <option value="auto">{{ t('asyncImage.create.qualityAuto') }}</option>
              <option value="low">{{ t('asyncImage.create.qualityLow') }}</option>
              <option value="medium">{{ t('asyncImage.create.qualityMedium') }}</option>
              <option value="high">{{ t('asyncImage.create.qualityHigh') }}</option>
            </select>
          </label>
          <label class="block">
            <span class="input-label">{{ t('asyncImage.create.count') }}</span>
            <select v-model.number="form.n" class="input w-full">
              <option :value="1">1</option>
              <option :value="2">2</option>
              <option :value="3">3</option>
              <option :value="4">4</option>
            </select>
          </label>
        </div>
        <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-700">
          <button type="button" class="btn btn-secondary" @click="closeCreateDialog">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="submitting || eligibleKeys.length === 0 || loadingModels || !form.model || (form.mode === 'edit' && editImages.length === 0)">
            <Icon v-if="submitting" name="refresh" size="sm" class="mr-2 animate-spin" />
            {{ submitting ? t('common.submitting') : t('asyncImage.actions.submit') }}
          </button>
        </div>
      </form>
    </BaseDialog>

    <BaseDialog :show="!!selectedTask" :title="t('asyncImage.detail.title')" width="extra-wide" @close="selectedTask = null">
      <div v-if="selectedTask" class="space-y-5">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3 lg:grid-cols-6">
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.columns.status') }}</p>
            <span class="mt-1 inline-flex badge" :class="statusClass(selectedTask.status)">{{ statusLabel(selectedTask.status) }}</span>
          </div>
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.create.model') }}</p>
            <p class="mt-1 truncate text-sm font-medium text-gray-900 dark:text-white">{{ selectedTask.model || '-' }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.columns.type') }}</p>
            <span class="mt-1 inline-flex badge" :class="requestTypeClass(selectedTask.request_type)">{{ requestTypeLabel(selectedTask.request_type) }}</span>
          </div>
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.detail.taskId') }}</p>
            <p class="mt-1 truncate font-mono text-xs text-gray-700 dark:text-gray-300" :title="selectedTask.task_id">{{ selectedTask.task_id }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.detail.requestedImages') }}</p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ selectedTask.requested_images || 1 }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.detail.actualImages') }}</p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ selectedTask.actual_images ?? taskImageUrls(selectedTask).length }}</p>
          </div>
        </div>
        <div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('asyncImage.detail.prompt') }}</p>
          <p class="mt-1 whitespace-pre-wrap text-sm text-gray-800 dark:text-gray-100">{{ selectedTask.prompt_preview || '-' }}</p>
        </div>
        <div v-if="selectedTask.status === 'failed'" class="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-200">
          <span class="font-medium">{{ t('asyncImage.detail.error') }}: </span>{{ selectedTask.error?.message || '-' }}
        </div>
        <div v-else-if="selectedTask.status === 'processing'" class="rounded border border-blue-200 bg-blue-50 px-3 py-2 text-sm text-blue-800 dark:border-blue-900/70 dark:bg-blue-950/30 dark:text-blue-200">
          {{ t('asyncImage.detail.pending') }}
        </div>
        <div v-else-if="taskImageUrls(selectedTask).length" class="space-y-3">
          <div v-if="imageCountMismatch(selectedTask)" class="rounded border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200">
            {{ t('asyncImage.detail.countMismatch', { requested: selectedTask.requested_images || 1, actual: selectedTask.actual_images ?? taskImageUrls(selectedTask).length }) }}
          </div>
          <div class="flex justify-end">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="downloading || !selectedTaskApiKey" @click="downloadSelectedTask">
              <Icon :name="downloading ? 'refresh' : 'download'" size="sm" class="mr-1.5" :class="downloading ? 'animate-spin' : ''" />
              {{ downloading ? t('common.submitting') : t('asyncImage.actions.downloadAll') }}
            </button>
          </div>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div v-for="url in taskImageUrls(selectedTask)" :key="url" class="overflow-hidden rounded border border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-800">
              <img :src="url" alt="" class="aspect-square h-full w-full object-cover" loading="lazy" />
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('asyncImage.detail.noImage') }}</p>
      </div>
    </BaseDialog>

    <ConfirmDialog
      :show="!!deleteTarget"
      :title="t('asyncImage.delete.title')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :loading="!!deletingTaskId"
      danger
      @confirm="confirmDeleteTask"
      @cancel="deleteTarget = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { keysAPI } from '@/api'
import { AsyncImageDownloadValidationError, deleteAsyncImageTask, downloadAsyncImageZip, getAsyncImageTask, listAsyncImageModels, listAsyncImageTasks, preferredAsyncImageModel, saveAsyncImageBlob, submitAsyncImageEdit, submitAsyncImageGeneration, type AsyncImageTask } from '@/api/asyncImage'
import { keyAllowsAsyncImage, keyCanManageAsyncImage } from '@/composables/useAsyncImageAccess'
import { useAppStore } from '@/stores/app'
import { isGPTImage2, isGPTImage2ExperimentalSize, validateGPTImage2CustomSize } from '@/utils/asyncImageSize'
import type { Column } from '@/components/common/types'
import type { ApiKey } from '@/types'

const POLL_INTERVAL_MS = 3000
const PAGE_SIZE = 20
const MAX_EDIT_INPUT_IMAGES = 4
const MAX_EDIT_UPLOAD_FILE_BYTES = 20 * 1024 * 1024
const MAX_EDIT_UPLOAD_TOTAL_BYTES = 40 * 1024 * 1024
const supportedEditImageTypes = new Set(['image/png', 'image/jpeg', 'image/webp'])
const supportedMaskImageTypes = new Set(['image/png', 'image/webp'])

type AsyncImageMode = 'generation' | 'edit'

interface EditImageUpload {
  file: File
  url: string
}

const { t } = useI18n()
const appStore = useAppStore()
const apiKeys = ref<ApiKey[]>([])
const selectedApiKeyId = ref(0)
const loadingKeys = ref(false)
const loadingModels = ref(false)
const loadingTasks = ref(false)
const submitting = ref(false)
const downloading = ref(false)
const tasks = ref<AsyncImageTask[]>([])
const availableModels = ref<string[]>([])
const hasMore = ref(false)
const offset = ref(0)
const showCreateDialog = ref(false)
const selectedTask = ref<AsyncImageTask | null>(null)
const deleteTarget = ref<AsyncImageTask | null>(null)
const deletingTaskId = ref('')
let pollTimer: ReturnType<typeof setTimeout> | null = null
let modelRequestID = 0
let taskListRequestID = 0

const filters = reactive({ status: '' })
const form = reactive({ apiKeyId: 0, mode: 'generation' as AsyncImageMode, model: '', prompt: '', size: '1024x1024', customWidth: 1024, customHeight: 1024, quality: 'auto', n: 1 })
const editImages = ref<EditImageUpload[]>([])
const editMask = ref<EditImageUpload | null>(null)

const eligibleKeys = computed(() => apiKeys.value.filter(keyAllowsAsyncImage))
const manageableKeys = computed(() => apiKeys.value.filter(keyCanManageAsyncImage))
const apiKeyFilterOptions = computed<SelectOption[]>(() => [
  { value: 0, label: t('asyncImage.filters.selectKey') },
  ...manageableKeys.value.map(key => ({ value: key.id, label: key.name || `API Key #${key.id}` })),
])
const statusFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('asyncImage.filters.allStatuses') },
  { value: 'processing', label: t('asyncImage.status.processing') },
  { value: 'completed', label: t('asyncImage.status.completed') },
  { value: 'failed', label: t('asyncImage.status.failed') },
])
const selectedApiKey = computed(() => manageableKeys.value.find(key => key.id === selectedApiKeyId.value) || null)
const formApiKey = computed(() => eligibleKeys.value.find(key => key.id === form.apiKeyId) || null)
const selectedTaskApiKey = computed(() => selectedApiKey.value)
const selectedModelIsGPTImage2 = computed(() => isGPTImage2(form.model))
const usingCustomSize = computed(() => selectedModelIsGPTImage2.value && form.size === 'custom')
const customSizeValidationCode = computed(() => usingCustomSize.value ? validateGPTImage2CustomSize(form.customWidth, form.customHeight) : null)
const customSizeError = computed(() => customSizeValidationCode.value ? t(`asyncImage.create.customSizeErrors.${customSizeValidationCode.value}`) : '')
const customSizeExperimental = computed(() => usingCustomSize.value && !customSizeValidationCode.value && isGPTImage2ExperimentalSize(form.customWidth, form.customHeight))
const currentPage = computed(() => Math.floor(offset.value / PAGE_SIZE) + 1)
const deleteConfirmMessage = computed(() => t('asyncImage.delete.confirm', { taskId: deleteTarget.value?.task_id || '' }))
const sizeOptions = computed(() => {
  if (!selectedModelIsGPTImage2.value) {
    return [
      { value: '1024x1024', label: '1024 x 1024' },
      { value: '1536x1024', label: '1536 x 1024' },
      { value: '1024x1536', label: '1024 x 1536' },
    ]
  }
  return [
    { value: 'auto', label: t('asyncImage.create.sizeAuto') },
    { value: '1024x1024', label: t('asyncImage.create.size1K') },
    { value: '1536x1024', label: t('asyncImage.create.sizeLandscape15K') },
    { value: '1024x1536', label: t('asyncImage.create.sizePortrait15K') },
    { value: '2048x2048', label: t('asyncImage.create.size2KSquare') },
    { value: '2048x1152', label: t('asyncImage.create.size2KWide') },
    { value: '2560x1440', label: t('asyncImage.create.sizeQHD') },
    { value: '3840x2160', label: t('asyncImage.create.size4KWide') },
    { value: '2160x3840', label: t('asyncImage.create.size4KPortrait') },
    { value: 'custom', label: t('asyncImage.create.sizeCustom') },
  ]
})
const columns = computed<Column[]>(() => [
  { key: 'prompt_preview', label: t('asyncImage.columns.prompt'), sortable: false, class: 'min-w-[280px] max-w-[360px]' },
  { key: 'model', label: t('asyncImage.columns.model'), sortable: false, class: 'w-44' },
  { key: 'request_type', label: t('asyncImage.columns.type'), sortable: false, class: 'w-24 text-center' },
  { key: 'status', label: t('asyncImage.columns.status'), sortable: false, class: 'w-28 text-center' },
  { key: 'result', label: t('asyncImage.columns.result'), sortable: false, class: 'w-[120px] text-center' },
  { key: 'created_at', label: t('asyncImage.columns.createdAt'), sortable: false, class: 'w-44' },
  { key: 'actions', label: t('asyncImage.columns.actions'), sortable: false, class: 'w-24 text-center' },
])

function formatDate(timestamp: number) {
  return timestamp ? new Date(timestamp * 1000).toLocaleString() : '-'
}

function statusLabel(status: string) {
  const key = `asyncImage.status.${status}`
  const translated = t(key)
  return translated === key ? status : translated
}

function statusClass(status: string) {
  if (status === 'completed') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (status === 'failed') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
}

function requestTypeLabel(requestType?: string) {
  return requestType === 'edit' ? t('asyncImage.requestTypes.edit') : t('asyncImage.requestTypes.generation')
}

function requestTypeClass(requestType?: string) {
  return requestType === 'edit'
    ? 'bg-violet-100 text-violet-700 dark:bg-violet-900/30 dark:text-violet-300'
    : 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300'
}

function taskImageUrls(task: AsyncImageTask): string[] {
  const urls = [task.image_url, ...(task.result?.data || []).map(item => item.url)].filter((url): url is string => Boolean(url))
  return [...new Set(urls)]
}

function imageCountMismatch(task: AsyncImageTask): boolean {
  if (task.status !== 'completed') return false
  const requested = task.requested_images || 1
  const actual = task.actual_images ?? taskImageUrls(task).length
  return actual < requested
}

function errorMessage(error: unknown, fallback: string) {
  return (error as { message?: string })?.message || fallback
}

async function loadApiKeys() {
  loadingKeys.value = true
  try {
    const loadedKeys = [] as ApiKey[]
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, 100, { sort_by: 'created_at', sort_order: 'desc' })
      loadedKeys.push(...(response.items || []))
      const pages = Number.isFinite(response.pages) && response.pages > 0 ? response.pages : 1
      if (page >= pages || (response.items || []).length === 0) break
      page += 1
    }
    apiKeys.value = loadedKeys
    if (!manageableKeys.value.some(key => key.id === selectedApiKeyId.value)) {
      selectedApiKeyId.value = eligibleKeys.value[0]?.id || manageableKeys.value[0]?.id || 0
    }
  } catch (error) {
    appStore.showError(errorMessage(error, t('asyncImage.errors.loadKeys')))
  } finally {
    loadingKeys.value = false
  }
}

async function loadFormModels() {
  const key = formApiKey.value
  const requestID = ++modelRequestID
  availableModels.value = []
  form.model = ''
  if (!key) return

  loadingModels.value = true
  try {
    const models = await listAsyncImageModels(key.key)
    if (requestID !== modelRequestID) return
    availableModels.value = models
    form.model = preferredAsyncImageModel(models)
    onModelChanged()
  } catch (error) {
    if (requestID !== modelRequestID) return
    appStore.showError(errorMessage(error, t('asyncImage.errors.loadModels')))
  } finally {
    if (requestID === modelRequestID) loadingModels.value = false
  }
}

function onModelChanged() {
  if (!selectedModelIsGPTImage2.value && form.size === 'custom') form.size = '1024x1024'
  if (!selectedModelIsGPTImage2.value && form.size === 'auto') form.size = '1024x1024'
}

function totalEditUploadBytes() {
  return editImages.value.reduce((total, image) => total + image.file.size, editMask.value?.file.size || 0)
}

function revokeEditUpload(upload: EditImageUpload | null) {
  if (upload) URL.revokeObjectURL(upload.url)
}

function clearEditUploads() {
  editImages.value.forEach(revokeEditUpload)
  editImages.value = []
  revokeEditUpload(editMask.value)
  editMask.value = null
}

function uploadErrorFor(file: File, allowedTypes: Set<string>) {
  if (!allowedTypes.has(file.type)) return t('asyncImage.create.invalidUploadType')
  if (file.size > MAX_EDIT_UPLOAD_FILE_BYTES) return t('asyncImage.create.uploadFileTooLarge')
  return ''
}

function handleReferenceImageFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!files.length) return
  if (editImages.value.length + files.length > MAX_EDIT_INPUT_IMAGES) {
    appStore.showError(t('asyncImage.create.referenceImageLimit', { count: MAX_EDIT_INPUT_IMAGES }))
    return
  }
  const invalid = files.find(file => uploadErrorFor(file, supportedEditImageTypes))
  if (invalid) {
    appStore.showError(uploadErrorFor(invalid, supportedEditImageTypes))
    return
  }
  if (totalEditUploadBytes() + files.reduce((total, file) => total + file.size, 0) > MAX_EDIT_UPLOAD_TOTAL_BYTES) {
    appStore.showError(t('asyncImage.create.uploadTotalTooLarge'))
    return
  }
  editImages.value.push(...files.map(file => ({ file, url: URL.createObjectURL(file) })))
}

function handleMaskImageFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const error = uploadErrorFor(file, supportedMaskImageTypes)
  if (error) {
    appStore.showError(error)
    return
  }
  if (totalEditUploadBytes() + file.size > MAX_EDIT_UPLOAD_TOTAL_BYTES) {
    appStore.showError(t('asyncImage.create.uploadTotalTooLarge'))
    return
  }
  revokeEditUpload(editMask.value)
  editMask.value = { file, url: URL.createObjectURL(file) }
}

function removeReferenceImage(index: number) {
  const [removed] = editImages.value.splice(index, 1)
  revokeEditUpload(removed || null)
  if (index === 0 && editMask.value) removeMaskImage()
}

function removeMaskImage() {
  revokeEditUpload(editMask.value)
  editMask.value = null
}

function stopPolling() {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

function schedulePolling() {
  stopPolling()
  if (!selectedApiKey.value || !tasks.value.some(task => task.status === 'processing')) return
  pollTimer = setTimeout(async () => {
    await refreshProcessingTasks()
    schedulePolling()
  }, POLL_INTERVAL_MS)
}

async function refreshProcessingTasks() {
  const key = selectedApiKey.value
  if (!key) return
  const active = tasks.value.filter(task => task.status === 'processing')
  if (!active.length) return
  const refreshed = await Promise.allSettled(active.map(task => getAsyncImageTask(key.key, task.task_id)))
  refreshed.forEach((result, index) => {
    if (result.status !== 'fulfilled') return
    const taskID = active[index].task_id
    const taskIndex = tasks.value.findIndex(task => task.task_id === taskID)
    if (taskIndex !== -1) tasks.value.splice(taskIndex, 1, result.value)
    if (selectedTask.value?.task_id === taskID) selectedTask.value = result.value
  })
}

async function loadTasks(showError = true): Promise<boolean> {
  const key = selectedApiKey.value
  const requestID = ++taskListRequestID
  stopPolling()
  if (!key) {
    tasks.value = []
    hasMore.value = false
    loadingTasks.value = false
    return true
  }
  loadingTasks.value = true
  try {
    const response = await listAsyncImageTasks(key.key, { status: filters.status, limit: PAGE_SIZE, offset: offset.value })
    if (requestID !== taskListRequestID) return false
    tasks.value = response.data || []
    hasMore.value = response.has_more === true
    return true
  } catch (error) {
    if (requestID !== taskListRequestID) return false
    tasks.value = []
    hasMore.value = false
    if (showError) appStore.showError(errorMessage(error, t('asyncImage.errors.loadTasks')))
    return false
  } finally {
    if (requestID === taskListRequestID) {
      loadingTasks.value = false
      schedulePolling()
    }
  }
}

function applyFilters() {
  offset.value = 0
  void loadTasks()
}

function selectApiKey() {
  offset.value = 0
  selectedTask.value = null
  void loadTasks()
}

async function confirmDeleteTask() {
  const target = deleteTarget.value
  const key = selectedApiKey.value
  if (!target || !key || deletingTaskId.value) return

  deletingTaskId.value = target.task_id
  try {
    await deleteAsyncImageTask(key.key, target.task_id)
    if (selectedTask.value?.task_id === target.task_id) selectedTask.value = null
    deleteTarget.value = null
    appStore.showSuccess(t('asyncImage.delete.success'))
    const loaded = await loadTasks()
    if (loaded && tasks.value.length === 0 && offset.value > 0) {
      offset.value = Math.max(0, offset.value - PAGE_SIZE)
      await loadTasks()
    }
  } catch (error) {
    const code = (error as { code?: string })?.code
    appStore.showError(t(code === 'IMAGE_TASK_DELETE_NOT_ALLOWED'
      ? 'asyncImage.errors.deleteNotAllowed'
      : 'asyncImage.errors.delete'))
  } finally {
    deletingTaskId.value = ''
  }
}

function previousPage() {
  offset.value = Math.max(0, offset.value - PAGE_SIZE)
  void loadTasks()
}

function nextPage() {
  if (!hasMore.value) return
  offset.value += PAGE_SIZE
  void loadTasks()
}

function openCreateDialog() {
  form.apiKeyId = eligibleKeys.value.some(key => key.id === selectedApiKeyId.value)
    ? selectedApiKeyId.value
    : eligibleKeys.value[0]?.id || 0
  form.mode = 'generation'
  form.size = '1024x1024'
  form.customWidth = 1024
  form.customHeight = 1024
  form.quality = 'auto'
  clearEditUploads()
  showCreateDialog.value = true
  void loadFormModels()
}

function closeCreateDialog() {
  if (submitting.value) return
  showCreateDialog.value = false
  clearEditUploads()
}

async function downloadSelectedTask() {
  const task = selectedTask.value
  const key = selectedTaskApiKey.value
  if (!task || !key || downloading.value || taskImageUrls(task).length === 0) return
  downloading.value = true
  try {
    const blob = await downloadAsyncImageZip(key.key, task.task_id)
    saveAsyncImageBlob(blob, `${task.task_id}.zip`)
  } catch (error) {
    appStore.showError(error instanceof AsyncImageDownloadValidationError
      ? t('asyncImage.errors.invalidArchive')
      : errorMessage(error, t('asyncImage.errors.download')))
  } finally {
    downloading.value = false
  }
}

async function submitTask() {
  const key = formApiKey.value
  if (!key) {
    appStore.showError(t('asyncImage.create.noKeys'))
    return
  }
  if (!form.model.trim()) {
    appStore.showError(t('asyncImage.create.modelRequired'))
    return
  }
  if (!form.prompt.trim()) {
    appStore.showError(t('asyncImage.create.promptRequired'))
    return
  }
  if (form.mode === 'edit' && editImages.value.length === 0) {
    appStore.showError(t('asyncImage.create.referenceImageRequired'))
    return
  }
  if (customSizeError.value) {
    appStore.showError(customSizeError.value)
    return
  }
  const size = usingCustomSize.value ? `${form.customWidth}x${form.customHeight}` : form.size
  submitting.value = true
  try {
    const payload = {
      model: form.model.trim(),
      prompt: form.prompt.trim(),
      size,
      n: form.n,
      ...(selectedModelIsGPTImage2.value ? { quality: form.quality } : {}),
    }
    const created = form.mode === 'edit'
      ? await submitAsyncImageEdit(key.key, {
        ...payload,
        images: editImages.value.map(image => image.file),
        ...(editMask.value ? { mask: editMask.value.file } : {}),
      })
      : await submitAsyncImageGeneration(key.key, payload)
    selectedApiKeyId.value = key.id
    filters.status = ''
    offset.value = 0
    tasks.value = [created, ...tasks.value.filter(task => task.task_id !== created.task_id)].slice(0, PAGE_SIZE)
    showCreateDialog.value = false
    form.prompt = ''
    clearEditUploads()
    appStore.showSuccess(t('asyncImage.create.submitted'))
    schedulePolling()
  } catch (error) {
    appStore.showError(errorMessage(error, t('asyncImage.errors.submit')))
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  await loadApiKeys()
  await loadTasks()
})

onBeforeUnmount(() => {
  taskListRequestID++
  stopPolling()
  clearEditUploads()
})
</script>

<style scoped>
.icon-button {
  @apply inline-flex h-8 w-8 items-center justify-center rounded border border-gray-200 text-gray-600 transition-colors hover:border-primary-300 hover:bg-primary-50 hover:text-primary-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 dark:border-dark-600 dark:text-dark-300 dark:hover:border-primary-700 dark:hover:bg-primary-900/20 dark:hover:text-primary-300;
}

.icon-button-danger {
  @apply text-red-600 hover:border-red-300 hover:bg-red-50 hover:text-red-700 disabled:cursor-not-allowed disabled:opacity-60 dark:text-red-400 dark:hover:border-red-800 dark:hover:bg-red-950/30 dark:hover:text-red-300;
}
</style>
