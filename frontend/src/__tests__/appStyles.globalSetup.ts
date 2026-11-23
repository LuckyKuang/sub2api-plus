import { ensureAppStylesCache } from './appStyles'

// Vitest calls this once per run in a plain Node context before any jsdom
// worker starts, so the Tailwind compile never races parallel workers or the
// bounded CPU budget inside a per-suite beforeAll hook.
export default async function setup(): Promise<void> {
  await ensureAppStylesCache()
}
