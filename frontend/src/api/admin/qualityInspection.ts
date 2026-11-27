import { apiClient } from '../client'

export interface QualityQuestion {
  id: string
  title: string
  kind: 'number' | 'choice' | 'json' | 'drawing' | 'reference'
  prompt: string
  instructions: string
  expected: string
  choices: string[]
  unit: string
  rounds: number
  enabled: boolean
  version?: string
}

export interface QualityPlan {
  id: number
  group_id: number
  group_name: string
  name: string
  platform: string
  model: string
  endpoint: string
  effort: string
  enabled: boolean
  schedule_enabled: boolean
  interval_minutes: number
  pause_start: string
  pause_end: string
  timezone: string
  max_accounts: number
  max_saved_rounds: number
  quarantine_enabled: boolean
  quarantine_cooldown_minutes: number
  quarantine_fail_rounds: number
  revision: number
  questions: QualityQuestion[]
  estimated_requests: number
  updated_at: string
}

export interface QualityPlanPayload {
  group_id: number
  name: string
  model: string
  endpoint: string
  effort: string
  enabled: boolean
  schedule_enabled: boolean
  interval_minutes: number
  pause_start: string
  pause_end: string
  timezone: string
  max_accounts: number
  max_saved_rounds: number
  quarantine_enabled: boolean
  quarantine_cooldown_minutes: number
  quarantine_fail_rounds: number
  revision: number
  questions: QualityQuestion[]
}

export interface QualitySettings {
  enabled: boolean
  quarantine_enabled: boolean
  quarantine_cooldown_minutes: number
  max_requests_per_round: number
  stability_rounds: number
}

export interface QualityRun {
  id: number
  plan_id: number
  trigger: string
  status: string
  model: string
  effort: string
  request_count: number
  message: string
  stats: Record<string, unknown> | null
  resolved_platform: string
  resolved_upstream_model: string
  resolved_endpoint: string
  resolved_source: string
  started_at: string | null
  finished_at: string | null
}

export interface QualityResult {
  id: number
  run_id: number
  account_id: number
  question_id: string
  kind: string
  status: string
  answer: string
  expected: string
  response: string
  has_artifact: boolean
  artifact_truncated: boolean
  first_token_ms: number | null
  duration_ms: number | null
  failure_class: string
  error_code: string
  bank_version: string
  created_at: string
}

const base = '/admin/quality-inspections'

async function unwrap<T>(promise: Promise<{ data: { data?: T } & T }>): Promise<T> {
  const response = await promise
  const body = response.data as { data?: T } & T
  return (body.data ?? body) as T
}

export async function listQualityPlans(): Promise<{
  plans: QualityPlan[]
  settings: QualitySettings
  questions: QualityQuestion[]
}> {
  return unwrap(apiClient.get(base))
}

export async function createQualityPlan(payload: QualityPlanPayload): Promise<QualityPlan> {
  return unwrap(apiClient.post(base, payload))
}

export async function updateQualityPlan(id: number, payload: QualityPlanPayload): Promise<QualityPlan> {
  return unwrap(apiClient.put(`${base}/plans/${id}`, payload))
}

export async function deleteQualityPlan(id: number): Promise<void> {
  await apiClient.delete(`${base}/plans/${id}`)
}

export async function runQualityPlan(id: number, confirmed: boolean): Promise<QualityRun> {
  return unwrap(apiClient.post(`${base}/plans/${id}/run`, { confirmed }))
}

export async function listQualityRuns(planId: number): Promise<QualityRun[]> {
  const data = await unwrap<{ runs: QualityRun[] }>(apiClient.get(`${base}/plans/${planId}/runs`))
  return data.runs ?? []
}

export async function getQualityRun(runId: number): Promise<{ run: QualityRun; results: QualityResult[] }> {
  return unwrap(apiClient.get(`${base}/runs/${runId}`))
}

export async function getQualityArtifactUrl(resultId: number): Promise<{ url: string }> {
  return unwrap(apiClient.get(`${base}/results/${resultId}/artifact-url`))
}

export async function compareQualityResults(ids: number[]): Promise<QualityResult[]> {
  const data = await unwrap<{ results: QualityResult[] }>(
    apiClient.get(`${base}/compare`, { params: { ids: ids.join(',') } }),
  )
  return data.results ?? []
}

export interface QualityGroupOption {
  id: number
  name: string
  platform: string
}

export async function listQualityGroupOptions(): Promise<QualityGroupOption[]> {
  const response = await apiClient.get('/admin/groups', { params: { page: 1, page_size: 200 } })
  const body = response.data as { data?: { items?: QualityGroupOption[] }; items?: QualityGroupOption[] }
  const items = body.data?.items ?? body.items ?? []
  return items.map((item) => ({ id: item.id, name: item.name, platform: item.platform }))
}
