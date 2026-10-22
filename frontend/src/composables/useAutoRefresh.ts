import { ref, onMounted, onBeforeUnmount, onActivated, watch } from 'vue'

export interface UseAutoRefreshOptions {
  storageKey: string
  intervals?: readonly number[]
  defaultInterval?: number
  defaultEnabled?: boolean
  minimumIntervalSeconds?: () => number
  onRefresh: () => Promise<void> | void
  shouldPause?: () => boolean
  /** Usage pages: off lasts only for this visit; persist only the interval. */
  temporaryPause?: boolean
  active?: () => boolean
}

export function retryAfterSeconds(error: unknown, now = Date.now()): number | null {
  const e = error as { retryAfter?: string | number; response?: { headers?: Record<string, string> } } | null
  const raw = e?.retryAfter ?? e?.response?.headers?.['retry-after']
  if (raw == null) return null
  const seconds = Number(raw)
  if (Number.isFinite(seconds) && seconds >= 0) return Math.ceil(seconds)
  const deadline = Date.parse(String(raw))
  return Number.isFinite(deadline) ? Math.max(0, Math.ceil((deadline - now) / 1000)) : null
}

export function useAutoRefresh(options: UseAutoRefreshOptions) {
  const intervals = options.intervals ?? [5, 10, 15, 30]
  const enabled = ref(options.defaultEnabled ?? false)
  const intervalSeconds = ref(options.defaultInterval ?? intervals[intervals.length - 1])
  const countdown = ref(0)
  const fetching = ref(false)
  const paused = ref(false)
  const error = ref<unknown>(null)
  const lastUpdated = ref<number | null>(null)
  let timerId: ReturnType<typeof setInterval> | undefined
  let deadline = 0
  let failures = 0
  let disposed = false
  let pending: Promise<void> | null = null
  let retryNotBefore = 0

  try {
    const saved = JSON.parse(localStorage.getItem(options.storageKey) ?? 'null')
    if (saved) {
      if (!options.temporaryPause) enabled.value = saved.enabled === true
      if (intervals.includes(Number(saved.interval_seconds))) intervalSeconds.value = Number(saved.interval_seconds)
    }
  } catch { /* Invalid or restricted storage uses defaults. */ }

  function save() {
    try { localStorage.setItem(options.storageKey, JSON.stringify({ ...(!options.temporaryPause ? { enabled: enabled.value } : {}), interval_seconds: intervalSeconds.value })) } catch { /* optional preference */ }
  }
  function resetCountdown() {
    deadline = Math.max(Date.now() + Math.max(intervalSeconds.value, options.minimumIntervalSeconds?.() ?? 0) * 1000, retryNotBefore)
    countdown.value = enabled.value ? Math.max(0, Math.ceil((deadline - Date.now()) / 1000)) : 0
  }
  function run(): Promise<void> {
    if (pending) return pending
    if (disposed || document.hidden || navigator.onLine === false || Date.now() < retryNotBefore) return Promise.resolve()
    fetching.value = true
    pending = Promise.resolve().then(async () => {
      try {
        await options.onRefresh()
        if (disposed) return
        error.value = null
        failures = 0
        retryNotBefore = 0
        lastUpdated.value = Date.now()
      } catch (failure) {
        if (disposed) return
        error.value = failure
        failures++
        const delay = retryAfterSeconds(failure) ?? Math.min(300, intervalSeconds.value * 2 ** Math.min(failures, 5) * (1 + Math.random() * 0.2))
        retryNotBefore = Date.now() + Math.max(intervalSeconds.value, delay) * 1000
      } finally {
        pending = null
        fetching.value = false
        if (!disposed) resetCountdown()
      }
    })
    return pending
  }
  function isPaused() {
    return document.hidden || navigator.onLine === false || options.active?.() === false || options.shouldPause?.() === true
  }
  function tick() {
    if (!enabled.value || disposed) return
    const wasPaused = paused.value
    paused.value = isPaused()
    if (paused.value || fetching.value) { countdown.value = 0; return }
    if (wasPaused) deadline = Math.max(Date.now(), retryNotBefore)
    countdown.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
    if (countdown.value === 0) void run()
  }
  function start() {
    if (timerId != null || disposed) return
    resetCountdown()
    timerId = setInterval(tick, 1000)
  }
  function stop() {
    if (timerId != null) clearInterval(timerId)
    timerId = undefined
  }
  function setEnabled(value: boolean) {
    enabled.value = value
    save()
    if (value) { start(); if (options.temporaryPause && !isPaused()) void run() }
    else { stop(); countdown.value = 0; paused.value = false }
  }
  function setInterval_(seconds: number) {
    if (!intervals.includes(seconds)) return
    intervalSeconds.value = seconds
    save()
    resetCountdown()
  }
  function activate() {
    if (options.active?.() === false) return
    if (options.temporaryPause) { enabled.value = true; start() }
    paused.value = isPaused()
    if (enabled.value && !paused.value) void run()
  }
  function visibilityChanged() {
    if (document.hidden) { paused.value = true; countdown.value = 0 }
    else if (options.temporaryPause) activate()
    else tick()
  }
  if (options.active) watch(options.active, (active, previous) => { if (active && !previous) activate(); else tick() })
  onActivated(() => { if (options.temporaryPause) activate() })
  onMounted(() => {
    if (enabled.value) start()
    document.addEventListener('visibilitychange', visibilityChanged)
    window.addEventListener('online', tick)
    window.addEventListener('offline', tick)
  })
  onBeforeUnmount(() => {
    disposed = true
    stop()
    document.removeEventListener('visibilitychange', visibilityChanged)
    window.removeEventListener('online', tick)
    window.removeEventListener('offline', tick)
  })
  return { enabled, intervalSeconds, countdown, fetching, paused, error, lastUpdated, intervals, setEnabled, setInterval: setInterval_, resetCountdown, start, stop, refresh: run, activate }
}
