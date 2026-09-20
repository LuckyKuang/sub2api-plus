/**
 * IANA 时区下拉选项。
 *
 * 选项优先用浏览器原生 `Intl.supportedValuesOf('timeZone')` 生成完整 IANA 列表
 * （自动跟随运行环境 tzdata 更新），label 带 UTC 偏移；
 * 运行环境不支持时回退到内置常用时区静态列表。
 */

export interface DropdownOption {
  value: string
  label: string
  // 兼容 common/Select.vue 的 SelectOption（要求 string 索引签名）。
  [key: string]: unknown
}

// 兜底列表：与 QuotaLimitCard 历史常用时区保持一致。
const FALLBACK_TIMEZONES = [
  'UTC',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Seoul',
  'Asia/Singapore',
  'Asia/Kolkata',
  'Asia/Dubai',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Moscow',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'America/Sao_Paulo',
  'Australia/Sydney',
  'Pacific/Auckland',
]

// "GMT-08:00" → "UTC-08:00"；纯 "GMT" → "UTC"
function formatUtcOffset(timeZone: string): string {
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone,
    timeZoneName: 'longOffset',
  })
  const part = formatter.formatToParts().find((p) => p.type === 'timeZoneName')
  const raw = part?.value ?? 'GMT'
  return raw === 'GMT' ? 'UTC' : `UTC${raw.slice(3)}`
}

function compareByOffset(a: DropdownOption, b: DropdownOption): number {
  const offsetOf = (label: string) => {
    const match = label.match(/UTC([+-])(\d{2}):(\d{2})/)
    if (!match) return 0
    const minutes = Number(match[2]) * 60 + Number(match[3])
    return match[1] === '-' ? -minutes : minutes
  }
  const diff = offsetOf(a.label) - offsetOf(b.label)
  if (diff !== 0) return diff
  return a.value.localeCompare(b.value)
}

function buildOptions(timezones: readonly string[]): DropdownOption[] {
  return timezones
    .map((tz) => {
      try {
        return { value: tz, label: `(${formatUtcOffset(tz)}) ${tz}` }
      } catch {
        return null
      }
    })
    .filter((opt): opt is DropdownOption => opt !== null)
    .sort(compareByOffset)
}

let cachedOptions: DropdownOption[] | null = null

export function getTimezoneOptions(): DropdownOption[] {
  if (cachedOptions) return cachedOptions
  let zones: readonly string[] = FALLBACK_TIMEZONES
  try {
    // 项目 TS lib 目标较旧，未声明 Intl.supportedValuesOf，这里做运行时特性探测。
    const supported = (
      Intl as unknown as { supportedValuesOf?: (key: string) => readonly string[] }
    ).supportedValuesOf?.('timeZone')
    if (supported && supported.length > 0) {
      // supportedValuesOf 不包含 'UTC'（非 region/city 形式的 IANA 名称），显式补入。
      zones = supported.includes('UTC') ? supported : ['UTC', ...supported]
    }
  } catch {
    // keep fallback
  }
  cachedOptions = buildOptions(zones)
  return cachedOptions
}
