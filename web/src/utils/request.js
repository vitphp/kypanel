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
request.interceptors.request.use((config) => {
  const auth = useAuthStore()
  if (auth.token) {
    config.headers.Authorization = `Bearer ${auth.token}`
  }
  return config
})

// 响应拦截：统一处理业务错误与 401
request.interceptors.response.use(
  (response) => {
    const res = response.data
    if (res.code !== 0) {
      ElMessage.error(res.msg || '请求失败')
      return Promise.reject(new Error(res.msg || '请求失败'))
    }
    return res
  },
  (error) => {
    if (error.response?.status === 401) {
      const auth = useAuthStore()
      auth.logout()
      // 401 后跳安全入口（登录页），/login 已不可访问
      const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
      router.push(entrance ? '/' + entrance : '/')
    } else {
      ElMessage.error(error.response?.data?.msg || error.message || '网络错误')
    }
    return Promise.reject(error)
  }
)

export default request
