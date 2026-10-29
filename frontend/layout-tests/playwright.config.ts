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
  testMatch: ['overlays.spec.ts', 'user-layout.spec.ts', 'user-audit-workspaces.spec.ts', 'motion.spec.ts'],
  fullyParallel: true,
  workers: 2,
  retries: 0,
  forbidOnly: true,
  // Cold transforms of the full AppLayout graph on a WSL bind mount can
  // exceed 30s; individual geometry/reachability expectations stay at 5s.
  timeout: 90_000,
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
  projects: [...viewports.flatMap(viewport => themes.flatMap(colorScheme => locales.map(locale => ({
    name: `${viewport.width}x${viewport.height}-${colorScheme}-${locale}`,
    testMatch: 'overlays.spec.ts',
    use: { viewport, colorScheme, locale },
  })))),
  ...themes.flatMap(colorScheme => locales.map(locale => ({
    name: `user-layout-${colorScheme}-${locale}`,
    testMatch: ['user-layout.spec.ts', 'user-audit-workspaces.spec.ts'],
    use: { viewport: { width: 1440, height: 900 }, colorScheme, locale },
  }))),
  ...[{ width: 390, height: 844 }, { width: 1440, height: 600 }].flatMap(viewport => themes.map(colorScheme => ({
    name: `product-${viewport.width}-${colorScheme}`,
    testMatch: 'overlays.spec.ts',
    use: { viewport, colorScheme, locale: colorScheme === 'dark' ? 'zh-CN' : 'en-US' },
  }))),
  ...themes.map(colorScheme => ({
    name: `motion-production-${colorScheme}`,
    testMatch: 'motion.spec.ts',
    use: { baseURL: 'http://127.0.0.1:18194', viewport: { width: 1440, height: 900 }, colorScheme, locale: 'en-US', reducedMotion: 'no-preference' as const },
  }))],
  webServer: [{
    cwd: frontend,
    command: 'pnpm exec vite --config layout-tests/vite.config.ts',
    url: 'http://127.0.0.1:18193/layout-tests/index.html',
    timeout: 30_000,
    reuseExistingServer: false,
  }, {
    cwd: frontend,
    command: 'pnpm exec vite build --config layout-tests/motion.vite.config.ts && pnpm exec vite preview --config layout-tests/motion.vite.config.ts',
    url: 'http://127.0.0.1:18194/layout-tests/motion.html',
    timeout: 120_000,
    reuseExistingServer: false,
  }],
})
