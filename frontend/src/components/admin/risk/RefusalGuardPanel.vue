<template>
  <section class="space-y-6" data-test="refusal-guard-panel">
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <template v-if="config">
      <div class="grid gap-4 sm:grid-cols-2">
        <label class="flex items-center gap-2"><input v-model="config.enabled" type="checkbox" />{{ t('admin.riskControl.refusal.enabled') }}</label>
        <label v-for="field in numericFields" :key="field.key" class="space-y-1 text-sm">
          <span>{{ t(`admin.riskControl.refusal.${field.key}`) }}</span>
          <input v-if="field.key === 'sessionDays'" v-model.number="sessionDays" type="number" min="0.0007" max="90" step="1" class="input w-full" />
          <input v-else v-model.number="config[field.key]" type="number" :min="field.min" :max="field.max" class="input w-full" />
        </label>
      </div>
      <div class="space-y-3">
        <div class="flex items-center justify-between"><h3 class="font-semibold">{{ t('admin.riskControl.refusal.rules') }}</h3><button type="button" class="btn btn-secondary" @click="addRule">{{ t('admin.riskControl.refusal.add') }}</button></div>
        <div v-for="(rule, index) in config.rules" :key="index" class="space-y-3 rounded border border-gray-200 p-4 dark:border-dark-600">
          <div class="flex items-center gap-3"><input v-model="rule.enabled" type="checkbox" :aria-label="t('admin.riskControl.refusal.enabled')" /><input v-model="rule.id" class="input flex-1" placeholder="ID" /><input v-model="rule.name" class="input flex-1" :placeholder="t('admin.riskControl.refusal.name')" /><button type="button" class="btn btn-secondary" @click="config.rules.splice(index, 1)">{{ t('admin.riskControl.refusal.remove') }}</button></div>
          <label v-for="field in ruleFields" :key="field" class="block text-sm">{{ t(`admin.riskControl.refusal.${field}`) }}
            <input :value="rule[field].join(', ')" class="input mt-1 w-full" @input="setRuleField(rule, field, $event)" />
          </label>
        </div>
      </div>
      <button type="button" class="btn btn-primary" :disabled="saving" @click="save">{{ t('admin.riskControl.refusal.save') }}</button>
    </template>
    <div class="space-y-3 border-t border-gray-200 pt-5 dark:border-dark-600">
      <div class="flex items-center justify-between"><h3 class="font-semibold">{{ t('admin.riskControl.refusal.current') }}</h3><div class="flex gap-2"><button type="button" class="btn btn-secondary" @click="loadStatus">{{ t('admin.riskControl.refusal.refresh') }}</button><button type="button" class="btn btn-secondary" @click="clearAll">{{ t('admin.riskControl.refusal.clearAll') }}</button></div></div>
      <h4 class="font-medium">{{ t('admin.riskControl.refusal.users') }}</h4>
      <div v-for="user in users" :key="user.user_id" class="flex items-center justify-between border-b py-2 text-sm dark:border-dark-600"><span>#{{ user.user_id }} {{ user.email }} · {{ t('admin.riskControl.refusal.count') }} {{ user.count }} ({{ user.count_ttl_seconds }}s) · {{ t('admin.riskControl.refusal.cooldown') }} {{ user.cooldown_remaining_seconds }}s</span><button type="button" class="btn btn-secondary" @click="clearUser(user.user_id)">{{ t('admin.riskControl.refusal.clear') }}</button></div>
      <h4 class="font-medium">{{ t('admin.riskControl.refusal.sessions') }}</h4>
      <div v-for="session in sessions" :key="session.block_key" class="flex items-center justify-between border-b py-2 text-sm dark:border-dark-600"><span>#{{ session.user_id }} · {{ session.session_id }} · {{ session.rule_id }} · {{ session.remaining_seconds }}s</span><button type="button" class="btn btn-secondary" @click="clearSession(session.block_key)">{{ t('admin.riskControl.refusal.release') }}</button></div>
      <button v-if="sessionTotal > sessions.length" type="button" class="btn btn-secondary" @click="nextPage">{{ t('admin.riskControl.refusal.more') }}</button>
    </div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { refusalGuardAPI, type RefusalConfig, type RefusalRule, type RefusalSession, type RefusalUser } from '@/api/admin/refusalGuard'
const { t } = useI18n()
const config = ref<RefusalConfig | null>(null)
const users = ref<RefusalUser[]>([])
const sessions = ref<RefusalSession[]>([])
const sessionTotal = ref(0)
const page = ref(1)
const sessionDays = ref(30)
const saving = ref(false)
const error = ref('')
const numericFields = [
  { key: 'user_threshold', min: 1, max: 100 },
  { key: 'count_window_seconds', min: 10, max: 86400 },
  { key: 'user_cooldown_seconds', min: 10, max: 86400 },
  { key: 'sessionDays', min: 1, max: 90 },
] as const
const ruleFields = ['status_codes', 'error_codes', 'error_types', 'message_keywords'] as const
function setRuleField(rule: RefusalRule, field: typeof ruleFields[number], event: Event) {
  const entries = (event.target as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean)
  if (field === 'status_codes') rule.status_codes = entries.map(Number)
  else rule[field] = entries
}
function addRule() { config.value?.rules.push({ id: `rule_${Date.now()}`, name: '', enabled: true, status_codes: [400], error_codes: [], error_types: [], message_keywords: [] }) }
async function loadStatus() { try { users.value = await refusalGuardAPI.listUsers(); const result = await refusalGuardAPI.listSessions(page.value); sessions.value = result.data; sessionTotal.value = result.total } catch { error.value = t('admin.riskControl.refusal.failed') } }
async function load() { try { config.value = await refusalGuardAPI.getConfig(); sessionDays.value = config.value.session_block_ttl_seconds / 86400; await loadStatus() } catch { error.value = t('admin.riskControl.refusal.failed') } }
async function save() { if (!config.value) return; saving.value = true; error.value = ''; try { config.value.session_block_ttl_seconds = Math.round(sessionDays.value * 86400); config.value = await refusalGuardAPI.updateConfig(config.value) } catch { error.value = t('admin.riskControl.refusal.failed') } finally { saving.value = false } }
async function clearUser(id: number) { if (!window.confirm(t('admin.riskControl.refusal.confirm'))) return; try { await refusalGuardAPI.clearUser(id); await loadStatus() } catch { error.value = t('admin.riskControl.refusal.failed') } }
async function clearSession(key: string) { if (!window.confirm(t('admin.riskControl.refusal.confirm'))) return; try { await refusalGuardAPI.clearSession(key); await loadStatus() } catch { error.value = t('admin.riskControl.refusal.failed') } }
async function clearAll() { if (!window.confirm(t('admin.riskControl.refusal.confirm'))) return; try { await refusalGuardAPI.clearAll(); await loadStatus() } catch { error.value = t('admin.riskControl.refusal.failed') } }
async function nextPage() { page.value++; await loadStatus() }
onMounted(load)
</script>
