import axios from 'axios'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import router from '../router'

const request = axios.create({
  baseURL: '/api',
  // 大文件上传（如备份到远程存储）可能很慢，默认 30 秒不够，
  // 设为 2 小时覆盖各种慢场景（包括 GB 级备份传输）。
  timeout: 2 * 60 * 60 * 1000
})

// 请求拦截：附加 JWT
// 注意：若调用方已显式指定 Authorization（如临时登录用临时 token 校验），
// 不覆盖——否则本地残留的登录 JWT 会顶掉临时 token，导致临时链接使用计数失效。
request.interceptors.request.use((config) => {
  const auth = useAuthStore()
  if (auth.token && !config.headers?.Authorization) {
    config.headers.Authorization = `Bearer ${auth.token}`
  }
  return config
})

// 响应拦截：统一处理业务错误与 401
request.interceptors.response.use(
  (response) => {
    // 任一请求成功即说明会话恢复正常，解除 401 重定向锁，允许后续再发生 401 时重新跳转。
    request._authRedirecting = false
    const res = response.data
    if (res.code !== 0) {
      if (!response.config?.silent) {
        ElMessage.error(res.msg || '请求失败')
      }
      return Promise.reject(new Error(res.msg || '请求失败'))
    }
    return res
  },
  (error) => {
    if (error.config?.silent) {
      return Promise.reject(error)
    }
    if (error.response?.status === 401) {
      // 多请求并行同时 401 时只处理一次：已跳转则不重复 logout / push，
      // 避免一次登出触发 N 次导航、产生 unhandled rejection 或路由竞态。
      if (!request._authRedirecting) {
        request._authRedirecting = true
        const auth = useAuthStore()
        auth.logout()
        // 401 后跳安全入口（登录页），/login 已不可访问
        const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
        router.push(entrance ? '/' + entrance : '/').catch(() => {})
      }
    } else {
      ElMessage.error(error.response?.data?.msg || error.message || '网络错误')
    }
    return Promise.reject(error)
  }
)

export default request
