import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  loadTencentCaptcha,
  normalizeTencentCaptchaRegion,
  type TencentCaptchaProof,
  type TencentCaptchaResult
} from '@/utils/tencentCaptcha'

export interface TencentCaptchaGateProps {
  appId: string
  region?: string
}

export interface TencentCaptchaGateHandle {
  verify: () => Promise<TencentCaptchaProof | null>
  reset: (reinitialize?: boolean) => void
}

interface TencentCaptchaInstance {
  show(): void
  destroy(): void
}

type TencentCaptchaConstructor = new (
  ...args: unknown[]
) => TencentCaptchaInstance

// 腾讯国际站票据官方有效期为 5 分钟，提前 1 分钟放弃缓存，避免提交时刚好过期。
const cachedProofMaxAgeMs = 4 * 60 * 1000

export const TencentCaptchaGate = forwardRef<TencentCaptchaGateHandle, TencentCaptchaGateProps>(
  function TencentCaptchaGate({ appId, region = 'cn' }, ref) {
    const { i18n } = useTranslation()
    const isInternational = normalizeTencentCaptchaRegion(region) === 'intl'
    const internationalContainerRef = useRef<HTMLDivElement | null>(null)
    const [internationalContainerVisible, setInternationalContainerVisible] = useState(isInternational)

    const instanceRef = useRef<TencentCaptchaInstance | null>(null)
    const pendingRef = useRef<Promise<TencentCaptchaProof | null> | null>(null)
    const cancelPendingRef = useRef<(() => void) | null>(null)
    const cachedProofRef = useRef<TencentCaptchaProof | null>(null)
    const cachedProofCreatedAtRef = useRef(0)
    const isMountedRef = useRef(false)

    const createVerificationPromise = (
      revealInternational: boolean = true
    ): Promise<TencentCaptchaProof | null> => {
      return new Promise((resolve, reject) => {
        let settled = false

        const finish = (callback: () => void, keepInternationalContainer: boolean = false): void => {
          if (settled) return
          settled = true
          if (cancelPendingRef.current === cancel) cancelPendingRef.current = null
          if (!keepInternationalContainer) {
            instanceRef.current?.destroy()
            instanceRef.current = null
            setInternationalContainerVisible(false)
          }
          callback()
        }
        const cancel = (): void => finish(() => resolve(null))

        cancelPendingRef.current = cancel
        const normalizedRegion = normalizeTencentCaptchaRegion(region)
        if (normalizedRegion === 'intl' && revealInternational) {
          setInternationalContainerVisible(true)
        }

        void Promise.resolve()
          .then(() => loadTencentCaptcha(normalizedRegion))
          .then((TencentCaptcha) => {
            if (cancelPendingRef.current !== cancel) return

            const userLanguage = i18n.language.toLowerCase().startsWith('zh') ? 'zh-cn' : 'en'
            const handleResult = (result: TencentCaptchaResult): void => {
              if (result.ret === 2) {
                finish(() => resolve(null))
                return
              }

              const ticket = result.ticket?.trim() || ''
              const randstr = result.randstr?.trim() || ''
              if (!ticket || !randstr || ticket.startsWith('trerror_') || result.errorCode !== undefined) {
                finish(() => reject(new Error('Tencent Captcha verification failed')))
                return
              }

              // 国际站保留已勾选的控件，避免成功回调后页面出现跳动；登录流程结束时由 reset 清理。
              finish(() => resolve({ ticket, randstr }), normalizedRegion === 'intl')
            }

            if (normalizedRegion === 'intl') {
              // 新版国际站先在指定容器内展示 Robot checkbox，用户点击后才弹出挑战。
              // document.body 会把 checkbox 追加到整个页面末尾，在登录页上通常落到视口外，
              // 表现为 SDK 已成功初始化但页面没有任何可见组件。
              const container = internationalContainerRef.current
              if (!container) {
                throw new Error('Tencent Captcha international container is unavailable')
              }
              instanceRef.current = new (TencentCaptcha as unknown as TencentCaptchaConstructor)(
                container,
                appId,
                handleResult,
                {
                  // 国际站与国内站保持一致：页面加载后显示 Robot checkbox，由用户主动确认。
                  enableAutoCheck: false,
                  userLanguage,
                  type: 'popup'
                }
              )
            } else {
              instanceRef.current = new (TencentCaptcha as unknown as TencentCaptchaConstructor)(
                appId,
                handleResult,
                { userLanguage }
              )
            }
            instanceRef.current.show()
          })
          .catch((error: unknown) => finish(() => reject(error)))
      })
    }

    const reset = (reinitialize: boolean = true) => {
      instanceRef.current?.destroy()
      instanceRef.current = null
      cancelPendingRef.current?.()
      cancelPendingRef.current = null
      pendingRef.current = null
      cachedProofRef.current = null
      cachedProofCreatedAtRef.current = 0
      setInternationalContainerVisible(isInternational)

      // 国际站的票据一次性使用，登录失败后需要立即创建新的 checkbox，
      // 不能等下一次点击登录才重新初始化。卸载阶段不再启动预加载。
      if (reinitialize && isMountedRef.current && isInternational) {
        void Promise.resolve().then(() => {
          if (isMountedRef.current) preload()
        })
      }
    }

    const preload = () => {
      if (!isInternational || pendingRef.current || cachedProofRef.current) return

      // 国际站要求首屏直接展示 checkbox，避免用户点击登录后才看到验证码。
      const verification = createVerificationPromise(true)
      pendingRef.current = verification
      void verification
        .then((proof) => {
          if (proof) {
            cachedProofRef.current = proof
            cachedProofCreatedAtRef.current = Date.now()
          }
        })
        .catch(() => undefined)
        .finally(() => {
          if (pendingRef.current === verification) pendingRef.current = null
        })
    }

    const verify = (): Promise<TencentCaptchaProof | null> => {
      if (cachedProofRef.current) {
        if (Date.now() - cachedProofCreatedAtRef.current >= cachedProofMaxAgeMs) {
          // 过期票据不能再次提交；先同步销毁旧实例，再在本次调用中创建新验证。
          reset(false)
        } else {
          const proof = cachedProofRef.current
          cachedProofRef.current = null
          cachedProofCreatedAtRef.current = 0
          // 预加载 promise 已经完成，消费缓存时同步清除引用，避免同一票据被并发复用。
          pendingRef.current = null
          return Promise.resolve(proof)
        }
      }

      if (pendingRef.current) {
        const verification = pendingRef.current
        setInternationalContainerVisible(true)
        // 预加载阶段可能因容器不可见而被 SDK 延迟绘制，提交时在容器显示后重新触发同一个实例。
        void Promise.resolve().then(() => {
          if (pendingRef.current === verification) instanceRef.current?.show()
        })
        return verification.then((proof) => {
          if (cachedProofRef.current === proof) {
            cachedProofRef.current = null
            cachedProofCreatedAtRef.current = 0
          }
          return proof
        })
      }

      setInternationalContainerVisible(true)
      const verification = createVerificationPromise(true)
      pendingRef.current = verification
      void verification
        .then(
          () => {
            if (pendingRef.current === verification) pendingRef.current = null
          },
          () => {
            if (pendingRef.current === verification) pendingRef.current = null
          }
        )
      return verification
    }

    useImperativeHandle(ref, () => ({ verify, reset }))

    useEffect(() => {
      isMountedRef.current = true
      preload()
      return () => {
        isMountedRef.current = false
        reset()
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    if (!isInternational) return null

    return (
      <div
        ref={internationalContainerRef}
        data-testid="tencent-captcha-international-container"
        className={
          internationalContainerVisible
            ? 'flex min-h-[60px] w-full justify-center'
            : 'pointer-events-none fixed left-1/2 top-0 flex h-[60px] w-[302px] -translate-x-1/2 scale-[0.01] opacity-0'
        }
      />
    )
  }
)

export default TencentCaptchaGate
