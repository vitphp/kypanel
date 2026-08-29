import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:9999',
        changeOrigin: true
      }
    }
  },
  build: {
    // 产物 webui/dist：go:embed 内嵌进后端二进制；
    // 前后端分离部署时把该文件夹整体上传到服务器 {DataDir}/web（磁盘优先托管）。
    outDir: '../webui/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500
  }
})
