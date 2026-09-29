import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5174,
    // 开发时把接口转到本机后端
    proxy: {
      '/api': { target: 'http://127.0.0.1:8800', changeOrigin: true, xfwd: true, ws: true },
      '/healthz': { target: 'http://127.0.0.1:8800', changeOrigin: true, xfwd: true },
      '/starrocks-webroot': { target: 'http://127.0.0.1:8800', changeOrigin: true, xfwd: true },
    },
  },
  preview: {
    host: '127.0.0.1',
    port: 5174,
  },
})
