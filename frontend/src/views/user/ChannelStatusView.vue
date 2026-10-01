<template>
  <ChannelStatusV1View v-if="enabled && mode === 'v1'" />
  <ChannelStatusV2View v-else-if="enabled && mode === 'v2' && auth.isAdmin" />
  <ChannelStatusV3View v-else-if="enabled && mode === 'v3'" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { getChannelMonitorMode, isChannelMonitorRouteEnabled } from '@/utils/featureFlags'
import { useAuthStore } from '@/stores/auth'
import ChannelStatusV1View from './ChannelStatusV1View.vue'
import ChannelStatusV2View from './ChannelStatusV2View.vue'
import ChannelStatusV3View from './ChannelStatusV3View.vue'
const auth = useAuthStore()
const mode = computed(getChannelMonitorMode)
const enabled = computed(isChannelMonitorRouteEnabled)
</script>
