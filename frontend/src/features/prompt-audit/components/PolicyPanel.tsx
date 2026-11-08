import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { PromptAuditDraft, PromptAuditGroup } from '../types'
import { cloneData, SCANNER_CATALOG } from '../viewModel'

export interface PolicyPanelProps {
  draft: PromptAuditDraft
  groups: PromptAuditGroup[]
  onUpdateDraft: (value: PromptAuditDraft) => void
}

export function PolicyPanel({ draft, groups, onUpdateDraft }: PolicyPanelProps) {
  const { t } = useTranslation()
  const [groupSearch, setGroupSearch] = useState('')

  const filteredGroups = useMemo(() => {
    const query = groupSearch.trim().toLowerCase()
    if (!query) return groups
    return groups.filter((group) => `${group.name} ${group.id} ${group.platform}`.toLowerCase().includes(query))
  }, [groupSearch, groups])

  const knownGroupIds = useMemo(() => new Set(groups.map((group) => group.id)), [groups])
  const missingGroupIds = draft.group_ids.filter((id) => !knownGroupIds.has(id))

  function patch(value: Partial<PromptAuditDraft>) {
    onUpdateDraft({ ...cloneData(draft), ...value })
  }

  function toggleGroup(id: number) {
    const selected = new Set(draft.group_ids)
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
    patch({ group_ids: [...selected].sort((a, b) => a - b) })
  }

  function toggleScanner(id: string) {
    const selected = new Set(draft.scanners)
    if (selected.has(id)) selected.delete(id)
    else selected.add(id)
    patch({ scanners: SCANNER_CATALOG.map((item) => item.id).filter((item) => selected.has(item)) })
  }

  function scannerLabel(id: string): string {
    return t(`admin.promptAudit.scanners.${id}`)
  }

  return (
    <section aria-labelledby="prompt-policy-title" className="py-6">
      <div>
        <h2 id="prompt-policy-title" className="text-base font-semibold text-gray-950 dark:text-white">
          {t('admin.promptAudit.policy.title')}
        </h2>
        <p className="mt-1 text-sm surface-muted">{t('admin.promptAudit.policy.description')}</p>
      </div>

      <div className="mt-5 grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(260px,0.45fr)]">
        <div className="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-700/60 dark:surface-inset sm:p-5">
          <fieldset className="min-w-0">
            <legend className="text-sm font-medium text-gray-900 dark:text-white">
              {t('admin.promptAudit.policy.scope')}
            </legend>
            <div className="mt-3 flex flex-wrap gap-5 text-sm surface-muted">
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name="prompt-audit-scope"
                  checked={draft.all_groups}
                  onChange={() => patch({ all_groups: true, group_ids: [] })}
                />
                {t('admin.promptAudit.policy.allGroups')}
              </label>
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name="prompt-audit-scope"
                  checked={!draft.all_groups}
                  onChange={() => patch({ all_groups: false })}
                />
                {t('admin.promptAudit.policy.selectedGroups')}
              </label>
            </div>
          </fieldset>

          {!draft.all_groups && (
            <div className="mt-4">
              <label className="block text-sm surface-muted">
                <span>{t('admin.promptAudit.policy.searchGroups')}</span>
                <input
                  value={groupSearch}
                  type="search"
                  className="input mt-1.5 w-full"
                  aria-label={t('admin.promptAudit.policy.searchGroups')}
                  onChange={(event) => setGroupSearch(event.target.value)}
                />
              </label>
              <div className="mt-3 max-h-52 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-700">
                {filteredGroups.map((group) => (
                  <label
                    key={group.id}
                    className="flex min-w-0 cursor-pointer flex-col items-start gap-1 rounded-md px-2 py-2 text-sm hover:surface-hover sm:flex-row sm:items-center sm:justify-between sm:gap-3"
                  >
                    <span className="flex min-w-0 items-start gap-2 text-gray-800 dark:text-dark-100">
                      <input
                        type="checkbox"
                        className="shrink-0"
                        checked={draft.group_ids.includes(group.id)}
                        onChange={() => toggleGroup(group.id)}
                      />
                      <span className="min-w-0 [overflow-wrap:anywhere]">{group.name}</span>
                    </span>
                    <span className="text-xs surface-muted">
                      {group.platform} · {group.status}
                    </span>
                  </label>
                ))}
                {filteredGroups.length === 0 && (
                  <p className="px-2 py-4 text-center text-sm surface-muted">{t('admin.promptAudit.policy.noGroups')}</p>
                )}
              </div>
              {missingGroupIds.length > 0 && (
                <div className="mt-3 rounded-lg bg-amber-50/[0.15] px-3 py-2 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
                  {t('admin.promptAudit.policy.missingGroups')}: {missingGroupIds.join(', ')}
                </div>
              )}
              <p className="mt-2 text-xs surface-muted">
                {t('admin.promptAudit.policy.selectedCount', { count: draft.group_ids.length })}
              </p>
            </div>
          )}

          <fieldset className="mt-5 min-w-0 border-t border-gray-100 pt-5 dark:border-dark-800">
            <legend className="text-sm font-medium text-gray-900 dark:text-white">
              {t('admin.promptAudit.policy.scanners')}
            </legend>
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {SCANNER_CATALOG.map((scanner) => (
                <label
                  key={scanner.id}
                  className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:surface-hover surface-muted"
                >
                  <input
                    type="checkbox"
                    checked={draft.scanners.includes(scanner.id)}
                    aria-label={scannerLabel(scanner.id)}
                    onChange={() => toggleScanner(scanner.id)}
                  />
                  <span>{scannerLabel(scanner.id)}</span>
                </label>
              ))}
            </div>
          </fieldset>
        </div>

        <div className="min-w-0 space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700/60 dark:surface-inset sm:p-5">
          <label className="block text-sm surface-muted">
            <span>{t('admin.promptAudit.policy.workerCount')}</span>
            <input
              value={draft.worker_count}
              type="number"
              min={1}
              max={32}
              className="input mt-1.5 w-full"
              aria-label={t('admin.promptAudit.policy.workerCount')}
              onChange={(event) => patch({ worker_count: Number(event.target.value) })}
            />
          </label>
          <label className="block text-sm surface-muted">
            <span>{t('admin.promptAudit.policy.queueCapacity')}</span>
            <input
              value={draft.queue_capacity}
              type="number"
              min={1}
              max={100000}
              className="input mt-1.5 w-full"
              aria-label={t('admin.promptAudit.policy.queueCapacity')}
              onChange={(event) => patch({ queue_capacity: Number(event.target.value) })}
            />
          </label>
          <div className="rounded-lg surface-inset px-4 py-3 text-sm surface-muted">
            <p className="font-medium text-gray-800 dark:text-dark-100">{t('admin.promptAudit.policy.strategy')}</p>
            <p className="mt-1">priority · {t('admin.promptAudit.policy.strategyHint')}</p>
          </div>
        </div>
      </div>
    </section>
  )
}

export default PolicyPanel
