import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 8191,
    host: '0.0.0.0',
    proxy: {
      '^/portal/': 'http://127.0.0.1:8190',
      '^/api/v1/': 'http://127.0.0.1:8190',
      '^/api/usage/': 'http://127.0.0.1:8190',
      '^/api/billing/': 'http://127.0.0.1:8190',
      '^/api/personas/': 'http://127.0.0.1:8190',
      '^/api/memories': 'http://127.0.0.1:8190',
      '^/api/sessions/': 'http://127.0.0.1:8190',
      '^/api/mcp/': 'http://127.0.0.1:8190',
      '^/api/knowledge-bases': 'http://127.0.0.1:8190',
      '^/api/anomaly': 'http://127.0.0.1:8190',
      '^/v1/': 'http://127.0.0.1:8190',
      '^/internal/': 'http://127.0.0.1:8190',
    },
  },
})