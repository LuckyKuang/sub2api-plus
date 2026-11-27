<template>
  <AppLayout workspace>
  <div class="workspace-page min-w-0" data-testid="quality-inspection">
    <div class="workspace-fixed card flex flex-wrap items-center justify-between gap-3 p-5 sm:p-6">
      <div>
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ copy.title }}</h1>
        <p class="mt-1 text-sm surface-muted">{{ copy.subtitle }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="reload">{{ copy.refresh }}</button>
        <button v-support-readonly class="btn btn-primary" :disabled="loading || !settings?.enabled" @click="startCreate">{{ copy.newPlan }}</button>
      </div>
    </div>

    <div class="workspace-scroll">
    <div v-if="!loading && !settings?.enabled" class="card p-4 text-sm text-amber-700 dark:text-amber-300">
      {{ copy.disabledHint }}
    </div>

    <p class="text-sm surface-muted">{{ copy.limitations }}</p>

    <div v-if="error" class="card p-4 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</div>

    <div class="card min-w-0 overflow-hidden">
      <div class="record-table-scroll overflow-x-auto">
      <table class="record-table w-full text-sm [&_th]:px-4 [&_th]:py-3 [&_td]:px-4 [&_td]:py-3">
        <thead>
          <tr>
            <th class="text-left">{{ copy.plan }}</th>
            <th class="text-left">{{ copy.group }}</th>
            <th class="text-left">{{ copy.model }}</th>
            <th class="text-left">{{ copy.status }}</th>
            <th class="text-left">{{ copy.estimate }}</th>
            <th class="text-right">{{ copy.actions }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="plan in pagedPlans" :key="plan.id">
            <td>{{ plan.name }}</td>
            <td>{{ plan.group_name }} <span class="text-xs surface-muted">({{ plan.platform }})</span></td>
            <td><code>{{ plan.model }}</code> · {{ plan.effort }}</td>
            <td>
              <span :class="['badge', plan.enabled ? 'badge-success' : 'badge-gray']">
                {{ plan.enabled ? copy.enabled : copy.paused }}
              </span>
            </td>
            <td>{{ plan.estimated_requests }}</td>
            <td class="text-right">
              <div class="flex items-center justify-end gap-1 whitespace-nowrap">
                <button v-support-readonly class="table-action" @click="startEdit(plan)">{{ copy.edit }}</button>
                <button class="table-action" @click="loadRuns(plan)">{{ copy.history }}</button>
                <ActionMenu :items="[
                  { key: 'run', label: copy.run, mutates: true, disabled: !plan.enabled || busy },
                  { key: 'delete', label: copy.remove, mutates: true, tone: 'danger', disabled: busy },
                ]" @select="onPlanAction(plan, $event)" />
              </div>
            </td>
          </tr>
          <tr v-if="loading || !plans.length">
            <td colspan="6" class="py-6 text-center surface-muted">{{ loading ? copy.loading : copy.empty }}</td>
          </tr>
        </tbody>
      </table>
      </div>
      <Pagination :page="planPage" :page-size="10" :total="plans.length" :disabled="loading" :show-page-size-selector="false" embedded @update:page="planPage = $event" />
    </div>

    <BaseDialog :show="Boolean(draft)" :title="draft?.id ? copy.editPlan : copy.newPlan" width="extra-wide" @close="closeDraft">
    <div v-if="draft" class="space-y-4">
      <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</p>
      <div class="grid gap-4 md:grid-cols-3">
        <label class="block">
          <span class="input-label">{{ copy.name }}</span>
          <input v-model="draft.name" class="input" maxlength="120" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.timezone }}</span>
          <input v-model="draft.timezone" class="input" placeholder="Asia/Shanghai" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.group }}</span>
          <Select v-model="draft.group_id" :options="groupOptions" :disabled="Boolean(draft.id)" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.model }}</span>
          <input v-model="draft.model" class="input" :placeholder="copy.modelPlaceholder" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.effort }}</span>
          <Select v-model="draft.effort" :options="effortOptions" :searchable="false" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.interval }}</span>
          <input v-model.number="draft.interval_minutes" type="number" min="30" max="1440" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.accounts }}</span>
          <input v-model.number="draft.max_accounts" type="number" min="1" max="5" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.maxRounds }}</span>
          <input v-model.number="draft.max_saved_rounds" type="number" min="1" max="200" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.pauseStart }}</span>
          <input v-model="draft.pause_start" type="time" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.pauseEnd }}</span>
          <input v-model="draft.pause_end" type="time" class="input" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.endpoint }}</span>
          <Select v-model="draft.endpoint" :options="endpointOptions" :searchable="false" />
          <span class="mt-1 block text-xs surface-muted">{{ copy.endpointHint }}</span>
        </label>
      </div>

      <div class="flex flex-wrap items-center gap-6">
        <label class="flex items-center gap-2 text-sm">
          <Toggle v-model="draft.enabled" /> {{ copy.enabled }}
        </label>
        <label class="flex items-center gap-2 text-sm">
          <Toggle v-model="draft.schedule_enabled" /> {{ copy.schedule }}
        </label>
        <label class="flex items-center gap-2 text-sm" :class="{ 'opacity-50': !settings?.quarantine_enabled }">
          <Toggle v-model="draft.quarantine_enabled" :disabled="!settings?.quarantine_enabled" />
          {{ copy.quarantine }}
        </label>
        <label class="flex items-center gap-2 text-sm">
          {{ copy.cooldown }}
          <input v-model.number="draft.quarantine_cooldown_minutes" type="number" min="5" max="10080" class="input w-24" />
        </label>
        <label class="flex items-center gap-2 text-sm">
          {{ copy.failRounds }}
          <input v-model.number="draft.quarantine_fail_rounds" type="number" min="1" max="5" class="input w-20" />
        </label>
      </div>

      <div class="space-y-3">
        <h3 class="text-sm font-medium">{{ copy.questions }}</h3>
        <div v-for="question in draft.questions" :key="question.id" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
          <div class="flex flex-wrap items-center gap-3">
            <label class="flex items-center gap-2 text-sm font-medium">
              <input v-model="question.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600" /> {{ question.title }} ({{ question.kind }})
            </label>
            <label class="flex items-center gap-2 text-xs">
              {{ copy.rounds }}
              <input v-model.number="question.rounds" type="number" min="1" max="3" class="input w-16" />
            </label>
            <label v-if="question.expected !== '' || ['number','choice','json'].includes(question.kind)" class="flex items-center gap-2 text-xs flex-1 min-w-0 w-full sm:w-auto">
              {{ copy.expected }}
              <input v-model="question.expected" class="input" />
            </label>
          </div>
          <textarea v-model="question.prompt" rows="2" class="input mt-2 w-full" :placeholder="copy.prompt" />
        </div>
      </div>

    </div>
      <template #footer>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-xs surface-muted">{{ copy.estimateHint }}: {{ estimated }}</p>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="saving" @click="closeDraft">{{ copy.cancel }}</button>
          <button v-support-readonly class="btn btn-primary" :disabled="saving || !draft?.group_id || !draft?.model || !draft?.questions.some(q => q.enabled)" @click="savePlan">
            {{ saving ? copy.saving : copy.save }}
          </button>
        </div>
      </div>
      </template>
    </BaseDialog>
    <ConfirmDialog :show="Boolean(confirmation)" :title="confirmation?.kind === 'delete' ? copy.deleteTitle : copy.runTitle"
      :message="confirmationMessage" :danger="confirmation?.kind === 'delete'" :loading="busy"
      @confirm="confirmPlanAction" @cancel="confirmation = null" />

    <div v-if="activePlan" class="card min-w-0 p-5 space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-lg font-semibold">{{ copy.history }} · {{ activePlan.name }}</h2>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="compareIds.length < 2" @click="runCompare">
            {{ copy.compare }} ({{ compareIds.length }})
          </button>
          <button class="btn btn-secondary" @click="closeHistory">{{ copy.close }}</button>
        </div>
      </div>

      <div class="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
        <div class="min-w-0 space-y-2">
          <button
            v-for="run in pagedRuns"
            :key="run.id"
            class="w-full rounded-lg border p-3 text-left text-sm"
            :class="activeRun?.id === run.id ? 'border-primary-500' : 'border-gray-200 dark:border-dark-700'"
            @click="openRun(run)"
          >
            <div class="flex items-center justify-between">
              <span :class="['badge', statusClass(String(run.stats?.verdict ?? run.status))]">{{ statusLabel(String(run.stats?.verdict ?? run.status)) }}</span>
              <span class="text-xs surface-muted">{{ formatTime(run.started_at) }}</span>
            </div>
            <div class="mt-1 text-xs surface-muted">
              {{ triggerLabel(run.trigger) }} · {{ run.request_count }} {{ copy.requests }}
            </div>
          </button>
          <p v-if="historyLoading || !runs.length" class="text-sm surface-muted">{{ historyLoading ? copy.loading : copy.noRuns }}</p>
          <Pagination v-if="runs.length" :page="runPage" :page-size="10" :total="runs.length" :disabled="historyLoading" :show-page-size-selector="false" embedded @update:page="runPage = $event" />
        </div>

        <div v-if="activeRun" class="min-w-0 space-y-3">
          <p class="text-xs surface-muted">
            {{ activeRun.resolved_platform || '—' }} / {{ activeRun.resolved_upstream_model || activeRun.model }}
            · {{ activeRun.message || statusLabel(activeRun.status) }}
          </p>
          <div class="record-table-scroll overflow-x-auto">
          <table class="record-table w-full text-sm [&_th]:px-4 [&_th]:py-3 [&_td]:px-4 [&_td]:py-3">
            <thead>
              <tr>
                <th class="text-left">{{ copy.question }}</th>
                <th class="text-left">{{ copy.verdict }}</th>
                <th class="text-left">{{ copy.answer }}</th>
                <th class="text-left">{{ copy.duration }}</th>
                <th class="text-left">{{ copy.compare }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="result in pagedResults" :key="result.id">
                <td>{{ result.question_id }} <span class="text-xs surface-muted">{{ result.kind }}</span></td>
                <td><span :class="['badge', statusClass(result.status)]">{{ statusLabel(result.status) }}</span></td>
                <td class="max-w-sm break-words text-xs">
                  <span v-if="result.answer">{{ result.answer }}</span>
                  <span v-else-if="result.error_code" class="text-red-600">{{ result.error_code }}</span>
                  <span v-else class="surface-muted">—</span>
                  <div class="mt-1 surface-muted">{{ copy.expected }}: {{ result.expected || '—' }}</div>
                  <details v-if="result.response" class="mt-2">
                    <summary class="cursor-pointer">{{ copy.rawResponse }}</summary>
                    <pre class="max-h-60 overflow-auto whitespace-pre-wrap break-words">{{ result.response }}</pre>
                  </details>
                </td>
                <td class="text-xs">{{ result.duration_ms ?? '—' }} ms</td>
                <td>
                  <label class="flex items-center gap-2 text-xs">
                    <input type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600" :aria-label="`${copy.compare} ${result.question_id}`" :disabled="compareIds.length >= 4 && !compareIds.includes(result.id)" :checked="compareIds.includes(result.id)" @change="toggleCompare(result)" />
                    <button v-if="result.has_artifact" class="table-action" @click="preview(result)">{{ copy.viewArtifact }}</button>
                  </label>
                </td>
              </tr>
              <tr v-if="resultsLoading || !results.length"><td colspan="5" class="py-6 text-center surface-muted">{{ resultsLoading ? copy.loading : copy.noResults }}</td></tr>
            </tbody>
          </table>
          </div>
          <Pagination :page="resultPage" :page-size="20" :total="results.length" :disabled="resultsLoading" :show-page-size-selector="false" embedded @update:page="resultPage = $event" />
        </div>
      </div>

      <div v-if="artifact" class="space-y-2">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-medium">{{ copy.artifact }}</h3>
          <button class="table-action" @click="artifact = null">{{ copy.close }}</button>
        </div>
        <iframe :title="copy.artifact" :src="artifact" class="h-[420px] w-full rounded-lg border border-gray-200 dark:border-dark-700" sandbox="allow-scripts" />
      </div>

      <div v-if="compareResults.length" class="space-y-2">
        <h3 class="text-sm font-medium">{{ copy.compareResult }}</h3>
        <div class="grid gap-3 md:grid-cols-2">
          <div v-for="item in compareResults" :key="item.id" class="rounded-lg border border-gray-200 p-2 dark:border-dark-700">
            <div class="mb-1 flex items-center justify-between text-xs">
              <span>{{ item.question_id }} · {{ item.bank_version }}</span>
              <span :class="['badge', statusClass(item.status)]">{{ statusLabel(item.status) }}</span>
            </div>
            <iframe
              v-if="compareUrls[item.id]"
              :title="`${copy.artifact} ${item.question_id}`"
              :src="compareUrls[item.id]"
              class="h-[300px] w-full rounded-lg"
              sandbox="allow-scripts"
            />
            <pre v-else class="max-h-[300px] overflow-auto whitespace-pre-wrap text-xs">{{ item.response.slice(0, 2000) }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
  </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ActionMenu from '@/components/common/ActionMenu.vue'
import Pagination from '@/components/common/Pagination.vue'
import Toggle from '@/components/common/Toggle.vue'
import { supportReadonly as vSupportReadonly } from '@/directives/supportReadonly'
import Select from '@/components/common/Select.vue'
import {
  compareQualityResults,
  createQualityPlan,
  deleteQualityPlan,
  getQualityArtifactUrl,
  getQualityRun,
  listQualityGroupOptions,
  listQualityPlans,
  listQualityRuns,
  runQualityPlan,
  updateQualityPlan,
  type QualityPlan,
  type QualityPlanPayload,
  type QualityQuestion,
  type QualityResult,
  type QualityRun,
  type QualitySettings,
} from '@/api/admin/qualityInspection'

const { t, tm } = useI18n()
const copy = computed(() => tm('admin.qualityInspection') as Record<string, string>)

const builtinQuestions = ref<QualityQuestion[]>([])
const planPage = ref(1)
const runPage = ref(1)
const resultPage = ref(1)
const historyLoading = ref(false)
const resultsLoading = ref(false)
const busy = ref(false)
const confirmation = ref<{kind: 'run' | 'delete'; plan: QualityPlan} | null>(null)
const confirmationMessage = computed(() => {
  const action = confirmation.value
  if (!action) return ''
  return action.kind === 'delete'
    ? t('admin.qualityInspection.deleteConfirm', { name: action.plan.name })
    : t('admin.qualityInspection.runConfirm', { group: action.plan.group_name, model: action.plan.model, count: action.plan.estimated_requests })
})
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const plans = ref<QualityPlan[]>([])
const settings = ref<QualitySettings | null>(null)
const groups = ref<{ id: number; name: string; platform: string }[]>([])
const draft = ref<(QualityPlanPayload & { id?: number }) | null>(null)
const activePlan = ref<QualityPlan | null>(null)
const runs = ref<QualityRun[]>([])
const activeRun = ref<QualityRun | null>(null)
const results = ref<QualityResult[]>([])
const artifact = ref<string | null>(null)
const compareIds = ref<number[]>([])
const compareResults = ref<QualityResult[]>([])
const compareUrls = ref<Record<number, string>>({})

const groupOptions = computed(() =>
  groups.value.map((group) => ({ value: group.id, label: `${group.name} · ${group.platform}` })),
)
const effortOptions = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'].map((value) => ({ value, label: value }))
const endpointOptions = computed(() => ['any', 'messages', 'responses', 'chat_completions', 'gemini'].map((value) => ({ value, label: value === 'any' ? copy.value.anyEndpoint : value })))
const pagedPlans = computed(() => plans.value.slice((planPage.value - 1) * 10, planPage.value * 10))
const pagedRuns = computed(() => runs.value.slice((runPage.value - 1) * 10, runPage.value * 10))
const pagedResults = computed(() => results.value.slice((resultPage.value - 1) * 20, resultPage.value * 20))
const estimated = computed(() => {
  const plan = draft.value
  if (!plan) return 0
  const accounts = Math.max(1, Number(plan.max_accounts) || 1)
  const rounds = plan.questions.filter((q) => q.enabled).reduce((sum, q) => sum + Math.max(1, Number(q.rounds) || 1), 0)
  return accounts * rounds
})

async function reload() {
  loading.value = true
  error.value = ''
  try {
    const [data, groupRows] = await Promise.all([listQualityPlans(), listQualityGroupOptions()])
    plans.value = data.plans ?? []
    builtinQuestions.value = data.questions ?? []
    planPage.value = Math.min(planPage.value, Math.max(1, Math.ceil(plans.value.length / 10)))
    settings.value = data.settings
    groups.value = groupRows
    if (activePlan.value) {
      const current = plans.value.find(plan => plan.id === activePlan.value?.id)
      if (current) await loadRuns(current)
      else closeHistory()
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : copy.value.loadFailed
  } finally {
    loading.value = false
  }
}

function startCreate() {
  error.value = ''
  draft.value = {
    group_id: groups.value[0]?.id ?? 0,
    name: '',
    model: '',
    endpoint: 'any',
    effort: 'medium',
    enabled: true,
    schedule_enabled: false,
    interval_minutes: 60,
    pause_start: '22:00',
    pause_end: '09:00',
    timezone: 'Asia/Shanghai',
    max_accounts: 1,
    max_saved_rounds: 20,
    quarantine_enabled: false,
    quarantine_cooldown_minutes: 60,
    quarantine_fail_rounds: 2,
    revision: 0,
    questions: builtinQuestions.value.map(question => ({ ...question, choices: [...(question.choices ?? [])], enabled: true })),
  }
}

function startEdit(plan: QualityPlan) {
  error.value = ''
  draft.value = { ...plan, questions: plan.questions.map((question) => ({ ...question, choices: [...(question.choices ?? [])] })) }
}

async function savePlan() {
  const plan = draft.value
  if (!plan) return
  saving.value = true
  error.value = ''
  try {
    const payload = {
      group_id: Number(plan.group_id),
      name: plan.name ?? '',
      model: plan.model ?? '',
      endpoint: plan.endpoint ?? 'any',
      effort: plan.effort ?? 'medium',
      enabled: Boolean(plan.enabled),
      schedule_enabled: Boolean(plan.schedule_enabled),
      interval_minutes: Number(plan.interval_minutes) || 60,
      pause_start: plan.pause_start ?? '22:00',
      pause_end: plan.pause_end ?? '09:00',
      timezone: plan.timezone ?? 'Asia/Shanghai',
      max_accounts: Number(plan.max_accounts) || 1,
      max_saved_rounds: Number(plan.max_saved_rounds) || 20,
      quarantine_enabled: Boolean(plan.quarantine_enabled),
      quarantine_cooldown_minutes: Number(plan.quarantine_cooldown_minutes) || 60,
      quarantine_fail_rounds: Number(plan.quarantine_fail_rounds) || 2,
      revision: Number(plan.revision) || 0,
      questions: plan.questions,
    }
    if (plan.id) {
      await updateQualityPlan(plan.id, payload)
    } else {
      await createQualityPlan(payload)
    }
    draft.value = null
    await reload()
  } catch (err) {
    error.value = err instanceof Error ? err.message : copy.value.saveFailed
  } finally {
    saving.value = false
  }
}

function closeDraft() {
  if (!saving.value) draft.value = null
}

function onPlanAction(plan: QualityPlan, key: string) {
  if (key === 'run' || key === 'delete') confirmation.value = { kind: key, plan }
}

async function confirmPlanAction() {
  const action = confirmation.value
  if (!action || busy.value) return
  busy.value = true
  error.value = ''
  try {
    if (action.kind === 'delete') {
      await deleteQualityPlan(action.plan.id)
      if (activePlan.value?.id === action.plan.id) closeHistory()
      await reload()
    } else {
      await runQualityPlan(action.plan.id, true)
      await loadRuns(action.plan)
    }
    confirmation.value = null
  } catch (err) {
    error.value = err instanceof Error ? err.message : (action.kind === 'delete' ? copy.value.deleteFailed : copy.value.runFailed)
    confirmation.value = null
  } finally {
    busy.value = false
  }
}

let historyRequest = 0
let resultRequest = 0
let artifactRequest = 0
let compareRequest = 0
function closeHistory() {
  historyRequest++
  resultRequest++
  artifactRequest++
  compareRequest++
  activePlan.value = null
  activeRun.value = null
  runs.value = []
  results.value = []
  artifact.value = null
  compareIds.value = []
  compareResults.value = []
  compareUrls.value = {}
}

async function loadRuns(plan: QualityPlan) {
  const request = ++historyRequest
  resultRequest++
  artifactRequest++
  compareRequest++
  historyLoading.value = true
  runPage.value = 1
  runs.value = []
  activePlan.value = plan
  activeRun.value = null
  results.value = []
  compareIds.value = []
  compareResults.value = []
  artifact.value = null
  try {
    const data = await listQualityRuns(plan.id)
    if (request !== historyRequest) return
    runs.value = data
    if (runs.value.length) await openRun(runs.value[0])
  } catch (err) {
    if (request === historyRequest) error.value = err instanceof Error ? err.message : copy.value.historyFailed
  } finally {
    if (request === historyRequest) historyLoading.value = false
  }
}

async function openRun(run: QualityRun) {
  const request = ++resultRequest
  artifactRequest++
  artifact.value = null
  results.value = []
  resultPage.value = 1
  resultsLoading.value = true
  activeRun.value = run
  try {
    const data = await getQualityRun(run.id)
    if (request === resultRequest) results.value = data.results ?? []
  } catch (err) {
    if (request === resultRequest) error.value = err instanceof Error ? err.message : copy.value.roundFailed
  } finally {
    if (request === resultRequest) resultsLoading.value = false
  }
}

async function preview(result: QualityResult) {
  const request = ++artifactRequest
  try {
    const data = await getQualityArtifactUrl(result.id)
    if (request === artifactRequest) artifact.value = data.url
  } catch (err) {
    if (request === artifactRequest) error.value = err instanceof Error ? err.message : copy.value.artifactFailed
  }
}

function toggleCompare(result: QualityResult) {
  const index = compareIds.value.indexOf(result.id)
  if (index >= 0) {
    compareIds.value.splice(index, 1)
  } else {
    if (compareIds.value.length >= 4) return
    compareIds.value.push(result.id)
  }
}

async function runCompare() {
  if (compareIds.value.length < 2) return
  const request = ++compareRequest
  try {
    const compared = await compareQualityResults([...compareIds.value])
    if (request !== compareRequest) return
    compareResults.value = compared
    const urls: Record<number, string> = {}
    await Promise.all(
      compareResults.value
        .filter((item) => item.has_artifact)
        .map(async (item) => {
          try {
            urls[item.id] = (await getQualityArtifactUrl(item.id)).url
          } catch {
            /* 作品不可用时回退到正文 */
          }
        }),
    )
    if (request === compareRequest) compareUrls.value = urls
  } catch (err) {
    if (request === compareRequest) error.value = err instanceof Error ? err.message : copy.value.compareFailed
  }
}

function statusLabel(status: string): string {
  return (tm('admin.qualityInspection.statuses') as Record<string, string>)[status] ?? status
}

function statusClass(status: string): string {
  if (status === 'pass' || status === 'completed') return 'badge-success'
  if (status === 'fail') return 'badge-danger'
  if (status === 'quota' || status === 'error') return 'badge-warning'
  return 'badge-gray'
}

function formatTime(value: string | null): string {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function triggerLabel(trigger: string) {
  return ['manual', 'timer', 'recheck'].includes(trigger) ? copy.value[trigger] : trigger
}

watch(() => [...compareIds.value], () => {
  compareRequest++
  compareResults.value = []
  compareUrls.value = {}
}, { flush: 'sync' })
onMounted(reload)
</script>
