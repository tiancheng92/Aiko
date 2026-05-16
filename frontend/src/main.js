import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './style.css'
import { GetConfig } from '../bindings/aiko/app'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'frosted'
}).catch(() => {
  document.documentElement.dataset.theme = 'frosted'
})

createApp(App).mount('#app')
