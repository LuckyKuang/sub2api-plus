import { defineConfig } from '@playwright/test'
import { existsSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

if (process.platform !== 'linux' || process.env.SUB2API_IN_VALIDATION !== '1' || !existsSync('/etc/sub2api-validation')) {
  throw new Error('Layout regressions must run inside the repository validation container')
}
const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..')

const viewports = [
  { width: 375, height: 812 },
  { width: 768, height: 900 },
  { width: 1000, height: 600 },
  { width: 1440, height: 900 },
]
const themes = ['light', 'dark'] as const
const locales = ['en-US', 'zh-CN']

export default defineConfig({
  testDir: '.',
  testMatch: 'overlays.spec.ts',
  fullyParallel: true,
  workers: 2,
  retries: 0,
  forbidOnly: true,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  reporter: 'list',
  outputDir: process.env.SUB2API_LAYOUT_OUTPUT_DIR || '/tmp/sub2api-layout-results',
  use: {
    browserName: 'chromium',
    baseURL: 'http://127.0.0.1:18193',
    reducedMotion: 'reduce',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: viewports.flatMap(viewport => themes.flatMap(colorScheme => locales.map(locale => ({
    name: `${viewport.width}x${viewport.height}-${colorScheme}-${locale}`,
    use: { viewport, colorScheme, locale },
  })))),
  webServer: {
    cwd: frontend,
    command: 'pnpm exec vite --config layout-tests/vite.config.ts',
    url: 'http://127.0.0.1:18193/layout-tests/index.html',
    timeout: 30_000,
    reuseExistingServer: false,
  },
})
