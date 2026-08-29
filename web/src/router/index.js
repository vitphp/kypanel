import { createRouter, createWebHistory } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import progress from '../utils/progress'

const routes = [
  {
    path: '/temp-login',
    name: 'TempLogin',
    component: () => import('../views/TempLogin.vue'),
    meta: { public: true }
  },
  {
    path: '/',
    component: () => import('../layout/AdminLayout.vue'),
    children: [
      {
        path: '',
        redirect: '/dashboard'
      },
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('../views/Dashboard.vue'),
        meta: { title: '概览', icon: 'Odometer' }
      },
      {
        path: 'websites',
        name: 'Website',
        component: () => import('../views/Website.vue'),
        meta: { title: '网站', icon: 'Monitor' }
      },
      {
        path: 'database',
        name: 'Database',
        component: () => import('../views/Database.vue'),
        meta: { title: '数据库', icon: 'Coin' }
      },
      {
        path: 'backup',
        name: 'Backup',
        component: () => import('../views/Backup.vue'),
        meta: { title: '备份中心', icon: 'Files' }
      },
      {
        path: 'ftp',
        name: 'Ftp',
        component: () => import('../views/Ftp.vue'),
        meta: { title: 'FTP', icon: 'UploadFilled' }
      },
      // 「文件管理」顶级菜单（与终端并列）
      {
        path: 'files',
        name: 'FileManager',
        component: () => import('../views/FileManager.vue'),
        meta: { title: '文件管理', icon: 'FolderOpened' }
      },
      // 回收站：独立顶级路由但菜单隐藏（方便从 FileManager 进入，URL 仍是 /files/trash）
      {
        path: 'files/trash',
        name: 'FileTrash',
        component: () => import('../views/Trash.vue'),
        meta: { title: '回收站', icon: 'Delete', hidden: true }
      },
      {
        path: 'container',
        name: 'Container',
        component: () => import('../views/Container.vue'),
        meta: { title: '容器', icon: 'Box' }
      },
      {
        path: 'apps',
        name: 'AppStore',
        component: () => import('../views/AppStore.vue'),
        meta: { title: '应用商店', icon: 'Grid' }
      },
      {
        path: 'cron',
        name: 'Cron',
        component: () => import('../views/Cron.vue'),
        meta: { title: '计划任务', icon: 'Timer' }
      },
      {
        path: 'logs',
        name: 'Logs',
        component: () => import('../views/Logs.vue'),
        meta: { title: '日志', icon: 'Document' }
      },
      {
        path: 'monitor',
        name: 'Monitor',
        component: () => import('../views/Monitor.vue'),
        meta: { title: '监控', icon: 'TrendCharts' }
      },
      {
        path: 'process',
        name: 'Process',
        component: () => import('../views/Process.vue'),
        meta: { title: '进程管理', icon: 'Cpu' }
      },
      {
        path: 'firewall',
        name: 'Firewall',
        component: () => import('../views/Firewall.vue'),
        meta: { title: '防火墙', icon: 'Lock' }
      },
      {
        path: 'settings',
        name: 'Settings',
        component: () => import('../views/Settings.vue'),
        meta: { title: '设置', icon: 'Setting' }
      },
      {
        path: 'users',
        name: 'Users',
        component: () => import('../views/Users.vue'),
        meta: { title: '用户管理', icon: 'User' }
      }
    ]
  },
  // 兼容旧路径，重定向到新的顶级路由
  { path: '/system', redirect: '/monitor' },
  { path: '/system/monitor', redirect: '/monitor' },
  { path: '/system/process', redirect: '/process' },
  { path: '/system/firewall', redirect: '/firewall' },
  { path: '/security', redirect: '/firewall' },
  // 旧「文件」分组路径兼容
  { path: '/files/manager', redirect: '/files' },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard'
  }
]

// 安全入口：后端把 entrance 注入 window.__SECURITY_ENTRANCE__。
// 入口本身就是登录页 URL（如 /jXcCn7），登录页路径对未登录用户完全隐藏，
// 后端未登录访问任何不带入口的路径（含 /login）一律 404。
// 因此把入口路径动态注册为登录页路由，这样访问 /<entrance> 时地址栏保持入口不变，
// 且能正确渲染登录页；前端内部任何跳登录页的逻辑（包括退登）都必须用入口路径。
const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
if (entrance) {
  const loginComponent = () => import('../views/Login.vue')
  routes.push(
    { path: '/' + entrance, name: 'Entrance', component: loginComponent, meta: { public: true } },
    { path: '/' + entrance + '/login', name: 'EntranceLogin', component: loginComponent, meta: { public: true } }
  )
}

const router = createRouter({
  history: createWebHistory(),
  routes
})

// 全局路由守卫：未登录跳转登录页（安全入口 /<entrance>）；同时驱动顶部绿色加载进度条
router.beforeEach((to) => {
  progress.start()
  const auth = useAuthStore()
  // 登录页的对外地址：永远走安全入口（/<entrance>），登录页路径不暴露
  const loginPath = entrance ? '/' + entrance : '/login'
  if (!to.meta.public && !auth.isLoggedIn()) {
    // 未登录访问非公开页：直接落到登录入口（不带 redirect 参数，避免 URL 污染与跳转死循环）
    return { path: loginPath }
  }
  // 已登录访问登录入口（/<entrance>）时，直接进入首页
  if ((to.path === loginPath || to.path === '/login') && auth.isLoggedIn()) {
    return { path: '/' }
  }
  return true
})

router.afterEach(() => {
  progress.done()
})

// 懒加载 chunk 加载失败（面板更新后旧页面请求已删除的旧 chunk）：
// 提示用户并自动刷新，避免「点了菜单没反应、要手动刷很久」的卡死体验。
let reloadingForChunk = false
router.onError((err) => {
  progress.done()
  const isChunkError = err && /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module/i.test(err.message || '')
  if (isChunkError && !reloadingForChunk) {
    reloadingForChunk = true
    ElMessage.warning('页面已更新，正在自动刷新…')
    setTimeout(() => { location.reload() }, 600)
  }
})

export default router
