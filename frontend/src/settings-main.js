import { createApp } from 'vue'
import SettingsApp from './SettingsApp.vue'
import './styles/tokens.css'
import './style.css'
import { GetConfig } from '../bindings/aiko/internal/services/configservice'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'frosted'
}).catch(() => {
  document.documentElement.dataset.theme = 'frosted'
})

createApp(SettingsApp).mount('#settings-app')
