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
