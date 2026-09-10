import request from '../utils/request'

// ===== 域名 =====
export function listMailDomains() {
  return request({ url: '/mail/domains', method: 'get' })
}
export function addMailDomain(data) {
  return request({ url: '/mail/domains', method: 'post', data })
}
export function updateMailDomain(id, data) {
  return request({ url: `/mail/domains/${id}`, method: 'patch', data })
}
export function deleteMailDomain(id) {
  return request({ url: `/mail/domains/${id}`, method: 'delete' })
}
export function getMailDnsGuide(id) {
  return request({ url: `/mail/domains/${id}/dns-guide`, method: 'get' })
}

// ===== 添加向导：给定域名(可未入库)生成配置值 + 检测是否指向本机 =====
export function getDomainGuide(domain) {
  return request({ url: '/mail/domain-guide', method: 'get', params: { domain } })
}
export function checkDomainReady(domain) {
  return request({ url: '/mail/domain-check', method: 'get', params: { domain } })
}

// ===== 已入库域名：按 id 批量检测对接状态（返回 map: id -> {ready, not_ready_msg, ...}）=====
export function checkMailDomainsReady(domainIds) {
  return request({ url: '/mail/domains/check-ready', method: 'post', data: { domain_ids: domainIds } })
}

// ===== 邮箱门户网站（官网 / webmail）=====
export function getMailPortal(id) {
  return request({ url: `/mail/domains/${id}/portal`, method: 'get' })
}
export function saveMailPortal(id, data) {
  // 开启 HTTPS 且无证书时后端会自动申请证书（ACME 文件验证可能耗时数分钟），故放宽超时
  return request({ url: `/mail/domains/${id}/portal`, method: 'put', data, timeout: 600000 })
}
// 上传门户 Logo（图片，≤2MB）
export function uploadMailPortalLogo(id, formData) {
  return request({
    url: `/mail/domains/${id}/portal/logo`,
    method: 'post',
    data: formData,
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 60000
  })
}
// 为门户申请/重新申请免费证书（可一次签发多个域名）
export function applyMailPortalCert(id, data) {
  return request({ url: `/mail/domains/${id}/portal/apply-cert`, method: 'post', data, timeout: 600000 })
}

// ===== 账号 =====
export function listMailAccounts(domainId) {
  return request({ url: '/mail/accounts', method: 'get', params: { domain_id: domainId } })
}
export function addMailAccount(data) {
  return request({ url: '/mail/accounts', method: 'post', data })
}
export function addMailAccountsBatch(data) {
  return request({ url: '/mail/accounts/batch', method: 'post', data })
}
export function randomMailAccounts(data) {
  return request({ url: '/mail/accounts/random', method: 'post', data })
}
export function deleteMailAccount(id) {
  return request({ url: `/mail/accounts/${id}`, method: 'delete' })
}
export function setMailAccountEnabled(id, enabled) {
  return request({ url: `/mail/accounts/${id}/enabled`, method: 'patch', data: { enabled } })
}
export function resetMailAccountPassword(id, password) {
  return request({ url: `/mail/accounts/${id}/password`, method: 'patch', data: { password } })
}

// ===== 收件箱（消息）=====
export function listMailMessages(mailboxId, folder) {
  return request({ url: '/mail/messages', method: 'get', params: { mailbox_id: mailboxId, folder: folder || 'inbox' } })
}
export function getMailMessage(mailboxId, id) {
  return request({ url: `/mail/messages/${id}`, method: 'get', params: { mailbox_id: mailboxId } })
}
export function deleteMailMessage(mailboxId, id) {
  return request({ url: `/mail/messages/${id}`, method: 'delete', params: { mailbox_id: mailboxId } })
}
export function setMailMessagesSeen(mailboxId, ids, seen) {
  return request({ url: '/mail/messages/seen', method: 'post', data: { mailbox_id: mailboxId, ids, seen } })
}
// 全部未读数（左侧菜单红点）
export function getMailUnreadCount() {
  return request({ url: '/mail/unread-count', method: 'get' })
}
// 某账号收件箱未读数
export function getMailboxUnreadCount(mailboxId) {
  return request({ url: '/mail/messages/unread-count', method: 'get', params: { mailbox_id: mailboxId } })
}
// 一键全部已读
export function markMailAllSeen(mailboxId) {
  return request({ url: '/mail/messages/read-all', method: 'post', data: { mailbox_id: mailboxId } })
}

// ===== 发信 =====
export function sendMail(data) {
  return request({ url: '/mail/send', method: 'post', data, timeout: 120000 })
}
// 保存草稿
export function saveMailDraft(data) {
  return request({ url: '/mail/drafts', method: 'post', data })
}
// 下载附件（返回 blob）
export function downloadMailAttachment(mailboxId, msgId, index) {
  return request({
    url: `/mail/messages/${msgId}/attachment`,
    method: 'get',
    params: { mailbox_id: mailboxId, index },
    responseType: 'blob',
    silent: true
  })
}
