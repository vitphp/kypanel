<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-header">
        <h1>{{ panelName }}</h1>
        <p>{{ panelSub }}</p>
      </div>

      <el-form ref="formRef" :model="form" :rules="rules" size="large" @keyup.enter="handleLogin">
        <el-form-item prop="username">
          <el-input v-model="form.username" placeholder="管理员账号" :prefix-icon="User" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input v-model="form.password" type="password" show-password placeholder="密码" :prefix-icon="Lock" />
        </el-form-item>

        <!-- 图形验证码：账号有失败记录或密码错误后显示 -->
        <el-form-item v-if="showCaptcha">
          <div class="captcha-row">
            <el-input v-model="form.captcha_code" placeholder="图形验证码" :prefix-icon="Picture" maxlength="4" />
            <img :src="captchaUrl" class="captcha-img" @click="refreshCaptcha" title="点击刷新" />
          </div>
        </el-form-item>

        <!-- 2FA 验证码：账号启用 2FA 后才显示（后端响应 need_totp 时触发） -->
        <el-form-item v-if="showTotp">
          <el-input v-model="form.otp_code" placeholder="6 位验证码" :prefix-icon="Key" maxlength="6" />
        </el-form-item>

        <el-form-item>
          <el-button type="primary" class="login-btn" :loading="loading" @click="handleLogin">
            登 录
          </el-button>
        </el-form-item>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock, Key, Picture } from '@element-plus/icons-vue'
import axios from 'axios'
import request from '../utils/request'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()

// 面板名称/副标题：后端通过 NoRoute 注入到 window.__PANEL_NAME__ / __PANEL_SUB__
const panelName = (window.__PANEL_NAME__ || '').trim() || '开猿运维'
const panelSub = (window.__PANEL_SUB__ || '').trim() || '开猿运维'

const formRef = ref()
const loading = ref(false)

// 是否显示图形验证码输入框（密码错误 1 次后置 true，由登录响应 need_captcha 触发）
const showCaptcha = ref(false)
// 是否显示 2FA 输入框（账号启用了 TOTP 时置 true，由登录响应 need_totp 触发）
const showTotp = ref(false)
// 验证码图片 URL（带 ?t= 时间戳避免缓存）
const captchaUrl = ref('')

// 安全入口值：登录/验证码接口路径内嵌该值（/api/<entrance>/auth/...），
// 攻击者不知道入口就无法调用接口。未启用入口时为空，走原始路径。
const entrance = (window.__SECURITY_ENTRANCE__ || '').trim()
const entrancePrefix = entrance ? '/' + entrance : ''

const form = reactive({
  username: '',
  password: '',
  captcha_code: '',
  otp_code: ''
})

const rules = {
  username: [{ required: true, message: '请输入账号', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
  // captcha_code / otp_code 动态字段在 handleLogin 里手动校验（el-form 对 v-if 字段支持不够好）
}

// 刷新图形验证码（密码错误后页面显示时调用；用户点击图片也调用）
async function refreshCaptcha() {
  const t = Date.now()
  // 接口路径内嵌安全入口；后端按 IP 维度绑定，无需传账号
  captchaUrl.value = `/api${entrancePrefix}/auth/captcha?t=${t}`
}

// 查询后端：当前 IP 是否需要验证码（不生成图片，只读状态）
async function checkCaptchaNeeded() {
  try {
    const resp = await axios.get(`/api${entrancePrefix}/auth/captcha-check`, { baseURL: '' })
    if (resp.data?.need_captcha) {
      showCaptcha.value = true
      // 立即拉取一张验证码图片
      await refreshCaptcha()
    } else {
      showCaptcha.value = false
    }
  } catch (e) {
    // 静默失败：保持当前状态
  }
}

// 登录（直接用 axios，避免被全局拦截器弹 toast，错误由本组件自行处理）
async function handleLogin() {
  await formRef.value.validate()
  // 手动校验动态字段（el-form 的 validate 对 v-if 隐藏字段有兼容性问题）
  if (showCaptcha.value && !form.captcha_code.trim()) {
    ElMessage.warning('请输入图形验证码')
    return
  }
  if (showTotp.value && !form.otp_code.trim()) {
    ElMessage.warning('请输入 2FA 验证码')
    return
  }
  loading.value = true
  try {
    const resp = await axios.post(`/api${entrancePrefix}/auth/login`, form, { baseURL: '' })
    const data = resp.data
    if (data.code === 0) {
      // 登录成功
      auth.setLogin(data.data.token, data.data.username)
      try {
        const p = await request.get('/auth/permissions')
        auth.setPermissions(p.data?.permissions ?? null)
      } catch (e) { auth.setPermissions(null) }
      ElMessage.success('登录成功')
      // 登录成功后统一进入首页（不再依赖 redirect 参数，避免入口值错乱时跳转异常）
      router.push('/')
    } else {
      // 登录失败：按需显示图形验证码 / 2FA 输入框
      if (data.need_captcha) {
        showCaptcha.value = true
        await refreshCaptcha()
      }
      if (data.need_totp) {
        showTotp.value = true
      }
      ElMessage.error(data.msg || '登录失败')
    }
  } catch (e) {
    // 网络错误等
    ElMessage.error(e.response?.data?.msg || e.message || '网络错误')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  // 进入登录页就查询：当前 IP 是否需要验证码（让 UI 与后端状态同步）
  checkCaptchaNeeded()
})
</script>

<style scoped>
.login-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f2d3d 0%, #2c3e50 100%);
}

.login-card {
  width: 400px;
  padding: 48px 40px;
  background: #fff;
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
}

.login-header {
  text-align: center;
  margin-bottom: 36px;
}

.login-header h1 {
  font-size: 28px;
  color: #1f2d3d;
  letter-spacing: 2px;
}

.login-header p {
  margin-top: 8px;
  color: #909399;
  font-size: 14px;
}

.captcha-row {
  display: flex;
  gap: 8px;
  width: 100%;
}

.captcha-row .el-input {
  flex: 1;
  min-width: 0;
}

.captcha-img {
  /* 固定宽高避免被 flex 拉伸；后端图片实际尺寸 150x50，scale=3 让字符清晰 */
  width: 150px;
  height: 50px;
  cursor: pointer;
  border-radius: 4px;
  border: 1px solid #dcdfe6;
  background: #fff;
  flex-shrink: 0;
  object-fit: contain;
}

.login-btn {
  width: 100%;
}
</style>