import { useCallback, useEffect, useRef, useState } from 'react'

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

function readSavedPreference(storageKey: string, temporaryPause: boolean): { enabled?: boolean; intervalSeconds?: number } {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null') as
      | { enabled?: boolean; interval_seconds?: number }
      | null
    if (!saved) return {}
    return {
      enabled: temporaryPause ? undefined : saved.enabled === true,
      intervalSeconds:
        typeof saved.interval_seconds === 'number' && Number.isFinite(saved.interval_seconds)
          ? saved.interval_seconds
          : undefined
    }
  } catch {
    /* Invalid or restricted storage uses defaults. */
    return {}
  }
}

export interface AutoRefreshState {
  enabled: boolean
  intervalSeconds: number
  countdown: number
  fetching: boolean
  paused: boolean
  error: unknown
  lastUpdated: number | null
  intervals: readonly number[]
  /** Report a failure outside the refresh loop (e.g. a manual chart reload). */
  reportError: (failure: unknown) => void
  setEnabled: (value: boolean) => void
  setInterval: (seconds: number) => void
  resetCountdown: () => void
  start: () => void
  stop: () => void
  refresh: () => Promise<void>
  activate: () => void
}

export function useAutoRefresh(options: UseAutoRefreshOptions): AutoRefreshState {
  const intervals = options.intervals ?? [5, 10, 15, 30]
  const saved = readSavedPreference(options.storageKey, options.temporaryPause === true)

  const [enabled, setEnabledState] = useState(saved.enabled ?? options.defaultEnabled ?? false)
  const [intervalSeconds, setIntervalSecondsState] = useState(() => {
    if (saved.intervalSeconds !== undefined && intervals.includes(saved.intervalSeconds)) {
      return saved.intervalSeconds
    }
    return options.defaultInterval ?? intervals[intervals.length - 1]
  })
  const [countdown, setCountdown] = useState(0)
  const [fetching, setFetching] = useState(false)
  const [paused, setPaused] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)

  const deadline = useRef(0)
  const failures = useRef(0)
  const disposed = useRef(false)
  const pending = useRef<Promise<void> | null>(null)
  const retryNotBefore = useRef(0)
  const timerId = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const optionsRef = useRef(options)
  optionsRef.current = options
  const intervalsRef = useRef(intervals)
  intervalsRef.current = intervals

  const save = useCallback((nextEnabled: boolean, nextInterval: number) => {
    try {
      localStorage.setItem(
        optionsRef.current.storageKey,
        JSON.stringify({
          ...(optionsRef.current.temporaryPause ? {} : { enabled: nextEnabled }),
          interval_seconds: nextInterval
        })
      )
    } catch {
      /* optional preference */
    }
  }, [])

  const resetCountdown = useCallback(() => {
    const current = optionsRef.current
    deadline.current = Math.max(
      Date.now() + Math.max(intervalSeconds, current.minimumIntervalSeconds?.() ?? 0) * 1000,
      retryNotBefore.current
    )
    setCountdown(enabled ? Math.max(0, Math.ceil((deadline.current - Date.now()) / 1000)) : 0)
  }, [enabled, intervalSeconds])

  const isPaused = useCallback(() => {
    const current = optionsRef.current
    return (
      document.hidden ||
      navigator.onLine === false ||
      current.active?.() === false ||
      current.shouldPause?.() === true
    )
  }, [])

  const run = useCallback((): Promise<void> => {
    if (pending.current) return pending.current
    const current = optionsRef.current
    if (
      disposed.current ||
      document.hidden ||
      navigator.onLine === false ||
      Date.now() < retryNotBefore.current
    ) {
      return Promise.resolve()
    }
    setFetching(true)
    pending.current = Promise.resolve().then(async () => {
      try {
        await current.onRefresh()
        if (disposed.current) return
        setError(null)
        failures.current = 0
        retryNotBefore.current = 0
        setLastUpdated(Date.now())
      } catch (failure) {
        if (disposed.current) return
        setError(failure)
        failures.current += 1
        const delay =
          retryAfterSeconds(failure) ??
          Math.min(
            300,
            intervalSeconds * 2 ** Math.min(failures.current, 5) * (1 + Math.random() * 0.2)
          )
        retryNotBefore.current = Date.now() + Math.max(intervalSeconds, delay) * 1000
      } finally {
        pending.current = null
        setFetching(false)
        if (!disposed.current) resetCountdown()
      }
    })
    return pending.current
  }, [intervalSeconds, resetCountdown])

  const tick = useCallback(() => {
    if (!enabled || disposed.current) return
    const wasPaused = paused
    const nextPaused = isPaused()
    setPaused(nextPaused)
    if (nextPaused || fetching) {
      setCountdown(0)
      return
    }
    if (wasPaused) deadline.current = Math.max(Date.now(), retryNotBefore.current)
    const remaining = Math.max(0, Math.ceil((deadline.current - Date.now()) / 1000))
    setCountdown(remaining)
    if (remaining === 0) void run()
  }, [enabled, fetching, isPaused, paused, run])

  const stop = useCallback(() => {
    if (timerId.current != null) clearInterval(timerId.current)
    timerId.current = undefined
  }, [])

  const start = useCallback(() => {
    if (timerId.current != null || disposed.current) return
    resetCountdown()
    timerId.current = setInterval(tick, 1000)
  }, [resetCountdown, tick])

  const activate = useCallback(() => {
    const current = optionsRef.current
    if (current.active?.() === false) return
    if (current.temporaryPause) {
      setEnabledState(true)
      start()
    }
    setPaused(isPaused())
    if (enabled && !isPaused()) void run()
  }, [enabled, isPaused, run, start])

  const setEnabled = useCallback(
    (value: boolean) => {
      const current = optionsRef.current
      setEnabledState(value)
      save(value, intervalSeconds)
      if (value) {
        start()
        if (current.temporaryPause && !isPaused()) void run()
      } else {
        stop()
        setCountdown(0)
        setPaused(false)
      }
    },
    [intervalSeconds, isPaused, run, save, start, stop]
  )

  const setIntervalSeconds = useCallback(
    (seconds: number) => {
      if (!intervalsRef.current.includes(seconds)) return
      setIntervalSecondsState(seconds)
      save(enabled, seconds)
      resetCountdown()
    },
    [enabled, resetCountdown, save]
  )

  const visibilityChanged = useCallback(() => {
    if (document.hidden) {
      setPaused(true)
      setCountdown(0)
    } else if (optionsRef.current.temporaryPause) {
      activate()
    } else {
      tick()
    }
  }, [activate, tick])

  useEffect(() => {
    if (enabled) start()
    document.addEventListener('visibilitychange', visibilityChanged)
    window.addEventListener('online', tick)
    window.addEventListener('offline', tick)
    return () => {
      disposed.current = true
      stop()
      document.removeEventListener('visibilitychange', visibilityChanged)
      window.removeEventListener('online', tick)
      window.removeEventListener('offline', tick)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const reportError = useCallback((failure: unknown) => {
    setError(failure)
  }, [])

  return {
    enabled,
    intervalSeconds,
    countdown,
    fetching,
    paused,
    error,
    lastUpdated,
    intervals,
    reportError,
    setEnabled,
    setInterval: setIntervalSeconds,
    resetCountdown,
    start,
    stop,
    refresh: run,
    activate
  }
}
