import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PlazaNavBar } from '@/components/auth/modelPlaza/PlazaNavBar'
import { ModelPlazaContent } from '@/components/auth/modelPlaza/ModelPlazaContent'
import { useProductAppearance } from '@/components/auth/useProductAppearance'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useLocaleRefresh } from '@/utils/localeRefresh'
import '@/views/authViews.css'

export default function ModelPlazaView() {
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const [searchParams] = useSearchParams()

  useProductAppearance()

  // embedded=1 但未登录(如转发的链接)自动降级为独立形态。
  const isEmbedded = searchParams.get('embedded') === '1' && authStore.isAuthenticated()

  const [data, setData] = useState<ModelPlazaResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadFailed, setLoadFailed] = useState(false)
  const requestVersionRef = useRef(0)

  // Locale changes reload the plaza so translated server data stays coherent.
  const localeRefresh = useLocaleRefresh(loadPlaza)
  useEffect(() => localeRefresh, [localeRefresh])

  async function loadPlaza() {
    const version = ++requestVersionRef.current
    setLoading(true)
    setLoadFailed(false)
    try {
      const response = await getModelPlaza()
      if (version === requestVersionRef.current) setData(response)
    } catch {
      if (version === requestVersionRef.current) setLoadFailed(true)
    } finally {
      if (version === requestVersionRef.current) setLoading(false)
    }
  }

  useEffect(() => {
    void appStore.fetchPublicSettings()
    void loadPlaza()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 后台内嵌形态:?embedded=1 且已登录。
  // The router mounts this view inside AuthLayout, so the admin chrome (AppLayout)
  // cannot be nested here; the embedded variant keeps its own content shape.
  if (isEmbedded) {
    return <ModelPlazaContent response={data} loading={loading} error={loadFailed} embedded />
  }

  // 独立形态:自带导航条(logo/站名 + 登录/回后台)
  return (
    <div className="flex min-h-screen flex-col gap-[var(--shell-gap)] bg-canvas p-[var(--shell-gutter)] dark:surface-inset">
      <PlazaNavBar />
      <main className="mx-auto w-full min-w-0 max-w-7xl">
        <ModelPlazaContent response={data} loading={loading} error={loadFailed} />
      </main>
    </div>
  )
}
