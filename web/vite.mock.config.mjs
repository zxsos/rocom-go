// 临时:mock 页的构建配置(只为「打开看一眼」用,看完即删)。
// 与主配置的差别:产物输出到 mock-dist、base 改成相对路径(这样随便起个静态服务器都能开)、
// 输入是 mock.html 而不是 index.html。
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: 'mock-dist',
    emptyOutDir: true,
    rollupOptions: { input: 'mock.html' },
  },
})
