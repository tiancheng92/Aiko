import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-vue': ['vue'],
          'vendor-three': ['three', '@pixiv/three-vrm', '@pixiv/three-vrm-animation'],
          'vendor-pixi': ['pixi.js', 'pixi-live2d-display'],
          'vendor-markdown': ['marked', 'marked-katex-extension', 'katex'],
        },
      },
    },
  },
})
