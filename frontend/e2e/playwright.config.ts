import { defineConfig, devices } from '@playwright/test'

if (!process.env.SUB2API_E2E_BASE_URL) {
  throw new Error('SUB2API_E2E_BASE_URL must identify the isolated real application server')
}

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  forbidOnly: true,
  reporter: 'list',
  outputDir: process.env.SUB2API_E2E_OUTPUT_DIR || '/tmp/sub2api-browser-results',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: process.env.SUB2API_E2E_BASE_URL,
    viewport: { width: 1600, height: 1100 },
    locale: 'en-US',
    actionTimeout: 15_000,
    navigationTimeout: 20_000,
    // Real login and account forms contain fixture credentials. Do not retain
    // browser snapshots, traces, or videos containing them.
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
})
