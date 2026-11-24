import { expect, test, type Page } from '@playwright/test'

// Acceptance source: openspec/changes/fix-ios16-ops-dashboard-layout.
// The real OpsDashboard view runs against intercepted ops APIs. Assertions use
// browser geometry: chart plot regions, canvas sizes, the health hover region,
// and document reachability. Class names or jsdom snapshots cannot prove them.

const now = Date.UTC(2026, 10, 24, 10, 0, 0)

function iso(offsetMinutes: number): string {
  return new Date(now + offsetMinutes * 60_000).toISOString()
}

function overview() {
  return {
    start_time: iso(-60),
    end_time: iso(0),
    platform: '',
    group_id: null,
    health_score: 92,
    system_metrics: {
      db_ok: true,
      redis_ok: true,
      cpu_usage_percent: 21.5,
      memory_usage_percent: 43.2,
      db_conn_active: 3,
      db_conn_idle: 5,
      db_conn_waiting: 0,
      db_max_open_conns: 50,
      redis_conn_total: 4,
      redis_conn_idle: 2,
      redis_pool_size: 20,
      goroutine_count: 120,
    },
    job_heartbeats: [],
    success_count: 1200,
    error_count_total: 12,
    business_limited_count: 2,
    error_count_sla: 8,
    request_count_total: 1200,
    request_count_sla: 1180,
    token_consumed: 530000,
    sla: 0.993,
    error_rate: 0.01,
    upstream_error_rate: 0.004,
    upstream_error_count_excl_429_529: 3,
    upstream_429_count: 1,
    upstream_529_count: 0,
    qps: { current: 4.2, peak: 12.5, avg: 3.1 },
    tps: { current: 118.4, peak: 302.1, avg: 96.7 },
    duration: { p50_ms: 120, p90_ms: 300, p95_ms: 420, p99_ms: 800, avg_ms: 180, max_ms: 1500 },
    ttft: { p50_ms: 90, p90_ms: 210, p95_ms: 280, p99_ms: 520, avg_ms: 120, max_ms: 900 },
  }
}

function throughputTrend() {
  return {
    bucket: 'minute',
    points: Array.from({ length: 8 }, (_, index) => ({
      bucket_start: iso(index * 7 - 56),
      request_count: 60 + index * 4,
      token_consumed: 12000 + index * 900,
      switch_count: index % 3,
      qps: 1.5 + index * 0.2,
      tps: 42 + index * 3,
    })),
    by_platform: [{ platform: 'openai', request_count: 420, token_consumed: 91000 }],
    top_groups: [{ group_id: 1, group_name: 'Fixture group', request_count: 420, token_consumed: 91000 }],
  }
}

function errorTrend(hasErrors: boolean) {
  return {
    bucket: 'minute',
    points: Array.from({ length: 8 }, (_, index) => ({
      bucket_start: iso(index * 7 - 56),
      error_count_total: hasErrors ? 2 + index : 0,
      business_limited_count: hasErrors ? 1 : 0,
      error_count_sla: hasErrors ? 2 + index : 0,
      upstream_error_count_excl_429_529: hasErrors ? 1 : 0,
      upstream_429_count: hasErrors ? 1 : 0,
      upstream_529_count: 0,
    })),
  }
}

function errorDistribution(hasErrors: boolean) {
  return hasErrors
    ? {
        total: 21,
        items: [
          { status_code: 500, total: 4, sla: 4, business_limited: 0 },
          { status_code: 502, total: 6, sla: 6, business_limited: 0 },
          { status_code: 429, total: 11, sla: 11, business_limited: 9 },
        ],
      }
    : { total: 0, items: [] }
}

async function intercept(page: Page, state: { errors: boolean }) {
  await page.route('**/api/v1/**', (route) => {
    const path = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, '')
    let data: unknown = { items: [], total: 0, page: 1, page_size: 20, pages: 0, enabled: false }

    if (path === '/admin/settings') {
      data = {
        ops_monitoring_enabled: true,
        ops_realtime_monitoring_enabled: true,
        ops_query_mode_default: 'auto',
        custom_menu_items: [],
      }
    } else if (path === '/payment/config') {
      data = { enabled: false }
    } else if (path === '/admin/groups/all') {
      data = []
    } else if (path === '/admin/ops/advanced-settings') {
      data = {
        data_retention: {
          cleanup_enabled: true,
          cleanup_schedule: '0 3 * * *',
          error_log_retention_days: 7,
          minute_metrics_retention_days: 3,
          hourly_metrics_retention_days: 30,
        },
        aggregation: { aggregation_enabled: true },
        openai_account_quota_auto_pause: { enabled: false },
        ignore_count_tokens_errors: false,
        ignore_context_canceled: false,
        ignore_no_available_accounts: false,
        ignore_invalid_api_key_errors: false,
        ignore_insufficient_balance_errors: false,
        display_openai_token_stats: false,
        display_alert_events: false,
        auto_refresh_enabled: false,
        auto_refresh_interval_seconds: 30,
      }
    } else if (path === '/admin/ops/settings/metric-thresholds') {
      data = {
        sla_percent_min: 99,
        ttft_p99_ms_max: 2000,
        request_error_rate_percent_max: 5,
        upstream_error_rate_percent_max: 5,
      }
    } else if (path === '/admin/ops/dashboard/snapshot-v2') {
      data = {
        generated_at: iso(0),
        overview: overview(),
        throughput_trend: throughputTrend(),
        error_trend: errorTrend(state.errors),
      }
    } else if (path === '/admin/ops/dashboard/throughput-trend') {
      data = throughputTrend()
    } else if (path === '/admin/ops/dashboard/latency-histogram') {
      data = {
        start_time: iso(-60),
        end_time: iso(0),
        platform: '',
        group_id: null,
        total_requests: 1200,
        buckets: [
          { range: '0-50ms', count: 220 },
          { range: '50-100ms', count: 380 },
          { range: '100-200ms', count: 340 },
          { range: '200-500ms', count: 180 },
          { range: '500ms+', count: 80 },
        ],
      }
    } else if (path === '/admin/ops/dashboard/error-distribution') {
      data = errorDistribution(state.errors)
    } else if (path === '/admin/ops/dashboard/error-trend') {
      data = errorTrend(state.errors)
    } else if (path === '/admin/ops/realtime-traffic') {
      data = {
        enabled: true,
        summary: {
          window: '1min',
          start_time: iso(-1),
          end_time: iso(0),
          platform: '',
          group_id: null,
          qps: { current: 4.2, peak: 12.5, avg: 3.1 },
          tps: { current: 118.4, peak: 302.1, avg: 96.7 },
        },
      }
    } else if (path === '/admin/ops/concurrency') {
      data = {
        enabled: true,
        platform: { openai: { max_capacity: 40, current_in_use: 6, waiting_in_queue: 0 } },
        group: {},
        account: {},
      }
    } else if (path === '/admin/ops/user-concurrency') {
      data = {
        enabled: true,
        user: {
          1: {
            user_id: 1,
            user_email: 'fixture@example.test',
            username: 'Fixture user',
            current_in_use: 1,
            max_capacity: 3,
            load_percentage: 33,
            waiting_in_queue: 0,
          },
        },
      }
    } else if (path === '/admin/ops/account-availability') {
      data = {
        enabled: true,
        platform: {
          openai: { platform: 'openai', total_accounts: 12, available_count: 10, rate_limit_count: 1, error_count: 1 },
        },
        group: {},
        account: {},
      }
    } else if (path === '/admin/ops/runtime/logging') {
      data = { level: 'info', persist_access_logs: true, enable_sampling: true }
    } else if (path === '/admin/ops/system-logs/health') {
      data = {
        queue_depth: 0,
        queue_capacity: 1000,
        dropped_count: 0,
        write_failed_count: 0,
        written_count: 42,
        avg_write_delay_ms: 1.2,
      }
    } else if (path.startsWith('/admin/ops/system-logs')) {
      data = { items: [], total: 0, page: 1, page_size: 20, pages: 0 }
    }

    return route.fulfill({ json: { code: 0, data } })
  })
}

async function openDashboard(page: Page, theme: string, locale: string, fullscreen = false) {
  await page.goto(
    `/layout-tests/index.html?view=admin-layout&ops=1&theme=${theme}&locale=${locale}${fullscreen ? '&fullscreen=1' : ''}`
  )
}

async function geometry(page: Page) {
  return page.evaluate(() => {
    const box = (element: Element | null) => {
      if (!element) return null
      const rect = element.getBoundingClientRect()
      return { top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right, height: rect.height, width: rect.width }
    }
    const byId = (id: string) => box(document.querySelector(`[data-testid="${id}"]`))
    const canvasElement = (id: string) => document.querySelector(`[data-testid="${id}"] canvas`)
    const canvasOf = (id: string) => box(canvasElement(id))
    // Before the fix a chart body had no dedicated plot region; measure the
    // canvas parent so the baseline run reports its content-sized geometry.
    const plotOf = (plotId: string, cardId: string) =>
      box(document.querySelector(`[data-testid="${plotId}"]`) ?? canvasElement(cardId)?.parentElement ?? null)
    const scroller = document.scrollingElement as HTMLElement
    return {
      scrollHeight: scroller.scrollHeight,
      clientHeight: scroller.clientHeight,
      scrollTop: scroller.scrollTop,
      innerWidth: window.innerWidth,
      innerHeight: window.innerHeight,
      overview: byId('ops-overview-card'),
      health: byId('ops-health-card'),
      concurrency: byId('ops-concurrency-card'),
      latency: plotOf('ops-latency-plot', 'ops-latency-chart'),
      latencyCanvas: canvasOf('ops-latency-chart'),
      errorDistribution: plotOf('ops-error-distribution-plot', 'ops-error-distribution-chart'),
      errorTrend: plotOf('ops-error-trend-plot', 'ops-error-trend-chart'),
    }
  })
}

const PHONE = { width: 428, height: 745 }

test('phone overview keeps the health hover region inside its card', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await intercept(page, { errors: false })
  await page.setViewportSize(PHONE)
  await openDashboard(page, theme, locale)

  const latency = page.getByTestId('ops-latency-chart')
  await expect(latency.locator('canvas')).toBeVisible()
  const first = await geometry(page)
  expect(first.overview).not.toBeNull()
  expect(first.health).not.toBeNull()
  expect(first.concurrency).not.toBeNull()

  // The hover surface must end inside the overview card, before the next card.
  expect(first.health!.bottom).toBeLessThanOrEqual(first.overview!.bottom + 1)
  expect(first.concurrency!.top).toBeGreaterThanOrEqual(first.overview!.bottom - 1)

  // The ring and diagnosis entry stay usable.
  await expect(page.getByTestId('ops-health-card').locator('svg').first()).toBeVisible()
  await expect(page.getByTestId('ops-health-card')).toHaveAttribute('class', /group/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
})

test('phone analysis charts keep definite plot heights with late error data', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  const state = { errors: false }
  await intercept(page, state)
  await page.setViewportSize(PHONE)
  await openDashboard(page, theme, locale)

  const latency = page.getByTestId('ops-latency-chart')
  await expect(latency.locator('canvas')).toBeVisible()
  await expect(page.getByTestId('ops-error-distribution-chart').locator('canvas')).toHaveCount(0)
  await expect(page.getByTestId('ops-error-trend-chart').locator('canvas')).toHaveCount(0)

  const first = await geometry(page)
  // The verified design value; a content-sized card would report the canvas
  // default height instead and reproduce the WebKit resize feedback loop.
  expect(first.latency!.height).toBeCloseTo(200, 0)
  expect(first.latencyCanvas!.height).toBeCloseTo(200, 0)

  const samples = [first]
  for (let index = 0; index < 6; index += 1) {
    await page.waitForTimeout(2_000)
    samples.push(await geometry(page))
  }
  const baseline = samples[0]
  for (const sample of samples.slice(1)) {
    expect(Math.abs(sample.latency!.height - baseline.latency!.height)).toBeLessThanOrEqual(1)
    expect(Math.abs(sample.latencyCanvas!.height - baseline.latencyCanvas!.height)).toBeLessThanOrEqual(1)
    expect(Math.abs(sample.scrollHeight - baseline.scrollHeight)).toBeLessThanOrEqual(1)
  }

  // A later refresh returns error data; all three charts must draw and stay stable.
  state.errors = true
  await page.getByTestId('ops-refresh').click()
  await expect(page.getByTestId('ops-error-distribution-chart').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('ops-error-trend-chart').locator('canvas')).toBeVisible()
  await expect(page.getByTestId('ops-error-distribution-plot')).toBeVisible()

  const withErrors = await geometry(page)
  for (const plot of [withErrors.latency, withErrors.errorDistribution, withErrors.errorTrend]) {
    expect(plot).not.toBeNull()
    expect(plot!.height).toBeCloseTo(200, 0)
  }
  const errorCards = ['ops-latency-chart', 'ops-error-distribution-chart', 'ops-error-trend-chart']
  for (const id of errorCards) {
    const card = page.getByTestId(id)
    await expect(card.locator('canvas')).toBeVisible()
    const rect = await card.evaluate((element) => {
      const parent = element.getBoundingClientRect()
      return { left: parent.left, right: parent.right, width: element.clientWidth, content: element.scrollWidth }
    })
    expect(rect.content).toBeLessThanOrEqual(rect.width + 1)
  }
  // Multi-row legend and detail actions remain inside the error-trend card.
  const errorTrendCard = page.getByTestId('ops-error-trend-chart')
  await expect(errorTrendCard.locator('button')).toHaveCount(2)
  for (const button of await errorTrendCard.locator('button').all()) {
    const box = await button.boundingBox()
    const card = await errorTrendCard.boundingBox()
    expect(box!.y).toBeGreaterThanOrEqual(card!.y)
    expect(box!.x + box!.width).toBeLessThanOrEqual(card!.x + card!.width + 1)
  }

  const refreshed = [withErrors]
  for (let index = 0; index < 3; index += 1) {
    await page.waitForTimeout(2_000)
    refreshed.push(await geometry(page))
  }
  const errorBaseline = refreshed[0]
  for (const sample of refreshed.slice(1)) {
    expect(Math.abs(sample.errorDistribution!.height - errorBaseline.errorDistribution!.height)).toBeLessThanOrEqual(1)
    expect(Math.abs(sample.errorTrend!.height - errorBaseline.errorTrend!.height)).toBeLessThanOrEqual(1)
    expect(Math.abs(sample.scrollHeight - errorBaseline.scrollHeight)).toBeLessThanOrEqual(1)
  }
})

test('phone overview bottom stays reachable after reload and rotation', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await intercept(page, { errors: true })
  await page.setViewportSize(PHONE)
  await openDashboard(page, theme, locale)

  const lastCard = page.getByTestId('ops-error-trend-chart')
  await expect(lastCard.locator('canvas')).toBeVisible()

  const checkBottom = async () => {
    await page.evaluate(() => window.scrollTo(0, document.scrollingElement!.scrollHeight))
    expect
      .poll(
        async () => page.evaluate(() => {
          const scroller = document.scrollingElement!
          return Math.round(scroller.scrollHeight - scroller.clientHeight - scroller.scrollTop)
        }),
        { timeout: 5_000 }
      )
      .toBeLessThanOrEqual(2)
    const box = await lastCard.boundingBox()
    expect(box!.y + box!.height).toBeLessThanOrEqual(PHONE.height + 1)
  }

  await checkBottom()

  await page.reload()
  await expect(lastCard.locator('canvas')).toBeVisible()
  await checkBottom()

  await page.setViewportSize({ width: 844, height: 428 })
  await expect(lastCard.locator('canvas')).toBeVisible()
  await page.setViewportSize(PHONE)
  await expect(lastCard.locator('canvas')).toBeVisible()
  await page.waitForTimeout(1_000)
  await checkBottom()
})

test('wide overview keeps the three-up chart grid and actions', async ({ page }, info) => {
  const theme = info.project.use.colorScheme === 'dark' ? 'dark' : 'light'
  const locale = info.project.use.locale?.startsWith('zh') ? 'zh' : 'en'
  await intercept(page, { errors: true })
  await page.setViewportSize({ width: 1440, height: 900 })

  for (const fullscreen of [false, true]) {
    await openDashboard(page, theme, locale, fullscreen)
    const cards = ['ops-latency-chart', 'ops-error-distribution-chart', 'ops-error-trend-chart'].map((id) =>
      page.getByTestId(id)
    )
    for (const card of cards) await expect(card.locator('canvas')).toBeVisible()
    const boxes = await Promise.all(cards.map((card) => card.boundingBox()))
    expect(Math.abs(boxes[0]!.y - boxes[1]!.y)).toBeLessThanOrEqual(1)
    expect(Math.abs(boxes[1]!.y - boxes[2]!.y)).toBeLessThanOrEqual(1)
    expect(boxes[1]!.x).toBeGreaterThan(boxes[0]!.x)
    expect(boxes[2]!.x).toBeGreaterThan(boxes[1]!.x)
    for (const box of boxes) expect(box!.width).toBeGreaterThan(300)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
  }

  // The error-detail action remains available on wide screens.
  await openDashboard(page, theme, locale)
  const errorTrend = page.getByTestId('ops-error-trend-chart')
  await expect(errorTrend.locator('canvas')).toBeVisible()
  await errorTrend.locator('button').first().click()
  await expect(page.locator('.modal-overlay').first()).toBeVisible()
  await page.keyboard.press('Escape')
})
