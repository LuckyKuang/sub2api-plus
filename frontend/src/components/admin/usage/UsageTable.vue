<template>
  <div :class="flat ? '' : 'card overflow-hidden'">
    <div
      v-if="showIpGeoToolbar"
      class="flex items-center justify-end gap-2 border-b border-gray-200 px-4 py-2 dark:border-dark-700"
    >
      <span v-if="pendingIpCount > 0" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('usage.ipGeo.pending', { count: pendingIpCount }) }}
      </span>
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded px-2 py-1 text-xs font-medium text-primary-600 transition-colors hover:bg-primary-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/30"
        :disabled="ipGeoBatchLoading || pendingIpCount === 0"
        @click="handleBatchFetchIpGeo"
      >
        {{ ipGeoBatchLoading ? t('usage.ipGeo.batchFetching') : t('usage.ipGeo.batchFetch') }}
      </button>
    </div>
    <div class="overflow-auto">
      <DataTable
        :columns="columns"
        :data="data"
        :loading="loading"
        :server-side-sort="serverSideSort"
        :default-sort-key="defaultSortKey"
        :default-sort-order="defaultSortOrder"
        @sort="(key, order) => $emit('sort', key, order)"
      >
        <template #cell-user="{ row }">
          <div class="text-sm">
            <button
              v-if="row.user?.email"
              class="font-medium text-primary-600 underline decoration-dashed underline-offset-2 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              @click="$emit('userClick', row.user_id, row.user?.email)"
              :title="t('admin.usage.clickToViewBalance')"
            >
              {{ row.user.email }}
            </button>
            <span v-else class="font-medium text-gray-900 dark:text-white">-</span>
            <span v-if="row.user?.deleted_at" class="ml-1 inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30">
              {{ t('admin.usage.userDeletedBadge') }}
            </span>
            <span class="ml-1 text-gray-500 dark:text-gray-400">#{{ row.user_id }}</span>
          </div>
        </template>

        <template #cell-api_key="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ row.api_key?.name || '-' }}</span>
        </template>

        <template #cell-account="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">{{ row.account?.name || '-' }}</span>
        </template>

        <template #cell-model="{ row }">
          <div class="space-y-0.5 text-xs">
            <div v-if="row.model_mapping_chain && row.model_mapping_chain.includes('→')" class="space-y-0.5">
              <div v-for="(step, i) in row.model_mapping_chain.split('→')" :key="i"
                   class="break-all"
                   :class="i === 0 ? 'font-medium text-gray-900 dark:text-white' : 'text-gray-500 dark:text-gray-400'"
                   :style="i > 0 ? `padding-left: ${i * 0.75}rem` : ''">
                <span v-if="i > 0" class="mr-0.5">↳</span>{{ step }}
              </div>
            </div>
            <div v-else-if="row.upstream_model && row.upstream_model !== row.model" class="space-y-0.5">
              <div class="break-all font-medium text-gray-900 dark:text-white">
                {{ row.model }}
              </div>
              <div class="break-all text-gray-500 dark:text-gray-400">
                <span class="mr-0.5">↳</span>{{ row.upstream_model }}
              </div>
            </div>
            <span v-else class="font-medium text-gray-900 dark:text-white">{{ row.model }}</span>
            <div
              v-if="row.upstream_model_mismatch === true && row.upstream_response_model"
              class="break-all pl-3 text-[11px]"
              :class="isLikelyModelVariant(row) ? 'text-amber-600 dark:text-amber-400' : 'text-orange-600 dark:text-orange-400'"
              :title="modelAuditTitle(row)"
            >
              <span class="mr-1">↳ {{ t('usage.upstreamResponseModel') }}:</span>{{ row.upstream_response_model }}
              <span
                class="ml-1 inline-flex rounded px-1 py-px text-[10px] font-medium ring-1 ring-inset"
                :class="isLikelyModelVariant(row)
                  ? 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/30'
                  : 'bg-orange-50 text-orange-700 ring-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:ring-orange-500/30'"
              >
                {{ isLikelyModelVariant(row) ? t('usage.modelVariant') : t('usage.modelMismatch') }}
              </span>
            </div>
          </div>
        </template>

        <template #cell-reasoning_effort="{ row }">
          <span class="text-sm text-gray-900 dark:text-white">
            {{ formatReasoningEffort(row.reasoning_effort) }}
          </span>
        </template>

        <template #cell-endpoint="{ row }">
          <div class="max-w-[320px] space-y-1 text-xs">
            <div class="break-all text-gray-700 dark:text-gray-300">
              <span class="font-medium text-gray-500 dark:text-gray-400">{{ t('usage.inbound') }}:</span>
              <span class="ml-1">{{ row.inbound_endpoint?.trim() || '-' }}</span>
            </div>
            <div v-if="showUpstreamEndpoint" class="break-all text-gray-700 dark:text-gray-300">
              <span class="font-medium text-gray-500 dark:text-gray-400">{{ t('usage.upstream') }}:</span>
              <span class="ml-1">{{ row.upstream_endpoint?.trim() || '-' }}</span>
            </div>
          </div>
        </template>

        <template #cell-group="{ row }">
          <span v-if="row.group" class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-200">
            {{ row.group.name }}
          </span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-stream="{ row }">
          <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="getRequestTypeBadgeClass(row)">
            {{ getRequestTypeLabel(row) }}
          </span>
        </template>

        <template #cell-billing_mode="{ row }">
          <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="getBillingModeBadgeClass(getDisplayBillingMode(row))">
            {{ getBillingModeLabel(getDisplayBillingMode(row), t) }}
          </span>
        </template>

        <template #cell-tokens="{ row }">
          <!-- 图片生成请求（仅按次计费时显示图片格式） -->
          <div v-if="isImageUsage(row)" class="flex items-center gap-1.5">
            <svg class="h-4 w-4 text-indigo-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
            </svg>
            <span class="font-medium text-gray-900 dark:text-white">{{ row.image_count }}{{ t('usage.imageUnit') }}</span>
            <span class="text-gray-400">({{ formatImageBillingSize(row, t) }})</span>
          </div>
          <!-- Token 请求 -->
          <div v-else class="flex items-center gap-1.5">
            <div class="space-y-1 text-sm">
              <div class="flex items-center gap-2">
                <div class="inline-flex items-center gap-1">
                  <Icon name="arrowDown" size="sm" class="h-3.5 w-3.5 text-emerald-500" />
                  <span class="font-medium text-gray-900 dark:text-white">{{ textInputTokens(row).toLocaleString() }}</span>
                  <span v-if="requestTokenShare(row, textInputTokens(row))" class="text-[10px] text-gray-400">{{ requestTokenShare(row, textInputTokens(row)) }}</span>
                </div>
                <div class="inline-flex items-center gap-1">
                  <Icon name="arrowUp" size="sm" class="h-3.5 w-3.5 text-violet-500" />
                  <span class="font-medium text-gray-900 dark:text-white">{{ textOutputTokens(row).toLocaleString() }}</span>
                  <span v-if="requestTokenShare(row, textOutputTokens(row))" class="text-[10px] text-gray-400">{{ requestTokenShare(row, textOutputTokens(row)) }}</span>
                </div>
              </div>
              <div v-if="row.cache_read_tokens > 0 || row.cache_creation_tokens > 0" class="flex items-center gap-2">
                <div v-if="row.cache_read_tokens > 0" class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-sky-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" /></svg>
                  <span class="font-medium text-sky-600 dark:text-sky-400">{{ formatCacheTokens(row.cache_read_tokens) }}</span>
                  <span v-if="requestTokenShare(row, row.cache_read_tokens)" class="text-[10px] text-gray-400">{{ requestTokenShare(row, row.cache_read_tokens) }}</span>
                </div>
                <div v-if="row.cache_creation_tokens > 0" class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-amber-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>
                  <span class="font-medium text-amber-600 dark:text-amber-400">{{ formatCacheTokens(row.cache_creation_tokens) }}</span>
                  <span v-if="requestTokenShare(row, row.cache_creation_tokens)" class="text-[10px] text-gray-400">{{ requestTokenShare(row, row.cache_creation_tokens) }}</span>
                  <span v-if="row.cache_creation_1h_tokens > 0" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-100 text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/20 dark:text-orange-400 dark:ring-orange-500/30">1h</span>
                  <span v-if="row.cache_ttl_overridden" :title="t('usage.cacheTtlOverriddenHint')" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30 cursor-help">R</span>
                </div>
              </div>
              <div v-if="hasImageInputTokens(row)" class="flex items-center gap-2">
                <div class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-fuchsia-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                  <span class="font-medium text-fuchsia-600 dark:text-fuchsia-400">{{ row.image_input_tokens.toLocaleString() }}</span>
                  <span v-if="requestTokenShare(row, row.image_input_tokens)" class="text-[10px] text-gray-400">{{ requestTokenShare(row, row.image_input_tokens) }}</span>
                </div>
              </div>
              <div v-if="hasImageOutputTokens(row)" class="flex items-center gap-2">
                <div class="inline-flex items-center gap-1">
                  <svg class="h-3.5 w-3.5 text-pink-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" /></svg>
                  <span class="font-medium text-pink-600 dark:text-pink-400">{{ row.image_output_tokens.toLocaleString() }}</span>
                  <span v-if="requestTokenShare(row, row.image_output_tokens)" class="text-[10px] text-gray-400">{{ requestTokenShare(row, row.image_output_tokens) }}</span>
                </div>
              </div>
            </div>
            <!-- Token Detail Tooltip -->
            <div
              class="group relative"
              @mouseenter="showTokenTooltip($event, row)"
              @mouseleave="hideTokenTooltip"
            >
              <div class="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100 dark:bg-gray-700 dark:group-hover:bg-blue-900/50">
                <Icon name="infoCircle" size="xs" class="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
              </div>
            </div>
          </div>
        </template>

        <template #cell-cost="{ row }">
          <div class="text-sm">
            <div class="flex items-center gap-1.5">
              <span class="font-medium text-green-600 dark:text-green-400">${{ row.actual_cost?.toFixed(6) || '0.000000' }}</span>
              <span
                v-if="row.long_context_billing_applied"
                data-testid="long-context-billing-marker"
                class="inline-flex items-center rounded px-1 py-px text-[10px] font-semibold leading-tight bg-amber-100 text-amber-700 ring-1 ring-inset ring-amber-200 dark:bg-amber-500/20 dark:text-amber-300 dark:ring-amber-500/30"
              >x2</span>
              <!-- Cost Detail Tooltip -->
              <div
                class="group relative"
                @mouseenter="showTooltip($event, row)"
                @mouseleave="hideTooltip"
              >
                <div class="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100 dark:bg-gray-700 dark:group-hover:bg-blue-900/50">
                  <Icon name="infoCircle" size="xs" class="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
                </div>
              </div>
            </div>
            <div v-if="showAccountBilling && row.account_rate_multiplier != null" class="mt-0.5 text-[11px] text-orange-500 dark:text-orange-400">
              A ${{ accountBilled(row).toFixed(6) }}
            </div>
          </div>
        </template>

        <!-- 主列固定：首字/总耗时/TPS；首输出模态与时间细节放入 ⓘ hover -->
        <template #cell-latency="{ row }">
          <div class="flex items-stretch gap-2">
            <span
              data-testid="latency-bar"
              class="w-1 shrink-0 rounded-full"
              :class="latencyBarClasses(row)"
              aria-hidden="true"
            ></span>
            <div class="grid grid-cols-[max-content_max-content] items-baseline gap-x-2 gap-y-0.5 text-xs">
              <span class="inline-flex items-center gap-1 text-gray-400 dark:text-gray-500">
                {{ primaryFirstTokenLabel(row) }}
                <div
                  v-if="hasLatencyDetails(row)"
                  data-testid="latency-details-trigger"
                  class="group relative"
                  @mouseenter="showLatencyTooltip($event, row)"
                  @mouseleave="hideLatencyTooltip"
                >
                  <div class="flex h-3.5 w-3.5 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100 dark:bg-gray-700 dark:group-hover:bg-blue-900/50">
                    <Icon name="infoCircle" size="xs" class="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400" />
                  </div>
                </div>
              </span>
              <span
                v-if="row.first_token_ms != null"
                data-testid="first-latency-value"
                class="font-medium tabular-nums"
                :class="primaryFirstTokenTextClass(row)"
              >{{ formatDuration(row.first_token_ms) }}</span>
              <span v-else data-testid="first-latency-value" class="text-gray-400 dark:text-gray-500">-</span>
              <span class="text-gray-400 dark:text-gray-500">{{ t('usage.latencyDuration') }}</span>
              <span data-testid="latency-duration" class="font-medium tabular-nums" :class="durationTextClass(row)">{{ formatDuration(row.duration_ms) }}</span>
              <template v-if="estimatedTps(row) != null">
                <span class="cursor-help text-gray-400 dark:text-gray-500" :title="t('usage.latencyTpsHint')">{{ t('usage.latencyTps') }}</span>
                <span data-testid="latency-tps" class="cursor-help font-medium tabular-nums text-cyan-600 dark:text-cyan-400" :title="t('usage.latencyTpsHint')">{{ formatTps(estimatedTps(row)!) }}</span>
              </template>
            </div>
          </div>
        </template>

        <template #cell-created_at="{ value }">
          <span class="text-sm text-gray-600 dark:text-gray-400">{{ formatDateTime(value) }}</span>
        </template>

        <template #cell-request_id="{ row }">
          <div v-if="row.request_id" class="flex max-w-[160px] items-center gap-1.5">
            <span class="truncate font-mono text-xs text-gray-500 dark:text-gray-400" :title="row.request_id">
              {{ row.request_id }}
            </span>
            <button
              type="button"
              class="shrink-0 rounded p-0.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-300"
              :class="copiedRequestId === row.request_id ? 'text-green-500 hover:text-green-500' : ''"
              :title="copiedRequestId === row.request_id ? t('keys.copied') : t('keys.copyToClipboard')"
              @click="copyRequestId(row.request_id)"
            >
              <Icon :name="copiedRequestId === row.request_id ? 'check' : 'copy'" size="sm" class="h-3.5 w-3.5" />
            </button>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-user_agent="{ row }">
          <div v-if="row.user_agent" class="flex max-w-[320px] items-center gap-1.5">
            <span class="truncate text-sm text-gray-600 dark:text-gray-400" :title="row.user_agent">{{ formatUserAgent(row.user_agent) }}</span>
            <button
              type="button"
              class="shrink-0 rounded p-0.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-300"
              :class="copiedUserAgent === row.user_agent ? 'text-green-500 hover:text-green-500' : ''"
              :title="copiedUserAgent === row.user_agent ? t('keys.copied') : t('keys.copyToClipboard')"
              @click="copyUserAgent(row.user_agent)"
            >
              <Icon :name="copiedUserAgent === row.user_agent ? 'check' : 'copy'" size="sm" class="h-3.5 w-3.5" />
            </button>
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-session_id="{ row }">
          <span
            v-if="row.session_id"
            data-testid="usage-session-id"
            class="block max-w-[320px] truncate font-mono text-sm text-gray-600 dark:text-gray-400"
            :title="row.session_id"
          >{{ row.session_id }}</span>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #cell-ip_address="{ row }">
          <div v-if="row.ip_address">
            <span class="text-sm font-mono text-gray-600 dark:text-gray-400">{{ row.ip_address }}</span>
            <IpGeoCell :ip="row.ip_address" />
          </div>
          <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
        </template>

        <template #empty><EmptyState :message="t('usage.noRecords')" /></template>
      </DataTable>
    </div>
  </div>

  <!-- Token Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="tokenTooltipVisible"
      class="fixed z-[9999] pointer-events-none -translate-y-1/2"
      :style="{
        left: tokenTooltipPosition.x + 'px',
        top: tokenTooltipPosition.y + 'px'
      }"
    >
      <div class="whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800">
        <div class="space-y-1.5">
          <div>
            <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.tokenDetails') }}</div>
            <div v-if="tokenTooltipData && tokenTooltipData.input_tokens > 0 && !hasImageInputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.input_tokens.toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, tokenTooltipData.input_tokens)" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, tokenTooltipData.input_tokens) }})</span></span>
            </div>
            <div v-if="tokenTooltipData && hasImageInputTokens(tokenTooltipData) && textInputTokens(tokenTooltipData) > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputTokens') }}</span>
              <span class="font-medium text-white">{{ textInputTokens(tokenTooltipData).toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, textInputTokens(tokenTooltipData))" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, textInputTokens(tokenTooltipData)) }})</span></span>
            </div>
            <div v-if="tokenTooltipData && hasImageInputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageInputTokens') }}</span>
              <span class="font-medium text-fuchsia-300">{{ tokenTooltipData.image_input_tokens.toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, tokenTooltipData.image_input_tokens)" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, tokenTooltipData.image_input_tokens) }})</span></span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.output_tokens > 0 && !hasImageOutputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.output_tokens.toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, tokenTooltipData.output_tokens)" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, tokenTooltipData.output_tokens) }})</span></span>
            </div>
            <div v-if="tokenTooltipData && hasImageOutputTokens(tokenTooltipData) && textOutputTokens(tokenTooltipData) > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputTokens') }}</span>
              <span class="font-medium text-white">{{ textOutputTokens(tokenTooltipData).toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, textOutputTokens(tokenTooltipData))" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, textOutputTokens(tokenTooltipData)) }})</span></span>
            </div>
            <div v-if="tokenTooltipData && hasImageOutputTokens(tokenTooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageOutputTokens') }}</span>
              <span class="font-medium text-pink-300">{{ tokenTooltipData.image_output_tokens.toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, tokenTooltipData.image_output_tokens)" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, tokenTooltipData.image_output_tokens) }})</span></span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_creation_tokens > 0">
              <!-- 有 5m/1h 明细时，展开显示 -->
              <template v-if="tokenTooltipData.cache_creation_5m_tokens > 0 || tokenTooltipData.cache_creation_1h_tokens > 0">
                <div v-if="tokenTooltipData.cache_creation_5m_tokens > 0" class="flex items-center justify-between gap-4">
                  <span class="text-gray-400 flex items-center gap-1.5">
                    {{ t('admin.usage.cacheCreation5mTokens') }}
                    <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-amber-500/20 text-amber-400 ring-1 ring-inset ring-amber-500/30">5m</span>
                  </span>
                  <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_5m_tokens.toLocaleString() }} <span v-if="formatTokenShare(tokenTooltipData.cache_creation_5m_tokens, tokenTooltipData.cache_creation_tokens)" class="text-gray-400">({{ formatTokenShare(tokenTooltipData.cache_creation_5m_tokens, tokenTooltipData.cache_creation_tokens) }})</span></span>
                </div>
                <div v-if="tokenTooltipData.cache_creation_1h_tokens > 0" class="flex items-center justify-between gap-4">
                  <span class="text-gray-400 flex items-center gap-1.5">
                    {{ t('admin.usage.cacheCreation1hTokens') }}
                    <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-500/20 text-orange-400 ring-1 ring-inset ring-orange-500/30">1h</span>
                  </span>
                  <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_1h_tokens.toLocaleString() }} <span v-if="formatTokenShare(tokenTooltipData.cache_creation_1h_tokens, tokenTooltipData.cache_creation_tokens)" class="text-gray-400">({{ formatTokenShare(tokenTooltipData.cache_creation_1h_tokens, tokenTooltipData.cache_creation_tokens) }})</span></span>
                </div>
              </template>
              <!-- 无明细时，只显示聚合值 -->
              <div v-else class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('admin.usage.cacheCreationTokens') }}</span>
                <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_tokens.toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, tokenTooltipData.cache_creation_tokens)" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, tokenTooltipData.cache_creation_tokens) }})</span></span>
              </div>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_ttl_overridden" class="flex items-center justify-between gap-4">
              <span class="text-gray-400 flex items-center gap-1.5">
                {{ t('usage.cacheTtlOverriddenLabel') }}
                <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-500/20 text-rose-400 ring-1 ring-inset ring-rose-500/30">R-{{ tokenTooltipData.cache_creation_1h_tokens > 0 ? '5m' : '1H' }}</span>
              </span>
              <span class="font-medium text-rose-400">{{ tokenTooltipData.cache_creation_1h_tokens > 0 ? t('usage.cacheTtlOverridden1h') : t('usage.cacheTtlOverridden5m') }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_read_tokens > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheReadTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.cache_read_tokens.toLocaleString() }} <span v-if="requestTokenShare(tokenTooltipData, tokenTooltipData.cache_read_tokens)" class="text-gray-400">({{ requestTokenShare(tokenTooltipData, tokenTooltipData.cache_read_tokens) }})</span></span>
            </div>
          </div>
          <div class="flex items-center justify-between gap-6 border-t border-gray-700 pt-1.5">
            <span class="text-gray-400">{{ t('usage.totalTokens') }}</span>
            <span class="font-semibold text-blue-400">{{ ((tokenTooltipData?.input_tokens || 0) + (tokenTooltipData?.output_tokens || 0) + (tokenTooltipData?.cache_creation_tokens || 0) + (tokenTooltipData?.cache_read_tokens || 0)).toLocaleString() }}</span>
          </div>
        </div>
        <div class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"></div>
      </div>
    </div>
  </Teleport>

  <!-- Latency Detail Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="latencyTooltipVisible"
      data-testid="latency-details-tooltip"
      class="fixed z-[9999] pointer-events-none -translate-y-1/2"
      :style="{
        left: latencyTooltipPosition.x + 'px',
        top: latencyTooltipPosition.y + 'px'
      }"
    >
      <div class="whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800">
        <div class="space-y-1.5">
          <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.latencyDetails') }}</div>
          <div v-if="latencyTooltipData?.first_output_kind" class="flex items-center justify-between gap-4">
            <span class="text-gray-400">{{ t('usage.latencyFirstOutputKind') }}</span>
            <span class="font-medium text-white">{{ firstOutputModalityLabel(latencyTooltipData, 'kind') }}</span>
          </div>
          <div v-if="latencyTooltipData && latencyDetailFirstOutputMs(latencyTooltipData) != null" class="flex items-center justify-between gap-4">
            <span class="text-gray-400">{{ firstOutputModalityLabel(latencyTooltipData, 'timing') }}</span>
            <span class="font-medium text-white">{{ formatDuration(latencyDetailFirstOutputMs(latencyTooltipData)) }}</span>
          </div>
          <div v-if="latencyTooltipData?.first_token_ms != null" class="flex items-center justify-between gap-4">
            <span class="text-gray-400">{{ latencyTooltipData.first_output_kind == null ? t('usage.latencyLegacyFirstEvent') : t('usage.latencyFirstToken') }}</span>
            <span class="font-medium text-white">{{ formatDuration(latencyTooltipData.first_token_ms) }}</span>
          </div>
          <div v-if="latencyTooltipData?.duration_ms != null" class="flex items-center justify-between gap-4">
            <span class="text-gray-400">{{ t('usage.latencyDuration') }}</span>
            <span class="font-medium text-white">{{ formatDuration(latencyTooltipData.duration_ms) }}</span>
          </div>
          <div v-if="latencyTooltipData && estimatedTps(latencyTooltipData) != null" class="flex items-center justify-between gap-4">
            <span class="text-gray-400">{{ t('usage.latencyTps') }}</span>
            <span class="font-medium text-cyan-300">{{ formatTps(estimatedTps(latencyTooltipData)!) }}</span>
          </div>
          <div v-if="latencyTooltipNote(latencyTooltipData)" class="border-t border-gray-700 pt-1.5 text-[11px] leading-relaxed text-gray-400">
            {{ latencyTooltipNote(latencyTooltipData) }}
          </div>
        </div>
        <div class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"></div>
      </div>
    </div>
  </Teleport>

  <!-- Cost Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="tooltipVisible"
      class="fixed z-[9999] pointer-events-none -translate-y-1/2"
      :style="{
        left: tooltipPosition.x + 'px',
        top: tooltipPosition.y + 'px'
      }"
    >
      <div class="whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800">
        <div class="space-y-1.5">
          <!-- Cost Breakdown -->
          <div class="mb-2 border-b border-gray-700 pb-1.5">
            <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.costDetails') }}</div>
            <div v-if="tooltipData && tooltipData.input_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.input_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && hasImageInputCost(tooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageInputCost') }}</span>
              <span class="font-medium text-fuchsia-300">${{ tooltipData.image_input_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.output_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.output_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && hasImageOutputCost(tooltipData)" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.imageOutputCost') }}</span>
              <span class="font-medium text-pink-300">${{ tooltipData.image_output_cost.toFixed(6) }}</span>
            </div>
            <!-- Token billing: show unit prices per 1M tokens -->
            <template v-if="tooltipData && !isImageUsage(tooltipData) && (!tooltipData.billing_mode || tooltipData.billing_mode === BILLING_MODE_TOKEN)">
              <div v-if="tooltipData && textInputTokens(tooltipData) > 0" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.inputTokenPrice') }}</span>
                <span class="font-medium text-sky-300">{{ formatTokenPricePerMillion(tooltipData.input_cost, textInputTokens(tooltipData)) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && hasImageInputTokens(tooltipData)" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageInputTokenPrice') }}</span>
                <span class="font-medium text-fuchsia-300">{{ formatTokenPricePerMillion(tooltipData.image_input_cost ?? 0, tooltipData.image_input_tokens) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && tooltipData.output_cost > 0 && textOutputTokens(tooltipData) > 0" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.outputTokenPrice') }}</span>
                <span class="font-medium text-violet-300">{{ formatTokenPricePerMillion(tooltipData.output_cost, textOutputTokens(tooltipData)) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && hasImageOutputTokens(tooltipData)" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageOutputTokenPrice') }}</span>
                <span class="font-medium text-pink-300">{{ formatTokenPricePerMillion(tooltipData.image_output_cost ?? 0, tooltipData.image_output_tokens) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
            </template>
            <template v-else-if="tooltipData && isImageUsage(tooltipData)">
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageCount') }}</span>
                <span class="font-medium text-white">{{ tooltipData.image_count }}{{ t('usage.imageUnit') }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageBillingSize') }}</span>
                <span class="font-medium text-white">{{ formatImageBillingSize(tooltipData, t) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageSizeSource') }}</span>
                <span class="font-medium text-white">{{ formatImageSizeSource(tooltipData, t) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageInputSize') }}</span>
                <span class="font-medium text-white">{{ formatImageInputSize(tooltipData, t) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageOutputSize') }}</span>
                <span class="font-medium text-white">{{ formatImageOutputSize(tooltipData, t) }}</span>
              </div>
              <div v-if="formatImageSizeBreakdown(tooltipData)" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageSizeBreakdown') }}</span>
                <span class="font-medium text-white">{{ formatImageSizeBreakdown(tooltipData) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageUnitPrice') }}</span>
                <span class="font-medium text-sky-300">${{ imageUnitPrice(tooltipData).toFixed(6) }}</span>
              </div>
              <div class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.imageTotalPrice') }}</span>
                <span class="font-medium text-white">${{ tooltipData.total_cost?.toFixed(6) || '0.000000' }}</span>
              </div>
            </template>
            <div v-else class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.unitPrice') }}</span>
              <span class="font-medium text-sky-300">${{ tooltipData?.total_cost?.toFixed(6) || '0.000000' }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.cache_creation_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheCreationCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.cache_creation_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.cache_read_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheReadCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.cache_read_cost.toFixed(6) }}</span>
            </div>
          </div>
          <!-- Rate and Summary -->
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.serviceTier') }}</span>
            <span class="font-semibold text-cyan-300">{{ getUsageServiceTierLabel(tooltipData?.service_tier, t) }}</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.rate') }}</span>
            <span class="font-semibold text-blue-400">{{ formatMultiplier(tooltipData?.rate_multiplier || 1) }}x</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.original') }}</span>
            <span class="font-medium text-white">${{ tooltipData?.total_cost?.toFixed(6) || '0.000000' }}</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.userBilled') }}</span>
            <span class="font-semibold text-green-400">${{ tooltipData?.actual_cost?.toFixed(6) || '0.000000' }}</span>
          </div>
          <!-- Account billing (separated from user billing) -->
          <template v-if="showAccountBilling">
            <div class="flex items-center justify-between gap-6 border-t border-gray-700 pt-1.5">
              <span class="text-gray-400">{{ t('usage.accountMultiplier') }}</span>
              <span class="font-semibold text-blue-400">{{ formatMultiplier(tooltipData?.account_rate_multiplier ?? 1) }}x</span>
            </div>
            <div class="flex items-center justify-between gap-6">
              <span class="text-gray-400">{{ t('usage.accountBilled') }}</span>
              <span class="font-semibold text-green-400">
                ${{ accountBilled({
                  total_cost: tooltipData?.total_cost,
                  account_stats_cost: tooltipData?.account_stats_cost,
                  account_rate_multiplier: tooltipData?.account_rate_multiplier,
                }).toFixed(6) }}
              </span>
            </div>
          </template>
        </div>
        <div class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"></div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatDateTime, formatReasoningEffort } from '@/utils/format'
import { formatCacheTokens, formatMultiplier } from '@/utils/formatters'
import { formatTokenPricePerMillion } from '@/utils/usagePricing'
import { formatTokenShare, sumTokenBuckets } from '@/utils/tokenShare'
import { getUsageServiceTierLabel } from '@/utils/usageServiceTier'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
import {
  LATENCY_BAR_CLASSES,
  LATENCY_BAR_FROM_CLASSES,
  LATENCY_BAR_TO_CLASSES,
  LATENCY_TEXT_CLASSES,
  durationSeverity,
  firstTokenSeverity,
} from '@/utils/latencyHealth'
import {
  BILLING_MODE_TOKEN,
  getBillingModeLabel,
  getBillingModeBadgeClass,
  isImageUsage,
  getDisplayBillingMode,
  imageUnitPrice,
} from '@/utils/billingMode'
import {
  formatImageBillingSize,
  formatImageInputSize,
  formatImageOutputSize,
  formatImageSizeBreakdown,
  formatImageSizeSource,
  hasImageOutputTokens,
  textOutputTokens,
  hasImageOutputCost,
  hasImageInputTokens,
  textInputTokens,
  hasImageInputCost,
} from '@/utils/imageUsage'

/** Compute the account-billed cost for display: (account_stats_cost ?? total_cost) * rate_multiplier */
function accountBilled(row: { total_cost?: number | null; account_stats_cost?: number | null; account_rate_multiplier?: number | null }): number {
  const base = row.account_stats_cost != null ? row.account_stats_cost : (row.total_cost ?? 0)
  const result = base * (row.account_rate_multiplier ?? 1)
  return Number.isNaN(result) ? 0 : result
}

function requestTokenTotal(row: Pick<AdminUsageLog, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens'>): number {
  return sumTokenBuckets(row.input_tokens, row.output_tokens, row.cache_creation_tokens, row.cache_read_tokens)
}

function requestTokenShare(row: Pick<AdminUsageLog, 'input_tokens' | 'output_tokens' | 'cache_creation_tokens' | 'cache_read_tokens'>, tokens: number | null | undefined): string | null {
  return formatTokenShare(tokens, requestTokenTotal(row))
}


import DataTable from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import IpGeoCell from '@/components/common/IpGeoCell.vue'
import Icon from '@/components/icons/Icon.vue'
import { fetchBatch, getEntry } from '@/utils/ipGeoLookup'
import type { AdminUsageLog } from '@/types'
import type { Column } from '@/components/common/types'

interface Props {
  data: AdminUsageLog[]
  loading?: boolean
  columns: Column[]
  serverSideSort?: boolean
  defaultSortKey?: string
  defaultSortOrder?: 'asc' | 'desc'
  showAccountBilling?: boolean
  showUpstreamEndpoint?: boolean
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
  serverSideSort: false,
  defaultSortKey: '',
  defaultSortOrder: 'asc',
  showAccountBilling: true,
  showUpstreamEndpoint: true,
  flat: false
})
const emit = defineEmits<{
  userClick: [userID: number, email?: string]
  sort: [key: string, order: 'asc' | 'desc']
  ipGeoBatchFailed: []
}>()
const { t } = useI18n()
const appStore = useAppStore()
const copiedRequestId = ref<string | null>(null)
const copiedUserAgent = ref<string | null>(null)
const showAccountBilling = props.showAccountBilling
const showUpstreamEndpoint = props.showUpstreamEndpoint
const ipGeoBatchLoading = ref(false)

const showIpGeoToolbar = computed(() => props.columns.some((col) => col.key === 'ip_address'))

const sentUpstreamModel = (row: AdminUsageLog): string => row.upstream_model?.trim() || row.model?.trim() || ''

const normalizeModelVariant = (model: string): string => model
  .trim()
  .toLowerCase()
  .replace(/-latest$/, '')
  .replace(/-\d{4}-\d{2}-\d{2}$/, '')
  .replace(/-\d{8}$/, '')

const isLikelyModelVariant = (row: AdminUsageLog): boolean => {
  const sent = sentUpstreamModel(row)
  const response = row.upstream_response_model?.trim() || ''
  return sent !== '' && response !== '' && normalizeModelVariant(sent) === normalizeModelVariant(response)
}

const modelAuditTitle = (row: AdminUsageLog): string => [
  `${t('usage.requestedModel')}: ${row.model || '-'}`,
  `${t('usage.sentUpstreamModel')}: ${sentUpstreamModel(row) || '-'}`,
  `${t('usage.upstreamResponseModel')}: ${row.upstream_response_model || '-'}`,
].join('\n')

const currentPageIps = computed(() =>
  Array.from(new Set(props.data.map((row) => row.ip_address).filter((ip): ip is string => Boolean(ip))))
)

const pendingIpCount = computed(() => {
  if (!showIpGeoToolbar.value) return 0
  return currentPageIps.value.filter((ip) => {
    const status = getEntry(ip).status
    return status === 'idle' || status === 'error'
  }).length
})

const handleBatchFetchIpGeo = async () => {
  ipGeoBatchLoading.value = true
  try {
    const ok = await fetchBatch(currentPageIps.value)
    if (!ok) emit('ipGeoBatchFailed')
  } finally {
    ipGeoBatchLoading.value = false
  }
}

const copyRequestId = async (requestId: string) => {
  try {
    await navigator.clipboard.writeText(requestId)
    copiedRequestId.value = requestId
    appStore.showSuccess(t('admin.usage.requestIdCopied'))
    window.setTimeout(() => {
      if (copiedRequestId.value === requestId) copiedRequestId.value = null
    }, 2000)
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

const copyUserAgent = async (userAgent: string) => {
  try {
    await navigator.clipboard.writeText(userAgent)
    copiedUserAgent.value = userAgent
    appStore.showSuccess(t('admin.usage.userAgentCopied'))
    window.setTimeout(() => {
      if (copiedUserAgent.value === userAgent) copiedUserAgent.value = null
    }, 2000)
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

// Tooltip state - cost
const tooltipVisible = ref(false)
const tooltipPosition = ref({ x: 0, y: 0 })
const tooltipData = ref<AdminUsageLog | null>(null)

// Tooltip state - token
const tokenTooltipVisible = ref(false)
const tokenTooltipPosition = ref({ x: 0, y: 0 })
const tokenTooltipData = ref<AdminUsageLog | null>(null)

// Tooltip state - latency details
const latencyTooltipVisible = ref(false)
const latencyTooltipPosition = ref({ x: 0, y: 0 })
const latencyTooltipData = ref<AdminUsageLog | null>(null)

const getRequestTypeLabel = (row: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(row)
  if (requestType === 'cyber') return t('usage.cyber')
  if (requestType === 'live') return t('usage.live')
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const getRequestTypeBadgeClass = (row: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(row)
  if (requestType === 'cyber') return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
  if (requestType === 'live') return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-900 dark:text-emerald-200'
  if (requestType === 'ws_v2') return 'bg-violet-100 text-violet-800 dark:bg-violet-900 dark:text-violet-200'
  if (requestType === 'stream') return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
  if (requestType === 'sync') return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200'
  return 'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200'
}



const formatUserAgent = (ua: string): string => {
  return ua
}

// 超过 1 分钟简化为 "Xm Ys"，免去人工换算（超过 1 小时再进位为 "Xh Ym"）
const formatDuration = (ms: number | null | undefined): string => {
  if (ms == null) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const totalSec = Math.round(ms / 1000)
  if (totalSec < 3600) return `${Math.floor(totalSec / 60)}m ${totalSec % 60}s`
  return `${Math.floor(totalSec / 3600)}h ${Math.floor((totalSec % 3600) / 60)}m`
}

const hasStrictFirstToken = (row: AdminUsageLog): boolean =>
  row.first_output_kind != null && row.first_token_ms != null

// Primary column always uses strict first_token_ms (or legacy first_token only).
const primaryFirstTokenLabel = (row: AdminUsageLog): string => {
  if (row.first_output_kind == null && row.first_token_ms != null) {
    return t('usage.latencyLegacyFirstEvent')
  }
  return t('usage.latencyFirstToken')
}

const primaryFirstTokenTextClass = (row: AdminUsageLog): string => {
  if (row.first_token_ms == null) {
    return 'text-gray-400 dark:text-gray-500'
  }
  // Only color by TTFT thresholds for new-semantics token-like samples.
  if (hasStrictFirstToken(row)) {
    return LATENCY_TEXT_CLASSES[firstTokenSeverity(row.first_token_ms)]
  }
  // Legacy first-event values are not comparable TTFT samples.
  return 'text-gray-600 dark:text-gray-300'
}

const hasLatencyDetails = (row: AdminUsageLog): boolean => {
  // Legacy first-event needs explanation so it is not mistaken for strict TTFT.
  if (row.first_output_kind == null) {
    return row.first_token_ms != null
  }
  // Non-text first output always deserves a detail popover.
  if (row.first_output_kind !== 'text') return true
  // Pure text with matching first output/token is fully represented by the primary column.
  if (row.first_token_ms == null) return true
  return row.first_output_ms != null && row.first_output_ms !== row.first_token_ms
}

// Shared modality labels for latency detail tooltip.
// role=kind  → value under "First Output Kind" (text → "Text")
// role=timing → row label for first_output_ms (text → "First Output")
const firstOutputModalityLabel = (
  row: AdminUsageLog | null | undefined,
  role: 'kind' | 'timing',
): string => {
  switch (row?.first_output_kind) {
    case 'image':
      return t('usage.latencyFirstImage')
    case 'audio':
      return t('usage.latencyFirstAudio')
    case 'reasoning':
      return t('usage.latencyFirstReasoning')
    case 'tool':
      return t('usage.latencyFirstTool')
    case 'text':
      return role === 'kind' ? t('usage.latencyOutputKindText') : t('usage.latencyFirstOutput')
    default:
      return role === 'kind' ? (row?.first_output_kind ?? '-') : t('usage.latencyFirstOutput')
  }
}

const latencyDetailFirstOutputMs = (row: AdminUsageLog | null | undefined): number | null => {
  if (row == null) return null
  if (row.first_output_ms != null) return row.first_output_ms
  // When kind exists but first_output_ms is missing, do not invent a value.
  return null
}

const latencyTooltipNote = (row: AdminUsageLog | null | undefined): string | null => {
  if (row == null) return null
  if (row.first_output_kind == null && row.first_token_ms != null) {
    return t('usage.latencyLegacyFirstEventHint')
  }
  if (row.first_output_kind === 'image' || row.first_output_kind === 'audio') {
    if (row.first_token_ms == null) {
      return t('usage.latencyMediaOnlyHint')
    }
    if (row.first_output_ms != null && row.first_output_ms !== row.first_token_ms) {
      return t('usage.latencyMixedModalityHint')
    }
  }
  if (
    (row.first_output_kind === 'tool' || row.first_output_kind === 'reasoning') &&
    row.first_token_ms != null
  ) {
    return t('usage.latencyNonTextFirstHint')
  }
  if (
    row.first_output_kind === 'text' &&
    row.first_output_ms != null &&
    row.first_token_ms != null &&
    row.first_output_ms !== row.first_token_ms
  ) {
    return t('usage.latencyMixedModalityHint')
  }
  return null
}

const durationTextClass = (row: AdminUsageLog): string => {
  if (row.duration_ms == null) {
    return 'text-gray-400 dark:text-gray-500'
  }
  return LATENCY_TEXT_CLASSES[durationSeverity(row.duration_ms)]
}

const latencyBarClasses = (row: AdminUsageLog): string | string[] => {
  if (row.duration_ms == null) {
    return 'bg-gray-300 dark:bg-gray-600'
  }
  if (hasStrictFirstToken(row)) {
    return [
      'bg-gradient-to-b from-40% to-60%',
      LATENCY_BAR_FROM_CLASSES[firstTokenSeverity(row.first_token_ms!)],
      LATENCY_BAR_TO_CLASSES[durationSeverity(row.duration_ms)],
    ]
  }
  return LATENCY_BAR_CLASSES[durationSeverity(row.duration_ms)]
}

// TPS is a coarse estimate (text tokens / post-first-token wall time), not a
// sampled decode rate. Reliability gates hide short generation windows, tiny
// samples, and unrealistically high bursts; they do not correct systemic
// underestimates from long reasoning/tool waits (needs last_token_ms later).
const TPS_MIN_GENERATION_MS = 300
const TPS_MIN_TEXT_TOKENS = 8
const TPS_MAX_VALUE = 500

const estimatedTps = (row: AdminUsageLog): number | null => {
  const requestType = resolveUsageRequestType(row)
  if (requestType !== 'stream' && requestType !== 'ws_v2') return null
  if (row.is_complete !== true) return null
  if (!hasStrictFirstToken(row) || row.duration_ms == null) return null

  const outputTokens = textOutputTokens(row)
  const generationMs = row.duration_ms - row.first_token_ms!
  if (
    !Number.isFinite(outputTokens) ||
    outputTokens < TPS_MIN_TEXT_TOKENS ||
    !Number.isFinite(generationMs) ||
    generationMs < TPS_MIN_GENERATION_MS
  ) {
    return null
  }

  const value = outputTokens * 1000 / generationMs
  if (!Number.isFinite(value) || value <= 0 || value > TPS_MAX_VALUE) return null
  return value
}

const formatTps = (value: number): string =>
  (Math.round(value * 10) / 10).toFixed(1).replace(/\.0$/, '')

// Cost tooltip functions
const showTooltip = (event: MouseEvent, row: AdminUsageLog) => {
  const target = event.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  tooltipData.value = row
  tooltipPosition.value.x = rect.right + 8
  tooltipPosition.value.y = rect.top + rect.height / 2
  tooltipVisible.value = true
}

const hideTooltip = () => {
  tooltipVisible.value = false
  tooltipData.value = null
}

// Token tooltip functions
const showTokenTooltip = (event: MouseEvent, row: AdminUsageLog) => {
  const target = event.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  tokenTooltipData.value = row
  tokenTooltipPosition.value.x = rect.right + 8
  tokenTooltipPosition.value.y = rect.top + rect.height / 2
  tokenTooltipVisible.value = true
}

const hideTokenTooltip = () => {
  tokenTooltipVisible.value = false
  tokenTooltipData.value = null
}

// Latency detail tooltip functions
const showLatencyTooltip = (event: MouseEvent, row: AdminUsageLog) => {
  const target = event.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()
  latencyTooltipData.value = row
  latencyTooltipPosition.value.x = rect.right + 8
  latencyTooltipPosition.value.y = rect.top + rect.height / 2
  latencyTooltipVisible.value = true
}

const hideLatencyTooltip = () => {
  latencyTooltipVisible.value = false
  latencyTooltipData.value = null
}
</script>
