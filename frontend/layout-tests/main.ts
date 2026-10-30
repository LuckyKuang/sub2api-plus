import { createApp, defineAsyncComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../src/i18n/locales/en'
import zh from '../src/i18n/locales/zh'
import '../src/style.css'
import LayoutFixture from './LayoutFixture.vue'
import { useConsoleAppearance } from '../src/composables/useConsoleAppearance'
import { defaultConsoleAppearance } from '../src/types/appearance'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'
import { useProductAppearance } from '../src/composables/useProductAppearance'
import { useStaticTableHeaders } from '../src/composables/useStaticTableHeaders'
import { useTransparentTableClipping } from '../src/composables/useTransparentTableClipping'

const parameters = new URLSearchParams(location.search)
const locale = parameters.get('locale') === 'zh' ? 'zh' : 'en'
document.documentElement.lang = locale
document.documentElement.classList.toggle('dark', parameters.get('theme') === 'dark')
const appearance = {
  ...defaultConsoleAppearance(),
  accent_color: parameters.get('accent') ?? defaultConsoleAppearance().accent_color,
  card_transparency: 100 * (1 - Number(parameters.get('alpha') ?? '1')),
}

const pinia = createPinia()
const UserLayoutFixture = defineAsyncComponent(() => import('./UserLayoutFixture.vue'))
const AdminLayoutFixture = defineAsyncComponent(() => import('./AdminLayoutFixture.vue'))
const router = createRouter({ history: createWebHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null }, meta: { title: 'Review' } }] })

createApp({
  setup() {
    useConsoleAppearance(() => appearance)
    useProductAppearance(() => parameters.get('product') === '1')
    // App.vue owns these document-wide table behaviors in production.
    const tableRoot = ref<HTMLElement | null>(document.body)
    useStaticTableHeaders(tableRoot)
    useTransparentTableClipping(tableRoot, () => true)
    return () => h(parameters.get('view') === 'admin-layout' ? AdminLayoutFixture : parameters.get('view') === 'user-layout' ? UserLayoutFixture : LayoutFixture)
  },
}).use(pinia).use(router).use(createI18n({
  legacy: false,
  locale,
  fallbackLocale: 'en',
  messages: { en, zh },
  warnHtmlMessage: false,
})).mount('#app')
