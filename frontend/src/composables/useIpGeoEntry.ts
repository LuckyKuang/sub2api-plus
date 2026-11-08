import { useEffect, useState } from 'react'
import { getEntry, type IpGeoEntry } from '@/utils/ipGeoLookup'

// The geo cache is a plain module-level Map. One shared ticker keeps every
// mounted cell in sync with it, mirroring the Vue reactive Map without adding
// a subscription API to the shared utility module.
const TICK_MS = 250
const subscribers = new Set<() => void>()
let timer: ReturnType<typeof setInterval> | null = null

function ensureTicker() {
  if (timer !== null) return
  timer = setInterval(() => {
    for (const notify of subscribers) notify()
  }, TICK_MS)
}

function maybeStopTicker() {
  if (timer === null || subscribers.size > 0) return
  clearInterval(timer)
  timer = null
}

function subscribe(notify: () => void): () => void {
  subscribers.add(notify)
  ensureTicker()
  return () => {
    subscribers.delete(notify)
    maybeStopTicker()
  }
}

/** Reactive view of one IP's geo entry. */
export function useIpGeoEntry(ip: string): IpGeoEntry {
  const [entry, setEntry] = useState<IpGeoEntry>(() => getEntry(ip))

  useEffect(() => {
    setEntry(getEntry(ip))
    const notify = () => {
      const next = getEntry(ip)
      setEntry((previous) => (previous === next ? previous : next))
    }
    return subscribe(notify)
  }, [ip])

  return entry
}

/** Reactive snapshot of many IPs' geo entries; re-renders when any entry changes. */
export function useIpGeoEntries(ips: string[]): IpGeoEntry[] {
  const key = ips.join('\u0000')
  const [entries, setEntries] = useState<IpGeoEntry[]>(() => ips.map((ip) => getEntry(ip)))

  useEffect(() => {
    const list = key === '' ? [] : key.split('\u0000')
    setEntries(list.map((ip) => getEntry(ip)))
    const notify = () => {
      setEntries((previous) => {
        const next = list.map((ip) => getEntry(ip))
        return next.length === previous.length && next.every((entry, index) => entry === previous[index])
          ? previous
          : next
      })
    }
    return subscribe(notify)
  }, [key])

  return entries
}
