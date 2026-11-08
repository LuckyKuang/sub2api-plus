import { useTranslation } from 'react-i18next'
import type { UserMonitorView } from '@/api/channelMonitor'
import { isChannelMonitorQuotaVisible } from '@/utils/featureFlags'
import { useChannelMonitorFormat, providerGradient } from '../useChannelMonitorFormat'
import { ProviderIcon } from './ProviderIcon'
import { MonitorMetricPair } from './MonitorMetricPair'
import { MonitorAvailabilityRow } from './MonitorAvailabilityRow'
import { MonitorTimeline } from './MonitorTimeline'
import { MonitorQuotaView } from './MonitorQuotaView'

// 图标配色与 utils/platformColors.ts 的平台色对齐（新 4 家）。
const PROVIDER_TINT: Record<string, string> = {
  openai: 'text-emerald-600 dark:text-emerald-300',
  anthropic: 'text-orange-600 dark:text-orange-300',
  gemini: 'text-sky-600 dark:text-sky-300',
  grok: 'text-zinc-700 dark:text-zinc-200',
  antigravity: 'text-purple-600 dark:text-purple-300',
  kimi: 'text-pink-600 dark:text-pink-300',
  zhipu: 'text-indigo-600 dark:text-indigo-300',
  deepseek: 'text-teal-600 dark:text-teal-300',
  opencode_go: 'text-amber-700 dark:text-amber-300'
}

export interface MonitorCardProps {
  item: UserMonitorView
  window: '7d' | '15d' | '30d'
  availabilityValue: number | null
  countdownSeconds: number
  onClick?: () => void
}

export function MonitorCard({ item, window, availabilityValue, countdownSeconds, onClick }: MonitorCardProps) {
  const { t } = useTranslation()
  const {
    statusLabel,
    statusBadgeClass,
    providerLabel,
    providerBadgeClass,
    formatLatency,
    formatMonitorModel
  } = useChannelMonitorFormat()

  const providerTintClass = PROVIDER_TINT[item.provider] ?? 'surface-muted'
  const quotaVisible = isChannelMonitorQuotaVisible() && !!item.latest_quota

  const availabilityLabel = `${t('monitorCommon.availabilityPrefix')} · ${t(`channelStatus.windowTab.${window}`)}`

  const extraModelsCount = item.extra_models?.length ?? 0
  const extraModelsCountLabel =
    extraModelsCount === 0 ? undefined : t('monitorCommon.extraModelsCount', { n: extraModelsCount })

  return (
    <button
      type="button"
      className="glass-surface group flex min-h-[280px] w-full flex-col border border-gray-200/80 p-5 text-left shadow-card transition-all duration-300 ease-out hover:-translate-y-1 hover:shadow-card-hover dark:border-dark-700/70 dark:hover:border-primary-500/30"
      onClick={onClick}
    >
      {/* Header: icon + name/model + status chip */}
      <div className="flex items-start gap-3">
        <span
          className={`grid h-9 w-9 flex-shrink-0 place-items-center rounded-xl ring-1 ring-black/5 dark:ring-white/10 ${providerGradient(item.provider)} ${providerTintClass}`}
        >
          <ProviderIcon provider={item.provider} size={20} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-base font-semibold text-gray-900 dark:text-gray-100">{item.name}</div>
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5">
            <span
              className={`inline-flex flex-shrink-0 items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium ${providerBadgeClass(item.provider)}`}
            >
              {providerLabel(item.provider)}
            </span>
            {/* 纯配额模式主模型是占位符 "quota"，展示层替换为本地化「配额」标签 */}
            <span className="truncate font-mono text-xs surface-muted">{formatMonitorModel(item.primary_model)}</span>
            {item.group_name && (
              <span className="inline-flex flex-shrink-0 items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium surface-inset surface-muted">
                {item.group_name}
              </span>
            )}
          </div>
        </div>
        <span
          className={`flex-shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold ${statusBadgeClass(item.primary_status)}`}
        >
          {statusLabel(item.primary_status)}
        </span>
      </div>

      <p className="mt-3 text-xs surface-muted" title={t('monitorCommon.timingHint')}>
        {t('monitorCommon.firstText')} {formatLatency(item.primary_first_text_ms)} ms · {t('monitorCommon.firstByte')}{' '}
        {formatLatency(item.primary_first_byte_ms)} ms
      </p>
      {/* Metrics */}
      <MonitorMetricPair
        primaryIcon="bolt"
        primaryLabel={t('usage.latencyDuration')}
        primaryValue={formatLatency(item.primary_latency_ms)}
        primaryUnit="ms"
        secondaryIcon="globe"
        secondaryLabel={t('monitorCommon.endpointPing')}
        secondaryValue={formatLatency(item.primary_ping_latency_ms)}
        secondaryUnit="ms"
      />

      {/* 配额模式：最新用量/余额快照（服务端已按系统开关剥离，此处 flag 为纵深防御） */}
      {quotaVisible && <MonitorQuotaView snapshot={item.latest_quota} />}

      {/* Divider */}
      <div className="mt-4 border-t border-gray-100 dark:border-dark-700/60" />

      {/* Availability row */}
      <MonitorAvailabilityRow windowLabel={availabilityLabel} value={availabilityValue} samplesLabel={extraModelsCountLabel} />

      {/* Timeline */}
      <MonitorTimeline buckets={item.timeline} countdownSeconds={countdownSeconds} />
    </button>
  )
}

export default MonitorCard
