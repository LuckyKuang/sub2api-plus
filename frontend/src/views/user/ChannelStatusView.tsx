import { useEffect } from 'react'
import { getChannelMonitorMode, isChannelMonitorRouteEnabled } from '@/utils/featureFlags'
import { useAppStore } from '@/stores/app'
import ChannelStatusV1View from './ChannelStatusV1View'
import ChannelStatusV2View from './ChannelStatusV2View'
import ChannelStatusV3View from './ChannelStatusV3View'

export default function ChannelStatusView() {
  const appStore = useAppStore()
  const settings = appStore.cachedPublicSettings
  const mode = getChannelMonitorMode()
  const enabled = isChannelMonitorRouteEnabled()

  useEffect(() => {
    const refreshSavedMode = () => {
      if (document.visibilityState === 'visible') void useAppStore.getState().fetchPublicSettings(true)
    }
    window.addEventListener('focus', refreshSavedMode)
    document.addEventListener('visibilitychange', refreshSavedMode)
    return () => {
      window.removeEventListener('focus', refreshSavedMode)
      document.removeEventListener('visibilitychange', refreshSavedMode)
    }
  }, [])

  if (!enabled) return null
  if (mode === 'v1') return <ChannelStatusV1View />
  if (mode === 'v2') return <ChannelStatusV2View />
  if (mode === 'v3') return <ChannelStatusV3View />
  return null
}
