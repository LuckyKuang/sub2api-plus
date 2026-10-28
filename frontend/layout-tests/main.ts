import { createApp, h } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../src/i18n/locales/en'
import zh from '../src/i18n/locales/zh'
import '../src/style.css'
import LayoutFixture from './LayoutFixture.vue'
import { useConsoleAppearance } from '../src/composables/useConsoleAppearance'
import { defaultConsoleAppearance } from '../src/types/appearance'

const parameters = new URLSearchParams(location.search)
const locale = parameters.get('locale') === 'zh' ? 'zh' : 'en'
document.documentElement.lang = locale
document.documentElement.classList.toggle('dark', parameters.get('theme') === 'dark')
const appearance = {
  ...defaultConsoleAppearance(),
  accent_color: parameters.get('accent') ?? defaultConsoleAppearance().accent_color,
  card_transparency: 100 * (1 - Number(parameters.get('alpha') ?? '1')),
}

createApp({
  setup() {
    useConsoleAppearance(() => appearance)
    return () => h(LayoutFixture)
  },
}).use(createI18n({
  legacy: false,
  locale,
  fallbackLocale: 'en',
  messages: { en, zh },
  warnHtmlMessage: false,
})).mount('#app')
