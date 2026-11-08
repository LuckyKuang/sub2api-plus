import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Select } from '@/components/ui'
import type {
  AdminGroup,
  AnnouncementCondition,
  AnnouncementConditionGroup,
  AnnouncementConditionType,
  AnnouncementOperator,
  AnnouncementTargeting
} from '@/types'
import { Icon } from '@/views/admin/AccountIcon'
import { GroupSelector } from './GroupSelector'

export interface AnnouncementTargetingEditorProps {
  value: AnnouncementTargeting
  groups: AdminGroup[]
  onValueChange: (value: AnnouncementTargeting) => void
}

type TargetingDraft = {
  any_of: AnnouncementConditionGroup[]
}

function defaultSubscriptionCondition(): AnnouncementCondition {
  return { type: 'subscription' as AnnouncementConditionType, operator: 'in' as AnnouncementOperator, group_ids: [] }
}

function defaultBalanceCondition(): AnnouncementCondition {
  return { type: 'balance' as AnnouncementConditionType, operator: 'gte' as AnnouncementOperator, value: 0 }
}

/** Targeting editor ported from the Vue AnnouncementTargetingEditor. */
export function AnnouncementTargetingEditor({ value, groups, onValueChange }: AnnouncementTargetingEditorProps) {
  const { t } = useTranslation()
  const anyOf = useMemo(() => value?.any_of ?? [], [value])
  const mode: 'all' | 'custom' = anyOf.length === 0 ? 'all' : 'custom'

  // Group selection is mirrored through a parallel map because GroupSelector is numeric list.
  const [subscriptionSelections, setSubscriptionSelections] = useState<Record<number, Record<number, number[]>>>({})

  const conditionTypeOptions = [
    { value: 'subscription', label: t('admin.announcements.form.conditionSubscription') },
    { value: 'balance', label: t('admin.announcements.form.conditionBalance') }
  ]

  const balanceOperatorOptions = [
    { value: 'gt', label: t('admin.announcements.operators.gt') },
    { value: 'gte', label: t('admin.announcements.operators.gte') },
    { value: 'lt', label: t('admin.announcements.operators.lt') },
    { value: 'lte', label: t('admin.announcements.operators.lte') },
    { value: 'eq', label: t('admin.announcements.operators.eq') }
  ]

  function setMode(next: 'all' | 'custom') {
    if (next === 'all') {
      onValueChange({ any_of: [] })
      return
    }
    if (anyOf.length === 0) {
      onValueChange({ any_of: [{ all_of: [defaultSubscriptionCondition()] }] })
    }
  }

  function updateTargeting(mutator: (draft: TargetingDraft) => void) {
    const draft: TargetingDraft = JSON.parse(JSON.stringify(value ?? { any_of: [] }))
    if (!draft.any_of) draft.any_of = []
    mutator(draft)
    onValueChange(draft)
  }

  // Sync from modelValue to subscriptionSelections (one-way: model -> local state)
  useEffect(() => {
    const groupsOfValue = value?.any_of ?? []
    setSubscriptionSelections((previous) => {
      const next: Record<number, Record<number, number[]>> = { ...previous }
      let changed = false
      for (let gi = 0; gi < groupsOfValue.length; gi++) {
        const allOf = groupsOfValue[gi]?.all_of ?? []
        for (let ci = 0; ci < allOf.length; ci++) {
          const condition = allOf[ci]
          if (condition?.type === 'subscription') {
            if (!next[gi]) next[gi] = {}
            const newIds = (condition.group_ids ?? []).slice()
            const currentIds = next[gi]?.[ci] ?? []
            if (JSON.stringify([...newIds].sort()) !== JSON.stringify([...currentIds].sort())) {
              next[gi][ci] = newIds
              changed = true
            }
          }
        }
      }
      return changed ? next : previous
    })
  }, [value])

  // Sync from subscriptionSelections to modelValue (one-way: local state -> model)
  useEffect(() => {
    if (Object.keys(subscriptionSelections).length === 0) return
    const timer = setTimeout(() => {
      const newTargeting: TargetingDraft = JSON.parse(JSON.stringify(value ?? { any_of: [] }))
      if (!newTargeting.any_of) newTargeting.any_of = []
      const groupsOfValue = newTargeting.any_of ?? []
      for (let gi = 0; gi < groupsOfValue.length; gi++) {
        const allOf = groupsOfValue[gi]?.all_of ?? []
        for (let ci = 0; ci < allOf.length; ci++) {
          const condition = allOf[ci]
          if (condition?.type === 'subscription') {
            condition.operator = 'in' as AnnouncementOperator
            condition.group_ids = (subscriptionSelections[gi]?.[ci] ?? []).slice()
          }
        }
      }
      if (JSON.stringify(value) !== JSON.stringify(newTargeting)) {
        onValueChange(newTargeting)
      }
    }, 0)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subscriptionSelections])

  const validationError = useMemo(() => {
    if (mode !== 'custom') return ''
    const groupsOfValue = anyOf
    if (groupsOfValue.length === 0) return t('admin.announcements.form.addOrGroup')
    if (groupsOfValue.length > 50) return 'any_of > 50'
    for (const group of groupsOfValue) {
      const allOf = group?.all_of ?? []
      if (allOf.length === 0) return t('admin.announcements.form.addAndCondition')
      if (allOf.length > 50) return 'all_of > 50'
      for (const condition of allOf) {
        if (condition.type === 'subscription') {
          if (!condition.group_ids || condition.group_ids.length === 0) {
            return t('admin.announcements.form.selectPackages')
          }
        }
      }
    }
    return ''
  }, [mode, anyOf, t])

  return (
    <div className="rounded-2xl border border-gray-200 surface-inset p-4 dark:border-dark-700">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="text-sm font-medium text-gray-900 dark:text-white">
            {t('admin.announcements.form.targetingMode')}
          </div>
          <div className="mt-1 text-xs surface-muted">
            {mode === 'all' ? t('admin.announcements.form.targetingAll') : t('admin.announcements.form.targetingCustom')}
          </div>
        </div>

        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 text-sm surface-muted">
            <input
              type="radio"
              name="announcement-targeting-mode"
              value="all"
              checked={mode === 'all'}
              className="h-4 w-4"
              onChange={() => setMode('all')}
            />
            {t('admin.announcements.form.targetingAll')}
          </label>
          <label className="flex items-center gap-2 text-sm surface-muted">
            <input
              type="radio"
              name="announcement-targeting-mode"
              value="custom"
              checked={mode === 'custom'}
              className="h-4 w-4"
              onChange={() => setMode('custom')}
            />
            {t('admin.announcements.form.targetingCustom')}
          </label>
        </div>
      </div>

      {mode === 'custom' && (
        <div className="mt-4 space-y-4">
          <div className="flex items-center justify-between">
            <div className="text-sm font-medium text-gray-900 dark:text-white">
              OR
              <span className="ml-1 text-xs font-normal surface-muted">({anyOf.length}/50)</span>
            </div>
            <Button
              variant="secondary"
              disabled={anyOf.length >= 50}
              onClick={() =>
                updateTargeting((draft) => {
                  if (draft.any_of.length >= 50) return
                  draft.any_of.push({ all_of: [defaultSubscriptionCondition()] })
                })
              }
            >
              <Icon name="plus" size="sm" className="mr-1" />
              {t('admin.announcements.form.addOrGroup')}
            </Button>
          </div>

          {anyOf.length === 0 && (
            <div className="rounded-xl border border-dashed border-gray-300 p-4 text-sm dark:border-dark-600 surface-muted">
              {t('admin.announcements.form.targetingCustom')}: {t('admin.announcements.form.addOrGroup')}
            </div>
          )}

          {anyOf.map((group, groupIndex) => (
            <div key={groupIndex} className="glass-surface border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-gray-900 dark:text-white">
                    {t('admin.announcements.form.targetingCustom')} #{groupIndex + 1}
                    <span className="ml-2 text-xs font-normal surface-muted">
                      AND ({(group.all_of?.length || 0)}/50)
                    </span>
                  </div>
                  <div className="mt-1 text-xs surface-muted">{t('admin.announcements.form.addAndCondition')}</div>
                </div>

                <Button
                  variant="secondary"
                  onClick={() =>
                    updateTargeting((draft) => {
                      draft.any_of.splice(groupIndex, 1)
                    })
                  }
                >
                  <Icon name="trash" size="sm" className="mr-1" />
                  {t('common.delete')}
                </Button>
              </div>

              <div className="mt-4 space-y-3">
                {(group.all_of || []).map((cond, condIndex) => (
                  <div
                    key={condIndex}
                    className="rounded-xl border border-gray-200 surface-inset p-3 dark:border-dark-700"
                  >
                    <div className="flex flex-col gap-3 md:flex-row md:items-end">
                      <div className="w-full md:w-52">
                        <label className="input-label">{t('admin.announcements.form.conditionType')}</label>
                        <Select
                          value={cond.type}
                          options={conditionTypeOptions}
                          onValueChange={(next) =>
                            updateTargeting((draft) => {
                              const targetGroup = draft.any_of[groupIndex]
                              if (!targetGroup?.all_of) return
                              targetGroup.all_of[condIndex] =
                                next === 'subscription' ? defaultSubscriptionCondition() : defaultBalanceCondition()
                            })
                          }
                        />
                      </div>

                      {cond.type === 'subscription' ? (
                        <div className="flex-1">
                          <label className="input-label">{t('admin.announcements.form.selectPackages')}</label>
                          <GroupSelector
                            value={subscriptionSelections[groupIndex]?.[condIndex] ?? []}
                            groups={groups}
                            onValueChange={(next) =>
                              setSubscriptionSelections((previous) => ({
                                ...previous,
                                [groupIndex]: { ...(previous[groupIndex] ?? {}), [condIndex]: next }
                              }))
                            }
                          />
                        </div>
                      ) : (
                        <div className="flex flex-1 flex-col gap-3 sm:flex-row">
                          <div className="w-full sm:w-44">
                            <label className="input-label">{t('admin.announcements.form.operator')}</label>
                            <Select
                              value={cond.operator}
                              options={balanceOperatorOptions}
                              onValueChange={(next) =>
                                updateTargeting((draft) => {
                                  const targetGroup = draft.any_of[groupIndex]
                                  if (!targetGroup?.all_of) return
                                  const target = targetGroup.all_of[condIndex]
                                  if (!target) return
                                  target.operator = next as AnnouncementOperator
                                })
                              }
                            />
                          </div>
                          <div className="w-full sm:flex-1">
                            <label className="input-label">{t('admin.announcements.form.balanceValue')}</label>
                            <input
                              value={String(cond.value ?? '')}
                              type="number"
                              step="any"
                              className="input"
                              onChange={(event) => {
                                const raw = event.target.value
                                const parsed = raw === '' ? 0 : Number(raw)
                                updateTargeting((draft) => {
                                  const targetGroup = draft.any_of[groupIndex]
                                  if (!targetGroup?.all_of) return
                                  const target = targetGroup.all_of[condIndex]
                                  if (!target) return
                                  target.value = Number.isFinite(parsed) ? parsed : 0
                                })
                              }}
                            />
                          </div>
                        </div>
                      )}

                      <div className="flex justify-end">
                        <Button
                          variant="secondary"
                          onClick={() =>
                            updateTargeting((draft) => {
                              const targetGroup = draft.any_of[groupIndex]
                              if (!targetGroup?.all_of) return
                              targetGroup.all_of.splice(condIndex, 1)
                            })
                          }
                        >
                          <Icon name="trash" size="sm" className="mr-1" />
                          {t('common.delete')}
                        </Button>
                      </div>
                    </div>
                  </div>
                ))}

                <div className="flex justify-end">
                  <Button
                    variant="secondary"
                    disabled={(group.all_of?.length || 0) >= 50}
                    onClick={() =>
                      updateTargeting((draft) => {
                        const targetGroup = draft.any_of[groupIndex]
                        if (!targetGroup.all_of) targetGroup.all_of = []
                        if (targetGroup.all_of.length >= 50) return
                        targetGroup.all_of.push(defaultSubscriptionCondition())
                      })
                    }
                  >
                    <Icon name="plus" size="sm" className="mr-1" />
                    {t('admin.announcements.form.addAndCondition')}
                  </Button>
                </div>
              </div>
            </div>
          ))}

          {validationError && (
            <div className="rounded-xl border border-red-200 bg-red-50/[0.15] p-3 text-sm text-red-700 dark:border-red-900/30 dark:bg-red-900/10 dark:text-red-300">
              {validationError}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default AnnouncementTargetingEditor
