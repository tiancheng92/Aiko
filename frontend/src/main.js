import { createApp } from 'vue'
import App from './App.vue'
import './styles/tokens.css'
import './style.css'

const app = createApp(App)

// Set theme from backend config (with fallback for dev/build)
import('./wailsjs/go/main/App').then(({ GetConfig }) => {
  GetConfig().then(cfg => {
    document.documentElement.dataset.theme = cfg.ThemeStyle || 'liquid-glass'
  }).catch(() => {
    document.documentElement.dataset.theme = 'liquid-glass'
  })
}).catch(() => {
  // Bindings not available (e.g., during build), use default
  document.documentElement.dataset.theme = 'liquid-glass'
})

app.mount('#app')
