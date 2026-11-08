import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
  const appStore = useAppStore()
  const authStore = useAuthStore()

  // Restore the injected public settings before the first render so branding
  // and feature flags never flash a default state.
  appStore.initFromInjectedConfig()
  authStore.checkAuth()

  return (
    <QueryClientProvider client={queryClient}>
      <AppRouter />
    </QueryClientProvider>
  )
}

export default App
