<template>
  <AppLayout workspace>
    <div class="workspace-page min-w-0" data-testid="quality-inspection">
      <header class="workspace-fixed card p-5 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0">
            <h1 class="flex items-center gap-2.5 text-xl font-semibold tracking-tight text-gray-900 dark:text-white">
              <span class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary-500/15 text-primary-600 dark:text-primary-400">
                <Icon name="shield" size="sm" />
              </span>
              <span class="truncate">{{ copy.title }}</span>
            </h1>
            <p class="mt-1.5 text-sm surface-muted">{{ copy.subtitle }}</p>
          </div>
          <div class="flex shrink-0 flex-wrap items-center gap-2">
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="reload">
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
              <span>{{ copy.refresh }}</span>
            </button>
            <button
              v-support-readonly
              type="button"
              class="btn btn-primary"
              :disabled="loading || !settings?.enabled"
              @click="startCreate"
            >
              <Icon name="plus" size="sm" />
              <span>{{ copy.newPlan }}</span>
            </button>
          </div>
        </div>

        <div
          v-if="!loading && !settings?.enabled"
          class="surface-subcard mt-4 flex flex-wrap items-center justify-between gap-3 border-amber-500/35 bg-amber-500/10 px-3.5 py-2.5 text-sm text-amber-800 dark:text-amber-200"
          role="status"
        >
          <div class="flex min-w-0 items-start gap-2">
            <Icon name="exclamationTriangle" size="sm" class="mt-0.5 shrink-0 text-amber-600 dark:text-amber-400" />
            <span>{{ copy.disabledHint }}</span>
          </div>
          <router-link
            to="/admin/settings"
            class="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-amber-800 hover:underline dark:text-amber-200"
          >
            <span>{{ copy.settingsLink }}</span>
            <Icon name="arrowRight" size="xs" />
          </router-link>
        </div>

        <div class="surface-subcard mt-3 flex items-start gap-2 px-3.5 py-2.5 text-xs surface-muted">
          <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0 opacity-80" />
          <p class="leading-relaxed">{{ copy.limitations }}</p>
        </div>
      </header>

      <div class="workspace-scroll">
        <div
          v-if="error"
          class="card flex items-start gap-2.5 border-red-500/35 bg-red-500/10 p-4 text-sm text-red-700 dark:text-red-300"
          role="alert"
        >
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
          <span class="min-w-0 break-words">{{ error }}</span>
        </div>

        <div class="card min-w-0 overflow-hidden">
          <div v-if="loading && !plans.length" class="flex flex-col items-center justify-center gap-2 py-14 text-sm surface-muted">
            <Icon name="refresh" size="md" class="animate-spin opacity-70" />
            <span>{{ copy.loading }}</span>
          </div>

          <EmptyState
            v-else-if="!plans.length"
            :title="copy.empty"
            :description="copy.emptyHint"
          >
            <template #icon>
              <Icon name="shield" size="lg" class="empty-state-icon" />
            </template>
            <template #action>
              <button
                v-support-readonly
                type="button"
                class="btn btn-primary"
                :disabled="loading || !settings?.enabled"
                @click="startCreate"
              >
                <Icon name="plus" size="sm" />
                <span>{{ copy.newPlan }}</span>
              </button>
            </template>
          </EmptyState>

          <template v-else>
            <div class="record-table-scroll overflow-x-auto">
              <table class="record-table w-full text-sm [&_th]:px-4 [&_th]:py-3 [&_td]:px-4 [&_td]:py-3">
                <thead>
                  <tr>
                    <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.plan }}</th>
                    <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.group }}</th>
                    <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.model }}</th>
                    <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.status }}</th>
                    <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.estimate }}</th>
                    <th class="text-right text-xs font-medium uppercase tracking-wider">{{ copy.actions }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="plan in pagedPlans"
                    :key="plan.id"
                    :class="{ 'table-row-selected': activePlan?.id === plan.id }"
                  >
                    <td>
                      <div class="font-medium text-gray-900 dark:text-white">{{ plan.name }}</div>
                      <div class="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs surface-muted">
                        <Icon name="clock" size="xs" class="opacity-75" />
                        <span v-if="plan.schedule_enabled" class="tabular-nums">
                          {{ t('admin.qualityInspection.intervalUnit', { minutes: plan.interval_minutes }) }}
                        </span>
                        <span v-else>{{ copy.manualOnly }}</span>
                      </div>
                    </td>
                    <td>
                      <div class="flex items-center gap-2">
                        <span class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-[var(--surface-edge)] bg-[var(--surface-soft)] surface-muted">
                          <PlatformIcon :platform="(plan.platform as GroupPlatform)" size="sm" />
                        </span>
                        <div class="min-w-0">
                          <div class="truncate font-medium text-gray-900 dark:text-white">{{ plan.group_name }}</div>
                          <div class="text-xs surface-muted">{{ plan.platform }}</div>
                        </div>
                      </div>
                    </td>
                    <td>
                      <div class="flex flex-wrap items-center gap-1.5">
                        <code class="rounded border border-[var(--surface-edge)] bg-[var(--surface-soft)] px-1.5 py-0.5 font-mono text-xs text-gray-900 dark:text-white">
                          {{ plan.model }}
                        </code>
                        <span class="badge badge-gray text-[11px]">{{ plan.effort }}</span>
                      </div>
                    </td>
                    <td>
                      <div class="flex flex-wrap items-center gap-1.5">
                        <span :class="['badge', plan.enabled ? 'badge-success' : 'badge-gray']">
                          {{ plan.enabled ? copy.enabled : copy.paused }}
                        </span>
                        <span v-if="plan.schedule_enabled" class="badge badge-primary text-[11px]">
                          {{ copy.schedule }}
                        </span>
                      </div>
                    </td>
                    <td>
                      <div class="font-medium tabular-nums text-gray-900 dark:text-white">
                        {{ plan.estimated_requests }}
                        <span class="ml-0.5 text-xs font-normal surface-muted">{{ copy.requests }}</span>
                      </div>
                      <div class="mt-0.5 text-xs tabular-nums surface-muted">
                        {{ t('admin.qualityInspection.accountsUnit', { count: plan.max_accounts }) }}
                      </div>
                    </td>
                    <td class="text-right">
                      <div class="flex items-center justify-end gap-1 whitespace-nowrap">
                        <button v-support-readonly type="button" class="table-action" @click="startEdit(plan)">
                          <Icon name="edit" size="sm" />
                          <span>{{ copy.edit }}</span>
                        </button>
                        <button
                          type="button"
                          class="table-action"
                          :class="{ 'table-action-active': activePlan?.id === plan.id }"
                          @click="loadRuns(plan)"
                        >
                          <Icon name="clock" size="sm" />
                          <span>{{ copy.history }}</span>
                        </button>
                        <ActionMenu
                          :items="[
                            { key: 'run', label: copy.run, icon: 'play', mutates: true, disabled: !plan.enabled || busy },
                            { key: 'delete', label: copy.remove, icon: 'trash', mutates: true, tone: 'danger', disabled: busy },
                          ]"
                          @select="onPlanAction(plan, $event)"
                        />
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="px-4 pb-3 sm:px-5">
              <Pagination
                :page="planPage"
                :page-size="10"
                :total="plans.length"
                :disabled="loading"
                :show-page-size-selector="false"
                embedded
                @update:page="planPage = $event"
              />
            </div>
          </template>
        </div>

        <BaseDialog
          :show="Boolean(draft)"
          :title="draft?.id ? copy.editPlan : copy.newPlan"
          width="extra-wide"
          @close="closeDraft"
        >
          <div v-if="draft" class="space-y-5">
            <div
              v-if="error"
              class="surface-subcard flex items-start gap-2 border-red-500/40 bg-red-500/10 p-3 text-sm text-red-600 dark:text-red-400"
              role="alert"
            >
              <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
              <span>{{ error }}</span>
            </div>

            <section class="surface-subcard space-y-4 p-4">
              <h3 class="text-xs font-semibold uppercase tracking-wider surface-muted">
                {{ copy.sectionTarget }}
              </h3>
              <div class="grid gap-4 md:grid-cols-2">
                <label class="block">
                  <span class="input-label">{{ copy.name }}</span>
                  <input
                    v-model="draft.name"
                    name="plan-name"
                    class="input"
                    maxlength="120"
                    :placeholder="copy.namePlaceholder"
                  />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.group }}</span>
                  <Select v-model="draft.group_id" :options="groupOptions" :disabled="Boolean(draft.id)" />
                </label>
              </div>
              <div class="grid gap-4 md:grid-cols-3">
                <label class="block">
                  <span class="input-label">{{ copy.model }}</span>
                  <input
                    v-model="draft.model"
                    name="plan-model"
                    class="input font-mono text-sm"
                    :placeholder="copy.modelPlaceholder"
                  />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.effort }}</span>
                  <Select v-model="draft.effort" :options="effortOptions" :searchable="false" />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.endpoint }}</span>
                  <Select v-model="draft.endpoint" :options="endpointOptions" :searchable="false" />
                </label>
              </div>
              <p class="text-xs surface-muted">{{ copy.endpointHint }}</p>
            </section>

            <section class="surface-subcard space-y-4 p-4">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <h3 class="text-xs font-semibold uppercase tracking-wider surface-muted">
                  {{ copy.sectionSchedule }}
                </h3>
                <div class="flex flex-wrap items-center gap-x-5 gap-y-2">
                  <label class="flex cursor-pointer items-center gap-2 text-sm text-gray-900 dark:text-white">
                    <Toggle v-model="draft.enabled" />
                    <span>{{ copy.enabled }}</span>
                  </label>
                  <label class="flex cursor-pointer items-center gap-2 text-sm text-gray-900 dark:text-white">
                    <Toggle v-model="draft.schedule_enabled" />
                    <span>{{ copy.schedule }}</span>
                  </label>
                  <label
                    class="flex items-center gap-2 text-sm text-gray-900 dark:text-white"
                    :class="settings?.quarantine_enabled ? 'cursor-pointer' : 'cursor-not-allowed opacity-50'"
                  >
                    <Toggle v-model="draft.quarantine_enabled" :disabled="!settings?.quarantine_enabled" />
                    <span>{{ copy.quarantine }}</span>
                  </label>
                </div>
              </div>

              <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <label class="block">
                  <span class="input-label">{{ copy.accounts }}</span>
                  <input v-model.number="draft.max_accounts" type="number" min="1" max="5" class="input tabular-nums" />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.maxRounds }}</span>
                  <input v-model.number="draft.max_saved_rounds" type="number" min="1" max="200" class="input tabular-nums" />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.interval }}</span>
                  <input v-model.number="draft.interval_minutes" type="number" min="30" max="1440" class="input tabular-nums" />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.pauseStart }}</span>
                  <input v-model="draft.pause_start" type="time" class="input tabular-nums" />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.pauseEnd }}</span>
                  <input v-model="draft.pause_end" type="time" class="input tabular-nums" />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.timezone }}</span>
                  <input v-model="draft.timezone" class="input" placeholder="Asia/Shanghai" />
                </label>
              </div>

              <div
                class="grid gap-4 border-t border-[var(--surface-edge)] pt-4 sm:grid-cols-2"
                :class="{ 'opacity-60': !settings?.quarantine_enabled || !draft.quarantine_enabled }"
              >
                <label class="block">
                  <span class="input-label">{{ copy.cooldown }}</span>
                  <input
                    v-model.number="draft.quarantine_cooldown_minutes"
                    type="number"
                    min="5"
                    max="10080"
                    class="input tabular-nums"
                  />
                </label>
                <label class="block">
                  <span class="input-label">{{ copy.failRounds }}</span>
                  <input
                    v-model.number="draft.quarantine_fail_rounds"
                    type="number"
                    min="1"
                    max="5"
                    class="input tabular-nums"
                  />
                </label>
              </div>
            </section>

            <section class="space-y-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h3 class="text-sm font-medium text-gray-900 dark:text-white">{{ copy.questions }}</h3>
                <span class="badge badge-gray tabular-nums">
                  {{ t('admin.qualityInspection.enabledCount', { active: enabledQuestionCount, total: draft.questions.length }) }}
                </span>
              </div>

              <div class="space-y-3">
                <div
                  v-for="question in draft.questions"
                  :key="question.id"
                  class="surface-subcard p-3.5 transition-opacity"
                  :class="{ 'opacity-60': !question.enabled }"
                >
                  <div class="flex flex-wrap items-center justify-between gap-3">
                    <label class="flex cursor-pointer items-center gap-2.5 text-sm font-medium text-gray-900 dark:text-white">
                      <input
                        v-model="question.enabled"
                        type="checkbox"
                        class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600"
                      />
                      <span>{{ question.title || question.id }}</span>
                      <span :class="['badge text-[11px]', kindBadgeClass(question.kind)]">
                        {{ kindLabel(question.kind) }}
                      </span>
                    </label>
                    <label class="flex items-center gap-2 text-xs surface-muted">
                      <span class="whitespace-nowrap">{{ copy.rounds }}</span>
                      <input
                        v-model.number="question.rounds"
                        type="number"
                        min="1"
                        max="3"
                        class="input w-20 tabular-nums"
                      />
                    </label>
                  </div>

                  <div class="mt-3 grid gap-3" :class="showExpectedField(question) ? 'md:grid-cols-[minmax(0,1fr)_220px]' : ''">
                    <label class="block min-w-0">
                      <span class="input-label text-xs">{{ copy.prompt }}</span>
                      <textarea
                        v-model="question.prompt"
                        rows="2"
                        class="input w-full resize-y text-sm"
                        :placeholder="copy.prompt"
                      />
                    </label>
                    <label v-if="showExpectedField(question)" class="block min-w-0">
                      <span class="input-label text-xs">{{ copy.expected }}</span>
                      <input v-model="question.expected" class="input font-mono text-sm" />
                    </label>
                  </div>
                </div>
              </div>
            </section>
          </div>

          <template #footer>
            <div class="flex w-full flex-wrap items-center justify-between gap-3">
              <div class="flex flex-wrap items-center gap-2 text-xs">
                <span class="surface-muted">{{ copy.estimateHint }}</span>
                <span :class="['badge tabular-nums', exceedsMaxRequests ? 'badge-warning' : 'badge-primary']">
                  {{ estimated }} {{ copy.requests }}
                </span>
                <span v-if="exceedsMaxRequests" class="text-amber-600 dark:text-amber-400">
                  {{ t('admin.qualityInspection.maxRequestsExceeded', { limit: settings?.max_requests_per_round }) }}
                </span>
              </div>
              <div class="flex shrink-0 items-center gap-2">
                <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeDraft">
                  {{ copy.cancel }}
                </button>
                <button
                  v-support-readonly
                  type="button"
                  class="btn btn-primary"
                  :disabled="saving || !draft?.group_id || !draft?.model || !draft?.questions.some(q => q.enabled)"
                  @click="savePlan"
                >
                  {{ saving ? copy.saving : copy.save }}
                </button>
              </div>
            </div>
          </template>
        </BaseDialog>

        <ConfirmDialog
          :show="Boolean(confirmation)"
          :title="confirmation?.kind === 'delete' ? copy.deleteTitle : copy.runTitle"
          :message="confirmationMessage"
          :danger="confirmation?.kind === 'delete'"
          :loading="busy"
          @confirm="confirmPlanAction"
          @cancel="confirmation = null"
        />

        <section v-if="activePlan" class="card min-w-0 space-y-4 p-5">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="flex min-w-0 flex-wrap items-center gap-2.5">
              <span class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-[var(--surface-edge)] bg-[var(--surface-soft)] surface-muted">
                <PlatformIcon :platform="(activePlan.platform as GroupPlatform)" size="sm" />
              </span>
              <div class="min-w-0">
                <h2 class="truncate text-base font-semibold text-gray-900 dark:text-white">
                  {{ copy.history }} · {{ activePlan.name }}
                </h2>
                <div class="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs surface-muted">
                  <span>{{ activePlan.group_name }}</span>
                  <span>·</span>
                  <code class="font-mono">{{ activePlan.model }}</code>
                </div>
              </div>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="compareIds.length < 2"
                @click="runCompare"
              >
                <Icon name="chart" size="sm" />
                <span>{{ copy.compare }} ({{ compareIds.length }})</span>
              </button>
              <button type="button" class="btn btn-secondary" @click="closeHistory">
                <Icon name="x" size="sm" />
                <span>{{ copy.close }}</span>
              </button>
            </div>
          </div>

          <div class="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
            <div class="min-w-0 space-y-2">
              <button
                v-for="run in pagedRuns"
                :key="run.id"
                type="button"
                class="surface-subcard-interactive w-full p-3 text-left text-sm"
                :class="activeRun?.id === run.id ? 'border-primary-500 bg-primary-500/10 ring-1 ring-primary-500/30' : ''"
                @click="openRun(run)"
              >
                <div class="flex items-center justify-between gap-2">
                  <span :class="['badge', statusClass(String(run.stats?.verdict ?? run.status))]">
                    {{ statusLabel(String(run.stats?.verdict ?? run.status)) }}
                  </span>
                  <span class="text-xs tabular-nums surface-muted">{{ formatTime(run.started_at) }}</span>
                </div>
                <div class="mt-2 flex items-center justify-between gap-2 text-xs surface-muted">
                  <span>{{ triggerLabel(run.trigger) }}</span>
                  <span class="tabular-nums">{{ run.request_count }} {{ copy.requests }}</span>
                </div>
              </button>

              <div v-if="historyLoading" class="surface-subcard flex items-center justify-center gap-2 p-6 text-sm surface-muted">
                <Icon name="refresh" size="sm" class="animate-spin" />
                <span>{{ copy.loading }}</span>
              </div>
              <EmptyState v-else-if="!runs.length" size="sm" :title="copy.noRuns" />

              <Pagination
                v-if="runs.length"
                :page="runPage"
                :page-size="10"
                :total="runs.length"
                :disabled="historyLoading"
                :show-page-size-selector="false"
                embedded
                @update:page="runPage = $event"
              />
            </div>

            <div v-if="activeRun" class="min-w-0 space-y-3">
              <div class="surface-subcard flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5 text-xs">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <PlatformIcon :platform="((activeRun.resolved_platform || activePlan.platform) as GroupPlatform)" size="sm" />
                  <span class="font-medium text-gray-900 dark:text-white">{{ activeRun.resolved_platform || '—' }}</span>
                  <span class="surface-muted">/</span>
                  <code class="rounded border border-[var(--surface-edge)] bg-[var(--surface-soft)] px-1.5 py-0.5 font-mono text-gray-900 dark:text-white">
                    {{ activeRun.resolved_upstream_model || activeRun.model }}
                  </code>
                </div>
                <span class="surface-muted">{{ activeRun.message || statusLabel(activeRun.status) }}</span>
              </div>

              <div class="record-table-scroll overflow-x-auto">
                <table class="record-table record-table-compact w-full text-sm [&_th]:px-4 [&_th]:py-3 [&_td]:px-4 [&_td]:py-3">
                  <thead>
                    <tr>
                      <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.question }}</th>
                      <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.verdict }}</th>
                      <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.answer }}</th>
                      <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.duration }}</th>
                      <th class="text-left text-xs font-medium uppercase tracking-wider">{{ copy.compare }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="result in pagedResults" :key="result.id">
                      <td>
                        <div class="font-medium text-gray-900 dark:text-white">{{ result.question_id }}</div>
                        <span :class="['badge mt-1 text-[11px]', kindBadgeClass(result.kind)]">
                          {{ kindLabel(result.kind) }}
                        </span>
                      </td>
                      <td>
                        <span :class="['badge', statusClass(result.status)]">{{ statusLabel(result.status) }}</span>
                      </td>
                      <td class="max-w-sm break-words text-xs">
                        <span v-if="result.answer" class="font-medium text-gray-900 dark:text-white">{{ result.answer }}</span>
                        <span v-else-if="result.error_code" class="font-mono text-red-600 dark:text-red-400">{{ result.error_code }}</span>
                        <span v-else class="surface-muted">—</span>
                        <div class="mt-1 surface-muted">{{ copy.expected }}: {{ result.expected || '—' }}</div>
                        <details v-if="result.response" class="mt-2">
                          <summary class="cursor-pointer font-medium text-primary-600 hover:underline dark:text-primary-400">
                            {{ copy.rawResponse }}
                          </summary>
                          <pre class="surface-subcard mt-1.5 max-h-60 overflow-auto whitespace-pre-wrap break-words p-2.5 font-mono text-[11px]">{{ result.response }}</pre>
                        </details>
                      </td>
                      <td class="whitespace-nowrap text-xs tabular-nums">
                        {{ result.duration_ms != null ? `${result.duration_ms} ms` : '—' }}
                      </td>
                      <td>
                        <label class="flex items-center gap-2 text-xs">
                          <input
                            type="checkbox"
                            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600"
                            :aria-label="`${copy.compare} ${result.question_id}`"
                            :disabled="compareIds.length >= 4 && !compareIds.includes(result.id)"
                            :checked="compareIds.includes(result.id)"
                            @change="toggleCompare(result)"
                          />
                          <button v-if="result.has_artifact" type="button" class="table-action" @click="preview(result)">
                            <Icon name="eye" size="sm" />
                            <span>{{ copy.viewArtifact }}</span>
                          </button>
                        </label>
                      </td>
                    </tr>
                    <tr v-if="resultsLoading || !results.length">
                      <td colspan="5" class="py-6 text-center surface-muted">
                        {{ resultsLoading ? copy.loading : copy.noResults }}
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div class="px-2">
                <Pagination
                  :page="resultPage"
                  :page-size="20"
                  :total="results.length"
                  :disabled="resultsLoading"
                  :show-page-size-selector="false"
                  embedded
                  @update:page="resultPage = $event"
                />
              </div>
            </div>
          </div>

          <div v-if="artifact" class="surface-subcard overflow-hidden">
            <div class="flex items-center justify-between border-b border-[var(--surface-edge)] px-3.5 py-2.5">
              <h3 class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                <Icon name="eye" size="sm" class="surface-muted" />
                <span>{{ copy.artifact }}</span>
              </h3>
              <button type="button" class="table-action" @click="artifact = null">
                <Icon name="x" size="sm" />
                <span>{{ copy.close }}</span>
              </button>
            </div>
            <iframe
              :title="copy.artifact"
              :src="artifact"
              class="h-[420px] w-full bg-white"
              sandbox="allow-scripts"
            />
          </div>

          <div v-if="compareResults.length" class="space-y-3">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <h3 class="text-sm font-medium text-gray-900 dark:text-white">{{ copy.compareResult }}</h3>
            </div>
            <div class="grid gap-3 md:grid-cols-2">
              <div v-for="item in compareResults" :key="item.id" class="surface-subcard overflow-hidden">
                <div class="flex items-center justify-between border-b border-[var(--surface-edge)] px-3 py-2 text-xs">
                  <span class="font-medium text-gray-900 dark:text-white">
                    {{ item.question_id }}
                    <span class="ml-1 font-normal surface-muted">· {{ item.bank_version }}</span>
                  </span>
                  <span :class="['badge', statusClass(item.status)]">{{ statusLabel(item.status) }}</span>
                </div>
                <iframe
                  v-if="compareUrls[item.id]"
                  :title="`${copy.artifact} ${item.question_id}`"
                  :src="compareUrls[item.id]"
                  class="h-[300px] w-full bg-white"
                  sandbox="allow-scripts"
                />
                <pre v-else class="max-h-[300px] overflow-auto whitespace-pre-wrap p-3 font-mono text-xs">{{ item.response.slice(0, 2000) }}</pre>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ActionMenu from '@/components/common/ActionMenu.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Pagination from '@/components/common/Pagination.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { supportReadonly as vSupportReadonly } from '@/directives/supportReadonly'
import Select from '@/components/common/Select.vue'
import type { GroupPlatform } from '@/types'
import {
  compareQualityResults,
  createQualityPlan,
  deleteQualityPlan,
  getQualityArtifactUrl,
  getQualityRun,
  listQualityGroupOptions,
  listQualityPlans,
  listQualityRuns,
  runQualityPlan,
  updateQualityPlan,
  type QualityPlan,
  type QualityPlanPayload,
  type QualityQuestion,
  type QualityResult,
  type QualityRun,
  type QualitySettings,
} from '@/api/admin/qualityInspection'

const { t, tm } = useI18n()
const copy = computed(() => tm('admin.qualityInspection') as Record<string, string>)

const builtinQuestions = ref<QualityQuestion[]>([])
const planPage = ref(1)
const runPage = ref(1)
const resultPage = ref(1)
const historyLoading = ref(false)
const resultsLoading = ref(false)
const busy = ref(false)
const confirmation = ref<{ kind: 'run' | 'delete'; plan: QualityPlan } | null>(null)
const confirmationMessage = computed(() => {
  const action = confirmation.value
  if (!action) return ''
  return action.kind === 'delete'
    ? t('admin.qualityInspection.deleteConfirm', { name: action.plan.name })
    : t('admin.qualityInspection.runConfirm', {
        group: action.plan.group_name,
        model: action.plan.model,
        count: action.plan.estimated_requests,
      })
})
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const plans = ref<QualityPlan[]>([])
const settings = ref<QualitySettings | null>(null)
const groups = ref<{ id: number; name: string; platform: string }[]>([])
const draft = ref<(QualityPlanPayload & { id?: number }) | null>(null)
const activePlan = ref<QualityPlan | null>(null)
const runs = ref<QualityRun[]>([])
const activeRun = ref<QualityRun | null>(null)
const results = ref<QualityResult[]>([])
const artifact = ref<string | null>(null)
const compareIds = ref<number[]>([])
const compareResults = ref<QualityResult[]>([])
const compareUrls = ref<Record<number, string>>({})

const groupOptions = computed(() =>
  groups.value.map((group) => ({ value: group.id, label: `${group.name} · ${group.platform}` })),
)
const effortOptions = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'].map((value) => ({ value, label: value }))
const endpointOptions = computed(() =>
  ['any', 'messages', 'responses', 'chat_completions', 'gemini'].map((value) => ({
    value,
    label: value === 'any' ? copy.value.anyEndpoint : value,
  })),
)
const pagedPlans = computed(() => plans.value.slice((planPage.value - 1) * 10, planPage.value * 10))
const pagedRuns = computed(() => runs.value.slice((runPage.value - 1) * 10, runPage.value * 10))
const pagedResults = computed(() => results.value.slice((resultPage.value - 1) * 20, resultPage.value * 20))
const enabledQuestionCount = computed(() => draft.value?.questions.filter((q) => q.enabled).length ?? 0)
const estimated = computed(() => {
  const plan = draft.value
  if (!plan) return 0
  const accounts = Math.max(1, Number(plan.max_accounts) || 1)
  const rounds = plan.questions.filter((q) => q.enabled).reduce((sum, q) => sum + Math.max(1, Number(q.rounds) || 1), 0)
  return accounts * rounds
})
const exceedsMaxRequests = computed(() => {
  const limit = settings.value?.max_requests_per_round ?? 0
  return limit > 0 && estimated.value > limit
})

function showExpectedField(question: QualityQuestion): boolean {
  return question.expected !== '' || ['number', 'choice', 'json'].includes(question.kind)
}

function kindLabel(kind: string): string {
  return (tm('admin.qualityInspection.kinds') as Record<string, string>)?.[kind] ?? kind
}

function kindBadgeClass(kind: string): string {
  if (kind === 'number' || kind === 'choice') return 'badge-primary'
  if (kind === 'json') return 'badge-purple'
  if (kind === 'drawing') return 'badge-warning'
  return 'badge-gray'
}

async function reload() {
  loading.value = true
  error.value = ''
  try {
    const [data, groupRows] = await Promise.all([listQualityPlans(), listQualityGroupOptions()])
    plans.value = data.plans ?? []
    builtinQuestions.value = data.questions ?? []
    planPage.value = Math.min(planPage.value, Math.max(1, Math.ceil(plans.value.length / 10)))
    settings.value = data.settings
    groups.value = groupRows
    if (activePlan.value) {
      const current = plans.value.find((plan) => plan.id === activePlan.value?.id)
      if (current) await loadRuns(current)
      else closeHistory()
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : copy.value.loadFailed
  } finally {
    loading.value = false
  }
}

function startCreate() {
  error.value = ''
  draft.value = {
    group_id: groups.value[0]?.id ?? 0,
    name: '',
    model: '',
    endpoint: 'any',
    effort: 'medium',
    enabled: true,
    schedule_enabled: false,
    interval_minutes: 60,
    pause_start: '22:00',
    pause_end: '09:00',
    timezone: 'Asia/Shanghai',
    max_accounts: 1,
    max_saved_rounds: 20,
    quarantine_enabled: false,
    quarantine_cooldown_minutes: 60,
    quarantine_fail_rounds: 2,
    revision: 0,
    questions: builtinQuestions.value.map((question) => ({
      ...question,
      choices: [...(question.choices ?? [])],
      enabled: true,
    })),
  }
}

function startEdit(plan: QualityPlan) {
  error.value = ''
  draft.value = {
    ...plan,
    questions: plan.questions.map((question) => ({ ...question, choices: [...(question.choices ?? [])] })),
  }
}

async function savePlan() {
  const plan = draft.value
  if (!plan) return
  saving.value = true
  error.value = ''
  try {
    const payload = {
      group_id: Number(plan.group_id),
      name: plan.name ?? '',
      model: plan.model ?? '',
      endpoint: plan.endpoint ?? 'any',
      effort: plan.effort ?? 'medium',
      enabled: Boolean(plan.enabled),
      schedule_enabled: Boolean(plan.schedule_enabled),
      interval_minutes: Number(plan.interval_minutes) || 60,
      pause_start: plan.pause_start ?? '22:00',
      pause_end: plan.pause_end ?? '09:00',
      timezone: plan.timezone ?? 'Asia/Shanghai',
      max_accounts: Number(plan.max_accounts) || 1,
      max_saved_rounds: Number(plan.max_saved_rounds) || 20,
      quarantine_enabled: Boolean(plan.quarantine_enabled),
      quarantine_cooldown_minutes: Number(plan.quarantine_cooldown_minutes) || 60,
      quarantine_fail_rounds: Number(plan.quarantine_fail_rounds) || 2,
      revision: Number(plan.revision) || 0,
      questions: plan.questions,
    }
    if (plan.id) {
      await updateQualityPlan(plan.id, payload)
    } else {
      await createQualityPlan(payload)
    }
    draft.value = null
    await reload()
  } catch (err) {
    error.value = err instanceof Error ? err.message : copy.value.saveFailed
  } finally {
    saving.value = false
  }
}

function closeDraft() {
  if (!saving.value) draft.value = null
}

function onPlanAction(plan: QualityPlan, key: string) {
  if (key === 'run' || key === 'delete') confirmation.value = { kind: key, plan }
}

async function confirmPlanAction() {
  const action = confirmation.value
  if (!action || busy.value) return
  busy.value = true
  error.value = ''
  try {
    if (action.kind === 'delete') {
      await deleteQualityPlan(action.plan.id)
      if (activePlan.value?.id === action.plan.id) closeHistory()
      await reload()
    } else {
      await runQualityPlan(action.plan.id, true)
      await loadRuns(action.plan)
    }
    confirmation.value = null
  } catch (err) {
    error.value =
      err instanceof Error ? err.message : action.kind === 'delete' ? copy.value.deleteFailed : copy.value.runFailed
    confirmation.value = null
  } finally {
    busy.value = false
  }
}

let historyRequest = 0
let resultRequest = 0
let artifactRequest = 0
let compareRequest = 0
function closeHistory() {
  historyRequest++
  resultRequest++
  artifactRequest++
  compareRequest++
  activePlan.value = null
  activeRun.value = null
  runs.value = []
  results.value = []
  artifact.value = null
  compareIds.value = []
  compareResults.value = []
  compareUrls.value = {}
}

async function loadRuns(plan: QualityPlan) {
  const request = ++historyRequest
  resultRequest++
  artifactRequest++
  compareRequest++
  historyLoading.value = true
  runPage.value = 1
  runs.value = []
  activePlan.value = plan
  activeRun.value = null
  results.value = []
  compareIds.value = []
  compareResults.value = []
  artifact.value = null
  try {
    const data = await listQualityRuns(plan.id)
    if (request !== historyRequest) return
    runs.value = data
    if (runs.value.length) await openRun(runs.value[0])
  } catch (err) {
    if (request === historyRequest) error.value = err instanceof Error ? err.message : copy.value.historyFailed
  } finally {
    if (request === historyRequest) historyLoading.value = false
  }
}

async function openRun(run: QualityRun) {
  const request = ++resultRequest
  artifactRequest++
  artifact.value = null
  results.value = []
  resultPage.value = 1
  resultsLoading.value = true
  activeRun.value = run
  try {
    const data = await getQualityRun(run.id)
    if (request === resultRequest) results.value = data.results ?? []
  } catch (err) {
    if (request === resultRequest) error.value = err instanceof Error ? err.message : copy.value.roundFailed
  } finally {
    if (request === resultRequest) resultsLoading.value = false
  }
}

async function preview(result: QualityResult) {
  const request = ++artifactRequest
  try {
    const data = await getQualityArtifactUrl(result.id)
    if (request === artifactRequest) artifact.value = data.url
  } catch (err) {
    if (request === artifactRequest) error.value = err instanceof Error ? err.message : copy.value.artifactFailed
  }
}

function toggleCompare(result: QualityResult) {
  const index = compareIds.value.indexOf(result.id)
  if (index >= 0) {
    compareIds.value.splice(index, 1)
  } else {
    if (compareIds.value.length >= 4) return
    compareIds.value.push(result.id)
  }
}

async function runCompare() {
  if (compareIds.value.length < 2) return
  const request = ++compareRequest
  try {
    const compared = await compareQualityResults([...compareIds.value])
    if (request !== compareRequest) return
    compareResults.value = compared
    const urls: Record<number, string> = {}
    await Promise.all(
      compareResults.value
        .filter((item) => item.has_artifact)
        .map(async (item) => {
          try {
            urls[item.id] = (await getQualityArtifactUrl(item.id)).url
          } catch {
            /* 作品不可用时回退到正文 */
          }
        }),
    )
    if (request === compareRequest) compareUrls.value = urls
  } catch (err) {
    if (request === compareRequest) error.value = err instanceof Error ? err.message : copy.value.compareFailed
  }
}

function statusLabel(status: string): string {
  return (tm('admin.qualityInspection.statuses') as Record<string, string>)[status] ?? status
}

function statusClass(status: string): string {
  if (status === 'pass' || status === 'completed') return 'badge-success'
  if (status === 'fail') return 'badge-danger'
  if (status === 'quota' || status === 'error') return 'badge-warning'
  return 'badge-gray'
}

function formatTime(value: string | null): string {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function triggerLabel(trigger: string) {
  return ['manual', 'timer', 'recheck'].includes(trigger) ? copy.value[trigger] : trigger
}

watch(
  () => [...compareIds.value],
  () => {
    compareRequest++
    compareResults.value = []
    compareUrls.value = {}
  },
  { flush: 'sync' },
)
onMounted(reload)
</script>
