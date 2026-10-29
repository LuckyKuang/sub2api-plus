<template>
  <div class="app-layout-user">
    <AppHeader />
    <main class="space-y-6 p-6" data-testid="motion-ready">
      <div class="flex flex-wrap gap-4">
        <button class="btn btn-primary" data-testid="open-motion-dialog" @click="dialog = true">Open dialog</button>
        <button class="btn btn-secondary" data-testid="open-motion-drawer" @click="drawer = true">Open drawer</button>
        <button class="btn btn-secondary" data-testid="open-motion-announcement" @click="announcement = true">Preview announcement</button>
      </div>
      <div class="flex flex-wrap gap-4">
        <Select v-model="selected" class="w-48" :options="options" aria-label="Motion select" />
        <CheckboxDropdown v-model="checked" :options="options" label="Motion checkboxes" />
        <AutoRefreshButton :enabled="false" :interval-seconds="15" :countdown="15" :intervals="[15, 30]" />
      </div>
      <div class="flex flex-wrap gap-4">
        <DateRangePicker v-model:start-date="start" v-model:end-date="end" />
        <DateTimePicker v-model="date" type="date" aria-label="Motion calendar" />
      </div>
      <button class="btn btn-secondary" data-testid="outside-control">Outside control</button>
      <button class="btn btn-secondary" data-testid="enable-admin-motion" @click="enableAdmin">Show administrator version menu</button>
      <div data-testid="version-control"><VersionBadge /></div>
      <output data-testid="motion-selection">{{ selected }} / {{ checked.join(',') }}</output>
      <BaseDialog :show="dialog" title="Motion dialog" @close="dialog = false">
        <label>Request name<input class="input" aria-label="Request name" /></label>
        <Select v-model="selected" :options="options" aria-label="Dialog select" />
      </BaseDialog>
      <BaseDialog :show="drawer" title="Motion drawer" placement="right" @close="drawer = false">Drawer details</BaseDialog>
      <AnnouncementPopup :preview="true" :announcement="announcement ? announcementData : null" @close="announcement = false" />
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import AppHeader from '../src/components/layout/AppHeader.vue'
import BaseDialog from '../src/components/common/BaseDialog.vue'
import Select from '../src/components/common/Select.vue'
import CheckboxDropdown from '../src/components/common/CheckboxDropdown.vue'
import AutoRefreshButton from '../src/components/common/AutoRefreshButton.vue'
import DateRangePicker from '../src/components/common/DateRangePicker.vue'
import DateTimePicker from '../src/components/common/DateTimePicker.vue'
import AnnouncementPopup from '../src/components/common/AnnouncementPopup.vue'
import VersionBadge from '../src/components/common/VersionBadge.vue'
import { useAuthStore } from '../src/stores/auth'
import { useAppStore } from '../src/stores/app'
import type { User } from '../src/types'

const auth = useAuthStore()
auth.user = { id: 9999, role: 'user', email: 'review@example.test', username: 'Reviewer', balance: 12.34 } as User
const app = useAppStore()
app.publicSettingsLoaded = true
app.versionLoaded = true
const dialog = ref(false), drawer = ref(false), announcement = ref(false)
const selected = ref<string | number | boolean | null>('first')
const checked = ref<string[]>([])
const options = [{ value: 'first', label: 'First option' }, { value: 'second', label: 'Second option' }]
const start = ref('2026-10-01'), end = ref('2026-10-29'), date = ref('2026-10-29')
const announcementData = { title: 'Motion announcement', content: 'Announcement preview only.', created_at: '2026-10-29T10:00:00Z' }
function enableAdmin() {
  auth.user = { ...auth.user!, role: 'admin' }
}
</script>
