import { expect, test } from '@playwright/test'
import {
  adminHeaders, bindAccount, BusinessFixture, createProxy, data, dismissNotifications,
  editProxyRoute, login, platforms, tableRow,
  type AccountSnapshot, type AccountType, type SavedProxy,
} from './support'

// The browser, Vue router/components, HTTP handlers, production services,
// PostgreSQL and Redis are real. The isolated control fixture replaces only
// outside proxy/provider destinations and independently reads saved DB rows.
// No browser route interception, Axios adapter or forged auth state is used.
for (const platform of platforms) {
  for (const type of ['apikey', 'oauth'] as const satisfies readonly AccountType[]) {
    test(`${platform} ${type}: real browser proxy binding, failure, repair and deletion`, async ({ page, request }, testInfo) => {
      const step = <T>(title: string, run: () => Promise<T>) => test.step(title, run, { timeout: 25_000 })
      const fixture = new BusinessFixture(request)
      const endpoints = await step('reset isolated network observations', async () => {
        await fixture.reset()
        return fixture.info()
      })
      const token = await step('real administrator login and compliance acknowledgment', () => login(page))
      const headers = adminHeaders(token)
      const options = { headers, timeout: 15_000 }
      const name = `browser-${platform}-${type}-${testInfo.workerIndex}`
      const secretKey = type === 'oauth' ? 'access_token' : 'api_key'
      const credentials: Record<string, string> = {
        [secretKey]: `isolated-${platform}-${type}-credential`,
      }
      if (type === 'apikey') credentials.base_url = 'https://provider.e2e.invalid/v1'
      if (platform === 'stepfun' && type === 'oauth') {
        // A Step Plan fixture has the same saved grant contract as browser
        // authorization. It is a static model key, not a PAYG API-key account
        // or an OAuth refresh token. This setup does not claim login coverage.
        Object.assign(credentials, {
          oauth_provider: 'stepfun', oauth_region: 'cn', account_mode: 'coding',
          api_protocol: 'chat_completions', base_url: 'https://api.stepfun.com/step_plan/v1',
        })
      }
      const account = await step('prepare an account through the real administrator API', async () => data<AccountSnapshot>(
        await request.post('/api/v1/admin/accounts', {
          ...options,
          data: { name, platform, type, credentials, concurrency: 1, priority: 1, rate_multiplier: 1, group_ids: [] },
        }),
      ))
      const proxyIDs: number[] = []
      let businessError: { cause: unknown } | undefined
      const assertAccount = async (proxyID: number | null) => {
        const saved = await fixture.account(account.id)
        expect(saved).toMatchObject({ id: account.id, name, platform, type, proxy_id: proxyID })
        expect(saved.credentials[secretKey]).toBe(credentials[secretKey])
      }

      try {
        const proxy = await step('create a proxy through the browser and retain the unbound credential owner', async () => {
          await assertAccount(null)
          const created = await createProxy(page, endpoints, `${name}-proxy`, 'healthy')
          proxyIDs.push(created.id)
          expect(created).toMatchObject({ protocol: 'http', port: endpoints.ports.healthy })
          return created
        })
        await step('bind the account through its editor and verify PostgreSQL persistence', async () => {
          await bindAccount(page, account, proxy)
          await assertAccount(proxy.id)
        })
        await step('reject bound proxy deletion in the UI and HTTP service', async () => {
          await page.goto('/admin/proxies')
          await expect(tableRow(page, proxy.name).getByRole('button', { name: '1 accounts', exact: true })).toBeVisible()
          const boundProxies = await data<SavedProxy[]>(await request.get('/api/v1/admin/proxies/all?with_count=true', options))
          expect(boundProxies.find(candidate => candidate.id === proxy.id)?.account_count).toBe(1)
          const deletions: string[] = []
          page.on('request', event => {
            if (event.method() === 'DELETE') deletions.push(new URL(event.url()).pathname)
          })
          await tableRow(page, proxy.name).getByRole('button', { name: 'Delete', exact: true }).click()
          await expect(page.getByText('This proxy is in use and cannot be deleted', { exact: true })).toBeVisible()
          expect(deletions).toEqual([])
          const blocked = await request.delete(`/api/v1/admin/proxies/${proxy.id}`, options)
          expect(blocked.status()).toBe(409)
          await assertAccount(proxy.id)
        })
        await step('edit an idle route and observe a real proxy failure without unbinding', async () => {
          await editProxyRoute(page, endpoints, proxy, 'refused')
          await assertAccount(proxy.id)
          const failed = page.waitForResponse(response =>
            new URL(response.url()).pathname === `/api/v1/admin/proxies/${proxy.id}/test`, { timeout: 15_000 })
          await tableRow(page, proxy.name).getByRole('button', { name: 'Test Connection', exact: true }).click()
          const failedResponse = await failed
          expect(failedResponse.ok()).toBeTruthy()
          expect((await failedResponse.json()).data.success).toBe(false)
          await expect(tableRow(page, proxy.name)).toContainText('Connection failed')
          expect((await fixture.state()).connections.refused).toBeGreaterThan(0)
          await assertAccount(proxy.id)
        })
        await step('replace an in-flight route and reject the actual late old success', async () => {
          await editProxyRoute(page, endpoints, proxy, 'held')
          await dismissNotifications(page)
          const oldProbe = page.waitForResponse(response =>
            new URL(response.url()).pathname === `/api/v1/admin/proxies/${proxy.id}/test`, { timeout: 15_000 })
          await tableRow(page, proxy.name).getByRole('button', { name: 'Test Connection', exact: true }).click()
          await expect.poll(async () => (await fixture.state()).held_started).toBe(true)
          await editProxyRoute(page, endpoints, proxy, 'repaired')
          await dismissNotifications(page)
          await fixture.release()
          const oldResponse = await oldProbe
          expect(oldResponse.ok()).toBeTruthy()
          expect((await oldResponse.json()).data.success).toBe(true)
          await expect(tableRow(page, proxy.name).getByRole('button', { name: 'Test Connection', exact: true })).toBeEnabled()
          await expect(tableRow(page, proxy.name)).not.toContainText('STALE')
          await expect(page.getByText(/^Proxy is working!/)).not.toBeVisible()
          await assertAccount(proxy.id)
          const persistedProxies = await data<SavedProxy[]>(await request.get('/api/v1/admin/proxies/all?with_count=true', options))
          const persistedProxy = persistedProxies.find(candidate => candidate.id === proxy.id)!
          expect(persistedProxy).toBeDefined()
          expect(persistedProxy.port).toBe(endpoints.ports.repaired)
          expect(persistedProxy.country).not.toBe('STALE')
        })
        await step('a fresh probe certifies the repaired endpoint', async () => {
          const freshProbe = page.waitForResponse(response =>
            new URL(response.url()).pathname === `/api/v1/admin/proxies/${proxy.id}/test`, { timeout: 15_000 })
          await tableRow(page, proxy.name).getByRole('button', { name: 'Test Connection', exact: true }).click()
          const repairedResponse = await freshProbe
          expect(repairedResponse.ok()).toBeTruthy()
          expect((await repairedResponse.json()).data).toMatchObject({ success: true, country: 'NL' })
          await expect(tableRow(page, proxy.name)).toContainText('NL')
          await expect(page.getByText(/^Proxy is working!/)).toBeVisible()
          await assertAccount(proxy.id)
        })
        await step('explicitly unbind through the editor and store SQL null', async () => {
          await bindAccount(page, account, null)
          await assertAccount(null)
        })
        await step('delete the unbound proxy through the confirmation dialog', async () => {
          await page.goto('/admin/proxies')
          await tableRow(page, proxy.name).getByRole('button', { name: 'Delete', exact: true }).click()
          const deleted = page.waitForResponse(response =>
            new URL(response.url()).pathname === `/api/v1/admin/proxies/${proxy.id}` && response.request().method() === 'DELETE', { timeout: 15_000 })
          await page.getByRole('dialog', { name: 'Delete Proxy', exact: true }).getByRole('button', { name: 'Delete', exact: true }).click()
          expect((await deleted).ok()).toBeTruthy()
          await expect(tableRow(page, proxy.name)).toHaveCount(0)
          expect((await request.get(`/api/v1/admin/proxies/${proxy.id}`, options)).status()).toBe(404)
        })
        await step('recreate and bind a distinct proxy while preserving the same credential owner', async () => {
          const replacement = await createProxy(page, endpoints, `${name}-replacement`, 'healthy')
          proxyIDs.push(replacement.id)
          expect(replacement.id).not.toBe(proxy.id)
          await bindAccount(page, account, replacement)
          await assertAccount(replacement.id)
        })
        await step('verify actual transport observations forbid direct fallback and leaked credentials', async () => {
          const state = await fixture.state()
          expect(state.connections.held).toBeGreaterThan(0)
          expect(state.connections.repaired).toBeGreaterThan(0)
          expect(state.direct_connections).toBe(0)
          expect(state.credentials_leaked).toBe(false)
        })
      } catch (cause) {
        businessError = { cause }
      }
      try {
        await step('remove disposable account and proxy records', async () => {
          await fixture.release()
          const removed = await request.delete(`/api/v1/admin/accounts/${account.id}`, options)
          expect(removed.ok()).toBeTruthy()
          for (const id of proxyIDs) {
            const removedProxy = await request.delete(`/api/v1/admin/proxies/${id}`, options)
            expect(removedProxy.ok()).toBeTruthy()
          }
        })
      } catch (cleanupError) {
        if (!businessError) throw cleanupError
        // Preserve the primary failed business stage. The outer runner also
        // drops this entire random database, including partial fixtures.
        testInfo.annotations.push({ type: 'cleanup error after business failure', description: String(cleanupError) })
      }
      if (businessError) throw businessError.cause
    })
  }
}

// This is a separate group-configuration acceptance flow. It does not extend
// the seven-provider proxy/OAuth coverage claims above and uses no route mocks.
test('composite billing: real browser edits and stale policy conflict', async ({ page, request }, testInfo) => {
  const token = await login(page)
  const options = { headers: adminHeaders(token), timeout: 15_000 }
  type BillingGroup = { id: number; name: string; billing_policy_version: number; billing_rate_policy: { platform_multipliers: Record<string, number>; model_multipliers: Array<{ platform: string; model: string; multiplier: number }> } }
  const name = `browser-composite-billing-${testInfo.workerIndex}`
  const group = await data<BillingGroup>(await request.post('/api/v1/admin/groups', { ...options, data: {
    name, platform: 'composite', rate_multiplier: 1,
    billing_rate_policy: { platform_multipliers: { openai: 1, stepfun: 2 }, model_multipliers: [{ platform: 'openai', model: 'gpt-*', multiplier: 1.5 }] },
  } }))
  try {
    await page.goto('/admin/groups')
    await tableRow(page, name).getByRole('button', { name: 'Edit', exact: true }).click()
    const editor = page.locator('#edit-group-form')
    await expect(editor).toBeVisible()
    await editor.getByText('Platform factors', { exact: true }).click()
    await editor.locator('#edit-billing-rate-openai-rate').fill('0')
    await editor.locator('#edit-model-billing-rate-model-0-name').fill('gpt-test')
    await editor.locator('#edit-model-billing-rate-model-0-rate').fill('0.75')
    const saved = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1/admin/groups/${group.id}` && response.request().method() === 'PUT')
    await page.locator('button[type="submit"][form="edit-group-form"]').click()
    expect((await saved).ok()).toBeTruthy()
    await expect(editor).not.toBeVisible()
    const first = await data<BillingGroup>(await request.get(`/api/v1/admin/groups/${group.id}`, options))
    expect(first.billing_rate_policy).toEqual({ platform_multipliers: { openai: 0, stepfun: 2 }, model_multipliers: [{ platform: 'openai', model: 'gpt-test', multiplier: 0.75 }] })
    expect(first.billing_policy_version).toBeGreaterThan(group.billing_policy_version)

    await tableRow(page, name).getByRole('button', { name: 'Edit', exact: true }).click()
    await expect(editor).toBeVisible()
    await editor.getByText('Platform factors', { exact: true }).click()
    await expect(editor.locator('#edit-billing-rate-openai-rate')).toHaveValue('0')
    // A second real HTTP writer changes the group while this browser holds its
    // original form version. The stale UI must not overwrite that change.
    const changed = await data<BillingGroup>(await request.put(`/api/v1/admin/groups/${group.id}`, { ...options, data: {
      expected_billing_policy_version: first.billing_policy_version,
      billing_rate_policy: { ...first.billing_rate_policy, platform_multipliers: { openai: 3, stepfun: 2 } },
    } }))
    await editor.locator('#edit-billing-rate-openai-rate').fill('4')
    const conflict = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1/admin/groups/${group.id}` && response.request().method() === 'PUT')
    await page.locator('button[type="submit"][form="edit-group-form"]').click()
    const rejected = await conflict
    expect(rejected.status()).toBe(409)
    expect(rejected.request().postDataJSON().expected_billing_policy_version).toBe(first.billing_policy_version)
    await expect(editor).toBeVisible()
    await expect(editor.locator('#edit-billing-rate-openai-rate')).toHaveValue('4')
    await expect(page.getByText('This group’s billing rules changed while you were editing.', { exact: false })).toBeVisible()
    const current = await data<BillingGroup>(await request.get(`/api/v1/admin/groups/${group.id}`, options))
    expect(current.billing_policy_version).toBe(changed.billing_policy_version)
    expect(current.billing_rate_policy.platform_multipliers.openai).toBe(3)
  } finally {
    const removed = await request.delete(`/api/v1/admin/groups/${group.id}`, options)
    expect(removed.ok()).toBeTruthy()
  }
})

test('composite API key: supplier quotes in create, edit, inline and bulk group selection', async ({ page, request }, testInfo) => {
  const token = await login(page)
  const options = { headers: adminHeaders(token), timeout: 15_000 }
  const name = `browser-key-rates-${testInfo.workerIndex}`
  const group = await data<{ id: number }>(await request.post('/api/v1/admin/groups', { ...options, data: {
    name, platform: 'composite', rate_multiplier: 1,
    billing_rate_policy: { platform_multipliers: { openai: 1.2, stepfun: 2 }, model_multipliers: [{ platform: 'openai', model: 'public-openai', multiplier: 1.5 }] },
    model_pricing: [
      { platform: 'openai', models: ['public-openai'], billing_mode: 'token', input_price: 0.000001, output_price: 0.000002 },
      { platform: 'stepfun', models: ['public-step'], billing_mode: 'token', input_price: 0.000001, output_price: 0.000002 },
    ],
  } }))
  let keyID: number | undefined
  try {
    for (const [model, supplier] of [['public-openai', 'openai'], ['public-step', 'stepfun']]) {
      await data(await request.post(`/api/v1/admin/groups/${group.id}/composite-routes`, { ...options, data: {
        public_model: model, match_type: 'exact', target_platform: supplier, upstream_model: model, endpoint: 'chat_completions', enabled: true,
      } }))
    }
    // Narrow dark-mode menus must retain all supplier quotes inside one dropdown.
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/keys')
    await page.locator('html').evaluate(element => element.classList.add('dark'))
    await page.locator('[data-tour="keys-create-btn"]').click()
    const form = page.locator('#key-form')
    await form.locator('label').filter({ has: page.locator('input[name="key-provider"][value="other"]') }).click()
    await form.locator('#key-form-group').click()
    const option = page.locator('.select-option').filter({ has: page.getByText(name, { exact: true }) })
    await expect(option.locator('[data-rate-entry="openai:token"]')).toContainText('×1.2')
    await expect(option.locator('[data-rate-entry="openai:token"]')).toContainText('×1.8')
    await expect(option.locator('[data-rate-entry="stepfun:token"]')).toContainText('×2')
    const bounds = await page.locator('.select-dropdown-portal').boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
    await option.click()
    await expect(form.locator('[data-rate-entry="openai:token"]')).toContainText('×1.8')
    await form.locator('[data-tour="key-form-name"]').fill(name)
    const created = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/keys' && response.request().method() === 'POST')
    await page.locator('button[form="key-form"][type="submit"]').click()
    const response = await created
    expect(response.ok()).toBeTruthy()
    keyID = ((await response.json()) as { data: { id: number } }).data.id
    await expect(form).not.toBeVisible()
    await page.setViewportSize({ width: 1280, height: 900 })
    const row = tableRow(page, name)
    await row.getByRole('button', { name: 'Edit', exact: true }).click()
    await expect(form.locator('[data-rate-entry="openai:token"]')).toContainText('×1.8')
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click()
    await row.locator('button[title="Click to change group"]').click()
    const inline = page.locator('[data-test="key-inline-group-selector"]')
    await expect(inline.locator('button').filter({ has: page.getByText(name, { exact: true }) }).locator('[data-rate-entry="openai:token"]')).toContainText('×1.8')
    await page.locator('h1').click()
    await row.locator('[data-test="select-row"]').check()
    await page.locator('[data-test="bulk-edit-keys"]').click()
    const bulk = page.locator('#bulk-edit-keys-form')
    await bulk.locator('[data-test="enable-group"]').check()
    await bulk.locator('[data-test="group-input"]').click()
    await page.locator('.select-option').filter({ has: page.getByText(name, { exact: true }) }).click()
    await expect(bulk.locator('[data-rate-entry="openai:token"]')).toContainText('×1.8')
    await expect(bulk.locator('[data-rate-entry="stepfun:token"]')).toContainText('×2')
  } finally {
    if (keyID !== undefined) expect((await request.delete(`/api/v1/keys/${keyID}`, options)).ok()).toBeTruthy()
    expect((await request.delete(`/api/v1/admin/groups/${group.id}`, options)).ok()).toBeTruthy()
  }
})
