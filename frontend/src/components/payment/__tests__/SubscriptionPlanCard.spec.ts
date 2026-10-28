import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import type { SubscriptionPlan } from "@/types/payment";
import { useAppStore } from '@/stores/app'
import SubscriptionPlanCard from "../SubscriptionPlanCard.vue";
import { offer, pricing, rateQuote } from '@/components/channels/__tests__/catalogFixtures'

type NamedMessageContext = { named: (key: string) => unknown }

const i18n = createI18n({
  legacy: false,
  locale: "en",
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      billingRates: {
        asOf: ({ named }: NamedMessageContext) => `Quoted at ${named('time')}`,
        factorBreakdown: ({ named }: NamedMessageContext) => `Base ${named('base')} × platform ${named('platform')} × model ${named('model')} × peak ${named('peak')}`,
        legacyFactorBreakdown: ({ named }: NamedMessageContext) => `Historical base ${named('base')} × policy ${named('policy')} × peak ${named('peak')}`,
      },
      payment: {
        days: "days",
        weeks: "weeks",
        months: "months",
        perMonth: "month",
        models: "Models",
        planCard: {
          quota: "Quota",
          rate: "Rate",
          unlimited: "Unlimited",
        },
        subscribeNow: "Subscribe now",
      },
    },
  },
});

const mountPlanCard = (groupPlatform: string, overrides: Partial<SubscriptionPlan> = {}) =>
  mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1,
        group_id: 10,
        group_platform: groupPlatform,
        name: "Pro",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: ["claude", "gemini_text", "gemini_image"],
        is_active: true,
        ...overrides,
      },
    },
    global: { plugins: [i18n, createPinia()] },
  });

describe("SubscriptionPlanCard", () => {
  it.each([
    { name: 'standard', asOf: '2026-10-26T08:00:00Z', peak: 1, effective: 6, calculation: 'product_v2' as const, scope: 'all_meters' as const, policy: 6, charge: '$12' },
    { name: 'peak', asOf: '2026-10-26T10:00:00Z', peak: 1.5, effective: 9, calculation: 'product_v2' as const, scope: 'all_meters' as const, policy: 6, charge: '$18' },
    { name: 'historical override', asOf: '2026-10-26T10:00:00Z', peak: 1, effective: 3, calculation: 'override_v1' as const, scope: 'token_only' as const, policy: 3, charge: '$6' }
  ])('shows the $name consumption charge, quote time and peak window before purchase', async ({ asOf, peak, effective, calculation, scope, policy, charge }) => {
    const wrapper = mountPlanCard('composite', { rate_multiplier: 99,
      peak_rate_enabled: true, peak_start: '09:00', peak_end: '12:00', peak_rate_multiplier: 1.5,
      model_catalog: { policy_version: 7, as_of: asOf, platforms: ['openai'], rate_min: effective, rate_max: effective,
        models: [offer('model001', { offer_key: 'openai-model001', billing_mode: 'per_request', billing_unit: 'request',
          pricing: { ...pricing(), billing_mode: 'per_request', input_price: null, output_price: null, per_request_price: 2 },
          rate_quote: rateQuote(effective, { base_multiplier: 1, platform_multiplier: 2, model_multiplier: 3, policy_multiplier: policy,
            peak_multiplier: peak, rate_calculation: calculation, peak_scope: scope, model: 'model001', billing_unit: 'request', as_of: asOf }) })] }
    })
    useAppStore().$patch({ cachedPublicSettings: { server_utc_offset: '+00:00' } })
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('09:00-12:00 ×1.5 (UTC+00:00)')
    expect(wrapper.text()).toContain(`×${effective}`)
    expect(wrapper.text()).not.toContain('×99')
    await wrapper.get('details > summary').trigger('click')
    await wrapper.get('details > details > summary').trigger('click')
    expect(wrapper.get('table').findAll('tbody td').map(cell => cell.text())).toEqual([charge])
    expect(wrapper.text()).toContain(`Quoted at ${asOf}`)
    if (calculation === 'override_v1') {
      expect(wrapper.text()).toContain('Historical base 1 × policy 3 × peak 1')
      expect(wrapper.text()).not.toContain('Base 1 × platform 2 × model 3')
      expect(wrapper.get('table').text()).not.toContain('$18')
    } else {
      expect(wrapper.text()).toContain(`Base 1 × platform 2 × model 3 × peak ${peak}`)
    }
    expect(wrapper.emitted('select')).toBeUndefined()
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[wrapper.props('plan')]])
    wrapper.unmount()
  })

  it('shows the backend quote range, including free models, and pre-purchase model details', () => {
    const wrapper = mountPlanCard('composite', { rate_multiplier: 99, model_catalog: { policy_version: 3, as_of: '2026-10-26T12:00:00Z', models: [offer('free', { rate_quote: rateQuote(0) }), offer('paid', { platform: 'stepfun', rate_quote: rateQuote(2) })], platforms: ['openai', 'stepfun'], rate_min: 0, rate_max: 2 } })
    expect(wrapper.text()).toContain('×0–×2')
    expect(wrapper.text()).not.toContain('×99')
    expect(wrapper.get('details summary').text()).toContain('billingRates.models')
  })
  it('shows unavailable instead of a default multiplier when sale quotes are absent or empty', () => {
    const missing = mountPlanCard('composite', { rate_multiplier: 1 })
    expect(missing.text()).toContain('billingRates.unknown')
    expect(missing.text()).not.toContain('×1')
    const empty = mountPlanCard('composite', { rate_multiplier: 1, model_catalog: { policy_version: 3, as_of: '', models: [], platforms: [], rate_min: null, rate_max: null } })
    expect(empty.text()).toContain('billingRates.unknown')
    expect(empty.text()).not.toContain('×1')
    missing.unmount()
    empty.unmount()
  })
  it("keeps the catalog purchase action at the card bottom and emits the unchanged selected plan", async () => {
    const wrapper = mountPlanCard("openai", { features: ["Priority support", "Advanced models"] });
    await wrapper.setProps({ catalog: true });
    const action = wrapper.get("button");
    expect(wrapper.classes()).toContain("subscription-plan-catalog");
    expect(action.classes()).toEqual(expect.arrayContaining(["mt-auto", "min-h-11", "w-full", "font-semibold"]));
    expect(action.element.parentElement?.lastElementChild).toBe(action.element);
    await action.trigger("click");
    expect(wrapper.emitted("select")).toEqual([[wrapper.props("plan")]]);
  });

  it("does not show Antigravity model scopes for OpenAI plans", () => {
    const text = mountPlanCard("openai").text();

    expect(text).not.toContain("Claude");
    expect(text).not.toContain("Gemini");
    expect(text).not.toContain("Imagen");
  });

  it("shows model scopes for Antigravity plans", () => {
    const text = mountPlanCard("antigravity").text();

    expect(text).toContain("Claude");
    expect(text).toContain("Gemini");
    expect(text).toContain("Imagen");
  });

  // #4607：管理端保存的单位是复数（months/weeks），此前用户侧只匹配单数
  // 'month'，「1 个月」的套餐卡片被显示成「1天」。测试环境的 vue-i18n 为
  // runtime-only 构建，t() 原样返回 key，故按 key 断言单位分支。
  it("renders plural admin-form validity units instead of mislabeled days (#4607)", () => {
    expect(mountPlanCard("openai", { validity_days: 1, validity_unit: "months" }).text()).toContain("/ payment.perMonth");
    expect(mountPlanCard("openai", { validity_days: 3, validity_unit: "months" }).text()).toContain("/ 3payment.months");
    expect(mountPlanCard("openai", { validity_days: 2, validity_unit: "weeks" }).text()).toContain("/ 2payment.weeks");
    expect(mountPlanCard("openai", { validity_days: 30, validity_unit: "day" }).text()).toContain("/ 30payment.days");
  });

  it("uses the configured currency symbol while preserving USD for legacy plans", () => {
    const cnyPlan = mountPlanCard("openai", { currency: "CNY", original_price: 20 }).text();

    expect(cnyPlan).toContain("¥10CNY");
    expect(cnyPlan).toContain("¥20CNY");
    expect(mountPlanCard("openai", { currency: "USD" }).text()).toContain("$10USD");
    expect(mountPlanCard("openai", { currency: "" }).text()).toContain("$10");
  });

  it.each([
    ["long Chinese", "企业全球加速专业订阅套餐（含高级模型与优先支持）"],
    ["long English", "Enterprise Global Acceleration Subscription with Priority Support"],
    ["unbroken token", "EnterpriseGlobalAccelerationSubscriptionWithPrioritySupport1234567890"],
  ])("keeps the full %s plan title accessible in a bounded two-line area", (_label, name) => {
    const wrapper = mountPlanCard("openai", { name });
    const title = wrapper.get("h3");

    expect(title.text()).toBe(name);
    expect(title.attributes("title")).toBe(name);
    expect(title.classes()).toEqual(expect.arrayContaining([
      "min-w-0",
      "h-12",
      "break-words",
      "line-clamp-2",
      "[overflow-wrap:anywhere]",
    ]));
    expect(title.classes()).not.toContain("truncate");
  });

  it("keeps title, badge, price, description, and purchase action in separate bounded regions", () => {
    const wrapper = mountPlanCard("openai", {
      name: "Enterprise Global Acceleration Subscription with Priority Support",
      price: 123.45,
      currency: "USD",
      description: "Includes advanced models and priority support.",
    });
    const title = wrapper.get("h3");
    const badge = wrapper.findAll("span").find((node) => node.text() === "OpenAI");
    const price = wrapper.findAll("span").find((node) => node.text() === "123.45");

    expect(title.element.parentElement?.classList).toContain("min-w-0");
    expect(title.element.parentElement?.classList).toContain("flex-1");
    expect(badge?.classes()).toContain("shrink-0");
    expect([...(badge?.element.parentElement?.classList ?? [])]).toEqual(expect.arrayContaining([
      "flex",
      "items-center",
      "justify-end",
    ]));
    expect(badge?.element.parentElement?.textContent).toContain("/ 30payment.days");
    expect(badge?.element.parentElement?.parentElement?.classList).toContain("shrink-0");
    expect(price?.element.parentElement?.parentElement?.classList).toContain("shrink-0");
    expect(wrapper.get("p").text()).toBe("Includes advanced models and priority support.");
    expect(wrapper.get("button").text()).toBe("payment.subscribeNow");
  });

  it("keeps short plan titles compact and aligned", () => {
    const wrapper = mountPlanCard("openai", { name: "Pro", description: "" });
    const title = wrapper.get("h3");
    const badge = wrapper.findAll("span").find((node) => node.text() === "OpenAI");

    expect(title.text()).toBe("Pro");
    expect(title.attributes("title")).toBe("Pro");
    expect(title.classes()).toEqual(expect.arrayContaining(["text-base", "font-bold", "h-12"]));
    expect([...(badge?.element.parentElement?.classList ?? [])]).toEqual(expect.arrayContaining([
      "flex",
      "items-center",
      "justify-end",
    ]));
    expect(badge?.element.parentElement?.textContent).toContain("/ 30payment.days");
  });
});
