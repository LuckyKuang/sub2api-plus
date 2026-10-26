import { expect, type APIRequestContext, type APIResponse, type Locator, type Page } from '@playwright/test'

export const platforms = ['openai', 'grok', 'deepseek', 'kimi', 'minimax', 'stepfun', 'zhipu'] as const
export type Platform = (typeof platforms)[number]
export type AccountType = 'apikey' | 'oauth'
export type RouteName = 'healthy' | 'refused' | 'held' | 'repaired'

export interface ProxyFixture {
  host: string
  ports: Record<RouteName, number>
  username: string
  password: string
}

export interface FixtureState {
  connections: Record<RouteName, number>
  held_started: boolean
  credentials_leaked: boolean
  direct_connections: number
}

export interface AccountSnapshot {
  id: number
  name: string
  platform: Platform
  type: AccountType
  proxy_id: number | null
  credentials: Record<string, unknown>
}

export interface SavedProxy {
  id: number
  name: string
  host: string
  port: number
  protocol: string
  username?: string
  password?: string
  country?: string
  account_count?: number
}

function requiredEnvironment(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required; browser business tests cannot be skipped`)
  return value
}

export async function data<T>(response: APIResponse): Promise<T> {
  const envelope = await response.json() as { code: number; reason?: unknown; data: T }
  // Only a bounded machine reason may reach diagnostics. Response bodies and
  // messages can contain credentials and must never be copied into test logs.
  const reason = typeof envelope.reason === 'string' && /^[A-Z][A-Z0-9_]{0,95}$/.test(envelope.reason)
    ? ` (${envelope.reason})` : ''
  expect(response.ok(), `${new URL(response.url()).pathname} returned HTTP ${response.status()}${reason}`).toBeTruthy()
  expect(envelope.code).toBe(0)
  return envelope.data
}

export class BusinessFixture {
  private readonly origin = requiredEnvironment('SUB2API_E2E_FIXTURE_URL')

  constructor(private readonly request: APIRequestContext) {}

  private url(path: string): string {
    return `${this.origin.replace(/\/$/, '')}/control/${path}`
  }

  async reset(): Promise<void> {
    const response = await this.request.post(this.url('reset'), { timeout: 5_000 })
    expect(response.ok()).toBeTruthy()
  }

  async info(): Promise<ProxyFixture> {
    const response = await this.request.get(this.url('info'), { timeout: 5_000 })
    expect(response.ok()).toBeTruthy()
    return response.json() as Promise<ProxyFixture>
  }

  async state(): Promise<FixtureState> {
    const response = await this.request.get(this.url('state'), { timeout: 5_000 })
    expect(response.ok()).toBeTruthy()
    return response.json() as Promise<FixtureState>
  }

  async release(): Promise<void> {
    const response = await this.request.post(this.url('release'), { timeout: 5_000 })
    expect(response.ok()).toBeTruthy()
  }

  async account(id: number): Promise<AccountSnapshot> {
    const response = await this.request.get(this.url(`accounts/${id}`), { timeout: 5_000 })
    expect(response.ok()).toBeTruthy()
    return response.json() as Promise<AccountSnapshot>
  }
}

// Authentication always traverses the real login form, HTTP auth handler and
// PostgreSQL-backed user service. Only a presentation preference is preloaded.
export async function login(page: Page): Promise<string> {
  await page.addInitScript(() => localStorage.setItem('sub2api_locale', 'en'))
  await page.goto('/login')
  await page.locator('#email').fill(requiredEnvironment('SUB2API_E2E_ADMIN_EMAIL'))
  await page.locator('#password').fill(requiredEnvironment('SUB2API_E2E_ADMIN_PASSWORD'))
  const response = page.waitForResponse(candidate =>
    new URL(candidate.url()).pathname === '/api/v1/auth/login' && candidate.request().method() === 'POST', { timeout: 15_000 })
  await page.locator('button[type="submit"]').click()
  const accepted = await response
  expect(accepted.ok()).toBeTruthy()
  const loginResult = await accepted.json() as { data: { user: { id: number } } }
  await expect.poll(() => page.evaluate(() => localStorage.getItem('auth_token'))).not.toBeNull()
  const token = await page.evaluate(() => localStorage.getItem('auth_token'))
  expect(token).toBeTruthy()
  // The guide is a presentation preference, separate from authentication. Its
  // backdrop would otherwise interrupt unrelated management actions.
  await page.evaluate(id => localStorage.setItem(`admin_guide_${id}_admin_v4_interactive`, 'true'), loginResult.data.user.id)
  const compliance = await data<{ required: boolean; ack_phrase_en: string }>(await page.request.get('/api/v1/admin/compliance', {
    headers: adminHeaders(token!), timeout: 10_000,
  }))
  if (compliance.required) {
    await page.goto('/admin/accounts')
    const dialog = page.getByRole('dialog', { name: 'Deployment and Operation Compliance Acknowledgment', exact: true })
    await dialog.locator('#admin-compliance-phrase').fill(compliance.ack_phrase_en)
    const acknowledged = page.waitForResponse(candidate =>
      new URL(candidate.url()).pathname === '/api/v1/admin/compliance/accept' && candidate.request().method() === 'POST', { timeout: 15_000 })
    await dialog.getByRole('button', { name: 'Acknowledge and Continue', exact: true }).click()
    expect((await acknowledged).ok()).toBeTruthy()
    await expect(dialog).not.toBeVisible()
  }
  return token!
}

export function adminHeaders(token: string): Record<string, string> {
  return { Authorization: `Bearer ${token}`, 'Accept-Language': 'en' }
}

export function tableRow(page: Page, name: string): Locator {
  return page.getByRole('row').filter({ has: page.getByText(name, { exact: true }) })
}

function inputByLabel(container: Locator, label: string): Locator {
  return container.locator('label').filter({ hasText: new RegExp(`^${label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`) })
    .locator('..').locator('input').first()
}

export async function createProxy(page: Page, fixture: ProxyFixture, name: string, route: RouteName): Promise<SavedProxy> {
  await page.goto('/admin/proxies')
  await page.locator('.table-page-layout > .layout-section-fixed')
    .getByRole('button', { name: 'Create Proxy', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Create Proxy', exact: true })
  await dialog.getByPlaceholder('Enter proxy name', { exact: true }).fill(name)
  await inputByLabel(dialog, 'Host').fill(fixture.host)
  await inputByLabel(dialog, 'Port').fill(String(fixture.ports[route]))
  await inputByLabel(dialog, 'Username (Optional)').fill(fixture.username)
  await inputByLabel(dialog, 'Password (Optional)').fill(fixture.password)
  const created = page.waitForResponse(response =>
    new URL(response.url()).pathname === '/api/v1/admin/proxies' && response.request().method() === 'POST', { timeout: 15_000 })
  await page.locator('button[form="create-proxy-form"]').click()
  const response = await created
  expect(response.ok()).toBeTruthy()
  const envelope = await response.json() as { data: SavedProxy }
  await expect(dialog).not.toBeVisible()
  await expect(tableRow(page, name)).toContainText(`${fixture.host}:${fixture.ports[route]}`)
  return envelope.data
}

export async function editProxyRoute(page: Page, fixture: ProxyFixture, proxy: SavedProxy, route: RouteName): Promise<void> {
  await tableRow(page, proxy.name).getByRole('button', { name: 'Edit', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Edit Proxy', exact: true })
  await inputByLabel(dialog, 'Host').fill(fixture.host)
  await inputByLabel(dialog, 'Port').fill(String(fixture.ports[route]))
  const updated = page.waitForResponse(response =>
    new URL(response.url()).pathname === `/api/v1/admin/proxies/${proxy.id}` && response.request().method() === 'PUT', { timeout: 15_000 })
  await page.locator('button[form="edit-proxy-form"]').click()
  expect((await updated).ok()).toBeTruthy()
  await expect(dialog).not.toBeVisible()
  await expect(tableRow(page, proxy.name)).toContainText(`${fixture.host}:${fixture.ports[route]}`)
}

export async function bindAccount(page: Page, account: AccountSnapshot, proxy: SavedProxy | null): Promise<void> {
  await page.goto('/admin/accounts')
  await tableRow(page, account.name).getByRole('button', { name: 'Edit', exact: true }).click()
  const form = page.locator('#edit-account-form')
  const selector = form.locator('label').filter({ hasText: /^Proxy$/ }).locator('..')
  await selector.locator('.select-trigger').click()
  await selector.locator('.select-option').filter({
    has: page.getByText(proxy?.name || 'No Proxy', { exact: true }),
  }).click()
  const saved = page.waitForResponse(response =>
    new URL(response.url()).pathname === `/api/v1/admin/accounts/${account.id}` && response.request().method() === 'PUT', { timeout: 15_000 })
  await page.locator('button[form="edit-account-form"]').click()
  const response = await saved
  expect(response.ok()).toBeTruthy()
  const body = response.request().postDataJSON() as { proxy_id: number }
  expect(body.proxy_id).toBe(proxy?.id || 0)
  await expect(form).not.toBeVisible()
}

export async function dismissNotifications(page: Page): Promise<void> {
  const close = page.getByRole('button', { name: 'Close notification', exact: true })
  for (;;) {
    const before = await close.count()
    if (before === 0) return
    await close.first().click({ timeout: 5_000 })
    // A leaving Vue transition still owns its DOM. Wait for that removed
    // toast before selecting the next button, rather than clicking it again.
    await expect.poll(() => close.count(), { timeout: 5_000 }).toBeLessThan(before)
  }
}
