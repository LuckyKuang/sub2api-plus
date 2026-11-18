<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ copy.title }}</h1>
        <p class="mt-1 text-sm surface-muted">{{ copy.subtitle }}</p>
      </div>
      <div class="flex items-center gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="reload">{{ copy.refresh }}</button>
        <button class="btn btn-primary" @click="startCreate">{{ copy.newPlan }}</button>
      </div>
    </div>

    <div v-if="!settings?.enabled" class="card p-4 text-sm text-amber-700 dark:text-amber-300">
      {{ copy.disabledHint }}
    </div>

    <div v-if="error" class="card p-4 text-sm text-red-600 dark:text-red-400" role="alert">{{ error }}</div>

    <div class="card overflow-hidden">
      <table class="record-table w-full">
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
          <tr v-for="plan in plans" :key="plan.id">
            <td>{{ plan.name }}</td>
            <td>{{ plan.group_name }} <span class="text-xs surface-muted">({{ plan.platform }})</span></td>
            <td><code>{{ plan.model }}</code> · {{ plan.effort }}</td>
            <td>
              <span :class="['badge', plan.enabled ? 'badge-success' : 'badge-muted']">
                {{ plan.enabled ? copy.enabled : copy.paused }}
              </span>
            </td>
            <td>{{ plan.estimated_requests }}</td>
            <td class="text-right">
              <button class="table-action" @click="startEdit(plan)">{{ copy.edit }}</button>
              <button class="table-action" @click="runPlan(plan)">{{ copy.run }}</button>
              <button class="table-action" @click="loadRuns(plan)">{{ copy.history }}</button>
              <button class="table-action text-red-600" @click="removePlan(plan)">{{ copy.remove }}</button>
            </td>
          </tr>
          <tr v-if="!plans.length">
            <td colspan="6" class="py-6 text-center surface-muted">{{ copy.empty }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="draft" class="card p-5 space-y-4">
      <h2 class="text-lg font-semibold">{{ draft.id ? copy.editPlan : copy.newPlan }}</h2>
      <div class="grid gap-4 md:grid-cols-3">
        <label class="block">
          <span class="input-label">{{ copy.group }}</span>
          <Select v-model="draft.group_id" :options="groupOptions" :searchable="false" :disabled="Boolean(draft.id)" />
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
          <input v-model="draft.pause_start" class="input" placeholder="22:00" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.pauseEnd }}</span>
          <input v-model="draft.pause_end" class="input" placeholder="09:00" />
        </label>
        <label class="block">
          <span class="input-label">{{ copy.endpoint }}</span>
          <Select v-model="draft.endpoint" :options="endpointOptions" :searchable="false" />
        </label>
      </div>

      <div class="flex flex-wrap items-center gap-6">
        <label class="flex items-center gap-2 text-sm">
          <input v-model="draft.enabled" type="checkbox" /> {{ copy.enabled }}
        </label>
        <label class="flex items-center gap-2 text-sm">
          <input v-model="draft.schedule_enabled" type="checkbox" /> {{ copy.schedule }}
        </label>
        <label class="flex items-center gap-2 text-sm" :class="{ 'opacity-50': !settings?.quarantine_enabled }">
          <input v-model="draft.quarantine_enabled" type="checkbox" :disabled="!settings?.quarantine_enabled" />
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
              <input v-model="question.enabled" type="checkbox" /> {{ question.title }} ({{ question.kind }})
            </label>
            <label class="flex items-center gap-2 text-xs">
              {{ copy.rounds }}
              <input v-model.number="question.rounds" type="number" min="1" max="3" class="input w-16" />
            </label>
            <label v-if="question.expected !== '' || ['number','choice','json'].includes(question.kind)" class="flex items-center gap-2 text-xs flex-1 min-w-[220px]">
              {{ copy.expected }}
              <input v-model="question.expected" class="input" />
            </label>
          </div>
          <textarea v-model="question.prompt" rows="2" class="input mt-2 w-full" :placeholder="copy.prompt" />
        </div>
      </div>

      <div class="flex items-center justify-between gap-3">
        <p class="text-xs surface-muted">{{ copy.estimateHint }}: {{ estimated }}</p>
        <div class="flex gap-2">
          <button class="btn btn-secondary" @click="draft = null">{{ copy.cancel }}</button>
          <button class="btn btn-primary" :disabled="saving || !draft.group_id || !draft.model" @click="savePlan">
            {{ saving ? copy.saving : copy.save }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="activePlan" class="card p-5 space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-lg font-semibold">{{ copy.history }} · {{ activePlan.name }}</h2>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="compareIds.length < 2" @click="runCompare">
            {{ copy.compare }} ({{ compareIds.length }})
          </button>
          <button class="btn btn-secondary" @click="activePlan = null">{{ copy.close }}</button>
        </div>
      </div>

      <div class="grid gap-4 lg:grid-cols-[260px_1fr]">
        <div class="space-y-2">
          <button
            v-for="run in runs"
            :key="run.id"
            class="w-full rounded-lg border p-3 text-left text-sm"
            :class="activeRun?.id === run.id ? 'border-primary-500' : 'border-gray-200 dark:border-dark-700'"
            @click="openRun(run)"
          >
            <div class="flex items-center justify-between">
              <span>{{ statusLabel(run.status) }}</span>
              <span class="text-xs surface-muted">{{ formatTime(run.started_at) }}</span>
            </div>
            <div class="mt-1 text-xs surface-muted">
              {{ run.trigger }} · {{ run.request_count }} {{ copy.requests }}
            </div>
          </button>
          <p v-if="!runs.length" class="text-sm surface-muted">{{ copy.noRuns }}</p>
        </div>

        <div v-if="activeRun" class="space-y-3">
          <p class="text-xs surface-muted">
            {{ activeRun.resolved_platform || '—' }} / {{ activeRun.resolved_upstream_model || activeRun.model }}
            · {{ activeRun.message || statusLabel(activeRun.status) }}
          </p>
          <table class="record-table w-full">
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
              <tr v-for="result in results" :key="result.id">
                <td>{{ result.question_id }} <span class="text-xs surface-muted">{{ result.kind }}</span></td>
                <td><span :class="['badge', statusClass(result.status)]">{{ statusLabel(result.status) }}</span></td>
                <td class="text-xs">
                  <span v-if="result.answer">{{ result.answer }}</span>
                  <span v-else-if="result.error_code" class="text-red-600">{{ result.error_code }}</span>
                  <span v-else class="surface-muted">—</span>
                </td>
                <td class="text-xs">{{ result.duration_ms ?? '—' }} ms</td>
                <td>
                  <label class="flex items-center gap-2 text-xs">
                    <input type="checkbox" :checked="compareIds.includes(result.id)" @change="toggleCompare(result)" />
                    <button v-if="result.has_artifact" class="table-action" @click="preview(result)">{{ copy.viewArtifact }}</button>
                  </label>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="artifact" class="space-y-2">
        <div class="flex items-center justify-between">
          <h3 class="text-sm font-medium">{{ copy.artifact }}</h3>
          <button class="table-action" @click="artifact = null">{{ copy.close }}</button>
        </div>
        <iframe :src="artifact" class="h-[420px] w-full rounded-lg border border-gray-200 dark:border-dark-700" sandbox="allow-scripts" />
      </div>

      <div v-if="compareResults.length" class="space-y-2">
        <h3 class="text-sm font-medium">{{ copy.compareResult }}</h3>
        <div class="grid gap-3" :class="compareResults.length > 2 ? 'md:grid-cols-2' : 'md:grid-cols-2'">
          <div v-for="item in compareResults" :key="item.id" class="rounded-lg border border-gray-200 p-2 dark:border-dark-700">
            <div class="mb-1 flex items-center justify-between text-xs">
              <span>{{ item.question_id }} · {{ item.bank_version }}</span>
              <span :class="['badge', statusClass(item.status)]">{{ statusLabel(item.status) }}</span>
            </div>
            <iframe
              v-if="compareUrls[item.id]"
              :src="compareUrls[item.id]"
              class="h-[300px] w-full rounded"
              sandbox="allow-scripts"
            />
            <pre v-else class="max-h-[300px] overflow-auto whitespace-pre-wrap text-xs">{{ item.response.slice(0, 2000) }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
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
  type QualityQuestion,
  type QualityResult,
  type QualityRun,
  type QualitySettings,
} from '@/api/admin/qualityInspection'

const copy = {
  title: '模型质量巡检',
  subtitle: '按分组对单个模型做定时巡检；单轮答错是线索，稳定性看多轮。',
  refresh: '刷新',
  newPlan: '新建计划',
  disabledHint: '模型质量巡检未开启：请先在「系统设置 → 功能开关」中开启。',
  plan: '计划',
  group: '分组',
  model: '模型',
  modelPlaceholder: '例如 gpt-5.1-codex',
  effort: '思考强度',
  status: '状态',
  estimate: '预估请求',
  actions: '操作',
  enabled: '启用',
  paused: '停用',
  edit: '编辑',
  run: '运行一轮',
  history: '历史',
  remove: '删除',
  empty: '还没有巡检计划。',
  editPlan: '编辑计划',
  interval: '间隔（分钟）',
  accounts: '每轮账号数',
  maxRounds: '保留轮次',
  pauseStart: '暂停开始',
  pauseEnd: '暂停结束',
  endpoint: '端点',
  schedule: '定时运行',
  quarantine: '自动剔除',
  cooldown: '冷却（分钟）',
  failRounds: '连续错答轮数',
  questions: '检测题目（可改题干、标准答案、请求次数）',
  rounds: '次数',
  expected: '标准答案',
  prompt: '题干',
  estimateHint: '单轮预估请求数',
  cancel: '取消',
  save: '保存',
  saving: '保存中…',
  compare: '对比',
  close: '关闭',
  noRuns: '还没有巡检记录。',
  requests: '次请求',
  question: '题目',
  verdict: '判定',
  answer: '识别答案',
  duration: '耗时',
  viewArtifact: '查看作品',
  artifact: '作品预览',
  compareResult: '跨时间对比（题目版本不一致时不可直接比较）',
}

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const plans = ref<QualityPlan[]>([])
const settings = ref<QualitySettings | null>(null)
const groups = ref<{ id: number; name: string; platform: string }[]>([])
const draft = ref<(Partial<QualityPlan> & { questions: QualityQuestion[] }) | null>(null)
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
const endpointOptions = ['any', 'messages', 'responses', 'chat_completions', 'gemini'].map((value) => ({ value, label: value }))
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
    const [data, groupRows] = await Promise.all([listQualityPlans(), listQualityGroupOptions().catch(() => [])])
    plans.value = data.plans ?? []
    settings.value = data.settings
    groups.value = groupRows
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function startCreate() {
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
    questions: [],
  }
  void loadBuiltinQuestions()
}

async function loadBuiltinQuestions() {
  try {
    const data = await listQualityPlans()
    if (draft.value && !draft.value.questions.length) {
      draft.value.questions = data.questions.map((question) => ({ ...question, enabled: true }))
    }
  } catch {
    /* 题库加载失败时保持空列表，保存会由后端校验拦截 */
  }
}

function startEdit(plan: QualityPlan) {
  draft.value = { ...plan, questions: plan.questions.map((question) => ({ ...question })) }
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
    error.value = err instanceof Error ? err.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function removePlan(plan: QualityPlan) {
  if (!window.confirm(`删除计划「${plan.name}」及其历史记录？`)) return
  try {
    await deleteQualityPlan(plan.id)
    await reload()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '删除失败'
  }
}

async function runPlan(plan: QualityPlan) {
  if (!window.confirm(`将对「${plan.group_name}」的 ${plan.model} 发起约 ${plan.estimated_requests} 次真实请求并产生费用，确认运行？`)) return
  try {
    await runQualityPlan(plan.id, true)
    await loadRuns(plan)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '运行失败'
  }
}

async function loadRuns(plan: QualityPlan) {
  activePlan.value = plan
  activeRun.value = null
  results.value = []
  compareIds.value = []
  compareResults.value = []
  artifact.value = null
  try {
    runs.value = await listQualityRuns(plan.id)
    if (runs.value.length) await openRun(runs.value[0])
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载历史失败'
  }
}

async function openRun(run: QualityRun) {
  activeRun.value = run
  try {
    const data = await getQualityRun(run.id)
    results.value = data.results ?? []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载轮次失败'
  }
}

async function preview(result: QualityResult) {
  try {
    const data = await getQualityArtifactUrl(result.id)
    artifact.value = data.url
  } catch (err) {
    error.value = err instanceof Error ? err.message : '作品不可用'
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
  try {
    compareResults.value = await compareQualityResults(compareIds.value)
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
    compareUrls.value = urls
  } catch (err) {
    error.value = err instanceof Error ? err.message : '对比失败'
  }
}

function statusLabel(status: string): string {
  const table: Record<string, string> = {
    pass: '通过',
    fail: '答错',
    unknown: '未识别',
    error: '请求故障',
    quota: '额度不足',
    reference: '仅参考',
    queued: '排队中',
    running: '运行中',
    completed: '已完成',
    completed_with_errors: '完成（有故障）',
    failed: '失败',
    cancelled: '已取消',
  }
  return table[status] ?? status
}

function statusClass(status: string): string {
  if (status === 'pass' || status === 'completed') return 'badge-success'
  if (status === 'fail') return 'badge-danger'
  if (status === 'quota' || status === 'error') return 'badge-warning'
  return 'badge-muted'
}

function formatTime(value: string | null): string {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

onMounted(reload)
</script>
