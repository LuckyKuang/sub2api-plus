import { Outlet } from 'react-router-dom'
import { Suspense } from 'react'
import { Toaster, LoadingState } from '@/components/ui'

export function AuthLayout() {
  return (
    <div className="min-h-screen bg-canvas text-gray-900 dark:bg-dark-950 dark:text-gray-100">
      <Suspense fallback={<LoadingState className="min-h-screen" />}>
        <Outlet />
      </Suspense>
      <Toaster />
    </div>
  )
}

export default AuthLayout
