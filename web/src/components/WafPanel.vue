<template>
  <div class="waf-panel">
    <!-- 子 Tab -->
    <el-tabs v-model="subTab" class="waf-tabs">
      <!-- ============ 总览 ============ -->
      <el-tab-pane label="防护总览" name="overview">
        <el-row :gutter="12" class="ov-row">
          <el-col :xs="24" :sm="12" :md="6">
            <el-card shadow="never" class="ov-card">
              <div class="ov-num" :class="{ 'is-off': !overview.enabled }">{{ overview.enabled ? '防护中' : '已关闭' }}</div>
              <div class="ov-label">WAF 状态</div>
              <el-switch
                v-model="overview.enabled"
                :loading="saving"
                @change="saveSetting"
                style="margin-top: 8px"
              />
            </el-card>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-card shadow="never" class="ov-card">
              <div class="ov-num">{{ overview.today_block }}</div>
              <div class="ov-label">今日拦截</div>
              <div class="ov-sub">累计拦截 {{ overview.total_block }} 次</div>
            </el-card>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-card shadow="never" class="ov-card">
              <div class="ov-num">{{ overview.enabled_rule_count }}</div>
              <div class="ov-label">启用规则</div>
              <div class="ov-sub">共 {{ overview.rule_count }} 条 · 自定义 {{ overview.custom_rule_count }} 条</div>
            </el-card>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-card shadow="never" class="ov-card">
              <div class="ov-num">{{ overview.current_bans.length }}</div>
              <div class="ov-label">当前封禁 IP</div>
              <div class="ov-sub">
                <el-tag v-if="overview.cc_enabled" size="small" type="warning" style="margin-right: 4px">CC防护开</el-tag>
                <el-tag v-else size="small" type="info" style="margin-right: 4px">CC防护关</el-tag>
                IP黑名单 {{ overview.ip_black_count }} · 白名单 {{ overview.ip_white_count }}
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-card shadow="never" class="ov-settings">
          <div class="card-title">防护设置</div>
          <el-form label-width="130px" class="set-form">
            <el-form-item label="防护模式">
              <el-radio-group v-model="overview.mode" @change="saveSetting">
                <el-radio value="block">拦截模式（命中规则直接拒绝请求）</el-radio>
                <el-radio value="observe">观察模式（仅记录日志，不拦截）</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item label="Apache 支持">
              <el-switch v-model="overview.apache" @change="saveSetting" />
              <span class="form-tip">开启后同时为 Apache 生成 WAF 规则（RewriteRule）</span>
            </el-form-item>
            <el-form-item label="Web 服务器">
              <el-tag size="small" :type="overview.nginx_installed ? 'success' : 'info'">Nginx {{ overview.nginx_installed ? '已安装' : '未安装' }}</el-tag>
              <el-tag size="small" :type="overview.apache_installed ? 'success' : 'info'" style="margin-left: 6px">Apache {{ overview.apache_installed ? '已安装' : '未安装' }}</el-tag>
              <span class="form-tip">Nginx 规则自动注入到每个站点配置，修改后热重载生效</span>
            </el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" class="ov-settings">
          <div class="card-title">当前封禁 IP</div>
          <div v-if="overview.current_bans.length === 0" class="empty-tip">暂无封禁 IP</div>
          <div v-else class="ban-list">
            <el-tag
              v-for="(ip, i) in overview.current_bans"
              :key="i"
              type="danger"
              closable
              @close="quickUnban(ip)"
            >{{ ip }}</el-tag>
          </div>
        </el-card>
      </el-tab-pane>

      <!-- ============ 规则库 ============ -->
      <el-tab-pane label="攻击规则库" name="rules">
        <div class="toolbar">
          <div class="toolbar-title">
            <el-icon><View /></el-icon>
            <span>攻击规则库</span>
            <span class="tip">内置 10 大类攻击防护规则，可单独启停；自定义规则单独维护</span>
          </div>
          <div class="toolbar-actions">
            <el-button size="small" @click="resetRules">恢复内置规则</el-button>
            <el-button size="small" type="primary" :icon="Plus" @click="openCustomRuleDialog">新增自定义规则</el-button>
          </div>
        </div>
        <el-collapse v-model="openGroups" class="rule-collapse">
          <el-collapse-item v-for="g in ruleGroups" :key="g.category" :name="g.category">
            <template #title>
              <div class="group-title">
                <el-tag size="small" :type="groupTag(g.category)">{{ g.name }}</el-tag>
                <span class="group-count">{{ g.rules.length }} 条 · {{ g.enabled }} 条启用</span>
              </div>
            </template>
            <el-table :data="g.rules" size="small">
              <el-table-column prop="name" label="规则名称" min-width="180" />
              <el-table-column label="匹配位置" width="110">
                <template #default="{ row }">
                  {{ fieldLabel(row.match_field) }}
                </template>
              </el-table-column>
              <el-table-column label="正则表达式" min-width="300">
                <template #default="{ row }">
                  <el-tooltip placement="top" :content="row.pattern">
                    <code class="pattern">{{ row.pattern.length > 60 ? row.pattern.slice(0, 60) + '…' : row.pattern }}</code>
                  </el-tooltip>
                </template>
              </el-table-column>
              <el-table-column label="动作" width="110">
                <template #default="{ row }">
                  <el-tag size="small" :type="row.action === 'block' ? 'danger' : 'info'">{{ row.action === 'block' ? '拦截' : '仅记录' }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="启用" width="90">
                <template #default="{ row }">
                  <el-switch :model-value="row.enabled" @change="(v) => toggleRule(row, v)" />
                </template>
              </el-table-column>
              <el-table-column prop="remark" label="说明" min-width="150">
                <template #default="{ row }">
                  <span class="muted">{{ row.remark || '-' }}</span>
                </template>
              </el-table-column>
            </el-table>
          </el-collapse-item>
        </el-collapse>
      </el-tab-pane>

      <!-- ============ 自定义规则 ============ -->
      <el-tab-pane label="自定义规则" name="custom">
        <div class="toolbar">
          <div class="toolbar-title">
            <el-icon><EditPen /></el-icon>
            <span>自定义规则</span>
            <span class="tip">自定义正则规则，支持匹配 URI / UA / 请求头 / 参数</span>
          </div>
          <div class="toolbar-actions">
            <el-button type="primary" size="small" :icon="Plus" @click="openCustomRuleDialog">新增规则</el-button>
          </div>
        </div>
        <el-table :data="customRules" v-loading="loadingCustom" size="small">
          <el-table-column prop="name" label="规则名称" min-width="140" />
          <el-table-column label="匹配位置" width="110">
            <template #default="{ row }">
              {{ fieldLabel(row.match_field) }}
            </template>
          </el-table-column>
          <el-table-column label="正则表达式" min-width="280">
            <template #default="{ row }">
              <el-tooltip placement="top" :content="row.pattern">
                <code class="pattern">{{ row.pattern.length > 50 ? row.pattern.slice(0, 50) + '…' : row.pattern }}</code>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column label="动作" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="row.action === 'block' ? 'danger' : 'success'">{{ row.action === 'block' ? '拦截' : '放行' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="priority" label="优先级" width="90" />
          <el-table-column label="启用" width="90">
            <template #default="{ row }">
              <el-switch :model-value="row.enabled" @change="(v) => toggleRule(row, v)" />
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120" fixed="right">
            <template #default="{ row }">
              <el-button size="small" text type="primary" @click="editCustomRule(row)">编辑</el-button>
              <el-button size="small" text type="danger" @click="deleteRule(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- ============ 黑白名单 ============ -->
      <el-tab-pane label="黑白名单" name="list">
        <div class="toolbar">
          <div class="toolbar-title">
            <el-icon><Lock /></el-icon>
            <span>黑白名单</span>
            <span class="tip">支持 IP/网段、URL、User-Agent；临时封禁到期自动解封</span>
          </div>
          <div class="toolbar-actions">
            <el-radio-group v-model="listFilter" size="small">
              <el-radio-button value="all">全部</el-radio-button>
              <el-radio-button value="ip">IP</el-radio-button>
              <el-radio-button value="url">URL</el-radio-button>
              <el-radio-button value="ua">User-Agent</el-radio-button>
            </el-radio-group>
            <el-button type="primary" size="small" :icon="Plus" @click="openIpRuleDialog">添加</el-button>
          </div>
        </div>
        <el-table :data="filteredListRules" v-loading="loadingList" size="small">
          <el-table-column label="类型" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="listTypeTag(row.type)">{{ listTypeLabel(row.type) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="内容" min-width="240">
            <template #default="{ row }">
              <el-tooltip v-if="row.content.split('\n').length > 1" placement="top" :content="row.content.split('\n').join('，')">
                <span class="mono">
                  <el-tag v-for="(c, i) in row.content.split('\n').slice(0, 3)" :key="i" size="small" class="content-tag">{{ c }}</el-tag>
                  <span class="more">+{{ row.content.split('\n').length - 3 }}</span>
                </span>
              </el-tooltip>
              <span v-else class="mono">{{ row.content }}</span>
            </template>
          </el-table-column>
          <el-table-column label="策略" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="row.action === 'block' ? 'danger' : 'success'">{{ row.action === 'block' ? '封禁' : '放行' }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="到期时间" width="160">
            <template #default="{ row }">
              <span v-if="row.expire_at">{{ formatTime(row.expire_at) }}</span>
              <span v-else class="muted">永久</span>
            </template>
          </el-table-column>
          <el-table-column prop="remark" label="备注" min-width="130">
            <template #default="{ row }">
              <span class="muted">{{ row.remark || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="90" fixed="right">
            <template #default="{ row }">
              <el-button size="small" text type="danger" @click="deleteIpRule(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- ============ CC 防护 ============ -->
      <el-tab-pane label="CC防护" name="cc">
        <el-card shadow="never" class="cc-card">
          <div class="card-title">
            CC 防护（流量过滤）
            <el-switch v-model="ccForm.enabled" style="margin-left: 12px" />
          </div>
          <el-form label-width="160px" class="set-form" style="max-width: 620px">
            <el-form-item label="单 IP 频率限制">
              <el-input-number v-model="ccForm.max_requests" :min="10" :max="10000" :step="10" style="width: 150px" />
              <span class="form-tip" style="margin-left: 8px">次 / {{ ccForm.window_sec }} 秒</span>
            </el-form-item>
            <el-form-item label="统计窗口">
              <el-input-number v-model="ccForm.window_sec" :min="1" :max="3600" :step="5" style="width: 150px" />
              <span class="form-tip" style="margin-left: 8px">秒</span>
            </el-form-item>
            <el-form-item label="超限自动封禁时长">
              <el-input-number v-model="ccForm.block_sec" :min="60" :max="86400" :step="300" style="width: 150px" />
              <span class="form-tip" style="margin-left: 8px">秒（默认 3600 = 1 小时）</span>
            </el-form-item>
            <el-form-item label="单 IP 最大并发连接">
              <el-input-number v-model="ccForm.max_conn_per_ip" :min="0" :max="1000" :step="10" style="width: 150px" />
              <span class="form-tip" style="margin-left: 8px">0 = 不限制</span>
            </el-form-item>
            <el-form-item label="统计静态资源">
              <el-switch v-model="ccForm.include_static" />
              <span class="form-tip" style="margin-left: 8px">开启后图片/JS/CSS 请求也计入频率</span>
            </el-form-item>
            <el-form-item label="CC 白名单 IP">
              <el-input v-model="ccForm.whitelist" type="textarea" :rows="3" placeholder="每行一个 IP，白名单不受频率限制" style="width: 320px" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="savingCc" @click="saveCc">保存 CC 配置</el-button>
              <span class="form-tip" style="margin-left: 12px">保存后自动应用到 Nginx/Apache 并热重载</span>
            </el-form-item>
          </el-form>
        </el-card>
      </el-tab-pane>

      <!-- ============ 攻击日志 ============ -->
      <el-tab-pane label="攻击日志" name="logs">
        <div class="toolbar">
          <div class="toolbar-title">
            <el-icon><Tickets /></el-icon>
            <span>攻击日志</span>
            <span class="tip">共 {{ logTotal }} 条拦截/记录</span>
          </div>
          <div class="toolbar-actions">
            <el-input v-model="logKeyword" placeholder="搜索 IP / URL / 规则 / 站点" size="small" style="width: 240px" clearable @keyup.enter="loadLogs" />
            <el-button size="small" :icon="Search" @click="loadLogs">搜索</el-button>
            <el-button size="small" type="danger" @click="clearLogs">清空日志</el-button>
          </div>
        </div>
        <el-table :data="logs" v-loading="loadingLogs" size="small">
          <el-table-column label="时间" width="170">
            <template #default="{ row }">
              <span class="mono">{{ formatTime(row.time) }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="ip" label="IP" width="130">
            <template #default="{ row }">
              <span class="mono">{{ row.ip }}</span>
              <div v-if="row.region" class="muted region">{{ row.region }}</div>
            </template>
          </el-table-column>
          <el-table-column prop="site" label="站点" min-width="130">
            <template #default="{ row }">
              <span class="muted">{{ row.site || '-' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="攻击类型" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="categoryTag(row.category)">{{ row.category_cn }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="rule_name" label="命中规则" min-width="150" />
          <el-table-column prop="method" label="方法" width="70" />
          <el-table-column label="URL" min-width="220">
            <template #default="{ row }">
              <el-tooltip placement="top" :content="row.uri">
                <span class="mono uri">{{ row.uri.length > 60 ? row.uri.slice(0, 60) + '…' : row.uri }}</span>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column label="动作" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.action === 'block' ? 'danger' : 'info'">{{ row.action === 'block' ? '拦截' : '记录' }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination
            layout="total, prev, pager, next"
            :total="logTotal"
            :page-size="logPageSize"
            :current-page="logPage"
            @current-change="(p) => { logPage = p; loadLogs() }"
          />
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 自定义规则对话框 -->
    <el-dialog v-model="customDialog.visible" :title="customDialog.editing ? '编辑自定义规则' : '新增自定义规则'" width="560" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="规则名称">
          <el-input v-model="customForm.name" placeholder="如：拦截恶意路径" />
        </el-form-item>
        <el-form-item label="匹配位置">
          <el-select v-model="customForm.match_field" style="width: 100%">
            <el-option value="uri" label="URI（请求路径+参数）" />
            <el-option value="args" label="请求参数" />
            <el-option value="ua" label="User-Agent" />
            <el-option value="referer" label="Referer" />
            <el-option value="header" label="请求头" />
          </el-select>
        </el-form-item>
        <el-form-item label="正则表达式">
          <el-input v-model="customForm.pattern" type="textarea" :rows="3" placeholder="如：(?i)(admin\.php|\.env)" />
        </el-form-item>
        <el-form-item label="动作">
          <el-radio-group v-model="customForm.action">
            <el-radio value="block">拦截</el-radio>
            <el-radio value="log">仅记录</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="customForm.priority" :min="0" :max="999" />
          <span class="form-tip" style="margin-left: 8px">数字越大越先匹配</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="customForm.remark" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="customDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="customDialog.submitting" @click="saveCustomRule">确定</el-button>
      </template>
    </el-dialog>

    <!-- 黑白名单对话框 -->
    <el-dialog v-model="ipRuleDialog.visible" title="添加黑白名单" width="520" :close-on-click-modal="false">
      <el-form label-width="90px">
        <el-form-item label="类型">
          <el-radio-group v-model="ipRuleForm.type">
            <el-radio-button value="ip">IP</el-radio-button>
            <el-radio-button value="url">URL</el-radio-button>
            <el-radio-button value="ua">User-Agent</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="内容">
          <el-input
            v-model="ipRuleForm.content"
            type="textarea"
            :rows="5"
            :placeholder="ipRuleForm.type === 'ip'
              ? '每行一个 IP 或网段\n1.2.3.4\n1.2.3.0/24'
              : ipRuleForm.type === 'url'
                ? '每行一个 URL 路径\n/admin\n/login.php'
                : '每行一个 UA 关键字\npython-requests\nsqlmap'"
          />
        </el-form-item>
        <el-form-item label="策略">
          <el-radio-group v-model="ipRuleForm.action">
            <el-radio value="block">封禁（黑名单）</el-radio>
            <el-radio value="allow">放行（白名单）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="临时封禁">
          <el-radio-group v-model="ipRuleForm.expireType">
            <el-radio value="forever">永久</el-radio>
            <el-radio value="temp">临时</el-radio>
          </el-radio-group>
          <el-input-number
            v-if="ipRuleForm.expireType === 'temp'"
            v-model="ipRuleForm.expireHours"
            :min="1"
            :max="720"
            style="margin-left: 12px"
          />
          <span v-if="ipRuleForm.expireType === 'temp'" class="form-tip" style="margin-left: 6px">小时</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="ipRuleForm.remark" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ipRuleDialog.visible = false">取消</el-button>
        <el-button type="primary" :loading="ipRuleDialog.submitting" @click="addIpRule">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onActivated } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search, Lock, View, EditPen, Tickets } from '@element-plus/icons-vue'
import request from '../utils/request'

const subTab = ref('overview')
const saving = ref(false)

// ---------- 总览 ----------
const overview = reactive({
  enabled: false,
  mode: 'block',
  apache: false,
  nginx_installed: false,
  apache_installed: false,
  rule_count: 0,
  enabled_rule_count: 0,
  block_rule_count: 0,
  custom_rule_count: 0,
  ip_black_count: 0,
  ip_white_count: 0,
  today_block: 0,
  today_log: 0,
  total_block: 0,
  current_bans: [],
  cc_enabled: false,
})

async function loadOverview() {
  const res = await request.get('/waf/overview')
  Object.assign(overview, res.data)
}

async function saveSetting() {
  saving.value = true
  try {
    await request.post('/waf/setting', {
      enabled: overview.enabled,
      mode: overview.mode,
      apache: overview.apache,
    })
    ElMessage.success('已保存并重新加载 WAF 配置')
  } catch (e) {
    loadOverview()
  } finally {
    saving.value = false
  }
}

async function quickUnban(ip) {
  await ElMessageBox.confirm(`确认解封 IP ${ip}？`, '提示', { type: 'warning' })
  const res = await request.get('/waf/iprules')
  const rule = (res.data.rules || []).find(
    (r) => r.type === 'ip' && r.action === 'block' && r.content.split('\n').includes(ip)
  )
  if (rule) {
    await request.post('/waf/iprule/delete', { id: rule.id })
  }
  ElMessage.success('已解封')
  loadOverview()
}

// ---------- 规则库 ----------
const openGroups = ref([])
const allRules = ref([])

const categoryNames = {
  sql_inject: 'SQL注入',
  xss: 'XSS跨站',
  cmd_inject: '命令注入',
  path_traversal: '路径穿越',
  file_include: '文件包含',
  code_exec: '代码执行',
  scanner: '恶意扫描器',
  sensitive_file: '敏感文件',
  webshell: 'WebShell',
  bad_ua: '恶意UA',
  custom: '自定义规则',
}

const ruleGroups = computed(() => {
  const map = {}
  for (const r of allRules.value) {
    if (!map[r.category]) {
      map[r.category] = { category: r.category, name: categoryNames[r.category] || r.category, rules: [], enabled: 0 }
    }
    map[r.category].rules.push(r)
    if (r.enabled) map[r.category].enabled++
  }
  return Object.values(map)
})

function groupTag(c) {
  const map = {
    sql_inject: 'danger',
    xss: 'warning',
    cmd_inject: 'danger',
    path_traversal: 'warning',
    file_include: 'warning',
    code_exec: 'danger',
    scanner: 'info',
    sensitive_file: 'info',
    webshell: 'danger',
    bad_ua: 'info',
    custom: 'primary',
  }
  return map[c] || 'primary'
}

function fieldLabel(f) {
  return { uri: 'URI', args: '参数', ua: 'User-Agent', referer: 'Referer', header: '请求头', body: '请求体' }[f] || f || 'URI'
}

async function loadRules() {
  const res = await request.get('/waf/rules')
  allRules.value = res.data.rules || []
  customRules.value = allRules.value.filter((r) => !r.builtin)
}

async function toggleRule(row, val) {
  try {
    await request.post('/waf/rule/update', { id: row.id, update: { enabled: val } })
    row.enabled = val
    ElMessage.success(val ? '规则已启用' : '规则已停用')
  } catch (e) {
    loadRules()
  }
}

async function resetRules() {
  await ElMessageBox.confirm('确认恢复内置规则？所有内置规则的启停状态将重置为默认。', '提示', { type: 'warning' })
  await request.post('/waf/rules/reset')
  ElMessage.success('已恢复内置规则')
  loadRules()
  loadOverview()
}

// ---------- 自定义规则 ----------
const loadingCustom = ref(false)
const customRules = ref([])
const customDialog = reactive({ visible: false, submitting: false, editing: false, id: 0 })
const customForm = reactive({ name: '', match_field: 'uri', pattern: '', action: 'block', priority: 0, remark: '' })

function openCustomRuleDialog() {
  Object.assign(customForm, { name: '', match_field: 'uri', pattern: '', action: 'block', priority: 0, remark: '' })
  customDialog.editing = false
  customDialog.id = 0
  customDialog.visible = true
}

function editCustomRule(row) {
  Object.assign(customForm, {
    name: row.name,
    match_field: row.match_field,
    pattern: row.pattern,
    action: row.action,
    priority: row.priority,
    remark: row.remark,
  })
  customDialog.editing = true
  customDialog.id = row.id
  customDialog.visible = true
}

async function saveCustomRule() {
  if (!customForm.pattern.trim()) {
    ElMessage.warning('请输入正则表达式')
    return
  }
  customDialog.submitting = true
  try {
    if (customDialog.editing) {
      await request.post('/waf/rule/update', { id: customDialog.id, update: { pattern: customForm.pattern, action: customForm.action } })
      ElMessage.success('规则已更新')
    } else {
      await request.post('/waf/rule/add', { ...customForm })
      ElMessage.success('规则已添加')
    }
    customDialog.visible = false
    loadRules()
  } catch (e) {
    // 已由拦截器提示
  } finally {
    customDialog.submitting = false
  }
}

async function deleteRule(row) {
  await ElMessageBox.confirm(`确认删除规则「${row.name}」？`, '提示', { type: 'warning' })
  await request.post('/waf/rule/delete', { id: row.id })
  ElMessage.success('已删除')
  loadRules()
  loadOverview()
}

// ---------- 黑白名单 ----------
const loadingList = ref(false)
const listRules = ref([])
const listFilter = ref('all')
const ipRuleDialog = reactive({ visible: false, submitting: false })
const ipRuleForm = reactive({ type: 'ip', content: '', action: 'block', expireType: 'forever', expireHours: 1, remark: '' })

const filteredListRules = computed(() => {
  if (listFilter.value === 'all') return listRules.value
  return listRules.value.filter((r) => r.type === listFilter.value)
})

function listTypeLabel(t) {
  return { ip: 'IP', url: 'URL', ua: 'User-Agent' }[t] || t
}
function listTypeTag(t) {
  return { ip: 'primary', url: 'success', ua: 'warning' }[t] || 'info'
}

async function loadIpRules() {
  loadingList.value = true
  try {
    const res = await request.get('/waf/iprules')
    listRules.value = res.data.rules || []
  } finally {
    loadingList.value = false
  }
}

function openIpRuleDialog() {
  Object.assign(ipRuleForm, { type: 'ip', content: '', action: 'block', expireType: 'forever', expireHours: 1, remark: '' })
  ipRuleDialog.visible = true
}

async function addIpRule() {
  if (!ipRuleForm.content.trim()) {
    ElMessage.warning('请输入内容')
    return
  }
  ipRuleDialog.submitting = true
  try {
    await request.post('/waf/iprule/add', {
      type: ipRuleForm.type,
      action: ipRuleForm.action,
      content: ipRuleForm.content,
      expire_seconds: ipRuleForm.expireType === 'temp' ? ipRuleForm.expireHours * 3600 : 0,
      remark: ipRuleForm.remark,
    })
    ElMessage.success('已添加')
    ipRuleDialog.visible = false
    loadIpRules()
    loadOverview()
  } catch (e) {
    // 已由拦截器提示
  } finally {
    ipRuleDialog.submitting = false
  }
}

async function deleteIpRule(row) {
  await ElMessageBox.confirm('确认删除该条规则？', '提示', { type: 'warning' })
  await request.post('/waf/iprule/delete', { id: row.id })
  ElMessage.success('已删除')
  loadIpRules()
  loadOverview()
}

// ---------- CC 防护 ----------
const ccForm = reactive({ enabled: false, max_requests: 100, window_sec: 10, block_sec: 3600, max_conn_per_ip: 50, include_static: false, whitelist: '' })
const savingCc = ref(false)

async function loadCc() {
  const res = await request.get('/waf/cc')
  Object.assign(ccForm, res.data)
}

async function saveCc() {
  savingCc.value = true
  try {
    await request.post('/waf/cc/save', { ...ccForm })
    ElMessage.success('CC 配置已保存并应用')
    loadOverview()
  } catch (e) {
    // 已由拦截器提示
  } finally {
    savingCc.value = false
  }
}

// ---------- 攻击日志 ----------
const loadingLogs = ref(false)
const logs = ref([])
const logTotal = ref(0)
const logPage = ref(1)
const logPageSize = ref(20)
const logKeyword = ref('')

function categoryTag(c) {
  const map = {
    sql_inject: 'danger',
    xss: 'warning',
    cmd_inject: 'danger',
    path_traversal: 'warning',
    file_include: 'warning',
    code_exec: 'danger',
    scanner: 'info',
    sensitive_file: 'info',
    webshell: 'danger',
    bad_ua: 'info',
    cc_attack: 'danger',
    custom: 'primary',
  }
  return map[c] || 'primary'
}

async function loadLogs() {
  loadingLogs.value = true
  try {
    const res = await request.get('/waf/logs', {
      params: { page: logPage.value, page_size: logPageSize.value, keyword: logKeyword.value },
    })
    logs.value = res.data.logs || []
    logTotal.value = res.data.total || 0
  } finally {
    loadingLogs.value = false
  }
}

async function clearLogs() {
  await ElMessageBox.confirm('确认清空全部攻击日志？', '提示', { type: 'warning' })
  await request.post('/waf/logs/clear')
  ElMessage.success('已清空')
  loadLogs()
  loadOverview()
}

function formatTime(t) {
  if (!t) return '-'
  const d = new Date(t)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

onMounted(() => {
  loadOverview()
  loadRules()
  loadIpRules()
  loadCc()
  loadLogs()
})

onActivated(() => {
  loadOverview()
})
</script>

<style scoped>
.waf-tabs {
  background: #fff;
  border-radius: 6px;
  padding: 4px 16px 16px;
  box-shadow: 0 1px 3px rgba(0, 21, 41, 0.08);
}
.ov-row {
  margin-bottom: 12px;
}
.ov-card {
  margin-bottom: 12px;
  text-align: center;
}
.ov-num {
  font-size: 26px;
  font-weight: 600;
  color: #303133;
  line-height: 1.2;
}
.ov-num.is-off {
  color: #909399;
}
.ov-label {
  color: #909399;
  font-size: 13px;
  margin-top: 4px;
}
.ov-sub {
  color: #c0c4cc;
  font-size: 12px;
  margin-top: 4px;
}
.ov-settings {
  margin-bottom: 12px;
}
.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 14px;
  display: flex;
  align-items: center;
}
.set-form {
  max-width: 760px;
}
.form-tip {
  color: #909399;
  font-size: 12px;
  margin-left: 6px;
}
.ban-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.empty-tip {
  color: #c0c4cc;
  font-size: 13px;
  padding: 8px 0;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 12px 0;
}
.toolbar-title {
  display: flex;
  align-items: center;
  gap: 6px;
  color: #303133;
  font-size: 14px;
  font-weight: 500;
}
.tip {
  color: #909399;
  font-size: 12px;
  font-weight: 400;
}
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.group-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 500;
  color: #303133;
}
.group-count {
  color: #909399;
  font-size: 12px;
  font-weight: 400;
}
.pattern {
  font-family: Consolas, Menlo, monospace;
  font-size: 12px;
  color: #606266;
  background: #f5f7fa;
  padding: 2px 6px;
  border-radius: 4px;
  word-break: break-all;
}
.mono {
  font-family: Consolas, Menlo, monospace;
  font-size: 12.5px;
}
.muted {
  color: #909399;
  font-size: 13px;
}
.more {
  color: #909399;
  font-size: 12px;
  margin-left: 4px;
}
.content-tag {
  margin-right: 4px;
}
.cc-card {
  margin-bottom: 12px;
}
.region {
  font-size: 12px;
  line-height: 1.3;
}
.uri {
  font-size: 12px;
  color: #606266;
}
.pager {
  display: flex;
  justify-content: flex-end;
  padding: 12px 0;
}
</style>
