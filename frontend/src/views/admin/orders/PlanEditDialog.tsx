import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Modal, Select, toast, Toggle } from '@/components/ui'
import { adminPaymentAPI, type AdminPaymentConfig } from '@/api/admin/payment'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatPaymentAmount } from '../parts/paymentOrder'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
import { platformTextClass } from '@/utils/platformColors'
import { GroupBadge } from '../parts/GroupBadge'
import { Icon } from '@/views/admin/AccountIcon'

export interface PlanEditDialogProps {
  open: boolean
  plan: SubscriptionPlan | null
  groups: AdminGroup[]
  paymentConfig?: AdminPaymentConfig | null
  onClose: () => void
  onSaved: () => void
}

interface PlanForm {
  name: string
  group_id: number | null
  description: string
  price: number
  original_price: number
  currency: string
  validity_days: number
  validity_unit: string
  sort_order: number
  for_sale: boolean
}

export function PlanEditDialog({ open, plan, groups, paymentConfig = null, onClose, onSaved }: PlanEditDialogProps) {
  const { t } = useTranslation()

  const [saving, setSaving] = useState(false)
  const [planForm, setPlanForm] = useState<PlanForm>({
    name: '',
    group_id: null,
    description: '',
    price: 0,
    original_price: 0,
    currency: '',
    validity_days: 30,
    validity_unit: 'days',
    sort_order: 0,
    for_sale: true
  })
  const [planFeaturesText, setPlanFeaturesText] = useState('')

  const validityUnitOptions = [
    { value: 'days', label: t('payment.admin.days') },
    { value: 'weeks', label: t('payment.admin.weeks') },
    { value: 'months', label: t('payment.admin.months') }
  ]

  const groupOptions = useMemo(
    () =>
      groups
        .filter((group) => group.subscription_type === 'subscription')
        .map((group) => ({
          value: group.id,
          label: `${group.name} — ${group.platform} (${group.rate_multiplier}x)`,
          platform: group.platform
        })),
    [groups]
  )

  const selectedGroupInfo = useMemo(() => {
    if (!planForm.group_id) return null
    return groups.find((group) => group.id === planForm.group_id) || null
  }, [groups, planForm.group_id])

  const subscriptionCnyPreview = useMemo(() => {
    const price = Number(planForm.price) || 0
    const rate = Number(paymentConfig?.subscription_usd_to_cny_rate) || 0
    if (price <= 0 || rate <= 0) return null

    const roundCnyAmount = (value: number): number => Math.round(value * 100) / 100
    const ceilCnyAmount = (value: number): number => Math.ceil(value * 100) / 100

    const amount = roundCnyAmount(price * rate)
    const feeRate = Number(paymentConfig?.recharge_fee_rate) || 0
    const fee = feeRate > 0 ? ceilCnyAmount((amount * feeRate) / 100) : 0
    const total = feeRate > 0 ? roundCnyAmount(amount + fee) : amount

    return {
      amount: formatPaymentAmount(amount, 'CNY'),
      feeRate,
      total: formatPaymentAmount(total, 'CNY')
    }
  }, [planForm.price, paymentConfig])

  // Reset form when dialog opens
  useEffect(() => {
    if (!open) return
    if (plan) {
      setPlanForm({
        name: plan.name,
        group_id: plan.group_id,
        description: plan.description,
        price: plan.price,
        original_price: plan.original_price || 0,
        currency: plan.currency || '',
        validity_days: plan.validity_days,
        validity_unit: plan.validity_unit || 'days',
        sort_order: plan.sort_order || 0,
        for_sale: plan.for_sale
      })
      setPlanFeaturesText((plan.features || []).join('\n'))
    } else {
      setPlanForm({
        name: '',
        group_id: null,
        description: '',
        price: 0,
        original_price: 0,
        currency: '',
        validity_days: 30,
        validity_unit: 'days',
        sort_order: 0,
        for_sale: true
      })
      setPlanFeaturesText('')
    }
  }, [open, plan])

  /** Build request payload with snake_case keys matching backend JSON tags */
  function buildPlanPayload() {
    const features = planFeaturesText
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
      .join('\n')
    return {
      name: planForm.name,
      group_id: planForm.group_id,
      description: planForm.description,
      price: planForm.price,
      original_price: planForm.original_price || 0,
      currency: planForm.currency.trim().toUpperCase(),
      validity_days: planForm.validity_days,
      validity_unit: planForm.validity_unit,
      sort_order: planForm.sort_order,
      for_sale: planForm.for_sale,
      features
    }
  }

  async function handleSavePlan() {
    if (!planForm.group_id) {
      toast.error(t('payment.admin.groupRequired'))
      return
    }
    if (!planForm.price || planForm.price <= 0) {
      toast.error(t('payment.admin.priceRequired'))
      return
    }
    if (!planForm.validity_days || planForm.validity_days < 1) {
      toast.error(t('payment.admin.validityRequired'))
      return
    }
    setSaving(true)
    try {
      const data = buildPlanPayload()
      if (plan) await adminPaymentAPI.updatePlan(plan.id, data)
      else await adminPaymentAPI.createPlan(data)
      toast.success(t('common.saved'))
      onClose()
      onSaved()
    } catch (err: unknown) {
      toast.error(extractApiErrorMessage(err, t('common.error')))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={plan ? t('payment.admin.editPlan') : t('payment.admin.createPlan')}
      size="lg"
      footer={
        <div className="flex justify-end gap-3">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" loading={saving} onClick={() => void handleSavePlan()}>
            {saving ? t('common.saving') : t('common.save')}
          </Button>
        </div>
      }
    >
      <form
        id="plan-form"
        onSubmit={(event) => {
          event.preventDefault()
          void handleSavePlan()
        }}
        className="space-y-4"
      >
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="input-label">
              {t('payment.admin.planName')} <span className="text-red-500">*</span>
            </label>
            <input
              value={planForm.name}
              type="text"
              className="input"
              required
              onChange={(event) => setPlanForm({ ...planForm, name: event.target.value })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('payment.admin.group')} <span className="text-red-500">*</span>
            </label>
            <Select
              value={planForm.group_id}
              options={groupOptions}
              placeholder={t('payment.admin.selectGroup')}
              className="w-full"
              onValueChange={(next) => setPlanForm({ ...planForm, group_id: next == null ? null : Number(next) })}
              renderSelected={(option) => {
                const platform = option?.platform
                const label = String(option?.label ?? '')
                return platform ? (
                  <span className={platformTextClass(String(platform))}>{label}</span>
                ) : (
                  <span>{label || t('payment.admin.selectGroup')}</span>
                )
              }}
              renderOption={(option, state) => (
                <>
                  <span
                    className={`flex-1 truncate text-left ${option.platform ? platformTextClass(String(option.platform)) : ''}`}
                  >
                    {String(option.label ?? '')}
                  </span>
                  {state.selected && <Icon name="check" size="sm" className="text-primary-500" strokeWidth={2} />}
                </>
              )}
            />
          </div>
        </div>

        {selectedGroupInfo && (
          <div className="rounded-lg border border-gray-200 surface-inset p-3 dark:border-dark-600">
            <div className="mb-2 flex items-center gap-2">
              <GroupBadge
                name={selectedGroupInfo.name}
                platform={selectedGroupInfo.platform}
                rateMultiplier={selectedGroupInfo.rate_multiplier}
              />
            </div>
            <div className="grid grid-cols-2 gap-2 text-xs">
              <div>
                <span className="surface-muted">{t('payment.admin.dailyLimit')}:</span>{' '}
                <span className="ml-1 font-medium surface-muted">
                  {selectedGroupInfo.daily_limit_usd != null ? '$' + selectedGroupInfo.daily_limit_usd : t('payment.admin.unlimited')}
                </span>
              </div>
              <div>
                <span className="surface-muted">{t('payment.admin.weeklyLimit')}:</span>{' '}
                <span className="ml-1 font-medium surface-muted">
                  {selectedGroupInfo.weekly_limit_usd != null ? '$' + selectedGroupInfo.weekly_limit_usd : t('payment.admin.unlimited')}
                </span>
              </div>
              <div>
                <span className="surface-muted">{t('payment.admin.monthlyLimit')}:</span>{' '}
                <span className="ml-1 font-medium surface-muted">
                  {selectedGroupInfo.monthly_limit_usd != null ? '$' + selectedGroupInfo.monthly_limit_usd : t('payment.admin.unlimited')}
                </span>
              </div>
              <div>
                <span className="surface-muted">{t('payment.admin.fiveHourLimit')}:</span>{' '}
                <span className="ml-1 font-medium surface-muted">
                  {selectedGroupInfo.five_hour_limit_usd != null
                    ? '$' + selectedGroupInfo.five_hour_limit_usd
                    : t('payment.admin.unlimited')}
                </span>
              </div>
            </div>
          </div>
        )}

        <div>
          <label className="input-label">
            {t('payment.admin.planDescription')} <span className="text-red-500">*</span>
          </label>
          <textarea
            value={planForm.description}
            rows={2}
            className="input"
            required
            onChange={(event) => setPlanForm({ ...planForm, description: event.target.value })}
          />
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="input-label">
              {t('payment.admin.price')} <span className="text-red-500">*</span>
            </label>
            <input
              value={planForm.price}
              type="number"
              step="0.01"
              min="0.01"
              className="input"
              required
              onChange={(event) => setPlanForm({ ...planForm, price: Number(event.target.value) })}
            />
            {subscriptionCnyPreview && (
              <p className="mt-1 text-xs font-medium text-primary-600 dark:text-primary-400">
                {t('payment.admin.subscriptionCnyPayPreview', { amount: subscriptionCnyPreview.amount })}
                {subscriptionCnyPreview.feeRate > 0 && (
                  <span>
                    {t('payment.admin.subscriptionCnyPayPreviewWithFee', {
                      feeRate: subscriptionCnyPreview.feeRate,
                      total: subscriptionCnyPreview.total
                    })}
                  </span>
                )}
              </p>
            )}
          </div>
          <div>
            <label className="input-label">{t('payment.admin.originalPrice')}</label>
            <input
              value={planForm.original_price}
              type="number"
              step="0.01"
              min="0"
              className="input"
              onChange={(event) => setPlanForm({ ...planForm, original_price: Number(event.target.value) })}
            />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="input-label">
              {t('payment.admin.validity')} <span className="text-red-500">*</span>
            </label>
            <input
              value={planForm.validity_days}
              type="number"
              min="1"
              className="input"
              required
              onChange={(event) => setPlanForm({ ...planForm, validity_days: Number(event.target.value) })}
            />
          </div>
          <div>
            <label className="input-label">
              {t('payment.admin.validityUnit')} <span className="text-red-500">*</span>
            </label>
            <Select
              value={planForm.validity_unit}
              options={validityUnitOptions}
              searchable={false}
              onValueChange={(next) => setPlanForm({ ...planForm, validity_unit: String(next ?? 'days') })}
            />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label className="input-label">{t('payment.admin.sortOrder')}</label>
            <input
              value={planForm.sort_order}
              type="number"
              min="0"
              className="input"
              onChange={(event) => setPlanForm({ ...planForm, sort_order: Number(event.target.value) })}
            />
          </div>
          <div>
            <label className="input-label">{t('payment.admin.currency')}</label>
            <input
              value={planForm.currency}
              type="text"
              maxLength={3}
              className="input uppercase"
              placeholder={t('payment.admin.currencyPlaceholder')}
              onChange={(event) => setPlanForm({ ...planForm, currency: event.target.value })}
            />
            <p className="mt-1 text-xs surface-muted">{t('payment.admin.currencyHint')}</p>
          </div>
        </div>
        <div>
          <label className="input-label">{t('payment.admin.features')}</label>
          <textarea
            value={planFeaturesText}
            rows={3}
            className="input"
            placeholder={t('payment.admin.featuresPlaceholder')}
            onChange={(event) => setPlanFeaturesText(event.target.value)}
          />
          <p className="mt-1 text-xs surface-muted">{t('payment.admin.featuresHint')}</p>
        </div>
        <div className="flex items-center gap-3">
          <label className="text-sm surface-muted">{t('payment.admin.forSale')}</label>
          <Toggle checked={planForm.for_sale} onCheckedChange={(next) => setPlanForm({ ...planForm, for_sale: next })} />
        </div>
      </form>
    </Modal>
  )
}

export default PlanEditDialog
