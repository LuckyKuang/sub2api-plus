<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-7xl space-y-6 p-4 sm:p-6">
      <section class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.title') }}</h1>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.description') }}</p>
        </div>
        <RouterLink class="btn btn-secondary" to="/admin/settings">{{ t('admin.ipAccessControl.backToSettings') }}</RouterLink>
      </section>

      <section class="card">
        <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div>
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.failureStates.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.failureStates.description') }}</p>
          </div>
          <div class="flex items-center gap-3">
            <span v-if="failureStatesUnavailable" class="rounded bg-rose-100 px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300">
              {{ t('admin.ipAccessControl.failureStates.unavailable') }}
            </span>
            <span v-if="failureStatesUpdatedAt" class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.ipAccessControl.failureStates.lastUpdated', { time: failureStatesUpdatedAt.toLocaleString() }) }}
            </span>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="failureStatesLoading" @click="refreshFailureStates">
              {{ t('admin.ipAccessControl.failureStates.refresh') }}
            </button>
          </div>
        </div>
        <div class="flex flex-wrap items-end gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
          <label class="min-w-52 flex-1">
            <span class="input-label">{{ t('common.search') }}</span>
            <input v-model.trim="failureFilters.query" class="input" :placeholder="t('admin.ipAccessControl.failureStates.searchPlaceholder')" @keyup.enter="searchFailureStates" />
          </label>
          <button type="button" class="btn btn-secondary" :disabled="failureStatesLoading" @click="searchFailureStates">{{ t('common.search') }}</button>
        </div>
        <DataTable :columns="failureStateColumns" :data="failureStates" :loading="failureStatesLoading" row-key="normalized_ip">
          <template #cell-normalized_ip="{ value }"><span class="font-mono text-sm">{{ value }}</span></template>
          <template #cell-failure_count="{ row }"><span class="font-medium text-gray-900 dark:text-white">{{ row.failure_count }} / {{ row.failure_threshold }}</span></template>
          <template #cell-window_started_at="{ value }"><span class="whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatTimestamp(value) }}</span></template>
          <template #cell-last_failed_at="{ value }"><span class="whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatTimestamp(value) }}</span></template>
          <template #cell-window_expires_at="{ value }"><span class="whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatTimestamp(value) }}</span></template>
          <template #cell-effectively_blocked="{ row }">
            <span :class="failureBlockStatusClass(row)">
              {{ failureBlockStatusLabel(row) }}
            </span>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap items-center gap-2">
              <button type="button" class="btn btn-secondary btn-sm" @click="confirmResetCounter(row.normalized_ip)">
                {{ t('admin.ipAccessControl.failureStates.resetCounter') }}
              </button>
              <button
                v-if="row.emergency_allowlisted"
                type="button"
                class="btn btn-secondary btn-sm"
                disabled
                :title="t('admin.ipAccessControl.failureStates.manualBlockDisabledEmergencyAllow')"
              >
                {{ t('admin.ipAccessControl.failureStates.emergencyAllowActive') }}
              </button>
              <button
                v-else-if="row.suppressed_by_allow_rule"
                type="button"
                class="btn btn-secondary btn-sm"
                @click="focusRulesForFailureState(row, true)"
              >
                {{ t('admin.ipAccessControl.failureStates.handleAllow') }}
              </button>
              <button
                v-else-if="row.active_block_rule"
                type="button"
                class="btn btn-secondary btn-sm"
                @click="focusRulesForFailureState(row)"
              >
                {{ t('admin.ipAccessControl.failureStates.viewRule') }}
              </button>
              <button
                v-else
                type="button"
                class="btn btn-danger btn-sm"
                :disabled="!canManuallyBlockFailureState(row)"
                :title="manualBlockDisabledReason(row)"
                @click="confirmManualBlock(row)"
              >
                {{ t('admin.ipAccessControl.failureStates.manualBlock') }}
              </button>
            </div>
          </template>
          <template #empty><div class="py-10 text-center text-sm text-gray-500">{{ t('admin.ipAccessControl.failureStates.empty') }}</div></template>
        </DataTable>
        <p class="border-t border-gray-100 px-5 py-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
          {{ t('admin.ipAccessControl.failureStates.operationHint') }}
        </p>
        <div class="border-t border-gray-100 p-4 dark:border-dark-700">
          <Pagination
            v-if="failureTotal > 0"
            :total="failureTotal"
            :page="failurePage"
            :page-size="failurePageSize"
            @update:page="changeFailurePage"
            @update:pageSize="changeFailurePageSize"
          />
        </div>
      </section>

      <section class="card">
        <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div>
            <div class="flex flex-wrap items-center">
              <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.trustedProxy.title') }}</h2>
              <HelpTooltip width-class="w-80">
                <p>{{ t('admin.ipAccessControl.trustedProxy.guideHelp') }}</p>
              </HelpTooltip>
              <button type="button" class="ml-2 text-sm text-primary-600 underline hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300" @click="showDeployGuide = true">
                {{ t('admin.ipAccessControl.trustedProxy.guideLink') }}
              </button>
            </div>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.description') }}</p>
          </div>
          <span v-if="proxyStatus" :class="proxyStateClass">{{ proxyStateLabel }}</span>
        </div>
        <div v-if="proxyStatusLoading" class="p-6 text-sm text-gray-500">{{ t('common.loading') }}</div>
        <div v-else-if="proxyStatus">
          <div class="grid grid-cols-1 divide-y divide-gray-100 sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4 dark:divide-dark-700">
            <div class="min-w-0 px-5 py-4">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.clientIp') }}</p>
              <p class="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">{{ proxyStatus.client_ip || '—' }}</p>
            </div>
            <div class="min-w-0 px-5 py-4">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.directPeerIp') }}</p>
              <p class="mt-1 break-all font-mono text-sm font-medium text-gray-900 dark:text-white">{{ proxyStatus.direct_peer_ip || '—' }}</p>
            </div>
            <div class="min-w-0 px-5 py-4">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.directPeerTrust') }}</p>
              <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ proxyStatus.direct_peer_trusted ? t('common.yes') : t('common.no') }}</p>
            </div>
            <div class="min-w-0 px-5 py-4">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.forwardedHeaders') }}</p>
              <p class="mt-1 break-words font-mono text-sm font-medium text-gray-900 dark:text-white">{{ forwardedHeaders.length ? forwardedHeaders.join(', ') : t('admin.ipAccessControl.trustedProxy.none') }}</p>
            </div>
          </div>
          <div class="border-t border-gray-100 px-5 py-4 dark:border-dark-700">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.loadedCidrs') }}</p>
            <div v-if="trustedProxies.length" class="mt-2 flex flex-wrap gap-2">
              <span v-for="proxy in trustedProxies" :key="proxy" class="rounded bg-gray-100 px-2 py-1 font-mono text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-200">{{ proxy }}</span>
            </div>
            <p v-else class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.trustedProxy.noneConfigured') }}</p>
            <p :class="proxyStatusMessageClass" class="mt-4 rounded-md border px-3 py-2 text-xs leading-5">{{ proxyStatusMessage }}</p>
            <div class="mt-4 grid grid-cols-1 gap-3 text-xs sm:grid-cols-3 xl:grid-cols-5">
              <p class="rounded border border-gray-200 px-3 py-2 text-gray-700 dark:border-dark-700 dark:text-gray-200"><span class="font-medium">{{ t('admin.ipAccessControl.trustedProxy.identitySafety') }}</span><br>{{ proxyStatus.safe_for_enforcement ? t('admin.ipAccessControl.trustedProxy.safe') : t('admin.ipAccessControl.trustedProxy.unsafe') }}</p>
              <p class="rounded border border-gray-200 px-3 py-2 text-gray-700 dark:border-dark-700 dark:text-gray-200"><span class="font-medium">{{ t('admin.ipAccessControl.trustedProxy.legacyMode') }}</span><br>{{ proxyStatus.legacy_forwarded_mode ? t('admin.ipAccessControl.trustedProxy.enabled') : t('admin.ipAccessControl.trustedProxy.disabled') }}</p>
              <p class="rounded border border-gray-200 px-3 py-2 text-gray-700 dark:border-dark-700 dark:text-gray-200"><span class="font-medium">{{ t('admin.ipAccessControl.trustedProxy.emergencyAllowlist') }}</span><br>{{ proxyStatus.emergency_allowlist_configured ? t('admin.ipAccessControl.trustedProxy.emergencyConfigured', { count: proxyStatus.emergency_allowlist_count }) : t('admin.ipAccessControl.trustedProxy.none') }}</p>
              <p class="rounded border border-gray-200 px-3 py-2 text-gray-700 dark:border-dark-700 dark:text-gray-200"><span class="font-medium">{{ t('admin.ipAccessControl.trustedProxy.globalEnforcement') }}</span><br>{{ automaticBlockingReady ? t('admin.ipAccessControl.trustedProxy.ready') : t('admin.ipAccessControl.trustedProxy.notReady') }}</p>
              <p class="rounded border border-gray-200 px-3 py-2 text-gray-700 dark:border-dark-700 dark:text-gray-200"><span class="font-medium">{{ t('admin.ipAccessControl.trustedProxy.manualBlocking') }}</span><br>{{ manualBlockingReady ? t('admin.ipAccessControl.trustedProxy.ready') : t('admin.ipAccessControl.trustedProxy.notReady') }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="card">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.protection.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.protection.description') }}</p>
        </div>
        <div v-if="settingsLoading" class="p-6 text-sm text-gray-500">{{ t('common.loading') }}</div>
        <div v-else class="space-y-5 p-5">
          <div class="flex items-center justify-between gap-5">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.protection.enforcement') }}</label>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.protection.enforcementHint') }}</p>
            </div>
            <Toggle v-model="settings.enforcement_enabled" :disabled="proxyStatusLoading || (!settings.enforcement_enabled && !automaticBlockingReady)" />
          </div>
          <div class="flex items-center justify-between gap-5 border-t border-gray-100 pt-5 dark:border-dark-700">
            <div>
              <label class="font-medium text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.protection.autoBlock') }}</label>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.protection.autoBlockHint') }}</p>
            </div>
            <Toggle v-model="settings.login_failure_auto_block_enabled" :disabled="!settings.enforcement_enabled || (!settings.login_failure_auto_block_enabled && !automaticBlockingReady)" />
          </div>
          <div v-if="settings.enforcement_enabled" class="grid grid-cols-1 gap-4 border-t border-gray-100 pt-5 sm:grid-cols-3 dark:border-dark-700">
            <label v-if="settings.login_failure_auto_block_enabled" class="block"><span class="input-label">{{ t('admin.ipAccessControl.protection.threshold') }}</span><input v-model.number="settings.login_failure_threshold" min="2" max="100" type="number" class="input" /></label>
            <label v-if="settings.login_failure_auto_block_enabled" class="block"><span class="input-label">{{ t('admin.ipAccessControl.protection.window') }}</span><input v-model.number="settings.login_failure_window_minutes" min="1" max="525600" type="number" class="input" /></label>
            <label class="block"><span class="input-label">{{ t('admin.ipAccessControl.protection.duration') }}</span><input v-model.number="settings.login_failure_block_minutes" min="1" max="525600" type="number" class="input" /></label>
          </div>
          <p v-if="!featureEnabled" class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200">{{ t('admin.ipAccessControl.protection.masterSwitchOff') }}</p>
          <p class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200">{{ t('admin.ipAccessControl.protection.proxyHint') }}</p>
          <p v-if="!proxyStatusLoading && !automaticBlockingReady" class="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-xs leading-5 text-rose-800 dark:border-rose-800/70 dark:bg-rose-950/30 dark:text-rose-200">
            {{ t('admin.ipAccessControl.protection.proxyNotReady') }}
            <button type="button" class="ml-1 underline" @click="showDeployGuide = true">{{ t('admin.ipAccessControl.trustedProxy.guideLink') }}</button>
          </p>
          <div class="flex justify-end border-t border-gray-100 pt-4 dark:border-dark-700"><button type="button" class="btn btn-primary" :disabled="settingsSaving" @click="saveSettings">{{ settingsSaving ? t('common.saving') : t('common.save') }}</button></div>
        </div>
      </section>

      <section ref="rulesSection" class="card">
        <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.rules.title') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.rules.description') }}</p></div>
          <button type="button" class="btn btn-primary" @click="openCreateDialog"><Icon name="plus" size="sm" class="mr-1.5" />{{ t('admin.ipAccessControl.rules.add') }}</button>
        </div>
        <div class="flex flex-wrap items-end gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
          <label class="min-w-52 flex-1"><span class="input-label">{{ t('common.search') }}</span><input v-model.trim="filters.query" class="input" :placeholder="t('admin.ipAccessControl.rules.searchPlaceholder')" @keyup.enter="search" /></label>
          <label class="w-full sm:w-44"><span class="input-label">{{ t('common.status') }}</span><Select v-model="filters.status" :options="statusOptions" @change="search" /></label>
          <button type="button" class="btn btn-secondary" :disabled="rulesLoading" @click="search">{{ t('common.search') }}</button>
        </div>
        <DataTable :columns="columns" :data="rules" :loading="rulesLoading" row-key="id">
          <template #cell-ip_or_cidr="{ value }"><span class="font-mono text-sm">{{ value }}</span></template>
          <template #cell-rule_kind="{ value }"><span :class="kindClass(value)">{{ kindLabel(value) }}</span></template>
          <template #cell-status="{ row }"><span :class="statusClass(row.status)">{{ statusLabel(row) }}</span></template>
          <template #cell-failure_count="{ row }"><span>{{ row.failure_count || '—' }}</span></template>
          <template #cell-hit_count="{ row }"><span>{{ row.hit_count ?? 0 }}</span></template>
          <template #cell-last_seen_at="{ value }"><span class="whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatLastSeenTime(value) }}</span></template>
          <template #cell-expires_at="{ value }"><span class="whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">{{ formatExpiryTime(value) }}</span></template>
          <template #cell-actions="{ row }"><div class="flex flex-wrap items-center gap-2"><button v-if="row.status === 'active' && !row.ip_or_cidr.includes('/') && row.rule_kind !== 'allow'" type="button" class="btn btn-secondary btn-sm" @click="confirmResetCounter(row.ip_or_cidr)">{{ t('admin.ipAccessControl.rules.resetCounter') }}</button><button v-if="row.status === 'active'" type="button" class="btn btn-danger btn-sm" @click="confirmRelease(row)">{{ row.rule_kind === 'allow' ? t('admin.ipAccessControl.rules.removeAllow') : t('admin.ipAccessControl.rules.releaseReset') }}</button><span v-else class="text-xs text-gray-400">—</span></div></template>
          <template #empty><div class="py-10 text-center text-sm text-gray-500">{{ t('admin.ipAccessControl.rules.empty') }}</div></template>
        </DataTable>
		<p class="border-t border-gray-100 px-5 py-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">{{ t('admin.ipAccessControl.rules.hitCountHint') }}</p>
        <div class="border-t border-gray-100 p-4 dark:border-dark-700"><Pagination v-if="total > 0" :total="total" :page="page" :page-size="pageSize" @update:page="changePage" @update:pageSize="changePageSize" /></div>
      </section>
    </div>

    <BaseDialog :show="createVisible" :title="t('admin.ipAccessControl.rules.addTitle')" width="narrow" @close="createVisible = false">
      <div class="space-y-4 py-2">
        <label class="block"><span class="input-label">{{ t('admin.ipAccessControl.rules.ip') }}</span><input v-model.trim="createForm.ip_or_cidr" class="input font-mono" placeholder="203.0.113.8 or 2001:db8::/32" /></label>
        <label class="block"><span class="input-label">{{ t('admin.ipAccessControl.rules.type') }}</span><Select v-model="createForm.rule_kind" :options="createKindOptions" /></label>
        <p v-if="!manualBlockingReady" class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200">{{ t('admin.ipAccessControl.rules.manualBlockUnavailable') }}</p>
        <label class="block"><span class="input-label">{{ t('admin.ipAccessControl.rules.reason') }}</span><textarea v-model.trim="createForm.reason" rows="3" class="input resize-y" /></label>
        <label class="block"><span class="input-label">{{ t('admin.ipAccessControl.rules.expiresAt') }}</span><input v-model="createForm.expires_at" type="datetime-local" class="input" /></label>
      </div>
      <template #footer><button type="button" class="btn btn-secondary" @click="createVisible = false">{{ t('common.cancel') }}</button><button type="button" class="btn btn-primary" :disabled="creating || !createForm.ip_or_cidr || (createForm.rule_kind === 'manual_block' && !manualBlockingReady)" @click="createRule">{{ creating ? t('common.saving') : t('common.create') }}</button></template>
    </BaseDialog>
    <ConfirmDialog :show="releaseTarget !== null" :title="releaseDialogTitle" :message="releaseDialogMessage" :confirm-text="releaseDialogConfirmText" :cancel-text="t('common.cancel')" danger @confirm="releaseAndReset" @cancel="releaseTarget = null" />
    <ConfirmDialog :show="resetCounterTarget !== null" :title="t('admin.ipAccessControl.failureStates.resetTitle')" :message="t('admin.ipAccessControl.failureStates.resetMessage', { ip: resetCounterTarget })" :confirm-text="t('admin.ipAccessControl.failureStates.resetCounter')" :cancel-text="t('common.cancel')" danger @confirm="resetCounter" @cancel="resetCounterTarget = null" />
    <ConfirmDialog
      :show="manualBlockTarget !== null"
      :title="t('admin.ipAccessControl.failureStates.manualBlockTitle')"
      :message="manualBlockDialogMessage"
      :confirm-text="manualBlockSubmitting ? t('admin.ipAccessControl.failureStates.blocking') : t('admin.ipAccessControl.failureStates.manualBlock')"
      :cancel-text="t('common.cancel')"
      :loading="manualBlockSubmitting"
      danger
      @confirm="manualBlockFailureState"
      @cancel="cancelManualBlock"
    />
    <teleport to="body">
      <transition name="modal">
        <div v-if="showDeployGuide" class="fixed inset-0 z-50 flex items-center justify-center p-4" @mousedown.self="showDeployGuide = false">
          <div class="fixed inset-0 bg-black/50" @click="showDeployGuide = false"></div>
          <div class="relative max-h-[85vh] w-full max-w-2xl overflow-y-auto rounded-xl bg-white p-6 shadow-2xl dark:bg-dark-800">
            <button type="button" class="absolute right-4 top-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200" :aria-label="t('common.close')" @click="showDeployGuide = false">
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
            <h2 class="mb-3 text-lg font-bold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.deployGuide.title') }}</h2>
            <p class="mb-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.deployGuide.intro') }}</p>
            <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ipAccessControl.deployGuide.whenNeeded') }}</p>

            <p class="mb-3 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.ipAccessControl.deployGuide.pickDeploy') }}</p>
            <div class="mb-5 flex flex-wrap gap-2">
              <button
                v-for="mode in deployGuideModes"
                :key="mode"
                type="button"
                class="rounded-lg px-3 py-1.5 text-sm"
                :class="deployGuideMode === mode ? 'bg-primary-600 text-white' : 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'"
                @click="deployGuideMode = mode"
              >
                {{ t(`admin.ipAccessControl.deployGuide.${mode}`) }}
              </button>
            </div>

            <div v-if="deployGuideMode === 'binary'" class="space-y-3 text-sm text-gray-600 dark:text-gray-300">
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.deployGuide.binaryTitle') }}</h3>
              <p>{{ t('admin.ipAccessControl.deployGuide.binaryWhere') }}</p>
              <ol class="ml-5 list-decimal space-y-1">
                <li>{{ t('admin.ipAccessControl.deployGuide.binaryStep1') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.binaryStep2') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.binaryStep3') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.binaryStep4') }}</li>
              </ol>
              <p>{{ t('admin.ipAccessControl.deployGuide.binaryYamlHint') }}</p>
              <pre class="overflow-x-auto rounded bg-gray-100 px-3 py-2 text-xs text-gray-800 dark:bg-dark-700 dark:text-gray-200">{{ t('admin.ipAccessControl.deployGuide.binaryYaml') }}</pre>
              <p>{{ t('admin.ipAccessControl.deployGuide.binaryStep5') }}</p>
              <code class="block rounded bg-gray-100 px-3 py-2 text-xs dark:bg-dark-700">{{ t('admin.ipAccessControl.deployGuide.binaryRestart') }}</code>
              <code class="block rounded bg-gray-100 px-3 py-2 text-xs dark:bg-dark-700">{{ t('admin.ipAccessControl.deployGuide.binaryStatus') }}</code>
              <p>{{ t('admin.ipAccessControl.deployGuide.binaryCustomFile') }}</p>
              <p>{{ t('admin.ipAccessControl.deployGuide.binaryEnvOverride') }}</p>
            </div>

            <div v-else-if="deployGuideMode === 'docker'" class="space-y-3 text-sm text-gray-600 dark:text-gray-300">
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.deployGuide.dockerTitle') }}</h3>
              <p>{{ t('admin.ipAccessControl.deployGuide.dockerWhere') }}</p>
              <ol class="ml-5 list-decimal space-y-1">
                <li>{{ t('admin.ipAccessControl.deployGuide.dockerStep1') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.dockerStep2') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.dockerStep3') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.dockerStep4') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.dockerStep5') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.dockerStep6') }}</li>
              </ol>
              <code class="block rounded bg-gray-100 px-3 py-2 text-xs dark:bg-dark-700">{{ t('admin.ipAccessControl.deployGuide.dockerRestart') }}</code>
              <code class="block rounded bg-gray-100 px-3 py-2 text-xs dark:bg-dark-700">{{ t('admin.ipAccessControl.deployGuide.dockerLogs') }}</code>
              <p>{{ t('admin.ipAccessControl.deployGuide.dockerFileHint') }}</p>
            </div>

            <div v-else class="space-y-3 text-sm text-gray-600 dark:text-gray-300">
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.deployGuide.standaloneTitle') }}</h3>
              <p>{{ t('admin.ipAccessControl.deployGuide.standaloneWhere') }}</p>
              <ol class="ml-5 list-decimal space-y-1">
                <li>{{ t('admin.ipAccessControl.deployGuide.standaloneStep1') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.standaloneStep2') }}</li>
              </ol>
            </div>

            <div class="mt-5 space-y-2 text-sm text-gray-600 dark:text-gray-300">
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ipAccessControl.deployGuide.proxyTitle') }}</h3>
              <p>{{ t('admin.ipAccessControl.deployGuide.proxyHint') }}</p>
              <p>{{ t('admin.ipAccessControl.deployGuide.proxyCloudflareHint') }}</p>
              <pre class="overflow-x-auto rounded bg-gray-100 px-3 py-2 text-xs text-gray-800 dark:bg-dark-700 dark:text-gray-200">{{ t('admin.ipAccessControl.deployGuide.proxyCloudflareHttp') }}</pre>
              <p>{{ t('admin.ipAccessControl.deployGuide.proxyHeaderHint') }}</p>
              <pre class="overflow-x-auto rounded bg-gray-100 px-3 py-2 text-xs text-gray-800 dark:bg-dark-700 dark:text-gray-200">{{ t('admin.ipAccessControl.deployGuide.proxyNginx') }}</pre>
              <p>{{ t('admin.ipAccessControl.deployGuide.proxyVerify') }}</p>
              <p>{{ t('admin.ipAccessControl.deployGuide.proxyTunnelHint') }}</p>
            </div>
            <div class="mt-5 rounded-lg bg-gray-50 p-3 text-sm text-gray-700 dark:bg-dark-700 dark:text-gray-300">
              <p class="font-medium">{{ t('admin.ipAccessControl.deployGuide.afterTitle') }}</p>
              <p class="mt-1">{{ t('admin.ipAccessControl.deployGuide.afterHint') }}</p>
            </div>
            <div class="mt-4 rounded-lg bg-amber-50 p-3 text-xs text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
              <p class="font-medium">{{ t('admin.ipAccessControl.deployGuide.warningsTitle') }}</p>
              <ul class="mt-2 list-disc space-y-1 pl-4">
                <li>{{ t('admin.ipAccessControl.deployGuide.warningCloudflare') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.warningEnv') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.warningLegacy') }}</li>
                <li>{{ t('admin.ipAccessControl.deployGuide.warningWizard') }}</li>
              </ul>
            </div>
            <div class="mt-4 text-right">
              <button type="button" class="btn btn-primary btn-sm" @click="showDeployGuide = false">{{ t('common.close') }}</button>
            </div>
          </div>
        </div>
      </transition>
    </teleport>
    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI, type IPAccessControlSettings, type IPLoginFailureState, type IPAccessRule, type IPAccessRuleKind, type IPAccessRuleStatus, type TrustedProxyStatus } from '@/api/admin'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const showDeployGuide = ref(false)
const deployGuideModes = ['binary', 'docker', 'standalone'] as const
const deployGuideMode = ref<(typeof deployGuideModes)[number]>('binary')
const proxyStatus = ref<TrustedProxyStatus | null>(null)
const proxyStatusLoading = ref(true)
const settings = reactive<IPAccessControlSettings>({ enforcement_enabled: false, login_failure_auto_block_enabled: false, login_failure_threshold: 8, login_failure_window_minutes: 15, login_failure_block_minutes: 1440 })
const settingsLoading = ref(true)
const settingsUnavailable = ref(false)
const settingsSaving = ref(false)
const failureStatesLoading = ref(false)
const failureStates = ref<IPLoginFailureState[]>([])
const failureStatesUpdatedAt = ref<Date | null>(null)
const failureStatesUnavailable = ref(false)
const failureTotal = ref(0)
const failurePage = ref(1)
const failurePageSize = ref(20)
const failureFilters = reactive({ query: '' })
let failureStatesRequestSequence = 0
const rulesLoading = ref(false)
const rules = ref<IPAccessRule[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filters = reactive<{ query: string; status: '' | IPAccessRuleStatus }>({ query: '', status: 'active' })
const createVisible = ref(false)
const creating = ref(false)
const releaseTarget = ref<IPAccessRule | null>(null)
const resetCounterTarget = ref<string | null>(null)
const manualBlockTarget = ref<IPLoginFailureState | null>(null)
const manualBlockSubmitting = ref(false)
const rulesSection = ref<HTMLElement | null>(null)
const releaseIsAllow = computed(() => releaseTarget.value?.rule_kind === 'allow')
const releaseDialogTitle = computed(() => t(releaseIsAllow.value ? 'admin.ipAccessControl.rules.removeAllowTitle' : 'admin.ipAccessControl.rules.releaseTitle'))
const releaseDialogMessage = computed(() => t(releaseIsAllow.value ? 'admin.ipAccessControl.rules.removeAllowMessage' : 'admin.ipAccessControl.rules.releaseMessage'))
const releaseDialogConfirmText = computed(() => t(releaseIsAllow.value ? 'admin.ipAccessControl.rules.removeAllow' : 'admin.ipAccessControl.rules.releaseReset'))
const manualBlockDialogMessage = computed(() => t('admin.ipAccessControl.failureStates.manualBlockMessage', {
  ip: manualBlockTarget.value?.normalized_ip || ''
}))
const createForm = reactive<{ ip_or_cidr: string; rule_kind: Extract<IPAccessRuleKind, 'manual_block' | 'allow'>; reason: string; expires_at: string }>({ ip_or_cidr: '', rule_kind: 'manual_block', reason: '', expires_at: '' })

const statusOptions = computed(() => [{ value: 'active', label: t('admin.ipAccessControl.rules.statusActive') }, { value: 'released', label: t('admin.ipAccessControl.rules.statusReleased') }, { value: 'expired', label: t('admin.ipAccessControl.rules.statusExpired') }])
// The backend returns arrays, but normalize them here as well so a stale
// deployment or malformed response cannot take down the complete admin view.
const trustedProxies = computed(() => Array.isArray(proxyStatus.value?.trusted_proxies) ? proxyStatus.value.trusted_proxies : [])
const forwardedHeaders = computed(() => Array.isArray(proxyStatus.value?.forwarded_headers) ? proxyStatus.value.forwarded_headers : [])
const featureEnabled = computed(() => appStore.cachedPublicSettings?.global_ip_access_control_enabled === true)
const automaticBlockingReady = computed(() => proxyStatus.value?.automatic_blocking_ready === true)
const manualBlockingReady = computed(() => proxyStatus.value?.manual_blocking_ready === true)
const createKindOptions = computed(() => [
  { value: 'manual_block', label: t('admin.ipAccessControl.rules.manualBlock'), disabled: !manualBlockingReady.value },
  { value: 'allow', label: t('admin.ipAccessControl.rules.allow') }
])
const failureStateColumns = computed<Column[]>(() => [
  { key: 'normalized_ip', label: t('admin.ipAccessControl.failureStates.ip') },
  { key: 'failure_count', label: t('admin.ipAccessControl.failureStates.failureCount') },
  { key: 'window_started_at', label: t('admin.ipAccessControl.failureStates.windowStartedAt') },
  { key: 'last_failed_at', label: t('admin.ipAccessControl.failureStates.lastFailedAt') },
  { key: 'window_expires_at', label: t('admin.ipAccessControl.failureStates.windowExpiresAt') },
  { key: 'effectively_blocked', label: t('admin.ipAccessControl.failureStates.blockStatus') },
  { key: 'actions', label: t('common.actions') }
])
const columns = computed<Column[]>(() => [{ key: 'ip_or_cidr', label: t('admin.ipAccessControl.rules.ip') }, { key: 'rule_kind', label: t('admin.ipAccessControl.rules.type') }, { key: 'status', label: t('common.status') }, { key: 'failure_count', label: t('admin.ipAccessControl.rules.failureCount') }, { key: 'hit_count', label: t('admin.ipAccessControl.rules.hitCount') }, { key: 'last_seen_at', label: t('admin.ipAccessControl.rules.lastSeenAt') }, { key: 'expires_at', label: t('admin.ipAccessControl.rules.expiresAt') }, { key: 'actions', label: t('common.actions') }])
const proxyStateLabel = computed(() => t(`admin.ipAccessControl.trustedProxy.states.${proxyStatus.value?.configuration_state || 'not_configured'}`))
const proxyStateClass = computed(() => {
  const state = proxyStatus.value?.configuration_state
  if (state === 'configured') return 'rounded bg-emerald-100 px-2 py-1 text-xs font-medium text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
  if (state === 'invalid') return 'rounded bg-rose-100 px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300'
  return 'rounded bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
})
const proxyStatusMessage = computed(() => {
  const status = proxyStatus.value
  if (!status || status.configuration_state === 'not_configured') return t('admin.ipAccessControl.trustedProxy.messages.notConfigured')
  if (status.configuration_state === 'empty') return t('admin.ipAccessControl.trustedProxy.messages.empty')
  if (status.configuration_state === 'invalid') return t('admin.ipAccessControl.trustedProxy.messages.invalid')
  if (status.trusted_proxy_applied) return t('admin.ipAccessControl.trustedProxy.messages.applied')
  if (status.direct_peer_trusted) return t('admin.ipAccessControl.trustedProxy.messages.trustedPeerNoForwarded')
  return t('admin.ipAccessControl.trustedProxy.messages.notApplied')
})
const proxyStatusMessageClass = computed(() => {
  const status = proxyStatus.value
  if (status?.configuration_state === 'configured' && status.trusted_proxy_applied) return 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-800/70 dark:bg-emerald-950/30 dark:text-emerald-200'
  if (status?.configuration_state === 'invalid') return 'border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-800/70 dark:bg-rose-950/30 dark:text-rose-200'
  return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800/70 dark:bg-amber-950/30 dark:text-amber-200'
})
function formatTimestamp(value?: string): string { return value ? new Date(value).toLocaleString() : '—' }
function formatExpiryTime(value?: string): string { return value ? new Date(value).toLocaleString() : t('admin.ipAccessControl.rules.never') }
function formatLastSeenTime(value?: string): string { return value ? new Date(value).toLocaleString() : t('admin.ipAccessControl.rules.lastSeenNever') }
function statusLabel(rule: IPAccessRule): string {
  if (rule.status === 'active') return t('admin.ipAccessControl.rules.statusActive')
  if (rule.status === 'released') return t(rule.rule_kind === 'allow' ? 'admin.ipAccessControl.rules.statusRemoved' : 'admin.ipAccessControl.rules.statusReleased')
  return t('admin.ipAccessControl.rules.statusExpired')
}
function kindLabel(value: IPAccessRuleKind): string { return value === 'allow' ? t('admin.ipAccessControl.rules.allow') : value === 'auto_block' ? t('admin.ipAccessControl.rules.autoBlock') : t('admin.ipAccessControl.rules.manualBlock') }
function statusClass(value: IPAccessRuleStatus): string { return value === 'active' ? 'rounded bg-emerald-100 px-2 py-1 text-xs font-medium text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300' : 'rounded bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300' }
function kindClass(value: IPAccessRuleKind): string { return value === 'allow' ? 'rounded bg-sky-100 px-2 py-1 text-xs font-medium text-sky-700 dark:bg-sky-950/50 dark:text-sky-300' : value === 'auto_block' ? 'rounded bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300' : 'rounded bg-rose-100 px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300' }
function failureBlockStatusLabel(state: IPLoginFailureState): string {
  if (state.effectively_blocked) return t('admin.ipAccessControl.failureStates.blocked')
  if (state.emergency_allowlisted) return t('admin.ipAccessControl.failureStates.emergencyAllowlisted')
  if (state.suppressed_by_allow_rule) return t('admin.ipAccessControl.failureStates.suppressedByAllow')
  if (state.active_block_rule && !state.runtime_enforcement_enabled) return t('admin.ipAccessControl.failureStates.ruleNotEnforced')
  if (state.active_block_rule) return t('admin.ipAccessControl.failureStates.ruleActive')
  return t('admin.ipAccessControl.failureStates.observing', { count: state.failure_count, threshold: state.failure_threshold })
}
function failureBlockStatusClass(state: IPLoginFailureState): string {
  if (state.effectively_blocked) return 'rounded bg-rose-100 px-2 py-1 text-xs font-medium text-rose-700 dark:bg-rose-950/50 dark:text-rose-300'
  if (state.emergency_allowlisted) return 'rounded bg-violet-100 px-2 py-1 text-xs font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300'
  if (state.suppressed_by_allow_rule) return 'rounded bg-sky-100 px-2 py-1 text-xs font-medium text-sky-700 dark:bg-sky-950/50 dark:text-sky-300'
  if (state.active_block_rule) return 'rounded bg-amber-100 px-2 py-1 text-xs font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
  return 'rounded bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}
function manualBlockDisabledReason(state: IPLoginFailureState): string {
  if (failureStatesUnavailable.value) return t('admin.ipAccessControl.failureStates.manualBlockDisabledStale')
  if (state.emergency_allowlisted) return t('admin.ipAccessControl.failureStates.manualBlockDisabledEmergencyAllow')
  if (state.suppressed_by_allow_rule) return t('admin.ipAccessControl.failureStates.manualBlockDisabledAllow')
  if (state.active_block_rule || state.effectively_blocked) return t('admin.ipAccessControl.failureStates.manualBlockDisabledAlreadyBlocked')
  if (!state.runtime_enforcement_enabled) return t('admin.ipAccessControl.failureStates.manualBlockDisabledEnforcement')
  if (!manualBlockingReady.value) return t('admin.ipAccessControl.failureStates.manualBlockDisabledIdentity')
  if (settingsSaving.value || failureStatesLoading.value || manualBlockSubmitting.value) return t('admin.ipAccessControl.failureStates.manualBlockDisabledBusy')
  return ''
}
function canManuallyBlockFailureState(state: IPLoginFailureState): boolean {
  return manualBlockDisabledReason(state) === ''
}
function reportSensitiveError(error: unknown): void {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    appStore.showError(
      stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
        ? t('stepUp.adminApiKeyForbidden')
        : t('stepUp.notEnabled')
    )
    return
  }
  appStore.showError(extractApiErrorMessage(error, t('common.error')))
}
async function loadSettings() {
  settingsLoading.value = true
  try {
    Object.assign(settings, await adminAPI.ipAccessControl.getSettings())
    settingsUnavailable.value = false
  } catch (error) {
    settingsUnavailable.value = true
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    settingsLoading.value = false
  }
}
async function loadProxyStatus() { proxyStatusLoading.value = true; try { proxyStatus.value = await adminAPI.ipAccessControl.getTrustedProxyStatus() } catch (error) { appStore.showError(extractApiErrorMessage(error, t('common.error'))) } finally { proxyStatusLoading.value = false } }
async function saveSettings() {
  settingsSaving.value = true
  try {
    if (!settings.enforcement_enabled) settings.login_failure_auto_block_enabled = false
    Object.assign(settings, await stepUp.run(() => adminAPI.ipAccessControl.updateSettings({ ...settings })))
    settingsUnavailable.value = false
    await loadFailureStates()
    appStore.showSuccess(t('common.saved'))
  } catch (error) {
    reportSensitiveError(error)
    await Promise.all([loadSettings(), loadProxyStatus()])
  } finally {
    settingsSaving.value = false
  }
}
function failureStateQuery() {
  return {
    page: failurePage.value,
    page_size: failurePageSize.value,
    query: failureFilters.query || undefined
  }
}
async function loadFailureStates() {
  const requestSequence = ++failureStatesRequestSequence
  const requestedPage = failurePage.value
  failureStatesLoading.value = true
  try {
    let result = await adminAPI.ipAccessControl.listFailureStates(failureStateQuery())
    if (requestSequence !== failureStatesRequestSequence) return
    const lastPage = Math.max(1, Math.ceil(result.total / failurePageSize.value))
    if (requestedPage > lastPage) {
      failurePage.value = lastPage
      result = await adminAPI.ipAccessControl.listFailureStates(failureStateQuery())
      if (requestSequence !== failureStatesRequestSequence) return
    }
    failureStates.value = result.items
    failureTotal.value = result.total
    failureStatesUpdatedAt.value = new Date()
    failureStatesUnavailable.value = false
  } catch (error) {
    if (requestSequence !== failureStatesRequestSequence) return
    failureStatesUnavailable.value = true
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    if (requestSequence === failureStatesRequestSequence) failureStatesLoading.value = false
  }
}
function searchFailureStates() { failurePage.value = 1; void loadFailureStates() }
function refreshFailureStates() { if (!failureStatesLoading.value) void loadFailureStates() }
function changeFailurePage(value: number) { failurePage.value = value; void loadFailureStates() }
function changeFailurePageSize(value: number) { failurePageSize.value = value; failurePage.value = 1; void loadFailureStates() }
function ruleQuery() {
  return {
    page: page.value,
    page_size: pageSize.value,
    query: filters.query || undefined,
    status: filters.status || undefined
  }
}
async function loadRules() {
  rulesLoading.value = true
  try {
    let result = await adminAPI.ipAccessControl.listRules(ruleQuery())
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize.value))
    if (page.value > lastPage) {
      page.value = lastPage
      result = await adminAPI.ipAccessControl.listRules(ruleQuery())
    }
    rules.value = result.items
    total.value = result.total
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    rulesLoading.value = false
  }
}
function search() { page.value = 1; void loadRules() }
function changePage(value: number) { page.value = value; void loadRules() }
function changePageSize(value: number) { pageSize.value = value; page.value = 1; void loadRules() }
function openCreateDialog() { Object.assign(createForm, { ip_or_cidr: '', rule_kind: manualBlockingReady.value ? 'manual_block' : 'allow', reason: '', expires_at: '' }); createVisible.value = true }
async function createRule() {
  creating.value = true
  try {
    await stepUp.run(() => adminAPI.ipAccessControl.createRule({ ip_or_cidr: createForm.ip_or_cidr, rule_kind: createForm.rule_kind, reason: createForm.reason || undefined, expires_at: createForm.expires_at ? new Date(createForm.expires_at).toISOString() : undefined }))
    createVisible.value = false
    await Promise.all([loadRules(), loadFailureStates()])
    appStore.showSuccess(t('common.saved'))
  } catch (error) {
    reportSensitiveError(error)
  } finally {
    creating.value = false
  }
}
function confirmRelease(rule: IPAccessRule) { releaseTarget.value = rule }
async function releaseAndReset() {
  const rule = releaseTarget.value
  if (!rule) return
  try {
    await stepUp.run(() => adminAPI.ipAccessControl.releaseRuleAndReset(rule.id))
    releaseTarget.value = null
    await Promise.all([loadRules(), loadFailureStates()])
    appStore.showSuccess(t('common.saved'))
  } catch (error) {
    if (isStepUpCancelled(error)) releaseTarget.value = null
    reportSensitiveError(error)
  }
}
function confirmResetCounter(ip: string) { resetCounterTarget.value = ip }
async function resetCounter() { const ip = resetCounterTarget.value; if (!ip) return; try { await stepUp.run(() => adminAPI.ipAccessControl.resetFailureState(ip)); resetCounterTarget.value = null; appStore.showSuccess(t('admin.ipAccessControl.failureStates.resetSuccess')); await Promise.all([loadFailureStates(), loadRules()]) } catch (error) { if (isStepUpCancelled(error)) resetCounterTarget.value = null; reportSensitiveError(error) } }
function confirmManualBlock(state: IPLoginFailureState) {
  if (!canManuallyBlockFailureState(state)) return
  const currentState = failureStates.value.find((item) => item.normalized_ip === state.normalized_ip)
  if (!currentState) {
    appStore.showError(t('admin.ipAccessControl.failureStates.manualBlockDisabledStale'))
    return
  }
  if (canManuallyBlockFailureState(currentState)) manualBlockTarget.value = currentState
}
function cancelManualBlock() {
  if (!manualBlockSubmitting.value) manualBlockTarget.value = null
}
async function manualBlockFailureState() {
  const target = manualBlockTarget.value
  if (!target || manualBlockSubmitting.value) return
  const currentState = failureStates.value.find((state) => state.normalized_ip === target.normalized_ip)
  if (!currentState) {
    manualBlockTarget.value = null
    appStore.showError(t('admin.ipAccessControl.failureStates.manualBlockDisabledStale'))
    return
  }
  if (!canManuallyBlockFailureState(currentState)) {
    manualBlockTarget.value = null
    appStore.showError(manualBlockDisabledReason(currentState))
    return
  }
  manualBlockSubmitting.value = true
  try {
    const result = await stepUp.run(() => adminAPI.ipAccessControl.blockFailureState({ ip: target.normalized_ip }))
    manualBlockTarget.value = null
    appStore.showSuccess(t(result.already_blocked
      ? 'admin.ipAccessControl.failureStates.alreadyBlocked'
      : 'admin.ipAccessControl.failureStates.manualBlockSuccess'))
    await Promise.all([loadFailureStates(), loadRules()])
  } catch (error) {
    manualBlockTarget.value = null
    reportSensitiveError(error)
    if (!isStepUpCancelled(error)) await Promise.all([loadFailureStates(), loadRules()])
  } finally {
    manualBlockSubmitting.value = false
  }
}
async function focusRulesForFailureState(state: IPLoginFailureState, showAllActive = false) {
  filters.status = 'active'
  filters.query = showAllActive ? '' : (state.block_rule_ip_or_cidr || state.normalized_ip)
  page.value = 1
  await loadRules()
  await nextTick()
  rulesSection.value?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
}
onMounted(() => {
  void loadProxyStatus()
  void loadSettings()
  void loadFailureStates()
  void loadRules()
})
</script>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>
