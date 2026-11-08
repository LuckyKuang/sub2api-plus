import { useEffect, useRef } from 'react'
import { driver, type Driver, type DriveStep } from 'driver.js'
import 'driver.js/dist/driver.css'
import { useTranslation } from 'react-i18next'
import { getAdminSteps, getUserSteps } from '@/components/Guide/steps'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingStore } from '@/stores/onboarding'

export interface OnboardingOptions {
  storageKey?: string
  autoStart?: boolean
}

interface CurrentClickListener {
  element: HTMLElement
  handler: () => void
  keyHandler?: (e: KeyboardEvent) => void
  originalTabIndex?: string | null
  eventTypes?: string[]
}

const STORAGE_VERSION = 'v4_interactive'

const TIMING = {
  INTERACTIVE_WAIT_MS: 800,
  ELEMENT_TIMEOUT_MS: 8000,
  AUTO_START_DELAY_MS: 1000
} as const

const CLASS_REORGANIZED = 'reorganized'
const CLASS_FOOTER_LEFT = 'footer-left'
const CLASS_FOOTER_RIGHT = 'footer-right'
const CLASS_DONE_BTN = 'driver-popover-done-btn'
const CLASS_PROGRESS_TEXT = 'driver-popover-progress-text'
const CLASS_NEXT_BTN = 'driver-popover-next-btn'
const CLASS_PREV_BTN = 'driver-popover-previous-btn'

const INPUT_TAGS = ['INPUT', 'TEXTAREA', 'SELECT']

function isInteractiveStep(step: DriveStep): boolean {
  return step.popover?.showButtons?.length === 1 && step.popover.showButtons[0] === 'close'
}

function resolveStepElement(selector: string): HTMLElement | null {
  return document.querySelector(selector) as HTMLElement | null
}

/**
 * Interactive product tour. Ported from the Vue composable: the driver instance
 * and its step listeners live for the lifetime of the mounted layout, and the
 * tour auto-starts once for an administrator on the standard run mode.
 */
export function useOnboardingTour(options: OnboardingOptions) {
  const { t } = useTranslation()
  const userStore = useAuthStore()
  const onboardingStore = useOnboardingStore()

  const driverRef = useRef<Driver | null>(null)
  const listenerRef = useRef<CurrentClickListener | null>(null)
  const autoStartTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const keyboardHandlerRef = useRef<((e: KeyboardEvent) => void) | null>(null)

  // Callbacks recreated each render; the effects below read them through refs so
  // they never need to re-run.
  const latest = useRef({ t, userStore, onboardingStore, options })
  latest.current = { t, userStore, onboardingStore, options }

  const getStorageKey = () => {
    const { storageKey = 'onboarding_tour' } = latest.current.options
    const userId = latest.current.userStore.user?.id ?? 'guest'
    const role = latest.current.userStore.user?.role ?? 'user'
    return `${storageKey}_${userId}_${role}_${STORAGE_VERSION}`
  }

  const hasSeen = () => localStorage.getItem(getStorageKey()) === 'true'
  const markAsSeen = () => localStorage.setItem(getStorageKey(), 'true')
  const clearSeen = () => localStorage.removeItem(getStorageKey())

  const ensureElement = async (selector: string, timeout = 5000): Promise<boolean> => {
    const startTime = Date.now()
    while (Date.now() - startTime < timeout) {
      const element = document.querySelector(selector)
      if (element && element.getBoundingClientRect().height > 0) return true
      await new Promise((resolve) => setTimeout(resolve, 150))
    }
    return false
  }

  const cleanupClickListener = () => {
    const current = listenerRef.current
    if (!current) return
    const { element, handler, keyHandler, originalTabIndex, eventTypes } = current
    if (eventTypes) eventTypes.forEach((type) => element.removeEventListener(type, handler))
    if (keyHandler) element.removeEventListener('keydown', keyHandler)
    if (originalTabIndex !== undefined) {
      if (originalTabIndex === null) element.removeAttribute('tabindex')
      else element.setAttribute('tabindex', originalTabIndex)
    }
    listenerRef.current = null
  }

  const startTour = async (startIndex = 0) => {
    const { t: translate, userStore: auth, onboardingStore: store } = latest.current
    const isAdmin = auth.user?.role === 'admin'
    const steps = isAdmin ? getAdminSteps(translate, auth.runMode === 'simple') : getUserSteps(translate)

    const currentStep = steps[startIndex]
    if (currentStep?.element && typeof currentStep.element === 'string') {
      await ensureElement(currentStep.element, TIMING.ELEMENT_TIMEOUT_MS)
    }

    if (driverRef.current) driverRef.current.destroy()

    const instance = driver({
      showProgress: true,
      steps,
      animate: true,
      allowClose: false,
      stagePadding: 4,
      popoverClass: 'theme-tour-popover',
      nextBtnText: translate('common.next'),
      prevBtnText: translate('common.back'),
      doneBtnText: translate('common.confirm'),

      onNextClick: async (_el, _step, { config, state }) => {
        if (state.activeIndex === (config.steps?.length ?? 0) - 1) {
          markAsSeen()
          driverRef.current?.destroy()
          store.setDriverInstance(null)
          return
        }
        // Interactive steps hide Next; this is defensive only.
        const index = state.activeIndex ?? 0
        const step = steps[index]
        if (step && isInteractiveStep(step) && step.element) {
          const target =
            typeof step.element === 'string' ? resolveStepElement(step.element) : (step.element as HTMLElement)
          if (target && !INPUT_TAGS.includes(target.tagName)) {
            target.click()
            return
          }
        }
        driverRef.current?.moveNext()
      },
      onPrevClick: () => {
        driverRef.current?.movePrevious()
      },
      onCloseClick: () => {
        markAsSeen()
        driverRef.current?.destroy()
        store.setDriverInstance(null)
      },

      onPopoverRender: (popover, { config, state }) => {
        try {
          const { title: titleEl, footer: footerEl, nextButton, previousButton } = popover
          if (!titleEl || !footerEl) {
            console.warn('Onboarding: Missing popover elements')
            return
          }

          const step = steps[state.activeIndex ?? 0]
          if (step && isInteractiveStep(step) && popover.description) {
            const hintClass = 'driver-popover-description-hint'
            if (!popover.description.querySelector(`.${hintClass}`)) {
              const hint = document.createElement('div')
              hint.className = `${hintClass} mt-2 text-xs text-gray-500 flex items-center gap-1`

              const iconSpan = document.createElement('span')
              iconSpan.className = 'i-mdi-keyboard-return mr-1'

              hint.appendChild(iconSpan)
              hint.appendChild(
                document.createTextNode(translate('onboarding.interactiveHint', 'Press Enter or Click to continue'))
              )
              popover.description.appendChild(hint)
            }
          }

          if (!footerEl.classList.contains(CLASS_REORGANIZED)) {
            footerEl.classList.add(CLASS_REORGANIZED)

            const progressEl = footerEl.querySelector(`.${CLASS_PROGRESS_TEXT}`)
            const nextBtnEl = nextButton || footerEl.querySelector(`.${CLASS_NEXT_BTN}`)
            const prevBtnEl = previousButton || footerEl.querySelector(`.${CLASS_PREV_BTN}`)

            const leftContainer = document.createElement('div')
            leftContainer.className = CLASS_FOOTER_LEFT

            const rightContainer = document.createElement('div')
            rightContainer.className = CLASS_FOOTER_RIGHT

            if (progressEl) leftContainer.appendChild(progressEl)

            const shortcutsEl = document.createElement('div')
            shortcutsEl.className = 'footer-shortcuts'

            const shortcut1 = document.createElement('span')
            shortcut1.className = 'shortcut-item'
            const kbd1 = document.createElement('kbd')
            kbd1.textContent = '←'
            const kbd2 = document.createElement('kbd')
            kbd2.textContent = '→'
            shortcut1.appendChild(kbd1)
            shortcut1.appendChild(kbd2)
            shortcut1.appendChild(document.createTextNode(` ${translate('onboarding.navigation.flipPage')}`))

            const shortcut2 = document.createElement('span')
            shortcut2.className = 'shortcut-item'
            const kbd3 = document.createElement('kbd')
            kbd3.textContent = 'ESC'
            shortcut2.appendChild(kbd3)
            shortcut2.appendChild(document.createTextNode(` ${translate('onboarding.navigation.exit')}`))

            shortcutsEl.appendChild(shortcut1)
            shortcutsEl.appendChild(shortcut2)
            leftContainer.appendChild(shortcutsEl)

            if (prevBtnEl) rightContainer.appendChild(prevBtnEl)
            if (nextBtnEl) rightContainer.appendChild(nextBtnEl)

            footerEl.innerHTML = ''
            footerEl.appendChild(leftContainer)
            footerEl.appendChild(rightContainer)
          }

          const isLastStep = state.activeIndex === (config.steps?.length ?? 0) - 1
          const activeNextBtn = nextButton || footerEl.querySelector(`.${CLASS_NEXT_BTN}`)
          if (activeNextBtn) {
            if (isLastStep) activeNextBtn.classList.add(CLASS_DONE_BTN)
            else activeNextBtn.classList.remove(CLASS_DONE_BTN)
          }
        } catch (e) {
          console.error('Onboarding Tour Render Error:', e)
        }
      },

      onHighlightStarted: async (element, step) => {
        cleanupClickListener()

        if (!element && step.element && typeof step.element === 'string') {
          const exists = await ensureElement(step.element, TIMING.ELEMENT_TIMEOUT_MS)
          if (!exists) {
            console.warn(`Tour element not found after 8s: ${step.element}`)
            return
          }
          element = resolveStepElement(step.element) ?? undefined
        }

        if (!isInteractiveStep(step) || !element) return

        const htmlElement = element as HTMLElement

        // Submit buttons let business code advance the tour after it succeeds.
        const isSubmitButton =
          htmlElement.getAttribute('type') === 'submit' ||
          (htmlElement.tagName === 'BUTTON' && !!htmlElement.closest('form'))
        if (isSubmitButton) return

        const originalTabIndex = htmlElement.getAttribute('tabindex')
        if (!htmlElement.isContentEditable && htmlElement.tabIndex === -1) {
          htmlElement.setAttribute('tabindex', '0')
        }

        // Select dropdowns teleport to <body>, so option clicks never bubble here.
        const isSelectComponent =
          htmlElement.querySelector('.select-trigger') !== null ||
          htmlElement.classList.contains('select-trigger')
        if (isSelectComponent) return

        let hasExecuted = false
        const boundStepIndex = driverRef.current?.getActiveIndex() ?? 0

        const clickHandler = async () => {
          if (hasExecuted) return
          hasExecuted = true

          await new Promise((resolve) => setTimeout(resolve, TIMING.INTERACTIVE_WAIT_MS))

          if (!driverRef.current || !driverRef.current.isActive()) return

          const currentIndex = driverRef.current.getActiveIndex() ?? 0
          if (currentIndex !== boundStepIndex) return

          const next = steps[currentIndex + 1]
          if (next?.element && typeof next.element === 'string') {
            const exists = await ensureElement(next.element, TIMING.ELEMENT_TIMEOUT_MS)
            if (!exists) {
              console.warn(`Onboarding: Next step element not found: ${next.element}`)
              return
            }
          }

          if (driverRef.current && driverRef.current.isActive()) driverRef.current.moveNext()
        }

        if (INPUT_TAGS.includes(htmlElement.tagName)) {
          const inputHandler = () => {
            htmlElement.removeEventListener('input', inputHandler)
            htmlElement.removeEventListener('change', inputHandler)
            void clickHandler()
          }
          htmlElement.addEventListener('input', inputHandler)
          htmlElement.addEventListener('change', inputHandler)
          listenerRef.current = {
            element: htmlElement,
            handler: inputHandler,
            originalTabIndex,
            eventTypes: ['input', 'change']
          }
        } else {
          const keyHandler = (e: KeyboardEvent) => {
            if (['Enter', ' '].includes(e.key)) {
              e.preventDefault()
              void clickHandler()
            }
          }
          htmlElement.addEventListener('click', clickHandler, { once: true })
          htmlElement.addEventListener('keydown', keyHandler)
          listenerRef.current = {
            element: htmlElement,
            handler: clickHandler as () => void,
            keyHandler,
            originalTabIndex,
            eventTypes: ['click']
          }
        }
      },

      onDestroyed: () => {
        cleanupClickListener()
        if (keyboardHandlerRef.current) {
          document.removeEventListener('keydown', keyboardHandlerRef.current, { capture: true })
          keyboardHandlerRef.current = null
        }
        store.setDriverInstance(null)
      }
    })

    driverRef.current = instance
    store.setDriverInstance(instance)

    keyboardHandlerRef.current = (e: KeyboardEvent) => {
      const active = driverRef.current
      if (!active?.isActive()) return

      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        markAsSeen()
        active.destroy()
        store.setDriverInstance(null)
        return
      }

      if (e.key === 'ArrowRight') {
        const target = e.target as HTMLElement
        if (['INPUT', 'TEXTAREA'].includes(target?.tagName)) return

        e.preventDefault()
        e.stopPropagation()

        // On interactive steps the arrow key must not skip the interaction.
        const index = active.getActiveIndex() ?? 0
        const step = steps[index]
        if (step && isInteractiveStep(step) && step.element) {
          const targetElement =
            typeof step.element === 'string' ? resolveStepElement(step.element) : (step.element as HTMLElement)
          if (targetElement && !INPUT_TAGS.includes(targetElement.tagName)) return
        }

        active.moveNext()
      } else if (e.key === 'Enter') {
        const target = e.target as HTMLElement
        if (['INPUT', 'TEXTAREA'].includes(target?.tagName)) return

        e.preventDefault()
        e.stopPropagation()

        const index = active.getActiveIndex() ?? 0
        const step = steps[index]
        if (step && isInteractiveStep(step) && step.element) {
          const targetElement =
            typeof step.element === 'string' ? resolveStepElement(step.element) : (step.element as HTMLElement)
          if (targetElement && !INPUT_TAGS.includes(targetElement.tagName)) {
            targetElement.click()
            return
          }
        }
        active.moveNext()
      } else if (e.key === 'ArrowLeft') {
        const target = e.target as HTMLElement
        if (INPUT_TAGS.includes(target?.tagName) || target?.isContentEditable) return

        e.preventDefault()
        e.stopPropagation()
        active.movePrevious()
      }
    }

    document.addEventListener('keydown', keyboardHandlerRef.current, { capture: true })
    instance.drive(startIndex)
  }

  const nextStep = async (delay = 300) => {
    if (!driverRef.current?.isActive()) return
    if (delay > 0) await new Promise((resolve) => setTimeout(resolve, delay))
    driverRef.current.moveNext()
  }

  const isCurrentStep = (elementSelector: string): boolean => {
    if (!driverRef.current?.isActive()) return false
    return driverRef.current.getActiveElement()?.matches(elementSelector) ?? false
  }

  const replayTour = () => {
    clearSeen()
    void startTour()
  }

  useEffect(() => {
    onboardingStore.setControlMethods({ nextStep, isCurrentStep })
    return () => onboardingStore.clearControlMethods()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const auth = latest.current.userStore
    const store = latest.current.onboardingStore

    if (store.isDriverActive()) {
      driverRef.current = store.getDriverInstance()
      return
    }

    if (auth.runMode === 'simple') return
    if (auth.user?.role !== 'admin') return
    if (!latest.current.options.autoStart || hasSeen()) return

    autoStartTimerRef.current = setTimeout(() => {
      void startTour()
    }, TIMING.AUTO_START_DELAY_MS)

    return () => {
      if (autoStartTimerRef.current) {
        clearTimeout(autoStartTimerRef.current)
        autoStartTimerRef.current = null
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return {
    startTour,
    replayTour,
    nextStep,
    isCurrentStep,
    hasSeen,
    markAsSeen,
    clearSeen
  }
}

export default useOnboardingTour
