import request from '../utils/request'

// 邮箱域名列表
export function listMailDomains() {
  return request({ url: '/mail/domains', method: 'get' })
}

// 添加域名
export function addMailDomain(data) {
  return request({ url: '/mail/domains', method: 'post', data })
}

// 更新域名（启停/备注/配额/DNS 勾选）
export function updateMailDomain(id, data) {
  return request({ url: `/mail/domains/${id}`, method: 'patch', data })
}

// 删除域名
export function deleteMailDomain(id) {
  return request({ url: `/mail/domains/${id}`, method: 'delete' })
}

// DNS 绑定引导
export function getMailDnsGuide(id) {
  return request({ url: `/mail/domains/${id}/dns-guide`, method: 'get' })
}

// DNS 自检（MX 是否生效）
export function checkMailDns(id) {
  return request({ url: `/mail/domains/${id}/dns-check`, method: 'get' })
}
