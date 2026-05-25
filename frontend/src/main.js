import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './style.css'
import { GetConfig } from '../wailsjs/go/main/App'
import { detectLocale, createI18nInstance } from './locales/index.js'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'frosted'
  const locale = detectLocale(cfg.Language || '')
  const i18n = createI18nInstance(locale)
  createApp(App).use(i18n).mount('#app')
}).catch(() => {
  document.documentElement.dataset.theme = 'frosted'
  const locale = detectLocale('')
  const i18n = createI18nInstance(locale)
  createApp(App).use(i18n).mount('#app')
})
