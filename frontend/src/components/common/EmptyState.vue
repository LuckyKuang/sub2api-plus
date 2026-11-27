<template>
  <div :class="['empty-state', sizeClass]">
    <!-- Icon -->
    <div :class="['flex items-center justify-center surface-subcard', iconBoxClass]">
      <slot name="icon">
        <component v-if="icon" :is="icon" :class="['empty-state-icon', iconSizeClass]" aria-hidden="true" />
        <svg
          v-else
          :class="['empty-state-icon', iconSizeClass]"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          stroke-width="1.5"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"
          />
        </svg>
      </slot>
    </div>

    <!-- Title -->
    <h3 class="empty-state-title">
      {{ displayTitle }}
    </h3>

    <!-- Description -->
    <p v-if="displayDescription" class="empty-state-description">
      {{ displayDescription }}
    </p>

    <!-- Action -->
    <div v-if="actionText || $slots.action" :class="size === 'sm' ? 'mt-4' : 'mt-5'">
      <slot name="action">
        <component
          :is="actionTo ? 'RouterLink' : 'button'"
          v-if="actionText"
          :to="actionTo"
          @click="!actionTo && $emit('action')"
          :class="['btn btn-primary', size === 'sm' && 'btn-sm']"
        >
          <Icon v-if="actionIcon" name="plus" :size="size === 'sm' ? 'sm' : 'md'" class="mr-1.5" />
          {{ actionText }}
        </component>
      </slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Component } from 'vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

interface Props {
  icon?: Component | string
  title?: string
  description?: string
  actionText?: string
  actionTo?: string | object
  actionIcon?: boolean
  message?: string
  size?: 'sm' | 'md' | 'lg'
}

const props = withDefaults(defineProps<Props>(), {
  description: '',
  actionIcon: true,
  size: 'md'
})

const displayTitle = computed(() => props.title || t('common.noData'))
const displayDescription = computed(() => props.description || props.message || '')
const sizeClass = computed(() => (props.size === 'md' ? '' : `empty-state--${props.size}`))
const iconBoxClass = computed(() => {
  if (props.size === 'sm') return 'mb-3 h-11 w-11 rounded-lg'
  if (props.size === 'lg') return 'mb-5 h-16 w-16 rounded-xl'
  return 'mb-4 h-14 w-14 rounded-xl'
})
const iconSizeClass = computed(() => {
  if (props.size === 'sm') return 'h-5 w-5'
  if (props.size === 'lg') return 'h-8 w-8'
  return 'h-7 w-7'
})

defineEmits(['action'])
</script>
