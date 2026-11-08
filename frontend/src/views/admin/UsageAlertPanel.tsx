import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { adminAPI } from '@/api'
import type {
  UsageAlertChannel,
  UsageAlertConfig,
  UsageAlertRule
} from '@/api/admin/accounts'
import { Modal, Select, Toggle } from '@/components/ui'
import { TriggerTooltip } from './TriggerTooltip'

type QuietRange = { start: string; end: string }
type EditableRule = UsageAlertRule & {
  _key: string
  quiet_hours_ranges: QuietRange[]
}

export interface UsageAlertPanelProps {
  show: boolean
  accountId: number | null
  onClose: () => void
}

const MAX_RULES = 20

const HELP_TRIGGER_CLASS =
  'inline-flex h-4 w-4 cursor-help items-center justify-center rounded-full border border-gray-400/70 text-[10px] font-semibold text-gray-400'

export function UsageAlertPanel({ show, accountId, onClose }: UsageAlertPanelProps) {
  const { t } = useTranslation()

  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testingIndex, setTestingIndex] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [successMessage, setSuccessMessage] = useState<string | null>(null)
  const [rules, setRules] = useState<EditableRule[]>([])
  const keySeq = useRef(0)

  const nextKey = () => {
    keySeq.current += 1
    return `local-${keySeq.current}`
  }

  const normalizeClock = (value: string, fallback: string) => {
    const raw = (value || '').trim()
    if (!raw) return fallback
    if (/^\d{2}:\d{2}:\d{2}$/.test(raw)) return raw
    if (/^\d{2}:\d{2}$/.test(raw)) return `${raw}:00`
    return fallback
  }

  const parseQuietHours = (items?: string[] | null): QuietRange[] => {
    if (!items?.length) return []
    return items
      .map((item) => {
        const [start, end] = String(item).split('-')
        return {
          start: normalizeClock(start || '', '18:00:00'),
          end: normalizeClock(end || '', '23:59:59')
        }
      })
      .filter((r) => r.start && r.end)
  }

  const emptyRule = (): EditableRule => ({
    _key: nextKey(),
    id: '',
    enabled: false,
    channel: 'wecom',
    webhook_url: '',
    cron_expression: '0 * * * *',
    force_probe: false,
    threshold_enabled: false,
    threshold_percent: 80,
    threshold_watch_cron: '*/5 * * * *',
    cooldown_seconds: 3600,
    quiet_hours_ranges: [],
    next_run_at: null,
    last_run_at: null,
    threshold_next_run_at: null,
    last_threshold_alert_at: null,
    last_error: ''
  })

  const toEditable = (rule: UsageAlertRule): EditableRule => ({
    _key: rule.id || nextKey(),
    id: rule.id || '',
    enabled: !!rule.enabled,
    channel: (rule.channel || 'wecom') as UsageAlertChannel,
    webhook_url: rule.webhook_url || '',
    cron_expression: rule.cron_expression || '0 * * * *',
    force_probe: !!rule.force_probe,
    threshold_enabled: !!rule.threshold_enabled,
    threshold_percent: rule.threshold_percent || 80,
    threshold_watch_cron: rule.threshold_watch_cron || '*/5 * * * *',
    cooldown_seconds: rule.cooldown_seconds || 3600,
    quiet_hours_ranges: parseQuietHours(rule.quiet_hours),
    next_run_at: rule.next_run_at || null,
    last_run_at: rule.last_run_at || null,
    threshold_next_run_at: rule.threshold_next_run_at || null,
    last_threshold_alert_at: rule.last_threshold_alert_at || null,
    last_error: rule.last_error || ''
  })

  const applyConfig = (cfg: UsageAlertConfig) => {
    setRules((cfg.rules || []).map(toEditable))
  }

  const payloadRules = (): UsageAlertRule[] =>
    rules.map((rule) => ({
      id: rule.id || undefined,
      enabled: !!rule.enabled,
      channel: rule.channel,
      webhook_url: rule.webhook_url.trim(),
      cron_expression: rule.cron_expression.trim() || '0 * * * *',
      force_probe: !!rule.force_probe,
      threshold_enabled: !!rule.threshold_enabled,
      threshold_percent: rule.threshold_enabled ? Number(rule.threshold_percent) || 0 : 0,
      threshold_watch_cron: rule.threshold_enabled ? (rule.threshold_watch_cron || '').trim() : '',
      cooldown_seconds: rule.threshold_enabled ? Number(rule.cooldown_seconds) || 0 : 0,
      quiet_hours: (rule.quiet_hours_ranges || [])
        .map((r) => `${normalizeClock(r.start, '')}-${normalizeClock(r.end, '')}`)
        .filter((item) => item.includes('-') && !item.startsWith('-') && !item.endsWith('-'))
    }))

  const addQuietRange = (ruleIndex: number) => {
    setRules((prev) =>
      prev.map((rule, index) => {
        if (index !== ruleIndex) return rule
        if ((rule.quiet_hours_ranges?.length || 0) >= 10) return rule
        return { ...rule, quiet_hours_ranges: [...rule.quiet_hours_ranges, { start: '18:00:00', end: '23:59:59' }] }
      })
    )
  }

  const removeQuietRange = (ruleIndex: number, quietIndex: number) => {
    setRules((prev) =>
      prev.map((rule, index) =>
        index === ruleIndex
          ? { ...rule, quiet_hours_ranges: rule.quiet_hours_ranges.filter((_, qi) => qi !== quietIndex) }
          : rule
      )
    )
  }

  const patchRule = (index: number, patch: Partial<EditableRule>) => {
    setRules((prev) => prev.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)))
  }

  const canTest = (rule: EditableRule) => rule.webhook_url.trim().length > 0

  const webhookPlaceholder = (channel: UsageAlertChannel) => {
    if (channel === 'feishu') return t('admin.usageAlert.webhookPlaceholderFeishu')
    if (channel === 'custom') return t('admin.usageAlert.webhookPlaceholderCustom')
    return t('admin.usageAlert.webhookPlaceholderWecom')
  }

  const webhookHint = (channel: UsageAlertChannel) => {
    if (channel === 'feishu') return t('admin.usageAlert.webhookHintFeishu')
    if (channel === 'custom') return t('admin.usageAlert.webhookHintCustom')
    return t('admin.usageAlert.webhookHintWecom')
  }

  const truncateError = (value: string) => (value.length > 120 ? `${value.slice(0, 120)}…` : value)

  const extractErrorMessage = (e: unknown): string => {
    const err = e as { message?: string; reason?: string }
    return err?.message || err?.reason || t('common.error')
  }

  const formatTime = (value: string) => {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    return new Intl.DateTimeFormat(undefined, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    }).format(date)
  }

  const addRule = () => {
    if (rules.length >= MAX_RULES) return
    setRules((prev) => [...prev, emptyRule()])
  }

  const removeRule = (index: number) => {
    setRules((prev) => prev.filter((_, i) => i !== index))
  }

  const loadConfig = async () => {
    if (!accountId) return
    setLoading(true)
    setError(null)
    setSuccessMessage(null)
    try {
      applyConfig(await adminAPI.accounts.getUsageAlert(accountId))
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (show && accountId) {
      void loadConfig()
    }
    // Account identity drives the config payload.
  }, [show, accountId])

  const handleSave = async () => {
    if (!accountId || saving) return
    setSaving(true)
    setError(null)
    setSuccessMessage(null)
    try {
      const cfg = await adminAPI.accounts.updateUsageAlert(accountId, { rules: payloadRules() })
      applyConfig(cfg)
      setSuccessMessage(t('admin.usageAlert.saveSuccess'))
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async (index: number) => {
    if (!accountId || testingIndex !== null) return
    const rule = rules[index]
    if (!rule || !canTest(rule)) return
    setTestingIndex(index)
    setError(null)
    setSuccessMessage(null)
    try {
      const draft = payloadRules()[index]
      const cfg = await adminAPI.accounts.testUsageAlert(accountId, {
        rule_id: draft.id || '',
        rule: draft
      })
      if (draft.id) {
        const updated = (cfg.rules || []).find((r) => r.id === draft.id)
        if (updated) {
          setRules((prev) => prev.map((entry, i) => (i === index ? { ...toEditable(updated), _key: entry._key } : entry)))
        }
      }
      setSuccessMessage(t('admin.usageAlert.testSuccess'))
    } catch (e) {
      setError(extractErrorMessage(e))
    } finally {
      setTestingIndex(null)
    }
  }

  const cronTooltip = (
    <div className="space-y-1 text-xs">
      <p className="font-medium">{t('admin.usageAlert.cronTooltipTitle')}</p>
      <p>{t('admin.usageAlert.cronTooltipMeaning')}</p>
      <p>{t('admin.usageAlert.cronTooltipExampleHourly')}</p>
      <p>{t('admin.usageAlert.cronTooltipExampleDaily')}</p>
    </div>
  )

  return (
    <Modal open={show} onClose={onClose} title={t('admin.usageAlert.title')} size="xl">
      <div className="space-y-4">
        <p className="text-sm surface-muted">{t('admin.usageAlert.description')}</p>

        {loading ? (
          <div className="py-8 text-center text-sm surface-muted">{t('common.loading')}</div>
        ) : (
          <>
            <div className="flex items-center justify-between gap-2">
              <div className="text-sm font-medium surface-muted">
                {t('admin.usageAlert.rulesTitle')}
                <span className="ml-1 text-xs font-normal surface-muted">
                  ({rules.length}/{MAX_RULES})
                </span>
              </div>
              <button type="button" className="btn btn-secondary text-xs" disabled={rules.length >= MAX_RULES} onClick={addRule}>
                {t('admin.usageAlert.addRule')}
              </button>
            </div>

            {rules.length === 0 && (
              <div className="rounded-lg border border-dashed border-gray-300 p-6 text-center text-sm dark:border-dark-600 surface-muted">
                {t('admin.usageAlert.emptyRules')}
              </div>
            )}

            {rules.map((rule, index) => (
              <div key={rule._key} className="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-sm font-medium text-gray-800 dark:text-gray-100">
                    {t('admin.usageAlert.ruleLabel', { n: index + 1 })}
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      className="btn btn-secondary text-xs"
                      disabled={testingIndex === index || saving || !canTest(rule)}
                      onClick={() => void handleTest(index)}
                    >
                      {testingIndex === index ? t('admin.usageAlert.testing') : t('admin.usageAlert.testSend')}
                    </button>
                    <button
                      type="button"
                      className="text-xs text-red-600 hover:underline dark:text-red-400 surface-control"
                      disabled={saving || testingIndex !== null}
                      onClick={() => removeRule(index)}
                    >
                      {t('common.delete')}
                    </button>
                  </div>
                </div>

                <div className="flex items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-medium surface-muted">{t('admin.usageAlert.enabled')}</div>
                    <div className="text-xs surface-muted">{t('admin.usageAlert.enabledHint')}</div>
                  </div>
                  <Toggle checked={rule.enabled} onCheckedChange={(checked) => patchRule(index, { enabled: checked })} />
                </div>

                <div>
                  <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.usageAlert.channel')}</label>
                  <Select
                    value={rule.channel}
                    className="w-full"
                    triggerClass="text-sm"
                    searchable={false}
                    options={[
                      { value: 'wecom', label: t('admin.usageAlert.channelWecom') },
                      { value: 'feishu', label: t('admin.usageAlert.channelFeishu') },
                      { value: 'custom', label: t('admin.usageAlert.channelCustom') }
                    ]}
                    onValueChange={(value) => patchRule(index, { channel: String(value ?? 'wecom') as UsageAlertChannel })}
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.usageAlert.webhookUrl')}</label>
                  <input
                    value={rule.webhook_url}
                    onChange={(event) => patchRule(index, { webhook_url: event.target.value })}
                    type="text"
                    className="input w-full text-sm"
                    placeholder={webhookPlaceholder(rule.channel)}
                  />
                  <p className="mt-1 text-xs surface-muted">{webhookHint(rule.channel)}</p>
                </div>

                <div>
                  <label className="mb-1 flex items-center gap-1 text-xs font-medium surface-muted">
                    {t('admin.usageAlert.cronExpression')}
                    <TriggerTooltip content={cronTooltip}>
                      <span className={HELP_TRIGGER_CLASS}>?</span>
                    </TriggerTooltip>
                  </label>
                  <input
                    value={rule.cron_expression}
                    onChange={(event) => patchRule(index, { cron_expression: event.target.value })}
                    type="text"
                    className="input w-full text-sm"
                    placeholder={t('admin.usageAlert.cronPlaceholder')}
                  />
                  <p className="mt-1 text-xs surface-muted">{t('admin.usageAlert.cronHelp')}</p>
                </div>

                <div className="flex items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-medium surface-muted">{t('admin.usageAlert.thresholdEnabled')}</div>
                    <div className="text-xs surface-muted">{t('admin.usageAlert.thresholdEnabledHint')}</div>
                  </div>
                  <Toggle
                    checked={rule.threshold_enabled}
                    onCheckedChange={(checked) => patchRule(index, { threshold_enabled: checked })}
                  />
                </div>

                {rule.threshold_enabled && (
                  <div className="space-y-3 rounded-lg border border-amber-200/70 bg-amber-50/40 p-3 dark:border-amber-900/40 dark:bg-amber-950/20">
                    <div>
                      <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.usageAlert.thresholdPercent')}</label>
                      <input
                        value={rule.threshold_percent}
                        onChange={(event) => patchRule(index, { threshold_percent: Number(event.target.value) })}
                        type="number"
                        min="1"
                        max="99"
                        step="1"
                        className="input w-32 text-sm"
                      />
                      <p className="mt-1 text-xs surface-muted">{t('admin.usageAlert.thresholdPercentHint')}</p>
                    </div>
                    <div>
                      <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.usageAlert.thresholdWatchCron')}</label>
                      <input
                        value={rule.threshold_watch_cron}
                        onChange={(event) => patchRule(index, { threshold_watch_cron: event.target.value })}
                        type="text"
                        className="input w-full text-sm"
                        placeholder={t('admin.usageAlert.thresholdWatchCronPlaceholder')}
                      />
                      <p className="mt-1 text-xs surface-muted">{t('admin.usageAlert.thresholdWatchCronHint')}</p>
                    </div>
                    <div>
                      <label className="mb-1 block text-xs font-medium surface-muted">{t('admin.usageAlert.cooldownSeconds')}</label>
                      <input
                        value={rule.cooldown_seconds}
                        onChange={(event) => patchRule(index, { cooldown_seconds: Number(event.target.value) })}
                        type="number"
                        min="1"
                        step="1"
                        className="input w-40 text-sm"
                      />
                      <p className="mt-1 text-xs surface-muted">{t('admin.usageAlert.cooldownSecondsHint')}</p>
                    </div>
                  </div>
                )}

                <div className="flex items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-medium surface-muted">{t('admin.usageAlert.forceProbe')}</div>
                    <div className="text-xs surface-muted">{t('admin.usageAlert.forceProbeHint')}</div>
                  </div>
                  <Toggle checked={rule.force_probe} onCheckedChange={(checked) => patchRule(index, { force_probe: checked })} />
                </div>

                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <div>
                      <div className="text-sm font-medium surface-muted">{t('admin.usageAlert.quietHours')}</div>
                      <div className="text-xs surface-muted">{t('admin.usageAlert.quietHoursHint')}</div>
                    </div>
                    <button
                      type="button"
                      className="btn btn-secondary text-xs"
                      disabled={(rule.quiet_hours_ranges?.length || 0) >= 10}
                      onClick={() => addQuietRange(index)}
                    >
                      {t('admin.usageAlert.quietHoursAdd')}
                    </button>
                  </div>
                  {!rule.quiet_hours_ranges?.length && <div className="text-xs surface-muted">{t('admin.usageAlert.quietHoursEmpty')}</div>}
                  {rule.quiet_hours_ranges.map((range, qi) => (
                    <div key={`${rule._key}-q-${qi}`} className="flex flex-wrap items-end gap-2">
                      <div>
                        <label className="mb-1 block text-[11px] surface-muted">{t('admin.usageAlert.quietHoursStart')}</label>
                        <input
                          aria-label={t('admin.usageAlert.quietHoursStart')}
                          value={range.start}
                          onChange={(event) =>
                            patchRule(index, {
                              quiet_hours_ranges: rule.quiet_hours_ranges.map((entry, i) =>
                                i === qi ? { ...entry, start: event.target.value } : entry
                              )
                            })
                          }
                          type="time"
                          step="1"
                          className="input text-sm"
                        />
                      </div>
                      <div>
                        <label className="mb-1 block text-[11px] surface-muted">{t('admin.usageAlert.quietHoursEnd')}</label>
                        <input
                          aria-label={t('admin.usageAlert.quietHoursEnd')}
                          value={range.end}
                          onChange={(event) =>
                            patchRule(index, {
                              quiet_hours_ranges: rule.quiet_hours_ranges.map((entry, i) =>
                                i === qi ? { ...entry, end: event.target.value } : entry
                              )
                            })
                          }
                          type="time"
                          step="1"
                          className="input text-sm"
                        />
                      </div>
                      <button
                        type="button"
                        className="mb-1 text-xs text-red-600 hover:underline dark:text-red-400 surface-control"
                        onClick={() => removeQuietRange(index, qi)}
                      >
                        {t('common.delete')}
                      </button>
                    </div>
                  ))}
                </div>

                {(rule.next_run_at || rule.last_run_at || rule.threshold_next_run_at || rule.last_threshold_alert_at || rule.last_error) && (
                  <div className="rounded-lg border border-gray-200 surface-inset p-3 text-xs dark:border-dark-700">
                    {rule.next_run_at && (
                      <div className="surface-muted">
                        {t('admin.usageAlert.nextRun')}: {formatTime(rule.next_run_at)}
                      </div>
                    )}
                    {rule.last_run_at && (
                      <div className="mt-1 surface-muted">
                        {t('admin.usageAlert.lastRun')}: {formatTime(rule.last_run_at)}
                      </div>
                    )}
                    {rule.threshold_next_run_at && (
                      <div className="mt-1 surface-muted">
                        {t('admin.usageAlert.thresholdNextRun')}: {formatTime(rule.threshold_next_run_at)}
                      </div>
                    )}
                    {rule.last_threshold_alert_at && (
                      <div className="mt-1 surface-muted">
                        {t('admin.usageAlert.lastThresholdAlert')}: {formatTime(rule.last_threshold_alert_at)}
                      </div>
                    )}
                    {rule.last_error && (
                      <div className="mt-1 text-red-600 dark:text-red-400" title={rule.last_error}>
                        {t('admin.usageAlert.lastError')}: {truncateError(rule.last_error)}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))}

            {error && <div className="text-sm text-red-600 dark:text-red-400">{error}</div>}
            {!error && successMessage && <div className="text-sm text-emerald-600 dark:text-emerald-400">{successMessage}</div>}

            <div className="flex flex-wrap justify-end gap-2 pt-2">
              <button type="button" className="btn btn-primary text-sm" disabled={saving || testingIndex !== null} onClick={() => void handleSave()}>
                {saving ? t('common.saving') : t('common.save')}
              </button>
            </div>
          </>
        )}
      </div>
    </Modal>
  )
}

export default UsageAlertPanel
