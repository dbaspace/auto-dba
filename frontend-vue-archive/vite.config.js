import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5174,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8800', changeOrigin: true, xfwd: true },
      '/healthz': { target: 'http://127.0.0.1:8800', changeOrigin: true, xfwd: true },
    },
  },
})
