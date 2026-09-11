import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 应用版本号:唯一真源是仓库根的 VERSION 文件(格式「赛季.大更新.小更新」,递增规则见 AGENTS.md)。
// 构建时读入并注入前端 —— 界面显示的版本因此与仓库里那份永远一致,不必手工同步两处。
const VERSION = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim()

// 构建产物直接输出到 Go embed 目录，dev 时把 /api 与 /api/stream 代理到后端。
export default defineConfig({
  // 注入为编译期常量(前端直接写 __APP_VERSION__;已登记进 eslint.config.js 的 globals)。
  define: { __APP_VERSION__: JSON.stringify(VERSION) },
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
