<template>
  <UsageView v-if="usage" />
  <DashboardView v-else-if="dashboard" />
  <AppLayout v-else :appearance="admin ? 'admin' : 'user'" :workspace="workspace" :record-workspace="recordWorkspace">
    <div :class="workspace ? 'workspace-page' : 'space-y-6'">
      <header class="workspace-fixed" data-testid="page-title"><h2 class="text-xl font-semibold">{{ t('nav.apiKeys') }}</h2></header>
      <div class="workspace-fixed flex flex-wrap gap-3" data-testid="page-filter">
        <Select v-model="selection" class="w-48" :options="options" />
        <button class="btn btn-secondary" data-testid="collapse-sidebar" @click="app.toggleSidebar()">{{ t('common.menu') }}</button>
        <button v-if="records" class="btn btn-secondary" data-testid="toggle-records" @click="empty = !empty">Toggle sample rows</button>
      </div>
      <section class="grid grid-cols-1 gap-3 sm:grid-cols-2 workspace-fixed" data-testid="card-group">
        <article v-for="index in count" :key="index" class="card p-5"><p class="text-sm surface-muted">{{ t('dashboard.todayCost') }}</p><p class="mt-2 text-2xl font-semibold">$12.34</p></article>
      </section>
      <section class="workspace-table-card card" data-testid="page-table">
        <DataTable v-if="records" class="workspace-table-viewport" :columns="columns" :data="empty ? [] : rows" selectable row-key="id">
          <template #cell-model="{ value }"><span class="truncate" :title="value">{{ value }}</span></template>
          <template #cell-group="{ value }"><span class="badge whitespace-nowrap">{{ value }}</span></template>
          <template #cell-actions>
            <div class="table-actions">
              <button v-for="label in ['Use API key', 'Import to CCS', 'Disable', 'Edit', 'Delete']" :key="label" class="table-action"><span>{{ label }}</span></button>
            </div>
          </template>
        </DataTable>
        <div v-else class="workspace-table-viewport">
          <table class="w-full text-sm"><thead><tr><th class="p-3 text-left">{{ t('nav.apiKeys') }}</th></tr></thead><tbody><tr v-for="index in 60" :key="index"><td class="p-3">{{ t('common.active') }} {{ index }}</td></tr></tbody></table>
        </div>
      </section>
      <footer class="pagination-card workspace-fixed flex items-center justify-between" data-testid="page-pagination"><span>1 / 3</span><button class="btn btn-secondary">{{ t('common.next') }}</button></footer>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { defineAsyncComponent, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '../src/components/layout/AppLayout.vue'
import Select from '../src/components/common/Select.vue'
import DataTable from '../src/components/common/DataTable.vue'
import type { Column } from '../src/components/common/types'
import { useAppStore } from '../src/stores/app'
import { useAuthStore } from '../src/stores/auth'
import type { User } from '../src/types'

const parameters = new URLSearchParams(location.search)
const dashboard = parameters.get('dashboard') === '1'
const usage = parameters.get('usage') === '1'
const recordWorkspace = parameters.get('recordWorkspace') === '1'
const UsageView = defineAsyncComponent(() => import('../src/views/user/UsageView.vue'))
const DashboardView = defineAsyncComponent(() => import('../src/views/user/DashboardView.vue'))
const admin = parameters.get('admin') === '1'
const workspace = parameters.get('workspace') !== '0'
const count = Number(parameters.get('count') ?? '2')
const records = parameters.get('records') === '1'
const dense = parameters.get('dense') === '1'
const empty = ref(false)
const columns: Column[] = [
  { key: 'name', label: 'API key name' },
  { key: 'model', label: 'Model' },
  { key: 'group', label: 'Group' },
  { key: 'cost', label: 'Actual cost', class: 'text-right' },
  ...(dense ? [
    { key: 'endpoint', label: 'Endpoint' },
    { key: 'client', label: 'Client' },
    { key: 'request_id', label: 'Request ID' },
  ] : []),
  { key: 'actions', label: 'Actions' },
]
const rows = Array.from({ length: 3 }, (_,index) => ({
  id: index + 1,
  name: `Audit key ${index + 1}`,
  model: index === 0 ? 'gpt-5.4-ui-audit-long-model-name-example' : 'gpt-5.4',
  group: '[UI测试] OpenAI 标准审计分组',
  cost: '$0.123456',
  endpoint: '/v1/images/generations',
  client: 'UI-Audit-Long-Client/1.0',
  request_id: `ui-audit-request-20261029-${String(index + 1).padStart(6, '0')}`,
}))
const app = useAppStore()
app.publicSettingsLoaded = true
app.versionLoaded = true
app.siteName = 'UI Review'
const auth = useAuthStore()
auth.user = { id: 9999, role: 'user', email: 'review@example.test', username: 'Reviewer', balance: 12.34 } as User
if (dashboard || usage) auth.token = 'browser-fixture-token'
localStorage.setItem('user_guide_9999_user_v4_interactive', 'true')
const { t } = useI18n()
const selection = ref('all')
const options = [{ value: 'all', label: t('common.all') }, { value: 'active', label: t('common.active') }]
</script>
