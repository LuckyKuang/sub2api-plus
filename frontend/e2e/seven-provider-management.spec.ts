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
