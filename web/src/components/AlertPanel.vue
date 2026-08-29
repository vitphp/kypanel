<template>
  <div class="alert-panel">
    <!-- 总开关 -->
    <el-card shadow="never" class="alert-card">
      <div class="alert-head">
        <div class="alert-head-left">
          <span class="alert-title">告警通知</span>
          <el-tag v-if="cfg.enabled" type="success" size="small">已开启</el-tag>
          <el-tag v-else type="info" size="small">已关闭</el-tag>
        </div>
        <el-switch v-model="cfg.enabled" @change="save" />
      </div>
      <p class="alert-desc">当 CPU / 内存 / 磁盘 / 负载持续超过阈值时，通过下方渠道推送告警。</p>
    </el-card>

    <!-- 阈值规则 -->
    <el-card shadow="never" class="alert-card">
      <template #header><span class="card-title">告警阈值</span></template>
      <el-form label-width="120px" label-position="left">
        <el-form-item v-for="r in ruleItems" :key="r.key" :label="r.label">
          <div class="rule-row">
            <el-switch v-model="cfg.rules[r.key].enabled" @change="save" />
            <el-input-number
              v-model="cfg.rules[r.key].threshold"
              :min="r.min" :max="r.max" :step="r.step"
              size="small" controls-position="right" @change="save"
            />
            <span class="rule-unit">{{ r.unit }}</span>
            <span class="rule-dur">持续</span>
            <el-input-number
              v-model="cfg.rules[r.key].duration"
              :min="10" :max="3600" :step="10"
              size="small" controls-position="right" @change="save"
            />
            <span class="rule-unit">秒</span>
          </div>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 通知渠道 -->
    <el-card shadow="never" class="alert-card">
      <template #header>
        <div class="card-head-row">
          <span class="card-title">通知渠道</span>
          <el-button type="primary" size="small" @click="addChannel">添加渠道</el-button>
        </div>
      </template>

      <el-empty v-if="!cfg.channels.length" description="尚未配置通知渠道" :image-size="60" />

      <div v-for="(ch, idx) in cfg.channels" :key="idx" class="channel-item">
        <div class="channel-row">
          <el-select v-model="ch.type" size="small" style="width: 130px" @change="save">
            <el-option label="Webhook" value="webhook" />
            <el-option label="钉钉机器人" value="dingtalk" />
            <el-option label="企业微信" value="wecom" />
            <el-option label="邮件 SMTP" value="smtp" />
          </el-select>
          <el-input v-model="ch.name" size="small" placeholder="渠道名称（可选）" style="width: 140px" @change="save" />
          <el-switch v-model="ch.enabled" @change="save" />
          <el-button size="small" @click="testChannel(ch)" :loading="testingIdx === idx">测试</el-button>
          <el-button size="small" type="danger" @click="removeChannel(idx)">删除</el-button>
        </div>
        <div class="channel-row" style="margin-top: 6px">
          <template v-if="ch.type === 'smtp'">
            <el-input v-model="ch.url" size="small" placeholder='{"host":"smtp.qq.com","port":465,"user":"x@qq.com","pass":"授权码","to":"a@x.com"}' style="flex: 1" @change="save" />
          </template>
          <template v-else>
            <el-input v-model="ch.url" size="small" placeholder="Webhook URL（钉钉/企微机器人的 webhook 地址）" style="flex: 1" @change="save" />
            <el-input v-if="ch.type === 'dingtalk'" v-model="ch.secret" size="small" placeholder="加签密钥（可选）" style="width: 180px" @change="save" />
          </template>
        </div>
      </div>
    </el-card>

    <!-- 告警记录 -->
    <el-card shadow="never" class="alert-card">
      <template #header>
        <div class="card-head-row">
          <span class="card-title">告警记录</span>
          <el-button type="danger" size="small" :disabled="!logs.length" @click="clearLogs">清空</el-button>
        </div>
      </template>
      <el-table :data="logs" size="small" max-height="320">
        <el-table-column prop="created_at" label="时间" width="160">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column prop="message" label="内容" show-overflow-tooltip />
        <el-table-column prop="level" label="级别" width="90">
          <template #default="{ row }">
            <el-tag :type="row.level === 'critical' ? 'danger' : 'warning'" size="small">{{ row.level === 'critical' ? '严重' : '警告' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="已推送" width="80">
          <template #default="{ row }">
            <el-tag :type="row.notified ? 'success' : 'info'" size="small">{{ row.notified ? '是' : '否' }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import request from '../utils/request'
import { fmtTimeISO } from '../utils/format'
import { ElMessage, ElMessageBox } from 'element-plus'

const cfg = reactive({
  enabled: false,
  rules: {
    cpu: { enabled: true, threshold: 90, duration: 60 },
    mem: { enabled: true, threshold: 90, duration: 60 },
    disk: { enabled: true, threshold: 90, duration: 60 },
    load: { enabled: true, threshold: 8, duration: 60 },
  },
  channels: [],
})

const ruleItems = [
  { key: 'cpu', label: 'CPU 使用率', min: 1, max: 100, step: 1, unit: '%' },
  { key: 'mem', label: '内存使用率', min: 1, max: 100, step: 1, unit: '%' },
  { key: 'disk', label: '磁盘使用率', min: 1, max: 100, step: 1, unit: '%' },
  { key: 'load', label: '系统负载(1min)', min: 0.1, max: 100, step: 0.1, unit: '' },
]

const logs = ref([])
const testingIdx = ref(-1)

async function load() {
  const res = await request.get('/alert/settings')
  if (res.data && res.data.config) {
    Object.assign(cfg, res.data.config)
  }
  logs.value = res.data.logs || []
}

async function save() {
  try {
    await request.post('/alert/settings', JSON.parse(JSON.stringify(cfg)))
  } catch (e) { /* interceptor */ }
}

function addChannel() {
  cfg.channels.push({ type: 'webhook', name: '', enabled: true, url: '', secret: '' })
}

function removeChannel(idx) {
  cfg.channels.splice(idx, 1)
  save()
}

async function testChannel(ch) {
  testingIdx.value = cfg.channels.indexOf(ch)
  try {
    await request.post('/alert/test-channel', ch)
    ElMessage.success('测试消息已发送')
  } catch (e) {
    ElMessage.error('发送失败，请检查渠道配置')
  } finally {
    testingIdx.value = -1
  }
}

async function clearLogs() {
  await ElMessageBox.confirm('确定清空所有告警记录吗？', '提示', { type: 'warning' })
  await request.post('/alert/clear-logs')
  logs.value = []
  ElMessage.success('已清空')
}

function fmtTime(t) {
  return fmtTimeISO(t)
}

onMounted(load)
</script>

<style scoped>
.alert-panel { display: flex; flex-direction: column; gap: 14px; }
.alert-card { border-radius: 8px; }
.alert-head { display: flex; align-items: center; justify-content: space-between; }
.alert-head-left { display: flex; align-items: center; gap: 10px; }
.alert-title { font-size: 15px; font-weight: 600; }
.alert-desc { margin: 8px 0 0; color: #909399; font-size: 13px; }
.card-title { font-weight: 600; }
.card-head-row { display: flex; align-items: center; justify-content: space-between; }
.rule-row { display: flex; align-items: center; gap: 10px; }
.rule-unit { color: #909399; font-size: 13px; }
.rule-dur { margin-left: 16px; color: #606266; font-size: 13px; }
.channel-item { padding: 10px 0; border-bottom: 1px solid #f0f0f0; }
.channel-item:last-child { border-bottom: none; }
.channel-row { display: flex; align-items: center; gap: 8px; }
</style>
