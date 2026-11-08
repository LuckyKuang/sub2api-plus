import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useEffect } from 'react'
import { AppRouter } from '@/router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      retry: 1
    }
  }
})

export function App() {
  const siteName = useAppStore((s) => s.siteName)
  const siteLogo = useAppStore((s) => s.siteLogo)

  // Restore any persisted session exactly once. `checkAuth` schedules token
  // refreshes and kicks off a user refresh, so it must not run per render.
  useEffect(() => {
    useAuthStore.getState().checkAuth()
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <AppRouter />
    </QueryClientProvider>
  )
}

export default App
