import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

interface AliyunCaptchaVerifyResult {
  captchaResult: boolean
  bizResult?: boolean
}

interface AliyunCaptchaInitOptions {
  SceneId: string
  prefix: string
  mode: 'popup' | 'embed'
  element: string
  button: string
  captchaVerifyCallback: (
    captchaVerifyParam: string
  ) => AliyunCaptchaVerifyResult | Promise<AliyunCaptchaVerifyResult>
  onBizResultCallback: (bizResult: boolean) => void
  getInstance: (instance: unknown) => void
  slideStyle?: { width: number; height: number }
  language?: string
}

declare global {
  interface Window {
    initAliyunCaptcha?: (options: AliyunCaptchaInitOptions) => void
    AliyunCaptchaConfig?: { region: string; prefix: string }
  }
}

export interface AliyunCaptchaWidgetProps {
  sceneId: string
  prefix: string
  region?: 'cn' | 'sgp'
  onVerify?: (param: string) => void
  onExpire?: () => void
  onError?: () => void
}

export interface AliyunCaptchaWidgetHandle {
  verify: () => Promise<string | null>
  reset: () => void
}

const SCRIPT_SRC = 'https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js'
const POPUP_ID = 'aliyunCaptcha-window-popup'
const MASK_ID = 'aliyunCaptcha-mask'
const POPUP_OPEN_TIMEOUT_MS = 8000
const POPUP_WATCH_INTERVAL_MS = 300

export const AliyunCaptchaWidget = forwardRef<AliyunCaptchaWidgetHandle, AliyunCaptchaWidgetProps>(
  function AliyunCaptchaWidget({ sceneId, prefix, region = 'cn', onVerify, onExpire, onError }, ref) {
    const { t, i18n } = useTranslation()
    const uid = Math.random().toString(36).slice(2, 10)
    const buttonId = `aliyun-captcha-button-${uid}`
    const elementId = `aliyun-captcha-element-${uid}`

    // idle: 未验证可点击；verifying: 弹窗已拉起（关闭后回到 idle 可重试）；verified: 已通过
    const [state, setState] = useState<'idle' | 'verifying' | 'verified'>('idle')

    // captchaVerifyParam 是一次性参数：verified 后缓存于此，提交失败需 reset 后重新验证
    const cachedParamRef = useRef<string | null>(null)
    const pendingRef = useRef<{ resolve: (value: string | null) => void } | null>(null)
    const popupWatchTimerRef = useRef<number | null>(null)
    const readyPromiseRef = useRef<Promise<void> | null>(null)
    const stateRef = useRef(state)
    useEffect(() => {
      stateRef.current = state
    }, [state])

    const handlersRef = useRef({ onVerify, onExpire, onError })
    handlersRef.current = { onVerify, onExpire, onError }

    const buttonText = (() => {
      switch (state) {
        case 'verified':
          return t('auth.captchaVerified')
        case 'verifying':
          return t('auth.captchaVerifying')
        default:
          return t('auth.captchaClickToVerify')
      }
    })()

    const loadScript = (): Promise<void> =>
      new Promise((resolve, reject) => {
        // 全局配置必须在脚本加载前就位（region/prefix 全站一致，重复赋值无副作用）
        window.AliyunCaptchaConfig = { region, prefix }

        if (window.initAliyunCaptcha) {
          resolve()
          return
        }

        const existingScript = document.querySelector<HTMLScriptElement>(
          'script[src*="aliyunCaptcha/AliyunCaptcha"]'
        )
        if (existingScript) {
          existingScript.addEventListener('load', () => resolve())
          existingScript.addEventListener('error', () =>
            reject(new Error('Failed to load Aliyun captcha script'))
          )
          return
        }

        const script = document.createElement('script')
        script.src = SCRIPT_SRC
        script.async = true
        script.onload = () => resolve()
        script.onerror = () => reject(new Error('Failed to load Aliyun captcha script'))
        document.head.appendChild(script)
      })

    const initCaptcha = (): void => {
      if (!window.initAliyunCaptcha) {
        throw new Error('Aliyun captcha script not ready')
      }
      window.initAliyunCaptcha({
        SceneId: sceneId,
        prefix,
        mode: 'popup',
        element: `#${elementId}`,
        button: `#${buttonId}`,
        // 这里不发业务请求，只把 captchaVerifyParam 当 token 交给页面，随登录/注册等
        // 业务请求的 turnstile_token 字段提交，由后端在业务接口内调阿里云校验。
        captchaVerifyCallback: (captchaVerifyParam: string) => {
          onCaptchaParam(captchaVerifyParam)
          return { captchaResult: true }
        },
        onBizResultCallback: () => {},
        getInstance: () => {},
        slideStyle: { width: 360, height: 40 },
        language: i18n.language.toLowerCase().startsWith('zh') ? 'cn' : 'en'
      })
    }

    const ensureReady = (): Promise<void> => {
      if (!readyPromiseRef.current) {
        readyPromiseRef.current = loadScript().then(() => initCaptcha())
        readyPromiseRef.current.catch(() => {
          // 失败后允许下次重试（如网络恢复）
          readyPromiseRef.current = null
        })
      }
      return readyPromiseRef.current
    }

    function onCaptchaParam(param: string): void {
      stopPopupWatch()
      cachedParamRef.current = param
      setState('verified')
      handlersRef.current.onVerify?.(param)
      const current = pendingRef.current
      pendingRef.current = null
      current?.resolve(param)
    }

    function settlePending(value: string | null): void {
      const current = pendingRef.current
      pendingRef.current = null
      current?.resolve(value)
    }

    function isPopupVisible(): boolean {
      const popup = document.getElementById(POPUP_ID)
      if (!popup) return false
      return window.getComputedStyle(popup).display !== 'none'
    }

    function stopPopupWatch(): void {
      if (popupWatchTimerRef.current !== null) {
        window.clearInterval(popupWatchTimerRef.current)
        popupWatchTimerRef.current = null
      }
    }

    // SDK 没有用户关闭弹窗的回调，靠轮询弹窗可见性兜底：
    // 弹窗出现过又消失且未产生 param → 用户主动关闭；迟迟未出现 → 打开失败。
    // initAliyunCaptcha 对触发按钮的事件绑定是异步完成的，首次 click 可能落空，
    // 因此弹窗出现前每个 tick 重试触发一次。
    function startPopupWatch(): void {
      stopPopupWatch()
      const startedAt = Date.now()
      let seen = false
      popupWatchTimerRef.current = window.setInterval(() => {
        if (stateRef.current === 'verified') {
          stopPopupWatch()
          return
        }
        if (isPopupVisible()) {
          seen = true
          return
        }
        if (seen || Date.now() - startedAt > POPUP_OPEN_TIMEOUT_MS) {
          stopPopupWatch()
          setState('idle')
          settlePending(null)
          return
        }
        document.getElementById(buttonId)?.click()
      }, POPUP_WATCH_INTERVAL_MS)
    }

    // 用户点击与程序化触发共用：置 verifying 并启动弹窗监视（幂等，重试 click 不重置计时）
    function handleTriggerClick(): void {
      if (stateRef.current === 'verified') {
        return
      }
      setState('verifying')
      if (popupWatchTimerRef.current === null) {
        startPopupWatch()
      }
    }

    // 程序化触发验证（OAuth 启动、passkey、未预验证时的表单提交兜底）：
    // 已通过预验证则直接复用缓存的 captchaVerifyParam；否则弹出验证码等待结果。
    // 用户关闭/未能弹出 resolve null；脚本加载失败 reject。
    async function verify(): Promise<string | null> {
      if (stateRef.current === 'verified' && cachedParamRef.current) {
        return cachedParamRef.current
      }
      settlePending(null)
      await ensureReady()
      return new Promise<string | null>((resolve) => {
        pendingRef.current = { resolve }
        document.getElementById(buttonId)?.click()
      })
    }

    // 重置为未验证态；captchaVerifyParam 是一次性参数，服务端校验失败后需用户重新验证
    function reset(): void {
      stopPopupWatch()
      settlePending(null)
      cachedParamRef.current = null
      setState('idle')
    }

    useImperativeHandle(ref, () => ({ verify, reset }))

    useEffect(() => {
      if (!sceneId || !prefix) {
        return
      }

      const button = document.getElementById(buttonId)
      button?.addEventListener('click', handleTriggerClick)
      void ensureReady().catch((error) => {
        console.error('Failed to initialize Aliyun captcha:', error)
        handlersRef.current.onError?.()
      })

      return () => {
        document.getElementById(buttonId)?.removeEventListener('click', handleTriggerClick)
        stopPopupWatch()
        settlePending(null)
        // SDK 不会自清理弹窗 DOM，残留会导致下次挂载时回调重复触发
        document.getElementById(MASK_ID)?.remove()
        document.getElementById(POPUP_ID)?.remove()
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [sceneId, prefix])

    if (!sceneId || !prefix) return null

    return (
      <div className="aliyun-captcha-wrapper">
        <button
          id={buttonId}
          type="button"
          className={cn('aliyun-captcha-button surface-control', state === 'verified' && 'aliyun-captcha-button--verified')}
          disabled={state === 'verified'}
        >
          {state === 'verified' ? (
            <svg className="aliyun-captcha-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path
                fillRule="evenodd"
                d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z"
                clipRule="evenodd"
              />
            </svg>
          ) : (
            <svg className="aliyun-captcha-icon" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path
                fillRule="evenodd"
                d="M9.661 2.237a.531.531 0 01.678 0 11.947 11.947 0 007.078 2.749.5.5 0 01.479.425c.069.52.104 1.05.104 1.59 0 5.162-3.26 9.563-7.834 11.256a.48.48 0 01-.332 0C5.26 16.564 2 12.163 2 7c0-.538.035-1.069.104-1.589a.5.5 0 01.48-.425 11.947 11.947 0 007.077-2.75z"
                clipRule="evenodd"
              />
            </svg>
          )}
          <span>{buttonText}</span>
        </button>
        <div id={elementId} />
      </div>
    )
  }
)

export default AliyunCaptchaWidget
