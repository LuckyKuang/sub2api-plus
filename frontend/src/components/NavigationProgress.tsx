import { cn } from '@/lib/utils'
import { useNavigationLoadingState } from '@/composables/useNavigationLoading'

export interface NavigationProgressProps {
  className?: string
}

/**
 * 导航进度条组件
 * 在页面顶部显示加载进度，提供导航反馈
 */
export function NavigationProgress({ className }: NavigationProgressProps) {
  const { isLoading } = useNavigationLoadingState()

  // 进度条可见性
  const isVisible = isLoading

  if (!isVisible) return null

  return (
    <div
      className={cn('navigation-progress', className)}
      role="progressbar"
      aria-label="Loading"
      aria-valuenow={0}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div className="navigation-progress-bar" />
    </div>
  )
}
