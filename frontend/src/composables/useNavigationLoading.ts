import { useSyncExternalStore } from 'react'

/**
 * 导航加载状态管理
 *
 * 功能：
 * 1. 在路由切换时显示加载状态
 * 2. 快速导航（< 100ms）不显示加载指示器（防闪烁）
 * 3. 导航取消时正确重置状态
 */

// 防闪烁延迟时间（毫秒）
export const ANTI_FLICKER_DELAY = 100

let isLoading = false
let navigationStartTime: number | null = null
let showLoadingTimer: ReturnType<typeof setTimeout> | null = null
let shouldShowLoading = false

const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/**
 * 清理计时器
 */
function clearTimer(): void {
  if (showLoadingTimer !== null) {
    clearTimeout(showLoadingTimer)
    showLoadingTimer = null
  }
}

/**
 * 导航开始时调用
 */
export function startNavigation(): void {
  navigationStartTime = Date.now()
  isLoading = true

  // 延迟显示加载指示器，实现防闪烁
  clearTimer()
  showLoadingTimer = setTimeout(() => {
    if (isLoading) {
      shouldShowLoading = true
      emit()
    }
  }, ANTI_FLICKER_DELAY)
  emit()
}

/**
 * 导航结束时调用
 */
export function endNavigation(): void {
  clearTimer()
  isLoading = false
  shouldShowLoading = false
  navigationStartTime = null
  emit()
}

/**
 * 导航取消时调用（比如快速连续点击不同链接）
 */
export function cancelNavigation(): void {
  clearTimer()
  // 保持加载状态，因为新的导航会立即开始
  // 但重置导航开始时间
  navigationStartTime = null
  emit()
}

/**
 * 重置所有状态（用于测试）
 */
export function resetState(): void {
  clearTimer()
  isLoading = false
  shouldShowLoading = false
  navigationStartTime = null
  emit()
}

/**
 * 获取导航持续时间（毫秒）
 */
export function getNavigationDuration(): number | null {
  if (navigationStartTime === null) {
    return null
  }
  return Date.now() - navigationStartTime
}

export function useNavigationLoading() {
  return {
    isLoading: useSyncExternalStore(subscribe, () => shouldShowLoading, () => shouldShowLoading),
    isNavigating: useSyncExternalStore(subscribe, () => isLoading, () => isLoading),
    startNavigation,
    endNavigation,
    cancelNavigation,
    resetState,
    getNavigationDuration,
    // 导出常量用于测试
    ANTI_FLICKER_DELAY
  }
}

// 单例实例，供全局使用（与 Vue 的 navigationLoadingInstance 对应）
export function useNavigationLoadingState() {
  return useNavigationLoading()
}

// 导出重置函数（用于测试）
export function _resetNavigationLoadingInstance(): void {
  resetState()
}
