import { useTranslation } from 'react-i18next'
import type { CheckMode, MonitorStatus, Provider } from '@/api/admin/channelMonitor'

/**
 * Channel monitor string constants. Mirrors the Vue `constants/channelMonitor`
 * module; kept local until that module is ported.
 */
export const PROVIDER_OPENAI: Provider = 'openai'
export const PROVIDER_ANTHROPIC: Provider = 'anthropic'
export const PROVIDER_GEMINI: Provider = 'gemini'
export const PROVIDER_GROK: Provider = 'grok'
export const PROVIDER_ANTIGRAVITY: Provider = 'antigravity'
export const PROVIDER_KIMI: Provider = 'kimi'
export const PROVIDER_ZHIPU: Provider = 'zhipu'
export const PROVIDER_DEEPSEEK: Provider = 'deepseek'
export const PROVIDER_MINIMAX: Provider = 'minimax'
export const PROVIDER_STEPFUN: Provider = 'stepfun'
export const PROVIDER_OPENCODE_GO: Provider = 'opencode_go'

export const CHECK_MODE_PROBE: CheckMode = 'probe'
export const CHECK_MODE_QUOTA: CheckMode = 'quota'
export const CHECK_MODE_QUOTA_PROBE: CheckMode = 'quota_probe'

export const PROVIDERS: readonly Provider[] = [
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GEMINI,
  PROVIDER_GROK,
  PROVIDER_ANTIGRAVITY,
  PROVIDER_KIMI,
  PROVIDER_ZHIPU,
  PROVIDER_DEEPSEEK,
  PROVIDER_MINIMAX,
  PROVIDER_STEPFUN,
  PROVIDER_OPENCODE_GO
]

export const STATUS_OPERATIONAL: MonitorStatus = 'operational'
export const STATUS_DEGRADED: MonitorStatus = 'degraded'
export const STATUS_FAILED: MonitorStatus = 'failed'
export const STATUS_ERROR: MonitorStatus = 'error'

export const MONITOR_STATUSES: readonly MonitorStatus[] = [
  STATUS_OPERATIONAL,
  STATUS_DEGRADED,
  STATUS_FAILED,
  STATUS_ERROR
]
