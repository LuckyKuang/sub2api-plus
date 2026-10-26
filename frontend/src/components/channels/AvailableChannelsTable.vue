<template>
  <div class="record-table-scroll table-wrapper">
    <!-- .table-wrapper 是 TablePageLayout 滚动链的挂载点：外层 .table-scroll-container
         负责卡片外观并 overflow-hidden，本层接收 overflow-y-auto 才能在内容超高时滚动。 -->
    <table
      data-testid="desktop-channels"
      class="record-table !hidden w-full table-fixed border-collapse text-sm lg:!table"
    >
      <thead>
        <tr class="border-b border-gray-100 surface-inset text-xs font-medium uppercase tracking-wide dark:border-dark-700 surface-muted">
          <th class="w-[180px] px-4 py-3 text-center">{{ columns.name }}</th>
          <th class="w-[200px] px-4 py-3 text-left">{{ columns.description }}</th>
          <th class="w-[140px] px-4 py-3 text-left">{{ columns.platform }}</th>
          <th class="px-4 py-3 text-left">{{ columns.groups }}</th>
          <th class="px-4 py-3 text-left">{{ columns.supportedModels }}</th>
        </tr>
      </thead>
      <tbody v-if="loading">
        <tr>
          <td colspan="5" class="py-10 text-center">
            <Icon name="refresh" size="lg" class="inline-block animate-spin text-gray-400" />
          </td>
        </tr>
      </tbody>
      <tbody v-else-if="rows.length === 0">
        <tr>
          <td colspan="5" class="py-12 text-center">
            <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
            <p class="text-sm surface-muted">{{ emptyLabel }}</p>
          </td>
        </tr>
      </tbody>
      <!-- 渠道名称和描述跨平台行合并，所有平台共同组成一张渠道卡片。 -->
      <tbody
        v-else
        v-for="(channel, chIdx) in rows"
        :key="`${channel.name}-${chIdx}`"
        class="record-group"
      >
        <tr
          v-for="(section, secIdx) in channel.platforms"
          :key="`${channel.name}-${section.platform}`"
          class="transition-colors"
        >
          <!-- 渠道名：只在第一行渲染并用 rowspan 纵向合并 -->
          <td
            v-if="secIdx === 0"
            :rowspan="channel.platforms.length"
            data-record-edges="t b l tl bl"
            class="px-4 py-3 text-center align-middle font-medium text-gray-900 dark:text-white"
          >
            {{ channel.name }}
          </td>

          <!-- 描述：独立一列，同样用 rowspan 纵向合并 -->
          <td
            v-if="secIdx === 0"
            :rowspan="channel.platforms.length"
            data-record-edges="t b"
            class="px-4 py-3 align-middle text-xs surface-muted"
          >
            <template v-if="channel.description">{{ channel.description }}</template>
            <span v-else class="surface-muted">-</span>
          </td>

          <!-- 平台徽章 -->
          <td :data-record-edges="[secIdx === 0 && 't', secIdx === channel.platforms.length - 1 && 'b'].filter(Boolean).join(' ')" class="align-top px-4 py-3">
            <span
              :class="[
                'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase',
                platformBadgeClass(section.platform),
              ]"
            >
              <PlatformIcon :platform="section.platform as GroupPlatform" size="xs" />
              {{ section.platform }}
            </span>
          </td>

          <!-- 分组：专属分组在前（紫色 shield 行），公开分组在后（灰色 globe 行）。 -->
          <td :data-record-edges="[secIdx === 0 && 't', secIdx === channel.platforms.length - 1 && 'b'].filter(Boolean).join(' ')" class="align-top px-4 py-3">
            <div class="flex flex-col gap-1.5">
              <div
                v-if="exclusiveGroups(section).length > 0"
                class="flex flex-wrap items-center gap-1.5"
              >
                <span
                  class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase text-purple-600 dark:text-purple-400"
                  :title="t('availableChannels.exclusiveTooltip')"
                >
                  <Icon name="shield" size="xs" class="h-3 w-3" />
                  {{ t('availableChannels.exclusive') }}
                </span>
                <div
                  v-for="g in exclusiveGroups(section)"
                  :key="`ex-${g.id}`"
                  class="inline-flex flex-wrap items-center gap-1"
                >
                  <GroupBadge
                    :name="g.name"
                    :platform="g.platform as GroupPlatform"
                    :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="g.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[g.id] ?? null"
                    always-show-rate
                  />
                  <span
                    v-if="hasPeakRate(g)"
                    class="inline-flex items-center gap-1 rounded-md bg-amber-50/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                    :title="peakRateTitle(g)"
                  >
                    <Icon name="clock" size="xs" class="h-3 w-3" />
                    {{ peakRateLabel(g) }}
                  </span>
                </div>
              </div>
              <div
                v-if="publicGroups(section).length > 0"
                class="flex flex-wrap items-center gap-1.5"
              >
                <span
                  class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase surface-muted"
                  :title="t('availableChannels.publicTooltip')"
                >
                  <Icon name="globe" size="xs" class="h-3 w-3" />
                  {{ t('availableChannels.public') }}
                </span>
                <div
                  v-for="g in publicGroups(section)"
                  :key="`pub-${g.id}`"
                  class="inline-flex flex-wrap items-center gap-1"
                >
                  <GroupBadge
                    :name="g.name"
                    :platform="g.platform as GroupPlatform"
                    :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="g.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[g.id] ?? null"
                    always-show-rate
                  />
                  <span
                    v-if="hasPeakRate(g)"
                    class="inline-flex items-center gap-1 rounded-md bg-amber-50/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                    :title="peakRateTitle(g)"
                  >
                    <Icon name="clock" size="xs" class="h-3 w-3" />
                    {{ peakRateLabel(g) }}
                  </span>
                </div>
              </div>
              <span v-if="section.groups.length === 0" class="text-xs surface-muted">-</span>
            </div>
          </td>

          <!-- 支持模型 -->
          <td :data-record-edges="['r', secIdx === 0 && 't tr', secIdx === channel.platforms.length - 1 && 'b br'].filter(Boolean).join(' ')" class="align-top px-4 py-3">
            <div class="flex flex-wrap gap-1">
              <SupportedModelChip
                v-for="m in section.supported_models"
                :key="`${section.platform}-${m.name}`"
                :model="m"
                :pricing-key-prefix="pricingKeyPrefix"
                :no-pricing-label="noPricingLabel"
                :show-platform="false"
                :platform-hint="section.platform"
              />
              <span v-if="section.supported_models.length === 0" class="text-xs surface-muted">
                {{ noModelsLabel }}
              </span>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <div data-testid="mobile-channels" class="w-full min-w-0 overflow-x-hidden lg:hidden">
      <div v-if="loading" data-testid="mobile-loading" class="py-10 text-center">
        <Icon name="refresh" size="lg" class="inline-block animate-spin text-gray-400" />
      </div>
      <div v-else-if="rows.length === 0" data-testid="mobile-empty" class="py-12 text-center">
        <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
        <p class="text-sm surface-muted">{{ emptyLabel }}</p>
      </div>
      <section
        v-else
        v-for="(channel, chIdx) in rows"
        :key="`mobile-${channel.name}-${chIdx}`"
        class="border-b-2 border-gray-200 px-4 py-4 last:border-b-0 dark:border-dark-600"
      >
        <header class="mb-3 min-w-0">
          <h3 class="break-words text-sm font-semibold text-gray-900 dark:text-white">
            {{ channel.name }}
          </h3>
          <p class="mt-1 break-words text-xs leading-5 surface-muted">
            {{ channel.description || '-' }}
          </p>
        </header>

        <div class="divide-y divide-gray-100 dark:divide-dark-700/60">
          <div
            v-for="section in channel.platforms"
            :key="`mobile-${channel.name}-${section.platform}`"
            class="min-w-0 py-3 first:pt-0 last:pb-0"
          >
            <span
              :class="[
                'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px] font-medium uppercase',
                platformBadgeClass(section.platform),
              ]"
            >
              <PlatformIcon :platform="section.platform as GroupPlatform" size="xs" />
              {{ section.platform }}
            </span>

            <dl class="mt-3 space-y-3">
              <div class="min-w-0">
                <dt class="mb-1.5 text-[11px] font-medium surface-muted">
                  {{ columns.groups }}
                </dt>
                <dd class="flex min-w-0 flex-col gap-2">
                  <div
                    v-if="exclusiveGroups(section).length > 0"
                    class="flex min-w-0 flex-wrap items-center gap-1.5"
                  >
                    <span
                      class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase text-purple-600 dark:text-purple-400"
                      :title="t('availableChannels.exclusiveTooltip')"
                    >
                      <Icon name="shield" size="xs" class="h-3 w-3" />
                      {{ t('availableChannels.exclusive') }}
                    </span>
                    <div
                      v-for="g in exclusiveGroups(section)"
                      :key="`mobile-ex-${g.id}`"
                      class="inline-flex max-w-full min-w-0 flex-wrap items-center gap-1"
                    >
                      <GroupBadge
                        class="max-w-full"
                        :name="g.name"
                        :platform="g.platform as GroupPlatform"
                        :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                        :rate-multiplier="g.rate_multiplier"
                        :user-rate-multiplier="userGroupRates[g.id] ?? null"
                        always-show-rate
                      />
                      <span
                        v-if="hasPeakRate(g)"
                        class="inline-flex items-center gap-1 rounded-md bg-amber-50/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                        :title="peakRateTitle(g)"
                      >
                        <Icon name="clock" size="xs" class="h-3 w-3" />
                        {{ peakRateLabel(g) }}
                      </span>
                    </div>
                  </div>
                  <div
                    v-if="publicGroups(section).length > 0"
                    class="flex min-w-0 flex-wrap items-center gap-1.5"
                  >
                    <span
                      class="inline-flex items-center gap-0.5 text-[10px] font-medium uppercase surface-muted"
                      :title="t('availableChannels.publicTooltip')"
                    >
                      <Icon name="globe" size="xs" class="h-3 w-3" />
                      {{ t('availableChannels.public') }}
                    </span>
                    <div
                      v-for="g in publicGroups(section)"
                      :key="`mobile-pub-${g.id}`"
                      class="inline-flex max-w-full min-w-0 flex-wrap items-center gap-1"
                    >
                      <GroupBadge
                        class="max-w-full"
                        :name="g.name"
                        :platform="g.platform as GroupPlatform"
                        :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                        :rate-multiplier="g.rate_multiplier"
                        :user-rate-multiplier="userGroupRates[g.id] ?? null"
                        always-show-rate
                      />
                      <span
                        v-if="hasPeakRate(g)"
                        class="inline-flex items-center gap-1 rounded-md bg-amber-50/[0.15] px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                        :title="peakRateTitle(g)"
                      >
                        <Icon name="clock" size="xs" class="h-3 w-3" />
                        {{ peakRateLabel(g) }}
                      </span>
                    </div>
                  </div>
                  <span v-if="section.groups.length === 0" class="text-xs surface-muted">-</span>
                </dd>
              </div>

              <div class="min-w-0">
                <dt class="mb-1.5 text-[11px] font-medium surface-muted">
                  {{ columns.supportedModels }}
                </dt>
                <dd class="flex min-w-0 flex-wrap gap-1">
                  <SupportedModelChip
                    v-for="m in section.supported_models"
                    :key="`mobile-${section.platform}-${m.name}`"
                    class="max-w-full [&>span]:max-w-full [&>span]:truncate"
                    :model="m"
                    :pricing-key-prefix="pricingKeyPrefix"
                    :no-pricing-label="noPricingLabel"
                    :show-platform="false"
                    :platform-hint="section.platform"
                  />
                  <span v-if="section.supported_models.length === 0" class="text-xs surface-muted">
                    {{ noModelsLabel }}
                  </span>
                </dd>
              </div>
            </dl>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import SupportedModelChip from './SupportedModelChip.vue'
import type { UserAvailableChannel, UserAvailableGroup, UserChannelPlatformSection } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBadgeClass } from '@/utils/platformColors'
import { useAppStore } from '@/stores/app'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'

const props = defineProps<{
  columns: {
    name: string
    description: string
    platform: string
    groups: string
    supportedModels: string
  }
  rows: UserAvailableChannel[]
  loading: boolean
  pricingKeyPrefix: string
  noPricingLabel: string
  noModelsLabel: string
  emptyLabel: string
  /** 用户专属倍率（group_id → multiplier）；无专属时由 GroupBadge 仅显示默认倍率。 */
  userGroupRates: Record<number, number>
}>()

// Suppress unused warning — props is accessed via template automatically but
// the explicit reference here keeps the linter from flagging userGroupRates.
void props.userGroupRates

const { t } = useI18n()

function exclusiveGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => g.is_exclusive)
}

function publicGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => !g.is_exclusive)
}

const appStore = useAppStore()

function hasPeakRate(group: UserAvailableGroup): boolean {
  return groupHasPeakRate(group)
}

function peakRateLabel(group: UserAvailableGroup): string {
  return formatPeakRateWindow(group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

function peakRateTitle(group: UserAvailableGroup): string {
  return t('common.peakRateTooltip', { window: peakRateLabel(group) }) + t('common.peakRateImageNote')
}
</script>
