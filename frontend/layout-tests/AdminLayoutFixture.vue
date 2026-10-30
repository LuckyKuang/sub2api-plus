<template>
  <AppLayout workspace>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3" data-testid="admin-toolbar">
          <Select v-model="selection" class="w-48" :options="options" data-testid="admin-select" />
          <button class="btn btn-secondary" data-testid="admin-collapse" @click="app.toggleSidebar()">{{ t('common.menu') }}</button>
        </div>
      </template>
      <template #table>
        <DataTable v-model:selected-keys="selectedKeys" class="workspace-table-viewport" :columns="columns" :data="rows" row-key="id" selectable :sticky-first-column="true" :sticky-actions-column="true" :expandable-actions="false">
          <template #cell-model="{ value }"><span>{{ value }}</span></template>
          <template #cell-group="{ value }"><span>{{ value }}</span></template>
          <template #cell-actions="{ row }">
            <div class="table-actions">
              <button v-for="action in ['Edit record', 'Inspect usage', 'Disable account']" :key="action" class="table-action" @click="lastAction = `${row.id}:${action}`">{{ action }}</button>
            </div>
          </template>
        </DataTable>
      </template>
      <template #pagination><Pagination :page="page" :total="120" :page-size="60" @update:page="page = $event" /></template>
    </TablePageLayout>
    <output class="sr-only" data-testid="admin-last-action">{{ lastAction }}</output>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '../src/components/layout/AppLayout.vue'
import TablePageLayout from '../src/components/layout/TablePageLayout.vue'
import DataTable from '../src/components/common/DataTable.vue'
import Pagination from '../src/components/common/Pagination.vue'
import Select from '../src/components/common/Select.vue'
import type { Column } from '../src/components/common/types'
import type { User } from '../src/types'
import { useAppStore } from '../src/stores/app'
import { useAuthStore } from '../src/stores/auth'

const app = useAppStore()
app.publicSettingsLoaded = true
app.versionLoaded = true
app.siteName = 'Admin layout review'
app.sidebarCollapsed = false
const auth = useAuthStore()
auth.user = { id: 9998, role: 'admin', email: 'fixture@example.test', username: 'Reviewer', balance: 0 } as User
localStorage.setItem('admin_guide', 'true')
localStorage.setItem('admin_guide_9998_admin_v4_interactive', 'true')
const { t } = useI18n()
const selection = ref('all')
const page = ref(1)
const lastAction = ref('')
const selectedKeys = ref<Array<string | number>>([])
const options = [{ value: 'all', label: t('common.all') }, { value: 'active', label: t('common.active') }]
const columns: Column[] = [
  { key: 'name', label: 'Account name', sticky: 'left' },
  { key: 'model', label: 'Model' }, { key: 'group', label: 'Group' },
  { key: 'endpoint', label: 'Endpoint' }, { key: 'request_id', label: 'Request ID' },
  { key: 'actions', label: 'Actions', sticky: 'right' },
]
const rows = Array.from({ length: 60 }, (_, index) => ({
  id: index + 1, name: `Admin record ${index + 1}`,
  model: 'gpt-5.4-admin-layout-long-model-name-complete',
  group: '管理端完整分组名称 Admin complete routing group',
  endpoint: '/v1/responses/admin-layout-independent-regression',
  request_id: `admin-layout-request-${index + 1}-independent-fixture`,
}))
</script>
