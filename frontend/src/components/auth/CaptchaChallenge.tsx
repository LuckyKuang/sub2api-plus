import { forwardRef, useImperativeHandle, useRef } from 'react'
import { TurnstileWidget, type TurnstileWidgetHandle } from './TurnstileWidget'
import { TencentCaptchaGate, type TencentCaptchaGateHandle } from './TencentCaptchaGate'
import { AliyunCaptchaWidget, type AliyunCaptchaWidgetHandle } from './AliyunCaptchaWidget'

// ActionCaptchaResult 动作触发式验证（腾讯/阿里云弹窗）的结果：
// 腾讯 token=ticket、randstr 非空；阿里云 token=captchaVerifyParam、randstr 恒为空。
export interface ActionCaptchaResult {
  token: string
  randstr: string
}

export interface CaptchaChallengeProps {
  turnstileEnabled: boolean
  turnstileSiteKey: string
  tencentEnabled: boolean
  tencentAppId: string
  tencentRegion?: string
  aliyunEnabled?: boolean
  aliyunSceneId?: string
  aliyunPrefix?: string
  aliyunRegion?: string
  onVerify?: (token: string, randstr: string) => void
  onExpire?: () => void
  onError?: () => void
}

export interface CaptchaChallengeHandle {
  reset: () => void
  verifyAction: () => Promise<ActionCaptchaResult | null>
}

export const CaptchaChallenge = forwardRef<CaptchaChallengeHandle, CaptchaChallengeProps>(
  function CaptchaChallenge(
    {
      turnstileEnabled,
      turnstileSiteKey,
      tencentEnabled,
      tencentAppId,
      tencentRegion = 'cn',
      aliyunEnabled = false,
      aliyunSceneId = '',
      aliyunPrefix = '',
      aliyunRegion = 'cn',
      onVerify,
      onExpire,
      onError
    },
    ref
  ) {
    const turnstileRef = useRef<TurnstileWidgetHandle | null>(null)
    const tencentRef = useRef<TencentCaptchaGateHandle | null>(null)
    const aliyunRef = useRef<AliyunCaptchaWidgetHandle | null>(null)

    useImperativeHandle(ref, () => ({
      reset: () => {
        turnstileRef.current?.reset()
        tencentRef.current?.reset()
        aliyunRef.current?.reset()
      },
      // verifyAction 弹出当前启用的动作触发式验证码（腾讯/阿里云）并等待结果；
      // 用户关闭弹窗返回 null，验证异常 onError 并返回 null。
      verifyAction: async (): Promise<ActionCaptchaResult | null> => {
        if (tencentEnabled && tencentAppId) {
          try {
            const proof = (await tencentRef.current?.verify()) ?? null
            if (!proof) return null
            return { token: proof.ticket, randstr: proof.randstr }
          } catch {
            onError?.()
            return null
          }
        }
        if (aliyunEnabled && aliyunSceneId && aliyunPrefix) {
          try {
            const param = (await aliyunRef.current?.verify()) ?? null
            if (!param) return null
            return { token: param, randstr: '' }
          } catch {
            onError?.()
            return null
          }
        }
        return null
      }
    }))

    if (turnstileEnabled && turnstileSiteKey) {
      return (
        <TurnstileWidget
          ref={turnstileRef}
          siteKey={turnstileSiteKey}
          onVerify={(token) => onVerify?.(token, '')}
          onExpire={onExpire}
          onError={onError}
        />
      )
    }

    if (tencentEnabled && tencentAppId) {
      return <TencentCaptchaGate ref={tencentRef} appId={tencentAppId} region={tencentRegion} />
    }

    if (aliyunEnabled && aliyunSceneId && aliyunPrefix) {
      return (
        <AliyunCaptchaWidget
          ref={aliyunRef}
          sceneId={aliyunSceneId}
          prefix={aliyunPrefix}
          region={aliyunRegion === 'sgp' ? 'sgp' : 'cn'}
          onVerify={(param) => onVerify?.(param, '')}
          onExpire={onExpire}
          onError={onError}
        />
      )
    }

    return null
  }
)

export default CaptchaChallenge
