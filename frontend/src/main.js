import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './style.css'
import { GetConfig } from './wailsjs/go/main/App'

GetConfig().then(cfg => {
  document.documentElement.dataset.theme = cfg.ThemeStyle || 'liquid-glass'
}).catch(() => {
  document.documentElement.dataset.theme = 'liquid-glass'
})

createApp(App).mount('#app')
