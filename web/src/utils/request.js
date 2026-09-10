import { ElMessage } from 'element-plus'
import { useAuthStore } from '../stores/auth'
import router from '../router'

// 轻量 HTTP 客户端：只用浏览器原生 fetch / XMLHttpRequest，不依赖第三方库。
// 调用签名与错误形状与 axios 对齐（错误对象带 response.status / response.data），
// 因为调用方普遍通过 e.response?.data?.msg 取后端提示。

const BASE_URL = '/api'
// 大文件上传（如备份到远程存储）可能很慢，默认 30 秒不够，设为 2 小时。
const DEFAULT_TIMEOUT = 2 * 60 * 60 * 1000

class HttpError extends Error {
  constructor(message, status, data, config) {
    super(message)
    this.name = 'HttpError'
    this.response = { status, data }
    this.config = config
  }
}

function isFormData(v) {
  return typeof FormData !== 'undefined' && v instanceof FormData
}

// 非对象请求体（字符串 / URLSearchParams / Blob / ArrayBuffer）直接透传
function isRawBody(v) {
  if (typeof v === 'string') return true
  if (typeof URLSearchParams !== 'undefined' && v instanceof URLSearchParams) return true
  if (typeof Blob !== 'undefined' && v instanceof Blob) return true
  if (typeof ArrayBuffer !== 'undefined' && (v instanceof ArrayBuffer || ArrayBuffer.isView(v))) return true
  return false
}

function buildBody(data) {
  if (data === undefined || data === null) return undefined
  if (isFormData(data) || isRawBody(data)) return data
  return JSON.stringify(data)
}

// 查询串：跳过 null/undefined，数组按重复键展开
function buildQuery(params) {
  if (!params) return ''
  const parts = []
  for (const key of Object.keys(params)) {
    const v = params[key]
    if (v === null || v === undefined) continue
    for (const item of Array.isArray(v) ? v : [v]) {
      if (item === null || item === undefined) continue
      parts.push(encodeURIComponent(key) + '=' + encodeURIComponent(item))
    }
  }
  return parts.join('&')
}

function resolveUrl(url, params, baseURL) {
  const base = baseURL === undefined ? BASE_URL : baseURL
  let u = (base || '') + (url || '')
  const qs = buildQuery(params)
  if (qs) u += (u.indexOf('?') >= 0 ? '&' : '?') + qs
  return u
}

function headerValue(headers, name) {
  if (!headers) return undefined
  const lower = name.toLowerCase()
  for (const k of Object.keys(headers)) {
    if (k.toLowerCase() === lower) return headers[k]
  }
  return undefined
}

// FormData 的 Content-Type 必须由浏览器自动生成（含 boundary），不能手写
function prepareHeaders(config, body) {
  const headers = { ...(config.headers || {}) }
  const method = String(config.method || 'get').toUpperCase()
  const hasJSONBody = body !== undefined && method !== 'GET' && method !== 'HEAD'
  if (isFormData(body)) {
    for (const k of Object.keys(headers)) {
      if (k.toLowerCase() === 'content-type') delete headers[k]
    }
  } else if (hasJSONBody && headerValue(headers, 'Content-Type') === undefined) {
    headers['Content-Type'] = 'application/json'
  }
  return headers
}

async function readBody(res, responseType) {
  if (responseType === 'blob') return res.blob()
  if (res.status === 204) return null
  const text = await res.text()
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

// 核心请求：不含业务码判断、不弹提示（响应体以 Blob 返回时按 blob 处理）
async function coreRequest(config) {
  const method = String(config.method || 'get').toUpperCase()
  const body = method === 'GET' || method === 'HEAD' ? undefined : buildBody(config.data)
  const headers = prepareHeaders({ ...config, method }, body)

  const ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null
  const ms = config.timeout === undefined || config.timeout === null ? DEFAULT_TIMEOUT : config.timeout
  let timer = null
  if (ctrl && ms > 0) timer = setTimeout(() => ctrl.abort(), ms)

  try {
    const res = await fetch(resolveUrl(config.url, config.params, config.baseURL), {
      method,
      headers,
      body,
      signal: ctrl ? ctrl.signal : undefined,
      credentials: 'same-origin'
    })
    let data
    if (!res.ok && config.responseType === 'blob') {
      // 下载类接口失败时后端返回 JSON 错误体，转文本以便给出可读提示
      const text = await res.text()
      try {
        data = JSON.parse(text)
      } catch {
        data = text
      }
    } else {
      data = await readBody(res, config.responseType)
    }
    if (!res.ok) {
      throw new HttpError((data && data.msg) || 'HTTP ' + res.status, res.status, data, config)
    }
    return { data, status: res.status, headers: res.headers, config }
  } catch (e) {
    if (e instanceof HttpError) throw e
    if (e && e.name === 'AbortError') throw new HttpError('请求超时', 0, null, config)
    throw new HttpError((e && e.message) || '网络错误', 0, null, config)
  } finally {
    if (timer) clearTimeout(timer)
  }
}

// 上传进度只有 XMLHttpRequest 能拿到，有 onUploadProgress 时走它
function uploadRequest(config) {
  return new Promise((resolve, reject) => {
    const method = String(config.method || 'post').toUpperCase()
    const body = buildBody(config.data)
    const headers = prepareHeaders(config, body)
    const xhr = new XMLHttpRequest()
    xhr.open(method, resolveUrl(config.url, config.params, config.baseURL), true)
    for (const k of Object.keys(headers)) {
      if (headers[k] !== undefined && headers[k] !== null) xhr.setRequestHeader(k, headers[k])
    }
    const ms = config.timeout === undefined || config.timeout === null ? DEFAULT_TIMEOUT : config.timeout
    if (ms > 0) xhr.timeout = ms
    if (xhr.upload && typeof config.onUploadProgress === 'function') {
      xhr.upload.onprogress = (ev) => {
        config.onUploadProgress({
          loaded: ev.loaded,
          total: ev.total,
          progress: ev.lengthComputable && ev.total ? ev.loaded / ev.total : 0
        })
      }
    }
    xhr.onload = () => {
      let data = null
      try {
        data = xhr.responseText ? JSON.parse(xhr.responseText) : null
      } catch {
        data = xhr.responseText
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve({ data, status: xhr.status, config })
      } else {
        reject(new HttpError((data && data.msg) || 'HTTP ' + xhr.status, xhr.status, data, config))
      }
    }
    xhr.onerror = () => reject(new HttpError('网络错误', 0, null, config))
    xhr.ontimeout = () => reject(new HttpError('请求超时', 0, null, config))
    xhr.onabort = () => reject(new HttpError('请求已取消', 0, null, config))
    xhr.send(body === undefined ? null : body)
  })
}

// 裸客户端：不注入登录态、不弹提示、不做业务码判断（登录页 / 临时登录用，baseURL 默认空）
const rawClient = {
  request(config) {
    return coreRequest({ ...config, baseURL: config.baseURL === undefined ? '' : config.baseURL })
  },
  get(url, config = {}) {
    return coreRequest({ ...config, url, method: 'get', baseURL: config.baseURL === undefined ? '' : config.baseURL })
  },
  post(url, data, config = {}) {
    return coreRequest({ ...config, url, method: 'post', data, baseURL: config.baseURL === undefined ? '' : config.baseURL })
  }
}

// 多请求并行同时 401 时只跳转一次
let authRedirecting = false

async function request(config) {
  const cfg = { ...config }
  const auth = useAuthStore()
  // 调用方已显式指定 Authorization 时不覆盖（临时登录用它自己的 token）
  if (auth.token && headerValue(cfg.headers, 'Authorization') === undefined) {
    cfg.headers = { ...(cfg.headers || {}), Authorization: 'Bearer ' + auth.token }
  }

  let resp
  try {
    resp = cfg.onUploadProgress ? await uploadRequest(cfg) : await coreRequest(cfg)
  } catch (e) {
    if (cfg.silent) throw e
    if (e && e.response && e.response.status === 401) {
      if (!authRedirecting) {
        authRedirecting = true
        auth.logout()
        // 401 后跳安全入口（登录页），/login 已不可访问
        const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
        router.push(entrance ? '/' + entrance : '/').catch(() => {})
      }
    } else {
      ElMessage.error((e && e.response && e.response.data && e.response.data.msg) || (e && e.message) || '网络错误')
    }
    throw e
  }

  // 任一请求成功即说明会话恢复正常，允许后续再发生 401 时重新跳转
  authRedirecting = false
  if (cfg.responseType === 'blob') return resp

  const res = resp.data
  if (!res || res.code !== 0) {
    const msg = (res && res.msg) || '请求失败'
    if (!cfg.silent) ElMessage.error(msg)
    throw new HttpError(msg, undefined, res, cfg)
  }
  return res
}

request.get = (url, config = {}) => request({ ...config, url, method: 'get' })
request.post = (url, data, config = {}) => request({ ...config, url, method: 'post', data })
request.put = (url, data, config = {}) => request({ ...config, url, method: 'put', data })
request.patch = (url, data, config = {}) => request({ ...config, url, method: 'patch', data })
request.delete = (url, config = {}) => request({ ...config, url, method: 'delete' })

export { rawClient }
export default request
