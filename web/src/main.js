import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import * as Icons from '@element-plus/icons-vue'

import App from './App.vue'
import router from './router'
import './styles/index.css'
import Skeleton from './components/Skeleton.vue'

// 安全入口（/<entrance>）即登录页，相关处理在 router/index.js 中：
// 把入口路径动态注册为登录页路由，登录页对外地址统一用入口，不暴露 /login。

const app = createApp(App)

// 全局注册图标组件
for (const [name, comp] of Object.entries(Icons)) {
  app.component(name, comp)
}
// 全局注册骨架屏组件
app.component('Skeleton', Skeleton)

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn })
app.mount('#app')
