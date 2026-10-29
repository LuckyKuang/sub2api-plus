import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'
import MotionFixture from './MotionFixture.vue'
import en from '../src/i18n/locales/en'
import zh from '../src/i18n/locales/zh'
import '../src/style.css'

const parameters = new URLSearchParams(location.search)
document.documentElement.classList.toggle('dark', parameters.get('theme') === 'dark')
const router = createRouter({
  history: createWebHistory(),
  routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null }, meta: { title: 'Motion review' } }],
})
createApp(MotionFixture).use(createPinia()).use(router).use(createI18n({
  legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en, zh }, warnHtmlMessage: false,
})).mount('#app')
