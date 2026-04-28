import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-vue':    ['vue'],
          'vendor-pixi':   ['pixi.js', 'pixi-live2d-display/cubism4'],
          'vendor-katex':  ['katex', 'marked-katex-extension'],
          'vendor-marked': ['marked'],
          'vendor-hljs':   [
            'highlight.js/lib/core',
            'highlight.js/lib/languages/javascript',
            'highlight.js/lib/languages/typescript',
            'highlight.js/lib/languages/python',
            'highlight.js/lib/languages/bash',
            'highlight.js/lib/languages/go',
            'highlight.js/lib/languages/json',
            'highlight.js/lib/languages/css',
            'highlight.js/lib/languages/xml',
          ],
        },
      },
    },
  },
})
