import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Toggle, toast } from '@/components/ui'
import { adminPaymentAPI, type AdminPaymentConfig } from '@/api/admin/payment'
import { extractI18nErrorMessage } from '@/utils/apiError'
import adminAPI from '@/api/admin'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
import { AdminDataTable, type Column } from '@/views/admin/AdminDataTable'
import { Icon } from '@/views/admin/AccountIcon'
import { ConfirmDialog } from '@/views/admin/shared'
import { currencySymbol } from '../parts/paymentOrder'
import { GroupBadge } from '../parts/GroupBadge'
import { PlanEditDialog } from './PlanEditDialog'
import { platformTextClass } from '@/utils/platformColors'

type RawPlan = Omit<SubscriptionPlan, 'features'> & { features: string | string[] }

function planCurrencySymbol(currency?: string): string {
  return currencySymbol(currency || 'USD')
}

function planValidityLabel(plan: Pick<SubscriptionPlan, 'validity_days' | 'validity_unit'>, t: (key: string) => string): string {
  const unit = String(plan.validity_unit || 'day').trim().toLowerCase()
  switch (unit) {
    case 'month':
    case 'months':
      return `${plan.validity_days}${t('payment.months')}`
    case 'week':
    case 'weeks':
      return `${plan.validity_days}${t('payment.weeks')}`
    default:
      return `${plan.validity_days}${t('payment.days')}`
  }
}

export default function AdminPaymentPlansView() {
  const { t } = useTranslation()

  const [groups, setGroups] = useState<AdminGroup[]>([])
  const [paymentConfig, setPaymentConfig] = useState<AdminPaymentConfig | null>(null)

  const [plansLoading, setPlansLoading] = useState(false)
  const [plans, setPlans] = useState<SubscriptionPlan[]>([])
  const [showPlanDialog, setShowPlanDialog] = useState(false)
  const [showDeletePlanDialog, setShowDeletePlanDialog] = useState(false)
  const [editingPlan, setEditingPlan] = useState<SubscriptionPlan | null>(null)
  const [deletingPlanId, setDeletingPlanId] = useState<number | null>(null)

  const planColumns: Column[] = [
    { key: 'id', label: 'ID' },
    { key: 'name', label: t('payment.admin.planName') },
    { key: 'group_id', label: t('payment.admin.group') },
    { key: 'price', label: t('payment.admin.price') },
    { key: 'validity_days', label: t('payment.admin.validity') },
    { key: 'for_sale', label: t('payment.admin.forSale') },
    { key: 'sort_order', label: t('payment.admin.sortOrder') },
    { key: 'actions', label: t('common.actions') }
  ]

  async function loadGroups() {
    try {
      setGroups(await adminAPI.groups.getAll())
    } catch {
      /* ignore */
    }
  }

  async function loadPaymentConfig() {
    try {
      const res = await adminPaymentAPI.getConfig()
      setPaymentConfig(res.data)
    } catch {
      /* preview only */
    }
  }

  async function loadPlans() {
    setPlansLoading(true)
    try {
      const res = await adminPaymentAPI.getPlans()
      // Backend returns features as newline-separated string; parse to array
      setPlans(
        (res.data || []).map((plan: RawPlan) => ({
          ...plan,
          features:
            typeof plan.features === 'string'
              ? plan.features
                  .split('\n')
                  .map((line) => line.trim())
                  .filter(Boolean)
              : plan.features || []
        }))
      )
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    } finally {
      setPlansLoading(false)
    }
  }

  useEffect(() => {
    void loadGroups()
    void loadPaymentConfig()
    void loadPlans()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const getGroup = (id: number): AdminGroup | undefined => groups.find((group) => group.id === id)

  const isGroupMissing = (id: number): boolean => id > 0 && !groups.find((group) => group.id === id)

  const getPlanNameClass = (groupId: number): string => {
    const group = getGroup(groupId)
    return group ? platformTextClass(group.platform) : 'text-gray-900 dark:text-white'
  }

  function openPlanEdit(plan: SubscriptionPlan | null) {
    setEditingPlan(plan)
    setShowPlanDialog(true)
  }

  /** Quick toggle for_sale from the list */
  async function toggleForSale(plan: SubscriptionPlan) {
    try {
      await adminPaymentAPI.updatePlan(plan.id, { for_sale: !plan.for_sale })
      setPlans((previous) => previous.map((entry) => (entry.id === plan.id ? { ...entry, for_sale: !entry.for_sale } : entry)))
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    }
  }

  function confirmDeletePlan(plan: SubscriptionPlan) {
    setDeletingPlanId(plan.id)
    setShowDeletePlanDialog(true)
  }

  async function handleDeletePlan() {
    if (!deletingPlanId) return
    try {
      await adminPaymentAPI.deletePlan(deletingPlanId)
      toast.success(t('common.deleted'))
      setShowDeletePlanDialog(false)
      void loadPlans()
    } catch (err: unknown) {
      toast.error(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
    }
  }

  return (
    <div className="workspace-page">
      <div className="card workspace-fixed flex flex-wrap items-center justify-end gap-2 px-4 py-3" data-test="plans-toolbar">
        <Button
          variant="secondary"
          onClick={() => void loadPlans()}
          disabled={plansLoading}
          title={t('common.refresh')}
          aria-label={t('common.refresh')}
        >
          <Icon name="refresh" size="md" className={plansLoading ? 'animate-spin' : ''} />
        </Button>
        <Button variant="primary" onClick={() => openPlanEdit(null)}>
          {t('payment.admin.createPlan')}
        </Button>
      </div>

      <div className="card workspace-table-card workspace-shell" data-test="plans-panel">
        <AdminDataTable
          className="workspace-table-viewport"
          columns={planColumns}
          data={plans}
          loading={plansLoading}
          renderCell={(key, row, value) => {
            const plan = row as SubscriptionPlan
            if (key === 'name') {
              return (
                <span className={`text-sm font-medium ${getPlanNameClass(plan.group_id)}`}>{String(value ?? '')}</span>
              )
            }
            if (key === 'group_id') {
              const id = Number(value ?? 0)
              if (isGroupMissing(id)) {
                return (
                  <span className="text-sm">
                    <span className="surface-muted">#{id}</span>
                    <span className="ml-1 badge badge-danger">{t('payment.admin.groupMissing')}</span>
                  </span>
                )
              }
              const group = getGroup(id)
              if (group) {
                return (
                  <GroupBadge
                    name={group.name}
                    platform={group.platform}
                    rateMultiplier={group.rate_multiplier}
                  />
                )
              }
              return <span className="text-sm surface-muted">-</span>
            }
            if (key === 'price') {
              return (
                <div className="text-sm">
                  <span className="font-medium text-gray-900 dark:text-white">
                    {planCurrencySymbol(plan.currency)}
                    {(Number(value ?? 0)).toFixed(2)}
                  </span>
                  {plan.currency && <span className="ml-1 text-xs surface-muted">{plan.currency}</span>}
                  {plan.original_price && (
                    <span className="ml-1 text-xs line-through surface-muted">
                      {planCurrencySymbol(plan.currency)}
                      {plan.original_price.toFixed(2)}
                    </span>
                  )}
                </div>
              )
            }
            if (key === 'validity_days') {
              return <span className="text-sm">{planValidityLabel(plan, t)}</span>
            }
            if (key === 'for_sale') {
              return (
                <Toggle
                  checked={Boolean(value)}
                  onCheckedChange={() => void toggleForSale(plan)}
                />
              )
            }
            if (key === 'actions') {
              return (
                <div className="table-actions">
                  <button type="button" className="table-action" onClick={() => openPlanEdit(plan)}>
                    <Icon name="edit" size="sm" />
                    <span className="text-xs">{t('common.edit')}</span>
                  </button>
                  <button
                    type="button"
                    className="table-action table-action-danger"
                    onClick={() => confirmDeletePlan(plan)}
                  >
                    <Icon name="trash" size="sm" />
                    <span className="text-xs">{t('common.delete')}</span>
                  </button>
                </div>
              )
            }
            return <span>{value == null ? '' : String(value)}</span>
          }}
        />
      </div>

      <PlanEditDialog
        open={showPlanDialog}
        plan={editingPlan}
        groups={groups}
        paymentConfig={paymentConfig}
        onClose={() => setShowPlanDialog(false)}
        onSaved={() => void loadPlans()}
      />

      <ConfirmDialog
        open={showDeletePlanDialog}
        title={t('payment.admin.deletePlan')}
        message={t('payment.admin.deletePlanConfirm')}
        confirmText={t('common.delete')}
        danger
        onConfirm={() => void handleDeletePlan()}
        onCancel={() => setShowDeletePlanDialog(false)}
      />
    </div>
  )
}
