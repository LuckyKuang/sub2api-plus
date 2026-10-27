import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../src/i18n/locales/en'
import zh from '../src/i18n/locales/zh'
import '../src/style.css'
import LayoutFixture from './LayoutFixture.vue'
import { consoleAccentPalette, consolePrimaryColors } from '../src/utils/consoleAccent'

const parameters = new URLSearchParams(location.search)
const locale = parameters.get('locale') === 'zh' ? 'zh' : 'en'
document.documentElement.lang = locale
document.documentElement.classList.toggle('dark', parameters.get('theme') === 'dark')
const accent = parameters.get('accent')
for (const [shade, channels] of Object.entries(consoleAccentPalette(accent))) {
  document.documentElement.style.setProperty('--console-accent-' + shade, channels)
}
const primary = consolePrimaryColors(accent)
document.documentElement.style.setProperty('--console-primary-foreground', primary.foreground)
document.documentElement.style.setProperty('--console-primary-end', primary.end)

createApp(LayoutFixture).use(createI18n({
  legacy: false,
  locale,
  fallbackLocale: 'en',
  messages: { en, zh },
  warnHtmlMessage: false,
})).mount('#app')
