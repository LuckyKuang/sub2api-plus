import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

interface TurnstileRenderOptions {
  sitekey: string
  callback: (token: string) => void
  'expired-callback'?: () => void
  'error-callback'?: () => void
  theme?: 'light' | 'dark' | 'auto'
  size?: 'normal' | 'compact' | 'flexible'
}

interface TurnstileAPI {
  render: (container: HTMLElement, options: TurnstileRenderOptions) => string
  reset: (widgetId?: string) => void
  remove: (widgetId?: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI
    onTurnstileLoad?: () => void
  }
}

export interface TurnstileWidgetProps {
  siteKey: string
  theme?: 'light' | 'dark' | 'auto'
  size?: 'normal' | 'compact' | 'flexible'
  onVerify?: (token: string) => void
  onExpire?: () => void
  onError?: () => void
}

export interface TurnstileWidgetHandle {
  reset: () => void
}

function loadScript(): Promise<void> {
  return new Promise((resolve, reject) => {
    if (window.turnstile) {
      resolve()
      return
    }

    // Check if script is already loading
    const existingScript = document.querySelector('script[src*="turnstile"]')
    if (existingScript) {
      window.onTurnstileLoad = () => resolve()
      return
    }

    const script = document.createElement('script')
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?onload=onTurnstileLoad'
    script.async = true
    script.defer = true

    window.onTurnstileLoad = () => resolve()

    script.onerror = () => {
      reject(new Error('Failed to load Turnstile script'))
    }

    document.head.appendChild(script)
  })
}

export const TurnstileWidget = forwardRef<TurnstileWidgetHandle, TurnstileWidgetHandle>(
  function TurnstileWidget(
    { siteKey, theme = 'auto', size = 'flexible', onVerify, onExpire, onError },
    ref
  ) {
    const { t } = useTranslation()
    const [loading, setLoading] = useState(true)
    const containerRef = useRef<HTMLDivElement | null>(null)
    const widgetIdRef = useRef<string | null>(null)
    const scriptLoadedRef = useRef(false)
    const handlersRef = useRef({ onVerify, onExpire, onError })

    handlersRef.current = { onVerify, onExpire, onError }

    const renderWidget = () => {
      if (!window.turnstile || !containerRef.current || !siteKey) {
        return
      }

      // Remove existing widget if any
      if (widgetIdRef.current) {
        try {
          window.turnstile.remove(widgetIdRef.current)
        } catch {
          // Ignore errors when removing
        }
        widgetIdRef.current = null
      }

      // Clear container
      containerRef.current.innerHTML = ''

      widgetIdRef.current = window.turnstile.render(containerRef.current, {
        sitekey: siteKey,
        callback: (token: string) => handlersRef.current.onVerify?.(token),
        'expired-callback': () => handlersRef.current.onExpire?.(),
        'error-callback': () => handlersRef.current.onError?.(),
        theme,
        size
      })
    }

    useImperativeHandle(ref, () => ({
      reset: () => {
        if (window.turnstile && widgetIdRef.current) {
          window.turnstile.reset(widgetIdRef.current)
        }
      }
    }))

    useEffect(() => {
      if (!siteKey) {
        return
      }

      let cancelled = false
      void (async () => {
        try {
          await loadScript()
          if (cancelled) return
          scriptLoadedRef.current = true
          renderWidget()
        } catch (error) {
          console.error('Failed to initialize Turnstile:', error)
          if (!cancelled) onError?.()
        } finally {
          if (!cancelled) setLoading(false)
        }
      })()

      return () => {
        cancelled = true
        if (window.turnstile && widgetIdRef.current) {
          try {
            window.turnstile.remove(widgetIdRef.current)
          } catch {
            // Ignore errors when removing
          }
        }
        widgetIdRef.current = null
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [siteKey])

    // Re-render when siteKey changes
    useEffect(() => {
      if (siteKey && scriptLoadedRef.current) {
        renderWidget()
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [siteKey, theme, size])

    if (!siteKey) return null

    return (
      <div className="turnstile-wrapper relative">
        {loading && (
          <div
            role="status"
            className="absolute inset-0 flex items-center justify-center rounded-lg border border-gray-200 text-sm dark:border-dark-600 surface-panel surface-muted"
          >
            {t('auth.captchaLoading')}
          </div>
        )}
        <div ref={containerRef} className="turnstile-container" />
      </div>
    )
  }
)

export default TurnstileWidget
