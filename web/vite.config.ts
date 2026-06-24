import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Dev server proxies /admin to the Go API so cookies stay same-origin.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/admin': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
