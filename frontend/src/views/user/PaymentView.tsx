import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { Gift } from 'lucide-react'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { cn } from '@/lib/utils'
import '@/styles/announcement-markdown.css'
import { paymentAPI } from '@/api/payment'
import { usePaymentStore } from '@/stores/payment'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useUserView } from './useUserView'
import { useLocaleRefresh } from '@/utils/localeRefresh'
import { getAdminSupportContext } from '@/utils/adminSupportContext'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel, type PeakRateFields } from '@/utils/peak-rate'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'
import {
  formatRechargeBonusNumber,
  normalizeRechargeBonusMode,
  normalizeRechargeBonusTiers,
  quoteRechargeBonus,
} from '@/utils/rechargeBonus'
import {
  platformBadgeClass,
  platformBadgeLightClass,
  platformLabel,
  platformTextClass,
} from '@/utils/platformColors'
import { planRateLabel, saleQuoteChanged } from '@/utils/billingRate'
import type {
  CheckoutInfoResponse,
  CreateOrderResult,
  MethodLimit,
  OrderType,
  SubscriptionPlan,
} from '@/types/payment'
import AmountInput from './AmountInput'
import PaymentMethodSelector, { type PaymentMethodOption } from './PaymentMethodSelector'
import PaymentStatusPanel from './PaymentStatusPanel'
import SubscriptionPlanCard from './SubscriptionPlanCard'
import PlanModelCatalog from './PlanModelCatalog'
import WorkspaceTabs from './WorkspaceTabs'
import { METHOD_ORDER, getPaymentPopupFeatures, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from './paymentProviderConfig'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  buildCreateOrderPayload,
  clearPaymentRecoverySnapshot,
  decidePaymentLaunch,
  getVisibleMethods,
  normalizeVisibleMethod,
  readPaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
  type PaymentLaunchContext,
  type PaymentRecoverySnapshot,
} from './paymentFlow'
import { DEFAULT_PAYMENT_CURRENCY, formatPaymentAmount, normalizePaymentCurrency } from './paymentCurrency'
import { planValiditySuffix as validitySuffixOf } from './paymentValidity'
import { buildPaymentErrorToastMessage, describePaymentScenarioError } from './paymentUx'
import { hasWechatResumeQuery, parseWechatResumeRoute, stripWechatResumeQuery } from './paymentWechatResume'
import './payment.css'

type PaymentTab = 'recharge' | 'subscription'

interface CreateOrderOptions {
  openid?: string
  wechatResumeToken?: string
  paymentType?: string
  isResume?: boolean
  mobileQrFallbackAttempted?: boolean
}

interface WeixinJSBridgeLike {
  invoke(
    action: string,
    payload: Record<string, unknown>,
    callback: (result: Record<string, unknown>) => void,
  ): void
}

const EMPTY_CHECKOUT: CheckoutInfoResponse = {
  methods: {}, global_min: 0, global_max: 0,
  plans: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, help_text: '', help_image_url: '', stripe_publishable_key: '',
  recharge_bonus_tiers: [], recharge_bonus_mode: 'bonus', recharge_bonus_notice: '',
}

function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    alipayMobilePrecreateDeepLink: false,
    createdAt: 0,
  }
}

function getWeixinJSBridge(): WeixinJSBridgeLike | undefined {
  return (window as Window & { WeixinJSBridge?: WeixinJSBridgeLike }).WeixinJSBridge
}

function waitForWeixinJSBridge(timeoutMs = 4000): Promise<WeixinJSBridgeLike | null> {
  const existing = getWeixinJSBridge()
  if (existing) return Promise.resolve(existing)

  return new Promise((resolve) => {
    let settled = false
    const finish = (bridge: WeixinJSBridgeLike | null) => {
      if (settled) return
      settled = true
      document.removeEventListener('WeixinJSBridgeReady', handleReady)
      document.removeEventListener('onWeixinJSBridgeReady', handleReady)
      window.clearTimeout(timer)
      resolve(bridge)
    }
    const handleReady = () => finish(getWeixinJSBridge() ?? null)
    const timer = window.setTimeout(() => finish(getWeixinJSBridge() ?? null), timeoutMs)
    document.addEventListener('WeixinJSBridgeReady', handleReady, false)
    document.addEventListener('onWeixinJSBridgeReady', handleReady, false)
  })
}

async function invokeWechatJsapiPayment(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  const bridge = await waitForWeixinJSBridge()
  if (!bridge) {
    throw new Error('WECHAT_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve) => {
    bridge.invoke('getBrandWCPayRequest', payload, (result) => resolve(result || {}))
  })
}

function isWechatBrowser(): boolean {
  return typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent)
}

function currencyFractionDigits(currency: string): number {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
    }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

function roundPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** currencyFractionDigits(currency)
  return Math.round(value * factor) / factor
}

function ceilPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** currencyFractionDigits(currency)
  return Math.ceil(value * factor) / factor
}

function amountFitsMethod(amt: number, methods: Record<string, MethodLimit>, methodType: string): boolean {
  if (amt <= 0) return true
  const ml = methods[methodType]
  if (!ml) return false
  if (ml.single_min > 0 && amt < ml.single_min) return false
  if (ml.single_max > 0 && amt > ml.single_max) return false
  return true
}

function buildSearch(query: Record<string, string | undefined>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value)
  }
  return params.toString()
}

function buildRouteUrl(path: string, query: Record<string, string | undefined>): string {
  const search = buildSearch(query)
  return search ? `${path}?${search}` : path
}

export default function PaymentView() {
  const { t, i18n } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const authStore = useUserView()
  const authUser = useAuthStore((s) => s.user)
  const paymentStore = usePaymentStore()
  const subscriptionStore = useSubscriptionStore()
  const appStore = useAppStore()

  // Assistance mode presents the target user; otherwise the signed-in user.
  const user = getAdminSupportContext() ? authStore.user : authUser
  const activeSubscriptions = subscriptionStore.activeSubscriptions

  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [errorHintMessage, setErrorHintMessage] = useState('')
  const [activeTab, setActiveTab] = useState<PaymentTab>('subscription')
  const [amount, setAmount] = useState<number | null>(null)
  const [selectedMethod, setSelectedMethod] = useState('')
  const [selectedPlan, setSelectedPlan] = useState<SubscriptionPlan | null>(null)
  const [previewImage, setPreviewImage] = useState('')
  const [paymentPhase, setPaymentPhase] = useState<'select' | 'paying'>('select')
  const [paymentState, setPaymentState] = useState<PaymentRecoverySnapshot>(emptyPaymentState)
  const [showRenewalModal, setShowRenewalModal] = useState(false)
  const [renewGroupId, setRenewGroupId] = useState<number | null>(null)
  const [checkout, setCheckout] = useState<CheckoutInfoResponse>(EMPTY_CHECKOUT)

  // Confirmation supersedes any background request already in flight.
  const saleQuoteRequest = useRef(0)
  // Cancellation, including cancel/reselect of the same plan, invalidates the
  // pending confirmation before its checkout response can create an order.
  const selectedPlanIdRef = useRef<number | null>(null)
  const checkoutRequest = useRef(0)
  const translatedGroupNames = useRef<Map<number, SubscriptionPlan['group_name']> | null>(null)
  // The quote refresher runs outside render, so it reads the latest values here.
  const loadingRef = useRef(loading)
  const paymentPhaseRef = useRef(paymentPhase)
  const submittingRef = useRef(submitting)
  const selectedPlanRef = useRef(selectedPlan)

  useEffect(() => { loadingRef.current = loading }, [loading])
  useEffect(() => { paymentPhaseRef.current = paymentPhase }, [paymentPhase])
  useEffect(() => { submittingRef.current = submitting }, [submitting])
  useEffect(() => { selectedPlanRef.current = selectedPlan }, [selectedPlan])

  useEffect(() => () => { saleQuoteRequest.current += 1 }, [])

  function updateSelectedPlan(plan: SubscriptionPlan | null) {
    const nextId = plan?.id ?? null
    if (nextId !== selectedPlanIdRef.current) {
      selectedPlanIdRef.current = nextId
      saleQuoteRequest.current += 1
    }
    setSelectedPlan(plan)
  }

  function getDaysRemaining(expiresAt: string): number {
    const diff = new Date(expiresAt).getTime() - Date.now()
    return Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
  }

  function subscriptionHasPeakRate(sub: { group?: PeakRateFields | null }): boolean {
    return hasPeakRate(sub.group)
  }

  function subscriptionPeakRateLabel(sub: { group?: PeakRateFields | null }): string {
    return formatPeakRateWindow(sub.group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
  }

  function persistRecoverySnapshot(snapshot: PaymentRecoverySnapshot) {
    if (typeof window === 'undefined' || !snapshot.orderId) return
    writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
  }

  function removeRecoverySnapshot() {
    if (typeof window === 'undefined') return
    clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
  }

  function resetPayment() {
    setPaymentPhase('select')
    setPaymentState(emptyPaymentState())
    removeRecoverySnapshot()
  }

  async function redirectToPaymentResult(state: PaymentRecoverySnapshot): Promise<void> {
    const query: Record<string, string | undefined> = {}
    if (state.orderId > 0) {
      query.order_id = String(state.orderId)
    }
    if (state.outTradeNo) {
      query.out_trade_no = state.outTradeNo
    }
    if (state.resumeToken) {
      query.resume_token = state.resumeToken
    }
    await navigate({ pathname: '/payment/result', search: buildSearch(query) })
  }

  function buildWechatOAuthAuthorizeUrl(
    authorizeUrl: string,
    context: { paymentType: string; orderType: OrderType; planId?: number; orderAmount: number },
  ): string {
    const normalizedUrl = authorizeUrl.trim()
    if (!normalizedUrl || typeof window === 'undefined') {
      return normalizedUrl
    }

    try {
      const targetUrl = new URL(normalizedUrl, window.location.origin)
      const redirectPath = targetUrl.searchParams.get('redirect') || '/purchase'
      const redirectUrl = new URL(redirectPath, window.location.origin)
      const paymentType = normalizeVisibleMethod(context.paymentType) || context.paymentType.trim() || 'wxpay'

      redirectUrl.searchParams.set('payment_type', paymentType)
      redirectUrl.searchParams.set('order_type', context.orderType)

      if (context.planId) {
        redirectUrl.searchParams.set('plan_id', String(context.planId))
      } else {
        redirectUrl.searchParams.delete('plan_id')
      }

      if (context.orderAmount > 0) {
        redirectUrl.searchParams.set('amount', String(context.orderAmount))
      } else {
        redirectUrl.searchParams.delete('amount')
      }

      targetUrl.searchParams.set('redirect', `${redirectUrl.pathname}${redirectUrl.search}`)
      return targetUrl.toString()
    } catch {
      return normalizedUrl
    }
  }

  function onPaymentDone() {
    const wasSubscription = paymentState.orderType === 'subscription'
    resetPayment()
    updateSelectedPlan(null)
    setActiveTab(wasSubscription || checkout.balance_disabled ? 'subscription' : 'recharge')
    if (wasSubscription) {
      void subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
    }
  }

  async function onPaymentSuccess() {
    const completedPayment = { ...paymentState }
    removeRecoverySnapshot()
    void authStore.refreshUser()
    if (paymentState.orderType === 'subscription') {
      void subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
    }
    await redirectToPaymentResult(completedPayment)
  }

  function onPaymentSettled() {
    removeRecoverySnapshot()
  }

  const renderedHelpText = useMemo(() => DOMPurify.sanitize(
    marked.parse(checkout.help_text || '', { async: false, gfm: true, breaks: false }) as string,
  ), [checkout.help_text])

  // 充值赠送活动文案：后台 Markdown 配置，空字符串时金额卡顶部不渲染
  const renderedBonusNotice = useMemo(() => {
    const raw = (checkout.recharge_bonus_notice || '').trim()
    if (!raw) return ''
    return DOMPurify.sanitize(marked.parse(raw, { async: false, gfm: true, breaks: true }) as string)
  }, [checkout.recharge_bonus_notice])

  // 订阅功能开关（public settings 的 subscription_enabled，opt-out）。关闭后购买页只保留充值：
  // 不再渲染「订阅」tab，只剩单个 tab 时顶部切换器也随之隐藏。
  const subscriptionEnabled = resolveFeatureFlag(appStore.cachedPublicSettings, FeatureFlags.subscription)

  const tabs = useMemo(() => {
    const result: { value: PaymentTab; label: string }[] = []
    if (subscriptionEnabled) result.push({ value: 'subscription', label: t('payment.tabSubscribe') })
    if (!checkout.balance_disabled) result.push({ value: 'recharge', label: t('payment.tabTopUp') })
    return result
  }, [subscriptionEnabled, checkout.balance_disabled, t])

  // tab 列表随 checkout（balance_disabled）与订阅开关变化。当前 tab 不在列表里时收敛到第一个可用 tab，
  // 两个方向都覆盖：关闭订阅 → 回到充值；仅订阅站点重新打开订阅 → 进入订阅。列表为空时模板展示不可用提示。
  useEffect(() => {
    if (tabs.some((tab) => tab.value === activeTab)) return
    const leavingSubscription = activeTab === 'subscription'
    setActiveTab(tabs[0]?.value ?? 'recharge')
    if (leavingSubscription) updateSelectedPlan(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tabs])

  const visibleMethods = useMemo(() => getVisibleMethods(checkout.methods), [checkout.methods])
  const enabledMethods = useMemo(() => Object.keys(visibleMethods), [visibleMethods])
  const validAmount = amount ?? 0
  const balanceRechargeMultiplier = (() => {
    const multiplier = checkout.balance_recharge_multiplier
    return Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 1
  })()
  // 订阅 CNY 换算汇率（1 USD = X CNY）。0 = 未配置，订阅保持 price 直付（与后端 opt-in 条件严格镜像）。
  const subscriptionUsdToCnyRate = (() => {
    const rate = checkout.subscription_usd_to_cny_rate
    return Number.isFinite(rate) && rate > 0 ? rate : 0
  })()
  const rechargeBonusTiers = normalizeRechargeBonusTiers(checkout.recharge_bonus_tiers)
  const rechargeBonusMode = normalizeRechargeBonusMode(checkout.recharge_bonus_mode)

  // Visible methods decide the amount range shown to users.
  const globalMinAmount = (() => {
    const limits = Object.values(visibleMethods)
    if (limits.length === 0) return 0
    if (limits.some(limit => limit.single_min <= 0)) return 0
    return Math.min(...limits.map(limit => limit.single_min))
  })()
  const globalMaxAmount = (() => {
    const limits = Object.values(visibleMethods)
    if (limits.length === 0) return 0
    if (limits.some(limit => limit.single_max <= 0)) return 0
    return Math.max(...limits.map(limit => limit.single_max))
  })()

  // Selected method's limits (for validation and error messages)
  const selectedLimit = visibleMethods[selectedMethod]
  const selectedCurrency = normalizePaymentCurrency(selectedLimit?.currency)
  const localeCode = i18n.resolvedLanguage || i18n.language

  function subscriptionPaymentAmountForCurrency(value: number, currency: string): number {
    const rate = subscriptionUsdToCnyRate
    if (rate <= 0 || currency !== DEFAULT_PAYMENT_CURRENCY) return roundPaymentAmount(value, currency)
    return roundPaymentAmount(value * rate, currency)
  }

  function formatSelectedPaymentAmount(value: number): string {
    return formatPaymentAmount(value, selectedCurrency, localeCode)
  }

  function formatSelectedSubscriptionPaymentAmount(value: number): string {
    return formatSelectedPaymentAmount(subscriptionPaymentAmountForCurrency(value, selectedCurrency))
  }

  // 充值优惠：阈值按输入金额命中；赠金模式按到账基数（输入 × 倍率）加赠送，折扣模式按百分比减实付。
  // 与后端 quoteRechargeBonus 一致；渠道限额、手续费、实付都按折后基数（payBaseAmount）计算，提交仍发送输入金额。
  const bonusQuote = quoteRechargeBonus(rechargeBonusTiers, validAmount, {
    multiplier: balanceRechargeMultiplier,
    mode: rechargeBonusMode,
    currencyDigits: currencyFractionDigits(selectedCurrency),
  })
  const payBaseAmount = bonusQuote.payBase
  const discountAmount = roundPaymentAmount(validAmount - payBaseAmount, selectedCurrency)
  const creditedAmount = bonusQuote.credited
  const showBonusRow = bonusQuote.mode !== 'discount' && bonusQuote.bonus > 0
  const showCreditedBalance = balanceRechargeMultiplier !== 1 || bonusQuote.percent > 0

  const methodOptions = useMemo<PaymentMethodOption[]>(() =>
    enabledMethods.map((type) => {
      const ml = visibleMethods[type]
      return {
        type,
        display_name: ml?.display_name,
        fee_rate: ml?.fee_rate ?? 0,
        available: ml?.available !== false && amountFitsMethod(payBaseAmount, visibleMethods, type),
      }
    }), [enabledMethods, visibleMethods, payBaseAmount])

  const feeRate = checkout.recharge_fee_rate ?? 0
  const feeAmount = feeRate > 0 && payBaseAmount > 0
    ? Math.ceil(((payBaseAmount * feeRate) / 100) * 100) / 100
    : 0
  const totalAmount = feeRate > 0 && payBaseAmount > 0
    ? Math.round((payBaseAmount + feeAmount) * 100) / 100
    : payBaseAmount
  const showActualPay = feeRate > 0 || discountAmount > 0

  let amountError = ''
  if (validAmount > 0) {
    // No method can handle this amount
    if (!enabledMethods.some((m) => amountFitsMethod(payBaseAmount, visibleMethods, m))) {
      amountError = t('payment.amountNoMethod')
    } else {
      // Selected method can't handle this amount (but others can)
      const ml = selectedLimit
      if (ml) {
        if (ml.single_min > 0 && payBaseAmount < ml.single_min) amountError = t('payment.amountTooLow', { min: formatSelectedPaymentAmount(ml.single_min) })
        else if (ml.single_max > 0 && payBaseAmount > ml.single_max) amountError = t('payment.amountTooHigh', { max: formatSelectedPaymentAmount(ml.single_max) })
      }
    }
  }

  const canSubmit = validAmount > 0
    && amountFitsMethod(payBaseAmount, visibleMethods, selectedMethod)
    && selectedLimit?.available !== false

  const subPaymentAmount = subscriptionPaymentAmountForCurrency(selectedPlan?.price ?? 0, selectedCurrency)

  const subFeeAmount = (() => {
    if (feeRate <= 0 || subPaymentAmount <= 0) return 0
    return ceilPaymentAmount((subPaymentAmount * feeRate) / 100, selectedCurrency)
  })()

  const subTotalAmount = (() => {
    if (feeRate <= 0 || subPaymentAmount <= 0) return subPaymentAmount
    return roundPaymentAmount(subPaymentAmount + subFeeAmount, selectedCurrency)
  })()

  function subscriptionTotalAmountForCurrency(value: number, currency: string): number {
    const paymentAmount = subscriptionPaymentAmountForCurrency(value, currency)
    if (feeRate <= 0 || paymentAmount <= 0) return paymentAmount
    const fee = ceilPaymentAmount((paymentAmount * feeRate) / 100, currency)
    return roundPaymentAmount(paymentAmount + fee, currency)
  }

  // Subscription-specific: method options based on gateway pay amount
  const subMethodOptions = useMemo<PaymentMethodOption[]>(() => {
    const price = selectedPlan?.price ?? 0
    return enabledMethods.map((type) => {
      const ml = visibleMethods[type]
      const currency = normalizePaymentCurrency(ml?.currency)
      return {
        type,
        display_name: ml?.display_name,
        fee_rate: ml?.fee_rate ?? 0,
        available: ml?.available !== false && amountFitsMethod(subscriptionTotalAmountForCurrency(price, currency), visibleMethods, type),
      }
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabledMethods, visibleMethods, selectedPlan?.price, feeRate, subscriptionUsdToCnyRate])

  const canSubmitSubscription = selectedPlan !== null
    && amountFitsMethod(subTotalAmount, visibleMethods, selectedMethod)
    && selectedLimit?.available !== false

  // Auto-switch to first available method when current selection can't handle the amount
  useEffect(() => {
    if (payBaseAmount <= 0 || amountFitsMethod(payBaseAmount, visibleMethods, selectedMethod)) return
    const available = enabledMethods.find((m) => amountFitsMethod(payBaseAmount, visibleMethods, m))
    if (available) setSelectedMethod(available)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payBaseAmount, selectedMethod, visibleMethods])

  // Payment button class: follows selected payment method color
  let paymentButtonClass = 'btn-primary'
  if (selectedMethod) {
    if (isBuiltInAlipayMethod(selectedMethod)) paymentButtonClass = 'btn-alipay'
    else if (isBuiltInWxpayMethod(selectedMethod)) paymentButtonClass = 'btn-wxpay'
    else if (selectedMethod === 'stripe') paymentButtonClass = 'btn-stripe'
    else if (selectedMethod === 'airwallex') paymentButtonClass = 'btn-airwallex'
  }

  // Subscription confirm: platform accent colors (clean card, no gradient)
  const planBadgeClass = platformBadgeClass(selectedPlan?.group_platform || '')
  const planTextClass = platformTextClass(selectedPlan?.group_platform || '')

  const renewalPlans = useMemo(() => {
    if (renewGroupId == null) return []
    return checkout.plans.filter(p => p.group_id === renewGroupId)
  }, [checkout.plans, renewGroupId])

  const planValiditySuffix = selectedPlan ? validitySuffixOf(selectedPlan, t) : ''

  function planHasPeakRate(plan: SubscriptionPlan): boolean {
    return hasPeakRate(plan)
  }

  function planPeakRateLabel(plan: SubscriptionPlan): string {
    return formatPeakRateWindow(plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
  }

  function selectPlan(plan: SubscriptionPlan) {
    updateSelectedPlan(plan)
    setErrorMessage('')
  }

  function selectPlanFromModal(plan: SubscriptionPlan) {
    setShowRenewalModal(false)
    setRenewGroupId(null)
    updateSelectedPlan(plan)
    setErrorMessage('')
  }

  function closeRenewalModal() {
    setShowRenewalModal(false)
    setRenewGroupId(null)
  }

  async function handleSubmitRecharge() {
    if (!canSubmit || submitting) return
    await createOrder(validAmount, 'balance')
  }

  async function confirmSubscribe() {
    if (!selectedPlan || submitting) return
    const previous = selectedPlan
    const previousPayAmount = subTotalAmount
    const previousCurrency = selectedCurrency
    setSubmitting(true)
    setErrorMessage('')
    const request = ++saleQuoteRequest.current
    try {
      const response = await paymentAPI.getCheckoutInfo()
      if (request !== saleQuoteRequest.current || selectedPlan?.id !== previous.id) return
      const current = response.data.plans.find(plan => plan.id === previous.id)
      setCheckout(response.data)
      if (!current) {
        updateSelectedPlan(null)
        setErrorMessage(t('billingRates.unavailablePlan'))
        return
      }
      // Recompute the refreshed quote exactly as the derived state would, so the
      // change check compares the values the user is about to pay.
      const nextVisibleMethods = getVisibleMethods(response.data.methods)
      const nextLimit = nextVisibleMethods[selectedMethod]
      const nextCurrency = normalizePaymentCurrency(nextLimit?.currency)
      const nextRate = (() => {
        const rate = response.data.subscription_usd_to_cny_rate
        return Number.isFinite(rate) && rate > 0 ? rate : 0
      })()
      const nextFeeRate = response.data.recharge_fee_rate ?? 0
      const nextPaymentAmount = (() => {
        const value = current.price
        if (nextRate <= 0 || nextCurrency !== DEFAULT_PAYMENT_CURRENCY) return roundPaymentAmount(value, nextCurrency)
        return roundPaymentAmount(value * nextRate, nextCurrency)
      })()
      const nextTotalAmount = (() => {
        if (nextFeeRate <= 0 || nextPaymentAmount <= 0) return nextPaymentAmount
        const fee = ceilPaymentAmount((nextPaymentAmount * nextFeeRate) / 100, nextCurrency)
        return roundPaymentAmount(nextPaymentAmount + fee, nextCurrency)
      })()
      const nextCanSubmit = amountFitsMethod(nextTotalAmount, nextVisibleMethods, selectedMethod)
        && nextLimit?.available !== false
      updateSelectedPlan(current)
      if (
        saleQuoteChanged(previous, current)
        || previousPayAmount !== nextTotalAmount
        || previousCurrency !== nextCurrency
        || !nextCanSubmit
      ) {
        setErrorMessage(t('billingRates.changed'))
        return
      }
    } catch {
      if (request === saleQuoteRequest.current) setErrorMessage(t('billingRates.refreshFailed'))
      return
    } finally {
      setSubmitting(false)
    }
    await createOrder(selectedPlan!.price, 'subscription', selectedPlan!.id)
  }

  function applyScenarioError(err: unknown, paymentMethod: string): boolean {
    const descriptor = describePaymentScenarioError(err, {
      paymentMethod,
      isMobile: isMobileDevice(),
      isWechatBrowser: isWechatBrowser(),
    })
    if (!descriptor) {
      setErrorMessage('')
      setErrorHintMessage('')
      return false
    }
    const message = t(descriptor.messageKey)
    const hint = descriptor.hintKey ? t(descriptor.hintKey) : ''
    setErrorMessage(message)
    setErrorHintMessage(hint)
    appStore.showError(buildPaymentErrorToastMessage(message, hint))
    return true
  }

  interface MobileQrFallbackContext {
    orderAmount: number
    orderType: OrderType
    planId?: number
    paymentType: string
    attempted: boolean
  }

  function shouldFallbackToDesktopQr(err: unknown, paymentMethod: string, attempted: boolean): boolean {
    if (attempted || !isMobileDevice()) {
      return false
    }

    const normalizedMethod = normalizeVisibleMethod(paymentMethod) || paymentMethod
    const reason = typeof err === 'object' && err && 'reason' in err && typeof err.reason === 'string'
      ? err.reason
      : ''
    const message = err instanceof Error
      ? err.message
      : (typeof err === 'object' && err && 'message' in err && typeof err.message === 'string'
        ? err.message
        : '')
    const normalizedMessage = message.toLowerCase()

    if (normalizedMethod === 'wxpay') {
      return reason === 'WECHAT_H5_NOT_AUTHORIZED'
        || reason === 'WECHAT_PAYMENT_MP_NOT_CONFIGURED'
        || reason === 'WECHAT_JSAPI_FAILED'
        || reason === 'PAYMENT_GATEWAY_ERROR'
        || reason === 'UNHANDLED_PAYMENT_SCENARIO'
        || normalizedMessage.includes('weixinjsbridge is unavailable')
        || normalizedMessage.includes('wechat_jsapi_unavailable')
    }

    if (normalizedMethod === 'alipay') {
      return reason === 'PAYMENT_GATEWAY_ERROR' || reason === 'UNHANDLED_PAYMENT_SCENARIO'
    }

    return false
  }

  async function attemptMobileQrFallback(err: unknown, context: MobileQrFallbackContext): Promise<boolean> {
    if (!shouldFallbackToDesktopQr(err, context.paymentType, context.attempted)) {
      return false
    }

    try {
      const visibleMethod = normalizeVisibleMethod(context.paymentType) || context.paymentType
      const payload = buildCreateOrderPayload({
        amount: context.orderAmount,
        paymentType: visibleMethod,
        orderType: context.orderType,
        planId: context.planId,
        origin: typeof window !== 'undefined' ? window.location.origin : '',
        isMobile: false,
        isWechatBrowser: false,
      })
      if (context.orderType === 'subscription' && selectedPlan && selectedPlan.id === context.planId && selectedPlan.model_catalog) {
        payload.expected_billing_policy_version = selectedPlan.model_catalog.policy_version
      }
      const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
      const stripeMethod = visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
      const stripeRouteUrl = result.client_secret
        ? buildRouteUrl('/payment/stripe', {
          order_id: String(result.order_id),
          method: stripeMethod,
          resume_token: result.resume_token || undefined,
        })
        : ''
      const decision = decidePaymentLaunch(result, {
        visibleMethod,
        orderType: context.orderType,
        isMobile: false,
        isWechatBrowser: false,
        stripePopupUrl: stripeRouteUrl,
        stripeRouteUrl,
      })

      if (decision.kind !== 'qr_waiting' || !decision.paymentState.qrCode) {
        return false
      }

      setErrorMessage('')
      setErrorHintMessage('')
      setPaymentState(decision.paymentState)
      setPaymentPhase('paying')
      persistRecoverySnapshot(decision.recovery)
      appStore.showWarning(t('payment.errors.mobilePaymentFallbackToQr'))
      return true
    } catch {
      return false
    }
  }

  async function createOrder(orderAmount: number, orderType: OrderType, planId?: number, options: CreateOrderOptions = {}) {
    setSubmitting(true)
    setErrorMessage('')
    setErrorHintMessage('')
    const requestType = normalizeVisibleMethod(options.paymentType || selectedMethod) || options.paymentType || selectedMethod
    try {
      const payload = buildCreateOrderPayload({
        amount: orderAmount,
        paymentType: requestType,
        orderType,
        planId,
        origin: typeof window !== 'undefined' ? window.location.origin : '',
        isMobile: isMobileDevice(),
        isWechatBrowser: isWechatBrowser(),
        forceQRCode: !!(checkout.alipay_force_qrcode && normalizeVisibleMethod(requestType) === 'alipay'),
        mobilePrecreateDeepLink: checkout.alipay_mobile_precreate_deep_link === true,
      })
      if (options.openid) {
        payload.openid = options.openid
      }
      if (options.wechatResumeToken) {
        payload.wechat_resume_token = options.wechatResumeToken
      }
      if (orderType === 'subscription' && selectedPlan && selectedPlan.id === planId && selectedPlan.model_catalog) {
        payload.expected_billing_policy_version = selectedPlan.model_catalog.policy_version
      }

      const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
      const openWindow = (url: string) => {
        const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
        if (!win || win.closed) {
          window.location.href = url
        }
      }
      const visibleMethod = normalizeVisibleMethod(requestType) || requestType
      // When user clicks the dedicated Stripe button, leave method blank so the
      // landing page renders Stripe's full Payment Element (card/link/alipay/wxpay).
      const stripeMethod = visibleMethod === 'stripe'
        ? ''
        : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
      const stripeRouteUrl = result.client_secret && visibleMethod !== 'airwallex'
        ? buildRouteUrl('/payment/stripe', {
          order_id: String(result.order_id),
          method: stripeMethod || undefined,
          resume_token: result.resume_token || undefined,
        })
        : ''
      const airwallexRouteUrl = result.client_secret && result.intent_id
        ? buildRouteUrl('/payment/airwallex', {
          order_id: String(result.order_id),
          out_trade_no: result.out_trade_no || undefined,
          resume_token: result.resume_token || undefined,
        })
        : ''
      const decision = decidePaymentLaunch(result, {
        visibleMethod,
        orderType,
        isMobile: isMobileDevice(),
        isWechatBrowser: isWechatBrowser(),
        forceQRCode: !!(checkout.alipay_force_qrcode && visibleMethod === 'alipay'),
        mobilePrecreateDeepLink: checkout.alipay_mobile_precreate_deep_link === true,
        stripePopupUrl: stripeRouteUrl,
        stripeRouteUrl,
        airwallexRouteUrl,
      } satisfies PaymentLaunchContext)

      if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
        window.location.href = buildWechatOAuthAuthorizeUrl(decision.oauth.authorize_url, {
          paymentType: visibleMethod,
          orderType,
          planId,
          orderAmount,
        })
        return
      }

      if (decision.kind === 'unhandled') {
        applyScenarioError({ reason: 'UNHANDLED_PAYMENT_SCENARIO' }, visibleMethod)
        return
      }

      setPaymentState(decision.paymentState)
      setPaymentPhase('paying')
      persistRecoverySnapshot(decision.recovery)

      if (decision.kind === 'stripe_popup') {
        openWindow(decision.paymentState.payUrl)
        return
      }
      if (decision.kind === 'stripe_route') {
        window.location.href = decision.paymentState.payUrl
        return
      }
      if (decision.kind === 'airwallex_route') {
        window.location.href = decision.paymentState.payUrl
        return
      }
      if (decision.kind === 'wechat_jsapi' && decision.jsapi) {
        try {
          const jsapiResult = await invokeWechatJsapiPayment(decision.jsapi as Record<string, unknown>)
          const errMsg = String(jsapiResult.err_msg || '').toLowerCase()
          if (errMsg.includes('cancel')) {
            appStore.showInfo(t('payment.qr.cancelled'))
            resetPayment()
          } else if (errMsg && !errMsg.includes('ok')) {
            resetPayment()
            const fallbackApplied = await attemptMobileQrFallback(
              { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
              {
                orderAmount,
                orderType,
                planId,
                paymentType: visibleMethod,
                attempted: options.mobileQrFallbackAttempted === true,
              },
            )
            if (!fallbackApplied) {
              applyScenarioError({ reason: 'WECHAT_JSAPI_FAILED', message: errMsg }, visibleMethod)
            }
          } else {
            const resultState = { ...decision.paymentState }
            resetPayment()
            await redirectToPaymentResult(resultState)
          }
        } catch (err: unknown) {
          resetPayment()
          const fallbackApplied = await attemptMobileQrFallback(err, {
            orderAmount,
            orderType,
            planId,
            paymentType: visibleMethod,
            attempted: options.mobileQrFallbackAttempted === true,
          })
          if (!fallbackApplied) {
            throw err
          }
        }
        return
      }
      if (decision.kind === 'redirect_waiting' && decision.paymentState.payUrl) {
        if (isMobileDevice()) {
          window.location.href = decision.paymentState.payUrl
          return
        }
        openWindow(decision.paymentState.payUrl)
      }
    } catch (err: unknown) {
      const apiErr = err as Record<string, unknown>
      // The toast repeats the message just written into the banner, so both are
      // derived from the same local values instead of the not-yet-committed state.
      let toastMessage = ''
      // Every branch above clears the banner hint, so the toast carries the message alone.
      const toastHint = ''
      if (apiErr.reason === 'BILLING_POLICY_CHANGED') {
        toastMessage = t('billingRates.changed')
        setErrorMessage(toastMessage)
        setErrorHintMessage('')
      } else if (apiErr.reason === 'TOO_MANY_PENDING') {
        const metadata = apiErr.metadata as Record<string, unknown> | undefined
        toastMessage = t('payment.errors.tooManyPending', { max: metadata?.max || '' })
        setErrorMessage(toastMessage)
        setErrorHintMessage('')
      } else if (apiErr.reason === 'CANCEL_RATE_LIMITED') {
        toastMessage = t('payment.errors.cancelRateLimited')
        setErrorMessage(toastMessage)
        setErrorHintMessage('')
      } else if (await attemptMobileQrFallback(err, {
        orderAmount,
        orderType,
        planId,
        paymentType: requestType,
        attempted: options.mobileQrFallbackAttempted === true,
      })) {
        return
      } else {
        const handled = applyScenarioError(
          err,
          normalizeVisibleMethod(options.paymentType || selectedMethod) || selectedMethod,
        )
        if (!handled) {
          toastMessage = extractI18nErrorMessage(err, t, 'payment.errors', t('payment.result.failed'))
          setErrorMessage(toastMessage)
          setErrorHintMessage('')
        }
        if (handled) {
          return
        }
      }
      appStore.showError(buildPaymentErrorToastMessage(toastMessage, toastHint))
    } finally {
      setSubmitting(false)
    }
  }

  async function resumeWechatPaymentFromQuery(plans: SubscriptionPlan[], fallbackBalanceAmount: number) {
    const resume = parseWechatResumeRoute(searchParams, plans, fallbackBalanceAmount)
    if (!resume) {
      return
    }

    setSelectedMethod(resume.paymentType)
    if (resume.orderType === 'balance' && resume.orderAmount > 0) {
      setAmount(resume.orderAmount)
    }
    if (resume.orderType === 'subscription' && resume.planId) {
      updateSelectedPlan(plans.find(plan => plan.id === resume.planId) ?? null)
    }

    const nextQuery = stripWechatResumeQuery(searchParams)
    await navigate({ pathname: location.pathname, search: nextQuery.toString() }, { replace: true })

    if (resume.wechatResumeToken) {
      await createOrder(0, resume.orderType, resume.planId, {
        wechatResumeToken: resume.wechatResumeToken,
        paymentType: resume.paymentType,
        isResume: true,
      })
      return
    }

    if (resume.orderAmount > 0 && resume.openid) {
      await createOrder(resume.orderAmount, resume.orderType, resume.planId, {
        openid: resume.openid,
        paymentType: resume.paymentType,
        isResume: true,
      })
    }
  }

  function applyTranslatedGroupNames() {
    const translated = translatedGroupNames.current
    if (!translated) return
    setCheckout(previous => ({
      ...previous,
      plans: previous.plans.map(plan => translated.has(plan.id) ? { ...plan, group_name: translated.get(plan.id)! } : plan),
    }))
    setSelectedPlan(previous => previous && translated.has(previous.id)
      ? { ...previous, group_name: translated.get(previous.id)! }
      : previous)
  }

  useLocaleRefresh(async () => {
    const request = ++checkoutRequest.current
    const res = await paymentAPI.getCheckoutInfo()
    if (request !== checkoutRequest.current) return
    // Only refresh translated display text. Preserve selected prices, payment
    // methods, recovery state and any recharge/confirmation draft.
    translatedGroupNames.current = new Map(res.data.plans.map(plan => [plan.id, plan.group_name]))
    applyTranslatedGroupNames()
  })

  // Refresh displayed request-time quotes while the page remains visible.
  useEffect(() => {
    let running = false
    let disposed = false
    const update = async () => {
      if (disposed || running || document.visibilityState === 'hidden') return
      running = true
      try {
        if (loadingRef.current || paymentPhaseRef.current !== 'select' || submittingRef.current) return
        const request = ++saleQuoteRequest.current
        const response = await paymentAPI.getCheckoutInfo()
        if (request !== saleQuoteRequest.current || paymentPhaseRef.current !== 'select' || submittingRef.current) return
        setCheckout(previous => ({ ...previous, plans: response.data.plans }))
        const currentSelected = selectedPlanRef.current
        if (currentSelected) {
          const updated = response.data.plans.find(plan => plan.id === currentSelected.id)
          if (!updated) {
            updateSelectedPlan(null)
            setErrorMessage(t('billingRates.unavailablePlan'))
          } else if (saleQuoteChanged(currentSelected, updated)) {
            updateSelectedPlan(updated)
            setErrorMessage(t('billingRates.changed'))
          } else {
            updateSelectedPlan(updated)
          }
        }
      } catch {
        // The owning page renders its own error state.
      } finally {
        running = false
      }
    }
    const interval = window.setInterval(() => { void update() }, 60_000)
    const onFocus = () => { void update() }
    const onVisibilityChange = () => { void update() }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      disposed = true
      window.clearInterval(interval)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    let cancelled = false
    async function init() {
      try {
        const res = await paymentAPI.getCheckoutInfo()
        if (cancelled) return
        setCheckout(res.data)
        applyTranslatedGroupNames()
        const methods = Object.keys(getVisibleMethods(res.data.methods))
        if (methods.length) {
          const order: readonly string[] = METHOD_ORDER
          const sorted = [...methods].sort((a, b) => {
            const ai = order.indexOf(a)
            const bi = order.indexOf(b)
            return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
          })
          setSelectedMethod(sorted[0])
        }
        if (typeof window !== 'undefined' && !getAdminSupportContext()) {
          if (hasWechatResumeQuery(searchParams)) {
            removeRecoverySnapshot()
          }
          const routeResumeToken = searchParams.get('resume_token') ?? searchParams.get('wechat_resume_token') ?? undefined
          const restored = readPaymentRecoverySnapshot(
            window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
            { resumeToken: routeResumeToken },
          )
          if (restored) {
            setPaymentState(restored)
            setPaymentPhase('paying')
            const restoredMethod = normalizeVisibleMethod(restored.paymentType)
              || (getVisibleMethods(res.data.methods)[restored.paymentType] ? restored.paymentType : '')
            if (restoredMethod) {
              setSelectedMethod(restoredMethod)
            }
          } else {
            removeRecoverySnapshot()
          }
        }
        if (!getAdminSupportContext()) {
          await resumeWechatPaymentFromQuery(res.data.plans, validAmount)
        }
        if (res.data.balance_disabled && subscriptionEnabled) {
          setActiveTab('subscription')
        }
        // Handle deep-link / renewal navigation: ?tab=subscription|recharge&group=123
        // ?tab=subscription is ignored when subscriptions are disabled.
        if (searchParams.get('tab') === 'recharge' && !res.data.balance_disabled) {
          setActiveTab('recharge')
        } else if (searchParams.get('tab') === 'subscription' && subscriptionEnabled) {
          setActiveTab('subscription')
          const group = searchParams.get('group')
          if (group) {
            const groupId = Number(group)
            const groupPlans = res.data.plans.filter(p => p.group_id === groupId)
            if (groupPlans.length === 1) {
              updateSelectedPlan(groupPlans[0])
            } else if (groupPlans.length > 1) {
              setRenewGroupId(groupId)
              setShowRenewalModal(true)
            }
          }
        }
      } catch (err: unknown) {
        appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error')))
      } finally {
        setLoading(false)
      }
      // Fetch active subscriptions (uses cache, non-blocking); skipped when the subscription feature is off
      if (subscriptionEnabled) {
        void subscriptionStore.fetchActiveSubscriptions().catch(() => {})
      }
    }
    void init()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Explicitly marked mutation controls keep the read-only admin support view safe.
  const supportReadonly = getAdminSupportContext() !== null

  if (loading) {
    return (
      <div className="payment-page workspace-page">
        <div className="card workspace-shell workspace-scroll items-center justify-center py-20">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
        </div>
      </div>
    )
  }

  return (
    <div className="payment-page workspace-page">
      {/* Tab Switcher (hide during payment and subscription confirm) */}
      {tabs.length > 1 && paymentPhase === 'select' && !selectedPlan && (
        <WorkspaceTabs
          value={activeTab}
          tabs={tabs}
          className="workspace-shell payment-tabs"
          label={`${t('payment.tabSubscribe')} / ${t('payment.tabTopUp')}`}
          onValueChange={setActiveTab}
        />
      )}
      <div className="workspace-shell workspace-scroll payment-activity" data-testid="payment-activity" tabIndex={0}>
        {errorMessage && (
          <div role="alert" className="card workspace-shell border border-amber-300 p-4 text-sm text-amber-800 dark:border-amber-700 dark:text-amber-200">
            <p>{errorMessage}</p>
            {errorHintMessage && <p className="mt-1">{errorHintMessage}</p>}
          </div>
        )}
        {/* Payment in progress (shared by recharge and subscription) */}
        {paymentPhase === 'paying' ? (
          <PaymentStatusPanel
            orderId={paymentState.orderId}
            amount={paymentState.amount}
            payAmount={paymentState.payAmount}
            qrCode={paymentState.qrCode}
            expiresAt={paymentState.expiresAt}
            paymentType={paymentState.paymentType}
            payUrl={paymentState.payUrl}
            orderType={paymentState.orderType}
            currency={paymentState.currency || selectedCurrency}
            outTradeNo={paymentState.outTradeNo}
            mobileAlipayDeepLink={paymentState.alipayMobilePrecreateDeepLink}
            onDone={onPaymentDone}
            onSuccess={() => { void onPaymentSuccess() }}
            onSettled={onPaymentSettled}
          />
        ) : (
          <>
            {/* Neither top-up nor subscriptions available (balance recharge disabled via API while subscriptions are off) */}
            {tabs.length === 0 ? (
              <div className="card workspace-shell py-16 text-center">
                <p className="surface-muted">{t('payment.billingUnavailable')}</p>
              </div>
            ) : activeTab === 'recharge' ? (
              <>
                {/* Recharge Account Card */}
                <div className="card workspace-shell p-5">
                  <p className="text-xs font-medium surface-muted">{t('payment.rechargeAccount')}</p>
                  <p className="mt-1 text-base font-semibold text-gray-900 dark:text-white">{user?.username || ''}</p>
                  <p className="mt-0.5 text-sm font-medium text-green-600 dark:text-green-400">{t('payment.currentBalance')}: {user?.balance?.toFixed(2) || '0.00'}</p>
                </div>
                {enabledMethods.length === 0 ? (
                  <div className="card workspace-shell py-16 text-center">
                    <p className="surface-muted">{t('payment.notAvailable')}</p>
                  </div>
                ) : (
                  <>
                    <div className="card workspace-shell p-6">
                      {/* 充值赠送活动文案（后台 Markdown 配置，空则不渲染） */}
                      {renderedBonusNotice && (
                        <div
                          className="mb-5 rounded-xl border border-amber-200 bg-amber-50/[0.15] px-4 py-3 text-sm leading-relaxed text-amber-900 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-100 [&_a]:font-medium [&_a]:underline [&_h1]:text-base [&_h1]:font-semibold [&_h2]:text-base [&_h2]:font-semibold [&_h3]:text-sm [&_h3]:font-semibold [&_ol]:my-1 [&_ol]:ml-5 [&_ol]:list-decimal [&_p]:my-1 [&_strong]:font-semibold [&_ul]:my-1 [&_ul]:ml-5 [&_ul]:list-disc"
                          data-testid="recharge-bonus-notice"
                          dangerouslySetInnerHTML={{ __html: renderedBonusNotice }}
                        ></div>
                      )}
                      <AmountInput
                        value={amount}
                        amounts={[10, 20, 50, 100, 200, 500, 1000, 2000, 5000]}
                        min={globalMinAmount}
                        max={globalMaxAmount}
                        bonusTiers={rechargeBonusTiers}
                        bonusMode={rechargeBonusMode}
                        multiplier={balanceRechargeMultiplier}
                        currency={selectedCurrency}
                        onValueChange={setAmount}
                      />
                      {amountError && <p className="mt-2 text-xs text-amber-600 dark:text-amber-300">{amountError}</p>}
                    </div>
                    <div className="card workspace-shell p-6">
                      <PaymentMethodSelector
                        methods={methodOptions}
                        selected={selectedMethod}
                        onSelect={setSelectedMethod}
                      />
                    </div>
                    {validAmount > 0 && (
                      <div className="card workspace-shell p-6">
                        <div className="space-y-2 text-sm">
                          <div className="flex justify-between">
                            <span className="surface-muted">{t('payment.paymentAmount')}</span>
                            <span className={discountAmount > 0 ? 'text-gray-400 line-through dark:text-gray-500' : 'text-gray-900 dark:text-white'}>{formatSelectedPaymentAmount(validAmount)}</span>
                          </div>
                          {discountAmount > 0 && (
                            <div className="flex justify-between" data-testid="recharge-discount-row">
                              <span className="surface-muted">{t('payment.rechargeBonus.discountLabelWithPercent', { percent: formatRechargeBonusNumber(bonusQuote.percent) })}</span>
                              <span className="font-medium text-red-600 dark:text-red-400">-{formatSelectedPaymentAmount(discountAmount)}</span>
                            </div>
                          )}
                          {feeRate > 0 && (
                            <div className="flex justify-between">
                              <span className="surface-muted">{t('payment.fee')} ({feeRate}%)</span>
                              <span className="text-gray-900 dark:text-white">{formatSelectedPaymentAmount(feeAmount)}</span>
                            </div>
                          )}
                          {showActualPay && (
                            <div className="flex justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
                              <span className="font-medium surface-muted">{t('payment.actualPay')}</span>
                              <span className="text-lg font-bold text-primary-600 dark:text-primary-400">{formatSelectedPaymentAmount(totalAmount)}</span>
                            </div>
                          )}
                          {showBonusRow && (
                            <div className={cn('flex justify-between', !showActualPay && 'border-t border-gray-200 pt-2 dark:border-dark-600')} data-testid="recharge-bonus-row">
                              <span className="surface-muted">{t('payment.rechargeBonus.amountLabelWithPercent', { percent: formatRechargeBonusNumber(bonusQuote.percent) })}</span>
                              <span className="font-medium text-red-600 dark:text-red-400">+${bonusQuote.bonus.toFixed(2)}</span>
                            </div>
                          )}
                          {showCreditedBalance && (
                            <div className={cn('flex justify-between', !showActualPay && !showBonusRow && 'border-t border-gray-200 pt-2 dark:border-dark-600')} data-testid="recharge-credited-row">
                              <span className="surface-muted">{t('payment.creditedBalance')}</span>
                              <span className={bonusQuote.percent > 0 ? 'font-semibold text-gray-900 dark:text-white' : 'text-gray-900 dark:text-white'}>${creditedAmount.toFixed(2)}</span>
                            </div>
                          )}
                          {balanceRechargeMultiplier !== 1 && (
                            <p className="border-t border-gray-200 pt-2 text-xs dark:border-dark-600 surface-muted">
                              {t('payment.rechargeRatePreview', { currency: selectedCurrency, usd: balanceRechargeMultiplier.toFixed(2) })}
                            </p>
                          )}
                        </div>
                      </div>
                    )}
                  </>
                )}
              </>
            ) : activeTab === 'subscription' ? (
              <>
                {/* Subscription confirm (inline, replaces plan list) */}
                {selectedPlan ? (
                  <>
                    <div className="card workspace-shell p-5">
                      {/* Header: platform badge + plan name */}
                      <div className="mb-3 flex flex-wrap items-center gap-2">
                        <span className={cn('rounded-md border px-2 py-0.5 text-xs font-medium', planBadgeClass)}>
                          {platformLabel(selectedPlan.group_platform || '')}
                        </span>
                        <h3 className="text-lg font-bold text-gray-900 dark:text-white">{selectedPlan.name}</h3>
                      </div>
                      {/* Price */}
                      <div className="flex items-baseline gap-2">
                        {selectedPlan.original_price && (
                          <span className="text-sm line-through surface-muted">
                            {formatSelectedSubscriptionPaymentAmount(selectedPlan.original_price)}
                          </span>
                        )}
                        <span className={cn('text-3xl font-bold', planTextClass)}>{formatSelectedSubscriptionPaymentAmount(selectedPlan.price)}</span>
                        <span className="text-sm surface-muted">/ {planValiditySuffix}</span>
                      </div>
                      {/* Description */}
                      {selectedPlan.description && (
                        <p className="mt-2 text-sm leading-relaxed surface-muted">
                          {selectedPlan.description}
                        </p>
                      )}
                      {/* Rate + Limits grid */}
                      <div className="mt-3 grid grid-cols-2 gap-3">
                        <div>
                          <span className="text-xs surface-muted">{t('payment.planCard.rate')}</span>
                          <div className="flex items-baseline">
                            <span className={cn('text-lg font-bold', planTextClass)}>{planRateLabel(selectedPlan) ?? t('billingRates.unknown')}</span>
                          </div>
                        </div>
                        {planHasPeakRate(selectedPlan) && (
                          <div>
                            <span className="text-xs surface-muted">{t('payment.planCard.peakRate')}</span>
                            <div className="text-sm font-semibold text-amber-700 dark:text-amber-300">
                              {planPeakRateLabel(selectedPlan)}
                            </div>
                          </div>
                        )}
                        {selectedPlan.daily_limit_usd != null && (
                          <div>
                            <span className="text-xs surface-muted">{t('payment.planCard.dailyLimit')}</span>
                            <div className="text-lg font-semibold text-gray-800 dark:text-gray-200">${selectedPlan.daily_limit_usd}</div>
                          </div>
                        )}
                        {selectedPlan.weekly_limit_usd != null && (
                          <div>
                            <span className="text-xs surface-muted">{t('payment.planCard.weeklyLimit')}</span>
                            <div className="text-lg font-semibold text-gray-800 dark:text-gray-200">${selectedPlan.weekly_limit_usd}</div>
                          </div>
                        )}
                        {selectedPlan.monthly_limit_usd != null && (
                          <div>
                            <span className="text-xs surface-muted">{t('payment.planCard.monthlyLimit')}</span>
                            <div className="text-lg font-semibold text-gray-800 dark:text-gray-200">${selectedPlan.monthly_limit_usd}</div>
                          </div>
                        )}
                        {selectedPlan.five_hour_limit_usd != null && (
                          <div>
                            <span className="text-xs surface-muted">{t('payment.planCard.fiveHourLimit')}</span>
                            <div className="text-lg font-semibold text-gray-800 dark:text-gray-200">&#36;{selectedPlan.five_hour_limit_usd}</div>
                          </div>
                        )}
                        {selectedPlan.daily_limit_usd == null && selectedPlan.weekly_limit_usd == null && selectedPlan.monthly_limit_usd == null && selectedPlan.five_hour_limit_usd == null && (
                          <div>
                            <span className="text-xs surface-muted">{t('payment.planCard.quota')}</span>
                            <div className="text-lg font-semibold text-gray-800 dark:text-gray-200">{t('payment.planCard.unlimited')}</div>
                          </div>
                        )}
                      </div>
                    </div>
                    <PlanModelCatalog catalog={selectedPlan.model_catalog} />
                    {enabledMethods.length >= 1 && (
                      <div className="card workspace-shell p-6">
                        <PaymentMethodSelector
                          methods={subMethodOptions}
                          selected={selectedMethod}
                          onSelect={setSelectedMethod}
                        />
                      </div>
                    )}
                    {feeRate > 0 && selectedPlan.price > 0 && (
                      <div className="card workspace-shell p-6">
                        <div className="space-y-2 text-sm">
                          <div className="flex justify-between">
                            <span className="surface-muted">{t('payment.amountLabel')}</span>
                            <span className="text-gray-900 dark:text-white">{formatSelectedPaymentAmount(subPaymentAmount)}</span>
                          </div>
                          <div className="flex justify-between">
                            <span className="surface-muted">{t('payment.fee')} ({feeRate}%)</span>
                            <span className="text-gray-900 dark:text-white">{formatSelectedPaymentAmount(subFeeAmount)}</span>
                          </div>
                          <div className="flex justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
                            <span className="font-medium surface-muted">{t('payment.actualPay')}</span>
                            <span className="text-lg font-bold text-primary-600 dark:text-primary-400">{formatSelectedPaymentAmount(subTotalAmount)}</span>
                          </div>
                        </div>
                      </div>
                    )}
                  </>
                ) : (
                  <>
                    {/* Plan list */}
                    {checkout.plans.length === 0 ? (
                      <div className="card workspace-shell py-16 text-center">
                        <Gift size={32} strokeWidth={1.5} className="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
                        <p className="surface-muted">{t('payment.noPlans')}</p>
                      </div>
                    ) : (
                      <div className="payment-plan-grid grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-4">
                        {checkout.plans.map(plan => (
                          <SubscriptionPlanCard
                            key={plan.id}
                            catalog
                            plan={plan}
                            activeSubscriptions={activeSubscriptions}
                            onSelect={selectPlan}
                          />
                        ))}
                      </div>
                    )}
                    {/* Active subscriptions (compact, below plan list) */}
                    {activeSubscriptions.length > 0 && (
                      <div>
                        <p className="mb-2 text-xs font-medium surface-muted">{t('payment.activeSubscription')}</p>
                        <div className="space-y-2">
                          {activeSubscriptions.map((sub) => (
                            <div key={sub.id}
                              className="flex items-center gap-3 rounded-xl border border-gray-100 surface-inset px-3 py-2 dark:border-dark-700">
                              <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-1.5">
                                  <span className="truncate text-xs font-semibold text-gray-900 dark:text-white">{sub.group?.name || t('payment.groupFallback', { id: sub.group_id })}</span>
                                  <span className={cn('shrink-0 rounded-full px-1.5 py-0.5 text-[9px] font-medium', platformBadgeLightClass(sub.group?.platform || ''))}>{platformLabel(sub.group?.platform || '')}</span>
                                </div>
                                <div className="flex flex-wrap gap-x-3 text-[11px] surface-muted">
                                  <span>{t('payment.planCard.rate')}: ×{sub.group?.rate_multiplier ?? 1}</span>
                                  {subscriptionHasPeakRate(sub) && <span>{t('payment.planCard.peakRate')}: {subscriptionPeakRateLabel(sub)}</span>}
                                  {sub.group?.daily_limit_usd == null && sub.group?.weekly_limit_usd == null && sub.group?.monthly_limit_usd == null && sub.group?.five_hour_limit_usd == null && (
                                    <span>{t('payment.planCard.quota')}: {t('payment.planCard.unlimited')}</span>
                                  )}
                                  {sub.expires_at ? <span>{t('userSubscriptions.daysRemaining', { days: getDaysRemaining(sub.expires_at) })}</span> : <span>{t('userSubscriptions.noExpiration')}</span>}
                                </div>
                              </div>
                              <span className="badge badge-success shrink-0 text-[10px]">{t('userSubscriptions.status.active')}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </>
                )}
              </>
            ) : null}
          </>
        )}
        {(checkout.help_text || checkout.help_image_url) && paymentPhase === 'select' && !selectedPlan && (
          <div className="card workspace-shell p-4">
            <div className="flex flex-col items-center gap-3">
              {checkout.help_image_url && (
                <img src={checkout.help_image_url} alt=""
                  className="h-40 max-w-full cursor-pointer rounded-lg object-contain transition-opacity hover:opacity-80"
                  onClick={() => setPreviewImage(checkout.help_image_url)} />
              )}
              {checkout.help_text && (
                <div className="markdown-body w-full overflow-x-auto break-words" dangerouslySetInnerHTML={{ __html: renderedHelpText }}></div>
              )}
            </div>
          </div>
        )}
        {checkout.redeem_purchase_enabled && checkout.redeem_purchase_url && paymentPhase === 'select' && !selectedPlan && (
          <div className="card workspace-shell flex items-center justify-between gap-4 p-5">
            <div className="min-w-0 flex-1">
              <h2 className="text-lg font-semibold">{t('featureExtensions.purchaseTitle')}</h2>
              <p className="mt-2 text-sm surface-muted">{t('featureExtensions.purchaseDescription')}</p>
            </div>
            {checkout.redeem_purchase_mode === 'iframe' ? (
              <Link to="/purchase/redeem-shop" className="btn btn-primary shrink-0 whitespace-nowrap">{t('featureExtensions.purchaseButton')}</Link>
            ) : (
              <a href={checkout.redeem_purchase_url} target="_blank" rel="noopener noreferrer" className="btn btn-primary shrink-0 whitespace-nowrap">{t('featureExtensions.purchaseButton')}</a>
            )}
          </div>
        )}
      </div>
      {paymentPhase === 'select' && tabs.length > 0 && activeTab === 'recharge' && enabledMethods.length > 0 && (
        <div
          className="card workspace-shell workspace-fixed payment-checkout-action p-4"
          data-testid="recharge-checkout-action"
        >
          <button
            className={cn('btn w-full py-3 text-base font-medium', paymentButtonClass)}
            disabled={supportReadonly || !canSubmit || submitting}
            onClick={() => { void handleSubmitRecharge() }}
          >
            {submitting ? (
              <span className="flex items-center justify-center gap-2">
                <span className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                {t('common.processing')}
              </span>
            ) : (
              <span>{t('payment.createOrder')} {formatSelectedPaymentAmount(totalAmount)}</span>
            )}
          </button>
        </div>
      )}
      {paymentPhase === 'select' && activeTab === 'subscription' && selectedPlan && (
        <div
          className="card workspace-shell workspace-fixed payment-checkout-action flex flex-col gap-2 p-4"
          data-testid="subscription-checkout-action"
        >
          <button
            className={cn('btn w-full py-3 text-base font-medium', paymentButtonClass)}
            disabled={supportReadonly || !canSubmitSubscription || submitting}
            onClick={() => { void confirmSubscribe() }}
          >
            {submitting ? (
              <span className="flex items-center justify-center gap-2">
                <span className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                {t('common.processing')}
              </span>
            ) : (
              <span>{t('payment.createOrder')} {formatSelectedPaymentAmount(subTotalAmount)}</span>
            )}
          </button>
          <button className="btn btn-secondary w-full" onClick={() => updateSelectedPlan(null)}>{t('common.cancel')}</button>
        </div>
      )}
      {/* Renewal Plan Selection Modal */}
      {showRenewalModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 surface-overlay" onClick={closeRenewalModal}>
          <div className="glass-surface relative flex max-h-full w-full max-w-lg flex-col border p-6" onClick={(event) => event.stopPropagation()}>
            {/* Close button */}
            <button className="absolute right-4 top-4 rounded-lg p-1 transition-colors hover:text-gray-600 dark:hover:text-gray-200 surface-control surface-muted" onClick={closeRenewalModal}>
              <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2"><path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
            <h3 className="mb-4 shrink-0 text-lg font-semibold text-gray-900 dark:text-white">{t('payment.selectPlan')}</h3>
            <div className="min-h-0 space-y-4 overflow-y-auto">
              {renewalPlans.map(plan => (
                <SubscriptionPlanCard
                  key={plan.id}
                  plan={plan}
                  activeSubscriptions={activeSubscriptions}
                  onSelect={selectPlanFromModal}
                />
              ))}
            </div>
          </div>
        </div>
      )}
      {/* Image Preview Overlay */}
      {previewImage && (
        <div className="fixed inset-0 z-[60] flex items-center justify-center surface-overlay" onClick={() => setPreviewImage('')}>
          <img src={previewImage} alt="" className="max-h-[85vh] max-w-[90vw] rounded-xl object-contain shadow-2xl" />
        </div>
      )}
    </div>
  )
}
