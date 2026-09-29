import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  // 与 nginx 的 /week04/ 位置对应；开发服务器同样挂在 /week04 下，保证前后端前缀一致。
  base: '/week04/',
  plugins: [react()],
  server: { proxy: { '/api': 'http://localhost:8081', '/health': 'http://localhost:8081' } }
})
