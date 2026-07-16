import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  base: '/admin/',
  plugins: [vue()],
  build: {
    outDir: 'internal/adapter/web/dist',
    emptyOutDir: true
  },
  server: {
    proxy: {
      '/admin/api': 'http://127.0.0.1:18080'
    }
  }
})
