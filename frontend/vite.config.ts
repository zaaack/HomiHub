import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': { target: 'http://localhost:8099', changeOrigin: true },
      '/dav': { target: 'http://localhost:8099', changeOrigin: true },
      '/.well-known': { target: 'http://localhost:8099', changeOrigin: true },
    },
  },
})
