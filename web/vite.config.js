import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 构建产物直接输出到 Go embed 目录，dev 时把 /api 与 /api/stream 代理到后端。
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/server/web',
    emptyOutDir: true,
    // 路由级分包(main.jsx 的 React.lazy)之外的补充:把 React 全家桶单独拆 chunk,
    // 让浏览器长缓存复用(配合 handleStatic 对 hash 产物返回 immutable),
    // 页面 chunk 相互独立,新增页面不使旧缓存失效。
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom', 'react-router-dom'],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': { target: 'http://localhost:4939', changeOrigin: true },
      '/img': { target: 'http://localhost:4939', changeOrigin: true },
    },
  },
})
