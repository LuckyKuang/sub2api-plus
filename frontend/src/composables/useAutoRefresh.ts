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

export interface UseAutoRefreshResult {
  enabled: boolean
  intervalSeconds: number
  countdown: number
  fetching: boolean
  paused: boolean
  error: unknown
  lastUpdated: number | null
  intervals: readonly number[]
  setEnabled: (value: boolean) => void
  setInterval: (seconds: number) => void
  resetCountdown: () => void
  start: () => void
  stop: () => void
  refresh: () => Promise<void>
  activate: () => void
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

interface PersistedAutoRefresh {
  enabled?: unknown
  interval_seconds?: unknown
}

function readPersisted(storageKey: string): PersistedAutoRefresh | null {
  try {
    return JSON.parse(localStorage.getItem(storageKey) ?? 'null') as PersistedAutoRefresh | null
  } catch {
    // Invalid or restricted storage uses defaults.
    return null
  }
}

export function useAutoRefresh(options: UseAutoRefreshOptions): UseAutoRefreshResult {
  const intervals = options.intervals ?? [5, 10, 15, 30]

  const [enabled, setEnabled] = useState(() => {
    const saved = readPersisted(options.storageKey)
    if (saved && options.temporaryPause !== true) return saved.enabled === true
    return options.defaultEnabled ?? false
  })
  const [intervalSeconds, setIntervalSeconds] = useState(() => {
    const saved = readPersisted(options.storageKey)
    if (saved && intervals.includes(Number(saved.interval_seconds))) return Number(saved.interval_seconds)
    return options.defaultInterval ?? intervals[intervals.length - 1]
  })
  const [countdown, setCountdownState] = useState(0)
  const [fetching, setFetching] = useState(false)
  const [paused, setPaused] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)

  const timerIdRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const deadlineRef = useRef(0)
  const failuresRef = useRef(0)
  const disposedRef = useRef(false)
  const pendingRef = useRef<Promise<void> | null>(null)
  const retryNotBeforeRef = useRef(0)

  // Latest-value refs so timer callbacks never close over stale state.
  const enabledRef = useRef(enabled)
  enabledRef.current = enabled
  const intervalSecondsRef = useRef(intervalSeconds)
  intervalSecondsRef.current = intervalSeconds
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const fetchingRef = useRef(fetching)
  fetchingRef.current = fetching
  const optionsRef = useRef(options)
  optionsRef.current = options

  const save = useCallback(() => {
    try {
      const current = optionsRef.current
      localStorage.setItem(
        current.storageKey,
        JSON.stringify({
          ...(!current.temporaryPause ? { enabled: enabledRef.current } : {}),
          interval_seconds: intervalSecondsRef.current
        })
      )
    } catch {
      // optional preference
    }
  }, [])

  const resetCountdown = useCallback(() => {
    deadlineRef.current = Math.max(
      Date.now() + Math.max(intervalSecondsRef.current, optionsRef.current.minimumIntervalSeconds?.() ?? 0) * 1000,
      retryNotBeforeRef.current
    )
    setCountdownState(
      enabledRef.current ? Math.max(0, Math.ceil((deadlineRef.current - Date.now()) / 1000)) : 0
    )
  }, [])

  const run = useCallback((): Promise<void> => {
    if (pendingRef.current) return pendingRef.current
    if (
      disposedRef.current ||
      document.hidden ||
      navigator.onLine === false ||
      Date.now() < retryNotBeforeRef.current
    ) {
      return Promise.resolve()
    }
    setFetching(true)
    pendingRef.current = Promise.resolve().then(async () => {
      try {
        await optionsRef.current.onRefresh()
        if (disposedRef.current) return
        setError(null)
        failuresRef.current = 0
        retryNotBeforeRef.current = 0
        setLastUpdated(Date.now())
      } catch (failure) {
        if (disposedRef.current) return
        setError(failure)
        failuresRef.current += 1
        const delay =
          retryAfterSeconds(failure) ??
          Math.min(
            300,
            intervalSecondsRef.current * 2 ** Math.min(failuresRef.current, 5) * (1 + Math.random() * 0.2)
          )
        retryNotBeforeRef.current = Date.now() + Math.max(intervalSecondsRef.current, delay) * 1000
      } finally {
        pendingRef.current = null
        setFetching(false)
        if (!disposedRef.current) resetCountdown()
      }
    })
    return pendingRef.current
  }, [resetCountdown])

  const isPaused = useCallback(() => {
    return (
      document.hidden ||
      navigator.onLine === false ||
      optionsRef.current.active?.() === false ||
      optionsRef.current.shouldPause?.() === true
    )
  }, [])

  const tick = useCallback(() => {
    if (!enabledRef.current || disposedRef.current) return
    const wasPaused = pausedRef.current
    pausedRef.current = isPaused()
    setPaused(pausedRef.current)
    if (pausedRef.current || fetchingRef.current) {
      setCountdownState(0)
      return
    }
    if (wasPaused) deadlineRef.current = Math.max(Date.now(), retryNotBeforeRef.current)
    const next = Math.max(0, Math.ceil((deadlineRef.current - Date.now()) / 1000))
    setCountdownState(next)
    if (next === 0) void run()
  }, [isPaused, run])

  const stop = useCallback(() => {
    if (timerIdRef.current != null) clearInterval(timerIdRef.current)
    timerIdRef.current = undefined
  }, [])

  const start = useCallback(() => {
    if (timerIdRef.current != null || disposedRef.current) return
    resetCountdown()
    timerIdRef.current = setInterval(tick, 1000)
  }, [resetCountdown, tick])

  const setEnabledValue = useCallback(
    (value: boolean) => {
      setEnabled(value)
      enabledRef.current = value
      save()
      if (value) {
        start()
        if (optionsRef.current.temporaryPause && !isPaused()) void run()
      } else {
        stop()
        setCountdownState(0)
        pausedRef.current = false
        setPaused(false)
      }
    },
    [isPaused, run, save, start, stop]
  )

  const setIntervalValue = useCallback(
    (seconds: number) => {
      if (!intervals.includes(seconds)) return
      setIntervalSeconds(seconds)
      intervalSecondsRef.current = seconds
      save()
      resetCountdown()
    },
    [intervals, resetCountdown, save]
  )

  const activate = useCallback(() => {
    if (optionsRef.current.active?.() === false) return
    if (optionsRef.current.temporaryPause) {
      setEnabled(true)
      enabledRef.current = true
      start()
    }
    pausedRef.current = isPaused()
    setPaused(pausedRef.current)
    if (enabledRef.current && !pausedRef.current) void run()
  }, [isPaused, run, start])

  const visibilityChanged = useCallback(() => {
    if (document.hidden) {
      pausedRef.current = true
      setPaused(true)
      setCountdownState(0)
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
      disposedRef.current = true
      stop()
      document.removeEventListener('visibilitychange', visibilityChanged)
      window.removeEventListener('online', tick)
      window.removeEventListener('offline', tick)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Mirrors the Vue watch(options.active, ...): react to activation changes.
  const previousActive = useRef<boolean | undefined>(undefined)
  useEffect(() => {
    if (!options.active) return
    const active = options.active()
    if (previousActive.current !== undefined && active !== previousActive.current) {
      if (active) activate()
      else tick()
    }
    previousActive.current = active
  })

  return {
    enabled,
    intervalSeconds,
    countdown,
    fetching,
    paused,
    error,
    lastUpdated,
    intervals,
    setEnabled: setEnabledValue,
    setInterval: setIntervalValue,
    resetCountdown,
    start,
    stop,
    refresh: run,
    activate
  }
}
