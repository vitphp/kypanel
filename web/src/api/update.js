import request from '@/utils/request'

export function checkUpdate(arch, force = false) {
  return request({
    url: '/update/check',
    method: 'get',
    params: { arch, force: force ? 1 : undefined }
  })
}

export function upgradePanel(data) {
  return request({
    url: '/update/upgrade',
    method: 'post',
    data
  })
}
