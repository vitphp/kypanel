<template>
  <div class="mail-shell">
    <!-- ===== 左栏：域名列表 ===== -->
    <aside class="mail-side">
      <div class="mail-side-head">
        <span class="mail-side-title">域名列表</span>
        <el-button size="small" :icon="Plus" @click="openAdd">添加域名</el-button>
      </div>
      <!-- 手机端：下拉切换域名 + 该域名操作按钮（桌面端隐藏） -->
      <div class="mail-side-mobile">
        <template v-if="list.length">
          <div class="msm-row">
            <el-select
              :model-value="currentDomainId"
              placeholder="选择域名"
              size="default"
              class="msm-select"
              @change="onMobileDomainChange"
            >
              <el-option v-for="row in list" :key="row.id" :label="row.domain" :value="row.id" />
            </el-select>
            <span v-if="currentDomain" class="mail-domain-status" :class="domainStatusClass(currentDomainId)">{{ domainStatusText(currentDomainId) }}</span>
          </div>
          <div v-if="currentDomain" class="msm-actions">
            <el-button size="small" :loading="checking" @click="checkOne(currentDomain)">检测</el-button>
            <el-button size="small" @click="openDnsGuide(currentDomain)">配置</el-button>
            <el-button size="small" :type="currentDomain.enabled ? 'warning' : 'success'" plain @click="toggleEnabledConfirm(currentDomain)">{{ currentDomain.enabled ? '停用' : '启用' }}</el-button>
            <el-button size="small" type="danger" plain @click="remove(currentDomain)">删除</el-button>
          </div>
        </template>
        <div v-else class="mail-domain-empty">还没有域名<br>点右上「添加域名」开始</div>
      </div>

      <div v-loading="loading" class="mail-domain-list">
        <div v-for="row in list" :key="row.id" class="mail-domain-item"
          :class="{ active: currentDomainId === row.id }"
          @click="selectDomain(row)">
          <div class="mail-domain-item-top">
            <span class="mail-domain-item-name">{{ row.domain }}</span>
            <span class="mail-domain-status" :class="domainStatusClass(row.id)">{{ domainStatusText(row.id) }}</span>
          </div>
          <div v-if="currentDomainId === row.id" class="mail-domain-actions">
            <el-button size="small" :loading="checking" @click.stop="checkOne(row)">检测</el-button>
            <el-button size="small" @click.stop="openDnsGuide(row)">配置</el-button>
            <el-button size="small" :type="row.enabled ? 'warning' : 'success'" plain @click.stop="toggleEnabledConfirm(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-button size="small" type="danger" plain @click.stop="remove(row)">删除</el-button>
          </div>
        </div>
        <div v-if="!list.length" class="mail-domain-empty">还没有域名<br>点右上「添加域名」开始</div>
      </div>
    </aside>

    <!-- ===== 右栏：当前域名功能 ===== -->
    <section class="mail-main">
      <div v-if="!currentDomain" class="mail-main-empty">
        <el-icon :size="40" color="#cbd5e1"><Message /></el-icon>
        <p>在左侧选择一个域名，查看它的收件箱 / 账号等</p>
      </div>

      <template v-else>
        <div class="mail-main-top">
          <el-tabs v-model="rightTab" class="mail-tabs" @tab-click="onRightTabClick">
            <el-tab-pane label="收件箱" name="inbox" />
            <el-tab-pane label="已发送" name="sent" />
            <el-tab-pane label="草稿" name="drafts" />
            <el-tab-pane label="账号管理" name="accounts" />
          </el-tabs>
        </div>

        <div class="mail-main-body">
          <!-- 收件箱 -->
          <div v-if="rightTab === 'inbox'" class="mail-inbox-pane">
            <div class="mail-pane-head">
              <div class="mail-pane-head-left">
                <span class="mail-pane-title">收件箱</span>
                <el-select v-model="inboxMailboxId" placeholder="选择邮箱账号" size="small" style="width: 220px" @change="loadInboxMessages" :disabled="!accList.length">
                  <el-option v-for="a in enabledAccOptions" :key="a.id" :label="a.address" :value="a.id" />
                </el-select>
              </div>
              <div class="mail-pane-head-actions">
                <template v-if="selectedMsgs.length">
                  <span class="msg-batch-info">已选 {{ selectedMsgs.length }} 封</span>
                  <el-button size="small" type="primary" plain :disabled="!selectedMsgs.some((m) => !m.seen)" @click="batchSetSeen(true)">标为已读</el-button>
                  <el-button size="small" plain :disabled="!selectedMsgs.some((m) => m.seen)" @click="batchSetSeen(false)">标为未读</el-button>
                  <el-button size="small" type="danger" plain @click="batchDelete">删除</el-button>
                  <el-button size="small" link @click="clearSelection">取消选择</el-button>
                </template>
                <template v-else>
                  <el-button size="small" type="primary" :icon="EditPen" :disabled="!accList.length" @click="openCompose">写信</el-button>
                  <el-button size="small" :disabled="!msgList.some((m) => !m.seen) || !inboxMailboxId" @click="markAllSeen">全部已读</el-button>
                  <el-button size="small" :icon="Refresh" circle :loading="inboxLoading" @click="loadInboxMessages" title="刷新" />
                </template>
              </div>
            </div>

            <template v-if="inboxMailboxId">
              <!-- 读信详情 -->
              <div v-if="detailMsg" class="mail-msg-detail">
                <div class="msg-detail-top">
                  <div class="msg-detail-subject">{{ detailMsg.subject || '(无主题)' }}</div>
                  <div class="msg-detail-meta">
                    <span>发件人：{{ detailMsg.from_name ? detailMsg.from_name + ' ' : '' }}<span class="mono">{{ detailMsg.from_addr }}</span></span>
                    <span class="msg-detail-date">{{ fmtTime(detailMsg.date) }}</span>
                  </div>
                </div>
                <div class="msg-body" v-html="detailMsg.html_body || toText(detailMsg.text_body)"></div>
                <!-- 附件 -->
                <div v-if="detailMsg.attachments && detailMsg.attachments.length" class="msg-attach-box">
                  <div class="msg-attach-title"><el-icon><Paperclip /></el-icon> 附件（{{ detailMsg.attachments.length }}）</div>
                  <div class="msg-attach-list">
                    <div v-for="a in detailMsg.attachments" :key="a.index" class="msg-attach-row">
                      <el-icon class="msg-attach-ic"><Document /></el-icon>
                      <span class="msg-attach-name">{{ a.filename }}</span>
                      <span class="msg-attach-size">{{ fmtSize(a.size) }}</span>
                      <el-button link type="primary" size="small" @click="downloadAttach(a)">下载</el-button>
                    </div>
                  </div>
                </div>
                <div class="msg-detail-actions">
                  <el-button size="small" :icon="Promotion" @click="replyDetail">回复</el-button>
                  <el-button size="small" :icon="Share" @click="forwardDetail">转发</el-button>
                  <el-button size="small" type="primary" plain @click="replyAllDetail">全部回复</el-button>
                  <el-button size="small" type="danger" plain @click="removeInboxMsg">删除</el-button>
                </div>
              </div>

              <!-- 消息列表 -->
              <div v-else class="msg-list-wrap" @mousedown="onInboxListMouseDown">
                <div v-show="dragRectVisible" class="msg-drag-rect" :style="dragRectStyle"></div>
                <el-table
                  ref="inboxTableRef"
                  v-loading="inboxLoading"
                  :data="msgList"
                  row-key="id"
                  max-height="560"
                  empty-text="还没有收到邮件"
                  :row-class-name="msgRowClass"
                  :row-style="{ cursor: 'pointer' }"
                  @row-click="onInboxRowClick"
                  @selection-change="onInboxSelectionChange"
                >
                  <el-table-column type="selection" :width="isMobile ? 40 : 50" reserve-selection />
                  <el-table-column :width="isMobile ? 22 : 30">
                    <template #default="{ row }">
                      <span class="msg-unread-dot" v-if="!row.seen"></span>
                    </template>
                  </el-table-column>
                  <el-table-column label="发件人" :min-width="isMobile ? 110 : 180">
                    <template #default="{ row }">
                      <span :class="{ 'msg-unread': !row.seen }">{{ row.from_name || row.from_addr || '(未知)' }}</span>
                      <span v-if="isMobile" class="msg-time-sub">{{ fmtTime(row.date) }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="主题" :min-width="isMobile ? 130 : 260">
                    <template #default="{ row }">
                      <el-icon v-if="row.has_attach" class="msg-attach-icon" title="含附件"><Paperclip /></el-icon>
                      <span :class="{ 'msg-unread': !row.seen }">{{ row.subject || '(无主题)' }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column v-if="!isMobile" label="时间" width="150">
                    <template #default="{ row }"><span class="msg-time">{{ fmtTime(row.date) }}</span></template>
                  </el-table-column>
                  <el-table-column label="操作" :width="isMobile ? 56 : 90" align="center">
                    <template #default="{ row }">
                      <el-button link type="danger" @click.stop="removeInboxMsgById(row)">删除</el-button>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </template>
            <template v-else>
              <div class="ph" style="padding:40px 20px">
                <el-icon :size="34" color="#cbd5e1"><Message /></el-icon>
                <p class="ph-sub">请选择该域名下的一个邮箱账号，查看它的收件箱。<br>若列表为空，请先在「账号管理」里添加账号。</p>
              </div>
            </template>
          </div>

          <!-- 已发送 -->
          <div v-else-if="rightTab === 'sent'" class="mail-inbox-pane">
            <div class="mail-pane-head">
              <div class="mail-pane-head-left">
                <span class="mail-pane-title">已发送</span>
                <el-select v-model="inboxMailboxId" placeholder="选择邮箱账号" size="small" style="width: 220px" @change="loadSentMessages" :disabled="!accList.length">
                  <el-option v-for="a in enabledAccOptions" :key="a.id" :label="a.address" :value="a.id" />
                </el-select>
              </div>
              <div class="mail-pane-head-actions">
                <el-button size="small" type="primary" :icon="EditPen" :disabled="!accList.length" @click="openCompose">写信</el-button>
                <el-button size="small" :icon="Refresh" circle :loading="sentLoading" @click="loadSentMessages" title="刷新" />
              </div>
            </div>
            <div v-if="inboxMailboxId" class="msg-list-wrap">
              <el-table v-loading="sentLoading" :data="sentList" row-key="id" max-height="560" empty-text="还没有发出去的邮件">
                <el-table-column label="收件人" :min-width="isMobile ? 130 : 200">
                  <template #default="{ row }">
                    <span class="sent-to">{{ row.to_addrs || '—' }}</span>
                    <span v-if="isMobile" class="msg-time-sub">{{ fmtTime(row.date) }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="主题" :min-width="isMobile ? 130 : 260">
                  <template #default="{ row }"><span>{{ row.subject || '(无主题)' }}</span></template>
                </el-table-column>
                <el-table-column v-if="!isMobile" label="时间" width="150">
                  <template #default="{ row }"><span class="msg-time">{{ fmtTime(row.date) }}</span></template>
                </el-table-column>
                <el-table-column label="操作" :width="isMobile ? 56 : 90" align="center">
                  <template #default="{ row }">
                    <el-button link type="danger" @click.stop="removeSentMsg(row)">删除</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
            <div v-else class="ph" style="padding:40px 20px">
              <el-icon :size="34" color="#cbd5e1"><Promotion /></el-icon>
              <p class="ph-sub">请选择该域名下的一个邮箱账号，查看已发送邮件。</p>
            </div>
          </div>

          <!-- 草稿箱 -->
          <div v-else-if="rightTab === 'drafts'" class="mail-inbox-pane">
            <div class="mail-pane-head">
              <div class="mail-pane-head-left">
                <span class="mail-pane-title">草稿箱</span>
                <el-select v-model="inboxMailboxId" placeholder="选择邮箱账号" size="small" style="width: 220px" @change="loadDrafts" :disabled="!accList.length">
                  <el-option v-for="a in enabledAccOptions" :key="a.id" :label="a.address" :value="a.id" />
                </el-select>
              </div>
              <div class="mail-pane-head-actions">
                <el-button size="small" type="primary" :icon="EditPen" :disabled="!accList.length" @click="openCompose">写信</el-button>
                <el-button size="small" :icon="Refresh" circle :loading="draftLoading" @click="loadDrafts" title="刷新" />
              </div>
            </div>
            <div v-if="inboxMailboxId" class="msg-list-wrap">
              <el-table v-loading="draftLoading" :data="draftList" row-key="id" max-height="560" empty-text="没有草稿" :row-style="{ cursor: 'pointer' }" @row-click="editDraft">
                <el-table-column label="收件人" :min-width="isMobile ? 130 : 200">
                  <template #default="{ row }">
                    <span class="sent-to">{{ row.to_addrs || '（未填写）' }}</span>
                    <span v-if="isMobile" class="msg-time-sub">{{ fmtTime(row.date) }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="主题" :min-width="isMobile ? 120 : 260">
                  <template #default="{ row }"><span>{{ row.subject || '(无主题)' }}</span></template>
                </el-table-column>
                <el-table-column v-if="!isMobile" label="时间" width="150">
                  <template #default="{ row }"><span class="msg-time">{{ fmtTime(row.date) }}</span></template>
                </el-table-column>
                <el-table-column label="操作" :width="isMobile ? 96 : 120" align="center">
                  <template #default="{ row }">
                    <el-button link type="primary" @click.stop="editDraft(row)">编辑</el-button>
                    <el-button link type="danger" @click.stop="removeDraft(row)">删除</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
            <div v-else class="ph" style="padding:40px 20px">
              <el-icon :size="34" color="#cbd5e1"><EditPen /></el-icon>
              <p class="ph-sub">请选择该域名下的一个邮箱账号，查看草稿。</p>
            </div>
          </div>

          <!-- 账号管理 -->
          <div v-else-if="rightTab === 'accounts'" class="mail-accounts-pane">
            <div class="mail-pane-head">
              <div class="mail-pane-head-left">
                <span class="mail-pane-title">账号管理</span>
                <!-- 未对接时才提示：此时下方列表为空、按钮不可用，需说明原因 -->
                <el-tag v-if="!statusMap[currentDomainId]?.ready" type="info" size="small">该域名尚未对接，暂不能添加用户</el-tag>
              </div>
              <el-button type="primary" size="small" :disabled="!statusMap[currentDomainId]?.ready" @click="openAccAdd">添加用户</el-button>
            </div>
            <template v-if="statusMap[currentDomainId]?.ready">
              <template v-if="accResult.length">
                <div class="acc-result-title">本次已生成（请复制保存）：</div>
                <div class="acc-result-box">
                  <div v-for="(a, i) in accResult" :key="i" class="acc-result-line">{{ a.address }}<span v-if="a.password">　密码：{{ a.password }}</span></div>
                </div>
              </template>
              <el-table v-loading="accLoading" :class="{ 'acc-table-gap': accResult.length }" :data="accList" size="small" max-height="280">
                <el-table-column label="邮箱地址" :min-width="isMobile ? 130 : 180">
                  <template #default="{ row }">
                    <div>{{ row.address }}</div>
                    <div v-if="isMobile" class="acc-addr-sub">容量 {{ row.quota_mb }} MB</div>
                  </template>
                </el-table-column>
                <el-table-column label="状态" :width="isMobile ? 68 : 80"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '启用' : '停用' }}</el-tag></template></el-table-column>
                <el-table-column v-if="!isMobile" prop="quota_mb" label="容量MB" width="92" />
                <el-table-column label="操作" :width="isMobile ? 110 : 130" align="right"><template #default="{ row }"><el-button link type="warning" size="small" @click="toggleAcc(row)">{{ row.enabled ? '停用' : '启用' }}</el-button><el-button link type="danger" size="small" @click="delAcc(row)">删除</el-button></template></el-table-column>
              </el-table>
            </template>
          </div>
        </div>
      </template>
    </section>


    <!-- ===== 添加域名：3 步向导 ===== -->
    <el-dialog v-model="addVisible" :title="addStep === 1 ? '第 1 步 · 填写你的域名' : addStep === 2 ? '第 2 步 · 去域名服务商添加解析' : '第 3 步 · 检测对接结果'" width="min(640px, 94vw)" align-center :close-on-click-modal="false">
      <!-- 步骤指示器 -->
      <el-steps :active="addStep - 1" align-center finish-status="success" class="md-steps">
        <el-step title="填域名" />
        <el-step title="配解析" />
        <el-step title="自动检测" />
      </el-steps>

      <!-- STEP 1：填域名 -->
      <div v-if="addStep === 1" class="md-step-body">
        <div class="md-field-hint">填一个你想开通邮箱的域名（例如 yoursite.com）。</div>
        <el-input v-model="addForm.domain" placeholder="example.com" size="large" @keyup.enter="goAddStep2" />
        <div v-if="addErr" class="md-err">{{ addErr }}</div>
      </div>

      <!-- STEP 2：配置 -->
      <div v-else-if="addStep === 2" class="md-step-body">
        <div class="md-step2-tip">请到你的<span class="md-link">域名服务商</span>（阿里云/腾讯云/Cloudflare）后台「DNS 解析」里，添加下面 {{ guide.needTxt ? 3 : 2 }} 条记录，复制「值」粘进服务商对应框即可。</div>
        <div class="md-guide-rec">
          <div class="md-rec-head"><b>记录 1</b><span class="md-rec-why">让 mail.域名 能找到这台服务器（收信用）</span></div>
          <div v-for="(f, j) in recFields1" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
        </div>
        <div class="md-guide-rec">
          <div class="md-rec-head"><b>记录 2</b><span class="md-rec-why">让全网把信投到这台服务器（收信用）</span></div>
          <div v-for="(f, j) in recFields2" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
        </div>
        <div class="md-guide-rec">
          <div class="md-rec-head"><b>记录 3</b><span class="md-rec-why">让这台服务器能发信、不被当垃圾（发信用）</span></div>
          <div v-for="(f, j) in recFields3" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
        </div>
        <div class="md-step2-foot">这 3 条都加到服务商后，点「我已添加完，下一步」。</div>
      </div>

      <!-- STEP 3：自动检测 -->
      <div v-else class="md-step-body">
        <div class="md-checking" :class="{ ok: addReady, checking: addChecking }">
          <template v-if="addChecking"><el-icon class="is-loading"><Loading /></el-icon> 正在检测你的解析是否生效…</template>
          <template v-else-if="addReady"><el-icon><CircleCheckFilled /></el-icon> 检测通过！</template>
          <template v-else><el-icon><WarningFilled /></el-icon> 还没检测通过</template>
        </div>
        <div class="md-check-detail">{{ addCheckDetail }}</div>
        <div v-if="addChecking" class="md-check-sub">每几秒自动重测。DNS 全球生效通常几分钟，若一直不通过请确认你确实在服务商后台添加了上面的记录。</div>
        <div v-if="!addReady && !addChecking" class="md-check-sub">可再等一会儿，DNS 仍在传播；面板会继续自动检测。</div>
      </div>

      <template #footer>
        <template v-if="addStep === 1">
          <el-button @click="addVisible = false">取消</el-button>
          <el-button type="primary" :loading="genGuideLoading" @click="goAddStep2">下一步</el-button>
        </template>
        <template v-else-if="addStep === 2">
          <el-button @click="addStep = 1">上一步</el-button>
          <el-button type="primary" @click="goAddStep3">我已添加完，下一步</el-button>
        </template>
        <template v-else>
          <el-button @click="addStep = 2">上一步（回去改配置）</el-button>
          <el-button type="primary" :disabled="!addReady" :loading="saving" @click="confirmAdd">检测通过，确认添加</el-button>
        </template>
      </template>
    </el-dialog>

    <!-- 域名配置：解析引导 + 门户网站 -->
    <el-dialog v-model="configVisible" title="域名配置" width="min(760px, 94vw)" align-center>
      <el-tabs v-model="configTab" class="md-config-tabs">
        <el-tab-pane label="域名解析" name="dns">
          <div v-if="dnsGuideData" class="md-dnsguide-body">
            <div v-for="(f, j) in dnsGuideData" :key="j" class="md-rec-field"><span class="md-rec-label">{{ f.label }}</span><span class="md-rec-val">{{ f.value }}</span><el-button v-if="f.copiable" link type="primary" size="small" @click="copy(f.value)">复制</el-button></div>
          </div>
          <div class="md-dnsguide-foot">
            <el-button :loading="checking" @click="checkOne(currentRow)">再检测一次</el-button>
            <span v-if="currentRow && statusMap[currentRow.id]?.ready" class="md-status-ok" style="font-size:13px">✓ 已对接</span>
          </div>
        </el-tab-pane>
        <el-tab-pane label="门户网站" name="portal" lazy>
          <MailPortalPanel ref="portalPanelRef" :domain="currentRow" @saved="load" />
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <el-dialog v-model="accAddVisible" title="添加用户" width="min(560px, 92vw)" align-center>
      <el-tabs v-model="accountTab" class="acc-tabs">
        <el-tab-pane label="单个添加" name="single">
          <div class="acc-form">
            <div class="acc-row"><span class="acc-label">邮箱名</span><el-input v-model="accSingle.name" placeholder="如 admin（将创建 admin@域名）" style="max-width:360px" /></div>
            <div class="acc-row"><span class="acc-label">密码</span><el-input v-model="accSingle.password" type="password" show-password placeholder="登录密码" style="max-width:360px" /></div>
            <div class="acc-row"><span class="acc-label">容量(MB)</span><el-input-number v-model="accSingle.quota" :min="1" :max="102400" style="max-width:200px" /></div>
            <div class="acc-row acc-submit"><el-button type="primary" :loading="accBusy" @click="doAddOne">添加这个账号</el-button></div>
          </div>
        </el-tab-pane>
        <el-tab-pane label="批量添加" name="batch">
          <div class="acc-batch-tip">每行一个，格式：<code>邮箱名 密码</code>（或 <code>邮箱名:密码</code>）</div>
          <el-input v-model="accBatch.lines" type="textarea" :rows="5" placeholder="admin1 密码123&#10;sales1:pass888" />
          <div class="acc-row acc-submit"><el-button type="primary" :loading="accBusy" @click="doAddBatch">批量添加</el-button></div>
        </el-tab-pane>
        <el-tab-pane label="随机生成" name="random">
          <div class="acc-rand-grid">
            <div class="acc-row"><span class="acc-label">前缀</span><el-input v-model="accRandom.prefix" placeholder="如 vip" style="max-width:160px" /></div>
            <div class="acc-row"><span class="acc-label">随机位数</span><el-input-number v-model="accRandom.length" :min="1" :max="16" /></div>
            <div class="acc-row"><span class="acc-label">字符</span><el-select v-model="accRandom.digit" style="width:150px"><el-option label="字母+数字" :value="0" /><el-option label="纯数字" :value="1" /><el-option label="纯字母" :value="2" /></el-select></div>
            <div class="acc-row"><span class="acc-label">数量</span><el-input-number v-model="accRandom.count" :min="1" :max="200" /></div>
            <div class="acc-row"><span class="acc-label">统一密码</span><el-input v-model="accRandom.password" placeholder="留空自动生成" style="max-width:200px" /></div>
            <div class="acc-row acc-submit"><el-button type="primary" :loading="accBusy" @click="doAddRandom">生成账号</el-button></div>
          </div>
        </el-tab-pane>
      </el-tabs>
      <template #footer>
        <el-button @click="accAddVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <MailCompose
      v-model="composeVisible"
      :accounts="enabledAccOptions"
      :default-from-id="inboxMailboxId"
      :prefill="composePrefill"
      :mode="composeMode"
      :draft-id="editingDraftId"
      @sent="onMailSent"
      @draft-saved="onDraftSaved"
    />

  </div>
</template>

<script setup>
import { ref, watch, nextTick, onMounted, onBeforeUnmount, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Loading, CircleCheckFilled, WarningFilled, Message, Promotion, EditPen, Refresh, Share, Paperclip, Document } from '@element-plus/icons-vue'
import MailCompose from './MailCompose.vue'
import MailPortalPanel from './MailPortalPanel.vue'
import { useIsMobile } from '../../composables/useIsMobile'
import {
  listMailDomains, addMailDomain, updateMailDomain, deleteMailDomain,
  getDomainGuide, checkDomainReady, checkMailDomainsReady,
  listMailAccounts, addMailAccount, addMailAccountsBatch, randomMailAccounts,
  deleteMailAccount, setMailAccountEnabled,
  listMailMessages, getMailMessage, deleteMailMessage, setMailMessagesSeen,
  markMailAllSeen, downloadMailAttachment, saveMailDraft
} from '../../api/mail'

const list = ref([])
const loading = ref(false)
const statusMap = ref({}) // domainId -> {ready, detail}
const checking = ref(false)
const checkingAll = ref(false)   // 后台批量检测中（用于状态文字显示「检测中」）

// 本组件自行声明 isMobile
const { isMobile } = useIsMobile()

// 添加向导
const addVisible = ref(false)
const addStep = ref(1)
const addErr = ref('')
const addForm = ref({ domain: '' })
const genGuideLoading = ref(false)
const guide = ref({ domain: '', mail_server: '', spf_value: '', mx_host_name: '' })
const addReady = ref(false)
const addChecking = ref(false)
const addCheckDetail = ref('')
const saving = ref(false)
let addCheckTimer = null

const recFields1 = computed(() => [
  { label: '记录类型', value: 'A' },
  { label: '主机记录', value: 'mail' },
  { label: '记录值', value: guide.value.mail_server, copiable: true }
])
const recFields2 = computed(() => [
  { label: '记录类型', value: 'MX' },
  { label: '主机记录', value: '@（留空）' },
  { label: '记录值', value: guide.value.mx_host_name, copiable: true },
  { label: '优先级', value: '10' }
])
const recFields3 = computed(() => [
  { label: '记录类型', value: 'TXT' },
  { label: '主机记录', value: '@（留空）' },
  { label: '记录值', value: guide.value.spf_value, copiable: true }
])

// 域名配置（列表"配置"按钮打开：解析引导 + 门户网站）
const configVisible = ref(false)
const configTab = ref('dns')
const dnsGuideData = ref([])
const currentRow = ref(null)
const portalPanelRef = ref(null)

// 切到「门户网站」页签时再读取一次最新配置，避免沿用上一次打开的旧数据
watch([configVisible, configTab], ([vis, tab]) => {
  if (vis && tab === 'portal') nextTick(() => portalPanelRef.value?.load())
})

// 布局：当前选中的域名 + 右侧 tab
const currentDomain = ref(null)     // 当前选中的域名行对象
// 右侧顶部 tab：inbox/sent/drafts/accounts，浏览器持久化（localStorage），下次进入沿用上次选择
const MAIL_TAB_KEY = 'mail_right_tab'
const VALID_TABS = ['inbox', 'sent', 'drafts', 'accounts']
const savedTab = (() => {
  try { const t = localStorage.getItem(MAIL_TAB_KEY); return VALID_TABS.includes(t) ? t : 'accounts' } catch { return 'accounts' }
})()
const rightTab = ref(savedTab)
watch(rightTab, (v) => {
  try { localStorage.setItem(MAIL_TAB_KEY, v) } catch { /* ignore */ }
  // 进入收件箱且已有选中域名时，加载账号并选第一个查看
  if (v === 'inbox' && currentDomainId.value) {
    loadAccounts().then(() => { if (rightTab.value === 'inbox') resetInboxForDomain() })
  }
  if (v === 'sent' && currentDomainId.value) {
    loadAccounts().then(() => { if (rightTab.value === 'sent') loadSentMessages() })
  }
  if (v === 'drafts' && currentDomainId.value) {
    loadAccounts().then(() => { if (rightTab.value === 'drafts') loadDrafts() })
  }
})
const currentDomainId = ref(0)      // 当前域名 id（账号操作等用）

function selectDomain(row) {
  currentDomain.value = row
  currentDomainId.value = row.id
  // 切到该域名时刷新账号列表；若当前在收件箱/已发送则同步加载
  loadAccounts().then(() => {
    if (rightTab.value === 'inbox') resetInboxForDomain()
    else if (rightTab.value === 'sent') loadSentMessages()
    else if (rightTab.value === 'drafts') loadDrafts()
  })
}

function onMobileDomainChange(id) {
  const row = list.value.find((x) => x.id === id)
  if (row) selectDomain(row)
}

// 域名对接状态：文字 / 颜色 class（桌面卡片与手机下拉共用）
function domainStatusText(id) {
  if (statusMap.value[id]?.ready) return '已对接'
  if (checkingAll.value && statusMap.value[id] === undefined) return '检测中'
  return '未完成'
}
function domainStatusClass(id) {
  if (statusMap.value[id]?.ready) return 'ok'
  if (checkingAll.value && statusMap.value[id] === undefined) return 'checking'
  return 'bad'
}

// 账号
const accAddVisible = ref(false)   // 添加用户弹窗
const accountTab = ref('single')
const accList = ref([])
const accLoading = ref(false)
const accBusy = ref(false)
const accResult = ref([])
const accSingle = ref({ name: '', password: '', quota: 1024 })
const accBatch = ref({ lines: '' })
const accRandom = ref({ prefix: '', length: 6, digit: 0, count: 10, password: '' })

// ===== 收件箱 =====
const inboxMailboxId = ref(0)   // 当前查看收件箱的账号 id（该域名下）
const msgList = ref([])
const inboxLoading = ref(false)
const detailMsg = ref(null)     // 正在阅读的邮件详情（null=列表）
const enabledAccOptions = computed(() => accList.value.filter((a) => a.enabled))
const inboxTableRef = ref(null) // 表格引用（用于清空选择）
const selectedMsgs = ref([])     // 复选框选中的消息
const selectionIds = computed(() => selectedMsgs.value.map((m) => m.id))

// 框选多选
const dragRectVisible = ref(false)
const dragRectStyle = ref({})
let dragSel = null        // { startIdx, lastIdx, moved, startX, startY, baseSelection, isBlankDrag }
let suppressNextRowClick = false

function onInboxSelectionChange(rows) {
  selectedMsgs.value = rows || []
}

// 写信
const composeVisible = ref(false)
const composePrefill = ref(null)
const composeMode = ref('')
function openCompose() {
  if (!accList.value.length) return ElMessage.warning('该域名下还没有邮箱账号')
  composePrefill.value = null
  composeMode.value = ''
  editingDraftId.value = 0
  composeVisible.value = true
}
function onMailSent() {
  editingDraftId.value = 0
  if (rightTab.value === 'sent') loadSentMessages()
  if (rightTab.value === 'drafts') loadDrafts()
}
function onDraftSaved(id) {
  editingDraftId.value = id || 0
  if (rightTab.value === 'drafts') loadDrafts()
}

// 回复 / 转发 / 全部回复
function replyDetail() { openReply('reply') }
function replyAllDetail() { openReply('replyAll') }
function forwardDetail() { openReply('forward') }

function openReply(kind) {
  if (!detailMsg.value) return
  const m = detailMsg.value
  const original = {
    from: m.from_addr,
    fromName: m.from_name,
    to: m.to_addrs || m.address,
    subject: m.subject || '',
    html: m.html_body || '',
    text: m.text_body || '',
    date: m.date
  }
  const quotedHtml = buildQuotedHtml(original, kind === 'forward')
  let to = ''
  let subject = original.subject
  if (kind === 'reply' || kind === 'replyAll') {
    to = original.from
    subject = /^re:/i.test(original.subject) ? original.subject : 'Re: ' + original.subject
    if (kind === 'replyAll' && original.to) {
      to = (original.from + ', ' + original.to).trim().replace(/^,\s*|,\s*$/g, '')
    }
  } else {
    subject = /^fwd:/i.test(original.subject) ? original.subject : 'Fwd: ' + original.subject
  }
  composePrefill.value = { to, subject, html: quotedHtml, text: '' }
  composeMode.value = kind === 'reply' ? 'reply' : (kind === 'forward' ? 'forward' : 'reply')
  editingDraftId.value = 0
  composeVisible.value = true
}

// 构造引用原邮件的 HTML 正文
function buildQuotedHtml(o, isForward) {
  const date = o.date ? new Date(o.date * 1000).toLocaleString('zh-CN') : ''
  const who = (o.fromName && o.fromName.trim()) || o.from
  const header = isForward
    ? `<br><br><div class="mc-quote-head">---------- 转发邮件 ----------</div><div class="mc-quote-meta">发件人: ${escHtml(who)} &lt;${escHtml(o.from)}&gt;<br>时&nbsp;&nbsp;间: ${escHtml(date)}<br>收件人: ${escHtml(o.to || '')}<br>主&nbsp;&nbsp;题: ${escHtml(o.subject)}</div><br>`
    : `<br><div class="mc-quote-head">在 ${escHtml(date)}, ${escHtml(who)} &lt;${escHtml(o.from)}&gt; 写道：</div>`
  const body = isForward ? (o.html || escHtml(o.text || '')) : (o.html || escHtml(o.text || ''))
  return header + `<blockquote class="mc-quote">${body}</blockquote>`
}

function escHtml(s) {
  return String(s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

// 草稿箱
const draftList = ref([])
const draftLoading = ref(false)
const editingDraftId = ref(0)
async function loadDrafts() {
  if (!inboxMailboxId.value) return
  draftLoading.value = true
  try {
    const { data } = await listMailMessages(inboxMailboxId.value, 'drafts')
    draftList.value = data || []
  } catch { /* handled */ }
  finally { draftLoading.value = false }
}
async function editDraft(row) {
  try {
    const { data } = await getMailMessage(inboxMailboxId.value, row.id)
    editingDraftId.value = row.id
    composePrefill.value = {
      to: data.to_addrs || '',
      subject: data.subject || '',
      html: data.html_body || '',
      text: data.text_body || ''
    }
    composeMode.value = 'draft'
    composeVisible.value = true
  } catch (e) { ElMessage.error('打开草稿失败') }
}
async function removeDraft(row) {
  try { await ElMessageBox.confirm('确定删除这封草稿？', '删除', { type: 'warning' }) } catch { return }
  try {
    await deleteMailMessage(inboxMailboxId.value, row.id)
    ElMessage.success('已删除')
    loadDrafts()
  } catch (e) { ElMessage.error('删除失败') }
}

// 已发送
const sentList = ref([])
const sentLoading = ref(false)
async function loadSentMessages() {
  if (!inboxMailboxId.value) return
  sentLoading.value = true
  try {
    const { data } = await listMailMessages(inboxMailboxId.value, 'sent')
    sentList.value = data || []
  } catch { /* handled */ }
  finally { sentLoading.value = false }
}
async function removeSentMsg(row) {
  try { await ElMessageBox.confirm('确定删除这封已发送邮件？', '删除', { type: 'warning' }) } catch { return }
  try {
    await deleteMailMessage(inboxMailboxId.value, row.id)
    ElMessage.success('已删除')
    loadSentMessages()
  } catch (e) { ElMessage.error('删除失败') }
}

// 顶部 tab 点击：已在「收件箱」且正在读某封邮件时，再点「收件箱」返回列表
function onRightTabClick(pane) {
  const name = (pane && typeof pane === 'object' ? pane.paneName : pane) || ''
  if (name === 'inbox' && detailMsg.value) {
    detailMsg.value = null
  }
}

// 点击复选框时不应触发打开邮件；框选拖动结束后也不应打开行
function onInboxRowClick(row, column, event) {
  const t = event?.target
  if (t && t.closest && t.closest('.el-checkbox, .el-table-column--selection')) return
  if (suppressNextRowClick) { suppressNextRowClick = false; return }
  openInboxMsg(row)
}

function onInboxListMouseDown(e) {
  if (e.button !== 0) return
  const t = e.target
  if (!t || !t.closest) return
  if (t.closest('.el-checkbox, button, input, a, .el-table__header-wrapper, .el-table__footer-wrapper, .el-table__empty-block, .el-pagination, .pagination-container, .msg-unread-dot')) return
  // 阻止默认的文本选择行为，否则拖动会变成选文字
  e.preventDefault()
  try { window.getSelection()?.removeAllRanges() } catch { /* ignore */ }
  const idx = findInboxRowAt(e.clientX, e.clientY)
  const baseSel = new Set(selectedMsgs.value.map((m) => m.id))
  dragSel = {
    startIdx: idx, lastIdx: idx, moved: false,
    startX: e.clientX, startY: e.clientY,
    isBlankDrag: idx < 0, onSelectedRow: false, baseSel,
  }
  dragRectVisible.value = true
  updateInboxDragRect(e)
  document.addEventListener('mousemove', onInboxDragMove)
  document.addEventListener('mouseup', onInboxDragUp)
}

function updateInboxDragRect(e) {
  if (!dragSel) return
  const sx = Math.min(dragSel.startX, e.clientX)
  const sy = Math.min(dragSel.startY, e.clientY)
  const ex = Math.max(dragSel.startX, e.clientX)
  const ey = Math.max(dragSel.startY, e.clientY)
  dragRectStyle.value = {
    position: 'fixed',
    left: sx + 'px',
    top: sy + 'px',
    width: (ex - sx) + 'px',
    height: (ey - sy) + 'px',
  }
}

function findInboxRowAt(x, y) {
  const rows = document.querySelectorAll('.mail-inbox-pane .el-table__body-wrapper table tbody .el-table__row')
  if (!rows.length) return -1
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect()
    if (r.height > 0 && y >= r.top && y <= r.bottom) return i
  }
  return -1
}
function findInboxRowAtClamped(x, y) {
  const rows = document.querySelectorAll('.mail-inbox-pane .el-table__body-wrapper table tbody .el-table__row')
  if (!rows.length) return -1
  const firstTop = rows[0].getBoundingClientRect().top
  const lastBottom = rows[rows.length - 1].getBoundingClientRect().bottom
  if (y < firstTop) return 0
  if (y > lastBottom) return rows.length - 1
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i].getBoundingClientRect()
    if (r.height > 0 && y >= r.top && y <= r.bottom) return i
  }
  return -1
}

function onInboxDragMove(e) {
  if (!dragSel) return
  updateInboxDragRect(e)
  if (!dragSel.moved && Math.abs(e.clientX - dragSel.startX) + Math.abs(e.clientY - dragSel.startY) > 5) {
    dragSel.moved = true
    if (dragSel.isBlankDrag && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
      inboxTableRef.value?.clearSelection()
      dragSel.baseSel = new Set()
    }
  }
  if (!dragSel.moved) return
  const idx = findInboxRowAtClamped(e.clientX, e.clientY)
  if (idx < 0) return
  if (dragSel.startIdx < 0) dragSel.startIdx = idx
  if (idx === dragSel.lastIdx) return
  dragSel.lastIdx = idx
  const rows = msgList.value
  const a = Math.min(dragSel.startIdx, idx)
  const b = Math.max(dragSel.startIdx, idx)
  const inRange = new Set(rows.slice(a, b + 1).map((r) => r.id))
  if (e.ctrlKey || e.metaKey) {
    // Ctrl/Cmd：与原选区做 XOR
    rows.forEach((r) => {
      const inBase = dragSel.baseSel.has(r.id)
      const inDrag = inRange.has(r.id)
      inboxTableRef.value?.toggleRowSelection(r, inBase ? !inDrag : inDrag)
    })
  } else {
    rows.forEach((r) => inboxTableRef.value?.toggleRowSelection(r, inRange.has(r.id)))
  }
}

function onInboxDragUp() {
  document.removeEventListener('mousemove', onInboxDragMove)
  document.removeEventListener('mouseup', onInboxDragUp)
  dragRectVisible.value = false
  const d = dragSel
  dragSel = null
  if (!d) return
  if (d.moved) {
    suppressNextRowClick = true
  } else if (d.isBlankDrag) {
    clearSelection()
  }
}

function clearSelection() {
  inboxTableRef.value?.clearSelection()
  selectedMsgs.value = []
}

function resetInboxForDomain() {
  detailMsg.value = null
  inboxMailboxId.value = 0
  msgList.value = []
  selectedMsgs.value = []
  const first = enabledAccOptions.value[0]
  if (first) {
    inboxMailboxId.value = first.id
    loadInboxMessages()
  }
}

async function loadInboxMessages() {
  if (!inboxMailboxId.value) return
  detailMsg.value = null
  inboxLoading.value = true
  try {
    const { data } = await listMailMessages(inboxMailboxId.value, 'inbox')
    msgList.value = data || []
    notifyMailChanged()
  } catch { /* error handled by interceptor */ }
  finally { inboxLoading.value = false }
}

// 通知侧边栏刷新邮件未读红点（与收件箱列表同步）
function notifyMailChanged() {
  try { window.dispatchEvent(new Event('mail-list-refreshed')) } catch { /* ignore */ }
}

// 静默刷新收件箱（不弹 loading、不打断正在阅读的详情）
async function silentRefreshInbox() {
  if (rightTab.value !== 'inbox' || !inboxMailboxId.value || inboxLoading.value) return
  try {
    const { data } = await listMailMessages(inboxMailboxId.value, 'inbox')
    msgList.value = data || []
    notifyMailChanged()
  } catch { /* 忽略，下轮重试 */ }
}

let inboxPollTimer = null
function startInboxPoll() {
  stopInboxPoll()
  inboxPollTimer = setInterval(silentRefreshInbox, 10000)
}
function stopInboxPoll() {
  if (inboxPollTimer) { clearInterval(inboxPollTimer); inboxPollTimer = null }
}

function msgRowClass({ row }) {
  return row.seen ? '' : 'msg-row-unread'
}

async function markAllSeen() {
  if (!inboxMailboxId.value) return
  inboxLoading.value = true
  try {
    await markMailAllSeen(inboxMailboxId.value)
    ElMessage.success('已全部标记为已读')
    msgList.value.forEach((m) => { m.seen = true })
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '操作失败') }
  finally { inboxLoading.value = false }
}

async function batchSetSeen(seen) {
  const ids = selectionIds.value
  if (!ids.length) return
  inboxLoading.value = true
  try {
    await setMailMessagesSeen(inboxMailboxId.value, ids, seen)
    ElMessage.success(seen ? '已标为已读' : '已标为未读')
    clearSelection()
    await loadInboxMessages()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '操作失败') }
  finally { inboxLoading.value = false }
}

async function batchDelete() {
  const ids = selectionIds.value
  if (!ids.length) return
  try {
    await ElMessageBox.confirm(`确定删除选中的 ${ids.length} 封邮件？`, '批量删除', { type: 'warning' })
  } catch { return }
  inboxLoading.value = true
  try {
    for (const id of ids) {
      await deleteMailMessage(inboxMailboxId.value, id)
    }
    ElMessage.success(`已删除 ${ids.length} 封`)
    clearSelection()
    loadInboxMessages()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '删除失败') }
  finally { inboxLoading.value = false }
}

async function openInboxMsg(row) {
  try {
    const { data } = await getMailMessage(inboxMailboxId.value, row.id)
    detailMsg.value = data
    const m = msgList.value.find((x) => x.id === row.id)
    if (m) m.seen = true
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '读取失败') }
}

async function removeInboxMsg() {
  if (!detailMsg.value) return
  try {
    await ElMessageBox.confirm('确定删除这封邮件？', '删除', { type: 'warning' })
  } catch { return }
  await removeInboxMsgById(detailMsg.value)
}

async function removeInboxMsgById(row) {
  try {
    await deleteMailMessage(inboxMailboxId.value, row.id)
    ElMessage.success('已删除')
    if (detailMsg.value && detailMsg.value.id === row.id) detailMsg.value = null
    loadInboxMessages()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '删除失败') }
}

function fmtTime(ts) {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// 纯文本正文转义，避免 HTML 注入
function toText(t) {
  const s = String(t || '')
  return s.split('\n').map((l) => `<div>${escapeHtml(l)}</div>`).join('')
}
function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function fmtSize(n) {
  n = Number(n) || 0
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(2) + ' MB'
}

// 下载附件：带鉴权拉 blob 后触发浏览器保存
async function downloadAttach(a) {
  try {
    const res = await downloadMailAttachment(inboxMailboxId.value, detailMsg.value.id, a.index)
    const blob = res.data instanceof Blob ? res.data : new Blob([res.data])
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = a.filename || 'attachment'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  } catch (e) {
    ElMessage.error('下载失败')
  }
}

async function load() {
  loading.value = true
  try {
    const { data } = await listMailDomains()
    list.value = data || []
    // 默认选中第一个域名（先把列表展示出来）
    if (list.value.length && !currentDomain.value) {
      selectDomain(list.value[0])
    }
  } finally {
    loading.value = false
  }
  // 后台检测所有域名对接状态，完成后再更新状态文字（不阻塞列表展示）
  checkAll().catch(() => {})
}

async function checkAll() {
  const ids = list.value.map((x) => x.id)
  if (!ids.length) return
  checkingAll.value = true
  try {
    const { data } = await checkMailDomainsReady(ids)
    if (data) {
      statusMap.value = {}
      for (const id of ids) {
        const s = data[id]
        if (s) statusMap.value[id] = { ready: !!s.ready, detail: s.not_ready_msg || '' }
      }
    }
  } catch { /* 忽略网络失败，保持原状 */ }
  finally { checkingAll.value = false }
}

async function checkOneById(id) {
  try {
    const { data } = await checkMailDomainsReady([id])
    const s = data?.[id]
    if (s) statusMap.value[id] = { ready: !!s.ready, detail: s.not_ready_msg || '' }
  } catch { /* ignore */ }
}

async function checkOne(row) {
  checking.value = true
  try {
    await checkOneById(row.id)
  } finally {
    checking.value = false
  }
}

// 打开 DNS 引导（给"未对接"域名展示记录值，供去服务商配置）
async function openDnsGuide(row) {
  currentRow.value = row
  configTab.value = 'dns'
  configVisible.value = true
  await checkOne(row)
  let srv = ''
  try {
    const { data } = await getDomainGuide(row.domain)
    srv = data?.mail_server || ''
  } catch { /* ignore */ }
  if (!srv) srv = '（自动检测到的本机IP）'
  dnsGuideData.value = [
    { label: 'A 记录 · 记录值', value: srv, copiable: true },
    { label: 'MX 记录 · 记录值', value: 'mail.' + row.domain, copiable: true },
    { label: 'TXT(SPF) · 记录值', value: 'v=spf1 ip4:' + srv + ' ~all', copiable: true }
  ]
}

// ===== 添加向导 =====
function openAdd() {
  addForm.value = { domain: '' }
  addErr.value = ''
  addStep.value = 1
  addReady.value = false
  addVisible.value = true
}

async function goAddStep2() {
  const d = addForm.value.domain.trim().toLowerCase()
  if (!d) return (addErr.value = '请输入域名')
  if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(d)) {
    return (addErr.value = '域名格式不对，示例：example.com')
  }
  if (list.value.some((x) => x.domain === d)) {
    return (addErr.value = '这个域名已经在列表里了')
  }
  addErr.value = ''
  genGuideLoading.value = true
  try {
    const { data } = await getDomainGuide(d)
    guide.value = data || {}
    guide.value.domain = d
    addStep.value = 2
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '获取配置失败')
  } finally {
    genGuideLoading.value = false
  }
}

function goAddStep3() {
  addStep.value = 3
  addReady.value = false
  addChecking.value = true
  addCheckDetail.value = ''
  runAutoCheck()
}

async function runAutoCheck() {
  const d = guide.value.domain
  if (!d) return
  try {
    const { data } = await checkDomainReady(d)
    if (data?.ready) {
      addReady.value = true
      addChecking.value = false
      addCheckDetail.value = data.detail || '检测通过'
      return
    }
    addCheckDetail.value = data?.detail || '还没检测通过'
  } catch (e) {
    addCheckDetail.value = ''
  }
  // 未通过：继续轮询
  if (addVisible.value && addStep.value === 3 && !addReady.value) {
    addCheckTimer = setTimeout(runAutoCheck, 4000)
  } else {
    addChecking.value = false
  }
}

async function confirmAdd() {
  if (!addReady.value) return
  saving.value = true
  try {
    const { data } = await addMailDomain({ domain: guide.value.domain })
    ElMessage.success('域名已添加并检测通过')
    addVisible.value = false
    if (addCheckTimer) clearTimeout(addCheckTimer)
    await load()
  } catch (e) {
    ElMessage.error(e?.response?.data?.msg || '添加失败')
  } finally {
    saving.value = false
  }
}

// ===== 域名其他操作 =====
async function toggleEnabledConfirm(row) {
  const action = row.enabled ? '停用' : '启用'
  try { await ElMessageBox.confirm(`确定${action}域名「${row.domain}」吗？`, action, { type: 'warning' }) } catch { return }
  await toggleEnabled(row)
}
async function toggleEnabled(row) {
  try {
    await updateMailDomain(row.id, { enabled: !row.enabled })
    load()
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '操作失败') }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`确定删除域名「${row.domain}」吗？`, '删除', { type: 'warning' }) } catch { return }
  try { await deleteMailDomain(row.id); ElMessage.success('已删除'); load() } catch (e) { ElMessage.error(e?.response?.data?.msg || '删除失败') }
}

// ===== 账号 =====
function openAccAdd() {
  accAddVisible.value = true
}
async function loadAccounts() {
  if (!currentDomainId.value) return
  accLoading.value = true
  try {
    const { data } = await listMailAccounts(currentDomainId.value)
    accList.value = data || []
  } finally { accLoading.value = false }
}

async function doAddOne() {
  const name = accSingle.value.name.trim()
  if (!name) return ElMessage.warning('请输入邮箱名')
  if (!accSingle.value.password) return ElMessage.warning('请输入密码')
  accBusy.value = true
  try {
    const { data } = await addMailAccount({ domain_id: currentDomainId.value, name, password: accSingle.value.password, quota: accSingle.value.quota })
    accResult.value = [{ address: data.address, password: accSingle.value.password }]
    accSingle.value.name = ''
    await loadAccounts()
    accAddVisible.value = false
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '添加失败') } finally { accBusy.value = false }
}

async function doAddBatch() {
  accBusy.value = true
  try {
    const { data } = await addMailAccountsBatch({ domain_id: currentDomainId.value, lines: accBatch.value.lines })
    ElMessage.success(`成功 ${data.created} 个${data.failed?.length ? '，失败 ' + data.failed.length + ' 个' : ''}`)
    await loadAccounts()
    accAddVisible.value = false
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '批量失败') } finally { accBusy.value = false }
}

async function doAddRandom() {
  accBusy.value = true
  try {
    const { data } = await randomMailAccounts({ domain_id: currentDomainId.value, ...accRandom.value })
    const dom = currentDomain.value ? currentDomain.value.domain : ''
    accResult.value = (data.accounts || []).map((a) => ({ address: a.name + '@' + dom, password: a.password }))
    if (data.failed?.length) ElMessage.warning(`失败 ${data.failed.length} 个`)
    await loadAccounts()
    accAddVisible.value = false
  } catch (e) { ElMessage.error(e?.response?.data?.msg || '生成失败') } finally { accBusy.value = false }
}

async function toggleAcc(row) {
  try { await setMailAccountEnabled(row.id, !row.enabled); await loadAccounts() } catch (e) { ElMessage.error('操作失败') }
}
async function delAcc(row) {
  try { await ElMessageBox.confirm(`确定删除账号 ${row.address} 吗？`, '删除', { type: 'warning' }) } catch { return }
  try { await deleteMailAccount(row.id); await loadAccounts() } catch (e) { ElMessage.error('删除失败') }
}

async function copy(text) {
  try { await navigator.clipboard.writeText(String(text || '')); ElMessage.success('已复制') } catch { ElMessage.error('复制失败') }
}

onMounted(() => { load(); startInboxPoll() })
onBeforeUnmount(() => { if (addCheckTimer) clearTimeout(addCheckTimer); stopInboxPoll() })
</script>

<style scoped>
/* ===== 左右布局外壳 ===== */
.mail-shell { display: flex; height: calc(100vh - 97px); min-height: 480px; }
/* 左栏：域名列表 */
.mail-side { width: 250px; flex: 0 0 250px; display: flex; flex-direction: column; background: #fff; overflow: hidden; }
.mail-side-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 12px 12px; border-bottom: 1px solid #f1f5f9; }
.mail-side-title { font-weight: 700; color: #0f172a; font-size: 14px; }
.mail-domain-list { flex: 1; overflow-y: auto; padding: 8px; }
/* 手机专用的域名下拉区：桌面/平板隐藏 */
.mail-side-mobile { display: none; }
.mail-domain-item { border: 1px solid transparent; border-radius: 10px; padding: 10px 12px; cursor: pointer; transition: background .15s; }
.mail-domain-item:hover { background: #f8fafc; }
.mail-domain-item.active { background: #eff6ff; border-color: #bfdbfe; }
.mail-domain-item-top { display: flex; align-items: center; gap: 6px; }
.mail-domain-item-name { flex: 0 1 auto; min-width: 0; font-size: 15px; font-weight: 600; color: #0f172a; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mail-domain-status { flex: 0 0 auto; margin-left: auto; font-size: 13.5px; font-weight: 600; }
.mail-domain-status.ok { color: #16a34a; }
.mail-domain-status.bad { color: #dc2626; }
.mail-domain-status.checking { color: #2563eb; }
.mail-domain-item-sub { font-size: 11px; margin-top: 4px; }
.mail-domain-item-sub.ok { color: #16a34a; }
.mail-domain-item-sub.bad { color: #d97706; }
.mail-domain-actions { margin-top: 8px; display: flex; flex-wrap: nowrap; gap: 2px; }
.mail-domain-actions .el-button { flex: 1 1 0; min-width: 0; margin: 0; padding: 5px 2px; font-size: 13px; }
.mail-domain-empty { color: #94a3b8; font-size: 13px; text-align: center; line-height: 1.8; padding: 40px 0; }
/* 右栏 */
.mail-main { flex: 1; min-width: 0; display: flex; flex-direction: column; background: #fff; border-left: 1px solid #e5e7eb; overflow: hidden; }
.mail-main-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: #94a3b8; }
.mail-main-empty p { margin: 0; font-size: 13px; }
.mail-main-top { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; padding: 0 16px; border-bottom: 1px solid #f1f5f9; flex-wrap: wrap; }
.mail-tabs { flex: 1; min-width: 0; }
.mail-tabs :deep(.el-tabs__header) { margin: 0; }
.mail-main-body { flex: 1; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; min-height: 0; }
.ph { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 90px 20px; }
.ph-title { font-size: 16px; font-weight: 600; color: #475569; margin: 4px 0 0; }
.ph-sub { font-size: 13px; color: #94a3b8; margin: 0; text-align: center; }
/* 右栏各面板头 */
.mail-pane-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 14px; min-height: 52px; }
.mail-pane-head-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.mail-pane-head-left { display: flex; align-items: center; gap: 10px; }
.mail-pane-title { font-size: 15px; font-weight: 700; color: #0f172a; }
.mail-pane-head-actions { display: flex; align-items: center; gap: 8px; }
.mail-accounts-pane { display: flex; flex-direction: column; }
.mail-accounts-pane :deep(.el-table--small),
.mail-accounts-pane :deep(.el-table--small .el-table__cell),
.mail-accounts-pane :deep(.el-table--small .cell) { font-size: 14px; }
.mail-accounts-pane :deep(.el-table--small .el-table__cell) { padding: 10px 0; }
.mail-accounts-pane :deep(.el-table__header th.el-table__cell) { font-size: 14px; font-weight: 700; color: #334155; }
.mail-accounts-pane :deep(.el-table .el-tag--small) { font-size: 13px; }
.mail-accounts-pane :deep(.el-table .el-button--small) { font-size: 14px; }
.mail-accounts-pane :deep(.mail-pane-head .el-button--small) { font-size: 13.5px; }
.acc-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
/* 域名信息 */
.dom-info { display: flex; flex-direction: column; gap: 10px; }
.dom-info-row { display: flex; gap: 10px; font-size: 13.5px; }
.dom-info-label { flex: 0 0 90px; color: #64748b; }
.dom-info-actions { margin-top: 14px; display: flex; gap: 10px; flex-wrap: wrap; }
.md-title { font-size: 18px; font-weight: 700; color: #0f172a; }
.md-subtitle { font-size: 12.5px; color: #94a3b8; margin-left: 8px; }
.md-domain-cell { display: flex; align-items: center; gap: 8px; }
.md-domain-name { font-weight: 600; color: #0f172a; }
.md-status-ok { font-size: 12.5px; color: #059669; font-weight: 500; }
.md-status-bad { font-size: 12.5px; color: #d97706; line-height: 1.5; max-width: 100%; }
.md-steps { margin: 6px 0 20px; }
.md-step-body { min-height: 180px; }
.md-field-hint { font-size: 13px; color: #64748b; margin-bottom: 10px; }
.md-err { color: #dc2626; font-size: 13px; margin-top: 8px; }
.md-step2-tip { font-size: 13px; color: #334155; background: #eff6ff; border: 1px solid #bfdbfe; padding: 10px 12px; border-radius: 8px; margin-bottom: 14px; line-height: 1.6; }
.md-link { color: #2563eb; font-weight: 600; }
.md-guide-rec { border: 1px solid #e2e8f0; border-radius: 8px; padding: 10px 12px; margin-bottom: 10px; }
.md-rec-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.md-rec-why { font-size: 12.5px; color: #64748b; }
.md-rec-field { display: flex; align-items: center; gap: 8px; padding: 3px 0; font-size: 13px; }
.md-rec-label { flex: 0 0 84px; color: #64748b; }
.md-rec-val { flex: 1; color: #0f172a; font-family: ui-monospace, monospace; word-break: break-all; }
.md-step2-foot { font-size: 13px; color: #475569; margin-top: 6px; }
.md-checking { display: flex; align-items: center; gap: 8px; font-size: 16px; font-weight: 700; margin-bottom: 10px; }
.md-checking .el-icon { font-size: 20px; }
.md-checking.ok { color: #059669; }
.md-checking.checking { color: #2563eb; }
.md-checking:not(.ok):not(.checking) { color: #d97706; }
.md-check-detail { font-size: 13.5px; color: #475569; background: #f8fafc; border-radius: 8px; padding: 10px 12px; line-height: 1.7; }
.md-check-sub { font-size: 12.5px; color: #94a3b8; margin-top: 10px; }
.md-dnsguide-body .md-rec-field { border-bottom: 1px dashed #e2e8f0; padding: 8px 0; }
.md-dnsguide-foot { margin-top: 14px; display: flex; align-items: center; gap: 12px; }
.acc-form { display: flex; flex-direction: column; gap: 12px; max-width: 520px; }
.acc-row { display: flex; align-items: center; gap: 10px; }
.acc-label { flex: 0 0 90px; color: #475569; font-size: 13.5px; }
.acc-submit { margin-top: 6px; }
.acc-batch-tip { font-size: 13px; color: #64748b; margin-bottom: 8px; }
.acc-batch-tip code { background: #f1f5f9; padding: 1px 6px; border-radius: 4px; }
.acc-rand-grid { display: flex; flex-direction: column; gap: 12px; max-width: 520px; }
.acc-result-title { font-size: 13px; font-weight: 600; color: #0f172a; margin-top: 10px; }
.acc-result-box { background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 8px; padding: 10px 12px; max-height: 220px; overflow: auto; margin-top: 8px; }
.acc-table-gap { margin-top: 12px; }
.acc-result-line { font-size: 13px; color: #166534; padding: 3px 0; font-family: ui-monospace, monospace; }
/* 收件箱 */
.mail-inbox-pane { display: flex; flex-direction: column; flex: 1; min-height: 0; }
.msg-list-wrap { position: relative; user-select: none; -webkit-user-select: none; flex: 1; min-height: 0; display: flex; flex-direction: column; }
.msg-list-wrap * { user-select: none; -webkit-user-select: none; }
/* 收件箱列表：整体放大字号、加高行，便于看清 */
.msg-list-wrap :deep(.el-table__header th.el-table__cell) { font-size: 14px; font-weight: 700; color: #334155; }
.msg-list-wrap :deep(.el-table td.el-table__cell) { padding: 12px 0; }
.msg-list-wrap :deep(.el-table__body td.el-table__cell .cell) { font-size: 14px; line-height: 1.5; }
.msg-list-wrap :deep(.el-table__body .msg-unread) { font-weight: 600; }
.msg-list-wrap :deep(.el-button.is-link) { font-size: 14px; }
.msg-drag-rect { position: fixed; background: rgba(37, 99, 235, 0.10); border: 1px solid #2563eb; border-radius: 2px; pointer-events: none; z-index: 9999; }
/* 收件箱：复选框列宽度与表头/行单元格内边距统一，避免复选框左右不对齐；并放大复选框本体 */
.mail-inbox-pane :deep(.el-table .el-table-column--selection .cell) { padding-left: 14px !important; padding-right: 6px !important; }
.mail-inbox-pane :deep(.el-table .el-table-column--selection .el-checkbox__inner) { transform: scale(1.2); transform-origin: center; }
.mail-inbox-pane :deep(.el-table .el-table-column--selection .el-checkbox) { display: flex; align-items: center; justify-content: center; }
.msg-batch-info { font-size: 13px; font-weight: 700; color: #1d4ed8; margin-right: 4px; }
.msg-unread-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #2563eb; }
.msg-unread { font-weight: 700; color: #0f172a; }
/* 未读行整行浅蓝高亮，已读行默认正常 */
.mail-inbox-pane :deep(.el-table .msg-row-unread td.el-table__cell) { background: #f0f7ff; }
.mail-inbox-pane :deep(.el-table .msg-row-unread:hover > td.el-table__cell) { background: #e5f1ff; }
.mail-inbox-pane :deep(.el-table .msg-row-unread .msg-unread-dot) { background: #2563eb; }
.muted { color: #94a3b8; }
.mono { font-family: ui-monospace, monospace; }
.msg-time { font-size: 13.5px; color: #475569; white-space: nowrap; }
/* 手机端：时间折行到发件人/收件人下方（配合隐藏「时间」列） */
.msg-time-sub { display: block; font-size: 11px; color: #94a3b8; margin-top: 2px; white-space: nowrap; }
.acc-addr-sub { font-size: 11.5px; color: #94a3b8; margin-top: 2px; }
.msg-attach-icon { color: #94a3b8; margin-right: 4px; vertical-align: -2px; }
.sent-to { color: #334155; word-break: break-all; }
.mail-msg-detail { border: 1px solid #e2e8f0; border-radius: 10px; }
.msg-detail-top { padding: 14px 16px; border-bottom: 1px solid #eef2f7; }
.msg-detail-subject { font-size: 16px; font-weight: 700; color: #0f172a; margin-bottom: 8px; word-break: break-word; }
.msg-detail-meta { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; font-size: 13px; color: #475569; }
.msg-detail-date { color: #94a3b8; }
.msg-body { padding: 16px; line-height: 1.7; font-size: 14px; color: #1e293b; word-break: break-word; max-height: 420px; overflow: auto; }
.msg-body :deep(img) { max-width: 100%; }
.msg-detail-actions { display: flex; gap: 10px; padding: 12px 16px; border-top: 1px solid #eef2f7; }
.msg-attach-box { border-top: 1px dashed #e2e8f0; padding: 10px 16px; }
.msg-attach-title { font-size: 13px; color: #475569; font-weight: 600; display: flex; align-items: center; gap: 4px; margin-bottom: 8px; }
.msg-attach-list { display: flex; flex-direction: column; gap: 4px; }
.msg-attach-row { display: flex; align-items: center; gap: 8px; font-size: 13px; background: #f8fafc; border-radius: 6px; padding: 6px 10px; }
.msg-attach-ic { color: #64748b; }
.msg-attach-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #1e293b; }
.msg-attach-size { color: #94a3b8; font-size: 12px; }
.mc-quote-head { color: #64748b; font-size: 13px; margin: 4px 0 4px; }
.mc-quote-meta { color: #94a3b8; font-size: 12.5px; line-height: 1.6; }
.mc-quote { border-left: 3px solid #cbd5e1; margin: 4px 0; padding: 4px 10px; color: #475569; background: #f8fafc; }

/* ============ 平板适配（<1024px，此时侧栏已是抽屉，右侧整宽） ============ */
@media (max-width: 1023px) {
  .mail-side { width: 210px; flex: 0 0 210px; }
  /* 域名操作按钮两行排列 */
  .mail-domain-actions { flex-wrap: wrap; row-gap: 4px; }
  .mail-domain-actions .el-button { flex: 1 1 40%; }
}

/* ============ 手机适配（<768px）：左右两栏改为上下堆叠 ============ */
@media (max-width: 767px) {
  /* 外壳高度自适应，交给外层容器滚动 */
  .mail-shell { flex-direction: column; height: auto; min-height: 0; }

  .mail-side { width: 100%; flex: 0 0 auto; }
  .mail-side-head { padding: 10px 12px; }
  .mail-domain-list { display: none; }
  .mail-domain-empty { padding: 18px 0; }
  .mail-side-mobile { display: block; padding: 12px; }
  .msm-row { display: flex; align-items: center; gap: 8px; }
  .msm-select { flex: 1; min-width: 0; }
  .msm-actions { display: flex; gap: 6px; margin-top: 10px; }
  .msm-actions .el-button { flex: 1 1 0; min-width: 0; margin: 0; padding: 5px 2px; font-size: 13px; }

  .mail-main { border-left: none; border-top: 1px solid #e5e7eb; }
  .mail-main-top { padding: 0 12px; flex-wrap: nowrap; }
  .mail-main-body { overflow: visible; padding: 12px; }
  .mail-main-empty { padding: 40px 20px; }

  /* 收窄页签内边距与字号 */
  .mail-tabs :deep(.el-tabs__item) { padding: 0 10px; font-size: 13.5px; }

  .mail-pane-head { flex-wrap: wrap; gap: 10px; min-height: 0; margin-bottom: 12px; }
  .mail-pane-head-left { flex: 1 1 100%; min-width: 0; }
  .mail-pane-head-actions { flex: 1 1 auto; justify-content: flex-start; }
  /* 邮箱账号下拉占满剩余宽度 */
  .mail-pane-head-left :deep(.el-select) { flex: 1 1 120px; min-width: 0; width: auto !important; }

  .msg-detail-top { padding: 12px; }
  .msg-body { padding: 12px; max-height: none; }
  .msg-attach-box { padding: 10px 12px; }
  .msg-detail-actions { flex-wrap: wrap; gap: 8px; padding: 12px; }

  .msg-list-wrap :deep(.el-table td.el-table__cell) { padding: 9px 0; }
  .mail-inbox-pane :deep(.el-table .el-table-column--selection .cell) { padding-left: 8px !important; padding-right: 2px !important; }

  .acc-form, .acc-rand-grid { max-width: 100%; }
  .acc-row { flex-wrap: wrap; }
  .acc-label { flex: 0 0 72px; }
  .acc-row :deep(.el-input), .acc-row :deep(.el-input-number) { max-width: none !important; flex: 1 1 auto; }
  .acc-row :deep(.el-select) { width: 100% !important; }
  .acc-form > .acc-row, .acc-rand-grid > .acc-row { align-items: center; }
}
</style>
