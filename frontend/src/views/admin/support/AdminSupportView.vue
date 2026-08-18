<template>
  <AppLayout>
    <div class="mx-auto max-w-[1500px] space-y-5">
      <AdminSupportBanner v-if="target" :target="target" />

      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ pageTitle }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.pageDescription') }}</p>
        </div>
        <button type="button" class="btn btn-secondary self-start" :disabled="loading" @click="loadPage">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          <span>{{ t('common.refresh') }}</span>
        </button>
      </div>

      <div v-if="loading" class="card flex min-h-64 items-center justify-center">
        <LoadingSpinner />
      </div>

      <div v-else-if="errorMessage" class="card border-red-200 p-6 text-center dark:border-red-900/60">
        <p class="font-medium text-red-700 dark:text-red-300">{{ errorMessage }}</p>
        <button type="button" class="btn btn-secondary mt-4" @click="loadPage">{{ t('admin.support.retry') }}</button>
      </div>

      <template v-else-if="target">
        <section v-if="resource === 'overview'" class="space-y-5">
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <SummaryCard icon="user" :label="t('admin.support.account')" :value="targetLabel" :hint="`#${target.id}`" />
            <SummaryCard icon="database" :label="t('common.balance')" :value="formatMoney(target.balance)" :hint="t('admin.support.frozenBalance', { amount: formatMoney(target.frozen_balance) })" />
            <SummaryCard icon="chart" :label="t('admin.support.concurrency')" :value="String(target.concurrency)" :hint="t('admin.support.rpmLimit', { value: target.rpm_limit || t('admin.support.unlimited') })" />
            <SummaryCard icon="shield" :label="t('common.status')" :value="statusLabel(target.status)" :hint="roleLabel(target.role)" />
          </div>

          <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
            <RouterLink :to="supportLink('api-keys')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="key" size="lg" class="text-primary-600 dark:text-primary-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.apiKeysTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.apiKeysSummary') }}</p>
            </RouterLink>
            <RouterLink :to="supportLink('async-images')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="sparkles" size="lg" class="text-violet-600 dark:text-violet-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.asyncImagesTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.asyncImagesSummary') }}</p>
            </RouterLink>
            <RouterLink :to="supportLink('usage')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="chart" size="lg" class="text-emerald-600 dark:text-emerald-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.usageTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.usageSummary') }}</p>
            </RouterLink>
            <RouterLink :to="supportLink('channels')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="database" size="lg" class="text-sky-600 dark:text-sky-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.channelsTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.channelsSummary') }}</p>
            </RouterLink>
            <RouterLink :to="supportLink('channel-status')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="chart" size="lg" class="text-cyan-600 dark:text-cyan-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.channelStatusTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.channelStatusSummary') }}</p>
            </RouterLink>
            <RouterLink :to="supportLink('subscriptions')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="shield" size="lg" class="text-amber-600 dark:text-amber-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.subscriptionsTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.subscriptionsSummary') }}</p>
            </RouterLink>
            <RouterLink :to="supportLink('orders')" class="card group p-5 transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:hover:border-primary-700">
              <Icon name="database" size="lg" class="text-indigo-600 dark:text-indigo-400" />
              <h2 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ t('admin.support.ordersTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.ordersSummary') }}</p>
            </RouterLink>
          </div>
        </section>

        <section v-else-if="resource === 'api-keys'" class="card overflow-hidden">
          <div class="border-b border-gray-200 px-5 py-4 dark:border-dark-700">
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.apiKeyMetadata') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.apiKeyConfidentiality') }}</p>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
              <thead class="bg-gray-50/80 dark:bg-dark-800/70">
                <tr>
                  <th class="table-heading">{{ t('common.name') }}</th>
                  <th class="table-heading">{{ t('common.status') }}</th>
                  <th class="table-heading">{{ t('admin.support.group') }}</th>
                  <th class="table-heading">{{ t('admin.support.quota') }}</th>
                  <th class="table-heading">{{ t('admin.support.concurrency') }}</th>
                  <th class="table-heading">{{ t('admin.support.ipRestrictions') }}</th>
                  <th class="table-heading">{{ t('admin.support.lastUsed') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/40">
                <tr v-for="key in apiKeys" :key="key.id" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                  <td class="table-cell">
                    <div class="font-medium text-gray-900 dark:text-white">{{ key.name || t('admin.support.unnamedKey') }}</div>
                    <div class="text-xs text-gray-400">#{{ key.id }}</div>
                  </td>
                  <td class="table-cell"><span :class="statusBadgeClass(key.status)">{{ statusLabel(key.status) }}</span></td>
                  <td class="table-cell">{{ key.group?.name || (key.group_id ? `#${key.group_id}` : t('common.none')) }}</td>
                  <td class="table-cell tabular-nums">{{ key.quota > 0 ? `${formatMoney(key.quota_used)} / ${formatMoney(key.quota)}` : t('admin.support.unlimited') }}</td>
                  <td class="table-cell tabular-nums">{{ key.current_concurrency }}</td>
                  <td class="table-cell">
                    <span v-if="key.has_ip_whitelist || key.has_ip_blacklist">
                      {{ t('admin.support.ipRestrictionCounts', { allow: key.ip_whitelist_size, deny: key.ip_blacklist_size }) }}
                    </span>
                    <span v-else class="text-gray-400">{{ t('common.none') }}</span>
                  </td>
                  <td class="table-cell whitespace-nowrap">{{ formatDate(key.last_used_at) }}</td>
                </tr>
                <tr v-if="apiKeys.length === 0">
                  <td colspan="7" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.noAPIKeys') }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="apiKeyPages > 1" class="flex items-center justify-between border-t border-gray-200 px-5 py-3 dark:border-dark-700">
            <button class="btn btn-secondary" :disabled="apiKeyPage <= 1" @click="changeAPIKeyPage(apiKeyPage - 1)">{{ t('common.back') }}</button>
            <span class="text-sm text-gray-500 dark:text-gray-400">{{ apiKeyPage }} / {{ apiKeyPages }}</span>
            <button class="btn btn-secondary" :disabled="apiKeyPage >= apiKeyPages" @click="changeAPIKeyPage(apiKeyPage + 1)">{{ t('common.next') }}</button>
          </div>
        </section>

        <section v-else-if="resource === 'async-images'" class="space-y-4">
          <div class="card p-4">
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.asyncImageHistory') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.asyncImageReadOnlyHint') }}</p>
              </div>
              <Select v-model="imageStatus" class="w-full sm:w-44" :options="imageStatusOptions" @change="resetAndLoadImages" />
            </div>
          </div>

          <div class="card overflow-hidden">
            <div class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead class="bg-gray-50/80 dark:bg-dark-800/70">
                  <tr>
                    <th class="table-heading">{{ t('admin.support.task') }}</th>
                    <th class="table-heading">{{ t('admin.support.model') }}</th>
                    <th class="table-heading">{{ t('common.status') }}</th>
                    <th class="table-heading">{{ t('admin.support.apiKeyId') }}</th>
                    <th class="table-heading">{{ t('admin.support.createdAt') }}</th>
                    <th class="table-heading">{{ t('common.view') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/40">
                  <tr v-for="task in imageTasks" :key="task.task_id" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                    <td class="table-cell max-w-sm">
                      <div class="truncate font-medium text-gray-900 dark:text-white">{{ task.prompt_preview || task.task_id }}</div>
                      <div class="truncate font-mono text-xs text-gray-400">{{ task.task_id }}</div>
                    </td>
                    <td class="table-cell">{{ task.model || t('common.notAvailable') }}</td>
                    <td class="table-cell"><span :class="statusBadgeClass(task.status)">{{ statusLabel(task.status) }}</span></td>
                    <td class="table-cell font-mono text-xs">#{{ task.api_key_id }}</td>
                    <td class="table-cell whitespace-nowrap">{{ formatUnixDate(task.created_at) }}</td>
                    <td class="table-cell"><button type="button" class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="openTask(task)">{{ t('common.view') }}</button></td>
                  </tr>
                  <tr v-if="imageTasks.length === 0"><td colspan="6" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.noAsyncImages') }}</td></tr>
                </tbody>
              </table>
            </div>
            <div v-if="imageOffset > 0 || imageHasMore" class="flex items-center justify-between border-t border-gray-200 px-5 py-3 dark:border-dark-700">
              <button class="btn btn-secondary" :disabled="imageOffset === 0" @click="changeImagePage(-1)">{{ t('common.back') }}</button>
              <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.resultRange', { start: imageOffset + 1, end: imageOffset + imageTasks.length }) }}</span>
              <button class="btn btn-secondary" :disabled="!imageHasMore" @click="changeImagePage(1)">{{ t('common.next') }}</button>
            </div>
          </div>

          <div v-if="selectedTask" class="card p-5">
            <div class="flex items-start justify-between gap-4">
              <div>
                <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.taskDetail') }}</h3>
                <p class="mt-1 break-all font-mono text-xs text-gray-400">{{ selectedTask.task_id }}</p>
              </div>
              <button type="button" class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200" :aria-label="t('common.close')" @click="selectedTask = null"><Icon name="x" size="sm" /></button>
            </div>
            <dl class="mt-4 grid grid-cols-1 gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <div><dt class="text-gray-500">{{ t('admin.support.model') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ selectedTask.model || t('common.notAvailable') }}</dd></div>
              <div><dt class="text-gray-500">{{ t('common.status') }}</dt><dd class="mt-1"><span :class="statusBadgeClass(selectedTask.status)">{{ statusLabel(selectedTask.status) }}</span></dd></div>
              <div><dt class="text-gray-500">{{ t('admin.support.apiKeyId') }}</dt><dd class="mt-1 text-gray-900 dark:text-white">#{{ selectedTask.api_key_id }}</dd></div>
              <div><dt class="text-gray-500">HTTP</dt><dd class="mt-1 text-gray-900 dark:text-white">{{ selectedTask.http_status || t('common.notAvailable') }}</dd></div>
            </dl>
            <p v-if="selectedTask.prompt_preview" class="mt-4 whitespace-pre-wrap rounded-lg bg-gray-50 p-3 text-sm text-gray-700 dark:bg-dark-800 dark:text-gray-200">{{ selectedTask.prompt_preview }}</p>
            <div v-if="selectedTaskImages.length" class="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <img v-for="url in selectedTaskImages" :key="url" :src="url" :alt="t('admin.support.generatedImage')" class="max-h-96 w-full rounded-lg border border-gray-200 object-contain dark:border-dark-700" />
            </div>
            <pre v-if="selectedTask.status === 'failed' && selectedTask.error" class="mt-4 max-h-56 overflow-auto whitespace-pre-wrap rounded-lg bg-red-50 p-3 text-xs text-red-800 dark:bg-red-950/30 dark:text-red-200">{{ formatJSON(selectedTask.error) }}</pre>
          </div>
        </section>

        <section v-else-if="resource === 'usage' && usage" class="space-y-4">
          <div class="card flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.usageTitle') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.usageReadOnlyHint') }}</p></div>
            <Select v-model="usagePeriod" class="w-full sm:w-44" :options="usagePeriodOptions" @change="reloadUsage" />
          </div>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <SummaryCard icon="chart" :label="t('admin.support.totalRequests')" :value="formatNumber(usage.total_requests)" />
            <SummaryCard icon="cube" :label="t('admin.support.totalTokens')" :value="formatNumber(usage.total_tokens)" />
            <SummaryCard icon="database" :label="t('admin.support.totalCost')" :value="formatMoney(usage.total_actual_cost ?? usage.total_cost)" />
            <SummaryCard icon="clock" :label="t('admin.support.averageDuration')" :value="usage.avg_duration_ms != null ? `${formatNumber(usage.avg_duration_ms)} ms` : t('common.notAvailable')" />
          </div>
        </section>

        <section v-else-if="resource === 'channels'" class="space-y-4">
          <div class="card p-4">
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.channelsTitle') }}</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.channelsReadOnlyHint') }}</p>
              </div>
              <div class="relative w-full sm:w-80">
                <Icon name="search" size="sm" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                <input v-model="channelSearch" type="search" class="input pl-9" :placeholder="t('admin.support.searchChannels')" />
              </div>
            </div>
          </div>
          <div class="card overflow-hidden">
            <AvailableChannelsTable
              :columns="channelColumnLabels"
              :rows="filteredChannels"
              :loading="false"
              :user-group-rates="channelGroupRates"
              pricing-key-prefix="availableChannels.pricing"
              :no-pricing-label="t('availableChannels.noPricing')"
              :no-models-label="t('availableChannels.noModels')"
              :empty-label="t('admin.support.noChannels')"
            />
          </div>
        </section>

        <section v-else-if="resource === 'channel-status'" class="space-y-4">
          <div class="card p-4">
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.channelStatusTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.channelStatusReadOnlyHint') }}</p>
          </div>
          <div class="card overflow-hidden">
            <div class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead class="bg-gray-50/80 dark:bg-dark-800/70">
                  <tr>
                    <th class="table-heading">{{ t('common.name') }}</th>
                    <th class="table-heading">{{ t('admin.support.provider') }}</th>
                    <th class="table-heading">{{ t('admin.support.group') }}</th>
                    <th class="table-heading">{{ t('admin.support.model') }}</th>
                    <th class="table-heading">{{ t('common.status') }}</th>
                    <th class="table-heading">{{ t('admin.support.availability7d') }}</th>
                    <th class="table-heading">{{ t('common.view') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/40">
                  <tr v-for="monitor in channelStatus" :key="monitor.id" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                    <td class="table-cell font-medium text-gray-900 dark:text-white">{{ monitor.name }}</td>
                    <td class="table-cell">{{ monitor.provider }}</td>
                    <td class="table-cell">{{ monitor.group_name || t('common.none') }}</td>
                    <td class="table-cell">{{ monitor.primary_model || t('common.notAvailable') }}</td>
                    <td class="table-cell"><span :class="statusBadgeClass(monitor.primary_status)">{{ monitor.primary_status }}</span></td>
                    <td class="table-cell tabular-nums">{{ formatPercent(monitor.availability_7d) }}</td>
                    <td class="table-cell"><button type="button" class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400" @click="openChannelStatus(monitor.id)">{{ t('common.view') }}</button></td>
                  </tr>
                  <tr v-if="channelStatus.length === 0"><td colspan="7" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.noChannelStatus') }}</td></tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-if="selectedChannelStatus" class="card p-5">
            <div class="flex items-start justify-between gap-4">
              <div>
                <h3 class="font-semibold text-gray-900 dark:text-white">{{ selectedChannelStatus.name }}</h3>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ selectedChannelStatus.provider }} · {{ selectedChannelStatus.group_name }}</p>
              </div>
              <button type="button" class="rounded-lg p-2 text-gray-400 hover:bg-gray-100 dark:hover:bg-dark-700" :aria-label="t('common.close')" @click="selectedChannelStatus = null"><Icon name="x" size="sm" /></button>
            </div>
            <div class="mt-4 overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead><tr><th class="table-heading">{{ t('admin.support.model') }}</th><th class="table-heading">{{ t('common.status') }}</th><th class="table-heading">7d</th><th class="table-heading">15d</th><th class="table-heading">30d</th><th class="table-heading">{{ t('admin.support.averageLatency') }}</th></tr></thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                  <tr v-for="model in selectedChannelStatus.models" :key="model.model">
                    <td class="table-cell font-medium text-gray-900 dark:text-white">{{ model.model }}</td>
                    <td class="table-cell">{{ model.latest_status }}</td>
                    <td class="table-cell">{{ formatPercent(model.availability_7d) }}</td>
                    <td class="table-cell">{{ formatPercent(model.availability_15d) }}</td>
                    <td class="table-cell">{{ formatPercent(model.availability_30d) }}</td>
                    <td class="table-cell">{{ model.avg_latency_7d_ms == null ? t('common.notAvailable') : `${model.avg_latency_7d_ms} ms` }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <section v-else-if="resource === 'subscriptions'" class="space-y-4">
          <div class="card p-4">
            <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.subscriptionsTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.subscriptionsReadOnlyHint') }}</p>
          </div>
          <div class="card overflow-hidden">
            <div class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead class="bg-gray-50/80 dark:bg-dark-800/70"><tr><th class="table-heading">{{ t('admin.support.group') }}</th><th class="table-heading">{{ t('common.status') }}</th><th class="table-heading">5h</th><th class="table-heading">{{ t('admin.support.daily') }}</th><th class="table-heading">{{ t('admin.support.weekly') }}</th><th class="table-heading">{{ t('admin.support.monthly') }}</th><th class="table-heading">{{ t('admin.support.expiresAt') }}</th></tr></thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/40">
                  <tr v-for="subscription in subscriptions" :key="subscription.id" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                    <td class="table-cell"><div class="font-medium text-gray-900 dark:text-white">{{ subscription.group?.name || `#${subscription.group_id}` }}</div><div class="text-xs text-gray-400">#{{ subscription.id }}</div></td>
                    <td class="table-cell"><span :class="statusBadgeClass(subscription.status)">{{ statusLabel(subscription.status) }}</span></td>
                    <td class="table-cell">{{ formatUsageWindow(subscription.five_hour_usage_usd, subscription.group?.five_hour_limit_usd) }}</td>
                    <td class="table-cell">{{ formatUsageWindow(subscription.daily_usage_usd, subscription.group?.daily_limit_usd) }}</td>
                    <td class="table-cell">{{ formatUsageWindow(subscription.weekly_usage_usd, subscription.group?.weekly_limit_usd) }}</td>
                    <td class="table-cell">{{ formatUsageWindow(subscription.monthly_usage_usd, subscription.group?.monthly_limit_usd) }}</td>
                    <td class="table-cell whitespace-nowrap">{{ formatDate(subscription.expires_at) }}</td>
                  </tr>
                  <tr v-if="subscriptions.length === 0"><td colspan="7" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.noSubscriptions') }}</td></tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <section v-else-if="resource === 'orders'" class="space-y-4">
          <div class="card p-4">
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.ordersTitle') }}</h2><p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.ordersReadOnlyHint') }}</p></div>
              <Select v-model="orderStatus" class="w-full sm:w-48" :options="orderStatusOptions" @change="resetAndLoadOrders" />
            </div>
          </div>
          <div class="card overflow-hidden">
            <div class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
                <thead class="bg-gray-50/80 dark:bg-dark-800/70"><tr><th class="table-heading">{{ t('payment.orders.orderId') }}</th><th class="table-heading">{{ t('payment.orders.orderNo') }}</th><th class="table-heading">{{ t('payment.orders.payAmount') }}</th><th class="table-heading">{{ t('payment.orders.paymentMethod') }}</th><th class="table-heading">{{ t('payment.orders.status') }}</th><th class="table-heading">{{ t('payment.orders.createdAt') }}</th></tr></thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900/40">
                  <tr v-for="order in orders" :key="order.id" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/50">
                    <td class="table-cell font-mono">#{{ order.id }}</td>
                    <td class="table-cell">{{ order.out_trade_no }}</td>
                    <td class="table-cell tabular-nums">{{ order.currency || 'USD' }} {{ Number(order.pay_amount).toFixed(2) }}</td>
                    <td class="table-cell">{{ order.payment_type }}</td>
                    <td class="table-cell"><OrderStatusBadge :status="order.status" /></td>
                    <td class="table-cell whitespace-nowrap">{{ formatDate(order.created_at) }}</td>
                  </tr>
                  <tr v-if="orders.length === 0"><td colspan="6" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.noOrders') }}</td></tr>
                </tbody>
              </table>
            </div>
            <div v-if="orderPages > 1" class="flex items-center justify-between border-t border-gray-200 px-5 py-3 dark:border-dark-700">
              <button class="btn btn-secondary" :disabled="orderPage <= 1" @click="changeOrderPage(orderPage - 1)">{{ t('common.back') }}</button>
              <span class="text-sm text-gray-500 dark:text-gray-400">{{ orderPage }} / {{ orderPages }}</span>
              <button class="btn btn-secondary" :disabled="orderPage >= orderPages" @click="changeOrderPage(orderPage + 1)">{{ t('common.next') }}</button>
            </div>
          </div>
        </section>

        <section v-else-if="resource === 'profile'" class="card p-5">
          <h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.support.profileTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.support.profileReadOnlyHint') }}</p>
          <dl class="mt-6 grid grid-cols-1 gap-x-8 gap-y-5 sm:grid-cols-2 xl:grid-cols-3">
            <ProfileField :label="t('common.email')" :value="target.email" />
            <ProfileField :label="t('admin.support.username')" :value="target.username || t('common.notAvailable')" />
            <ProfileField :label="t('admin.support.userId')" :value="`#${target.id}`" />
            <ProfileField :label="t('admin.support.role')" :value="roleLabel(target.role)" />
            <ProfileField :label="t('common.status')" :value="statusLabel(target.status)" />
            <ProfileField :label="t('admin.support.createdAt')" :value="formatDate(target.created_at)" />
            <ProfileField :label="t('admin.support.lastActive')" :value="formatDate(target.last_active_at)" />
            <ProfileField :label="t('admin.support.concurrency')" :value="String(target.concurrency)" />
            <ProfileField :label="t('admin.support.rpmLimitLabel')" :value="target.rpm_limit ? String(target.rpm_limit) : t('admin.support.unlimited')" />
          </dl>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminSupportBanner from '@/components/admin/support/AdminSupportBanner.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import AvailableChannelsTable from '@/components/channels/AvailableChannelsTable.vue'
import OrderStatusBadge from '@/components/payment/OrderStatusBadge.vue'
import { useAdminSupportViewStore } from '@/stores'
import * as supportAPI from '@/api/admin/supportView'
import type { AdminSupportAPIKey, AdminSupportImageTask, AdminSupportUsage } from '@/api/admin/supportView'
import type { UserAvailableChannel } from '@/api/channels'
import type { UserMonitorDetail, UserMonitorView } from '@/api/channelMonitor'
import type { UserSubscription } from '@/types'
import type { PaymentOrder } from '@/types/payment'
import { adminSupportPath, parseAdminSupportTargetId, selfPathForSupportResource, type AdminSupportResource } from '@/utils/adminSupport'
import { sanitizeUrl } from '@/utils/url'

const SummaryCard = defineComponent({
  props: { icon: { type: String as PropType<'user' | 'database' | 'chart' | 'shield' | 'key' | 'sparkles' | 'cube' | 'clock'>, required: true }, label: { type: String, required: true }, value: { type: String, required: true }, hint: { type: String, default: '' } },
  setup(props) {
    return () => h('div', { class: 'card p-4' }, [
      h('div', { class: 'flex items-start gap-3' }, [
        h(Icon, { name: props.icon, size: 'md', class: 'mt-0.5 text-primary-600 dark:text-primary-400' }),
        h('div', { class: 'min-w-0' }, [
          h('p', { class: 'text-xs font-medium text-gray-500 dark:text-gray-400' }, props.label),
          h('p', { class: 'mt-1 truncate text-lg font-bold text-gray-900 dark:text-white', title: props.value }, props.value),
          props.hint ? h('p', { class: 'mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400', title: props.hint }, props.hint) : null
        ])
      ])
    ])
  }
})

const ProfileField = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, required: true } },
  setup(props) {
    return () => h('div', [h('dt', { class: 'text-sm text-gray-500 dark:text-gray-400' }, props.label), h('dd', { class: 'mt-1 break-words font-medium text-gray-900 dark:text-white' }, props.value)])
  }
})

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const supportStore = useAdminSupportViewStore()
const loading = ref(false)
const errorMessage = ref('')
const apiKeys = ref<AdminSupportAPIKey[]>([])
const apiKeyPage = ref(1)
const apiKeyPages = ref(1)
const imageTasks = ref<AdminSupportImageTask[]>([])
const imageStatus = ref('')
const imageOffset = ref(0)
const imageHasMore = ref(false)
const imageLimit = 20
const selectedTask = ref<AdminSupportImageTask | null>(null)
const usage = ref<AdminSupportUsage | null>(null)
const usagePeriod = ref('month')
const channels = ref<UserAvailableChannel[]>([])
const channelGroupRates = ref<Record<number, number>>({})
const channelSearch = ref('')
const channelStatus = ref<UserMonitorView[]>([])
const selectedChannelStatus = ref<UserMonitorDetail | null>(null)
const subscriptions = ref<UserSubscription[]>([])
const orders = ref<PaymentOrder[]>([])
const orderStatus = ref('')
const orderPage = ref(1)
const orderPages = ref(1)
let loadSequence = 0

interface SupportReadRequest {
  sequence: number
  userId: number
  resource: AdminSupportResource
}

const resource = computed(() => route.meta.adminSupportResource as AdminSupportResource)
const targetUserId = computed(() => parseAdminSupportTargetId(route.params.user_id))
const target = computed(() => supportStore.target?.id === targetUserId.value ? supportStore.target : null)
const targetLabel = computed(() => target.value?.username?.trim() || target.value?.email || '')
const pageTitle = computed(() => {
  const keys: Record<AdminSupportResource, string> = {
    overview: 'overview',
    'api-keys': 'apiKeysTitle',
    'async-images': 'asyncImagesTitle',
    usage: 'usageTitle',
    channels: 'channelsTitle',
    'channel-status': 'channelStatusTitle',
    subscriptions: 'subscriptionsTitle',
    orders: 'ordersTitle',
    profile: 'profileTitle'
  }
  return t(`admin.support.${keys[resource.value]}`)
})

const imageStatusOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'processing', label: t('common.processing') },
  { value: 'completed', label: t('admin.support.completed') },
  { value: 'failed', label: t('admin.support.failed') }
])
const usagePeriodOptions = computed(() => [
  { value: 'today', label: t('common.today') },
  { value: 'week', label: t('admin.support.thisWeek') },
  { value: 'month', label: t('admin.support.thisMonth') }
])
const selectedTaskImages = computed(() => imageURLs(selectedTask.value))
const channelColumnLabels = computed(() => ({
  name: t('availableChannels.columns.name'),
  description: t('availableChannels.columns.description'),
  platform: t('availableChannels.columns.platform'),
  groups: t('availableChannels.columns.groups'),
  supportedModels: t('availableChannels.columns.supportedModels')
}))
const filteredChannels = computed(() => {
  const query = channelSearch.value.trim().toLowerCase()
  if (!query) return channels.value
  return channels.value
    .map((channel) => {
      if (channel.name.toLowerCase().includes(query) || channel.description?.toLowerCase().includes(query)) return channel
      const platforms = channel.platforms.filter((section) =>
        section.platform.toLowerCase().includes(query) ||
        section.groups.some((group) => group.name.toLowerCase().includes(query)) ||
        section.supported_models.some((model) => model.name.toLowerCase().includes(query))
      )
      return platforms.length > 0 ? { ...channel, platforms } : null
    })
    .filter((channel): channel is UserAvailableChannel => channel !== null)
})
const orderStatusOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'PENDING', label: t('payment.status.pending') },
  { value: 'COMPLETED', label: t('payment.status.completed') },
  { value: 'FAILED', label: t('payment.status.failed') },
  { value: 'REFUNDED', label: t('payment.status.refunded') }
])

function supportLink(nextResource: AdminSupportResource): string {
  return targetUserId.value === null
    ? selfPathForSupportResource(nextResource)
    : adminSupportPath(targetUserId.value, nextResource)
}

function beginReadRequest(): SupportReadRequest | null {
  if (targetUserId.value === null) return null
  return {
    sequence: ++loadSequence,
    userId: targetUserId.value,
    resource: resource.value
  }
}

function isCurrentReadRequest(request: SupportReadRequest): boolean {
  return request.sequence === loadSequence &&
    request.userId === targetUserId.value &&
    request.resource === resource.value
}

function setReadError(error: unknown, request: SupportReadRequest): void {
  if (!isCurrentReadRequest(request)) return
  const apiError = error as { message?: string }
  errorMessage.value = apiError.message || t('admin.support.loadFailed')
}

async function loadResource(request: SupportReadRequest): Promise<void> {
  if (request.resource === 'api-keys') await loadAPIKeys(request)
  if (request.resource === 'async-images') await loadImages(request)
  if (request.resource === 'usage') await loadUsage(request)
  if (request.resource === 'channels') await loadChannels(request)
  if (request.resource === 'channel-status') await loadChannelStatus(request)
  if (request.resource === 'subscriptions') await loadSubscriptions(request)
  if (request.resource === 'orders') await loadOrders(request)
}

async function loadPage(): Promise<void> {
  const request = beginReadRequest()
  if (!request) {
    await router.replace(selfPathForSupportResource(resource.value))
    return
  }
  loading.value = true
  errorMessage.value = ''
  selectedTask.value = null
  try {
    await supportStore.loadTarget(request.userId)
  } catch (error) {
    if (!isCurrentReadRequest(request)) return
    const apiError = error as { status?: number; message?: string }
    if (apiError.status === 404) {
      supportStore.clearTarget()
      loading.value = false
      await router.replace(selfPathForSupportResource(request.resource))
      return
    }
    errorMessage.value = apiError.message || t('admin.support.loadFailed')
    loading.value = false
    return
  }

  if (!isCurrentReadRequest(request)) return
  try {
    await loadResource(request)
  } catch (error) {
    setReadError(error, request)
  } finally {
    if (isCurrentReadRequest(request)) loading.value = false
  }
}

async function runReadAction(action: (request: SupportReadRequest) => Promise<void>): Promise<void> {
  const request = beginReadRequest()
  if (!request) return
  loading.value = true
  errorMessage.value = ''
  try {
    await action(request)
  } catch (error) {
    setReadError(error, request)
  } finally {
    if (isCurrentReadRequest(request)) loading.value = false
  }
}

async function loadAPIKeys(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.listAPIKeys(request.userId, apiKeyPage.value, 20)
  if (!isCurrentReadRequest(request)) return
  apiKeys.value = response.items
  apiKeyPages.value = Math.max(1, response.pages || 1)
}

async function changeAPIKeyPage(page: number): Promise<void> {
  apiKeyPage.value = page
  await runReadAction(loadAPIKeys)
}

async function loadImages(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.listAsyncImages(request.userId, { status: imageStatus.value || undefined, limit: imageLimit, offset: imageOffset.value })
  if (!isCurrentReadRequest(request)) return
  imageTasks.value = response.items
  imageHasMore.value = response.has_more
}

async function resetAndLoadImages(): Promise<void> {
  imageOffset.value = 0
  selectedTask.value = null
  await runReadAction(loadImages)
}

async function changeImagePage(direction: -1 | 1): Promise<void> {
  imageOffset.value = Math.max(0, imageOffset.value + direction * imageLimit)
  selectedTask.value = null
  await runReadAction(loadImages)
}

async function openTask(task: AdminSupportImageTask): Promise<void> {
  const request = beginReadRequest()
  if (!request) return
  errorMessage.value = ''
  try {
    const detail = await supportAPI.getAsyncImage(request.userId, task.task_id)
    if (isCurrentReadRequest(request)) selectedTask.value = detail
  } catch (error) {
    setReadError(error, request)
  }
}

async function loadUsage(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.getUsage(request.userId, usagePeriod.value)
  if (isCurrentReadRequest(request)) usage.value = response
}

async function reloadUsage(): Promise<void> {
  await runReadAction(loadUsage)
}

async function loadChannels(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.listChannels(request.userId)
  if (!isCurrentReadRequest(request)) return
  channels.value = response.items
  channelGroupRates.value = response.group_rates || {}
}

async function loadChannelStatus(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.listChannelStatus(request.userId)
  if (!isCurrentReadRequest(request)) return
  channelStatus.value = response.items
  selectedChannelStatus.value = null
}

async function openChannelStatus(monitorId: number): Promise<void> {
  const request = beginReadRequest()
  if (!request) return
  errorMessage.value = ''
  try {
    const detail = await supportAPI.getChannelStatus(request.userId, monitorId)
    if (isCurrentReadRequest(request)) selectedChannelStatus.value = detail
  } catch (error) {
    setReadError(error, request)
  }
}

async function loadSubscriptions(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.listSubscriptions(request.userId)
  if (isCurrentReadRequest(request)) subscriptions.value = response
}

async function loadOrders(request: SupportReadRequest): Promise<void> {
  const response = await supportAPI.listOrders(request.userId, {
    page: orderPage.value,
    page_size: 20,
    status: orderStatus.value || undefined
  })
  if (!isCurrentReadRequest(request)) return
  orders.value = response.items
  orderPages.value = Math.max(1, response.pages || 1)
}

async function resetAndLoadOrders(): Promise<void> {
  orderPage.value = 1
  await runReadAction(loadOrders)
}

async function changeOrderPage(page: number): Promise<void> {
  orderPage.value = page
  await runReadAction(loadOrders)
}

function imageURLs(task: AdminSupportImageTask | null): string[] {
  if (!task) return []
  const result = task.result as { data?: Array<{ url?: unknown }> } | undefined
  const urls = result?.data?.map((item) => typeof item.url === 'string' ? sanitizeUrl(item.url) : '').filter(Boolean) ?? []
  if (urls.length === 0 && task.image_url) {
    const url = sanitizeUrl(task.image_url)
    if (url) urls.push(url)
  }
  return [...new Set(urls)]
}

function statusLabel(status: string): string {
  const key = ['active', 'inactive', 'disabled'].includes(status) ? `common.${status}` : status === 'processing' ? 'common.processing' : status === 'completed' ? 'admin.support.completed' : status === 'failed' ? 'admin.support.failed' : status === 'expired' ? 'admin.support.expired' : status === 'quota_exhausted' ? 'admin.support.quotaExhausted' : ''
  return key ? t(key) : status
}

function statusBadgeClass(status: string): string {
  const base = 'inline-flex rounded-full px-2 py-0.5 text-xs font-medium'
  if (status === 'active' || status === 'completed' || status === 'operational') return `${base} bg-emerald-100 text-emerald-700 dark:bg-emerald-900/35 dark:text-emerald-300`
  if (status === 'processing') return `${base} bg-blue-100 text-blue-700 dark:bg-blue-900/35 dark:text-blue-300`
  if (status === 'degraded') return `${base} bg-amber-100 text-amber-700 dark:bg-amber-900/35 dark:text-amber-300`
  if (status === 'failed' || status === 'error' || status === 'quota_exhausted') return `${base} bg-red-100 text-red-700 dark:bg-red-900/35 dark:text-red-300`
  return `${base} bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300`
}

function roleLabel(role: string): string { return role === 'admin' ? t('admin.support.adminRole') : t('admin.support.userRole') }
function formatMoney(value: number): string { return `$${Number(value || 0).toFixed(4)}` }
function formatNumber(value: number): string { return new Intl.NumberFormat().format(Number(value || 0)) }
function formatPercent(value: number): string { return `${Number(value || 0).toFixed(2)}%` }
function formatUsageWindow(used: number, limit?: number | null): string { return `${formatMoney(used)} / ${limit ? formatMoney(limit) : t('admin.support.unlimited')}` }
function formatDate(value?: string | null): string { return value ? new Date(value).toLocaleString() : t('common.notAvailable') }
function formatUnixDate(value?: number | null): string { return value ? new Date(value * 1000).toLocaleString() : t('common.notAvailable') }
function formatJSON(value: unknown): string { try { return JSON.stringify(value, null, 2) } catch { return String(value) } }

watch(() => [route.params.user_id, route.meta.adminSupportResource], () => {
  apiKeyPage.value = 1
  imageOffset.value = 0
  orderPage.value = 1
  void loadPage()
}, { immediate: true })
</script>

<style scoped>
.table-heading {
  padding: 0.75rem 1.25rem;
  text-align: left;
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: rgb(107 114 128);
  text-transform: uppercase;
  white-space: nowrap;
}

.table-cell {
  padding: 0.875rem 1.25rem;
  font-size: 0.875rem;
  color: rgb(75 85 99);
}

.dark .table-heading,
.dark .table-cell {
  color: rgb(156 163 175);
}
</style>
