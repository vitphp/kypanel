<template>
  <div class="settings-page">
    <el-tabs v-model="activeTab" class="settings-tabs">
      <!-- 面板账号 -->
      <el-tab-pane label="面板账号" name="account">
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="setting-card">
              <template #header>
                <div class="card-head">
                  <span><el-icon><User /></el-icon>账号与登录</span>
                </div>
              </template>
              <ul class="setting-list">
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">管理员账号</div>
                    <div class="setting-desc">登录面板的用户名</div>
                  </div>
                  <el-button link type="primary" @click="usernameDialog.show = true">修改</el-button>
                </li>
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">登录密码</div>
                    <div class="setting-desc">修改后所有会话将立即失效</div>
                  </div>
                  <el-button link type="primary" @click="pwdDialog.show = true">修改</el-button>
                </li>
              </ul>
            </el-card>
          </el-col>

          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="setting-card">
              <template #header>
                <div class="card-head">
                  <span><el-icon><Connection /></el-icon>面板网络</span>
                </div>
              </template>
              <ul class="setting-list">
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">面板名称</div>
                    <div class="setting-desc">自定义品牌名；登录页、顶栏、浏览器标签均显示</div>
                  </div>
                  <span class="setting-value">{{ info.panel_name || '开猿运维' }}</span>
                  <el-button link type="primary" @click="openPanelNameDialog">修改</el-button>
                </li>
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">监听端口</div>
                    <div class="setting-desc">HTTP/HTTPS 端口，修改后需重启</div>
                  </div>
                  <span class="setting-value">{{ info.port || '-' }}</span>
                  <el-button link type="primary" @click="openPortDialog">修改</el-button>
                </li>
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">绑定域名</div>
                    <div class="setting-desc">绑定后仅可通过域名访问</div>
                  </div>
                  <span class="setting-value">{{ info.domain || '未绑定' }}</span>
                  <el-button link type="primary" @click="domainDialog.show = true">修改</el-button>
                </li>
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">安全入口</div>
                    <div class="setting-desc">登录页 URL 前缀；1-10 位字母数字（仅字母和数字），留空则关闭入口</div>
                  </div>
                  <span class="setting-value">{{ info.security_entrance || '未启用' }}</span>
                  <el-button link type="primary" @click="openEntranceDialog">修改</el-button>
                </li>
              </ul>
            </el-card>
          </el-col>

          </el-row>
      </el-tab-pane>

      <!-- 安全 -->
      <el-tab-pane label="安全" name="security">
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="setting-card">
              <template #header>
                <div class="card-head">
                  <span><el-icon><Shield /></el-icon>登录安全</span>
                </div>
              </template>
              <ul class="setting-list">
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">双因素认证（2FA）</div>
                    <div class="setting-desc">TOTP 动态验证码，提升登录安全</div>
                  </div>
                  <el-tag :type="totpEnabled ? 'success' : 'info'" size="small" style="margin-right: 8px">{{ totpEnabled ? '已启用' : '未启用' }}</el-tag>
                  <el-button v-if="!totpEnabled" link type="primary" @click="beginEnableTotp">启用</el-button>
                  <el-button v-else link type="danger" @click="disableTotpVisible = true">关闭</el-button>
                </li>
                <li class="setting-item">
                  <div class="setting-info">
                    <div class="setting-title">登录 IP 白名单</div>
                    <div class="setting-desc">每行一个精确 IP，留空不限制（127.0.0.1 始终放行）</div>
                  </div>
                  <el-button link type="primary" @click="allowlistDialog.show = true">修改</el-button>
                </li>
              </ul>
            </el-card>
          </el-col>

          <el-col :xs="24" :md="12">
            <el-card shadow="never" class="setting-card">
              <template #header>
                <div class="card-head">
                  <span><el-icon><UserFilled /></el-icon>在线会话</span>
                  <el-button link type="danger" size="small" :disabled="!sessions.length" @click="kickAll">踢下线全部</el-button>
                </div>
              </template>
              <div style="padding: 4px 0 8px; color: #94a3b8; font-size: 13px">
                当前在线 {{ sessions.length }} 个会话（已下线/被踢的会话会自动从列表移除）
              </div>
              <Skeleton v-if="sessionsLoading" type="table" :rows="4" :columns="[{width:'130px'},{width:'150px'},{width:'80px'},{width:'70px'},{flex:1},{width:'90px'}]" />
              <el-table v-else-if="sessions.length" :data="sessions" size="small" max-height="280" style="width: 100%">
                <el-table-column prop="ip" label="IP" min-width="130" />
                <el-table-column label="归属地" min-width="150">
                  <template #default="{ row }">
                    <span v-if="row.region" style="font-size: 12px; color: #64748b">
                      <template v-if="row.region.country && row.region.country !== '0' && row.region.country !== '中国'">{{ row.region.country }} </template>
                      <template v-if="row.region.province && row.region.province !== '0'">{{ row.region.province }} </template>
                      <template v-if="row.region.city && row.region.city !== '0' && row.region.city !== row.region.province">{{ row.region.city }} </template>
                      <template v-if="row.region.isp && row.region.isp !== '0'">· {{ row.region.isp }}</template>
                    </span>
                    <span v-else style="color: #c0c4cc; font-size: 12px">-</span>
                  </template>
                </el-table-column>
                <el-table-column prop="username" label="账号" min-width="80" />
                <el-table-column label="状态" min-width="70">
                  <template #default="{ row }">
                    <el-tag type="success" size="small">在线</el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="最后活跃" min-width="160">
                  <template #default="{ row }">{{ fmtTime(row.last_seen) }}</template>
                </el-table-column>
                <el-table-column label="操作" min-width="90">
                  <template #default="{ row }">
                    <el-button v-if="row.active && !row.is_current" link type="danger" size="small" @click="kickOne(row)">踢下线</el-button>
                    <span v-else-if="row.is_current" style="color: #67c23a; font-size: 12px; font-weight: 500">当前登录</span>
                  </template>
                </el-table-column>
              </el-table>
              <div v-else style="text-align: center; color: #c0c4cc; padding: 32px 0; font-size: 13px">暂无会话</div>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      <!-- 令牌中心（API 令牌 + AI 助手 MCP 合并） -->
      <el-tab-pane label="令牌中心" name="token">
        <!-- 顶部：两个不同的请求地址（API + MCP） -->
        <el-card shadow="never" class="setting-card" style="margin-bottom: 16px">
          <template #header>
            <div class="card-head">
              <span><el-icon><Position /></el-icon>请求地址</span>
            </div>
          </template>
          <el-row :gutter="16">
            <el-col :xs="24" :md="12">
              <div class="endpoint-label">API 接口（外部脚本）</div>
              <el-input :model-value="apiUrl" readonly class="endpoint-input">
                <template #append><el-button @click="copyText(apiUrl)">复制</el-button></template>
              </el-input>
            </el-col>
            <el-col :xs="24" :md="12">
              <div class="endpoint-label">MCP 服务（AI 工具）</div>
              <el-input :model-value="mcpUrl" readonly class="endpoint-input">
                <template #append><el-button @click="copyText(mcpUrl)">复制</el-button></template>
              </el-input>
            </el-col>
          </el-row>
        </el-card>

        <!-- 令牌列表 -->
        <el-card shadow="never" class="setting-card" style="margin-bottom: 16px">
          <template #header>
            <div class="card-head">
              <span><el-icon><Key /></el-icon>令牌列表</span>
              <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;">
                <el-radio-group v-model="tokenFilter" size="small">
                  <el-radio-button value="">全部</el-radio-button>
                  <el-radio-button value="api">API</el-radio-button>
                  <el-radio-button value="mcp">MCP</el-radio-button>
                </el-radio-group>
                <el-button size="small" type="primary" @click="openCreateToken">+ 创建令牌</el-button>
              </div>
            </div>
          </template>
          <el-alert type="info" :closable="false" style="margin-bottom: 12px"
            title="创建令牌控制面板的外部访问权限。可选三种类型：API（外部脚本调用）/ MCP（AI 工具连接）/ 全部（两种都能用）。每个令牌可独立设置权限范围、IP 白名单与过期时间。所有令牌为 36 位字母数字组合，仅创建时返回一次明文。" />
          <Skeleton v-if="tokensLoading" type="table" :rows="5" :columns="[{flex:1},{width:'100px'},{flex:2},{width:'140px'},{width:'140px'},{width:'140px'},{width:'70px'}]" />
          <el-table v-else :data="filteredTokens" size="small" max-height="380">
            <el-table-column prop="name" label="名称" min-width="120" />
            <el-table-column label="类型" width="100">
              <template #default="{ row }">
                <el-tag :type="tokenTagType(row.type)" size="small">{{ tokenTagLabel(row.type) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="权限范围" min-width="150">
              <template #default="{ row }">
                <template v-if="!row.scopes || !row.scopes.length">
                  <el-tag size="small" type="danger">全部权限</el-tag>
                </template>
                <template v-else>
                  <el-tag v-for="s in row.scopes" :key="s" size="small" style="margin-right:4px">{{ scopeLabel(s) }}</el-tag>
                </template>
              </template>
            </el-table-column>
            <el-table-column label="IP 白名单" min-width="160">
              <template #default="{ row }">
                <span v-if="!row.allow_ips.length" class="muted">不限制</span>
                <span v-else class="ip-list">{{ row.allow_ips.join('、') }}</span>
              </template>
            </el-table-column>
            <el-table-column label="过期" width="140">
              <template #default="{ row }">
                <span v-if="!row.expire_at" class="muted">永不过期</span>
                <span v-else>{{ fmtTime(row.expire_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="最近使用" width="140">
              <template #default="{ row }"><span class="muted">{{ fmtTime(row.last_used_at) }}</span></template>
            </el-table-column>
            <el-table-column label="创建时间" width="140">
              <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="70" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" size="small" @click="deleteToken(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>

        <!-- AI 工具配置示例（折叠面板） -->
        <el-card shadow="never" class="setting-card">
          <template #header>
            <div class="card-head">
              <span><el-icon><ChatDotRound /></el-icon>AI 工具配置示例</span>
            </div>
          </template>
          <el-alert type="info" :closable="false" style="margin-bottom: 16px"
            title="通过 MCP 协议，Claude Code / Codex / Cursor 等 AI 工具可直接连接面板，调用面板能力完成服务器管理、网站部署、故障排查等运维操作。所有 AI 操作均记录在「日志 → API 日志 / MCP 日志」里。"
            description="请先在上方创建一个 MCP 或「全部」类型的令牌，然后填入下方示例。" />
          <el-collapse>
            <el-collapse-item title="Claude Code">
              <p class="mcp-p">命令行方式：</p>
              <pre class="mcp-pre">claude mcp add --transport http kypanel {{ mcpUrl }} --header "Authorization: Bearer &lt;你的 MCP 令牌&gt;"</pre>
              <p class="mcp-p">或写入项目根目录 .mcp.json：</p>
              <pre class="mcp-pre">{{ claudeJson }}</pre>
            </el-collapse-item>
            <el-collapse-item title="Cursor">
              <p class="mcp-p">Settings → MCP → Add server，选择 HTTP 方式：</p>
              <pre class="mcp-pre">URL:     {{ mcpUrl }}
Headers: Authorization: Bearer &lt;你的 MCP 令牌&gt;</pre>
            </el-collapse-item>
            <el-collapse-item title="Codex (OpenAI)">
              <p class="mcp-p">写入 ~/.codex/config.toml：</p>
              <pre class="mcp-pre">{{ codexToml }}</pre>
            </el-collapse-item>
          </el-collapse>
        </el-card>
      </el-tab-pane>

      <!-- 临时访问 -->
      <el-tab-pane label="临时访问" name="temp">
        <el-card shadow="never" class="setting-card">
          <template #header>
            <div class="card-head">
              <span><el-icon><Timer /></el-icon>临时登录链接</span>
              <el-button size="small" type="primary" @click="openCreateTemp">+ 创建临时链接</el-button>
            </div>
          </template>
          <el-alert type="info" :closable="false" style="margin-bottom: 12px"
            title="创建临时登录链接，访客打开后免密码直接进入面板后台。可设置有效期（到期自动失效并踢下线）。在有效期内链接可被多次使用，使用情况（IP + 归属地）会记录在「记录」弹窗里。"
            description="注意：临时链接等同于超管权限，请谨慎分享。" />
          <Skeleton v-if="tempLoading" type="table" :rows="4" :columns="[{flex:1},{width:'170px'},{width:'80px'},{flex:2},{width:'80px'},{width:'180px'}]" />
          <el-table v-else :data="tempList" size="small">
            <el-table-column prop="name" label="名称" min-width="120">
              <template #default="{ row }">
                <span v-if="row.name">{{ row.name }}</span>
                <span v-else class="muted">（未命名）</span>
              </template>
            </el-table-column>
            <el-table-column label="有效期" width="170">
              <template #default="{ row }">
                <span v-if="!row.expire_at" class="muted">永不过期</span>
                <span v-else :class="isExpired(row) ? 'used-up' : ''">{{ fmtTime(row.expire_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="使用" width="80" align="center">
              <template #default="{ row }">
                <el-tag v-if="row.used_count > 0" type="warning" size="small">已使用</el-tag>
                <el-tag v-else type="info" size="small">未使用</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="最后使用 IP / 归属地" min-width="180">
              <template #default="{ row }">
                <span v-if="row.last_ip">{{ row.last_ip }}</span>
                <span v-else class="muted">-</span>
                <div v-if="row.last_region" class="muted" style="font-size: 11px">{{ row.last_region }}</div>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="80" align="center">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">{{ row.status === 1 ? '启用' : '禁用' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="220" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="copyTempLink(row)">复制链接</el-button>
                <el-button link type="success" size="small" @click="openTempLogs(row)">记录</el-button>
                <el-button link size="small" :type="row.status === 1 ? 'warning' : 'success'" @click="toggleTemp(row)">{{ row.status === 1 ? '禁用' : '启用' }}</el-button>
                <el-button link type="danger" size="small" @click="deleteTemp(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="!tempList.length && !tempLoading" style="text-align: center; color: #c0c4cc; padding: 32px 0; font-size: 13px">
            暂无临时链接，点右上角「+ 创建临时链接」开始
          </div>
        </el-card>

      </el-tab-pane>

      <!-- 高级 -->
      <el-tab-pane label="高级" name="advanced">
        <el-card shadow="never" class="setting-card">
          <template #header>
            <div class="card-head">
              <span><el-icon><MagicStick /></el-icon>SSL 证书</span>
            </div>
          </template>
          <ul class="setting-list">
            <li class="setting-item">
              <div class="setting-info">
                <div class="setting-title">LiteSSL EAB 凭据</div>
                <div class="setting-desc">ACME DNS-01 申请通配符证书所需的 EAB 凭据</div>
              </div>
              <span class="setting-value">{{ litesslInfo.eab_kid ? litesslInfo.eab_kid.slice(0, 8) + '...' : '未配置' }}</span>
              <el-button link type="primary" @click="openLiteSSL">配置</el-button>
            </li>
          </ul>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- ===== 通用弹窗（账号、密码、端口、域名、MySQL、白名单、LiteSSL） ===== -->
    <el-dialog v-model="usernameDialog.show" title="修改管理员账号" width="420px">
      <el-form label-width="80px">
        <el-form-item label="新用户名" required>
          <el-input v-model="usernameDialog.username" placeholder="请输入新用户名" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="usernameDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="usernameDialog.loading" @click="changeUsername">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="pwdDialog.show" title="修改登录密码" width="420px">
      <el-form label-width="80px">
        <el-form-item label="原密码" required><el-input v-model="pwdDialog.old" type="password" show-password /></el-form-item>
        <el-form-item label="新密码" required><el-input v-model="pwdDialog.neww" type="password" show-password placeholder="至少 6 位" /></el-form-item>
        <el-form-item label="重复密码" required><el-input v-model="pwdDialog.confirm" type="password" show-password /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="pwdDialog.loading" @click="changePwd">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="panelNameDialog.show" title="自定义面板名称" width="480px" destroy-on-close>
      <el-form label-width="80px">
        <el-form-item label="面板名称" required>
          <el-input v-model="panelNameDialog.name" placeholder="如 开猿运维" maxlength="32" />
        </el-form-item>
        <el-form-item label="副标题">
          <el-input v-model="panelNameDialog.sub" placeholder="如 开猿运维" maxlength="32" />
        </el-form-item>
        <el-form-item><span class="muted">修改后登录页/顶栏/浏览器标签立即生效。</span></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="panelNameDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="panelNameDialog.loading" @click="savePanelName">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="portDialog.show" title="修改面板端口" width="420px" destroy-on-close>
      <el-form label-width="80px">
        <el-form-item label="端口号" required>
          <el-input-number v-model="portDialog.port" :min="1" :max="65535" controls-position="right" style="width: 100%" />
        </el-form-item>
        <el-form-item><span class="muted">修改后会自动重启面板服务并跳转到新端口</span></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="portDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="portDialog.loading" @click="savePort">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="domainDialog.show" title="面板绑定域名" width="520px" destroy-on-close>
      <el-form label-width="80px">
        <el-form-item label="域名">
          <el-input v-model="domainDialog.domain" placeholder="如 panel.example.com，留空取消绑定" />
        </el-form-item>
        <el-form-item><span class="muted">绑定后仅可通过域名访问，IP+端口将被拒绝</span></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="domainDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="domainDialog.loading" @click="saveDomain">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="entranceDialog.show" title="面板安全入口" width="520px" destroy-on-close>
      <el-form label-width="80px">
        <el-form-item label="安全入口">
          <div class="entrance-row">
            <el-input v-model="entranceDialog.entrance" placeholder="1-10 位字母数字（留空关闭）" maxlength="10" style="flex: 1" />
            <el-button :icon="Refresh" @click="generateEntrance" title="生成新的 6 位随机码" />
          </div>
        </el-form-item>
        <el-form-item><span class="muted">修改后必须用新入口 <code>{{ '/<entrance>/' }}</code> 才能访问登录页。仅接受 1-10 位字母和数字（不分大小写，留空则关闭入口），也可点击刷新按钮生成 6 位随机码。</span></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="entranceDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="entranceDialog.loading" @click="saveEntrance">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="allowlistDialog.show" title="登录 IP 白名单" width="520px">
      <el-form label-width="80px">
        <el-form-item label="IP 列表">
          <el-input v-model="allowlistDialog.text" type="textarea" :rows="6"
            placeholder="每行一个精确 IP（不支持通配符/CIDR），留空不限制" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="allowlistDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="allowlistDialog.loading" @click="saveAllowlist">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="litesslDialog.show" title="LiteSSL EAB 凭据" width="520px">
      <el-form label-width="120px">
        <el-form-item label="EAB Kid"><el-input v-model="litesslDialog.eab_kid" /></el-form-item>
        <el-form-item label="EAB HMAC"><el-input v-model="litesslDialog.eab_hmac" type="password" show-password /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="litesslDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="litesslDialog.loading" @click="saveLiteSSL">保存</el-button>
      </template>
    </el-dialog>

    <!-- 2FA -->
    <el-dialog v-model="totpDialog.show" title="启用双因素认证" width="460px">
      <p class="muted" style="margin: 0 0 12px">
        在 Google Authenticator 等 TOTP 应用中输入密钥（或选择「手动输入密钥」），然后输入应用显示的 6 位验证码。
      </p>
      <el-form label-width="80px">
        <el-form-item label="密钥">
          <el-input :model-value="totpDialog.secret" readonly>
            <template #append><el-button @click="copyText(totpDialog.secret)">复制</el-button></template>
          </el-input>
        </el-form-item>
        <el-form-item label="验证码" required>
          <el-input v-model="totpDialog.code" placeholder="6 位动态验证码" maxlength="6" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="totpDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="totpDialog.loading" @click="confirmEnableTotp">确认</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="disableTotpVisible" title="关闭双因素认证" width="400px">
      <p class="muted" style="margin: 0 0 12px">请输入当前 6 位验证码以确认关闭。</p>
      <el-input v-model="disableTotpCode" placeholder="6 位验证码" maxlength="6" />
      <template #footer>
        <el-button @click="disableTotpVisible = false">取消</el-button>
        <el-button type="danger" :loading="disablingTotp" @click="confirmDisableTotp">确认关闭</el-button>
      </template>
    </el-dialog>

    <!-- 令牌：创建 -->
    <el-dialog v-model="tokenDialog.show" :title="'创建' + tokenTagLabel(tokenDialog.type) + '令牌'" width="560px">
      <el-form label-width="100px">
        <el-form-item label="名称" required>
          <el-input v-model="tokenDialog.name" placeholder="如「CI 脚本」「Cursor」" />
        </el-form-item>
        <el-form-item label="类型" required>
          <el-radio-group v-model="tokenDialog.type">
            <el-radio-button value="api">API（外部脚本）</el-radio-button>
            <el-radio-button value="mcp">MCP（AI 工具）</el-radio-button>
            <el-radio-button value="all">全部（通用）</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="权限范围">
          <el-select v-model="tokenDialog.scopes" multiple collapse-tags clearable style="width: 100%"
            placeholder="留空 = 全部权限，可多选限制到指定模块">
            <el-option v-for="s in scopeOptions" :key="s.key" :label="s.label" :value="s.key" />
          </el-select>
          <div class="muted" style="line-height:1.6;margin-top:4px">
            留空 = 全部权限（谨慎）。勾选后该令牌只能操作所选模块，例如只给脚本
            <code>网站 + 文件 + 计划任务</code>，不给数据库/防火墙等高危模块，降低令牌泄露风险。
          </div>
        </el-form-item>
        <el-form-item label="IP 白名单">
          <el-input v-model="tokenDialog.allowIPs" :rows="5" type="textarea"
            placeholder="每行一个精确 IP（不支持通配符/CIDR），留空不限制" />
        </el-form-item>
        <el-form-item label="过期">
          <el-input-number v-model="tokenDialog.expireDays" :min="0" :max="3650" controls-position="right" style="width: 140px" />
          <span class="muted" style="margin-left: 8px">天（0 = 永不过期）</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tokenDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="tokenDialog.loading" @click="submitCreateToken">生成</el-button>
      </template>
    </el-dialog>

    <!-- API 令牌：明文展示（仅一次） -->
    <el-dialog v-model="plainDialog.show" :title="plainDialog.title" width="560px">
      <el-alert type="warning" :closable="false" style="margin-bottom: 12px"
        title="请立即复制并妥善保存，此令牌仅显示一次，关闭后无法再次查看完整内容。" />
      <el-input v-model="plainDialog.token" type="textarea" :rows="3" readonly />
      <template #footer>
        <el-button @click="plainDialog.show = false">关闭</el-button>
        <el-button type="primary" @click="copyText(plainDialog.token)">复制令牌</el-button>
      </template>
    </el-dialog>

    <!-- 临时访问：创建 -->
    <el-dialog v-model="tempDialog.show" title="创建临时登录链接" width="520px">
      <el-form label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="tempDialog.name" placeholder="如「给同事临时看」（可选）" />
        </el-form-item>
        <el-form-item label="有效期">
          <el-select v-model="tempDialog.expireSecs" style="width: 200px">
            <el-option label="永不过期" :value="0" />
            <el-option label="30 分钟" :value="1800" />
            <el-option label="1 小时" :value="3600" />
            <el-option label="6 小时" :value="21600" />
            <el-option label="1 天" :value="86400" />
            <el-option label="7 天" :value="604800" />
            <el-option label="30 天" :value="2592000" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <span class="muted">到期后访客会被自动踢下线，需重新获取链接。链接在有效期内可被多次使用。</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tempDialog.show = false">取消</el-button>
        <el-button type="primary" :loading="tempDialog.loading" @click="submitCreateTemp">生成</el-button>
      </template>
    </el-dialog>

    <!-- 临时访问：链接展示（仅一次） -->
    <el-dialog v-model="tempLinkDialog.show" title="临时链接已生成" width="600px">
      <el-alert type="warning" :closable="false" style="margin-bottom: 12px"
        title="请立即复制链接并妥善保存，完整链接仅显示这一次。" />
      <el-input v-model="tempLinkDialog.link" type="textarea" :rows="3" readonly />
      <template #footer>
        <el-button @click="tempLinkDialog.show = false">关闭</el-button>
        <el-button type="primary" @click="copyText(tempLinkDialog.link)">复制链接</el-button>
      </template>
    </el-dialog>

    <!-- 临时访问：使用记录（登录日志 + 操作日志） -->
    <el-dialog v-model="tempLogsDialog.show" :title="(tempLogsDialog.row?.name || '未命名') + ' - 使用记录'" width="900px" top="5vh">
      <el-tabs v-model="tempLogsDialog.tab" @tab-change="loadTempLogsDetail">
        <!-- 登录日志：使用 TempAccessUseLog -->
        <el-tab-pane label="登录日志" name="use">
          <Skeleton v-if="tempLogsDialog.useLoading" type="table" :rows="4" :columns="[{width:'170px'},{width:'140px'},{flex:2},{flex:1}]" />
          <el-table v-else :data="tempLogsDialog.useLogs" size="small" max-height="420" empty-text="暂无登录记录">
            <el-table-column label="时间" width="170">
              <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="ip" label="IP" width="140" />
            <el-table-column label="归属地">
              <template #default="{ row }">
                <span v-if="row.region">{{ row.region }}</span>
                <span v-else class="muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="user_agent" label="User-Agent" show-overflow-tooltip min-width="200" />
          </el-table>
        </el-tab-pane>

        <!-- 操作日志：使用 OperationLog (Source=temp) -->
        <el-tab-pane label="操作日志" name="op">
          <Skeleton v-if="tempLogsDialog.opLoading" type="table" :rows="5" :columns="[{width:'170px'},{width:'130px'},{flex:1},{width:'140px'},{width:'90px'}]" />
          <el-table v-else :data="tempLogsDialog.opLogs" size="small" max-height="420" empty-text="暂无操作记录">
            <el-table-column label="时间" width="170">
              <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column label="模块" width="130">
              <template #default="{ row }">
                <el-tag size="small" type="success">{{ moduleName(row.action.replace(/^temp\./, '').split('.')[0]) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="detail" label="操作" min-width="220" show-overflow-tooltip />
            <el-table-column prop="ip" label="IP" width="140" />
            <el-table-column label="结果" width="90" align="center">
              <template #default="{ row }">
                <el-tag :type="row.status === 'success' ? 'success' : 'danger'" size="small">
                  {{ row.status === 'success' ? '成功' : '失败' }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import request from '../utils/request'
import { fmtTimeISO } from '../utils/format'
import { usePanelStore } from '../stores/panel'

// 设置页 Tab 持久化（localStorage）：下次进入设置页停留在上次选中的 Tab
const SETTINGS_TAB_KEY = 'lp_settings_active_tab'
function readSettingsTab() {
  try {
    const v = localStorage.getItem(SETTINGS_TAB_KEY)
    return v || 'account'
  } catch (e) { return 'account' }
}
const activeTab = ref(readSettingsTab())

// ====== 账号、密码、端口、域名 ======
// 注意：info.port 初始为 null（不是 9999），避免页面刷新时先显示占位默认值再变成后端真实值。
// 后端返回前显示 '-'（用 || '-'），返回后变成真实端口。
const info = ref({ port: null, domain: '', username: '' })
async function loadInfo() {
  const res = await request.get('/settings/info')
  info.value = { ...info.value, ...res.data }
  // 同步面板名称到 store（顶栏实时刷新用）
  usePanelStore().setInfo(res.data)
}

const usernameDialog = ref({ show: false, username: '', loading: false })
async function changeUsername() {
  if (!usernameDialog.value.username.trim()) return ElMessage.warning('请填写新用户名')
  usernameDialog.value.loading = true
  try {
    await request.post('/settings/username', { username: usernameDialog.value.username.trim() })
    ElMessage.success('账号已修改，需重新登录')
    usernameDialog.value.show = false
    usernameDialog.value.username = ''
    loadInfo()
  } finally { usernameDialog.value.loading = false }
}

const pwdDialog = ref({ show: false, old: '', neww: '', confirm: '', loading: false })
async function changePwd() {
  if (!pwdDialog.value.old) return ElMessage.warning('请填写原密码')
  if (pwdDialog.value.neww.length < 6) return ElMessage.warning('新密码至少 6 位')
  if (pwdDialog.value.neww !== pwdDialog.value.confirm) return ElMessage.warning('两次密码不一致')
  pwdDialog.value.loading = true
  try {
    await request.post('/auth/change-password', {
      old_password: pwdDialog.value.old, new_password: pwdDialog.value.neww
    })
    ElMessage.success('密码已修改')
    pwdDialog.value.show = false
    pwdDialog.value = { show: false, old: '', neww: '', confirm: '', loading: false }
  } finally { pwdDialog.value.loading = false }
}

// 端口 dialog 初始 port 留 null（不预设 9999），避免点击「修改」时 dialog 内瞬间闪现默认值
// 端口 dialog 初始 port 留 null（不预设 9999），避免点击「修改」时 dialog 内瞬间闪现默认值
const portDialog = ref({ show: false, port: null, loading: false })

// 面板名称 dialog：保存到后端后，下次访问登录页 / 顶栏 / 浏览器标签都会用新名字
const panelNameDialog = ref({ show: false, name: '', sub: '', loading: false })
function openPanelNameDialog() {
  panelNameDialog.value.name = info.value.panel_name || '开猿运维'
  panelNameDialog.value.sub = info.value.panel_sub || '开猿运维'
  panelNameDialog.value.show = true
}
async function savePanelName() {
  panelNameDialog.value.loading = true
  try {
    await request.post('/settings/panel-name', {
      name: panelNameDialog.value.name.trim(),
      sub: panelNameDialog.value.sub.trim()
    })
    ElMessage.success('面板名称已保存，下次访问登录页生效')
    panelNameDialog.value.show = false
    // 同时刷新 store，让顶栏立即显示新名称（无需刷新页面）
    await loadInfo()
  } finally { panelNameDialog.value.loading = false }
}

// 打开端口修改弹窗：先把当前端口同步到 dialog，避免显示陈旧的默认值
function openPortDialog() {
  portDialog.value.port = info.value.port || 9999
  portDialog.value.show = true
}
async function savePort() {
  portDialog.value.loading = true
  try {
    const res = await request.post('/settings/port', { port: portDialog.value.port })
    // 端口变更后，service 会自动放行新端口并异步重启 panel（800ms 后）
    const newPort = portDialog.value.port
    portDialog.value.show = false
    ElMessageBox.alert(
      `端口已保存为 ${newPort}，防火墙已自动放行，新端口已生效。\n\n页面将自动跳转到新端口 ${newPort}。`,
      '端口修改成功',
      { confirmButtonText: '立即跳转', type: 'success' }
    ).then(() => {
      // 拼新地址：用 location 同样的协议 + 主机，端口换成 newPort
      window.location.href = `${location.protocol}//${location.hostname}:${newPort}${location.pathname}${location.search}${location.hash}`
    }).catch(() => {})
    loadInfo()
  } finally { portDialog.value.loading = false }
}

const domainDialog = ref({ show: false, domain: '', loading: false })
async function saveDomain() {
  domainDialog.value.loading = true
  try {
    await request.post('/settings/domain', { domain: domainDialog.value.domain.trim() })
    ElMessage.success('域名绑定已保存')
    domainDialog.value.show = false
    loadInfo()
  } finally { domainDialog.value.loading = false }
}

const entranceDialog = ref({ show: false, entrance: '', loading: false })
// 打开安全入口弹窗：同步当前入口值
function openEntranceDialog() {
  entranceDialog.value.entrance = info.value.security_entrance || ''
  entranceDialog.value.show = true
}
// 点击刷新按钮：调后端生成 6 位随机入口（仅生成，不保存）
async function generateEntrance() {
  try {
    const res = await request.get('/settings/security-entrance/generate')
    entranceDialog.value.entrance = res.data?.entrance || ''
  } catch (e) {
    // 错误已由全局拦截器提示
  }
}
// 保存安全入口：写入 config.json + 更新后端内存 currentEntrance，立即生效。
// 设置页停留原位即可（列表值 loadInfo 更新），不跳转、不刷新，跟改端口/域名一样。
// 新入口对「下次退出后重新访问登录页」生效（登录页加载时拿到最新入口值）。
async function saveEntrance() {
  entranceDialog.value.loading = true
  try {
    const newVal = entranceDialog.value.entrance.trim()
    await request.post('/settings/security-entrance', { entrance: newVal })
    ElMessage.success(newVal ? `安全入口已更新为 ${newVal}` : '安全入口已关闭')
    entranceDialog.value.show = false
    await loadInfo()
  } finally { entranceDialog.value.loading = false }
}

// ====== 安全：白名单、会话、2FA ======
const allowlistText = ref('')
const allowlistDialog = ref({ show: false, text: '', loading: false })
async function loadSecurity() {
  const res = await request.get('/settings/login-allowlist')
  const list = res.data || []
  allowlistText.value = list.join('\n')
  allowlistDialog.value.text = allowlistText.value
  loadSessions()
  loadTotpStatus()
}
async function saveAllowlist() {
  const list = allowlistDialog.value.text.split('\n').map(s => s.trim()).filter(Boolean)
  allowlistDialog.value.loading = true
  try {
    await request.post('/settings/login-allowlist', { list })
    ElMessage.success('白名单已保存')
    allowlistDialog.value.show = false
    allowlistText.value = allowlistDialog.value.text
  } finally { allowlistDialog.value.loading = false }
}

const sessions = ref([])
const sessionsLoading = ref(true)
async function loadSessions() {
  try {
    const res = await request.get('/settings/sessions')
    sessions.value = res.data || []
  } finally {
    sessionsLoading.value = false
  }
}
async function kickOne(row) {
  try { await ElMessageBox.confirm(`确定将 ${row.ip}（${row.username}）踢下线吗？`, '踢下线', { type: 'warning' }) }
  catch { return }
  await request.post('/settings/session/kick', { session_id: row.id })
  ElMessage.success('已踢下线')
  loadSessions()
}
async function kickAll() {
  try { await ElMessageBox.confirm('确定踢下线所有会话吗？', '踢下线全部', { type: 'warning' }) } catch { return }
  const adminId = sessions.value[0]?.admin_id
  if (!adminId) return
  await request.post('/settings/session/kick-all', { admin_id: adminId })
  ElMessage.success('已踢下线全部')
  loadSessions()
}

const totpEnabled = ref(false)
const totpDialog = ref({ show: false, secret: '', code: '', loading: false })
const disableTotpVisible = ref(false)
const disableTotpCode = ref('')
const disablingTotp = ref(false)
async function loadTotpStatus() {
  const res = await request.get('/auth/totp/status')
  totpEnabled.value = !!(res.data && res.data.enabled)
}
async function beginEnableTotp() {
  const res = await request.post('/auth/totp/enable-begin')
  totpDialog.value = { show: true, secret: res.data?.secret || '', code: '', loading: false }
}
async function confirmEnableTotp() {
  if (!totpDialog.value.code || totpDialog.value.code.length !== 6) return ElMessage.warning('请输入 6 位验证码')
  totpDialog.value.loading = true
  try {
    await request.post('/auth/totp/enable-confirm', { secret: totpDialog.value.secret, code: totpDialog.value.code })
    ElMessage.success('双因素认证已启用')
    totpDialog.value.show = false
    loadTotpStatus()
  } finally { totpDialog.value.loading = false }
}
async function confirmDisableTotp() {
  if (!disableTotpCode.value || disableTotpCode.value.length !== 6) return ElMessage.warning('请输入 6 位验证码')
  disablingTotp.value = true
  try {
    await request.post('/auth/totp/disable', { code: disableTotpCode.value })
    ElMessage.success('已关闭')
    disableTotpVisible.value = false
    disableTotpCode.value = ''
    loadTotpStatus()
  } finally { disablingTotp.value = false }
}

// ====== 令牌中心（API + MCP 合并） ======
const tokens = ref([])
const tokensLoading = ref(true)
const tokenFilter = ref('')
// 过滤：api 包含 api/all；mcp 包含 mcp/all；全部=所有
const filteredTokens = computed(() => {
  if (tokenFilter.value === 'api') return tokens.value.filter(t => t.type === 'api' || t.type === 'all')
  if (tokenFilter.value === 'mcp') return tokens.value.filter(t => t.type === 'mcp' || t.type === 'all')
  return tokens.value
})
// 令牌类型 tag 颜色与展示
function tokenTagType(t) {
  return ({ api: 'primary', mcp: 'success', all: 'warning' })[t] || 'info'
}
function tokenTagLabel(t) {
  return ({ api: 'API', mcp: 'MCP', all: '全部' })[t] || t
}

async function loadTokens() {
  tokensLoading.value = true
  try {
    const res = await request.get('/api-tokens')
    tokens.value = res.data || []
  } finally { tokensLoading.value = false }
}

// 令牌权限范围选项（与后端 service.ApiTokenScopeModules 保持一致）
const scopeOptions = [
  { key: 'site', label: '网站' }, { key: 'database', label: '数据库' }, { key: 'backup', label: '备份' },
  { key: 'ftp', label: 'FTP' }, { key: 'file', label: '文件' }, { key: 'container', label: '容器' },
  { key: 'appstore', label: '应用商店' }, { key: 'cron', label: '计划任务' }, { key: 'log', label: '日志' },
  { key: 'monitor', label: '监控' }, { key: 'process', label: '进程' }, { key: 'firewall', label: '防火墙' },
  { key: 'mcp', label: 'MCP' }, { key: 'settings', label: '设置' }
]
function scopeLabel(key) {
  return (scopeOptions.find(o => o.key === key) || {}).label || key
}

const tokenDialog = ref({ show: false, type: 'api', name: '', allowIPs: '', expireDays: 0, scopes: [], loading: false })
function openCreateToken() {
  tokenDialog.value = { show: true, type: 'api', name: '', allowIPs: '', expireDays: 0, scopes: [], loading: false }
}
async function submitCreateToken() {
  if (!tokenDialog.value.name.trim()) return ElMessage.warning('请输入名称')
  tokenDialog.value.loading = true
  try {
    const res = await request.post('/api-tokens', {
      name: tokenDialog.value.name.trim(),
      type: tokenDialog.value.type,
      allow_ips: tokenDialog.value.allowIPs,
      expire_days: tokenDialog.value.expireDays,
      scopes: tokenDialog.value.scopes.join(',')
    })
    tokenDialog.value.show = false
    plainDialog.value = { show: true, title: tokenTagLabel(tokenDialog.value.type) + ' 访问令牌', token: res.data.token }
    loadTokens()
  } finally { tokenDialog.value.loading = false }
}

async function deleteToken(row) {
  try { await ElMessageBox.confirm(`确定删除令牌「${row.name}」吗？使用该令牌的脚本/AI 工具将立即断开。`, '删除令牌', { type: 'warning' }) } catch { return }
  await request.post(`/api-tokens/${row.id}/delete`)
  ElMessage.success('已删除')
  loadTokens()
}

const plainDialog = ref({ show: false, title: '', token: '' })

// ====== MCP 服务地址 ======
const mcpUrl = computed(() => {
  const loc = window.location
  return `${loc.protocol}//${loc.host}/api/mcp`
})
// API 请求地址（示例：https://host/api/system/info），具体接口路径以路由为准
const apiUrl = computed(() => {
  const loc = window.location
  return `${loc.protocol}//${loc.host}/api`
})
const claudeJson = computed(() => `{
  "mcpServers": {
    "kypanel": {
      "type": "http",
      "url": "${mcpUrl.value}",
      "headers": { "Authorization": "Bearer <你的 MCP 令牌>" }
    }
  }
}`)
const codexToml = computed(() => `[mcp_servers.kypanel]
url = "${mcpUrl.value}"
headers = { Authorization = "Bearer <你的 MCP 令牌>" }`)

// ====== LiteSSL ======
const litesslInfo = ref({ eab_kid: '', eab_hmac: '' })
const litesslDialog = ref({ show: false, eab_kid: '', eab_hmac: '', loading: false })
async function loadLiteSSL() {
  const res = await request.get('/settings/litessl')
  litesslInfo.value = res.data || { eab_kid: '', eab_hmac: '' }
}
async function openLiteSSL() {
  litesslDialog.value = { show: true, eab_kid: litesslInfo.value.eab_kid || '', eab_hmac: litesslInfo.value.eab_hmac || '', loading: false }
}
async function saveLiteSSL() {
  litesslDialog.value.loading = true
  try {
    await request.post('/settings/litessl', { eab_kid: litesslDialog.value.eab_kid, eab_hmac: litesslDialog.value.eab_hmac })
    ElMessage.success('已保存')
    litesslDialog.value.show = false
    loadLiteSSL()
  } finally { litesslDialog.value.loading = false }
}

// ====== 临时访问 ======
const tempList = ref([])
const tempLoading = ref(true)

const tempDialog = ref({ show: false, name: '', expireSecs: 1800, loading: false })
const tempLinkDialog = ref({ show: false, link: '' })
// 使用记录弹窗（登录日志 + 操作日志）
const tempLogsDialog = ref({
  show: false, tab: 'use', row: null,
  useLogs: [], useLoading: false,
  opLogs: [], opLoading: false
})

// 临时记录模块名（与 Logs.vue 一致）
const moduleNames = {
  auth: '认证', file: '文件', app: '应用', apps: '应用', site: '网站', cron: '计划任务',
  database: '数据库', ftp: 'FTP', docker: '容器', settings: '设置',
  waf: 'WAF', firewall: '防火墙', monitor: '监控', alert: '告警', backup: '备份',
  security: '安全', system: '系统', api_token: 'API令牌', temp_access: '临时访问', temp: '临时访问'
}
function moduleName(m) { return moduleNames[m] || m }

function openCreateTemp() {
  tempDialog.value = { show: true, name: '', expireSecs: 1800, loading: false }
}

async function loadTempList() {
  tempLoading.value = true
  try {
    const res = await request.get('/temp-access')
    tempList.value = res.data || []
  } finally { tempLoading.value = false }
}

async function submitCreateTemp() {
  tempDialog.value.loading = true
  try {
    const res = await request.post('/temp-access', {
      name: tempDialog.value.name.trim(),
      max_uses: 0, // 不限次数（前端 UI 已移除该选项；后端保留字段兼容）
      expire_secs: tempDialog.value.expireSecs
    })
    tempDialog.value.show = false
    tempLinkDialog.value = { show: true, link: res.data.link }
    loadTempList()
  } finally { tempDialog.value.loading = false }
}

async function deleteTemp(row) {
  try { await ElMessageBox.confirm(`确定删除临时链接「${row.name || '未命名'}」吗？访客将立即无法使用。`, '删除', { type: 'warning' }) } catch { return }
  await request.post(`/temp-access/${row.id}/delete`)
  ElMessage.success('已删除')
  loadTempList()
}

async function toggleTemp(row) {
  await request.post(`/temp-access/${row.id}/toggle`)
  ElMessage.success(row.status === 1 ? '已禁用' : '已启用')
  loadTempList()
}

// 打开"使用记录"弹窗：登录日志 + 操作日志 2 个 Tab
function openTempLogs(row) {
  tempLogsDialog.value = {
    show: true, tab: 'use', row,
    useLogs: [], useLoading: false,
    opLogs: [], opLoading: false
  }
  loadTempLogsDetail()
}

// 加载当前 Tab 对应的日志
async function loadTempLogsDetail() {
  if (!tempLogsDialog.value.row) return
  const id = tempLogsDialog.value.row.id
  if (tempLogsDialog.value.tab === 'use') {
    tempLogsDialog.value.useLoading = true
    try {
      const res = await request.get('/temp-access/use-logs', { params: { temp_access_id: id, limit: 200 } })
      tempLogsDialog.value.useLogs = res.data || []
    } finally { tempLogsDialog.value.useLoading = false }
  } else {
    tempLogsDialog.value.opLoading = true
    try {
      const res = await request.get('/temp-access/operations', { params: { temp_access_id: id, page_size: 200 } })
      tempLogsDialog.value.opLogs = res.data.list || []
    } finally { tempLogsDialog.value.opLoading = false }
  }
}

async function copyTempLink(row) {
  // 复制完整链接需要重新拼接（列表不返回明文 token，这里用请求 Host 拼接 + 从后端拿）
  try {
    const res = await request.get(`/temp-access/link?id=${row.id}`)
    await copyText(res.data.link)
  } catch {
    ElMessage.warning('复制失败，请重新生成链接')
  }
}

function isExpired(row) {
  return row.expire_at && new Date(row.expire_at) < new Date()
}

// ====== 工具 ======
function fmtTime(t) {
  return fmtTimeISO(t)
}
async function copyText(text) {
  if (!text) return
  try { await navigator.clipboard.writeText(text); ElMessage.success('已复制到剪贴板') }
  catch { ElMessage.warning('复制失败，请手动复制') }
}

// Tab 切换时持久化
watch(activeTab, () => {
  try { localStorage.setItem(SETTINGS_TAB_KEY, activeTab.value) } catch (e) { /* 忽略 */ }
})

onMounted(() => {
  loadInfo()
  loadSecurity()
  loadTokens()
  loadLiteSSL()
  loadTempList()
})
</script>

<style scoped>
.settings-page { padding: 0 8px; }
.settings-tabs :deep(.el-tabs__nav-wrap::after) { height: 1px; }

/* 卡片风格（与 Dashboard 保持一致） */
.setting-card {
  background: #ffffff;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.04);
  transition: box-shadow 0.2s;
}
.setting-card:hover { box-shadow: 0 4px 12px rgba(15, 23, 42, 0.06); }
.setting-card :deep(.el-card__header) {
  padding: 14px 18px;
  border-bottom: 1px solid #f1f5f9;
}
.setting-card :deep(.el-card__body) { padding: 6px 18px 16px; }

/* 卡片头部：图标 + 标题 + 可选右侧操作 */
.card-head {
  display: flex; align-items: center; justify-content: space-between; width: 100%;
}
.card-head > span:first-child {
  display: flex; align-items: center; gap: 8px;
  font-size: 15px; font-weight: 600; color: #1f2937;
}
.card-head > span:first-child .el-icon { color: #6366f1; font-size: 16px; }

/* 设置项列表（卡片内部紧凑行） */
.setting-list { list-style: none; margin: 0; padding: 0; }
.setting-item {
  display: flex; align-items: center; gap: 12px;
  padding: 14px 0;
  border-bottom: 1px dashed #f1f5f9;
}
.setting-item:last-child { border-bottom: none; }
.setting-info { flex: 1; min-width: 0; }
.setting-title { font-size: 14px; color: #1f2937; font-weight: 500; }
.setting-desc { font-size: 12px; color: #94a3b8; margin-top: 2px; }
.setting-value {
  font-size: 13px; color: #475569; max-width: 280px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

/* 令牌中心：两个不同的请求地址展示（左右两列） */
.endpoint-label { font-size: 13px; color: #475569; margin-bottom: 6px; font-weight: 500; }
.endpoint-input { font-family: 'SF Mono', Consolas, monospace; font-size: 13px; }

/* MCP 服务地址 */
.mcp-url {
  font-family: 'SF Mono', Consolas, monospace;
  background: #f8fafc; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 10px 14px; font-size: 13px; color: #1f2937;
  word-break: break-all;
}

/* IP 白名单展示 */
.ip-list { font-size: 12px; color: #475569; font-family: 'SF Mono', Consolas, monospace; }

.muted { color: #94a3b8; font-size: 13px; font-weight: normal; }

.entrance-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}
.entrance-row .el-input { flex: 1; min-width: 0; }
.mcp-p { margin: 0 0 8px; font-size: 13px; color: #475569; }
.mcp-pre {
  background: #f8fafc; border: 1px solid #e5e7eb; border-radius: 6px;
  padding: 10px 12px; font-size: 12px; line-height: 1.7;
  white-space: pre-wrap; word-break: break-all; margin: 0; color: #1f2937;
}
.log-box {
  background: #1e1e1e; color: #d4d4d4; padding: 12px; border-radius: 4px;
  font-size: 12px; max-height: 500px; overflow: auto; white-space: pre-wrap;
}
</style>