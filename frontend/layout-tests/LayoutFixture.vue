<template>
  <main class="fixture-screen" data-testid="layout-ready">
    <section v-if="controlsOnly" class="fixture-style-gallery" data-testid="style-gallery">
      <nav class="card workspace-tabs" aria-label="Workspace">
        <button v-for="(label, index) in copy.tabs" :key="label" class="workspace-tab" role="tab" :aria-selected="selectedTab === index" @click="selectedTab = index">{{ label }}</button>
      </nav>
      <nav class="card-tabs" aria-label="Sections">
        <button v-for="(label, index) in copy.sections" :key="label" class="card-tab" role="tab" :aria-selected="selectedSection === index" @click="selectedSection = index">{{ label }}</button>
      </nav>
      <article class="card p-4">
        <div class="fixture-compact-controls">
          <div class="segmented-control" aria-label="Results">
            <button v-for="(label, index) in copy.segments" :key="label" class="segmented-option" :aria-pressed="selectedSegment === index" @click="selectedSegment = index">{{ label }}</button>
          </div>
        </div>
        <table class="table record-table fixture-action-table">
          <tbody><tr><td>{{ copy.record }}<div class="table-actions" data-testid="table-actions">
            <button class="table-action" data-testid="table-action-edit" @click="galleryClicks++"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 16 12-12 4 4L8 20H4z" fill="currentColor" /></svg><span>{{ copy.edit }}</span></button>
            <button class="table-action table-action-danger" @click="galleryClicks++"><span>{{ copy.remove }}</span></button>
            <button class="table-action table-action-neutral" @click="galleryClicks++"><span>{{ copy.more }}</span></button>
            <button class="table-action" disabled data-testid="table-action-disabled" @click="galleryClicks++"><span>{{ copy.disabled }}</span></button>
            <button class="table-action" disabled aria-busy="true" data-testid="table-action-busy" @click="galleryClicks++"><span>{{ copy.busy }}</span></button>
          </div></td></tr></tbody>
        </table>
        <output data-testid="gallery-clicks">{{ galleryClicks }}</output>
        <div class="fixture-fields">
          <label class="input-label">{{ copy.select }}<input class="input" :placeholder="copy.select" /></label>
          <SearchableSelect :model-value="null" :options="options" :disabled="true" :aria-label="copy.disabled" />
          <SearchableSelect :model-value="null" :options="options" :error="true" :aria-label="copy.error" />
          <button class="btn btn-primary">{{ copy.save }}</button>
          <button class="btn btn-secondary">{{ copy.cancel }}</button>
        </div>
      </article>
    </section>
    <template v-else>
    <button class="btn btn-primary" data-testid="open-dialog" @click="openDialog(false)">{{ copy.open }}</button>
    <button class="btn btn-secondary" data-testid="open-extreme-dialog" @click="openDialog(true)">{{ copy.open }}</button>
    <output data-testid="edge-date-result">{{ edgeStart }}</output>
    <output data-testid="edge-select-result">{{ edgeSelection }}</output>
    <output data-testid="saved-result">{{ saved }}</output>

    <!-- The popup must escape both a clipping card and its nested scrollport. -->
    <section class="fixture-edge-card surface-card" data-testid="edge-card">
      <div class="fixture-card-scroll" data-testid="card-scroll">
        <div class="fixture-card-spacer"></div>
        <div class="fixture-fields">
          <div data-testid="checkbox-dropdown-field">
            <CheckboxDropdown v-model="checkedOptions" :options="options" :label="copy.select" />
            <output data-testid="checkbox-selection">{{ checkedOptions.join(',') }}</output>
          </div>
          <div data-testid="edge-date-field">
            <DateRangePicker v-model:start-date="edgeStart" v-model:end-date="edgeEnd" />
          </div>
          <div data-testid="edge-select-field">
            <SearchableSelect v-model="edgeSelection" :options="options" :searchable="false" :placeholder="copy.select" />
          </div>
        </div>
      </div>
    </section>

    <BaseDialog :show="dialogOpen" :title="extremeTitle ? copy.title.repeat(12) : copy.title" @close="dialogOpen = false">
      <p v-for="index in 28" :key="index" class="fixture-record">{{ copy.record }} {{ index }}</p>
      <div class="fixture-fields">
        <div data-testid="dialog-date-field">
          <DateRangePicker v-model:start-date="dialogStart" v-model:end-date="dialogEnd" />
        </div>
        <output data-testid="dialog-date-result">{{ dialogStart }}</output>
        <div data-testid="dialog-select-field">
          <SearchableSelect v-model="dialogSelection" :options="options" :searchable="true" :placeholder="copy.select" />
        </div>
        <output data-testid="dialog-select-result">{{ dialogSelection }}</output>
        <button class="btn btn-secondary" data-testid="last-content" @click="lastClicked++">{{ copy.last }}</button>
        <output data-testid="last-content-result">{{ lastClicked }}</output>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" data-testid="dialog-cancel" @click="dialogOpen = false">{{ copy.cancel }}</button>
          <button class="btn btn-primary" data-testid="dialog-save" @click="save">{{ copy.save }}</button>
        </div>
      </template>
    </BaseDialog>
    </template>
  </main>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '../src/components/common/BaseDialog.vue'
import DateRangePicker from '../src/components/common/DateRangePicker.vue'
import SearchableSelect from '../src/components/common/SearchableSelect.vue'
import CheckboxDropdown from '../src/components/common/CheckboxDropdown.vue'

const { locale } = useI18n()
const controlsOnly = new URLSearchParams(location.search).get('view') === 'controls'
const selectedTab = ref(0)
const selectedSection = ref(0)
const selectedSegment = ref(0)
const galleryClicks = ref(0)
const checkedOptions = ref<number[]>([])
const copy = computed(() => locale.value === 'zh' ? {
  tabs: ['运行监控', '告警事件', '系统日志'],
  sections: ['详细配置与使用说明', '包含较长中文翻译的统计和历史记录'],
  segments: ['全部请求', '成功的请求', '异常与需要进一步检查的请求'],
  edit: '编辑当前记录的完整配置',
  remove: '删除记录',
  more: '更多操作',
  disabled: '当前不可用的操作',
  busy: '正在处理，请稍候',
  error: '包含错误状态的选择控件',
  open: '打开包含长标题和滚动内容的对话框',
  title: '统一界面布局检查：较长的中文标题应完整换行，关闭按钮仍然可见且可操作，不应被标题覆盖',
  select: '选择包含较长中文名称和说明的可用选项',
  option: '包含较长中文名称和补充说明的可用选项',
  record: '可滚动内容：最后一条记录和所有操作必须可以访问',
  last: '最后一条内容的操作',
  cancel: '取消当前修改并返回之前的页面',
  save: '保存当前修改并继续后续操作',
} : {
  tabs: ['Monitoring', 'Alert events', 'System logs'],
  sections: ['Configuration and usage instructions', 'Statistics and history with a longer translated label'],
  segments: ['All requests', 'Successful requests', 'Abnormal requests requiring additional review'],
  edit: 'Edit the complete configuration for this record',
  remove: 'Delete record',
  more: 'More actions',
  disabled: 'Currently unavailable action',
  busy: 'Processing, please wait',
  error: 'Selection with a validation error',
  open: 'Open dialog with a long title and scrolling content',
  title: 'Shared interface layout: a long translated title must wrap completely while the close button stays visible and available',
  select: 'Select an available option with a longer translated name',
  option: 'Available option with a longer translated name and additional description',
  record: 'Scrollable content: the final record and every action must remain reachable',
  last: 'Action on the final content record',
  cancel: 'Cancel the current changes and return to the previous page',
  save: 'Save the current changes and continue to the next action',
})
const options = computed(() => Array.from({ length: 24 }, (_, index) => ({
  value: index + 1,
  label: `${String(index + 1).padStart(2, '0')} · ${copy.value.option}`,
})))
const dialogOpen = ref(false)
const extremeTitle = ref(false)
const edgeStart = ref('2024-04-01')
const edgeEnd = ref('2024-04-07')
const dialogStart = ref('2024-04-01')
const dialogEnd = ref('2024-04-07')
const edgeSelection = ref<string | number | boolean | null>(null)
const dialogSelection = ref<string | number | boolean | null>(null)
const lastClicked = ref(0)
const saved = ref(0)
function save() { saved.value++; dialogOpen.value = false }
function openDialog(extreme: boolean) { extremeTitle.value = extreme; dialogOpen.value = true }
</script>

<style scoped>
/* Fixture geometry creates hostile containers; component styling comes only
   from production style.css and the imported Vue components. */
.fixture-screen { min-height: 100dvh; padding: 24px; color: var(--surface-text); }
.fixture-style-gallery { display: grid; gap: 16px; width: min(640px, 100%); margin-inline: auto; min-width: 0; }
.fixture-compact-controls { width: min(256px, 100%); margin-bottom: 16px; }
.fixture-action-table { table-layout: fixed; margin-block: 16px; }
.fixture-screen > output { display: block; }
.fixture-edge-card { position: fixed; right: 8px; bottom: 8px; width: min(328px, calc(100vw - 16px)); height: 168px; overflow: hidden; }
.fixture-card-scroll { height: 100%; padding: 16px; overflow: auto; }
.fixture-card-spacer { height: 96px; }
.fixture-fields { display: flex; min-width: 0; flex-direction: column; gap: 12px; }
.fixture-record { min-height: 48px; margin-bottom: 12px; }
</style>
