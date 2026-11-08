import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type { ScheduledTestPlan, ScheduledTestResult } from '@/types'
import { Modal, Select, Input, Toggle, type SelectOption } from '@/components/ui'
import { Icon } from './AccountIcon'
import { ConfirmDialog } from './shared'
import { TriggerTooltip } from './TriggerTooltip'

export interface ScheduledTestsPanelProps {
  show: boolean
  accountId: number | null
  modelOptions: SelectOption[]
  onClose: () => void
}

const HELP_TRIGGER_CLASS =
  'inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-gray-400/70 text-[10px] font-semibold text-gray-400 transition-colors hover:border-primary-500 hover:text-primary-600 dark:border-gray-500 dark:text-gray-500 dark:hover:border-primary-400 dark:hover:text-primary-400'

export function ScheduledTestsPanel({ show, accountId, modelOptions, onClose }: ScheduledTestsPanelProps) {
  const { t } = useTranslation()
  const showError = useAppStore((state) => state.showError)
  const showSuccess = useAppStore((state) => state.showSuccess)

  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [loadingResults, setLoadingResults] = useState(false)
  const [plans, setPlans] = useState<ScheduledTestPlan[]>([])
  const [results, setResults] = useState<ScheduledTestResult[]>([])
  const [expandedPlanId, setExpandedPlanId] = useState<number | null>(null)
  const resultsRequestId = useRef(0)
  const [expandedResultIds, setExpandedResultIds] = useState<Set<number>>(new Set())
  const [showAddForm, setShowAddForm] = useState(false)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deletingPlan, setDeletingPlan] = useState<ScheduledTestPlan | null>(null)
  const [editingPlanId, setEditingPlanId] = useState<number | null>(null)
  const [updating, setUpdating] = useState(false)
  const [editForm, setEditForm] = useState({
    model_id: '',
    cron_expression: '',
    max_results: '100',
    enabled: true,
    auto_recover: false
  })

  const [newPlan, setNewPlan] = useState({
    model_id: '',
    cron_expression: '',
    max_results: '100',
    enabled: true,
    auto_recover: false
  })

  const resetNewPlan = () => {
    setNewPlan({ model_id: '', cron_expression: '', max_results: '100', enabled: true, auto_recover: false })
  }

  const loadPlans = async () => {
    if (!accountId) return
    setLoading(true)
    try {
      setPlans(await adminAPI.scheduledTests.listByAccount(accountId))
    } catch (error) {
      showError((error as { message?: string })?.message || 'Failed to load plans')
    } finally {
      setLoading(false)
    }
  }

  // Load plans when dialog opens
  useEffect(() => {
    if (show && accountId) {
      void loadPlans()
    } else {
      setPlans([])
      setResults([])
      setExpandedPlanId(null)
      setExpandedResultIds(new Set())
      setShowAddForm(false)
      setShowDeleteConfirm(false)
    }
    // Account identity drives the plan list.
  }, [show, accountId])

  useEffect(() => () => void resultsRequestId.current++, [])

  const handleCreate = async () => {
    if (!accountId || !newPlan.model_id || !newPlan.cron_expression) return
    setCreating(true)
    try {
      const maxResults = Number(newPlan.max_results) || 100
      await adminAPI.scheduledTests.create({
        account_id: accountId,
        model_id: newPlan.model_id,
        cron_expression: newPlan.cron_expression,
        enabled: newPlan.enabled,
        max_results: maxResults,
        auto_recover: newPlan.auto_recover
      })
      showSuccess(t('admin.scheduledTests.createSuccess'))
      setShowAddForm(false)
      resetNewPlan()
      await loadPlans()
    } catch (error) {
      showError((error as { message?: string })?.message || 'Failed to create plan')
    } finally {
      setCreating(false)
    }
  }

  const handleToggleEnabled = async (plan: ScheduledTestPlan, enabled: boolean) => {
    try {
      const updated = await adminAPI.scheduledTests.update(plan.id, { enabled })
      setPlans((prev) => prev.map((p) => (p.id === plan.id ? updated : p)))
      showSuccess(t('admin.scheduledTests.updateSuccess'))
    } catch (error) {
      showError((error as { message?: string })?.message || 'Failed to update plan')
    }
  }

  const startEdit = (plan: ScheduledTestPlan) => {
    setEditingPlanId(plan.id)
    setEditForm({
      model_id: plan.model_id,
      cron_expression: plan.cron_expression,
      max_results: String(plan.max_results),
      enabled: plan.enabled,
      auto_recover: plan.auto_recover
    })
  }

  const cancelEdit = () => {
    setEditingPlanId(null)
  }

  const handleEdit = async () => {
    if (!editingPlanId || !editForm.model_id || !editForm.cron_expression) return
    setUpdating(true)
    try {
      const updated = await adminAPI.scheduledTests.update(editingPlanId, {
        model_id: editForm.model_id,
        cron_expression: editForm.cron_expression,
        max_results: Number(editForm.max_results) || 100,
        enabled: editForm.enabled,
        auto_recover: editForm.auto_recover
      })
      setPlans((prev) => prev.map((p) => (p.id === editingPlanId ? updated : p)))
      showSuccess(t('admin.scheduledTests.updateSuccess'))
      setEditingPlanId(null)
    } catch (error) {
      showError((error as { message?: string })?.message || 'Failed to update plan')
    } finally {
      setUpdating(false)
    }
  }

  const confirmDeletePlan = (plan: ScheduledTestPlan) => {
    setDeletingPlan(plan)
    setShowDeleteConfirm(true)
  }

  const handleDelete = async () => {
    if (!deletingPlan) return
    try {
      await adminAPI.scheduledTests.delete(deletingPlan.id)
      showSuccess(t('admin.scheduledTests.deleteSuccess'))
      setPlans((prev) => prev.filter((p) => p.id !== deletingPlan.id))
      if (expandedPlanId === deletingPlan.id) {
        setExpandedPlanId(null)
        setResults([])
      }
    } catch (error) {
      showError((error as { message?: string })?.message || 'Failed to delete plan')
    } finally {
      setShowDeleteConfirm(false)
      setDeletingPlan(null)
    }
  }

  const toggleExpand = async (planId: number) => {
    if (expandedPlanId === planId) {
      setExpandedPlanId(null)
      setResults([])
      setExpandedResultIds(new Set())
      return
    }

    setExpandedPlanId(planId)
    setExpandedResultIds(new Set())
    setLoadingResults(true)
    const requestId = resultsRequestId.current
    try {
      const data = await adminAPI.scheduledTests.listResults(planId, 20)
      if (requestId !== resultsRequestId.current) return
      setResults(data)
    } catch (error) {
      if (requestId !== resultsRequestId.current) return
      showError((error as { message?: string })?.message || 'Failed to load results')
      setResults([])
    } finally {
      if (requestId === resultsRequestId.current) setLoadingResults(false)
    }
  }

  const toggleResultDetail = (resultId: number) => {
    setExpandedResultIds((prev) => {
      const next = new Set(prev)
      if (next.has(resultId)) next.delete(resultId)
      else next.add(resultId)
      return next
    })
  }

  const cronTooltip = (
    <div className="space-y-1.5">
      <p className="font-medium">{t('admin.scheduledTests.cronTooltipTitle')}</p>
      <p>{t('admin.scheduledTests.cronTooltipMeaning')}</p>
      <p>{t('admin.scheduledTests.cronTooltipExampleEvery30Min')}</p>
      <p>{t('admin.scheduledTests.cronTooltipExampleHourly')}</p>
      <p>{t('admin.scheduledTests.cronTooltipExampleDaily')}</p>
      <p>{t('admin.scheduledTests.cronTooltipExampleWeekly')}</p>
      <p>{t('admin.scheduledTests.cronTooltipRange')}</p>
    </div>
  )

  const maxResultsTooltip = (
    <div className="space-y-1.5">
      <p className="font-medium">{t('admin.scheduledTests.maxResultsTooltipTitle')}</p>
      <p>{t('admin.scheduledTests.maxResultsTooltipMeaning')}</p>
      <p>{t('admin.scheduledTests.maxResultsTooltipBody')}</p>
      <p>{t('admin.scheduledTests.maxResultsTooltipExample')}</p>
      <p>{t('admin.scheduledTests.maxResultsTooltipRange')}</p>
    </div>
  )

  return (
    <Modal open={show} onClose={onClose} title={t('admin.scheduledTests.title')} size="xl">
      <div className="space-y-4">
        {/* Add Plan Button */}
        <div className="flex items-center justify-between">
          <p className="text-sm surface-muted">{t('admin.scheduledTests.title')}</p>
          <button onClick={() => setShowAddForm((prev) => !prev)} className="btn btn-primary flex items-center gap-1.5 text-sm">
            <Icon name="plus" size="sm" strokeWidth={2} />
            {t('admin.scheduledTests.addPlan')}
          </button>
        </div>

        {/* Add Plan Form */}
        {showAddForm && (
          <div className="rounded-xl border border-primary-200 bg-primary-50/50 p-4 dark:border-primary-800 dark:bg-primary-900/20">
            <div className="mb-3 text-sm font-medium surface-muted">{t('admin.scheduledTests.addPlan')}</div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.scheduledTests.model')}</label>
                <Select
                  value={newPlan.model_id}
                  options={modelOptions}
                  onValueChange={(value) => setNewPlan((prev) => ({ ...prev, model_id: String(value ?? '') }))}
                  placeholder={t('admin.scheduledTests.model')}
                  searchable={modelOptions.length > 5}
                />
              </div>
              <div>
                <label className="mb-1 flex items-center gap-1 text-xs font-medium surface-muted">
                  {t('admin.scheduledTests.cronExpression')}
                  <TriggerTooltip content={cronTooltip}>
                    <span className={HELP_TRIGGER_CLASS}>?</span>
                  </TriggerTooltip>
                </label>
                <Input
                  value={newPlan.cron_expression}
                  onValueChange={(value) => setNewPlan((prev) => ({ ...prev, cron_expression: value }))}
                  placeholder="*/30 * * * *"
                  hint={t('admin.scheduledTests.cronHelp')}
                />
              </div>
              <div>
                <label className="mb-1 flex items-center gap-1 text-xs font-medium surface-muted">
                  {t('admin.scheduledTests.maxResults')}
                  <TriggerTooltip content={maxResultsTooltip}>
                    <span className={HELP_TRIGGER_CLASS}>?</span>
                  </TriggerTooltip>
                </label>
                <Input
                  value={newPlan.max_results}
                  onValueChange={(value) => setNewPlan((prev) => ({ ...prev, max_results: value }))}
                  type="number"
                  placeholder="100"
                />
              </div>
              <div className="flex items-end">
                <label className="flex items-center gap-2 text-sm surface-muted">
                  <Toggle
                    checked={newPlan.enabled}
                    onCheckedChange={(checked) => setNewPlan((prev) => ({ ...prev, enabled: checked }))}
                  />
                  {t('admin.scheduledTests.enabled')}
                </label>
              </div>
              <div className="flex items-end">
                <div>
                  <label className="flex items-center gap-2 text-sm surface-muted">
                    <Toggle
                      checked={newPlan.auto_recover}
                      onCheckedChange={(checked) => setNewPlan((prev) => ({ ...prev, auto_recover: checked }))}
                    />
                    {t('admin.scheduledTests.autoRecover')}
                  </label>
                  <p className="mt-0.5 text-xs surface-muted">{t('admin.scheduledTests.autoRecoverHelp')}</p>
                </div>
              </div>
            </div>
            <div className="mt-3 flex justify-end gap-2">
              <button
                onClick={() => {
                  setShowAddForm(false)
                  resetNewPlan()
                }}
                className="rounded-lg px-3 py-1.5 text-sm font-medium transition-colors hover:surface-hover dark:hover:bg-dark-500 surface-control surface-muted"
              >
                {t('common.cancel')}
              </button>
              <button
                onClick={() => void handleCreate()}
                disabled={!newPlan.model_id || !newPlan.cron_expression || creating}
                className="flex items-center gap-1.5 rounded-lg bg-primary-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {creating && <Icon name="refresh" size="sm" className="animate-spin" strokeWidth={2} />}
                {t('common.save')}
              </button>
            </div>
          </div>
        )}

        {/* Loading State */}
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Icon name="refresh" size="md" className="animate-spin text-gray-400" strokeWidth={2} />
            <span className="ml-2 text-sm surface-muted">{t('common.loading')}...</span>
          </div>
        ) : plans.length === 0 ? (
          /* Empty State */
          <div className="rounded-xl border border-dashed border-gray-300 py-10 text-center dark:border-dark-600">
            <Icon name="calendar" size="lg" className="mx-auto mb-2 text-gray-400" strokeWidth={1.5} />
            <p className="text-sm surface-muted">{t('admin.scheduledTests.noPlans')}</p>
          </div>
        ) : (
          /* Plans List */
          <div className="space-y-3">
            {plans.map((plan) => (
              <div
                key={plan.id}
                className="rounded-xl border border-gray-200 surface-inset transition-all dark:border-dark-600"
              >
                {/* Plan Header */}
                <div className="flex cursor-pointer items-center justify-between px-4 py-3" onClick={() => void toggleExpand(plan.id)}>
                  <div className="flex flex-1 items-center gap-4">
                    {/* Model */}
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{plan.model_id}</div>
                      <div className="mt-0.5 font-mono text-xs surface-muted">{plan.cron_expression}</div>
                    </div>

                    {/* Enabled Toggle */}
                    <div className="flex items-center gap-1.5" onClick={(event) => event.stopPropagation()}>
                      <Toggle
                        checked={plan.enabled}
                        onCheckedChange={(val) => void handleToggleEnabled(plan, val)}
                      />
                      <span className="text-xs surface-muted">{plan.enabled ? t('admin.scheduledTests.enabled') : ''}</span>
                    </div>

                    {/* Auto Recover Badge */}
                    {plan.auto_recover && (
                      <span className="inline-flex items-center rounded-full bg-emerald-100/[0.15] px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-400">
                        {t('admin.scheduledTests.autoRecover')}
                      </span>
                    )}
                  </div>

                  <div className="flex items-center gap-3">
                    {/* Last Run */}
                    {plan.last_run_at && (
                      <div className="hidden text-right text-xs sm:block surface-muted">
                        <div>{t('admin.scheduledTests.lastRun')}</div>
                        <div>{formatDateTime(plan.last_run_at)}</div>
                      </div>
                    )}

                    {/* Next Run */}
                    {plan.next_run_at && (
                      <div className="hidden text-right text-xs sm:block surface-muted">
                        <div>{t('admin.scheduledTests.nextRun')}</div>
                        <div>{formatDateTime(plan.next_run_at)}</div>
                      </div>
                    )}

                    {/* Actions */}
                    <div className="flex items-center gap-1" onClick={(event) => event.stopPropagation()}>
                      <button
                        onClick={() => startEdit(plan)}
                        className="rounded-lg p-1.5 transition-colors hover:bg-blue-50/25 hover:text-blue-500 dark:hover:bg-blue-900/20 surface-control surface-muted"
                        title={t('admin.scheduledTests.editPlan')}
                      >
                        <Icon name="edit" size="sm" strokeWidth={2} />
                      </button>
                      <button
                        onClick={() => confirmDeletePlan(plan)}
                        className="rounded-lg p-1.5 transition-colors hover:bg-red-50/25 hover:text-red-500 dark:hover:bg-red-900/20 surface-control surface-muted"
                        title={t('admin.scheduledTests.deletePlan')}
                      >
                        <Icon name="trash" size="sm" strokeWidth={2} />
                      </button>
                    </div>

                    {/* Expand indicator */}
                    <Icon
                      name="chevronDown"
                      size="sm"
                      className={cn(
                        'text-gray-400 transition-transform duration-200',
                        expandedPlanId === plan.id ? 'rotate-180' : ''
                      )}
                    />
                  </div>
                </div>

                {/* Edit Form */}
                {editingPlanId === plan.id && (
                  <div
                    className="border-t border-blue-100 bg-blue-50/50 px-4 py-3 dark:border-blue-900 dark:bg-blue-900/10"
                    onClick={(event) => event.stopPropagation()}
                  >
                    <div className="mb-2 text-xs font-medium surface-muted">{t('admin.scheduledTests.editPlan')}</div>
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <div>
                        <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.scheduledTests.model')}</label>
                        <Select
                          value={editForm.model_id}
                          options={modelOptions}
                          onValueChange={(value) => setEditForm((prev) => ({ ...prev, model_id: String(value ?? '') }))}
                          placeholder={t('admin.scheduledTests.model')}
                          searchable={modelOptions.length > 5}
                        />
                      </div>
                      <div>
                        <label className="mb-1 flex items-center gap-1 text-xs font-medium surface-muted">
                          {t('admin.scheduledTests.cronExpression')}
                          <TriggerTooltip content={cronTooltip}>
                            <span className={HELP_TRIGGER_CLASS}>?</span>
                          </TriggerTooltip>
                        </label>
                        <Input
                          value={editForm.cron_expression}
                          onValueChange={(value) => setEditForm((prev) => ({ ...prev, cron_expression: value }))}
                          placeholder="*/30 * * * *"
                          hint={t('admin.scheduledTests.cronHelp')}
                        />
                      </div>
                      <div>
                        <label className="mb-1 flex items-center gap-1 text-xs font-medium surface-muted">
                          {t('admin.scheduledTests.maxResults')}
                          <TriggerTooltip content={maxResultsTooltip}>
                            <span className={HELP_TRIGGER_CLASS}>?</span>
                          </TriggerTooltip>
                        </label>
                        <Input
                          value={editForm.max_results}
                          onValueChange={(value) => setEditForm((prev) => ({ ...prev, max_results: value }))}
                          type="number"
                          placeholder="100"
                        />
                      </div>
                      <div className="flex items-end">
                        <label className="flex items-center gap-2 text-sm surface-muted">
                          <Toggle
                            checked={editForm.enabled}
                            onCheckedChange={(checked) => setEditForm((prev) => ({ ...prev, enabled: checked }))}
                          />
                          {t('admin.scheduledTests.enabled')}
                        </label>
                      </div>
                      <div className="flex items-end">
                        <div>
                          <label className="flex items-center gap-2 text-sm surface-muted">
                            <Toggle
                              checked={editForm.auto_recover}
                              onCheckedChange={(checked) => setEditForm((prev) => ({ ...prev, auto_recover: checked }))}
                            />
                            {t('admin.scheduledTests.autoRecover')}
                          </label>
                          <p className="mt-0.5 text-xs surface-muted">{t('admin.scheduledTests.autoRecoverHelp')}</p>
                        </div>
                      </div>
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                      <button
                        onClick={cancelEdit}
                        className="rounded-lg px-3 py-1.5 text-sm font-medium transition-colors hover:surface-hover dark:hover:bg-dark-500 surface-control surface-muted"
                      >
                        {t('common.cancel')}
                      </button>
                      <button
                        onClick={() => void handleEdit()}
                        disabled={!editForm.model_id || !editForm.cron_expression || updating}
                        className="flex items-center gap-1.5 rounded-lg bg-primary-500 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {updating && <Icon name="refresh" size="sm" className="animate-spin" strokeWidth={2} />}
                        {t('common.save')}
                      </button>
                    </div>
                  </div>
                )}

                {/* Expanded Results Section */}
                {expandedPlanId === plan.id && (
                  <div className="border-t border-gray-100 px-4 py-3 dark:border-dark-700">
                    <div className="mb-2 text-xs font-medium surface-muted">{t('admin.scheduledTests.results')}</div>

                    {/* Results Loading */}
                    {loadingResults ? (
                      <div className="flex items-center justify-center py-4">
                        <Icon name="refresh" size="sm" className="animate-spin text-gray-400" strokeWidth={2} />
                        <span className="ml-2 text-xs surface-muted">{t('common.loading')}...</span>
                      </div>
                    ) : results.length === 0 ? (
                      /* No Results */
                      <div className="py-4 text-center text-xs surface-muted">{t('admin.scheduledTests.noResults')}</div>
                    ) : (
                      /* Results List */
                      <div className="max-h-64 space-y-2 overflow-y-auto">
                        {results.map((result) => (
                          <div
                            key={result.id}
                            className="rounded-lg border border-gray-100 surface-inset p-3 dark:border-dark-700"
                          >
                            <div className="flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                {/* Status Badge */}
                                <span
                                  className={cn(
                                    'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
                                    result.status === 'success'
                                      ? 'bg-green-100/[0.15] text-green-700 dark:bg-green-500/20 dark:text-green-400'
                                      : result.status === 'running'
                                        ? 'bg-blue-100/[0.15] text-blue-700 dark:bg-blue-500/20 dark:text-blue-400'
                                        : 'bg-red-100/[0.15] text-red-700 dark:bg-red-500/20 dark:text-red-400'
                                  )}
                                >
                                  {result.status === 'success'
                                    ? t('admin.scheduledTests.success')
                                    : result.status === 'running'
                                      ? t('admin.scheduledTests.running')
                                      : t('admin.scheduledTests.failed')}
                                </span>

                                {/* Latency */}
                                {result.latency_ms > 0 && <span className="text-xs surface-muted">{result.latency_ms}ms</span>}
                              </div>

                              {/* Started At */}
                              <span className="text-xs surface-muted">{formatDateTime(result.started_at)}</span>
                            </div>

                            {/* Response / Error (collapsible) */}
                            {result.error_message ? (
                              <div className="mt-2">
                                <div
                                  className="cursor-pointer text-xs font-medium text-red-600 dark:text-red-400"
                                  onClick={() => toggleResultDetail(result.id)}
                                >
                                  {t('admin.scheduledTests.errorMessage')}
                                  <Icon
                                    name="chevronDown"
                                    size="sm"
                                    className={cn(
                                      'inline transition-transform duration-200',
                                      expandedResultIds.has(result.id) ? 'rotate-180' : ''
                                    )}
                                  />
                                </div>
                                {expandedResultIds.has(result.id) && (
                                  <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-red-50/[0.15] p-2 text-xs text-red-700 dark:bg-red-900/20 dark:text-red-300">
                                    {result.error_message}
                                  </pre>
                                )}
                              </div>
                            ) : result.response_text ? (
                              <div className="mt-2">
                                <div
                                  className="cursor-pointer text-xs font-medium surface-muted"
                                  onClick={() => toggleResultDetail(result.id)}
                                >
                                  {t('admin.scheduledTests.responseText')}
                                  <Icon
                                    name="chevronDown"
                                    size="sm"
                                    className={cn(
                                      'inline transition-transform duration-200',
                                      expandedResultIds.has(result.id) ? 'rotate-180' : ''
                                    )}
                                  />
                                </div>
                                {expandedResultIds.has(result.id) && (
                                  <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap rounded surface-inset p-2 text-xs surface-muted">
                                    {result.response_text}
                                  </pre>
                                )}
                              </div>
                            ) : null}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Delete Confirmation */}
      <ConfirmDialog
        open={showDeleteConfirm}
        title={t('admin.scheduledTests.deletePlan')}
        message={t('admin.scheduledTests.confirmDelete')}
        confirmText={t('common.delete')}
        cancelText={t('common.cancel')}
        danger
        onConfirm={() => void handleDelete()}
        onCancel={() => setShowDeleteConfirm(false)}
      />
    </Modal>
  )
}

export default ScheduledTestsPanel
