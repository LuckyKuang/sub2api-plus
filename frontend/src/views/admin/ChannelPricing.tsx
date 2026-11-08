import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import type { BillingMode } from '@/api/admin/channels'
import { Button, Input, Select, Toggle } from '@/components/ui'
import {
  COMMON_TIMEZONES,
  formatTimezoneOffset,
  getPlatformTagClass,
  isValidTimePricingMultiplier,
  type IntervalFormEntry,
  type TimePricingFormEntry
} from './channelTypes'

// ---------------------------------------------------------------------------
// ModelTagInput
// ---------------------------------------------------------------------------

export function ModelTagInput({
  models,
  placeholder,
  platform,
  onModelsChange
}: {
  models: string[]
  placeholder?: string
  platform?: string
  onModelsChange: (models: string[]) => void
}) {
  const { t } = useTranslation()
  const [inputValue, setInputValue] = useState('')

  const addModel = () => {
    const val = inputValue.trim()
    if (!val) return
    if (!models.includes(val)) onModelsChange([...models, val])
    setInputValue('')
  }

  const removeModel = (idx: number) => {
    const newModels = [...models]
    newModels.splice(idx, 1)
    onModelsChange(newModels)
  }

  const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text') || ''
    const items = text.split(/[,\n;]+/).map((s) => s.trim()).filter(Boolean)
    if (items.length === 0) return
    e.preventDefault()
    const unique = [...new Set([...models, ...items])]
    onModelsChange(unique)
    setInputValue('')
  }

  return (
    <div>
      <div className="flex flex-wrap gap-1.5 rounded-lg border border-gray-200 surface-inset p-2 dark:border-dark-600 min-h-[2.5rem]">
        {models.map((model, idx) => (
          <span
            key={`${model}-${idx}`}
            className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-sm ${getPlatformTagClass(platform || '')}`}
          >
            {model}
            <button
              type="button"
              onClick={() => removeModel(idx)}
              className="ml-0.5 rounded-full p-0.5 hover:bg-primary-200/25 dark:hover:bg-primary-800/25 surface-control"
            >
              <X className="h-3 w-3" />
            </button>
          </span>
        ))}
        <Input
          value={inputValue}
          onValueChange={setInputValue}
          type="text"
          className="flex-1 min-w-[120px] border-none bg-transparent text-sm outline-none placeholder:text-gray-400 dark:text-white shadow-none focus:ring-0"
          placeholder={models.length === 0 ? placeholder : ''}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              addModel()
            }
            if (event.key === 'Tab' && inputValue.trim()) {
              event.preventDefault()
              addModel()
            }
            if (event.key === 'Backspace' && inputValue === '' && models.length > 0) {
              removeModel(models.length - 1)
            }
          }}
          onPaste={handlePaste}
          onBlur={addModel}
        />
      </div>
      <p className="mt-1 text-xs surface-muted">
        {t('admin.channels.form.modelInputHint', 'Press Enter to add, supports paste for batch import.')}
      </p>
    </div>
  )
}

// ---------------------------------------------------------------------------
// IntervalRow
// ---------------------------------------------------------------------------

function toInt(val: string): number {
  const n = parseInt(val, 10)
  return isNaN(n) ? 0 : n
}

function toIntOrNull(val: string): number | null {
  if (val === '') return null
  const n = parseInt(val, 10)
  return isNaN(n) ? null : n
}

export function IntervalRow({
  interval,
  mode,
  enableMultipliers,
  onUpdate,
  onRemove
}: {
  interval: IntervalFormEntry
  mode: BillingMode
  enableMultipliers?: boolean
  onUpdate: (interval: IntervalFormEntry) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()

  const isEmpty =
    (interval.input_price == null || interval.input_price === '') &&
    (interval.output_price == null || interval.output_price === '') &&
    (interval.cache_write_price == null || interval.cache_write_price === '') &&
    (interval.cache_write_1h_price == null || interval.cache_write_1h_price === '') &&
    (interval.cache_read_price == null || interval.cache_read_price === '') &&
    (interval.input_multiplier == null || interval.input_multiplier === '') &&
    (interval.output_multiplier == null || interval.output_multiplier === '') &&
    (interval.cache_write_multiplier == null || interval.cache_write_multiplier === '') &&
    (interval.cache_read_multiplier == null || interval.cache_read_multiplier === '') &&
    (interval.per_request_price == null || interval.per_request_price === '')

  const emitField = (field: keyof IntervalFormEntry, value: string | number | null) => {
    onUpdate({ ...interval, [field]: value === '' ? null : value })
  }

  const wrapperClass = `flex items-start gap-2 rounded border p-2 ${
    isEmpty
      ? 'border-red-400 bg-red-50/[0.15] dark:border-red-500 dark:bg-red-950/20'
      : 'border-gray-200 surface-inset dark:border-dark-500 dark:surface-inset'
  }`

  if (mode === 'token') {
    return (
      <div className={wrapperClass}>
        <div className="pricing-interval-grid grid min-w-0 flex-1 gap-2">
          <div>
            <label className="text-xs surface-muted">{t('admin.channels.form.minTokens')}</label>
            <Input
              value={interval.min_tokens}
              onValueChange={(value) => emitField('min_tokens', toInt(value))}
              type="number"
              min="0"
              className="mt-0.5 text-xs"
            />
          </div>
          <div>
            <label className="text-xs surface-muted">
              {t('admin.channels.form.maxTokens')} <span className="text-gray-300">{t('admin.channels.form.inclusive')}</span>
            </label>
            <Input
              value={interval.max_tokens ?? ''}
              onValueChange={(value) => emitField('max_tokens', toIntOrNull(value))}
              type="number"
              min="0"
              className="mt-0.5 text-xs"
              placeholder="∞"
            />
          </div>
          <div>
            <label className="text-xs surface-muted">
              {t('admin.channels.form.inputPrice')} {isEmpty && <span className="text-red-500">*</span>} <span className="text-gray-300">$/M</span>
            </label>
            <Input
              value={interval.input_price}
              onValueChange={(value) => emitField('input_price', value)}
              type="number"
              step="any"
              min="0"
              className="mt-0.5 text-xs"
            />
          </div>
          <div>
            <label className="text-xs surface-muted">
              {t('admin.channels.form.outputPrice')} {isEmpty && <span className="text-red-500">*</span>} <span className="text-gray-300">$/M</span>
            </label>
            <Input
              value={interval.output_price}
              onValueChange={(value) => emitField('output_price', value)}
              type="number"
              step="any"
              min="0"
              className="mt-0.5 text-xs"
            />
          </div>
          <div>
            <label className="text-xs surface-muted">
              {t('admin.channels.form.cacheWrite5mPriceShort')} <span className="text-gray-300">$/M</span>
            </label>
            <Input
              value={interval.cache_write_price}
              onValueChange={(value) => emitField('cache_write_price', value)}
              type="number"
              step="any"
              min="0"
              className="mt-0.5 text-xs"
            />
          </div>
          <div>
            <label className="text-xs surface-muted">
              {t('admin.channels.form.cacheWrite1hPriceShort')} <span className="text-gray-300">$/M</span>
            </label>
            <Input
              value={interval.cache_write_1h_price}
              onValueChange={(value) => emitField('cache_write_1h_price', value)}
              type="number"
              step="any"
              min="0"
              className="mt-0.5 text-xs"
            />
          </div>
          <div>
            <label className="text-xs surface-muted">
              {t('admin.channels.form.cacheReadPriceShort')} <span className="text-gray-300">$/M</span>
            </label>
            <Input
              value={interval.cache_read_price}
              onValueChange={(value) => emitField('cache_read_price', value)}
              type="number"
              step="any"
              min="0"
              className="mt-0.5 text-xs"
            />
          </div>
          {enableMultipliers && (
            <>
              <div>
                <label className="text-xs surface-muted">{t('admin.channels.form.inputMultiplier')}</label>
                <Input
                  value={interval.input_multiplier}
                  onValueChange={(value) => emitField('input_multiplier', value)}
                  type="number"
                  step="any"
                  min="0.000001"
                  className="mt-0.5 text-xs"
                />
              </div>
              <div>
                <label className="text-xs surface-muted">{t('admin.channels.form.outputMultiplier')}</label>
                <Input
                  value={interval.output_multiplier}
                  onValueChange={(value) => emitField('output_multiplier', value)}
                  type="number"
                  step="any"
                  min="0.000001"
                  className="mt-0.5 text-xs"
                />
              </div>
              <div>
                <label className="text-xs surface-muted">{t('admin.channels.form.cacheWriteMultiplier')}</label>
                <Input
                  value={interval.cache_write_multiplier}
                  onValueChange={(value) => emitField('cache_write_multiplier', value)}
                  type="number"
                  step="any"
                  min="0.000001"
                  className="mt-0.5 text-xs"
                />
              </div>
              <div>
                <label className="text-xs surface-muted">{t('admin.channels.form.cacheReadMultiplier')}</label>
                <Input
                  value={interval.cache_read_multiplier}
                  onValueChange={(value) => emitField('cache_read_multiplier', value)}
                  type="number"
                  step="any"
                  min="0.000001"
                  className="mt-0.5 text-xs"
                />
              </div>
            </>
          )}
        </div>
        <button type="button" onClick={onRemove} className="mt-4 rounded p-0.5 hover:text-red-500 surface-control surface-muted">
          <X className="h-4 w-4" />
        </button>
      </div>
    )
  }

  return (
    <div className={wrapperClass}>
      <div className="w-24">
        <label className="text-xs surface-muted">
          {mode === 'image' ? t('admin.channels.form.resolution') : t('admin.channels.form.tierLabel')}
        </label>
        <Input
          value={interval.tier_label}
          onValueChange={(value) => emitField('tier_label', value)}
          type="text"
          className="mt-0.5 text-xs"
          placeholder={mode === 'image' ? '1K / 2K / 4K' : ''}
        />
      </div>
      <div className="w-20">
        <label className="text-xs surface-muted">{t('admin.channels.form.minTokens')}</label>
        <Input
          value={interval.min_tokens}
          onValueChange={(value) => emitField('min_tokens', toInt(value))}
          type="number"
          min="0"
          className="mt-0.5 text-xs"
        />
      </div>
      <div className="w-20">
        <label className="text-xs surface-muted">
          {t('admin.channels.form.maxTokens')} <span className="text-gray-300">{t('admin.channels.form.inclusive')}</span>
        </label>
        <Input
          value={interval.max_tokens ?? ''}
          onValueChange={(value) => emitField('max_tokens', toIntOrNull(value))}
          type="number"
          min="0"
          className="mt-0.5 text-xs"
          placeholder="∞"
        />
      </div>
      <div className="flex-1">
        <label className="text-xs surface-muted">
          {t('admin.channels.form.perRequestPrice')} {isEmpty && <span className="text-red-500">*</span>} <span className="text-gray-300">$</span>
        </label>
        <Input
          value={interval.per_request_price}
          onValueChange={(value) => emitField('per_request_price', value)}
          type="number"
          step="any"
          min="0"
          className="mt-0.5 text-xs"
        />
      </div>
      <button type="button" onClick={onRemove} className="mt-4 rounded p-0.5 hover:text-red-500 surface-control surface-muted">
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// TimePricingSection
// ---------------------------------------------------------------------------

export function TimePricingSection({
  value,
  onChange
}: {
  value: TimePricingFormEntry
  onChange: (value: TimePricingFormEntry) => void
}) {
  const { t } = useTranslation()

  const addPeriod = () => {
    onChange({
      ...value,
      periods: [...value.periods, { start_time: '00:00:00', end_time: '00:00:00', multiplier: '1.00' }]
    })
  }

  const updatePeriod = (index: number, patch: Partial<TimePricingFormEntry['periods'][number]>) => {
    onChange({
      ...value,
      periods: value.periods.map((period, i) => (i === index ? { ...period, ...patch } : period))
    })
  }

  const removePeriod = (index: number) => {
    onChange({ ...value, periods: value.periods.filter((_, i) => i !== index) })
  }

  return (
    <div className="space-y-2 rounded-lg border border-gray-200 surface-inset p-3 dark:border-dark-600">
      <div className="flex items-center justify-between gap-3">
        <label className="input-label mb-0 text-xs">{t('admin.channels.form.timePricing')}</label>
        <label className="flex items-center gap-2 text-xs surface-muted">
          <input
            type="checkbox"
            checked={value.weekdays_only}
            onChange={(event) => onChange({ ...value, weekdays_only: event.target.checked })}
            className="h-3.5 w-3.5 rounded border-gray-300 text-primary-600"
          />
          {t('admin.channels.form.weekdaysOnly')}
        </label>
      </div>

      <div className="flex items-center gap-2">
        <label className="text-xs surface-muted">{t('admin.channels.form.timezone')}</label>
        <Select
          searchable
          value={value.timezone}
          options={COMMON_TIMEZONES.map((tz) => ({
            value: tz,
            label: `${tz} (${formatTimezoneOffset(tz)})`
          }))}
          onValueChange={(timezone) => onChange({ ...value, timezone: String(timezone ?? '') })}
          triggerClass="text-xs"
        />
      </div>

      {value.periods.length === 0 ? (
        <p className="text-xs surface-muted">{t('admin.channels.form.noTimePeriods')}</p>
      ) : (
        <div className="space-y-1">
          {value.periods.map((period, index) => (
            <div key={`period-${index}`} className="flex items-center gap-2">
              <Input
                type="time"
                step="1"
                value={period.start_time}
                onValueChange={(start_time) => updatePeriod(index, { start_time })}
                className="w-28 text-xs"
              />
              <span className="text-xs surface-muted">→</span>
              <Input
                type="time"
                step="1"
                value={period.end_time}
                onValueChange={(end_time) => updatePeriod(index, { end_time })}
                className="w-28 text-xs"
              />
              <Input
                type="number"
                step="0.01"
                min="0.01"
                value={period.multiplier}
                onValueChange={(multiplier) => updatePeriod(index, { multiplier })}
                className="w-20 text-xs"
              />
              <span className="text-xs surface-muted">x</span>
              <button type="button" onClick={() => removePeriod(index)} className="rounded p-0.5 hover:text-red-500 surface-control surface-muted">
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}

      <Button variant="secondary" size="xs" onClick={addPeriod}>
        + {t('admin.channels.form.addPeriod')}
      </Button>
    </div>
  )
}

export function isTimePricingMultiplierValid(value: number | string): boolean {
  return isValidTimePricingMultiplier(value)
}

export function ToggleRow({
  label,
  hint,
  hintClass,
  checked,
  onChange
}: {
  label: string
  hint?: string
  hintClass?: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <label className="text-xs font-medium surface-muted">{label}</label>
        {hint && <p className={`mt-0.5 text-[11px] ${hintClass ?? 'surface-muted'}`}>{hint}</p>}
      </div>
      <Toggle checked={checked} onCheckedChange={onChange} />
    </div>
  )
}
