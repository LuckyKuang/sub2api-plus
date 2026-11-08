import { create } from 'zustand'
import type { Driver } from 'driver.js'

/**
 * Onboarding Store
 * Manages onboarding tour state and control methods
 */

type VoidCallback = () => void
type NextStepCallback = (delay?: number) => Promise<void>
type IsCurrentStepCallback = (selector: string) => boolean

interface OnboardingState {
  setReplayCallback: (callback: VoidCallback | null) => void
  setControlMethods: (methods: {
    nextStep: NextStepCallback,
    isCurrentStep: IsCurrentStepCallback
  }) => void
  clearControlMethods: () => void
  setDriverInstance: (driver: Driver | null) => void
  getDriverInstance: () => Driver | null
  isDriverActive: () => boolean
  replay: () => void
  nextStep: (delay?: number) => Promise<void>
  isCurrentStep: (selector: string) => boolean
}

export const useOnboardingStore = create<OnboardingState>()(() => {
  // Callbacks registered by views; module-level like the Vue refs they replace.
  let replayCallback: VoidCallback | null = null
  let nextStepCallback: NextStepCallback | null = null
  let isCurrentStepCallback: IsCurrentStepCallback | null = null

  // 全局 driver 实例，跨组件保持
  let driverInstance: Driver | null = null

  function setReplayCallback(callback: VoidCallback | null): void {
    replayCallback = callback
  }

  function setControlMethods(methods: {
    nextStep: NextStepCallback,
    isCurrentStep: IsCurrentStepCallback
  }): void {
    nextStepCallback = methods.nextStep
    isCurrentStepCallback = methods.isCurrentStep
  }

  function clearControlMethods(): void {
    nextStepCallback = null
    isCurrentStepCallback = null
  }

  function setDriverInstance(driver: Driver | null): void {
    driverInstance = driver
  }

  function getDriverInstance(): Driver | null {
    return driverInstance
  }

  function isDriverActive(): boolean {
    return driverInstance?.isActive?.() ?? false
  }

  function replay(): void {
    if (replayCallback) {
      replayCallback()
    }
  }

  /**
   * Manually advance to the next step
   * @param delay Optional delay in ms (useful for waiting for animations)
   */
  async function nextStep(delay = 0): Promise<void> {
    if (nextStepCallback) {
      await nextStepCallback(delay)
    }
  }

  /**
   * Check if the tour is currently highlighting a specific element
   */
  function isCurrentStep(selector: string): boolean {
    if (isCurrentStepCallback) {
      return isCurrentStepCallback(selector)
    }
    return false
  }

  return {
    setReplayCallback,
    setControlMethods,
    clearControlMethods,
    setDriverInstance,
    getDriverInstance,
    isDriverActive,
    replay,
    nextStep,
    isCurrentStep
  }
})
