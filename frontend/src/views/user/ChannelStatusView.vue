<template>
  <ChannelStatusV1View v-if="enabled && mode === 'v1'" />
  <ChannelStatusV2View v-else-if="enabled && mode === 'v2'" />
  <ChannelStatusV3View v-else-if="enabled && mode === 'v3'" />
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { getChannelMonitorMode, isChannelMonitorRouteEnabled } from '@/utils/featureFlags'
import { useAppStore } from '@/stores/app'
import ChannelStatusV1View from './ChannelStatusV1View.vue'
import ChannelStatusV2View from './ChannelStatusV2View.vue'
import ChannelStatusV3View from './ChannelStatusV3View.vue'
const app = useAppStore()
const mode = computed(getChannelMonitorMode)
const enabled = computed(isChannelMonitorRouteEnabled)

function refreshSavedMode() {
  if (document.visibilityState === 'visible') void app.fetchPublicSettings(true)
}

onMounted(() => {
  window.addEventListener('focus', refreshSavedMode)
  document.addEventListener('visibilitychange', refreshSavedMode)
})
onUnmounted(() => {
  window.removeEventListener('focus', refreshSavedMode)
  document.removeEventListener('visibilitychange', refreshSavedMode)
})
</script>
